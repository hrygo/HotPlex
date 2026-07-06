package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

// Sentinel errors for the audit store layer.
var (
	ErrStoreClosed    = errors.New("audit: store closed")
	ErrCheckpointGap  = errors.New("audit: checkpoint gap (rows missing between expected and found)")
	ErrHashTailBroken = errors.New("audit: hash chain tail broken")
)

// AuditAdvisoryLockKey is the PG advisory lock key for chain tail serialization.
// Picked to avoid collision with other hotplex advisory locks (sentinel: "AUD17").
const AuditAdvisoryLockKey int64 = 819207

// Query holds the by-user activity search parameters.
type Query struct {
	UserID     string
	Action     string
	Outcome    string
	From       time.Time
	To         time.Time
	Limit      int
	Offset     int
	IncludePII bool
}

// Store is the abstract audit storage layer.
type Store interface {
	BeginTx(ctx context.Context) (Tx, error)
	Query(ctx context.Context, q Query) ([]UserActivity, error)
	// QueryAsc returns rows in ascending id order starting at FromID
	// (inclusive). Used by the verifier to stream the chain without
	// loading the whole table into memory (spec §5.5 chain verification).
	// At most Limit rows are returned (Limit<=0 → no rows).
	QueryAsc(ctx context.Context, fromID int64, limit int) ([]UserActivity, error)
	DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error)
	SaveCheckpoint(ctx context.Context, c Checkpoint) error
	LatestCheckpoint(ctx context.Context) (*Checkpoint, error)
	Close() error
	Dialect() dbutil.Dialect
}

// Tx is a single audit-write transaction. All hash-chain rows in one batch
// must use the same Tx; the previous row's self_hash feeds the next row's prev_hash.
type Tx interface {
	Append(ctx context.Context, ua *UserActivity) error
	AppendBatch(ctx context.Context, uas []*UserActivity) error
	SaveCheckpoint(ctx context.Context, c Checkpoint) error
	TailHash(ctx context.Context) (string, error)
	Commit() error
	Rollback() error
}

// NewStore returns a Store appropriate for the given dialect.
func NewStore(db *sql.DB, dialect dbutil.Dialect, writeMu *sqlutil.WriteMu, log *slog.Logger) (Store, error) {
	switch dialect {
	case dbutil.DialectSQLite:
		return newSQLiteStore(db, writeMu, log), nil
	case dbutil.DialectPostgres:
		return newPGStore(db, log), nil
	default:
		return nil, fmt.Errorf("audit: unknown dialect %q", dialect)
	}
}

