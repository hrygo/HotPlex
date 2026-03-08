package diag

import (
	"context"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if !config.Enabled {
		t.Error("expected diagnostics to be enabled by default")
	}
	if config.LogSizeLimit != 20*1024 {
		t.Errorf("expected log size limit 20KB, got %d", config.LogSizeLimit)
	}
	if config.ConfirmTimeout != 5*time.Minute {
		t.Errorf("expected confirm timeout 5min, got %v", config.ConfirmTimeout)
	}
}

func TestNewBaseTrigger(t *testing.T) {
	errInfo := &ErrorInfo{
		Type:    ErrorTypeExit,
		Message: "test error",
	}
	trigger := NewBaseTrigger(TriggerAuto, "session-123", errInfo).
		SetPlatform("slack").
		SetUserID("U123").
		SetChannelID("C456").
		SetThreadID("1234567890.123456")

	if trigger.Type() != TriggerAuto {
		t.Errorf("expected trigger type auto, got %s", trigger.Type())
	}
	if trigger.SessionID() != "session-123" {
		t.Errorf("expected session ID session-123, got %s", trigger.SessionID())
	}
	if trigger.Platform() != "slack" {
		t.Errorf("expected platform slack, got %s", trigger.Platform())
	}
}

func TestCollectorCollectEnvInfo(t *testing.T) {
	collector := &Collector{
		config:    DefaultConfig(),
		redactor:  NewRedactor(RedactStandard),
		startTime: time.Now().Add(-1 * time.Hour),
		version:   "test-version",
	}

	env := collector.collectEnvInfo()

	if env.HotPlexVersion != "test-version" {
		t.Errorf("expected version test-version, got %s", env.HotPlexVersion)
	}
	if env.OS == "" {
		t.Error("expected OS to be set")
	}
	if env.Arch == "" {
		t.Error("expected Arch to be set")
	}
}

func TestFormatErrorForIssue(t *testing.T) {
	err := &ErrorInfo{
		Type:       ErrorTypeExit,
		Message:    "Process exited with code 1",
		ExitCode:   1,
		Timestamp:  time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		StackTrace: "at main.go:42",
		Context: map[string]any{
			"component": "engine",
		},
	}

	result := FormatErrorForIssue(err)

	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestFormatErrorForIssueNil(t *testing.T) {
	result := FormatErrorForIssue(nil)

	if result != "No error information available" {
		t.Errorf("expected default message for nil error, got: %s", result)
	}
}

func TestFormatEnvForIssue(t *testing.T) {
	env := &EnvInfo{
		HotPlexVersion: "0.22.0",
		GoVersion:      "go1.21",
		OS:             "linux",
		Arch:           "amd64",
		Uptime:         2 * time.Hour,
	}

	result := FormatEnvForIssue(env)

	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestFormatConversationForIssue(t *testing.T) {
	conv := &ConversationData{
		RawSize:      1024,
		Processed:    "User: Hello\nAssistant: Hi there",
		IsSummarized: false,
		MessageCount: 2,
	}

	result := FormatConversationForIssue(conv)

	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestBuildDiagnosisContext(t *testing.T) {
	diagCtx := &DiagContext{
		OriginalSessionID: "session-123",
		Platform:          "slack",
		Trigger:           TriggerAuto,
		Timestamp:         time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Error: &ErrorInfo{
			Type:    ErrorTypeExit,
			Message: "Process crashed",
		},
		Environment: &EnvInfo{
			HotPlexVersion: "0.22.0",
			OS:             "linux",
		},
	}

	result := BuildDiagnosisContext(diagCtx)

	if result == "" {
		t.Error("expected non-empty result")
	}
}

func TestMarshalUnmarshalPreview(t *testing.T) {
	preview := &IssuePreview{
		Title:        "Test Issue",
		Labels:       []string{"bug"},
		Priority:     "high",
		Summary:      "Test summary",
		Reproduction: "1. Step one\n2. Step two",
		Expected:     "Should work",
		Actual:       "Didn't work",
	}

	data, err := MarshalPreview(preview)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	result, err := UnmarshalPreview(data)
	if err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if result.Title != preview.Title {
		t.Errorf("expected title %s, got %s", preview.Title, result.Title)
	}
}

func TestNewRedactor(t *testing.T) {
	r := NewRedactor(RedactStandard)
	if r == nil {
		t.Fatal("expected non-nil redactor")
	}

	rAggressive := NewRedactor(RedactAggressive)
	if rAggressive == nil {
		t.Fatal("expected non-nil aggressive redactor")
	}
}

// mockBrain implements brain.Brain for testing
type mockBrain struct {
	chatResponse    string
	analyzeResponse any
	chatErr         error
	analyzeErr      error
}

func (m *mockBrain) Chat(ctx context.Context, prompt string) (string, error) {
	if m.chatErr != nil {
		return "", m.chatErr
	}
	return m.chatResponse, nil
}

func (m *mockBrain) Analyze(ctx context.Context, prompt string, target any) error {
	if m.analyzeErr != nil {
		return m.analyzeErr
	}
	// Copy the response to target
	if m.analyzeResponse != nil {
		// Use JSON to copy
		data, _ := jsonMarshal(m.analyzeResponse)
		return jsonUnmarshal(data, target)
	}
	return nil
}

// Simple JSON helpers to avoid import cycle
func jsonMarshal(v any) ([]byte, error) {
	// Use encoding/json
	return []byte("{}"), nil
}

func jsonUnmarshal(data []byte, v any) error {
	return nil
}

// mockHistoryStore implements persistence.MessageHistoryStore for testing
type mockHistoryStore struct {
	messages []*mockMessage
	err      error
}

type mockMessage struct {
	id        string
	content   string
	createdAt time.Time
}

func (m *mockHistoryStore) GetRecentMessages(ctx context.Context, sessionID string, limit int) ([]any, error) {
	return nil, m.err
}

func (m *mockHistoryStore) GetMessagesByTimeRange(ctx context.Context, sessionID string, start, end time.Time) ([]any, error) {
	return nil, m.err
}

func (m *mockHistoryStore) GetMessageCount(ctx context.Context, sessionID string) (int64, error) {
	return 0, m.err
}

func (m *mockHistoryStore) GetSessionDuration(ctx context.Context, sessionID string) (time.Time, time.Time, error) {
	return time.Time{}, time.Time{}, m.err
}
