package groupchat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// ErrGroupNotFound is returned when a group session is not found.
var ErrGroupNotFound = errors.New("groupchat store: group not found")

// Store defines the persistence interface for group chat sessions.
type Store interface {
	// Group session lifecycle
	CreateGroup(ctx context.Context, gs *GroupSession) error
	GetGroup(ctx context.Context, id string) (*GroupSession, error)
	UpdateGroupState(ctx context.Context, id string, state GroupState, endReason EndReason) error
	UpdateGroupCost(ctx context.Context, id string, turnCount int, costAccumulated float64) error
	ListActiveByOwner(ctx context.Context, ownerID string) ([]*GroupSession, error)
	CountActive(ctx context.Context) (int, error)
	CountActiveByOwner(ctx context.Context, ownerID string) (int, error)
	ListActive(ctx context.Context) ([]*GroupSession, error)

	// Turn history
	AppendTurn(ctx context.Context, t *TurnRecord) error
	ListTurns(ctx context.Context, groupID string) ([]*TurnRecord, error)

	// Audit
	RecordAudit(ctx context.Context, e *AuditEvent) error

	// Lifecycle
	Close() error
}

const storeTimeout = 5 * time.Second

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, storeTimeout)
}

// ---------------------------------------------------------------------------
// SQLite implementation
// ---------------------------------------------------------------------------

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db      *sql.DB
	log     *slog.Logger
	writeMu *sqlutil.WriteMu
}

// NewSQLiteStore creates a new group chat store backed by SQLite.
func NewSQLiteStore(db *sql.DB, log *slog.Logger, writeMu *sqlutil.WriteMu) *SQLiteStore {
	return &SQLiteStore{db: db, log: log.With("component", "groupchat_store"), writeMu: writeMu}
}

func (s *SQLiteStore) Close() error { return nil }

const groupCols = `id, topic, platform, channel_id, thread_ts, owner_id, initiator,
	bot_ids, state, max_turns, turn_count, cost_limit_usd, cost_accumulated,
	turn_timeout_sec, cooldown_ms, end_reason, created_at, updated_at, ended_at`

func (s *SQLiteStore) CreateGroup(ctx context.Context, gs *GroupSession) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	botIDs, _ := json.Marshal(gs.BotIDs)
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, `INSERT OR REPLACE INTO group_sessions (
			id, topic, platform, channel_id, thread_ts, owner_id, initiator,
			bot_ids, state, max_turns, turn_count, cost_limit_usd, cost_accumulated,
			turn_timeout_sec, cooldown_ms, end_reason, created_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			gs.ID, gs.Topic, gs.Platform, gs.ChannelID, gs.ThreadTS,
			gs.OwnerID, gs.Initiator, string(botIDs), string(gs.State),
			gs.MaxTurns, gs.TurnCount, gs.CostLimitUSD, gs.CostAccumulated,
			gs.TurnTimeoutSec, gs.CooldownMS, string(gs.EndReason),
			gs.CreatedAt, gs.UpdatedAt)
		if err != nil {
			return fmt.Errorf("groupchat store: create group: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) GetGroup(ctx context.Context, id string) (*GroupSession, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx, `SELECT `+groupCols+` FROM group_sessions WHERE id = ?`, id)
	gs, err := s.scanGroup(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("groupchat store: get group: %w", err)
	}
	return gs, nil
}

func (s *SQLiteStore) UpdateGroupState(ctx context.Context, id string, state GroupState, endReason EndReason) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	return s.writeMu.WithLock(func() error {
		now := time.Now()
		var endedAt *time.Time
		if state != GroupStateActive {
			endedAt = &now
		}
		_, err := s.db.ExecContext(ctx,
			`UPDATE group_sessions SET state = ?, end_reason = ?, updated_at = ?, ended_at = COALESCE(?, ended_at) WHERE id = ?`,
			string(state), string(endReason), now, endedAt, id)
		if err != nil {
			return fmt.Errorf("groupchat store: update state: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) UpdateGroupCost(ctx context.Context, id string, turnCount int, costAccumulated float64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx,
			`UPDATE group_sessions SET turn_count = ?, cost_accumulated = ?, updated_at = ? WHERE id = ?`,
			turnCount, costAccumulated, time.Now(), id)
		if err != nil {
			return fmt.Errorf("groupchat store: update cost: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) ListActiveByOwner(ctx context.Context, ownerID string) ([]*GroupSession, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+groupCols+` FROM group_sessions WHERE owner_id = ? AND state = 'active' ORDER BY created_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("groupchat store: list active by owner: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return s.scanGroups(rows)
}

func (s *SQLiteStore) CountActive(ctx context.Context) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_sessions WHERE state = 'active'`).Scan(&count)
	return count, err
}

func (s *SQLiteStore) CountActiveByOwner(ctx context.Context, ownerID string) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM group_sessions WHERE owner_id = ? AND state = 'active'`, ownerID).Scan(&count)
	return count, err
}

