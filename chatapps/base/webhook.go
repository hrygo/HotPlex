package base

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/dedup"
	"github.com/hrygo/hotplex/internal/panicx"
)

// WebhookRunner manages the lifecycle of webhook processing goroutines.
// This eliminates the duplicate webhookWg pattern across all adapters.
type WebhookRunner struct {
	wg           sync.WaitGroup
	logger       *slog.Logger
	deduplicator *dedup.Deduplicator
	keyStrategy  dedup.KeyStrategy
}

// NewWebhookRunner creates a new WebhookRunner with deduplication.
func NewWebhookRunner(logger *slog.Logger) *WebhookRunner {
	return &WebhookRunner{
		logger: logger,
		// Issue #129: Reduce TTL from 30s to 5s to prevent normal messages being skipped
		// Short window is sufficient to prevent network jitter/button double-click duplicates
		deduplicator: dedup.NewDeduplicator(5*time.Second, 10*time.Second),
		keyStrategy:  dedup.NewSlackKeyStrategy(),
	}
}

// Run executes the handler in a goroutine and tracks its completion.
// If handler is nil, this is a no-op.
// Implements event deduplication to prevent duplicate processing.
func (r *WebhookRunner) Run(ctx context.Context, handler MessageHandler, msg *ChatMessage) {
	if handler == nil {
		return
	}

	// Generate deduplication key
	eventData := map[string]any{
		"platform":   msg.Platform,
		"event_type": msg.Metadata["event_type"],
		"channel":    msg.Metadata["channel_id"],
		"event_ts":   msg.Metadata["event_ts"],
		"session_id": msg.SessionID,
	}
	key := r.keyStrategy.GenerateKey(eventData)

	// Check for duplicate
	if r.deduplicator.Check(key) {
		r.logger.Debug("Duplicate event detected, skipping",
			"platform", msg.Platform,
			"session_id", msg.SessionID,
			"key", key)
		return
	}

	r.wg.Add(1)
	panicx.SafeGo(r.logger, func() {
		defer r.wg.Done()
		if err := handler(ctx, msg); err != nil {
			if r.logger != nil {
				r.logger.Error("Handle message failed", "error", err)
			}
		}
	})
}

// Wait blocks until all running goroutines complete or timeout occurs.
// Returns true if all goroutines completed, false if timeout occurred.
func (r *WebhookRunner) Wait(timeout time.Duration) bool {
	done := make(chan struct{})
	panicx.SafeGo(r.logger, func() {
		r.wg.Wait()
		close(done)
	})

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		if r.logger != nil {
			r.logger.Warn("Timeout waiting for webhook goroutines")
		}
		return false
	}
}

// WaitDefault blocks with the default 5 second timeout.
func (r *WebhookRunner) WaitDefault() bool {
	return r.Wait(5 * time.Second)
}

// Stop is an alias for WaitDefault for API consistency with adapters.
func (r *WebhookRunner) Stop() bool {
	return r.WaitDefault()
}
