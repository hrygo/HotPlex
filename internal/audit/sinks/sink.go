// Package sinks provides AlertSink implementations for the user behavior audit
// system. See spec section 5.6.
package sinks

import (
	"context"
	"time"
)

// AlertEvent is the read-only snapshot passed to a sink. Mirrors audit.AuditEvent
// but lives in this package to avoid a circular import (W2.A defines the same type
// in the parent audit package; the collector fans out to sinks.Sink instances).
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

// FromAuditEvent converts an audit.AuditEvent to an AlertEvent.
// Use this in the collector when calling sink.OnAlertEvent.
// TODO: populate fields from audit.AuditEvent once W2.A defines it.
func FromAuditEvent() AlertEvent {
	return AlertEvent{}
}
