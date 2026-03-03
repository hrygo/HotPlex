package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/event"
)

// MockCallback implements event.Callback for testing
type MockCallback struct {
	events []string
}

func (m *MockCallback) Handle(eventType string, data any) error {
	m.events = append(m.events, eventType)
	return nil
}

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
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

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
			got := handler.mapCommand(tt.feishuCmd)
			if got != tt.want {
				t.Errorf("mapCommand(%q) = %q, want %q", tt.feishuCmd, got, tt.want)
			}
		})
	}
}

func TestCommandHandler_URLVerification(t *testing.T) {
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

	event := CommandEvent{
		Header: &CommandHeader{
			EventType: "url_verification",
		},
		Token: "test_challenge",
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest("POST", "/feishu/commands", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", "1234567890")
	req.Header.Set("X-Signature", calculateHMACSHA256("1234567890"+"test_encrypt_key"+string(body), "test_encrypt_key"))

	rr := httptest.NewRecorder()
	handler.HandleCommand(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["challenge"] != "test_challenge" {
		t.Errorf("Expected challenge %s, got %s", "test_challenge", response["challenge"])
	}
}

func TestCommandHandler_UnknownEventType(t *testing.T) {
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

	event := CommandEvent{
		Header: &CommandHeader{
			EventType: "unknown.event",
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest("POST", "/feishu/commands", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", "1234567890")
	req.Header.Set("X-Signature", calculateHMACSHA256("1234567890"+"test_encrypt_key"+string(body), "test_encrypt_key"))

	rr := httptest.NewRecorder()
	handler.HandleCommand(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestCommandHandler_MissingCommandName(t *testing.T) {
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

	event := CommandEvent{
		Header: &CommandHeader{
			EventType: "application.open_event_v6",
		},
		Event: &CommandEventData{
			OperatorID: &UserID{UserID: "user123"},
			// Name is missing
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest("POST", "/feishu/commands", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", "1234567890")
	req.Header.Set("X-Signature", calculateHMACSHA256("1234567890"+"test_encrypt_key"+string(body), "test_encrypt_key"))

	rr := httptest.NewRecorder()
	handler.HandleCommand(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestCommandHandler_UnknownCommand(t *testing.T) {
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

	event := CommandEvent{
		Header: &CommandHeader{
			EventType: "application.open_event_v6",
		},
		Event: &CommandEventData{
			Name:       "unknown_command",
			OperatorID: &UserID{UserID: "user123"},
		},
	}

	body, _ := json.Marshal(event)
	req := httptest.NewRequest("POST", "/feishu/commands", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Timestamp", "1234567890")
	req.Header.Set("X-Signature", calculateHMACSHA256("1234567890"+"test_encrypt_key"+string(body), "test_encrypt_key"))

	rr := httptest.NewRecorder()
	handler.HandleCommand(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
}

func TestCommandHandler_MethodNotAllowed(t *testing.T) {
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

	req := httptest.NewRequest("GET", "/feishu/commands", nil)
	rr := httptest.NewRecorder()
	handler.HandleCommand(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
}

func TestCommandCallback_Handle(t *testing.T) {
	logger := slog.Default()
	config := &Config{
		AppID:             "test_app_id",
		AppSecret:         "test_app_secret",
		VerificationToken: "test_verification_token",
		EncryptKey:        "test_encrypt_key",
		ServerAddr:        ":0",
		SystemPrompt:      "test",
	}

	adapter, err := NewAdapter(config, logger)
	if err != nil {
		t.Fatalf("Failed to create adapter: %v", err)
	}
	defer func() { _ = adapter.Stop() }()

	registry := command.NewRegistry()
	handler := NewCommandHandler(adapter, registry)

	callback := handler.createCommandCallback(context.Background(), "user123")

	// Test that callback doesn't panic
	err = callback("test_event", "test_data")
	if err != nil {
		t.Errorf("Callback() returned error: %v", err)
	}

	// Verify it implements the interface
	_ = event.Callback(callback)
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
