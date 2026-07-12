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

// NewSQLStore creates a store and marks records left accepted by a previous
// gateway process as unknown. Such records may already have reached a worker,
// so automatic redelivery would be unsafe.
func NewSQLStore(ctx context.Context, db *sql.DB, dialect dbutil.Dialect, writeMu *sqlutil.WriteMu, log *slog.Logger) (*SQLStore, error) {
	if db == nil {
		return nil, errors.New("execution: nil database")
	}
	if log == nil {
		log = slog.Default()
	}
	s := &SQLStore{
		db:      db,
		dialect: dialect,
		writeMu: writeMu,
		log:     log.With("component", "execution_store"),
	}
	recovered, err := s.recoverAccepted(ctx)
	if err != nil {
		return nil, err
	}
	if recovered > 0 {
		s.log.Warn("execution: recovered ambiguous accepted inputs", "count", recovered)
	}
	return s, nil
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

func (s *SQLStore) recoverAccepted(ctx context.Context) (int64, error) {
	ctx, cancel := withTimeout(ctx)
	defer cancel()

	var affected int64
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			UPDATE execution_inputs
			SET status = ?, error_code = ?, updated_at = ?
			WHERE status = ?`),
			StatusUnknown, "GATEWAY_RESTART", time.Now().UnixMilli(), StatusAccepted)
		if err != nil {
			return fmt.Errorf("execution: recover accepted: %w", err)
		}
		affected, _ = result.RowsAffected()
		return nil
	})
	return affected, err
}

// Accept implements Store.Accept.
func (s *SQLStore) Accept(ctx context.Context, request AcceptRequest) (*Record, bool, error) {
	if request.SessionID == "" || request.ClientMessageID == "" || request.PayloadHash == "" {
		return nil, false, errors.New("execution: session id, client message id, and payload hash are required")
	}

	ctx, cancel := withTimeout(ctx)
	defer cancel()

	now := time.Now().UnixMilli()
	record := &Record{
		ExecutionID:     "exec_" + uuid.NewString(),
		SessionID:       request.SessionID,
		ClientMessageID: request.ClientMessageID,
		PayloadHash:     request.PayloadHash,
		Status:          StatusAccepted,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	var inserted bool
	err := s.withWriteLock(func() error {
		result, err := s.db.ExecContext(ctx, s.rebind(`
			INSERT INTO execution_inputs
				(execution_id, session_id, client_message_id, payload_hash, status, error_code, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '', ?, ?)
			ON CONFLICT(session_id, client_message_id) DO NOTHING`),
			record.ExecutionID, record.SessionID, record.ClientMessageID, record.PayloadHash,
			record.Status, record.CreatedAt, record.UpdatedAt)
		if err != nil {
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
	record := new(Record)
	var deliveredAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, s.rebind(`
		SELECT execution_id, session_id, client_message_id, payload_hash, status,
		       error_code, created_at, updated_at, delivered_at
		FROM execution_inputs
		WHERE session_id = ? AND client_message_id = ?`), sessionID, clientMessageID).Scan(
		&record.ExecutionID, &record.SessionID, &record.ClientMessageID, &record.PayloadHash,
		&record.Status, &record.ErrorCode, &record.CreatedAt, &record.UpdatedAt, &deliveredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("execution: get input: %w", err)
	}
	if deliveredAt.Valid {
		record.DeliveredAt = &deliveredAt.Int64
	}
	return record, nil
}

// SetStatus implements Store.SetStatus.
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
			WHERE execution_id = ? AND (status = ? OR status = ?)`),
			status, errorCode, now, deliveredAt, executionID, StatusAccepted, status)
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
		return ErrNotFound
	}
	return nil
}
