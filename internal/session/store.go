package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/pkg/events"
)

// Store defines the interface for session persistence.
type Store interface {
	Upsert(ctx context.Context, info *SessionInfo) error
	UpdateWorkerSessionIDSQL(ctx context.Context, id, workerSessionID string) error
	Get(ctx context.Context, id string) (*SessionInfo, error)
	List(ctx context.Context, userID, platform string, limit, offset int) ([]*SessionInfo, error)
	GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error)
	GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error)
	DeleteTerminated(ctx context.Context, cronCutoff, defaultCutoff time.Time) error
	DeletePhysical(ctx context.Context, id string) error
	GetSessionsByState(ctx context.Context, state events.SessionState) ([]string, error)
	Close() error
}

var _ Store = (*SQLiteStore)(nil)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db      *sql.DB
	log     *slog.Logger
	writeMu *sqlutil.WriteMu
}

// DB returns the underlying *sql.DB for sharing with other stores (e.g., eventstore).
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// NewSQLiteStore creates and initializes a new SQLiteStore.
// If writeMu is non-nil, all write operations are serialized through it.
func NewSQLiteStore(ctx context.Context, cfg *config.Config, writeMu *sqlutil.WriteMu) (*SQLiteStore, error) {
	db, err := openSQLiteDB(cfg, dbOpenOpts{
		Label:       "session",
		MaxOpen:     cfg.DB.MaxOpenConns,
		MaxIdle:     cfg.DB.MaxOpenConns,
		MaxLifetime: 0,
		MaxIdleTime: 5 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	if err := RunMigrations(ctx, db, dbutil.DialectSQLite); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, log: slog.Default().With("component", "session_store"), writeMu: writeMu}, nil
}

// marshalSessionJSON serializes the Context and PlatformKey fields for Upsert.
func marshalSessionJSON(info *SessionInfo) (ctxJSON, pkJSON []byte, err error) {
	if info.Context != nil {
		ctxJSON, err = json.Marshal(info.Context)
		if err != nil {
			return nil, nil, fmt.Errorf("session store: marshal context: %w", err)
		}
	}
	if info.PlatformKey != nil {
		pkJSON, err = json.Marshal(info.PlatformKey)
		if err != nil {
			return nil, nil, fmt.Errorf("session store: marshal platform key: %w", err)
		}
	}
	return ctxJSON, pkJSON, nil
}

// upsertTimeout ensures the context has a deadline, defaulting to 5 seconds.
func upsertTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, 5*time.Second)
}

func (s *SQLiteStore) Upsert(ctx context.Context, info *SessionInfo) error {
	ctx, cancel := upsertTimeout(ctx)
	defer cancel()

	ctxJSON, pkJSON, err := marshalSessionJSON(info)
	if err != nil {
		return err
	}

	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["sessions.upsert_session"],
			info.ID, info.UserID, info.OwnerID, info.BotID, info.BotName, info.WorkerSessionID, info.WorkerType, string(info.State),
			info.Platform, string(pkJSON), info.WorkDir, info.Title,
			info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
			string(ctxJSON), info.Source, info.ClientKey,
		)
		if err != nil {
			return fmt.Errorf("session store: upsert: %w", err)
		}
		return nil
	})
}

