package eventstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
)

// pgStore implements EventStore + TurnQuerier using PostgreSQL.
type pgStore struct {
	db      *dbutil.DB
	dialect dbutil.Dialect
	sql     map[string]string // Rebound query cache (PG $N placeholders)
	log     *slog.Logger
}

// pgEventTx is a PostgreSQL-backed transaction for batch event and turn writes.
type pgEventTx struct {
	tx      *sql.Tx
	sql     map[string]string // Reference to pgStore's rebound queries
	dialect dbutil.Dialect
}

// Interface checks.
var (
	_ EventStore  = (*pgStore)(nil)
	_ TurnQuerier = (*pgStore)(nil)
)

// NewPGStore creates a PostgreSQL-backed event store. All embedded SQL queries
// are rebound to PG $N placeholders. The turns.insert query gets an additional
// RETURNING id clause since PG does not support LastInsertId.
func NewPGStore(db *dbutil.DB, log *slog.Logger) *pgStore {
	d := db.Dialect()
	s := &pgStore{
		db:      db,
		dialect: d,
		sql:     make(map[string]string, len(queries)),
		log:     log,
	}
	for k, v := range queries {
		s.sql[k] = d.Rebind(v)
	}
	// Override turns.insert with RETURNING id for PG auto-increment.
	// Strip trailing semicolons to prevent RETURNING from being appended after a statement terminator.
	s.sql["turns.insert"] = d.Rebind(strings.TrimRight(strings.TrimSpace(queries["turns.insert"]), ";")) + " RETURNING id"
	return s
}

// ---------------------------------------------------------------------------
// EventStore implementation
// ---------------------------------------------------------------------------

func (s *pgStore) Append(ctx context.Context, event *StoredEvent) error {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, s.sql["insert"],
		event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.Source, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("eventstore: append: %w", err)
	}
	return nil
}

func (s *pgStore) BeginTx(ctx context.Context) (EventTx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("eventstore: begin tx: %w", err)
	}
	return &pgEventTx{tx: tx, sql: s.sql, dialect: s.dialect}, nil
}

func (s *pgStore) QueryBySession(ctx context.Context, sessionID string, cursor int64, dir CursorDirection, limit int) (*EventPage, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	// Fetch one extra to detect has_more.
	fetchLimit := limit + 1

	var rows *sql.Rows
	var err error

	switch dir {
	case CursorAfter:
		rows, err = s.db.QueryContext(ctx, s.sql["query_after"], sessionID, cursor, fetchLimit)
	case CursorBefore:
		rows, err = s.db.QueryContext(ctx, s.sql["query_before"], sessionID, cursor, fetchLimit)
	default: // CursorLatest
		rows, err = s.db.QueryContext(ctx, s.sql["query_latest"], sessionID, fetchLimit)
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events, err := scanEvents(rows)
	if err != nil {
		return nil, fmt.Errorf("eventstore: scan events: %w", err)
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}

	// For DESC queries (CursorLatest, CursorBefore), reverse to ASC order.
	if dir == CursorLatest || dir == CursorBefore {
		slices.Reverse(events)
	}

	page := &EventPage{
		Events: events,
	}

	if len(events) > 0 {
		page.OldestSeq = events[0].Seq
		page.NewestSeq = events[len(events)-1].Seq
	}

	if len(events) > 0 {
		switch dir {
		case CursorLatest, CursorBefore:
			page.HasOlder = hasMore
		default:
			var exists int
			err := s.db.QueryRowContext(ctx, s.sql["has_older"], sessionID, page.OldestSeq).Scan(&exists)
			page.HasOlder = err == nil && exists == 1
		}
	}

	return page, nil
}

func (s *pgStore) DeleteBySession(ctx context.Context, sessionID string) error {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, s.sql["delete_by_session"], sessionID)
	if err != nil {
		return fmt.Errorf("eventstore: delete by session: %w", err)
	}
	return nil
}

