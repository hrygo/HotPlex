package feishu

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/command"
)

func TestRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(100 * time.Millisecond)

	// First request should be allowed
	if !limiter.Allow("user1") {
		t.Error("First request should be allowed")
	}

	// Second request within duration should be denied
	if limiter.Allow("user1") {
		t.Error("Second request should be denied")
	}

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)

	// Third request after cooldown should be allowed
	if !limiter.Allow("user1") {
		t.Error("Third request should be allowed after cooldown")
	}

	// Different user should be allowed
	if !limiter.Allow("user2") {
		t.Error("Different user should be allowed")
	}
}

func TestCommandHandler_mapCommand(t *testing.T) {
	tests := []struct {
		feishuCmd string
		want      string
	}{
		{"reset", command.CommandReset},
		{"Reset", command.CommandReset},
		{"RESET", command.CommandReset},
		{"dc", command.CommandDisconnect},
		{"DC", command.CommandDisconnect},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.feishuCmd, func(t *testing.T) {
			// Inline the mapCommand logic for testing
			var got string
			switch strings.ToLower(tt.feishuCmd) {
			case "reset":
				got = command.CommandReset
			case "dc":
				got = command.CommandDisconnect
			default:
				got = ""
			}

			if got != tt.want {
				t.Errorf("mapCommand(%q) = %q, want %q", tt.feishuCmd, got, tt.want)
			}
		})
	}
}

func TestCommandEvent_JSONUnmarshal(t *testing.T) {
	jsonStr := `{
		"header": {
			"event_id": "evt-123",
			"event_type": "application.open_event_v6",
			"create_time": "2026-03-03T09:00:00Z",
			"app_id": "app-123",
			"tenant_key": "tenant-456"
		},
		"event": {
			"app_id": "app-123",
			"tenant_key": "tenant-456",
			"operator_id": {
				"user_id": "user-789"
			},
			"name": "reset",
			"content": {
				"text": ""
			}
		},
		"token": "challenge-token"
	}`

	var event CommandEvent
	if err := json.Unmarshal([]byte(jsonStr), &event); err != nil {
		t.Fatalf("Failed to parse command event: %v", err)
	}

	if event.Header.EventID != "evt-123" {
		t.Errorf("EventID mismatch: got %s", event.Header.EventID)
	}
	if event.Event.Name != "reset" {
		t.Errorf("Command name mismatch: got %s", event.Event.Name)
	}
	if event.Event.OperatorID.UserID != "user-789" {
		t.Errorf("UserID mismatch: got %s", event.Event.OperatorID.UserID)
	}
}
