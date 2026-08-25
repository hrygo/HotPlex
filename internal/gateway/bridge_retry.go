package gateway

import (
	"context"
	"time"

	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/internal/worker"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// CancelRetry cancels any pending auto-retry for a session.
// Called by handler when a user sends a new input.
func (b *Bridge) CancelRetry(sessionID string) {
	b.retryCancelMu.Lock()
	defer b.retryCancelMu.Unlock()
	if ch, ok := b.retryCancel[sessionID]; ok {
		close(ch)
		delete(b.retryCancel, sessionID)
	}
}

// autoRetry performs exponential backoff then sends the retry input to the worker.
// cancelCh is pre-registered by the caller to eliminate the race window between
// goroutine launch and cancel channel registration.
func (b *Bridge) autoRetry(ctx context.Context, w worker.Worker, sessionID string, attempt int, cancelCh chan struct{}, lifecycle *workerRunLifecycle) {
	delay := b.retryCtrl.Delay(attempt)

	// Clean up cancel channel on exit (registered by caller before goroutine launch).
	defer func() {
		b.retryCancelMu.Lock()
		delete(b.retryCancel, sessionID)
		b.retryCancelMu.Unlock()
	}()

	// Notify user if enabled.
	if b.retryCtrl.ShouldNotify() {
		releaseEvent, admitted := lifecycle.beginEvent()
		if !admitted {
			return
		}
		msg := b.retryCtrl.NotifyMessage(attempt)
		notifyEnv := buildNotifyEnvelope(sessionID, msg, 0)
		_ = b.hub.SendToSession(ctx, notifyEnv)
		releaseEvent()
	}

	// Wait with backoff, respecting cancellation.
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-cancelCh:
		b.log.Info("bridge: auto-retry cancelled by user input", "session_id", sessionID)
		return
	case <-timer.C:
	}

	// Send retry input to worker.
	releaseEvent, admitted := lifecycle.beginEvent()
	if !admitted {
		return
	}
	defer releaseEvent()
	b.log.Info("bridge: auto-retry sending input", "session_id", sessionID, "attempt", attempt)
	observability.RetryAttempts().Add(ctx, 1, metric.WithAttributes(attribute.String("reason", "llm_error")))
	if err := w.Input(ctx, b.retryCtrl.RetryInput(), nil); err != nil {
		b.log.Warn("bridge: auto-retry input failed", "session_id", sessionID, "err", err)
	}
}
