package sinks

import "context"

// NoopSink is a sink that does nothing. The default sink.
type NoopSink struct{}

// NewNoopSink returns a NoopSink instance.
func NewNoopSink() *NoopSink { return &NoopSink{} }

// OnAlertEvent does nothing and returns nil.
func (NoopSink) OnAlertEvent(ctx context.Context, e AlertEvent) error {
	return nil
}
