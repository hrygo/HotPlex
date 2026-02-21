package hotplex

import (
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestEngine_dispatchCallback tests the dispatchCallback method
func TestEngine_dispatchCallback(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	stats := &SessionStats{SessionID: "test"}

	t.Run("error message", func(t *testing.T) {
		msg := StreamMessage{
			Type:  "error",
			Error: "something went wrong",
		}

		var receivedErr error
		cb := func(eventType string, data any) error {
			if eventType == "error" {
				receivedErr = errors.New(data.(string))
			}
			return nil
		}

		_ = engine.dispatchCallback(msg, cb, stats)
		if receivedErr == nil {
			t.Error("Expected error to be passed to callback")
		}
	})

	t.Run("system message", func(t *testing.T) {
		msg := StreamMessage{
			Type: "system",
		}

		var called bool
		cb := func(eventType string, data any) error {
			called = true
			return nil
		}

		_ = engine.dispatchCallback(msg, cb, stats)
		// System messages don't trigger callback
		if called {
			t.Error("System message should not trigger callback")
		}
	})

	t.Run("nil stats", func(t *testing.T) {
		msg := StreamMessage{
			Type: "thinking",
		}

		cb := func(eventType string, data any) error {
			return nil
		}

		// Should not panic with nil stats
		err := engine.dispatchCallback(msg, cb, nil)
		if err != nil {
			t.Errorf("dispatchCallback with nil stats returned error: %v", err)
		}
	})
}

func TestEngine_handleThinkingEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	stats := &SessionStats{SessionID: "test"}

	msg := StreamMessage{
		Type: "thinking",
		Content: []ContentBlock{
			{Type: "text", Text: "thinking..."},
		},
	}

	var received string
	cb := func(eventType string, data any) error {
		if eventType == "thinking" {
			if event, ok := data.(*EventWithMeta); ok {
				received = event.EventData
			}
		}
		return nil
	}

	_ = engine.handleThinkingEvent(msg, cb, stats, 100)
	if received != "thinking..." {
		t.Errorf("received = %q, want 'thinking...'", received)
	}
}

func TestEngine_handleToolUseEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	stats := &SessionStats{SessionID: "test"}

	t.Run("with tool name", func(t *testing.T) {
		msg := StreamMessage{
			Type: "tool_use",
			Name: "bash",
			Content: []ContentBlock{
				{Type: "tool_use", ID: "tool-123"},
			},
		}

		var received string
		cb := func(eventType string, data any) error {
			if eventType == "tool_use" {
				if event, ok := data.(*EventWithMeta); ok {
					received = event.EventData
				}
			}
			return nil
		}

		_ = engine.handleToolUseEvent(msg, cb, stats, 100)
		if received != "bash" {
			t.Errorf("received = %q, want 'bash'", received)
		}
	})

	t.Run("without tool name", func(t *testing.T) {
		msg := StreamMessage{
			Type: "tool_use",
			Name: "",
		}

		cb := func(eventType string, data any) error {
			return nil
		}

		// Should return nil without error
		err := engine.handleToolUseEvent(msg, cb, stats, 100)
		if err != nil {
			t.Errorf("handleToolUseEvent returned error: %v", err)
		}
	})
}

func TestEngine_handleToolResultEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	stats := &SessionStats{SessionID: "test"}
	stats.RecordToolUse("bash", "tool-123")

	t.Run("with output", func(t *testing.T) {
		msg := StreamMessage{
			Type:   "tool_result",
			Output: "command output",
			Content: []ContentBlock{
				{Type: "tool_result", ID: "tool-123"},
			},
		}

		var received string
		cb := func(eventType string, data any) error {
			if eventType == "tool_result" {
				if event, ok := data.(*EventWithMeta); ok {
					received = event.EventData
				}
			}
			return nil
		}

		_ = engine.handleToolResultEvent(msg, cb, stats, 100)
		if received != "command output" {
			t.Errorf("received = %q, want 'command output'", received)
		}
	})

	t.Run("without output", func(t *testing.T) {
		msg := StreamMessage{
			Type:   "tool_result",
			Output: "",
		}

		cb := func(eventType string, data any) error {
			return nil
		}

		// Should return nil without calling callback
		err := engine.handleToolResultEvent(msg, cb, stats, 100)
		if err != nil {
			t.Errorf("handleToolResultEvent returned error: %v", err)
		}
	})
}

