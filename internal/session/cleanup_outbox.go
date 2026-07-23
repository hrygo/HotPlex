package session

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/worker"
)

// CleanupTask is a durable request to delete a worker-owned remote session.
// WorkerSessionID is captured at deletion time and is never read from a later
// HotPlex session row.
type CleanupTask struct {
	ID              string
	SessionID       string
	WorkerType      worker.WorkerType
	WorkerSessionID string
	Attempts        int
	NextAttemptAt   time.Time
	LeaseUntil      *time.Time
	LeaseToken      string
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CleanupTaskStore is implemented by persistent session stores. Keeping this
// separate from Store preserves lightweight manager test doubles.
type CleanupTaskStore interface {
	MarkDeletedWithCleanup(ctx context.Context, info *SessionInfo) error
	DeletePhysicalWithCleanup(ctx context.Context, id string) (*SessionInfo, error)
	ClaimCleanupTasks(ctx context.Context, now, leaseUntil time.Time, limit int) ([]CleanupTask, error)
	CompleteCleanupTask(ctx context.Context, taskID, leaseToken string) error
	RetryCleanupTask(ctx context.Context, taskID, leaseToken string, nextAttemptAt time.Time, lastError string) error
	HasPendingCleanup(ctx context.Context, sessionID string) (bool, error)
}

// CleanupExecutor dispatches a task to the worker-type-specific cleaner.
type CleanupExecutor func(context.Context, worker.WorkerType, string) error

const (
	cleanupBatchSize      = 16
	cleanupLeaseDuration  = 30 * time.Second
	cleanupAttemptTimeout = 10 * time.Second
	cleanupPollInterval   = time.Second
	cleanupRetryMax       = 5 * time.Minute
)

// CleanupRunner leases and executes persistent cleanup tasks until ctx ends.
type CleanupRunner struct {
	log     *slog.Logger
	store   CleanupTaskStore
	execute CleanupExecutor
	now     func() time.Time
}

func NewCleanupRunner(log *slog.Logger, store CleanupTaskStore, execute CleanupExecutor) *CleanupRunner {
	if log == nil {
		log = slog.Default()
	}
	return &CleanupRunner{log: log.With("component", "session_cleanup_outbox"), store: store, execute: execute, now: time.Now}
}

// Run drains due work at startup and then polls. A failed task never prevents
// later tasks from running.
func (r *CleanupRunner) Run(ctx context.Context) {
	if r == nil || r.store == nil || r.execute == nil {
		return
	}
	r.RunOnce(ctx)
	ticker := time.NewTicker(cleanupPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.RunOnce(ctx)
		}
	}
}

// RunOnce executes one leased batch. It is exported for deterministic tests.
func (r *CleanupRunner) RunOnce(ctx context.Context) {
	if r == nil || r.store == nil || r.execute == nil {
		return
	}
	now := r.now()
	tasks, err := r.store.ClaimCleanupTasks(ctx, now, now.Add(cleanupLeaseDuration), cleanupBatchSize)
	if err != nil {
		r.log.Warn("session cleanup: claim tasks failed", "err", err)
		return
	}
	for _, task := range tasks {
		attemptCtx, cancel := context.WithTimeout(ctx, cleanupAttemptTimeout)
		err := r.execute(attemptCtx, task.WorkerType, task.WorkerSessionID)
		cancel()
		if err == nil {
			if completeErr := r.store.CompleteCleanupTask(ctx, task.ID, task.LeaseToken); completeErr != nil {
				r.log.Warn("session cleanup: complete task failed", "task_id", task.ID, "session_id", task.SessionID, "err", completeErr)
			}
			continue
		}
		next := r.now().Add(cleanupBackoff(task.Attempts))
		if retryErr := r.store.RetryCleanupTask(ctx, task.ID, task.LeaseToken, next, err.Error()); retryErr != nil {
			r.log.Warn("session cleanup: retry scheduling failed", "task_id", task.ID, "session_id", task.SessionID, "err", retryErr)
			continue
		}
		r.log.Warn("session cleanup: remote delete failed; retry scheduled", "task_id", task.ID, "session_id", task.SessionID, "worker_type", task.WorkerType, "attempt", task.Attempts, "next_attempt_at", next, "err", err)
	}
}

func cleanupBackoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	shift := min(attempts-1, 8)
	delay := time.Second * time.Duration(math.Pow(2, float64(shift)))
	if delay > cleanupRetryMax {
		return cleanupRetryMax
	}
	return delay
}

func newCleanupTask(info *SessionInfo, now time.Time) *CleanupTask {
	if info == nil || info.WorkerSessionID == "" {
		return nil
	}
	return &CleanupTask{ID: uuid.NewString(), SessionID: info.ID, WorkerType: info.WorkerType, WorkerSessionID: info.WorkerSessionID, NextAttemptAt: now, CreatedAt: now, UpdatedAt: now}
}

// upsertSessionArgs passes time.Time values to the driver unformatted. The
// modernc/sqlite driver already serializes them to RFC3339Nano (verified in the
// running DB, e.g. "2026-07-14T22:42:20.215036468+08:00") and binds both the
// stored column and the get_expired_* comparison params in that same format,
// keeping the lexicographic TEXT comparison correct. Manually UTC-formatting
// only the write side (a prior attempt at issue #879 #4) broke that invariant:
// GC expiry queries silently returned no rows. #879 #4's premise (driver uses
// time.Time.String()) does not hold on modernc v1.51.0 — the format is already
// canonical, so no write-side reformatting is applied here.
func upsertSessionArgs(info *SessionInfo, ctxJSON, pkJSON []byte) []any {
	return []any{info.ID, info.UserID, info.OwnerID, info.BotID, info.BotName, info.WorkerSessionID, info.WorkerType, string(info.State),
		info.Platform, string(pkJSON), info.WorkDir, info.Title, info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
		string(ctxJSON), info.Source, info.ClientKey, nullableString(info.WorkspaceID), info.ID}
}

func cleanupTaskColumns() string {
	return "id, session_id, worker_type, worker_session_id, attempts, next_attempt_at, lease_until, COALESCE(lease_token, ''), last_error, created_at, updated_at"
}

func scanCleanupTask(sc interface{ Scan(...any) error }) (CleanupTask, error) {
	var task CleanupTask
	var lease sql.NullTime
	err := sc.Scan(&task.ID, &task.SessionID, &task.WorkerType, &task.WorkerSessionID, &task.Attempts, &task.NextAttemptAt, &lease, &task.LeaseToken, &task.LastError, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return CleanupTask{}, err
	}
	if lease.Valid {
		task.LeaseUntil = &lease.Time
	}
	return task, nil
}

func insertCleanupTask(ctx context.Context, execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, info *SessionInfo, now time.Time) error {
	task := newCleanupTask(info, now)
	if task == nil {
		return nil
	}
	_, err := execer.ExecContext(ctx, `INSERT INTO session_cleanup_tasks (id, session_id, worker_type, worker_session_id, attempts, next_attempt_at, lease_until, lease_token, last_error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?) ON CONFLICT(session_id) DO NOTHING`, task.ID, task.SessionID, task.WorkerType, task.WorkerSessionID, task.Attempts, task.NextAttemptAt, task.LastError, task.CreatedAt, task.UpdatedAt)
	if err != nil {
		return fmt.Errorf("session cleanup: enqueue: %w", err)
	}
	return nil
}

func isCleanupPendingError(err error) bool {
	return errors.Is(err, ErrSessionCleanupPending) || (err != nil && containsCleanupPending(err.Error()))
}

func containsCleanupPending(message string) bool {
	return strings.Contains(message, "session cleanup pending") || strings.Contains(message, "session_cleanup_tasks")
}

func ensureSQLiteLifecycleLock(ctx context.Context, tx *sql.Tx, sessionID string) error {
	// SQLiteStore already holds its process-wide writeMu for every caller of
	// this helper. The transaction therefore has the required lifecycle lock
	// without retaining a row after session retention GC.
	return nil
}

