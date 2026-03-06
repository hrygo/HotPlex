package base

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/dedup"
	"github.com/hrygo/hotplex/internal/panicx"
)

// DefaultDedupWindow 默去重配置 (reduced from 30s to 5s per Issue #129)
// 30 second TTL was too long, causing legitimate messages to be incorrectly filtered as duplicates
const (
	DefaultDedupWindow  = 5 * time.Second
	DefaultDedupCleanup = 10 * time.Second
)

// WebhookRunner manages the lifecycle of webhook processing goroutines.
// This eliminates the duplicate webhookWg pattern across all adapters.
type WebhookRunner struct {
	wg           sync.WaitGroup
	logger       *slog.Logger
	deduplicator *dedup.Deduplicator
	keyStrategy  dedup.KeyStrategy
}

// WebhookRunnerOption configures the WebhookRunner
type WebhookRunnerOption func(*WebhookRunner)

// WithDeduplication enables event deduplication with custom settings
func WithDeduplication(window, cleanup time.Duration, strategy dedup.KeyStrategy) WebhookRunnerOption {
	return func(r *WebhookRunner) {
		r.deduplicator = dedup.NewDeduplicator(window, cleanup)
		r.keyStrategy = strategy
	}
}

// NewWebhookRunner creates a new WebhookRunner.
func NewWebhookRunner(logger *slog.Logger, opts ...WebhookRunnerOption) *WebhookRunner {
	r := &WebhookRunner{
		logger: logger,
	}

	// Apply options
	for _, opt := range opts {
		opt(r)
	}

	// Default deduplication if not configured
	if r.deduplicator == nil {
		r.deduplicator = dedup.NewDeduplicator(DefaultDedupWindow, DefaultDedupCleanup)
	}
	if r.keyStrategy == nil {
		r.keyStrategy = dedup.NewSlackKeyStrategy()
	}

	return r
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
	result := r.WaitDefault()

	// Shutdown deduplicator to stop cleanup goroutine
	if r.deduplicator != nil {
		r.deduplicator.Shutdown()
	}

	return result
}
