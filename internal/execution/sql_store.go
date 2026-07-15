package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/hrygo/hotplex/internal/dbutil"
	"github.com/hrygo/hotplex/internal/sqlutil"
)

const storeTimeout = 5 * time.Second

// SQLStore persists execution ingress records in the gateway database.
type SQLStore struct {
	db      *sql.DB
	dialect dbutil.Dialect
	writeMu *sqlutil.WriteMu
	log     *slog.Logger
}

var _ Store = (*SQLStore)(nil)

func NewSQLStore(_ context.Context, db *sql.DB, dialect dbutil.Dialect, writeMu *sqlutil.WriteMu, log *slog.Logger) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("execution: nil database")
	}
	if log == nil {
		log = slog.Default()
	}
	return &SQLStore{
		db:      db,
		dialect: dialect,
		writeMu: writeMu,
		log:     log.With("component", "execution_store"),
	}, nil
}

func withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, storeTimeout)
}

func (s *SQLStore) rebind(query string) string {
	return s.dialect.Rebind(query)
}

func (s *SQLStore) withWriteLock(fn func() error) error {
	if s.writeMu == nil {
		return fn()
	}
	return s.writeMu.WithLock(fn)
}

const executionColumns = `execution_id, session_id, client_message_id, payload_hash, status,
	error_code, created_at, updated_at, delivered_at,
	owner_instance_id, worker_run_id, lease_until,
	runtime_status, runtime_error_code, started_at, finished_at, fence_reason`

func (s *SQLStore) scanRecord(sc interface {
	Scan(dest ...any) error
}) (*Record, error) {
	r := new(Record)
	var deliveredAt, startedAt, finishedAt sql.NullInt64
	err := sc.Scan(
		&r.ExecutionID, &r.SessionID, &r.ClientMessageID, &r.PayloadHash, &r.Status,
		&r.ErrorCode, &r.CreatedAt, &r.UpdatedAt, &deliveredAt,
		&r.OwnerInstanceID, &r.WorkerRunID, &r.LeaseUntil,
		&r.RuntimeStatus, &r.RuntimeErrorCode, &startedAt, &finishedAt, &r.FenceReason,
	)
	if err != nil {
		return nil, err
	}
	if deliveredAt.Valid {
		r.DeliveredAt = &deliveredAt.Int64
	}
	if startedAt.Valid {
		r.StartedAt = &startedAt.Int64
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.Int64
	}
	return r, nil
}

func (s *SQLStore) Accept(ctx context.Context, request AcceptRequest) (*Record, bool, error) {
	if request.SessionID == "" || request.ClientMessageID == "" || request.PayloadHash == "" {
		return nil, false, errors.New("execution: session id, client message id, and payload hash are required")
	}
	if request.OwnerInstanceID == "" {
		return nil, false, errors.New("execution: owner instance id is required")
	}
	if request.WorkerRunID == "" {
		return nil, false, errors.New("execution: worker run id is required")
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	leaseUntil := now + int64(LeaseTTL)*1000
	record := &Record{
		ExecutionID:     "exec_" + uuid.NewString(),
		SessionID:       request.SessionID,
		ClientMessageID: request.ClientMessageID,
		PayloadHash:     request.PayloadHash,
		Status:          StatusAccepted,
		CreatedAt:       now,
		UpdatedAt:       now,
		OwnerInstanceID: request.OwnerInstanceID,
		WorkerRunID:     request.WorkerRunID,
		LeaseUntil:      leaseUntil,
		RuntimeStatus:   RuntimePending,
	}

	var inserted bool
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO execution_inputs
				(execution_id, session_id, client_message_id, payload_hash, status, error_code,
				 created_at, updated_at, owner_instance_id, worker_run_id, lease_until,
				 runtime_status, runtime_error_code, fence_reason)
			VALUES (?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?, '', '')
			ON CONFLICT(session_id, client_message_id) DO NOTHING`),
			record.ExecutionID, record.SessionID, record.ClientMessageID, record.PayloadHash,
			record.Status, record.CreatedAt, record.UpdatedAt,
			record.OwnerInstanceID, record.WorkerRunID, record.LeaseUntil, record.RuntimeStatus)
		if err != nil {
			if s.dialect.IsUniqueViolation(err) {
				return ErrSessionBusy
			}
			return fmt.Errorf("execution: accept input: %w", err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("execution: accept rows affected: %w", err)
		}
		inserted = rows == 1
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if inserted {
		return record, false, nil
	}

	existing, err := s.getByClientMessage(ctx, request.SessionID, request.ClientMessageID)
	if err != nil {
		return nil, false, err
	}
	if existing.PayloadHash != request.PayloadHash {
		return nil, true, ErrPayloadConflict
	}
	return existing, true, nil
}

func (s *SQLStore) getByClientMessage(ctx context.Context, sessionID, clientMessageID string) (*Record, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT `+executionColumns+`
		FROM execution_inputs
		WHERE session_id = ? AND client_message_id = ?`), sessionID, clientMessageID)
	r, err := s.scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("execution: get input: %w", err)
	}
	return r, nil
}

