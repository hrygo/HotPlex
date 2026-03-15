package diag

import (
	"testing"
	"time"
)

func TestBaseTrigger(t *testing.T) {
	errInfo := &ErrorInfo{
		Type:    ErrorTypeExit,
		Message: "test error",
		ExitCode: 1,
	}

	trigger := NewBaseTrigger(TriggerAuto, "session-123", errInfo)
	trigger.SetPlatform("slack").
		SetUserID("U123").
		SetChannelID("C456").
		SetThreadID("T789")

	// Test getters
	if trigger.Type() != TriggerAuto {
		t.Errorf("Type() = %v, want %v", trigger.Type(), TriggerAuto)
	}
	if trigger.SessionID() != "session-123" {
		t.Errorf("SessionID() = %q, want %q", trigger.SessionID(), "session-123")
	}
	if trigger.Error() != errInfo {
		t.Error("Error() should return the same pointer")
	}
	if trigger.Platform() != "slack" {
		t.Errorf("Platform() = %q, want %q", trigger.Platform(), "slack")
	}
	if trigger.UserID() != "U123" {
		t.Errorf("UserID() = %q, want %q", trigger.UserID(), "U123")
	}
	if trigger.ChannelID() != "C456" {
		t.Errorf("ChannelID() = %q, want %q", trigger.ChannelID(), "C456")
	}
	if trigger.ThreadID() != "T789" {
		t.Errorf("ThreadID() = %q, want %q", trigger.ThreadID(), "T789")
	}
}

func TestBaseTrigger_DefaultValues(t *testing.T) {
	trigger := NewBaseTrigger(TriggerCommand, "session-1", nil)

	// Platform/UserID/ChannelID/ThreadID should be empty by default
	if trigger.Platform() != "" {
		t.Errorf("Platform() = %q, want empty", trigger.Platform())
	}
	if trigger.UserID() != "" {
		t.Errorf("UserID() = %q, want empty", trigger.UserID())
	}
	if trigger.ChannelID() != "" {
		t.Errorf("ChannelID() = %q, want empty", trigger.ChannelID())
	}
	if trigger.ThreadID() != "" {
		t.Errorf("ThreadID() = %q, want empty", trigger.ThreadID())
	}
}

func TestDiagContext_WithAllFields(t *testing.T) {
	now := time.Now()
	env := &EnvInfo{
		OS:       "linux",
		Arch:     "amd64",
		GoVersion: "1.21",
	}

	conv := &ConversationData{
		Processed:    "user: hello",
		MessageCount: 1,
	}

	ctx := &DiagContext{
		OriginalSessionID: "session-1",
		Platform:         "slack",
		UserID:           "U123",
		ChannelID:        "C456",
		ThreadID:         "T789",
		Trigger:          TriggerAuto,
		Error: &ErrorInfo{
			Type:    ErrorTypeExit,
			Message: "process exited",
		},
		Conversation: conv,
		Logs:         []byte("log line 1\nlog line 2"),
		Environment:  env,
		Timestamp:    now,
	}

	// Verify all fields are set correctly
	if ctx.OriginalSessionID != "session-1" {
		t.Errorf("OriginalSessionID = %q", ctx.OriginalSessionID)
	}
	if ctx.Platform != "slack" {
		t.Errorf("Platform = %q", ctx.Platform)
	}
	if ctx.Error.Type != ErrorTypeExit {
		t.Errorf("Error.Type = %v", ctx.Error.Type)
	}
	if ctx.Conversation.MessageCount != 1 {
		t.Errorf("Conversation.MessageCount = %d", ctx.Conversation.MessageCount)
	}
	if ctx.Environment.OS != "linux" {
		t.Errorf("Environment.OS = %q", ctx.Environment.OS)
	}
}

func TestErrorInfo_String(t *testing.T) {
	tests := []struct {
		name     string
		err      *ErrorInfo
		wantType ErrorType
	}{
		{
			name:     "cli exit",
			err:      &ErrorInfo{Type: ErrorTypeExit, ExitCode: 1},
			wantType: ErrorTypeExit,
		},
		{
			name:     "timeout",
			err:      &ErrorInfo{Type: ErrorTypeTimeout, Message: "timeout"},
			wantType: ErrorTypeTimeout,
		},
		{
			name:     "waf violation",
			err:      &ErrorInfo{Type: ErrorTypeWAF, Message: "blocked"},
			wantType: ErrorTypeWAF,
		},
		{
			name:     "panic",
			err:      &ErrorInfo{Type: ErrorTypePanic, StackTrace: "goroutine 1..."},
			wantType: ErrorTypePanic,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", tt.err.Type, tt.wantType)
			}
		})
	}
}
