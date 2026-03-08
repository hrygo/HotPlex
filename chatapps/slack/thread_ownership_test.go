package slack

import (
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewThreadKey(t *testing.T) {
	tests := []struct {
		name      string
		channelID string
		threadTS  string
		expected  ThreadKey
	}{
		{
			name:      "basic thread key",
			channelID: "C12345",
			threadTS:  "1234567890.123456",
			expected:  "C12345:1234567890.123456",
		},
		{
			name:      "empty thread ts",
			channelID: "C12345",
			threadTS:  "",
			expected:  "C12345:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NewThreadKey(tt.channelID, tt.threadTS)
			if result != tt.expected {
				t.Errorf("NewThreadKey() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestThreadOwnershipTracker_ClaimOwnership(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// First claim
	isNew := tracker.ClaimOwnership(key, "BOT001")
	if !isNew {
		t.Error("First claim should return true")
	}

	// Verify ownership
	if !tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should own the thread")
	}

	// Second claim (not new)
	isNew = tracker.ClaimOwnership(key, "BOT001")
	if isNew {
		t.Error("Second claim should return false")
	}

	// Add second owner
	tracker.ClaimOwnership(key, "BOT002")

	// Both should be owners
	if !tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should still own the thread")
	}
	if !tracker.IsOwner(key, "BOT002") {
		t.Error("BOT002 should own the thread")
	}
}

func TestThreadOwnershipTracker_ReleaseOwnership(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Claim and release
	tracker.ClaimOwnership(key, "BOT001")
	tracker.ReleaseOwnership(key, "BOT001")

	if tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should not own the thread after release")
	}

	// Thread should have no owners
	owners := tracker.GetOwners(key)
	if owners != nil {
		t.Errorf("Expected no owners, got %v", owners)
	}
}

func TestThreadOwnershipTracker_TransferOwnership(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Initial claim
	tracker.ClaimOwnership(key, "BOT001")

	// Transfer to BOT002
	tracker.TransferOwnership(key, "BOT001", "BOT002")

	// BOT001 should no longer own
	if tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should not own the thread after transfer")
	}

	// BOT002 should own
	if !tracker.IsOwner(key, "BOT002") {
		t.Error("BOT002 should own the thread after transfer")
	}
}

func TestThreadOwnershipTracker_SetOwners(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Set multiple owners
	tracker.SetOwners(key, []string{"BOT001", "BOT002", "BOT003"})

	// All should be owners
	for _, botID := range []string{"BOT001", "BOT002", "BOT003"} {
		if !tracker.IsOwner(key, botID) {
			t.Errorf("%s should own the thread", botID)
		}
	}

	// Get owners
	owners := tracker.GetOwners(key)
	if len(owners) != 3 {
		t.Errorf("Expected 3 owners, got %d", len(owners))
	}
}

func TestThreadOwnershipTracker_TTL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// Use very short TTL for testing
	tracker := NewThreadOwnershipTracker(100*time.Millisecond, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Claim ownership
	tracker.ClaimOwnership(key, "BOT001")

	// Should be owner immediately
	if !tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should own the thread")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should no longer be owner after TTL
	if tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should not own the thread after TTL expiration")
	}

	// HasOwner should also return false
	if tracker.HasOwner(key) {
		t.Error("Thread should have no owner after TTL expiration")
	}
}

func TestThreadOwnershipTracker_CleanupExpired(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tracker := NewThreadOwnershipTracker(100*time.Millisecond, logger)

	// Create multiple threads
	key1 := NewThreadKey("C12345", "1111111111.111111")
	key2 := NewThreadKey("C12345", "2222222222.222222")
	key3 := NewThreadKey("C12345", "3333333333.333333")

	tracker.ClaimOwnership(key1, "BOT001")
	tracker.ClaimOwnership(key2, "BOT001")
	tracker.ClaimOwnership(key3, "BOT001")

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Cleanup
	expired := tracker.CleanupExpired()
	if expired != 3 {
		t.Errorf("Expected 3 expired, got %d", expired)
	}
}

func TestThreadOwnershipTracker_UpdateLastActive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	tracker := NewThreadOwnershipTracker(100*time.Millisecond, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Claim ownership
	tracker.ClaimOwnership(key, "BOT001")

	// Wait almost TTL
	time.Sleep(80 * time.Millisecond)

	// Update last active
	tracker.UpdateLastActive(key)

	// Wait another 80ms (total 160ms from claim, but only 80ms from last update)
	time.Sleep(80 * time.Millisecond)

	// Should still be owner because last active was updated
	if !tracker.IsOwner(key, "BOT001") {
		t.Error("BOT001 should still own the thread after last active update")
	}
}

// --- Config Tests ---