func TestEngine_handleAssistantEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	stats := &SessionStats{SessionID: "test"}

	t.Run("with text content", func(t *testing.T) {
		msg := StreamMessage{
			Type: "assistant",
			Content: []ContentBlock{
				{Type: "text", Text: "Hello, world!"},
			},
		}

		var received string
		cb := func(eventType string, data any) error {
			if eventType == "answer" {
				if event, ok := data.(*EventWithMeta); ok {
					received = event.EventData
				}
			}
			return nil
		}

		_ = engine.handleAssistantEvent(msg, cb, stats, 100)
		if received != "Hello, world!" {
			t.Errorf("received = %q, want 'Hello, world!'", received)
		}
	})

	t.Run("with tool_use content", func(t *testing.T) {
		msg := StreamMessage{
			Type: "assistant",
			Content: []ContentBlock{
				{Type: "tool_use", Name: "bash", ID: "tool-456"},
			},
		}

		var received string
		cb := func(eventType string, data any) error {
			if eventType == "tool_use" {
				if event, ok := data.(*EventWithMeta); ok {
					received = event.EventData
				}
			}
			return nil
		}

		_ = engine.handleAssistantEvent(msg, cb, stats, 100)
		if received != "bash" {
			t.Errorf("received = %q, want 'bash'", received)
		}
	})
}

func TestEngine_handleDefaultEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	msg := StreamMessage{
		Type: "unknown",
		Content: []ContentBlock{
			{Type: "text", Text: "some text"},
		},
	}

	var called bool
	cb := func(eventType string, data any) error {
		called = true
		return nil
	}

	_ = engine.handleDefaultEvent(msg, cb, 100)
	if !called {
		t.Error("handleDefaultEvent should call callback")
	}
}

func TestEngine_handleUserEvent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	engine := &Engine{
		opts:   EngineOptions{Namespace: "test"},
		logger: logger,
	}

	stats := &SessionStats{SessionID: "test"}
	stats.RecordToolUse("bash", "tool-789")

	t.Run("with tool_result", func(t *testing.T) {
		msg := StreamMessage{
			Type: "user",
			Content: []ContentBlock{
				{Type: "tool_result", Content: "result content"},
			},
		}

		var received string
		cb := func(eventType string, data any) error {
			if eventType == "tool_result" {
				if event, ok := data.(*EventWithMeta); ok {
					received = event.EventData
				}
			}
			return nil
		}

		_ = engine.handleUserEvent(msg, cb, stats, 100)
		if received != "result content" {
			t.Errorf("received = %q, want 'result content'", received)
		}
	})

	t.Run("without tool_result", func(t *testing.T) {
		msg := StreamMessage{
			Type: "user",
			Content: []ContentBlock{
				{Type: "text", Text: "not a tool result"},
			},
		}

		var called bool
		cb := func(eventType string, data any) error {
			called = true
			return nil
		}

		_ = engine.handleUserEvent(msg, cb, stats, 100)
		if called {
			t.Error("handleUserEvent should not call callback for non-tool_result")
		}
	})
}