func ensurePGLifecycleLock(ctx context.Context, tx *sql.Tx, rebind func(string) string, sessionID string) error {
	// Transaction-scoped advisory locks disappear on commit/rollback. A hash
	// collision only adds contention; it cannot permit conflicting lifecycle
	// writes to run concurrently.
	_, err := tx.ExecContext(ctx, rebind(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`), sessionID)
	return err
}

func (s *SQLiteStore) HasPendingCleanup(ctx context.Context, sessionID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM session_cleanup_tasks WHERE session_id = ?)`, sessionID).Scan(&exists)
	return exists, err
}

func (s *SQLiteStore) MarkDeletedWithCleanup(ctx context.Context, info *SessionInfo) error {
	ctxJSON, pkJSON, err := marshalSessionJSON(info)
	if err != nil {
		return err
	}
	return s.writeMu.WithLock(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err := ensureSQLiteLifecycleLock(ctx, tx, info.ID); err != nil {
			return err
		}
		ctxJSON, err = preservePersistedSpecSnapshot(ctx, tx, queries["store.get_context_json"], info.ID, ctxJSON)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, queries["sessions.upsert_session"], upsertSessionArgs(info, ctxJSON, pkJSON)...); err != nil {
			if isCleanupPendingError(err) {
				return ErrSessionCleanupPending
			}
			return fmt.Errorf("session cleanup: mark deleted: %w", err)
		}
		if err := insertCleanupTask(ctx, tx, info, time.Now()); err != nil {
			return err
		}
		return tx.Commit()
	})
}

func (s *SQLiteStore) DeletePhysicalWithCleanup(ctx context.Context, id string) (*SessionInfo, error) {
	var deleted *SessionInfo
	err := s.writeMu.WithLock(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err := ensureSQLiteLifecycleLock(ctx, tx, id); err != nil {
			return err
		}
		info, err := scanSession(tx.QueryRowContext(ctx, queries["store.get_session"], id))
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, queries["store.delete_physical"], id); err != nil {
			return fmt.Errorf("session cleanup: physical delete: %w", err)
		}
		if err := insertCleanupTask(ctx, tx, info, time.Now()); err != nil {
			return err
		}
		deleted = info
		return tx.Commit()
	})
	return deleted, err
}

func (s *SQLiteStore) ClaimCleanupTasks(ctx context.Context, now, leaseUntil time.Time, limit int) ([]CleanupTask, error) {
	if limit <= 0 {
		return nil, nil
	}
	tasks := make([]CleanupTask, 0, limit)
	err := s.writeMu.WithLock(func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := tx.QueryContext(ctx, `SELECT `+cleanupTaskColumns()+` FROM session_cleanup_tasks WHERE next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?) ORDER BY next_attempt_at, created_at LIMIT ?`, now, now, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			task, err := scanCleanupTask(rows)
			if err != nil {
				_ = rows.Close()
				return err
			}
			leaseToken := uuid.NewString()
			result, err := tx.ExecContext(ctx, `UPDATE session_cleanup_tasks SET attempts = attempts + 1, lease_until = ?, lease_token = ?, updated_at = ? WHERE id = ? AND (lease_until IS NULL OR lease_until <= ?)`, leaseUntil, leaseToken, now, task.ID, now)
			if err != nil {
				_ = rows.Close()
				return err
			}
			updated, err := result.RowsAffected()
			if err != nil {
				_ = rows.Close()
				return err
			}
			if updated == 1 {
				task.Attempts++
				task.LeaseUntil = &leaseUntil
				task.LeaseToken = leaseToken
				task.UpdatedAt = now
				tasks = append(tasks, task)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
		return tx.Commit()
	})
	return tasks, err
}

func (s *SQLiteStore) CompleteCleanupTask(ctx context.Context, taskID, leaseToken string) error {
	return s.writeMu.WithLock(func() error {
		result, err := s.db.ExecContext(ctx, `DELETE FROM session_cleanup_tasks WHERE id = ? AND lease_token = ?`, taskID, leaseToken)
		if err != nil {
			return err
		}
		return cleanupLeaseResult(result)
	})
}

func (s *SQLiteStore) RetryCleanupTask(ctx context.Context, taskID, leaseToken string, nextAttemptAt time.Time, lastError string) error {
	return s.writeMu.WithLock(func() error {
		result, err := s.db.ExecContext(ctx, `UPDATE session_cleanup_tasks SET next_attempt_at = ?, lease_until = NULL, lease_token = NULL, last_error = ?, updated_at = ? WHERE id = ? AND lease_token = ?`, nextAttemptAt, truncateCleanupError(lastError), time.Now(), taskID, leaseToken)
		if err != nil {
			return err
		}
		return cleanupLeaseResult(result)
	})
}

func (s *pgStore) HasPendingCleanup(ctx context.Context, sessionID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, s.dialect.Rebind(`SELECT EXISTS(SELECT 1 FROM session_cleanup_tasks WHERE session_id = ?)`), sessionID).Scan(&exists)
	return exists, err
}

