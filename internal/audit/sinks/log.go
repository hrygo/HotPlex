package sinks

import (
	"context"
	"log/slog"
)

// LogSink writes audit events to a slog.Logger. For debugging and development.
type LogSink struct {
	log *slog.Logger
}

// NewLogSink returns a LogSink that writes to log.
func NewLogSink(log *slog.Logger) *LogSink {
	if log == nil {
		log = slog.Default()
	}
	return &LogSink{log: log}
}

// OnAlertEvent logs the event at INFO level with structured attributes.
func (s *LogSink) OnAlertEvent(ctx context.Context, e AlertEvent) error {
	s.log.Info("audit_event",
		"event_id", e.EventID,
		"ts", e.Ts.UnixMilli(),
		"user_id", e.UserID,
		"user_id_type", e.UserIDType,
		"platform", e.Platform,
		"session_id", e.SessionID,
		"action", e.Action,
		"resource_type", e.ResourceType,
		"resource_id", e.ResourceID,
		"outcome", e.Outcome,
		"event_ref", e.EventRef,
		"ip", e.IP,
		"user_agent", e.UserAgent,
		"detail_count", len(e.Detail),
	)
	return nil
}
