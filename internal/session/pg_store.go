package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/pkg/events"
)

// pgStore implements Store using PostgreSQL.
type pgStore struct {
	db      *dbutil.DB
	dialect dbutil.Dialect
	queries map[string]string // Rebound queries ($N placeholders)
	log     *slog.Logger
}

// NewPGStore creates and initializes a new pgStore using the provided db connection.
func NewPGStore(ctx context.Context, db *dbutil.DB) (Store, error) {
	if err := RunMigrations(ctx, db.DB, dbutil.DialectPostgres); err != nil {
		return nil, fmt.Errorf("session store: pg migrations: %w", err)
	}

	// Copy and rebind all queries from ? to $N placeholders.
	q := make(map[string]string, len(queries))
	for k, v := range queries {
		q[k] = dbutil.DialectPostgres.Rebind(v)
	}

	return &pgStore{
		db:      db,
		dialect: dbutil.DialectPostgres,
		queries: q,
		log:     slog.Default().With("component", "session_pg_store"),
	}, nil
}

// Upsert inserts or updates a session record.
// Unlike SQLiteStore, no write serialization is needed — PG handles concurrency natively.
func (s *pgStore) Upsert(ctx context.Context, info *SessionInfo) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	var ctxJSON []byte
	if info.Context != nil {
		var err error
		ctxJSON, err = json.Marshal(info.Context)
		if err != nil {
			return fmt.Errorf("session store: marshal context: %w", err)
		}
	}

	var platformKeyJSON []byte
	if info.PlatformKey != nil {
		var err2 error
		platformKeyJSON, err2 = json.Marshal(info.PlatformKey)
		if err2 != nil {
			return fmt.Errorf("session store: marshal platform key: %w", err2)
		}
	}

	_, err := s.db.ExecContext(ctx, s.queries["sessions.upsert_session"],
		info.ID, info.UserID, info.OwnerID, info.BotID, info.WorkerSessionID, info.WorkerType, string(info.State),
		info.Platform, string(platformKeyJSON), info.WorkDir, info.Title,
		info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
		string(ctxJSON), info.Source,
	)
	if err != nil {
		return fmt.Errorf("session store: upsert: %w", err)
	}
	return nil
}

// Get loads a session by ID. Returns ErrSessionNotFound if not found.
func (s *pgStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	info, err := scanSession(s.db.QueryRowContext(ctx, s.queries["store.get_session"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session store: load: %w", err)
	}
	return info, nil
}

// List returns sessions with pagination, excluding soft-deleted records.
func (s *pgStore) List(ctx context.Context, userID, platform string, limit, offset int) ([]*SessionInfo, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, s.queries["store.list_sessions"], userID, userID, platform, platform, limit, offset)
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

// GetExpiredMaxLifetime returns session IDs that exceeded their max lifetime.
func (s *pgStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["store.get_expired_max_lifetime"],
		string(events.StateCreated), string(events.StateRunning), string(events.StateIdle), now)
	if err != nil {
		return nil, fmt.Errorf("session store: get expired max lifetime: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectIDs(rows)
}

// GetExpiredIdle returns session IDs that exceeded their idle timeout.
func (s *pgStore) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["store.get_expired_idle"], events.StateIdle, now)
	if err != nil {
		return nil, fmt.Errorf("session store: get expired idle: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectIDs(rows)
}

// DeleteTerminated removes terminated sessions older than the respective cutoffs.
func (s *pgStore) DeleteTerminated(ctx context.Context, cronCutoff, defaultCutoff time.Time) error {
	_, err := s.db.ExecContext(ctx, s.queries["store.delete_terminated"], events.StateTerminated, cronCutoff, defaultCutoff)
	if err != nil {
		return fmt.Errorf("session store: delete terminated: %w", err)
	}
	return nil
}

// DeletePhysical deletes a session by ID, bypassing the state machine.
func (s *pgStore) DeletePhysical(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.queries["store.delete_physical"], id)
	if err != nil {
		return fmt.Errorf("session store: delete physical: %w", err)
	}
	return nil
}

// Compact is a no-op for PostgreSQL. PG handles bloat automatically with autovacuum;
// there is no equivalent of SQLite VACUUM needed for routine session table maintenance.
func (s *pgStore) Compact(_ context.Context, _ float64) error {
	return nil
}

// GetSessionsByState returns all session IDs in the given state.
func (s *pgStore) GetSessionsByState(ctx context.Context, state events.SessionState) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, s.queries["store.get_sessions_by_state"], string(state))
	if err != nil {
		return nil, fmt.Errorf("session store: query by state: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return collectIDs(rows)
}

// Close is a no-op for pgStore — the connection is managed by gatewayStores,
// which calls s.db.Close() on the shared *dbutil.DB after s.session.Close().
func (s *pgStore) Close() error {
	return nil
}

var _ Store = (*pgStore)(nil)
