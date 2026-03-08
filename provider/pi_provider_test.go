package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPiProvider(t *testing.T) {
	enabledTrue := true
	tests := []struct {
		name    string
		config  ProviderConfig
		wantErr bool
	}{
		{
			name: "default config",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi", // Provide BinaryPath to avoid PATH lookup
			},
			wantErr: false,
		},
		{
			name: "with pi config",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
					Thinking: "high",
				},
			},
			wantErr: false,
		},
		{
			name: "with custom binary path",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewPiProvider(tt.config, nil)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, provider)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, provider)
				assert.Equal(t, ProviderTypePi, provider.Metadata().Type)
				assert.Equal(t, "pi", provider.Metadata().BinaryName)
			}
		})
	}
}

func TestPiProvider_Metadata(t *testing.T) {
	enabled := true
	provider, err := NewPiProvider(ProviderConfig{
		Type:       ProviderTypePi,
		Enabled:    &enabled,
		BinaryPath: "/usr/local/bin/pi",
	}, nil)
	require.NoError(t, err)

	meta := provider.Metadata()
	assert.Equal(t, ProviderTypePi, meta.Type)
	assert.Equal(t, "Pi (pi-coding-agent)", meta.DisplayName)
	assert.Equal(t, "pi", meta.BinaryName)

	// Verify features
	assert.True(t, meta.Features.SupportsResume)
	assert.True(t, meta.Features.SupportsStreamJSON)
	assert.True(t, meta.Features.MultiTurnReady)
	assert.True(t, meta.Features.RequiresInitialPromptAsArg)
	assert.False(t, meta.Features.SupportsSSE)
	assert.False(t, meta.Features.SupportsHTTPAPI)
}