// queryAsc is the shared ascending-order reader backing both dialects'
// QueryAsc. It returns rows with id >= fromID in ascending id order, at
// most `limit` rows (limit<=0 → empty result). The columns scanned must
// match the canonical 16-field user_activity projection used by Query.
func queryAsc(db *sql.DB, d dbutil.Dialect, ctx context.Context, fromID int64, limit int) ([]UserActivity, error) {
	if limit <= 0 {
		return nil, nil
	}
	if limit > 1000 {
		limit = 1000
	}
	sqlStr := d.Rebind(
		"SELECT id, ts, user_id, user_id_type, platform, session_id, action, " +
			"resource_type, resource_id, outcome, detail_json, event_ref, " +
			"ip, user_agent, prev_hash, self_hash FROM user_activity" +
			" WHERE id >= ? ORDER BY id ASC LIMIT ?")
	rows, err := db.QueryContext(ctx, sqlStr, fromID, limit)
	if err != nil {
		return nil, fmt.Errorf("audit: query_asc: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []UserActivity
	for rows.Next() {
		var ua UserActivity
		var sessionID, resourceType, resourceID, eventRef, ip, userAgent sql.NullString
		if err := rows.Scan(
			&ua.ID, &ua.Ts, &ua.UserID, &ua.UserIDType, &ua.Platform,
			&sessionID, &ua.Action, &resourceType, &resourceID,
			&ua.Outcome, &ua.DetailJSON, &eventRef, &ip, &ua.UserAgent,
			&ua.PrevHash, &ua.SelfHash,
		); err != nil {
			return nil, fmt.Errorf("audit: query_asc scan: %w", err)
		}
		ua.SessionID = sessionID.String
		ua.ResourceType = resourceType.String
		ua.ResourceID = resourceID.String
		ua.EventRef = eventRef.String
		ua.IP = ip.String
		ua.UserAgent = userAgent.String
		results = append(results, ua)
	}
	return results, rows.Err()
}

// ---------------------------------------------------------------------------
// SQLite implementation
// ---------------------------------------------------------------------------

type sqliteStore struct {
	db      *sql.DB
	writeMu *sqlutil.WriteMu
	log     *slog.Logger
	d       dbutil.Dialect
}

func newSQLiteStore(db *sql.DB, writeMu *sqlutil.WriteMu, log *slog.Logger) *sqliteStore {
	return &sqliteStore{db: db, writeMu: writeMu, log: log, d: dbutil.DialectSQLite}
}

func (s *sqliteStore) Dialect() dbutil.Dialect { return s.d }

func (s *sqliteStore) BeginTx(ctx context.Context) (Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("audit: begin tx: %w", err)
	}
	return &sqliteTx{store: s, tx: tx}, nil
}

func (s *sqliteStore) Query(ctx context.Context, q Query) ([]UserActivity, error) {
	var conditions []string
	var args []any
	if q.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, q.UserID)
	}
	if q.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, q.Action)
	}
	if q.Outcome != "" {
		conditions = append(conditions, "outcome = ?")
		args = append(args, q.Outcome)
	}
	if !q.From.IsZero() {
		conditions = append(conditions, "ts >= ?")
		args = append(args, q.From.UnixMilli())
	}
	if !q.To.IsZero() {
		conditions = append(conditions, "ts <= ?")
		args = append(args, q.To.UnixMilli())
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	sqlStr := s.d.Rebind(
		"SELECT id, ts, user_id, user_id_type, platform, session_id, action, " +
			"resource_type, resource_id, outcome, detail_json, event_ref, " +
			"ip, user_agent, prev_hash, self_hash FROM user_activity" +
			where + " ORDER BY id DESC LIMIT ? OFFSET ?")
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []UserActivity
	for rows.Next() {
		var ua UserActivity
		var sessionID, resourceType, resourceID, eventRef, ip, userAgent sql.NullString
		if err := rows.Scan(
			&ua.ID, &ua.Ts, &ua.UserID, &ua.UserIDType, &ua.Platform,
			&sessionID, &ua.Action, &resourceType, &resourceID,
			&ua.Outcome, &ua.DetailJSON, &eventRef, &ip, &ua.UserAgent,
			&ua.PrevHash, &ua.SelfHash,
		); err != nil {
			return nil, fmt.Errorf("audit: scan: %w", err)
		}
		ua.SessionID = sessionID.String
		ua.ResourceType = resourceType.String
		ua.ResourceID = resourceID.String
		ua.EventRef = eventRef.String
		ua.IP = ip.String
		ua.UserAgent = userAgent.String
		results = append(results, ua)
	}
	return results, rows.Err()
}

func (s *sqliteStore) QueryAsc(ctx context.Context, fromID int64, limit int) ([]UserActivity, error) {
	return queryAsc(s.db, s.d, ctx, fromID, limit)
}

func (s *sqliteStore) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	err := s.writeMu.WithLock(func() error {
		res, err := s.db.ExecContext(ctx, "DELETE FROM user_activity WHERE ts < ?", cutoff.UnixMilli())
		if err != nil {
			return fmt.Errorf("audit: delete: %w", err)
		}
		d, _ := res.RowsAffected()
		deleted = d
		return nil
	})
	return deleted, err
}

func (s *sqliteStore) SaveCheckpoint(ctx context.Context, c Checkpoint) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx,
			s.d.Rebind("INSERT INTO audit_chain_checkpoints (pruned_at, last_self_hash, next_id) VALUES (?, ?, ?)"),
			c.PrunedAt.UnixMilli(), c.LastSelfHash, c.NextID)
		if err != nil {
			return fmt.Errorf("audit: save checkpoint: %w", err)
		}
		return nil
	})
}