func (s *SQLStore) SetStatus(ctx context.Context, executionID string, status Status, errorCode string) error {
	if executionID == "" {
		return errors.New("execution: execution id is required")
	}
	if status != StatusDelivered && status != StatusUnknown && status != StatusFailed {
		return fmt.Errorf("execution: invalid terminal status %q", status)
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	var deliveredAt any
	if status == StatusDelivered {
		deliveredAt = now
	}

	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET status = ?, error_code = ?, updated_at = ?, delivered_at = ?
			WHERE execution_id = ? AND status = ?`),
			status, errorCode, now, deliveredAt, executionID, StatusAccepted)
		if err != nil {
			return fmt.Errorf("execution: set status: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		var current Status
		readCtx, readCancel := context.WithTimeout(context.WithoutCancel(ctx), storeTimeout)
		err := s.db.QueryRowContext(readCtx, s.rebind(`
			SELECT status FROM execution_inputs WHERE execution_id = ?`), executionID).Scan(&current)
		readCancel()
		if err == nil && current == status {
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("execution: get current status: %w", err)
		}
		return ErrNotFound
	}
	return nil
}

func (s *SQLStore) SetDelivery(ctx context.Context, executionID, ownerID string, status Status, errorCode string) error {
	if executionID == "" || ownerID == "" {
		return errors.New("execution: execution id and owner id are required")
	}
	if status != StatusDelivered && status != StatusUnknown && status != StatusFailed {
		return fmt.Errorf("execution: invalid delivery status %q", status)
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	var deliveredAt any
	if status == StatusDelivered {
		deliveredAt = now
	}

	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET status = ?, error_code = ?, updated_at = ?, delivered_at = ?
			WHERE execution_id = ? AND owner_instance_id = ? AND status = ?`),
			status, errorCode, now, deliveredAt, executionID, ownerID, StatusAccepted)
		if err != nil {
			return fmt.Errorf("execution: set delivery: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		r, err := s.getByID(ctx, executionID)
		if err != nil {
			return err
		}
		if r.Status == status {
			return nil
		}
		if r.OwnerInstanceID != ownerID {
			return ErrOwnerMismatch
		}
		return nil
	}
	return nil
}

func (s *SQLStore) MarkRunning(ctx context.Context, executionID, ownerID, workerRunID string) error {
	if executionID == "" || ownerID == "" || workerRunID == "" {
		return errors.New("execution: execution id, owner id, and worker run id are required")
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET runtime_status = ?, worker_run_id = ?, started_at = ?, updated_at = ?
			WHERE execution_id = ? AND owner_instance_id = ? AND runtime_status = ?`),
			RuntimeRunning, workerRunID, now, now, executionID, ownerID, RuntimePending)
		if err != nil {
			return fmt.Errorf("execution: mark running: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		r, err := s.getByID(ctx, executionID)
		if err != nil {
			return err
		}
		if r.OwnerInstanceID != ownerID {
			return ErrOwnerMismatch
		}
		if r.RuntimeStatus == RuntimeRunning && r.WorkerRunID == workerRunID {
			return nil
		}
		return fmt.Errorf("execution: mark running: runtime_status is %q, expected pending", r.RuntimeStatus)
	}
	return nil
}

func (s *SQLStore) FinishRuntime(ctx context.Context, executionID, workerRunID string, status RuntimeStatus, errorCode string) error {
	if executionID == "" || workerRunID == "" {
		return errors.New("execution: execution id and worker run id are required")
	}
	if status != RuntimeCompleted && status != RuntimeFailed && status != RuntimeUnknown {
		return fmt.Errorf("execution: invalid terminal runtime status %q", status)
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	fenceReason := ""
	if status == RuntimeUnknown {
		fenceReason = "RUNTIME_AMBIGUOUS"
	}

	var currentFilter string
	if status == RuntimeCompleted || status == RuntimeFailed {
		currentFilter = "('pending', 'running', 'unknown')"
	} else {
		currentFilter = "('pending', 'running')"
	}

	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET runtime_status = ?, runtime_error_code = ?, finished_at = ?,
			    fence_reason = ?, updated_at = ?
			WHERE execution_id = ? AND worker_run_id = ?
			  AND runtime_status IN `+currentFilter),
			status, errorCode, now, fenceReason, now,
			executionID, workerRunID)
		if err != nil {
			return fmt.Errorf("execution: finish runtime: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		r, err := s.getByID(ctx, executionID)
		if err != nil {
			return err
		}
		if r.RuntimeStatus == status && r.WorkerRunID == workerRunID {
			return nil
		}
		if r.WorkerRunID != workerRunID {
			return ErrRunMismatch
		}
		return fmt.Errorf("execution: finish runtime: current %q cannot transition to %q", r.RuntimeStatus, status)
	}
	return nil
}