func TestOwnerPolicy(t *testing.T) {
	tests := []struct {
		name           string
		config         *Config
		userID         string
		expectedCan    bool
		expectedPolicy OwnerPolicy
	}{
		{
			name: "no owner config - public access",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
			},
			userID:         "U12345",
			expectedCan:    true,
			expectedPolicy: OwnerPolicyPublic,
		},
		{
			name: "owner_only - primary owner",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
				Owner: &OwnerConfig{
					Primary: "U12345",
					Policy:  OwnerPolicyOwnerOnly,
				},
			},
			userID:         "U12345",
			expectedCan:    true,
			expectedPolicy: OwnerPolicyOwnerOnly,
		},
		{
			name: "owner_only - non-owner blocked",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
				Owner: &OwnerConfig{
					Primary: "U12345",
					Policy:  OwnerPolicyOwnerOnly,
				},
			},
			userID:         "U67890",
			expectedCan:    false,
			expectedPolicy: OwnerPolicyOwnerOnly,
		},
		{
			name: "trusted - primary owner",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
				Owner: &OwnerConfig{
					Primary: "U12345",
					Trusted: []string{"U11111"},
					Policy:  OwnerPolicyTrusted,
				},
			},
			userID:         "U12345",
			expectedCan:    true,
			expectedPolicy: OwnerPolicyTrusted,
		},
		{
			name: "trusted - trusted user",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
				Owner: &OwnerConfig{
					Primary: "U12345",
					Trusted: []string{"U11111"},
					Policy:  OwnerPolicyTrusted,
				},
			},
			userID:         "U11111",
			expectedCan:    true,
			expectedPolicy: OwnerPolicyTrusted,
		},
		{
			name: "trusted - non-trusted blocked",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
				Owner: &OwnerConfig{
					Primary: "U12345",
					Trusted: []string{"U11111"},
					Policy:  OwnerPolicyTrusted,
				},
			},
			userID:         "U99999",
			expectedCan:    false,
			expectedPolicy: OwnerPolicyTrusted,
		},
		{
			name: "public - anyone can access",
			config: &Config{
				BotToken:   "xoxb-123-456-abc",
				SigningSecret: strings.Repeat("a", 32),
				Owner: &OwnerConfig{
					Primary: "U12345",
					Policy:  OwnerPolicyPublic,
				},
			},
			userID:         "U99999",
			expectedCan:    true,
			expectedPolicy: OwnerPolicyPublic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canRespond := tt.config.CanRespond(tt.userID)
			if canRespond != tt.expectedCan {
				t.Errorf("CanRespond() = %v, want %v", canRespond, tt.expectedCan)
			}

			policy := tt.config.GetOwnerPolicy()
			if policy != tt.expectedPolicy {
				t.Errorf("GetOwnerPolicy() = %v, want %v", policy, tt.expectedPolicy)
			}
		})
	}
}

func TestIsOwner(t *testing.T) {
	config := &Config{
		BotToken:   "xoxb-123-456-abc",
		SigningSecret: strings.Repeat("a", 32),
		Owner: &OwnerConfig{
			Primary: "U12345",
		},
	}

	if !config.IsOwner("U12345") {
		t.Error("U12345 should be owner")
	}

	if config.IsOwner("U67890") {
		t.Error("U67890 should not be owner")
	}

	// No owner config
	noOwnerConfig := &Config{
		BotToken:   "xoxb-123-456-abc",
		SigningSecret: strings.Repeat("a", 32),
	}

	if noOwnerConfig.IsOwner("U12345") {
		t.Error("Should return false when no owner config")
	}
}

func TestIsTrusted(t *testing.T) {
	config := &Config{
		BotToken:   "xoxb-123-456-abc",
		SigningSecret: strings.Repeat("a", 32),
		Owner: &OwnerConfig{
			Trusted: []string{"U11111", "U22222"},
		},
	}

	if !config.IsTrusted("U11111") {
		t.Error("U11111 should be trusted")
	}

	if !config.IsTrusted("U22222") {
		t.Error("U22222 should be trusted")
	}

	if config.IsTrusted("U99999") {
		t.Error("U99999 should not be trusted")
	}
}

func TestThreadOwnershipConfig(t *testing.T) {
	// With config
	config := &Config{
		BotToken:   "xoxb-123-456-abc",
		SigningSecret: strings.Repeat("a", 32),
		ThreadOwnership: &ThreadOwnershipConfig{
			Enabled: true,
			TTL:     12 * time.Hour,
		},
	}

	if !config.IsThreadOwnershipEnabled() {
		t.Error("Thread ownership should be enabled")
	}

	if config.GetThreadOwnershipTTL() != 12*time.Hour {
		t.Errorf("TTL should be 12h, got %v", config.GetThreadOwnershipTTL())
	}

	// Without config
	noConfig := &Config{
		BotToken:   "xoxb-123-456-abc",
		SigningSecret: strings.Repeat("a", 32),
	}

	if noConfig.IsThreadOwnershipEnabled() {
		t.Error("Thread ownership should be disabled by default")
	}

	if noConfig.GetThreadOwnershipTTL() != 24*time.Hour {
		t.Errorf("Default TTL should be 24h, got %v", noConfig.GetThreadOwnershipTTL())
	}
}
