// Package sinks AlertEvent and Sink types. The package-level documentation
// lives in doc.go.
package sinks

import (
	"context"
	"time"
)

// AlertEvent is the read-only snapshot passed to a sink. Mirrors audit.AuditEvent
// field-for-field. The two types are kept separate to preserve the one-way import
// direction documented above.
type AlertEvent struct {
	EventID      string
	Ts           time.Time
	UserID       string
	UserIDType   string
	Platform     string
	SessionID    string
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	Detail       map[string]any
	EventRef     string
	IP           string
	UserAgent    string
}

// Sink is the extension point. Implementations must be non-blocking (use internal
// goroutine + queue) and must not return errors that affect the audit write path.
type Sink interface {
	// OnAlertEvent is called for every persisted audit event.
	// The implementation MUST return quickly. It MUST NOT panic.
	// Errors are non-fatal; the audit collector continues regardless.
	OnAlertEvent(ctx context.Context, e AlertEvent) error
}

// Closer is an optional lifecycle contract. Collectors call it after all
// queued events have been handed to the sink.
type Closer interface {
	Close(ctx context.Context) error
}
