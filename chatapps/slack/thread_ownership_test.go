package slack

import (
	"io"
	"log/slog"
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
			expected:  "", // Empty key to prevent collisions
		},
		{
			name:      "empty channel id",
			channelID: "",
			threadTS:  "1234567890.123456",
			expected:  "", // Empty key to prevent collisions
		},
		{
			name:      "both empty",
			channelID: "",
			threadTS:  "",
			expected:  "", // Empty key
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

func TestThreadOwnershipTracker_Claim(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// First claim
	isNew := tracker.Claim(key)
	if !isNew {
		t.Error("First claim should return true")
	}

	// Verify ownership
	if !tracker.Owns(key) {
		t.Error("Bot should own the thread after claim")
	}

	// Second claim (not new)
	isNew = tracker.Claim(key)
	if isNew {
		t.Error("Second claim should return false")
	}

	// Should still own
	if !tracker.Owns(key) {
		t.Error("Bot should still own the thread")
	}
}

func TestThreadOwnershipTracker_Release(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Claim and release
	tracker.Claim(key)
	tracker.Release(key)

	if tracker.Owns(key) {
		t.Error("Bot should not own the thread after release")
	}
}

func TestThreadOwnershipTracker_TTL(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Use very short TTL for testing
	tracker := NewThreadOwnershipTracker(100*time.Millisecond, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Claim ownership
	tracker.Claim(key)

	// Should be owner immediately
	if !tracker.Owns(key) {
		t.Error("Bot should own the thread")
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Should no longer be owner after TTL
	if tracker.Owns(key) {
		t.Error("Bot should not own the thread after TTL expiration")
	}
}

func TestThreadOwnershipTracker_CleanupExpired(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewThreadOwnershipTracker(100*time.Millisecond, logger)

	// Create multiple threads
	key1 := NewThreadKey("C12345", "1111111111.111111")
	key2 := NewThreadKey("C12345", "2222222222.222222")
	key3 := NewThreadKey("C12345", "3333333333.333333")

	tracker.Claim(key1)
	tracker.Claim(key2)
	tracker.Claim(key3)

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	// Cleanup
	expired := tracker.CleanupExpired()
	if expired != 3 {
		t.Errorf("Expected 3 expired, got %d", expired)
	}
}

func TestThreadOwnershipTracker_UpdateLastActive(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewThreadOwnershipTracker(100*time.Millisecond, logger)
	key := NewThreadKey("C12345", "1234567890.123456")

	// Claim ownership
	tracker.Claim(key)

	// Wait almost TTL
	time.Sleep(80 * time.Millisecond)

	// Update last active
	tracker.UpdateLastActive(key)

	// Wait another 80ms (total 160ms from claim, but only 80ms from last update)
	time.Sleep(80 * time.Millisecond)

	// Should still be owner because last active was updated
	if !tracker.Owns(key) {
		t.Error("Bot should still own the thread after last active update")
	}
}

func TestThreadOwnershipTracker_MultipleThreads(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := NewThreadOwnershipTracker(24*time.Hour, logger)

	key1 := NewThreadKey("C12345", "1111111111.111111")
	key2 := NewThreadKey("C12345", "2222222222.222222")

	// Claim both
	tracker.Claim(key1)
	tracker.Claim(key2)

	// Both owned
	if !tracker.Owns(key1) {
		t.Error("Bot should own thread 1")
	}
	if !tracker.Owns(key2) {
		t.Error("Bot should own thread 2")
	}

	// Release one
	tracker.Release(key1)

	// Only one owned
	if tracker.Owns(key1) {
		t.Error("Bot should not own thread 1 after release")
	}
	if !tracker.Owns(key2) {
		t.Error("Bot should still own thread 2")
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
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			},
			userID:         "U12345",
			expectedCan:    true,
			expectedPolicy: OwnerPolicyPublic,
		},
		{
			name: "owner_only - primary owner",
			config: &Config{
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
				BotToken:      "xoxb-123-456-abc",
				SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
		BotToken:      "xoxb-123-456-abc",
		SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
		BotToken:      "xoxb-123-456-abc",
		SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if noOwnerConfig.IsOwner("U12345") {
		t.Error("Should return false when no owner config")
	}
}

func TestIsTrusted(t *testing.T) {
	config := &Config{
		BotToken:      "xoxb-123-456-abc",
		SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
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
		BotToken:      "xoxb-123-456-abc",
		SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ThreadOwnership: &ThreadOwnershipConfig{
			Enabled: PtrBool(true),
			TTL:     12 * time.Hour,
			Persist: PtrBool(true),
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
		BotToken:      "xoxb-123-456-abc",
		SigningSecret: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	if !noConfig.IsThreadOwnershipEnabled() {
		t.Error("Thread ownership should be enabled by default")
	}
	if noConfig.GetThreadOwnershipTTL() != 24*time.Hour {
		t.Errorf("Default TTL should be 24h, got %v", noConfig.GetThreadOwnershipTTL())
	}
}