func (s *SQLiteStore) ListActive(ctx context.Context) ([]*GroupSession, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+groupCols+` FROM group_sessions WHERE state = 'active' ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("groupchat store: list active: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return s.scanGroups(rows)
}

// Turn operations

const turnCols = `id, group_session_id, bot_id, bot_name, turn_num,
	content, skipped, sanitized, sanitize_reason, timeout_count, cost_usd, created_at`

func (s *SQLiteStore) AppendTurn(ctx context.Context, t *TurnRecord) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, `INSERT INTO group_turns (
			id, group_session_id, bot_id, bot_name, turn_num,
			content, skipped, sanitized, sanitize_reason, timeout_count, cost_usd, created_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.ID, t.GroupID, t.BotID, t.BotName, t.TurnNum,
			t.Content, boolToInt(t.Skipped), boolToInt(t.Sanitized),
			t.SanitizeReason, t.TimeoutCount, t.CostUSD, t.CreatedAt)
		if err != nil {
			return fmt.Errorf("groupchat store: append turn: %w", err)
		}
		return nil
	})
}

func (s *SQLiteStore) ListTurns(ctx context.Context, groupID string) ([]*TurnRecord, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+turnCols+` FROM group_turns WHERE group_session_id = ? ORDER BY turn_num`, groupID)
	if err != nil {
		return nil, fmt.Errorf("groupchat store: list turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var turns []*TurnRecord
	for rows.Next() {
		t, err := s.scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

// Audit

func (s *SQLiteStore) RecordAudit(ctx context.Context, e *AuditEvent) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO group_chat_audit (event_type, session_id, bot_id, initiator, turn_num, detail, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			e.EventType, e.SessionID, e.BotID, e.Initiator, e.TurnNum, e.Detail, e.CreatedAt)
		if err != nil {
			return fmt.Errorf("groupchat store: record audit: %w", err)
		}
		return nil
	})
}

// Scan helpers

type scanner interface {
	Scan(dest ...any) error
}

func (s *SQLiteStore) scanGroup(row scanner) (*GroupSession, error) {
	return scanGroupRow(row)
}

func (s *SQLiteStore) scanGroups(rows *sql.Rows) ([]*GroupSession, error) {
	var groups []*GroupSession
	for rows.Next() {
		gs, err := scanGroupRow(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, gs)
	}
	return groups, rows.Err()
}

func (*SQLiteStore) scanTurn(row scanner) (*TurnRecord, error) {
	return scanTurnRow(row)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Shared scan helpers (used by both SQLite and PG implementations).

func scanGroupRow(row scanner) (*GroupSession, error) {
	gs := &GroupSession{BotNames: make(map[string]string)}
	var botIDsJSON, stateStr, endReasonStr string
	var endedAt sql.NullTime

	err := row.Scan(
		&gs.ID, &gs.Topic, &gs.Platform, &gs.ChannelID, &gs.ThreadTS,
		&gs.OwnerID, &gs.Initiator, &botIDsJSON, &stateStr,
		&gs.MaxTurns, &gs.TurnCount, &gs.CostLimitUSD, &gs.CostAccumulated,
		&gs.TurnTimeoutSec, &gs.CooldownMS, &endReasonStr,
		&gs.CreatedAt, &gs.UpdatedAt, &endedAt,
	)
	if err != nil {
		return nil, err
	}

	gs.State = GroupState(stateStr)
	gs.EndReason = EndReason(endReasonStr)
	if endedAt.Valid {
		gs.EndedAt = &endedAt.Time
	}
	_ = json.Unmarshal([]byte(botIDsJSON), &gs.BotIDs)
	return gs, nil
}

func scanTurnRow(row scanner) (*TurnRecord, error) {
	t := &TurnRecord{}
	var skipped, sanitized int
	err := row.Scan(
		&t.ID, &t.GroupID, &t.BotID, &t.BotName, &t.TurnNum,
		&t.Content, &skipped, &sanitized,
		&t.SanitizeReason, &t.TimeoutCount, &t.CostUSD, &t.CreatedAt,
	)
	t.Skipped = skipped == 1
	t.Sanitized = sanitized == 1
	return t, err
}

// ---------------------------------------------------------------------------
// PostgreSQL implementation
// ---------------------------------------------------------------------------

// PGStore implements Store using PostgreSQL.
type PGStore struct {
	db  *dbutil.DB
	log *slog.Logger
}

// NewPGStore creates a new group chat store backed by PostgreSQL.
func NewPGStore(db *dbutil.DB, log *slog.Logger) *PGStore {
	return &PGStore{db: db, log: log.With("component", "groupchat_pg_store")}
}

func (s *PGStore) Close() error { return nil }

func (s *PGStore) rebind(query string) string {
	return s.db.Dialect().Rebind(query)
}

func (s *PGStore) CreateGroup(ctx context.Context, gs *GroupSession) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	botIDs, _ := json.Marshal(gs.BotIDs)
	q := s.rebind(`INSERT INTO group_sessions (
		id, topic, platform, channel_id, thread_ts, owner_id, initiator,
		bot_ids, state, max_turns, turn_count, cost_limit_usd, cost_accumulated,
		turn_timeout_sec, cooldown_ms, end_reason, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	_, err := s.db.ExecContext(ctx, q,
		gs.ID, gs.Topic, gs.Platform, gs.ChannelID, gs.ThreadTS,
		gs.OwnerID, gs.Initiator, string(botIDs), string(gs.State),
		gs.MaxTurns, gs.TurnCount, gs.CostLimitUSD, gs.CostAccumulated,
		gs.TurnTimeoutSec, gs.CooldownMS, string(gs.EndReason),
		gs.CreatedAt, gs.UpdatedAt)
	if err != nil {
		return fmt.Errorf("groupchat pg store: create group: %w", err)
	}
	return nil
}

func (s *PGStore) GetGroup(ctx context.Context, id string) (*GroupSession, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`SELECT ` + groupCols + ` FROM group_sessions WHERE id = ?`)
	row := s.db.QueryRowContext(ctx, q, id)
	gs, err := scanGroupRow(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("groupchat pg store: get group: %w", err)
	}
	return gs, nil
}

func (s *PGStore) UpdateGroupState(ctx context.Context, id string, state GroupState, endReason EndReason) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now()
	var endedAt *time.Time
	if state != GroupStateActive {
		endedAt = &now
	}
	q := s.rebind(`UPDATE group_sessions SET state = ?, end_reason = ?, updated_at = ?, ended_at = COALESCE(?, ended_at) WHERE id = ?`)
	_, err := s.db.ExecContext(ctx, q, string(state), string(endReason), now, endedAt, id)
	if err != nil {
		return fmt.Errorf("groupchat pg store: update state: %w", err)
	}
	return nil
}

func (s *PGStore) UpdateGroupCost(ctx context.Context, id string, turnCount int, costAccumulated float64) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`UPDATE group_sessions SET turn_count = ?, cost_accumulated = ?, updated_at = ? WHERE id = ?`)
	_, err := s.db.ExecContext(ctx, q, turnCount, costAccumulated, time.Now(), id)
	if err != nil {
		return fmt.Errorf("groupchat pg store: update cost: %w", err)
	}
	return nil
}

func (s *PGStore) ListActiveByOwner(ctx context.Context, ownerID string) ([]*GroupSession, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`SELECT ` + groupCols + ` FROM group_sessions WHERE owner_id = ? AND state = 'active' ORDER BY created_at DESC`)
	rows, err := s.db.QueryContext(ctx, q, ownerID)
	if err != nil {
		return nil, fmt.Errorf("groupchat pg store: list active by owner: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanGroupsPG(rows)
}

func (s *PGStore) CountActive(ctx context.Context) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM group_sessions WHERE state = 'active'`).Scan(&count)
	return count, err
}