func (s *sqliteStore) LatestCheckpoint(ctx context.Context) (*Checkpoint, error) {
	var c Checkpoint
	var prunedAtMs int64
	err := s.db.QueryRowContext(ctx,
		s.d.Rebind("SELECT id, pruned_at, last_self_hash, next_id FROM audit_chain_checkpoints ORDER BY id DESC LIMIT 1"),
	).Scan(&c.ID, &prunedAtMs, &c.LastSelfHash, &c.NextID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("audit: latest checkpoint: %w", err)
	}
	c.PrunedAt = time.UnixMilli(prunedAtMs)
	return &c, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

// sqliteTx implements Tx for SQLite.
type sqliteTx struct {
	store *sqliteStore
	tx    *sql.Tx
	done  bool
}

func (t *sqliteTx) Append(ctx context.Context, ua *UserActivity) error {
	if t.done {
		return ErrStoreClosed
	}
	_, err := t.tx.ExecContext(ctx, t.store.d.Rebind(
		"INSERT INTO user_activity (ts, user_id, user_id_type, platform, session_id, "+
			"action, resource_type, resource_id, outcome, detail_json, event_ref, "+
			"ip, user_agent, prev_hash, self_hash) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"),
		ua.Ts, ua.UserID, ua.UserIDType, ua.Platform, ua.SessionID,
		ua.Action, ua.ResourceType, ua.ResourceID, ua.Outcome, ua.DetailJSON, ua.EventRef,
		ua.IP, ua.UserAgent, ua.PrevHash, ua.SelfHash,
	)
	if err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	return nil
}

func (t *sqliteTx) AppendBatch(ctx context.Context, uas []*UserActivity) error {
	if t.done {
		return ErrStoreClosed
	}
	for _, ua := range uas {
		if err := t.Append(ctx, ua); err != nil {
			return err
		}
	}
	return nil
}

func (t *sqliteTx) SaveCheckpoint(ctx context.Context, c Checkpoint) error {
	if t.done {
		return ErrStoreClosed
	}
	_, err := t.tx.ExecContext(ctx, t.store.d.Rebind(
		"INSERT INTO audit_chain_checkpoints (pruned_at, last_self_hash, next_id) VALUES (?, ?, ?)"),
		c.PrunedAt.UnixMilli(), c.LastSelfHash, c.NextID)
	if err != nil {
		return fmt.Errorf("audit: tx save checkpoint: %w", err)
	}
	return nil
}

func (t *sqliteTx) TailHash(ctx context.Context) (string, error) {
	if t.done {
		return "", ErrStoreClosed
	}
	var h sql.NullString
	err := t.tx.QueryRowContext(ctx,
		"SELECT self_hash FROM user_activity ORDER BY id DESC LIMIT 1",
	).Scan(&h)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("audit: tail hash: %w", err)
	}
	if !h.Valid {
		return "", nil
	}
	return h.String, nil
}

func (t *sqliteTx) Commit() error {
	if t.done {
		return nil
	}
	t.done = true
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true
	return t.tx.Rollback()
}

// ---------------------------------------------------------------------------
// PostgreSQL implementation
// ---------------------------------------------------------------------------

type pgStore struct {
	db  *sql.DB
	log *slog.Logger
	d   dbutil.Dialect
}

func newPGStore(db *sql.DB, log *slog.Logger) *pgStore {
	return &pgStore{db: db, log: log, d: dbutil.DialectPostgres}
}

func (s *pgStore) Dialect() dbutil.Dialect { return s.d }

func (s *pgStore) BeginTx(ctx context.Context) (Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("audit: pg begin tx: %w", err)
	}
	// Acquire advisory lock to serialize chain tail writes.
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", AuditAdvisoryLockKey); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("audit: pg advisory lock: %w", err)
	}
	return &pgTx{tx: tx, d: s.d}, nil
}

func (s *pgStore) Query(ctx context.Context, q Query) ([]UserActivity, error) {
	// Same logic as SQLite — Rebind handles placeholder conversion.
	var conditions []string
	var args []any
	if q.UserID != "" {
		conditions = append(conditions, "user_id = ?")
		args = append(args, q.UserID)
	}
	if q.Action != "" {
		conditions = append(conditions, "action = ?")
		args = append(args, q.Action)
	}
	if q.Outcome != "" {
		conditions = append(conditions, "outcome = ?")
		args = append(args, q.Outcome)
	}
	if !q.From.IsZero() {
		conditions = append(conditions, "ts >= ?")
		args = append(args, q.From.UnixMilli())
	}
	if !q.To.IsZero() {
		conditions = append(conditions, "ts <= ?")
		args = append(args, q.To.UnixMilli())
	}

	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}

	sqlStr := s.d.Rebind(
		"SELECT id, ts, user_id, user_id_type, platform, session_id, action, " +
			"resource_type, resource_id, outcome, detail_json, event_ref, " +
			"ip, user_agent, prev_hash, self_hash FROM user_activity" +
			where + " ORDER BY id DESC LIMIT ? OFFSET ?")
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: pg query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []UserActivity
	for rows.Next() {
		var ua UserActivity
		var sessionID, resourceType, resourceID, eventRef, ip, userAgent sql.NullString
		if err := rows.Scan(
			&ua.ID, &ua.Ts, &ua.UserID, &ua.UserIDType, &ua.Platform,
			&sessionID, &ua.Action, &resourceType, &resourceID,
			&ua.Outcome, &ua.DetailJSON, &eventRef, &ip, &ua.UserAgent,
			&ua.PrevHash, &ua.SelfHash,
		); err != nil {
			return nil, fmt.Errorf("audit: pg scan: %w", err)
		}
		ua.SessionID = sessionID.String
		ua.ResourceType = resourceType.String
		ua.ResourceID = resourceID.String
		ua.EventRef = eventRef.String
		ua.IP = ip.String
		ua.UserAgent = userAgent.String
		results = append(results, ua)
	}
	return results, rows.Err()
}

