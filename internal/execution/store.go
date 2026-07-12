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

var (
	// ErrNotFound means the execution does not exist.
	ErrNotFound = errors.New("execution: record not found")
	// ErrPayloadConflict means a client message ID was reused with different data.
	ErrPayloadConflict = errors.New("execution: client message id reused with different payload")
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
}

// AcceptRequest describes an input that must be durably accepted before dispatch.
type AcceptRequest struct {
	SessionID       string
	ClientMessageID string
	PayloadHash     string
}

// Store is the narrow persistence contract required by the gateway Handler.
type Store interface {
	// Accept creates a durable accepted record. When the session/message key
	// already exists it returns that record with duplicate=true. Reusing the key
	// with a different payload returns ErrPayloadConflict.
	Accept(ctx context.Context, request AcceptRequest) (record *Record, duplicate bool, err error)

	// SetStatus advances an accepted record to a delivery outcome. Repeating the
	// same update is idempotent; terminal outcomes never regress.
	SetStatus(ctx context.Context, executionID string, status Status, errorCode string) error
}
