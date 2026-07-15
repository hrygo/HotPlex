// Package execution persists the ingress state of user inputs before they are
// delivered to a worker. It intentionally stores only a payload hash, never the
// user-provided content itself.
package execution

import (
	"context"
	"errors"
)

// Status is the durable delivery state of an input execution.
type Status string

const (
	StatusAccepted  Status = "accepted"
	StatusDelivered Status = "delivered"
	StatusUnknown   Status = "unknown"
	StatusFailed    Status = "failed"
)

// RuntimeStatus is the execution fact of a Worker turn, independent of delivery
// status. It allows knowledge refinement: unknown → completed/failed is allowed
// when a late Done converges, but terminals never regress.
type RuntimeStatus string

const (
	RuntimePending   RuntimeStatus = "pending"
	RuntimeRunning   RuntimeStatus = "running"
	RuntimeCompleted RuntimeStatus = "completed"
	RuntimeFailed    RuntimeStatus = "failed"
	RuntimeUnknown   RuntimeStatus = "unknown"
)

// LeaseTTL is the default lease time-to-live for an active execution.
const LeaseTTL = 60 // seconds

var (
	// ErrNotFound means the execution does not exist.
	ErrNotFound = errors.New("execution: record not found")
	// ErrPayloadConflict means a client message ID was reused with different data.
	ErrPayloadConflict = errors.New("execution: client message id reused with different payload")
	// ErrSessionBusy means the session already has an active (pending/running or
	// fenced) execution and cannot accept a new one until the current one
	// terminates or the fence is cleared.
	ErrSessionBusy = errors.New("execution: session has an active execution")
	// ErrExecutionFenced means the session has a fence_reason set and the next
	// input requires a fresh Worker session before it can be accepted.
	ErrExecutionFenced = errors.New("execution: session is fenced, fresh worker required")
	// ErrOwnerMismatch means a conditional update did not match the expected owner.
	ErrOwnerMismatch = errors.New("execution: owner mismatch")
	// ErrLeaseExpired means the execution's lease has expired.
	ErrLeaseExpired = errors.New("execution: lease expired")
	// ErrRunMismatch means a conditional update did not match the expected worker_run_id.
	ErrRunMismatch = errors.New("execution: worker run mismatch")
)

// Record is the secret-free durable representation of an input delivery.
type Record struct {
	ExecutionID     string
	SessionID       string
	ClientMessageID string
	PayloadHash     string
	Status          Status
	ErrorCode       string
	CreatedAt       int64
	UpdatedAt       int64
	DeliveredAt     *int64
	// --- Durable ingress reliability closure fields (migration 027) ---
	OwnerInstanceID  string
	WorkerRunID      string
	LeaseUntil       int64
	RuntimeStatus    RuntimeStatus
	RuntimeErrorCode string
	StartedAt        *int64
	FinishedAt       *int64
	FenceReason      string
}

// AcceptRequest describes an input that must be durably accepted before dispatch.
type AcceptRequest struct {
	SessionID       string
	ClientMessageID string
	PayloadHash     string
	// OwnerInstanceID is the Gateway process that owns this execution's lease.
	OwnerInstanceID string
	// WorkerRunID is the Bridge worker run that will dispatch this input.
	WorkerRunID string
}

// Store is the persistence contract for execution ingress records.
type Store interface {
	// Accept creates a durable accepted record with owner, lease, and runtime
	// status set to pending. When the session/message key already exists it
	// returns that record with duplicate=true. Reusing the key with a different
	// payload returns ErrPayloadConflict.
	Accept(ctx context.Context, request AcceptRequest) (record *Record, duplicate bool, err error)

	// SetStatus advances an accepted record to a delivery outcome. Repeating the
	// same update is idempotent; terminal outcomes never regress.
	//
	// Deprecated: use SetDelivery for owner-conditioned updates.
	SetStatus(ctx context.Context, executionID string, status Status, errorCode string) error

	// SetDelivery advances the delivery status of an execution owned by ownerID.
	// The update is conditional on the current owner; mismatch returns
	// ErrOwnerMismatch. Terminal delivery statuses never regress.
	SetDelivery(ctx context.Context, executionID, ownerID string, status Status, errorCode string) error

	// MarkRunning transitions runtime_status from pending to running and records
	// the worker_run_id. Called after active gate passes, before Worker.Input.
	MarkRunning(ctx context.Context, executionID, ownerID, workerRunID string) error

	// FinishRuntime sets the terminal runtime status (completed/failed) for an
	// execution matching executionID and workerRunID. Sets finished_at, releases
	// the active gate, and stops lease renewal for this execution. Late Done
	// events with matching workerRunID can refine unknown → completed/failed.
	FinishRuntime(ctx context.Context, executionID, workerRunID string, status RuntimeStatus, errorCode string) error

	// ActiveBySession returns the current pending/running execution for the
	// session, or ErrNotFound if none exists. Used by the active gate check.
	ActiveBySession(ctx context.Context, sessionID string) (*Record, error)

	// OpenBySession returns the most recent non-terminal execution (pending,
	// running, or unknown) for the session, or ErrNotFound if none exists.
	// Used by the Done-event convergence path to finish a runtime that lease
	// recovery may have already marked unknown (unknown -> completed/failed).
	OpenBySession(ctx context.Context, sessionID string) (*Record, error)

	// FenceBySession returns the fenced execution for the session (if any), or
	// ErrNotFound. A fenced session requires fresh Worker before new input.
	FenceBySession(ctx context.Context, sessionID string) (*Record, error)

	// ClearFenceAfterFreshStart atomically clears the fence_reason and sets a new
	// worker_run_id, only if the current fence_reason matches the given reason.
	// This is the final step of the fresh-Worker flow.
	ClearFenceAfterFreshStart(ctx context.Context, executionID, reason, freshWorkerRunID string) error

	// RenewLeases batch-renews all pending/running executions owned by ownerID,
	// extending lease_until to now + ttl. Returns the number of renewed records.
	// No-op when the owner has no active executions.
	RenewLeases(ctx context.Context, ownerID string, ttlSeconds int64) (int64, error)

	// RecoverExpiredLeases recovers executions whose lease has expired, setting
	// runtime_status to unknown and fence_reason. Only pending/running records
	// with lease_until <= now are affected. Returns the number of recovered
	// records. Safe for concurrent reconcilers: the conditional UPDATE ensures
	// only one wins per row.
	RecoverExpiredLeases(ctx context.Context, nowUnixMilli int64) (int64, error)

	// TerminateOwnerLeases marks all active (pending/running) executions owned by
	// ownerID as unknown with a fence_reason. Used during graceful shutdown.
	TerminateOwnerLeases(ctx context.Context, ownerID, reason string) (int64, error)
}
