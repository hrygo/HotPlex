// Package sinks provides AlertSink implementations for the user behavior audit
// system. See spec section 5.6.
//
// This package intentionally does NOT import the parent audit package. The
// AlertEvent type here mirrors audit.AuditEvent by value, and the bridge between
// the two (audit.AlertSink → sinks.Sink) is performed in cmd/hotplex via a small
// sinkAdapter. This keeps the dependency direction one-way: cmd/hotplex imports
// both, and sinks stays a leaf package that third-party code can import without
// pulling in the collector.
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
