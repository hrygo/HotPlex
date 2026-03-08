package diag

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
)

// Notifier handles sending diagnostic notifications to chat platforms.
type Notifier struct {
	config        *Config
	logger        *slog.Logger
	diagnostician DiagnosticianInterface

	// Platform adapters indexed by platform name
	adapters map[string]adapterOps
	mu       sync.RWMutex
}

// adapterOps holds the adapter and its operations
type adapterOps struct {
	adapter   base.ChatAdapter
	ops       base.MessageOperations
	channelID string
}

// Compile-time interface compliance check
var _ NotifierInterface = (*Notifier)(nil)

// NotifierInterface defines the notifier contract.
type NotifierInterface interface {
	// Notify sends a diagnostic notification to the fixed channel.
	Notify(ctx context.Context, result *DiagResult) error
	// RegisterAdapter registers a platform adapter for notifications.
	RegisterAdapter(platform string, adapter base.ChatAdapter, channelID string) error
	// Start starts the notifier (starts cleanup goroutines).
	Start(ctx context.Context) error
	// Stop stops the notifier.
	Stop() error
}

// NewNotifier creates a new Notifier.
func NewNotifier(config *Config, diagnostician DiagnosticianInterface, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		config:        config,
		logger:        logger.With("component", "diag.notifier"),
		diagnostician: diagnostician,
		adapters:      make(map[string]adapterOps),
	}
}

// RegisterAdapter registers a platform adapter for notifications.
func (n *Notifier) RegisterAdapter(platform string, adapter base.ChatAdapter, channelID string) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// Check if adapter implements MessageOperations
	ops, ok := adapter.(base.MessageOperations)
	if !ok {
		n.logger.Warn("Adapter does not implement MessageOperations", "platform", platform)
	}

	n.adapters[platform] = adapterOps{
		adapter:   adapter,
		ops:       ops,
		channelID: channelID,
	}

	n.logger.Info("Registered adapter for diagnostics", "platform", platform, "channel", channelID)
	return nil
}

// Notify sends a diagnostic notification to the fixed channel.
func (n *Notifier) Notify(ctx context.Context, result *DiagResult) error {
	if result == nil {
		return fmt.Errorf("nil result")
	}

	platform := result.Context.Platform
	if platform == "" {
		platform = "slack" // Default platform
	}

	n.mu.RLock()
	adapterOps, ok := n.adapters[platform]
	n.mu.RUnlock()

	if !ok {
		n.logger.Warn("No adapter registered for platform", "platform", platform)
		// Fall back to auto-creating the issue
		return n.fallbackNotify(ctx, result)
	}

	// Build notification message
	msg := n.buildNotificationMessage(result)

	// Send to fixed channel
	if adapterOps.ops != nil {
		return n.sendWithOperations(ctx, adapterOps, msg, result)
	}

	return n.fallbackNotify(ctx, result)
}

// sendWithOperations sends the notification using MessageOperations.
func (n *Notifier) sendWithOperations(ctx context.Context, ao adapterOps, msg string, result *DiagResult) error {
	channelID := n.config.NotifyChannel
	if channelID == "" {
		channelID = ao.channelID
	}

	// Try to send as a thread reply or direct message
	// For now, use a simple approach - send to the fixed channel
	n.logger.Info("Sending diagnostic notification",
		"channel", channelID,
		"diag_id", result.ID,
	)

	// Build a ChatMessage for the notification
	chatMsg := &base.ChatMessage{
		Content: msg,
	}

	// Try using the adapter's SendMessage if available
	if sender, ok := ao.adapter.(interface {
		SendMessage(context.Context, string, *base.ChatMessage) error
	}); ok {
		return sender.SendMessage(ctx, channelID, chatMsg)
	}

	// Fallback: log and auto-create issue
	n.logger.Warn("Cannot send notification, auto-creating issue")
	return n.fallbackNotify(ctx, result)
}

// fallbackNotify handles cases where notification is not possible.
func (n *Notifier) fallbackNotify(ctx context.Context, result *DiagResult) error {
	// Auto-create the issue
	_, err := n.diagnostician.ConfirmIssue(ctx, result.ID)
	if err != nil {
		return fmt.Errorf("fallback create issue: %w", err)
	}
	return nil
}

// buildNotificationMessage builds the notification message text.
func (n *Notifier) buildNotificationMessage(result *DiagResult) string {
	var msg string

	// Header based on trigger type
	switch result.Context.Trigger {
	case TriggerAuto:
		msg = "🔍 *Auto-Diagnosis Report*\n\n"
	case TriggerCommand:
		msg = "📊 *Diagnostic Report*\n\n"
	}

	// Summary
	if result.Preview != nil {
		msg += fmt.Sprintf("**%s**\n", result.Preview.Title)
		msg += fmt.Sprintf("Priority: %s\n", result.Preview.Priority)
		msg += fmt.Sprintf("\n%s\n", result.Preview.Summary)
	}

	// Error info
	if result.Context.Error != nil {
		msg += fmt.Sprintf("\n**Error:** %s\n", result.Context.Error.Message)
	}

	// Context link
	msg += fmt.Sprintf("\nSession: `%s`", result.Context.OriginalSessionID)

	// Add action buttons info (actual buttons would be via Block Kit)
	msg += "\n\n_Reply `confirm` to create issue or `ignore` to dismiss_"

	return msg
}

// Start starts the notifier.
func (n *Notifier) Start(ctx context.Context) error {
	n.logger.Info("Starting diagnostic notifier")

	// Start periodic cleanup
	go n.cleanupLoop(ctx)

	return nil
}

// Stop stops the notifier.
func (n *Notifier) Stop() error {
	n.logger.Info("Stopping diagnostic notifier")
	return nil
}

// cleanupLoop periodically cleans up stale pending diagnoses.
func (n *Notifier) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if diag, ok := n.diagnostician.(interface{ CleanupStale(time.Duration) int }); ok {
				cleaned := diag.CleanupStale(30 * time.Minute)
				if cleaned > 0 {
					n.logger.Debug("Cleaned up stale diagnoses", "count", cleaned)
				}
			}
		}
	}
}

// NotifyAll sends notifications to all registered platforms.
func (n *Notifier) NotifyAll(ctx context.Context, result *DiagResult) error {
	n.mu.RLock()
	defer n.mu.RUnlock()

	var lastErr error
	for platform := range n.adapters {
		resultCopy := *result
		resultCopy.Context.Platform = platform
		if err := n.Notify(ctx, &resultCopy); err != nil {
			lastErr = err
			n.logger.Error("Failed to notify platform", "platform", platform, "error", err)
		}
	}
	return lastErr
}
