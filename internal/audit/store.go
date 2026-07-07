package audit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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
	UserIDs    []string
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
	ListIdentityLinks(ctx context.Context, principalUserID string) ([]IdentityLink, error)
	UpsertIdentityLink(ctx context.Context, link IdentityLink) error
	DeleteIdentityLink(ctx context.Context, id string) error
	Close() error
	Dialect() dbutil.Dialect
}

// Tx is a single audit-write transaction. All hash-chain rows in one batch
// must use the same Tx; the previous row's self_hash feeds the next row's prev_hash.
//
// GC also runs its whole prune inside one Tx (LastRowBefore → DeleteByIDLEQ →
// SaveCheckpoint → Commit). Doing the prune under the Tx guarantees the same
// single-writer serialization that protects the append path: on SQLite the
// process-wide writeMu is held for the entire Tx; on PostgreSQL the
// pg_advisory_xact_lock acquired in BeginTx is held until Commit. This closes
// the C1/C2 race where GC previously ran each step as a separate store call
// and could interleave with a concurrent flushBatch, breaking the hash chain.
type Tx interface {
	Append(ctx context.Context, ua *UserActivity) error
	AppendBatch(ctx context.Context, uas []*UserActivity) error
	SaveCheckpoint(ctx context.Context, c Checkpoint) error
	TailHash(ctx context.Context) (string, error)
	// LastRowBefore returns the highest-id row with ts < cutoff, as
	// (id, self_hash). Returns (0, "", nil) when no row matches — GC treats
	// that as "nothing to prune". Keyed off id (the true monotonic order)
	// rather than ts to avoid equal-ms collisions (review C1).
	LastRowBefore(ctx context.Context, cutoff time.Time) (int64, string, error)
	// DeleteByIDLEQ deletes every row with id <= maxID and returns the count.
	// Used by GC; the id boundary is derived from LastRowBefore's id so the
	// deleted set is exactly the set the checkpoint anchored.
	DeleteByIDLEQ(ctx context.Context, maxID int64) (int64, error)
	// RowCount returns the total row count. GC uses it after the prune to
	// detect the full-prune / empty-table case and rewrite the checkpoint
	// with LastSelfHash="" so the next append is treated as genesis.
	RowCount(ctx context.Context) (int64, error)
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
			&ua.Outcome, &ua.DetailJSON, &eventRef, &ip, &userAgent,
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

// BeginTx starts a write transaction. The process-wide writeMu is held for
// the entire transaction lifetime to serialize against concurrent GC writes
// (SaveCheckpoint/DeleteBefore) and other stores sharing the SQLite file,
// preventing SQLITE_BUSY on the hot append path. writeMu is released in
// Commit/Rollback. PostgreSQL ignores writeMu (no-op), so it relies on
// pg_advisory_xact_lock acquired in the PG BeginTx instead.
//
// Lock ordering: Collector callers already hold c.mu, so the consistent
// order is c.mu → writeMu. GC acquires only writeMu. No path acquires them
// in reverse, so there is no deadlock cycle.
func (s *sqliteStore) BeginTx(ctx context.Context) (Tx, error) {
	s.writeMu.Lock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.writeMu.Unlock()
		return nil, fmt.Errorf("audit: begin tx: %w", err)
	}
	return &sqliteTx{store: s, tx: tx}, nil
}