func (s *pgStore) MarkDeletedWithCleanup(ctx context.Context, info *SessionInfo) error {
	ctxJSON, pkJSON, err := marshalSessionJSON(info)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensurePGLifecycleLock(ctx, tx, s.dialect.Rebind, info.ID); err != nil {
		return err
	}
	ctxJSON, err = preservePersistedSpecSnapshot(ctx, tx, s.queries["store.get_context_json"], info.ID, ctxJSON)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, s.queries["sessions.upsert_session"], upsertSessionArgs(info, ctxJSON, pkJSON)...); err != nil {
		if isCleanupPendingError(err) {
			return ErrSessionCleanupPending
		}
		return fmt.Errorf("session cleanup: mark deleted: %w", err)
	}
	if err := insertCleanupTask(ctx, pgExec{tx: tx, rebind: s.dialect.Rebind}, info, time.Now()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *pgStore) DeletePhysicalWithCleanup(ctx context.Context, id string) (*SessionInfo, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := ensurePGLifecycleLock(ctx, tx, s.dialect.Rebind, id); err != nil {
		return nil, err
	}
	info, err := scanSession(tx.QueryRowContext(ctx, s.queries["store.get_session"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, s.queries["store.delete_physical"], id); err != nil {
		return nil, err
	}
	if err := insertCleanupTask(ctx, pgExec{tx: tx, rebind: s.dialect.Rebind}, info, time.Now()); err != nil {
		return nil, err
	}
	return info, tx.Commit()
}

func (s *pgStore) ClaimCleanupTasks(ctx context.Context, now, leaseUntil time.Time, limit int) ([]CleanupTask, error) {
	if limit <= 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	query := s.dialect.Rebind(`SELECT ` + cleanupTaskColumns() + ` FROM session_cleanup_tasks WHERE next_attempt_at <= ? AND (lease_until IS NULL OR lease_until <= ?) ORDER BY next_attempt_at, created_at LIMIT ? FOR UPDATE SKIP LOCKED`)
	rows, err := tx.QueryContext(ctx, query, now, now, limit)
	if err != nil {
		return nil, err
	}
	tasks := make([]CleanupTask, 0, limit)
	update := s.dialect.Rebind(`UPDATE session_cleanup_tasks SET attempts = attempts + 1, lease_until = ?, lease_token = ?, updated_at = ? WHERE id = ?`)
	for rows.Next() {
		task, err := scanCleanupTask(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		leaseToken := uuid.NewString()
		if _, err := tx.ExecContext(ctx, update, leaseUntil, leaseToken, now, task.ID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		task.Attempts++
		task.LeaseUntil = &leaseUntil
		task.LeaseToken = leaseToken
		task.UpdatedAt = now
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return tasks, tx.Commit()
}

func (s *pgStore) CompleteCleanupTask(ctx context.Context, taskID, leaseToken string) error {
	result, err := s.db.ExecContext(ctx, s.dialect.Rebind(`DELETE FROM session_cleanup_tasks WHERE id = ? AND lease_token = ?`), taskID, leaseToken)
	if err != nil {
		return err
	}
	return cleanupLeaseResult(result)
}

func (s *pgStore) RetryCleanupTask(ctx context.Context, taskID, leaseToken string, nextAttemptAt time.Time, lastError string) error {
	result, err := s.db.ExecContext(ctx, s.dialect.Rebind(`UPDATE session_cleanup_tasks SET next_attempt_at = ?, lease_until = NULL, lease_token = NULL, last_error = ?, updated_at = ? WHERE id = ? AND lease_token = ?`), nextAttemptAt, truncateCleanupError(lastError), time.Now(), taskID, leaseToken)
	if err != nil {
		return err
	}
	return cleanupLeaseResult(result)
}

type pgExec struct {
	tx     *sql.Tx
	rebind func(string) string
}

func (e pgExec) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return e.tx.ExecContext(ctx, e.rebind(query), args...)
}

func truncateCleanupError(message string) string {
	const maxLen = 1024
	if len(message) <= maxLen {
		return message
	}
	return message[:maxLen]
}

func cleanupLeaseResult(result sql.Result) error {
	updated, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if updated == 0 {
		return ErrCleanupLeaseLost
	}
	return nil
}
