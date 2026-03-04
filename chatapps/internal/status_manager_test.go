package internal

import (
	"context"
	"log/slog"
	"testing"

	"github.com/hrygo/hotplex/chatapps/base"
)

// mockStatusProvider is a mock implementation of StatusProvider for testing
type mockStatusProvider struct {
	calls []struct {
		method    string
		channelID string
		threadTS  string
		status    base.StatusType
		text      string
	}
}

func (m *mockStatusProvider) SetStatus(ctx context.Context, channelID, threadTS string, status base.StatusType, text string) error {
	m.calls = append(m.calls, struct {
		method    string
		channelID string
		threadTS  string
		status    base.StatusType
		text      string
	}{
		method:    "SetStatus",
		channelID: channelID,
		threadTS:  threadTS,
		status:    status,
		text:      text,
	})
	return nil
}

func (m *mockStatusProvider) ClearStatus(ctx context.Context, channelID, threadTS string) error {
	m.calls = append(m.calls, struct {
		method    string
		channelID string
		threadTS  string
		status    base.StatusType
		text      string
	}{
		method:    "ClearStatus",
		channelID: channelID,
		threadTS:  threadTS,
	})
	return nil
}

func TestStatusManager_Notify(t *testing.T) {
	provider := &mockStatusProvider{}
	logger := testLogger()
	manager := NewStatusManager(provider, logger)

	ctx := context.Background()

	// First notification should call provider
	err := manager.Notify(ctx, "C123", "T100", base.StatusThinking, "Thinking...")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(provider.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(provider.calls))
	}

	if provider.calls[0].method != "SetStatus" {
		t.Errorf("expected SetStatus, got %s", provider.calls[0].method)
	}

	if provider.calls[0].status != base.StatusThinking {
		t.Errorf("expected StatusThinking, got %s", provider.calls[0].status)
	}
}

func TestStatusManager_Notify_Deduplication(t *testing.T) {
	provider := &mockStatusProvider{}
	logger := testLogger()
	manager := NewStatusManager(provider, logger)

	ctx := context.Background()

	// First notification
	_ = manager.Notify(ctx, "C123", "T100", base.StatusThinking, "Thinking...")

	// Second notification with same status - should be deduplicated
	_ = manager.Notify(ctx, "C123", "T100", base.StatusThinking, "Still thinking...")

	if len(provider.calls) != 1 {
		t.Fatalf("expected 1 call (deduplicated), got %d", len(provider.calls))
	}
}

func TestStatusManager_Notify_StatusChange(t *testing.T) {
	provider := &mockStatusProvider{}
	logger := testLogger()
	manager := NewStatusManager(provider, logger)

	ctx := context.Background()

	// First notification - thinking
	_ = manager.Notify(ctx, "C123", "T100", base.StatusThinking, "Thinking...")

	// Status change - should trigger new call
	_ = manager.Notify(ctx, "C123", "T100", base.StatusToolUse, "Using tool...")

	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(provider.calls))
	}

	if provider.calls[1].status != base.StatusToolUse {
		t.Errorf("expected StatusToolUse, got %s", provider.calls[1].status)
	}
}

func TestStatusManager_Clear(t *testing.T) {
	provider := &mockStatusProvider{}
	logger := testLogger()
	manager := NewStatusManager(provider, logger)

	ctx := context.Background()

	// Set a status first
	_ = manager.Notify(ctx, "C123", "T100", base.StatusThinking, "Thinking...")

	// Clear status
	err := manager.Clear(ctx, "C123", "T100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(provider.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(provider.calls))
	}

	if provider.calls[1].method != "ClearStatus" {
		t.Errorf("expected ClearStatus, got %s", provider.calls[1].method)
	}
}

func TestStatusManager_Current(t *testing.T) {
	provider := &mockStatusProvider{}
	logger := testLogger()
	manager := NewStatusManager(provider, logger)

	ctx := context.Background()

	// Initial state should be empty (not yet set)
	if manager.Current() != "" {
		t.Errorf("expected initial empty status, got %s", manager.Current())
	}

	// After notify, should be updated
	_ = manager.Notify(ctx, "C123", "T100", base.StatusThinking, "Thinking...")
	if manager.Current() != base.StatusThinking {
		t.Errorf("expected StatusThinking, got %s", manager.Current())
	}

	// After clear, should be idle
	_ = manager.Clear(ctx, "C123", "T100")
	if manager.Current() != base.StatusIdle {
		t.Errorf("expected StatusIdle after clear, got %s", manager.Current())
	}
}

func testLogger() *slog.Logger {
	return slog.Default()
}