func (s *SQLStore) ActiveBySession(ctx context.Context, sessionID string) (*Record, error) {
	if sessionID == "" {
		return nil, errors.New("execution: session id is required")
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT `+executionColumns+`
		FROM execution_inputs
		WHERE session_id = ? AND runtime_status IN ('pending', 'running')
		ORDER BY created_at DESC LIMIT 1`), sessionID)
	r, err := s.scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("execution: active by session: %w", err)
	}
	return r, nil
}

func (s *SQLStore) FenceBySession(ctx context.Context, sessionID string) (*Record, error) {
	if sessionID == "" {
		return nil, errors.New("execution: session id is required")
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT `+executionColumns+`
		FROM execution_inputs
		WHERE session_id = ? AND fence_reason <> ''
		ORDER BY created_at DESC LIMIT 1`), sessionID)
	r, err := s.scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("execution: fence by session: %w", err)
	}
	return r, nil
}

func (s *SQLStore) ClearFenceAfterFreshStart(ctx context.Context, executionID, reason, freshWorkerRunID string) error {
	if executionID == "" || reason == "" || freshWorkerRunID == "" {
		return errors.New("execution: execution id, reason, and fresh worker run id are required")
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET fence_reason = '', worker_run_id = ?, updated_at = ?
			WHERE execution_id = ? AND fence_reason = ?`),
			freshWorkerRunID, now, executionID, reason)
		if err != nil {
			return fmt.Errorf("execution: clear fence: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	if err != nil {
		return err
	}
	if rows == 0 {
		r, err := s.getByID(ctx, executionID)
		if err != nil {
			return err
		}
		if r.FenceReason == "" {
			return nil
		}
		return fmt.Errorf("execution: clear fence: fence_reason is %q, expected %q", r.FenceReason, reason)
	}
	return nil
}

func (s *SQLStore) RenewLeases(ctx context.Context, ownerID string, ttlSeconds int64) (int64, error) {
	if ownerID == "" {
		return 0, errors.New("execution: owner id is required")
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	newLease := now + ttlSeconds*1000
	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET lease_until = ?, updated_at = ?
			WHERE owner_instance_id = ? AND runtime_status IN ('pending', 'running')`),
			newLease, now, ownerID)
		if err != nil {
			return fmt.Errorf("execution: renew leases: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	return rows, err
}

func (s *SQLStore) RecoverExpiredLeases(ctx context.Context, nowUnixMilli int64) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET status = CASE WHEN status = 'accepted' THEN 'unknown' ELSE status END,
			    runtime_status = 'unknown',
			    error_code = CASE
			        WHEN status = 'accepted' THEN 'GATEWAY_LEASE_EXPIRED'
			        ELSE error_code
			    END,
			    runtime_error_code = 'GATEWAY_LEASE_EXPIRED',
			    fence_reason = 'GATEWAY_LEASE_EXPIRED',
			    finished_at = ?,
			    updated_at = ?
			WHERE runtime_status IN ('pending', 'running')
			  AND lease_until <= ?`),
			nowUnixMilli, nowUnixMilli, nowUnixMilli)
		if err != nil {
			return fmt.Errorf("execution: recover expired leases: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	return rows, err
}

func (s *SQLStore) TerminateOwnerLeases(ctx context.Context, ownerID, reason string) (int64, error) {
	if ownerID == "" {
		return 0, errors.New("execution: owner id is required")
	}
	if reason == "" {
		reason = "GATEWAY_SHUTDOWN"
	}
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	var rows int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET runtime_status = 'unknown',
			    runtime_error_code = ?,
			    fence_reason = ?,
			    finished_at = ?,
			    updated_at = ?
			WHERE owner_instance_id = ?
			  AND runtime_status IN ('pending', 'running')`),
			reason, reason, now, now, ownerID)
		if err != nil {
			return fmt.Errorf("execution: terminate owner leases: %w", err)
		}
		rows, _ = result.RowsAffected()
		return nil
	})
	return rows, err
}

func (s *SQLStore) getByID(ctx context.Context, executionID string) (*Record, error) {
	row := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT `+executionColumns+`
		FROM execution_inputs
		WHERE execution_id = ?`), executionID)
	r, err := s.scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("execution: get by id: %w", err)
	}
	return r, nil
}