func TestPiProvider_BuildCLIArgs(t *testing.T) {
	enabledTrue := true
	tests := []struct {
		name      string
		config    ProviderConfig
		sessionID string
		opts      *ProviderSessionOptions
		wantArgs  []string
	}{
		{
			name: "basic config with prompt",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
				},
			},
			sessionID: "",
			opts: &ProviderSessionOptions{
				InitialPrompt: "Hello",
			},
			wantArgs: []string{"--mode", "json", "--provider", "anthropic", "--model", "claude-sonnet-4-20250514", "Hello"},
		},
		{
			name: "with thinking level",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
					Thinking: "high",
				},
			},
			sessionID: "",
			opts: &ProviderSessionOptions{
				InitialPrompt: "Test",
			},
			wantArgs: []string{"--mode", "json", "--provider", "anthropic", "--model", "claude-sonnet-4-20250514", "--thinking", "high", "Test"},
		},
		{
			name: "with session resume",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider: "anthropic",
				},
			},
			sessionID: "test-session-123",
			opts: &ProviderSessionOptions{
				ResumeSession: true,
			},
			wantArgs: []string{"--mode", "json", "--session", "test-session-123", "--provider", "anthropic"},
		},
		{
			name: "with no-session flag",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider:  "anthropic",
					NoSession: true,
				},
			},
			sessionID: "",
			opts:      nil,
			wantArgs:  []string{"--mode", "json", "--provider", "anthropic", "--no-session"},
		},
		{
			name: "with model override from opts",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider: "anthropic",
					Model:    "claude-sonnet-4-20250514",
				},
			},
			sessionID: "",
			opts: &ProviderSessionOptions{
				Model: "claude-opus-4-6",
			},
			wantArgs: []string{"--mode", "json", "--provider", "anthropic", "--model", "claude-opus-4-6"},
		},
		{
			name: "with task instructions",
			config: ProviderConfig{
				Type:       ProviderTypePi,
				Enabled:    &enabledTrue,
				BinaryPath: "/usr/local/bin/pi",
				Pi: &PiConfig{
					Provider: "anthropic",
				},
			},
			sessionID: "",
			opts: &ProviderSessionOptions{
				InitialPrompt:    "What is the status?",
				TaskInstructions: "You are a helpful assistant.",
			},
			wantArgs: []string{"--mode", "json", "--provider", "anthropic", "<context>\nYou are a helpful assistant.\n</context>\n\n<user_query>\nWhat is the status?\n</user_query>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewPiProvider(tt.config, nil)
			require.NoError(t, err)

			args := provider.BuildCLIArgs(tt.sessionID, tt.opts)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func TestPiProvider_BuildInputMessage(t *testing.T) {
	enabledTrue := true
	provider, err := NewPiProvider(ProviderConfig{
		Type:       ProviderTypePi,
		Enabled:    &enabledTrue,
		BinaryPath: "/usr/local/bin/pi",
	}, nil)
	require.NoError(t, err)

	// Test without task instructions
	msg, err := provider.BuildInputMessage("Hello", "")
	require.NoError(t, err)
	assert.Equal(t, "Hello", msg["prompt"])

	// Test with task instructions
	msg, err = provider.BuildInputMessage("What is this?", "Context info")
	require.NoError(t, err)
	assert.Contains(t, msg["prompt"].(string), "<context>")
	assert.Contains(t, msg["prompt"].(string), "Context info")
	assert.Contains(t, msg["prompt"].(string), "<user_query>")
	assert.Contains(t, msg["prompt"].(string), "What is this?")
}

func TestPiProvider_ParseEvent(t *testing.T) {
	enabled := true
	provider, err := NewPiProvider(ProviderConfig{
		Type:       ProviderTypePi,
		Enabled:    &enabled,
		BinaryPath: "/usr/local/bin/pi",
	}, nil)
	require.NoError(t, err)

	tests := []struct {
		name      string
		line      string
		wantType  ProviderEventType
		wantCount int
	}{
		{
			name:      "session event",
			line:      `{"type":"session","version":3,"id":"test-id","timestamp":"2024-01-01T00:00:00Z","cwd":"/test"}`,
			wantType:  EventTypeSystem,
			wantCount: 1,
		},
		{
			name:      "agent_start event",
			line:      `{"type":"agent_start"}`,
			wantType:  EventTypeSystem,
			wantCount: 1,
		},
		{
			name:      "agent_end event",
			line:      `{"type":"agent_end","messages":[]}`,
			wantType:  EventTypeResult,
			wantCount: 1,
		},
		{
			name:      "turn_start event",
			line:      `{"type":"turn_start"}`,
			wantType:  EventTypeSystem,
			wantCount: 1,
		},
		{
			name:      "turn_end event",
			line:      `{"type":"turn_end","message":{}}`,
			wantType:  EventTypeResult,
			wantCount: 1,
		},
		{
			name:      "message with text content",
			line:      `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"Hello world"}]}}`,
			wantType:  EventTypeAnswer,
			wantCount: 1,
		},
		{
			name:      "message with thinking content",
			line:      `{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"Let me think..."}]}}`,
			wantType:  EventTypeThinking,
			wantCount: 1,
		},
		{
			name:      "tool execution start",
			line:      `{"type":"tool_execution_start","toolCallId":"call-123","toolName":"Read","args":{"file_path":"/test.txt"}}`,
			wantType:  EventTypeToolUse,
			wantCount: 1,
		},
		{
			name:      "tool execution end",
			line:      `{"type":"tool_execution_end","toolCallId":"call-123","toolName":"Read","result":"file content","isError":false}`,
			wantType:  EventTypeToolResult,
			wantCount: 1,
		},
		{
			name:      "text_delta event",
			line:      `{"type":"message_update","message":{},"assistantMessageEvent":{"type":"text_delta","delta":"Hello"}}`,
			wantType:  EventTypeAnswer,
			wantCount: 1,
		},
		{
			name:      "thinking_delta event",
			line:      `{"type":"message_update","message":{},"assistantMessageEvent":{"type":"thinking_delta","delta":"Hmm..."}}`,
			wantType:  EventTypeThinking,
			wantCount: 1,
		},
		{
			name:      "invalid JSON returns raw event",
			line:      `not valid json`,
			wantType:  EventTypeRaw,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := provider.ParseEvent(tt.line)
			require.NoError(t, err)
			require.Len(t, events, tt.wantCount)
			assert.Equal(t, tt.wantType, events[0].Type)
		})
	}
}

func TestPiProvider_DetectTurnEnd(t *testing.T) {
	enabledTrue := true
	provider, err := NewPiProvider(ProviderConfig{
		Type:       ProviderTypePi,
		Enabled:    &enabledTrue,
		BinaryPath: "/usr/local/bin/pi",
	}, nil)
	require.NoError(t, err)

	tests := []struct {
		name    string
		event   *ProviderEvent
		wantEnd bool
	}{
		{
			name:    "turn_end signals end",
			event:   &ProviderEvent{Type: EventTypeResult, RawType: "turn_end"},
			wantEnd: true,
		},
		{
			name:    "agent_end signals end",
			event:   &ProviderEvent{Type: EventTypeResult, RawType: "agent_end"},
			wantEnd: true,
		},
		{
			name:    "error signals end",
			event:   &ProviderEvent{Type: EventTypeError},
			wantEnd: true,
		},
		{
			name:    "result type signals end",
			event:   &ProviderEvent{Type: EventTypeResult},
			wantEnd: true,
		},
		{
			name:    "answer does not signal end",
			event:   &ProviderEvent{Type: EventTypeAnswer},
			wantEnd: false,
		},
		{
			name:    "tool_use does not signal end",
			event:   &ProviderEvent{Type: EventTypeToolUse},
			wantEnd: false,
		},
		{
			name:    "nil event does not signal end",
			event:   nil,
			wantEnd: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := provider.DetectTurnEnd(tt.event)
			assert.Equal(t, tt.wantEnd, got)
		})
	}
}