func TestEngine_handleStreamRawLine(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a mock manager with session
	mockMgr := &mockSessionManager{sessions: make(map[string]*Session)}
	mockMgr.sessions["test-session"] = &Session{
		Status:       SessionStatusBusy,
		statusChange: make(chan SessionStatus, 10),
	}

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockMgr,
	}

	stats := &SessionStats{SessionID: "test-session"}
	doneChan := make(chan struct{})

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	t.Run("result message", func(t *testing.T) {
		line := `{"type":"result","status":"success","duration_ms":100}`

		cb := func(eventType string, data any) error {
			return nil
		}

		// Create new doneChan for this test
		testDoneChan := make(chan struct{})
		err := engine.handleStreamRawLine(line, cfg, stats, cb, testDoneChan)
		if err != nil {
			t.Errorf("handleStreamRawLine error: %v", err)
		}

		// doneChan should be closed
		select {
		case <-testDoneChan:
			// Expected
		default:
			t.Error("doneChan should be closed after result message")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		line := "not valid json"

		var answerReceived string
		cb := func(eventType string, data any) error {
			if eventType == "answer" {
				answerReceived = data.(string)
			}
			return nil
		}

		testDoneChan := make(chan struct{})
		err := engine.handleStreamRawLine(line, cfg, stats, cb, testDoneChan)
		if err != nil {
			t.Errorf("handleStreamRawLine error: %v", err)
		}

		// Invalid JSON should be passed as answer
		if answerReceived != line {
			t.Errorf("answerReceived = %q, want %q", answerReceived, line)
		}
	})

	t.Run("system message", func(t *testing.T) {
		line := `{"type":"system"}`

		var called bool
		cb := func(eventType string, data any) error {
			called = true
			return nil
		}

		testDoneChan := make(chan struct{})
		_ = engine.handleStreamRawLine(line, cfg, stats, cb, testDoneChan)

		// System messages should not trigger callback
		if called {
			t.Error("System message should not trigger callback")
		}
	})

	_ = doneChan // Avoid unused variable warning
}

func TestEngine_handleResultMessage(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// Create a mock manager for testing
	mockMgr := &mockSessionManager{sessions: make(map[string]*Session)}
	mockMgr.sessions["test-session"] = &Session{
		Status:       SessionStatusBusy,
		statusChange: make(chan SessionStatus, 10),
	}

	engine := &Engine{
		opts:    EngineOptions{Namespace: "test"},
		logger:  logger,
		manager: mockMgr,
	}

	stats := &SessionStats{
		SessionID: "test-session",
		StartTime: time.Now(),
		ToolsUsed: map[string]bool{"bash": true},
	}

	cfg := &Config{
		WorkDir:   "/tmp",
		SessionID: "test-session",
	}

	t.Run("with usage stats", func(t *testing.T) {
		msg := StreamMessage{
			Type:     "result",
			Duration: 1000,
			Usage: &UsageStats{
				InputTokens:  100,
				OutputTokens: 50,
			},
		}

		var sessionStatsReceived bool
		cb := func(eventType string, data any) error {
			if eventType == "session_stats" {
				sessionStatsReceived = true
			}
			return nil
		}

		engine.handleResultMessage(msg, stats, cfg, cb)

		if stats.TotalDurationMs != 1000 {
			t.Errorf("TotalDurationMs = %d, want 1000", stats.TotalDurationMs)
		}
		if stats.InputTokens != 100 {
			t.Errorf("InputTokens = %d, want 100", stats.InputTokens)
		}
		if !sessionStatsReceived {
			t.Error("session_stats event should be sent")
		}
	})

	t.Run("with error", func(t *testing.T) {
		msg := StreamMessage{
			Type:     "result",
			IsError:  true,
			Error:    "something went wrong",
			Duration: 500,
		}

		var errorMsg string
		cb := func(eventType string, data any) error {
			if eventType == "session_stats" {
				if ssd, ok := data.(*SessionStatsData); ok {
					errorMsg = ssd.ErrorMessage
				}
			}
			return nil
		}

		engine.handleResultMessage(msg, stats, cfg, cb)

		if errorMsg != "something went wrong" {
			t.Errorf("ErrorMessage = %q", errorMsg)
		}
	})
}