func (s *pgStore) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	res, err := s.db.ExecContext(ctx, s.sql["delete_expired"], cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("eventstore: delete expired: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	return rowsAffected, nil
}

func (s *pgStore) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// EventTx implementation
// ---------------------------------------------------------------------------

func (t *pgEventTx) Append(ctx context.Context, event *StoredEvent) error {
	_, err := t.tx.ExecContext(ctx, t.sql["insert"],
		event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.Source, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("eventstore: tx append: %w", err)
	}
	return nil
}

func (t *pgEventTx) AppendTurn(ctx context.Context, turn *TurnWriteRequest) error {
	var successVal any
	if turn.Success != nil {
		successVal = t.dialect.BoolValue(*turn.Success)
	}

	var id int64
	err := t.tx.QueryRowContext(ctx, t.sql["turns.insert"],
		turn.SessionID, turn.Generation, turn.TurnNum, turn.Seq, turn.Role, turn.Content,
		turn.Platform, turn.UserID, turn.Model, successVal, turn.Source, turn.ToolsJSON, turn.ToolCount,
		turn.TokensInput, turn.TokensCacheWrite, turn.TokensCacheRead, turn.TokensOut,
		turn.DurationMs, turn.CostUSD, turn.CreatedAt,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("eventstore: tx append turn: %w", err)
	}
	return nil
}

func (t *pgEventTx) Commit() error {
	return t.tx.Commit()
}

func (t *pgEventTx) Rollback() error {
	return t.tx.Rollback()
}

// ---------------------------------------------------------------------------
// TurnQuerier implementation
// ---------------------------------------------------------------------------

func (s *pgStore) resolveGeneration(ctx context.Context, sessionID string) (int64, error) {
	gen, err := s.LatestGeneration(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if gen == 0 {
		return 0, ErrNotFound
	}
	return gen, nil
}

func (s *pgStore) QueryTurns(ctx context.Context, sessionID string, limit, offset int) ([]*TurnRecord, error) {
	gen, err := s.resolveGeneration(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("eventstore: resolve generation: %w", err)
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, s.sql["turns.query"], sessionID, gen, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query turns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTurnsPG(rows)
}

func (s *pgStore) QueryTurnsBefore(ctx context.Context, sessionID string, beforeID int64, limit int) ([]*TurnRecord, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, s.sql["turns.query_before"], sessionID, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query turns before: %w", err)
	}
	defer func() { _ = rows.Close() }()
	records, err := scanTurnsPG(rows)
	if err != nil {
		return nil, fmt.Errorf("eventstore: scan turns: %w", err)
	}
	// Reverse to ASC order (SQL returns DESC).
	slices.Reverse(records)
	return records, nil
}

func (s *pgStore) QueryTurnStats(ctx context.Context, sessionID string) (*TurnStats, error) {
	gen, err := s.resolveGeneration(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("eventstore: resolve generation: %w", err)
	}
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, s.sql["turns.stats"], sessionID, gen)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query turn stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := &TurnStats{SessionID: sessionID, Generation: gen}
	for rows.Next() {
		var ts TurnStatItem
		var success sql.NullBool
		var toolsJSON sql.NullString
		var toolCount sql.NullInt64
		if err := rows.Scan(&ts.TurnNum, &ts.Seq, &success, &ts.Source,
			&toolsJSON, &toolCount,
			&ts.TokensInput, &ts.TokensCacheWrite, &ts.TokensCacheRead, &ts.TokensIn,
			&ts.TokensOut, &ts.DurationMs, &ts.CostUSD, &ts.Model, &ts.CreatedAt); err != nil {
			s.log.Warn("eventstore: scan turn stats row", "session_id", sessionID, "error", err)
			continue
		}
		ts.Success = success.Valid && success.Bool
		stats.TotalTurns++
		if ts.Success {
			stats.SuccessTurns++
		} else {
			stats.FailedTurns++
		}
		stats.TotalDurMs += ts.DurationMs
		stats.TotalCostUSD += ts.CostUSD
		stats.TotalTokIn += ts.TokensIn
		stats.TotalTokInput += ts.TokensInput
		stats.TotalTokCacheWrite += ts.TokensCacheWrite
		stats.TotalTokCacheRead += ts.TokensCacheRead
		stats.TotalTokOut += ts.TokensOut
		stats.Turns = append(stats.Turns, ts)
	}
	if stats.TotalTurns == 0 {
		return nil, ErrNotFound
	}
	return stats, rows.Err()
}

func (s *pgStore) LatestGeneration(ctx context.Context, sessionID string) (int64, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	var gen int64
	err := s.db.QueryRowContext(ctx, s.sql["turns.latest_generation"], sessionID).Scan(&gen)
	if err != nil {
		return 0, fmt.Errorf("eventstore: latest generation: %w", err)
	}
	return gen, nil
}

func (s *pgStore) DeleteExpiredTurns(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	res, err := s.db.ExecContext(ctx, s.sql["turns.delete_expired"], cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("eventstore: delete expired turns: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	return rowsAffected, nil
}

// ---------------------------------------------------------------------------
// PG-specific scanning helpers
// ---------------------------------------------------------------------------

// scanTurnsPG scans turn rows from PG, using sql.NullBool for the success
// column (PG BOOLEAN) instead of sql.NullInt64 (SQLite INTEGER).
func scanTurnsPG(rows *sql.Rows) ([]*TurnRecord, error) {
	var records []*TurnRecord
	for rows.Next() {
		var r TurnRecord
		var success sql.NullBool
		var toolsJSON sql.NullString
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Generation, &r.TurnNum, &r.Seq, &r.Role, &r.Content,
			&r.Platform, &r.UserID, &r.Model, &success, &r.Source,
			&toolsJSON, &r.ToolCount,
			&r.TokensInput, &r.TokensCacheWrite, &r.TokensCacheRead, &r.TokensIn,
			&r.TokensOut, &r.DurationMs, &r.CostUSD, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("eventstore: scan turn: %w", err)
		}
		if success.Valid {
			r.Success = &success.Bool
		}
		if toolsJSON.Valid && toolsJSON.String != "" {
			_ = json.Unmarshal([]byte(toolsJSON.String), &r.Tools) //nolint:errcheck // best-effort
		}
		records = append(records, &r)
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	return records, rows.Err()
}