func (s *pgStore) QueryAsc(ctx context.Context, fromID int64, limit int) ([]UserActivity, error) {
	return queryAsc(s.db, s.d, ctx, fromID, limit)
}

func (s *pgStore) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM user_activity WHERE ts < $1", cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("audit: pg delete: %w", err)
	}
	d, _ := res.RowsAffected()
	return d, nil
}

func (s *pgStore) SaveCheckpoint(ctx context.Context, c Checkpoint) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO audit_chain_checkpoints (pruned_at, last_self_hash, next_id) VALUES ($1, $2, $3)",
		c.PrunedAt.UnixMilli(), c.LastSelfHash, c.NextID)
	if err != nil {
		return fmt.Errorf("audit: pg save checkpoint: %w", err)
	}
	return nil
}

func (s *pgStore) LatestCheckpoint(ctx context.Context) (*Checkpoint, error) {
	var c Checkpoint
	var prunedAtMs int64
	err := s.db.QueryRowContext(ctx,
		"SELECT id, pruned_at, last_self_hash, next_id FROM audit_chain_checkpoints ORDER BY id DESC LIMIT 1",
	).Scan(&c.ID, &prunedAtMs, &c.LastSelfHash, &c.NextID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("audit: pg latest checkpoint: %w", err)
	}
	c.PrunedAt = time.UnixMilli(prunedAtMs)
	return &c, nil
}

func (s *pgStore) Close() error { return s.db.Close() }

// pgTx implements Tx for PostgreSQL with advisory lock serialization.
type pgTx struct {
	tx   *sql.Tx
	d    dbutil.Dialect
	done bool
}

func (t *pgTx) Append(ctx context.Context, ua *UserActivity) error {
	if t.done {
		return ErrStoreClosed
	}
	_, err := t.tx.ExecContext(ctx, t.d.Rebind(
		"INSERT INTO user_activity (ts, user_id, user_id_type, platform, session_id, "+
			"action, resource_type, resource_id, outcome, detail_json, event_ref, "+
			"ip, user_agent, prev_hash, self_hash) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"),
		ua.Ts, ua.UserID, ua.UserIDType, ua.Platform, ua.SessionID,
		ua.Action, ua.ResourceType, ua.ResourceID, ua.Outcome, ua.DetailJSON, ua.EventRef,
		ua.IP, ua.UserAgent, ua.PrevHash, ua.SelfHash,
	)
	if err != nil {
		return fmt.Errorf("audit: pg append: %w", err)
	}
	return nil
}

func (t *pgTx) TailHash(ctx context.Context) (string, error) {
	if t.done {
		return "", ErrStoreClosed
	}
	var h sql.NullString
	err := t.tx.QueryRowContext(ctx,
		"SELECT self_hash FROM user_activity ORDER BY id DESC LIMIT 1",
	).Scan(&h)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("audit: pg tail hash: %w", err)
	}
	if !h.Valid {
		return "", nil
	}
	return h.String, nil
}

func (t *pgTx) AppendBatch(ctx context.Context, uas []*UserActivity) error {
	if t.done {
		return ErrStoreClosed
	}
	for _, ua := range uas {
		if err := t.Append(ctx, ua); err != nil {
			return err
		}
	}
	return nil
}

func (t *pgTx) SaveCheckpoint(ctx context.Context, c Checkpoint) error {
	if t.done {
		return ErrStoreClosed
	}
	_, err := t.tx.ExecContext(ctx,
		"INSERT INTO audit_chain_checkpoints (pruned_at, last_self_hash, next_id) VALUES ($1, $2, $3)",
		c.PrunedAt.UnixMilli(), c.LastSelfHash, c.NextID)
	if err != nil {
		return fmt.Errorf("audit: pg tx save checkpoint: %w", err)
	}
	return nil
}

func (t *pgTx) Commit() error {
	if t.done {
		return nil
	}
	t.done = true
	return t.tx.Commit()
}

func (t *pgTx) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true
	return t.tx.Rollback()
}