func (s *sqliteStore) Query(ctx context.Context, q Query) ([]UserActivity, error) {
	var conditions []string
	var args []any
	userIDs := normalizedUserIDs(q)
	if len(userIDs) == 1 {
		conditions = append(conditions, "user_id = ?")
		args = append(args, userIDs[0])
	} else if len(userIDs) > 1 {
		conditions = append(conditions, "user_id IN ("+strings.TrimRight(strings.Repeat("?,", len(userIDs)), ",")+")")
		for _, id := range userIDs {
			args = append(args, id)
		}
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
			&ua.Outcome, &ua.DetailJSON, &eventRef, &ip, &userAgent,
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

func normalizedUserIDs(q Query) []string {
	seen := make(map[string]struct{}, len(q.UserIDs)+1)
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	add(q.UserID)
	for _, v := range q.UserIDs {
		add(v)
	}
	return out
}

func listIdentityLinks(db *sql.DB, d dbutil.Dialect, ctx context.Context, principalUserID string) ([]IdentityLink, error) {
	var conditions []string
	var args []any
	if strings.TrimSpace(principalUserID) != "" {
		conditions = append(conditions, "principal_user_id = ?")
		args = append(args, strings.TrimSpace(principalUserID))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}
	rows, err := db.QueryContext(ctx, d.Rebind(
		"SELECT id, principal_user_id, provider, subject, subject_type, display_name, email, created_at, updated_at "+
			"FROM audit_identity_links"+where+" ORDER BY provider, subject"), args...)
	if err != nil {
		return nil, fmt.Errorf("audit: list identity links: %w", err)
	}
	defer func() { _ = rows.Close() }()
	links := make([]IdentityLink, 0)
	for rows.Next() {
		var l IdentityLink
		if err := rows.Scan(&l.ID, &l.PrincipalUserID, &l.Provider, &l.Subject, &l.SubjectType, &l.DisplayName, &l.Email, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return nil, fmt.Errorf("audit: scan identity link: %w", err)
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

func (s *sqliteStore) ListIdentityLinks(ctx context.Context, principalUserID string) ([]IdentityLink, error) {
	return listIdentityLinks(s.db, s.d, ctx, principalUserID)
}

func (s *sqliteStore) UpsertIdentityLink(ctx context.Context, link IdentityLink) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, s.d.Rebind(`
INSERT INTO audit_identity_links
    (id, principal_user_id, provider, subject, subject_type, display_name, email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, subject) DO UPDATE SET
    principal_user_id = excluded.principal_user_id,
    subject_type = excluded.subject_type,
    display_name = excluded.display_name,
    email = excluded.email,
    updated_at = excluded.updated_at`),
			link.ID, link.PrincipalUserID, link.Provider, link.Subject, link.SubjectType,
			link.DisplayName, link.Email, link.CreatedAt, link.UpdatedAt)
		if err != nil {
			return fmt.Errorf("audit: upsert identity link: %w", err)
		}
		return nil
	})
}

func (s *sqliteStore) DeleteIdentityLink(ctx context.Context, id string) error {
	return s.writeMu.WithLock(func() error {
		_, err := s.db.ExecContext(ctx, s.d.Rebind("DELETE FROM audit_identity_links WHERE id = ?"), id)
		if err != nil {
			return fmt.Errorf("audit: delete identity link: %w", err)
		}
		return nil
	})
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

// sqliteTx implements Tx for SQLite. The store's writeMu is acquired in
// BeginTx and held until Commit or Rollback releases it (exactly once, via
// releaseOnce — safe against double Commit/Rollback and panics).
type sqliteTx struct {
	store       *sqliteStore
	tx          *sql.Tx
	done        bool
	releaseOnce sync.Once
}

// releaseWriteMu releases the writeMu acquired in BeginTx. Idempotent via
// releaseOnce, so it is safe to call from Commit, Rollback, and a deferred
// panic-recovery guard.
func (t *sqliteTx) releaseWriteMu() {
	t.releaseOnce.Do(func() { t.store.writeMu.Unlock() })
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

func (t *sqliteTx) LastRowBefore(ctx context.Context, cutoff time.Time) (int64, string, error) {
	if t.done {
		return 0, "", ErrStoreClosed
	}
	var id int64
	var h sql.NullString
	err := t.tx.QueryRowContext(ctx,
		"SELECT id, self_hash FROM user_activity WHERE ts < ? ORDER BY id DESC LIMIT 1",
		cutoff.UnixMilli(),
	).Scan(&id, &h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("audit: last row before: %w", err)
	}
	return id, h.String, nil
}

func (t *sqliteTx) DeleteByIDLEQ(ctx context.Context, maxID int64) (int64, error) {
	if t.done {
		return 0, ErrStoreClosed
	}
	res, err := t.tx.ExecContext(ctx, "DELETE FROM user_activity WHERE id <= ?", maxID)
	if err != nil {
		return 0, fmt.Errorf("audit: delete by id: %w", err)
	}
	d, _ := res.RowsAffected()
	return d, nil
}

func (t *sqliteTx) RowCount(ctx context.Context) (int64, error) {
	if t.done {
		return 0, ErrStoreClosed
	}
	var n int64
	err := t.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_activity").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("audit: row count: %w", err)
	}
	return n, nil
}

func (t *sqliteTx) Commit() error {
	if t.done {
		return nil
	}
	t.done = true
	defer t.releaseWriteMu()
	return t.tx.Commit()
}

func (t *sqliteTx) Rollback() error {
	if t.done {
		return nil
	}
	t.done = true
	defer t.releaseWriteMu()
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
	userIDs := normalizedUserIDs(q)
	if len(userIDs) == 1 {
		conditions = append(conditions, "user_id = ?")
		args = append(args, userIDs[0])
	} else if len(userIDs) > 1 {
		conditions = append(conditions, "user_id IN ("+strings.TrimRight(strings.Repeat("?,", len(userIDs)), ",")+")")
		for _, id := range userIDs {
			args = append(args, id)
		}
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
			&ua.Outcome, &ua.DetailJSON, &eventRef, &ip, &userAgent,
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

func (s *pgStore) ListIdentityLinks(ctx context.Context, principalUserID string) ([]IdentityLink, error) {
	return listIdentityLinks(s.db, s.d, ctx, principalUserID)
}

func (s *pgStore) UpsertIdentityLink(ctx context.Context, link IdentityLink) error {
	_, err := s.db.ExecContext(ctx, s.d.Rebind(`
INSERT INTO audit_identity_links
    (id, principal_user_id, provider, subject, subject_type, display_name, email, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider, subject) DO UPDATE SET
    principal_user_id = excluded.principal_user_id,
    subject_type = excluded.subject_type,
    display_name = excluded.display_name,
    email = excluded.email,
    updated_at = excluded.updated_at`),
		link.ID, link.PrincipalUserID, link.Provider, link.Subject, link.SubjectType,
		link.DisplayName, link.Email, link.CreatedAt, link.UpdatedAt)
	if err != nil {
		return fmt.Errorf("audit: pg upsert identity link: %w", err)
	}
	return nil
}

func (s *pgStore) DeleteIdentityLink(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, s.d.Rebind("DELETE FROM audit_identity_links WHERE id = ?"), id)
	if err != nil {
		return fmt.Errorf("audit: pg delete identity link: %w", err)
	}
	return nil
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

func (t *pgTx) LastRowBefore(ctx context.Context, cutoff time.Time) (int64, string, error) {
	if t.done {
		return 0, "", ErrStoreClosed
	}
	var id int64
	var h sql.NullString
	err := t.tx.QueryRowContext(ctx,
		"SELECT id, self_hash FROM user_activity WHERE ts < $1 ORDER BY id DESC LIMIT 1",
		cutoff.UnixMilli(),
	).Scan(&id, &h)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", fmt.Errorf("audit: pg last row before: %w", err)
	}
	return id, h.String, nil
}

func (t *pgTx) DeleteByIDLEQ(ctx context.Context, maxID int64) (int64, error) {
	if t.done {
		return 0, ErrStoreClosed
	}
	res, err := t.tx.ExecContext(ctx, "DELETE FROM user_activity WHERE id <= $1", maxID)
	if err != nil {
		return 0, fmt.Errorf("audit: pg delete by id: %w", err)
	}
	d, _ := res.RowsAffected()
	return d, nil
}

func (t *pgTx) RowCount(ctx context.Context) (int64, error) {
	if t.done {
		return 0, ErrStoreClosed
	}
	var n int64
	err := t.tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_activity").Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("audit: pg row count: %w", err)
	}
	return n, nil
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