// UpdateWorkerSessionIDSQL performs a targeted UPDATE on the worker_session_id
// column only, avoiding the full-row overwrite of Upsert.
func (s *SQLiteStore) UpdateWorkerSessionIDSQL(ctx context.Context, id, workerSessionID string) error {
	ctx, cancel := upsertTimeout(ctx)
	defer cancel()
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["sessions.update_worker_session_id"], workerSessionID, id)
		if err != nil {
			return fmt.Errorf("session store: update worker session id: %w", err)
		}
		return nil
	})
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(sc rowScanner) (*SessionInfo, error) {
	var info SessionInfo
	var ctxJSON, platformKeyStr sql.NullString
	var expiresAt, idleExpiresAt sql.NullTime
	var createdAt, updatedAt time.Time

	err := sc.Scan(
		&info.ID, &info.UserID, &info.OwnerID, &info.WorkerSessionID, &info.WorkerType, &info.State, &info.BotID, &info.BotName,
		&info.Platform, &platformKeyStr, &info.WorkDir, &info.Title,
		&createdAt, &updatedAt, &expiresAt, &idleExpiresAt, &ctxJSON, &info.Source, &info.ClientKey,
	)
	if err != nil {
		return nil, err
	}

	info.CreatedAt = createdAt
	info.UpdatedAt = updatedAt
	if expiresAt.Valid {
		info.ExpiresAt = &expiresAt.Time
	}
	if idleExpiresAt.Valid {
		info.IdleExpiresAt = &idleExpiresAt.Time
	}
	if ctxJSON.Valid && ctxJSON.String != "" {
		if err := json.Unmarshal([]byte(ctxJSON.String), &info.Context); err != nil {
			return nil, fmt.Errorf("session store: unmarshal context: %w", err)
		}
	}
	if platformKeyStr.Valid && platformKeyStr.String != "" {
		if err := json.Unmarshal([]byte(platformKeyStr.String), &info.PlatformKey); err != nil {
			return nil, fmt.Errorf("session store: unmarshal platform key: %w", err)
		}
	}
	return &info, nil
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	info, err := scanSession(s.db.QueryRowContext(ctx, queries["store.get_session"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session store: load: %w", err)
	}
	return info, nil
}

func (s *SQLiteStore) List(ctx context.Context, userID, platform string, limit, offset int) ([]*SessionInfo, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, queries["store.list_sessions"], userID, userID, platform, platform, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("session store: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*SessionInfo
	for rows.Next() {
		si, err := scanSession(rows)
		if err != nil {
			s.log.Warn("session store: skipping corrupted row", "err", err)
			continue
		}
		sessions = append(sessions, si)
	}
	return sessions, rows.Err()
}

func collectIDs(rows *sql.Rows) ([]string, error) {
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (s *SQLiteStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, queries["store.get_expired_max_lifetime"],
		string(events.StateCreated), string(events.StateRunning), string(events.StateIdle), now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectIDs(rows)
}

func (s *SQLiteStore) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, queries["store.get_expired_idle"], events.StateIdle, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectIDs(rows)
}

// Events lifecycle is managed independently — session deletion does not cascade to events.
func (s *SQLiteStore) DeleteTerminated(ctx context.Context, cronCutoff, defaultCutoff time.Time) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["store.delete_terminated"], events.StateTerminated, cronCutoff, defaultCutoff)
		if err != nil {
			return fmt.Errorf("session store: delete terminated: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) DeletePhysical(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, queries["store.delete_physical"], id)
		if err != nil {
			return fmt.Errorf("session store: delete physical: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Compact(ctx context.Context, threshold float64) error {
	return s.writeMu.WithLock(func() error {
		var pageCount, freeCount int
		if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
			return fmt.Errorf("session store: compact page_count: %w", err)
		}
		if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freeCount); err != nil {
			return fmt.Errorf("session store: compact freelist_count: %w", err)
		}
		if pageCount == 0 || float64(freeCount)/float64(pageCount) < threshold {
			return nil
		}
		start := time.Now()
		s.log.Info("session store: VACUUM starting",
			"page_count", pageCount, "free_count", freeCount,
			"ratio", fmt.Sprintf("%.1f%%", float64(freeCount)/float64(pageCount)*100))
		if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			return fmt.Errorf("session store: compact checkpoint: %w", err)
		}
		_, err := s.db.ExecContext(ctx, "VACUUM")
		s.log.Info("session store: VACUUM completed", "duration", time.Since(start))
		return err
	})
}

func (s *SQLiteStore) GetSessionsByState(ctx context.Context, state events.SessionState) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, queries["store.get_sessions_by_state"], string(state))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return collectIDs(rows)
}