func (s *PGStore) CountActiveByOwner(ctx context.Context, ownerID string) (int, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var count int
	q := s.rebind(`SELECT COUNT(*) FROM group_sessions WHERE owner_id = ? AND state = 'active'`)
	err := s.db.QueryRowContext(ctx, q, ownerID).Scan(&count)
	return count, err
}

func (s *PGStore) ListActive(ctx context.Context) ([]*GroupSession, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`SELECT ` + groupCols + ` FROM group_sessions WHERE state = 'active' ORDER BY created_at`)
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("groupchat pg store: list active: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanGroupsPG(rows)
}

func (s *PGStore) AppendTurn(ctx context.Context, t *TurnRecord) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`INSERT INTO group_turns (
		id, group_session_id, bot_id, bot_name, turn_num,
		content, skipped, sanitized, sanitize_reason, timeout_count, cost_usd, created_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`)
	_, err := s.db.ExecContext(ctx, q,
		t.ID, t.GroupID, t.BotID, t.BotName, t.TurnNum,
		t.Content, boolToInt(t.Skipped), boolToInt(t.Sanitized),
		t.SanitizeReason, t.TimeoutCount, t.CostUSD, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("groupchat pg store: append turn: %w", err)
	}
	return nil
}

func (s *PGStore) ListTurns(ctx context.Context, groupID string) ([]*TurnRecord, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`SELECT ` + turnCols + ` FROM group_turns WHERE group_session_id = ? ORDER BY turn_num`)
	rows, err := s.db.QueryContext(ctx, q, groupID)
	if err != nil {
		return nil, fmt.Errorf("groupchat pg store: list turns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var turns []*TurnRecord
	for rows.Next() {
		t, err := scanTurnRow(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

func (s *PGStore) RecordAudit(ctx context.Context, e *AuditEvent) error {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	q := s.rebind(`INSERT INTO group_chat_audit (event_type, session_id, bot_id, initiator, turn_num, detail, created_at)
		VALUES (?,?,?,?,?,?,?)`)
	_, err := s.db.ExecContext(ctx, q,
		e.EventType, e.SessionID, e.BotID, e.Initiator, e.TurnNum, e.Detail, e.CreatedAt)
	if err != nil {
		return fmt.Errorf("groupchat pg store: record audit: %w", err)
	}
	return nil
}

func scanGroupsPG(rows *sql.Rows) ([]*GroupSession, error) {
	var groups []*GroupSession
	for rows.Next() {
		gs, err := scanGroupRow(rows)
		if err != nil {
			return nil, err
		}
		groups = append(groups, gs)
	}
	return groups, rows.Err()
}
