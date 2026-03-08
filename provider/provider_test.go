package provider

import (
	"testing"
)

func TestProviderInterface_Compliance(t *testing.T) {
	// Verify ClaudeCodeProvider implements Provider interface
	var _ Provider = (*ClaudeCodeProvider)(nil)

	// Verify OpenCodeProvider implements Provider interface
	var _ Provider = (*OpenCodeProvider)(nil)
}

func TestClaudeCodeProvider_Metadata(t *testing.T) {
	enabled := true
	provider, err := NewClaudeCodeProvider(ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &enabled,
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create Claude Code provider: %v", err)
	}

	meta := provider.Metadata()
	if meta.Type != ProviderTypeClaudeCode {
		t.Errorf("Expected type %s, got %s", ProviderTypeClaudeCode, meta.Type)
	}
	if meta.DisplayName != "Claude Code" {
		t.Errorf("Expected display name 'Claude Code', got %s", meta.DisplayName)
	}
	if !meta.Features.SupportsStreamJSON {
		t.Error("Expected SupportsStreamJSON to be true")
	}
	if !meta.Features.SupportsResume {
		t.Error("Expected SupportsResume to be true")
	}
}

func TestClaudeCodeProvider_BuildCLIArgs(t *testing.T) {
	enabled := true
	provider, err := NewClaudeCodeProvider(ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &enabled,
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	opts := &ProviderSessionOptions{
		WorkDir:          "/tmp/test",
		PermissionMode:   "bypass-permissions",
		AllowedTools:     []string{"bash", "read"},
		BaseSystemPrompt: "You are a helpful assistant",
	}

	args := provider.BuildCLIArgs("test-session-id", opts)

	// Verify required arguments
	assertContains(t, args, "--print")
	assertContains(t, args, "--verbose")
	assertContains(t, args, "--output-format")
	assertContains(t, args, "stream-json")
	assertContains(t, args, "--input-format")
	assertContains(t, args, "stream-json")
	assertContains(t, args, "--session-id")
	assertContains(t, args, "test-session-id")
	assertContains(t, args, "--permission-mode")
	assertContains(t, args, "bypass-permissions")
	assertContains(t, args, "--allowed-tools")
	assertContains(t, args, "--append-system-prompt")
}

func TestClaudeCodeProvider_BuildInputMessage(t *testing.T) {
	enabled := true
	provider, err := NewClaudeCodeProvider(ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &enabled,
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	msg, err := provider.BuildInputMessage("Hello, world!", "You are a code reviewer")
	if err != nil {
		t.Fatalf("Failed to build input message: %v", err)
	}

	msgType, ok := msg["type"].(string)
	if !ok || msgType != "user" {
		t.Errorf("Expected type 'user', got %v", msg["type"])
	}

	message, ok := msg["message"].(map[string]any)
	if !ok {
		t.Fatal("Expected message to be a map")
	}

	content, ok := message["content"].([]map[string]any)
	if !ok || len(content) == 0 {
		t.Fatal("Expected content to be a non-empty slice")
	}

	// Task prompt should be prepended with XML tags and CDATA
	text := content[0]["text"].(string)
	if text == "" {
		t.Error("Expected non-empty text content")
	}
	expected := "<context>\n<![CDATA[\nYou are a code reviewer\n]]>\n</context>\n\n<user_query>\n<![CDATA[\nHello, world!\n]]>\n</user_query>"
	if text != expected {
		t.Errorf("Expected structured XML prompt, got:\n%s", text)
	}
}

func TestClaudeCodeProvider_BuildInputMessage_NoInstructions(t *testing.T) {
	enabled := true
	provider, _ := NewClaudeCodeProvider(ProviderConfig{Type: ProviderTypeClaudeCode, Enabled: &enabled}, nil)
	msg, _ := provider.BuildInputMessage("Hello!", "")
	content := msg["message"].(map[string]any)["content"].([]map[string]any)
	text := content[0]["text"].(string)
	expected := "Hello!"
	if text != expected {
		t.Errorf("Expected raw prompt when no instructions, got:\n%s", text)
	}
}

func TestClaudeCodeProvider_ParseEvent(t *testing.T) {
	enabled := true
	provider, err := NewClaudeCodeProvider(ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &enabled,
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		name        string
		line        string
		wantType    ProviderEventType
		wantContent string
	}{
		{
			name:        "result event",
			line:        `{"type":"result","result":"Task completed","duration_ms":1000}`,
			wantType:    EventTypeResult,
			wantContent: "Task completed",
		},
		{
			name:        "error event",
			line:        `{"type":"error","error":"Something went wrong"}`,
			wantType:    EventTypeError,
			wantContent: "Something went wrong",
		},
		{
			name:        "thinking event",
			line:        `{"type":"thinking","content":[{"type":"text","text":"Thinking..."}]}`,
			wantType:    EventTypeThinking,
			wantContent: "Thinking...",
		},
		{
			name:     "tool_use event",
			line:     `{"type":"tool_use","name":"bash","content":[{"type":"tool_use","id":"tool-123"}]}`,
			wantType: EventTypeToolUse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := provider.ParseEvent(tt.line)
			if err != nil {
				t.Fatalf("ParseEvent failed: %v", err)
			}
			if len(events) == 0 {
				t.Fatal("Expected at least one event")
			}
			event := events[0]
			if event.Type != tt.wantType {
				t.Errorf("Expected type %s, got %s", tt.wantType, event.Type)
			}
			if tt.wantContent != "" && event.Content != tt.wantContent {
				t.Errorf("Expected content %q, got %q", tt.wantContent, event.Content)
			}
		})
	}
}

func TestClaudeCodeProvider_DetectTurnEnd(t *testing.T) {
	enabled := true
	provider, _ := NewClaudeCodeProvider(ProviderConfig{Type: ProviderTypeClaudeCode, Enabled: &enabled}, nil)

	tests := []struct {
		event *ProviderEvent
		want  bool
	}{
		{&ProviderEvent{Type: EventTypeResult}, true},
		{&ProviderEvent{Type: EventTypeError}, true},
		{&ProviderEvent{Type: EventTypeAnswer}, false},
		{&ProviderEvent{Type: EventTypeToolUse}, false},
		{nil, false},
	}

	for _, tt := range tests {
		got := provider.DetectTurnEnd(tt.event)
		if got != tt.want {
			t.Errorf("DetectTurnEnd(%v) = %v, want %v", tt.event, got, tt.want)
		}
	}
}

func TestOpenCodeProvider_Metadata(t *testing.T) {
	enabled := true
	provider, err := NewOpenCodeProvider(ProviderConfig{
		Type:       ProviderTypeOpenCode,
		Enabled:    &enabled,
		BinaryPath: "/usr/local/bin/opencode",
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create OpenCode provider: %v", err)
	}

	meta := provider.Metadata()
	if meta.Type != ProviderTypeOpenCode {
		t.Errorf("Expected type %s, got %s", ProviderTypeOpenCode, meta.Type)
	}
	if !meta.Features.SupportsSSE {
		t.Error("Expected SupportsSSE to be true")
	}
	if !meta.Features.SupportsHTTPAPI {
		t.Error("Expected SupportsHTTPAPI to be true")
	}
}

func TestOpenCodeProvider_BuildCLIArgs(t *testing.T) {
	enabled := true
	provider, err := NewOpenCodeProvider(ProviderConfig{
		Type:       ProviderTypeOpenCode,
		Enabled:    &enabled,
		BinaryPath: "/usr/local/bin/opencode",
		OpenCode: &OpenCodeConfig{
			Provider: "anthropic",
			Model:    "claude-3-5-sonnet",
		},
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	opts := &ProviderSessionOptions{
		WorkDir: "/tmp/test",
	}

	args := provider.BuildCLIArgs("test-session", opts)

	assertContains(t, args, "run")
	assertContains(t, args, "--format")
	assertContains(t, args, "json")
	assertContains(t, args, "--session")
	assertContains(t, args, "ses_test-session")
	assertContains(t, args, "--provider")
	assertContains(t, args, "anthropic")
}

func TestOpenCodeProvider_DetectTurnEnd(t *testing.T) {
	enabled := true
	provider, _ := NewOpenCodeProvider(ProviderConfig{Type: ProviderTypeOpenCode, Enabled: &enabled, BinaryPath: "/usr/local/bin/opencode"}, nil)

	tests := []struct {
		event *ProviderEvent
		want  bool
	}{
		{&ProviderEvent{Type: EventTypeAnswer, Content: "some answer"}, false},
		{&ProviderEvent{Type: EventTypeResult}, true},
		{&ProviderEvent{Type: EventTypeError}, true},
	}

	for _, tt := range tests {
		got := provider.DetectTurnEnd(tt.event)
		if got != tt.want {
			t.Errorf("DetectTurnEnd(%s) = %v, want %v", tt.event.Type, got, tt.want)
		}
	}
}

func TestOpenCodeProvider_ParseEvent(t *testing.T) {
	enabled := true
	provider, err := NewOpenCodeProvider(ProviderConfig{
		Type:       ProviderTypeOpenCode,
		Enabled:    &enabled,
		BinaryPath: "/usr/local/bin/opencode",
	}, nil)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	tests := []struct {
		name     string
		line     string
		wantType ProviderEventType
	}{
		{
			name:     "text part",
			line:     `{"parts":[{"type":"text","text":"Hello"}]}`,
			wantType: EventTypeAnswer,
		},
		{
			name:     "reasoning part",
			line:     `{"parts":[{"type":"reasoning","text":"Thinking..."}]}`,
			wantType: EventTypeThinking,
		},
		{
			name:     "tool use part",
			line:     `{"parts":[{"type":"tool","name":"bash","id":"tool-1"}]}`,
			wantType: EventTypeToolUse,
		},
		{
			name:     "tool result part",
			line:     `{"parts":[{"type":"tool","name":"bash","id":"tool-1","output":"done"}]}`,
			wantType: EventTypeToolResult,
		},
		{
			name:     "step-start part",
			line:     `{"parts":[{"type":"step-start","step_number":1,"total_steps":3}]}`,
			wantType: EventTypeStepStart,
		},
		{
			name:     "step-finish part",
			line:     `{"parts":[{"type":"step-finish","step_number":3,"total_steps":3}]}`,
			wantType: EventTypeStepFinish,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := provider.ParseEvent(tt.line)
			if err != nil {
				t.Fatalf("ParseEvent failed: %v", err)
			}
			if len(events) == 0 {
				t.Fatal("Expected at least one event")
			}
			found := false
			for _, event := range events {
				if event.Type == tt.wantType {
					found = true
					break
				}
			}
			if !found {
				var types []ProviderEventType
				for _, e := range events {
					types = append(types, e.Type)
				}
				t.Errorf("Expected type %s in %v", tt.wantType, types)
			}
		})
	}
}

func TestProviderFactory(t *testing.T) {
	factory := NewProviderFactory(nil)

	// Test registration
	types := factory.ListRegistered()
	if len(types) < 2 {
		t.Errorf("Expected at least 2 registered providers, got %d", len(types))
	}

	enabled := true
	// Test create Claude Code provider
	ccProvider, err := factory.Create(ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &enabled,
	})
	if err != nil {
		t.Fatalf("Failed to create Claude Code provider: %v", err)
	}
	if ccProvider.Name() != string(ProviderTypeClaudeCode) {
		t.Errorf("Expected name %s, got %s", ProviderTypeClaudeCode, ccProvider.Name())
	}

	// Test create OpenCode provider
	ocProvider, err := factory.Create(ProviderConfig{
		Type:       ProviderTypeOpenCode,
		Enabled:    &enabled,
		BinaryPath: "/usr/local/bin/opencode",
	})
	if err != nil {
		t.Fatalf("Failed to create OpenCode provider: %v", err)
	}
	if ocProvider.Name() != string(ProviderTypeOpenCode) {
		t.Errorf("Expected name %s, got %s", ProviderTypeOpenCode, ocProvider.Name())
	}

	disabled := false
	_, err = factory.Create(ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &disabled,
	})
	if err == nil {
		t.Error("Expected error for disabled provider")
	}

	enabled = true
	_, err = factory.Create(ProviderConfig{
		Type:    "unknown",
		Enabled: &enabled,
	})
	if err == nil {
		t.Error("Expected error for unknown provider")
	}
}

func TestProviderRegistry(t *testing.T) {
	factory := NewProviderFactory(nil)
	registry := NewProviderRegistry(factory, nil)

	// Test get or create
	provider1, err := registry.GetOrCreate(ProviderTypeClaudeCode)
	if err != nil {
		t.Fatalf("Failed to get provider: %v", err)
	}

	// Should return same instance from cache
	provider2, err := registry.GetOrCreate(ProviderTypeClaudeCode)
	if err != nil {
		t.Fatalf("Failed to get cached provider: %v", err)
	}

	if provider1 != provider2 {
		t.Error("Expected same provider instance from cache")
	}

	// Test list
	types := registry.List()
	if len(types) != 1 {
		t.Errorf("Expected 1 cached provider, got %d", len(types))
	}

	// Test remove
	registry.Remove(ProviderTypeClaudeCode)
	types = registry.List()
	if len(types) != 0 {
		t.Errorf("Expected 0 cached providers after remove, got %d", len(types))
	}
}

func TestProviderEvent_ToEventWithMeta(t *testing.T) {
	event := &ProviderEvent{
		Type:    EventTypeAnswer,
		Content: "Hello, world!",
		Status:  "success",
		Metadata: &ProviderEventMeta{
			DurationMs:   1000,
			InputTokens:  100,
			OutputTokens: 50,
			TotalCostUSD: 0.01,
		},
	}

	eventWithMeta := event.ToEventWithMeta()

	if eventWithMeta.EventType != string(EventTypeAnswer) {
		t.Errorf("Expected event type %s, got %s", EventTypeAnswer, eventWithMeta.EventType)
	}
	if eventWithMeta.EventData != "Hello, world!" {
		t.Errorf("Expected event data 'Hello, world!', got %s", eventWithMeta.EventData)
	}
	if eventWithMeta.Meta == nil {
		t.Fatal("Expected non-nil meta")
	}
	if eventWithMeta.Meta.DurationMs != 1000 {
		t.Errorf("Expected duration 1000, got %d", eventWithMeta.Meta.DurationMs)
	}
}

func TestMergeProviderConfigs(t *testing.T) {
	enabled := true
	base := ProviderConfig{
		Type:         ProviderTypeClaudeCode,
		Enabled:      &enabled,
		DefaultModel: "claude-3-5-sonnet",
		AllowedTools: []string{"bash"},
		ExtraEnv:     map[string]string{"KEY1": "VALUE1"},
	}

	overlay := ProviderConfig{
		Type:         ProviderTypeClaudeCode,
		DefaultModel: "claude-3-opus",                     // Override
		AllowedTools: []string{"read", "write"},           // Override
		ExtraEnv:     map[string]string{"KEY2": "VALUE2"}, // Merge
		Timeout:      60000000000,                         // 1 minute
	}

	result := MergeProviderConfigs(base, overlay)

	if result.DefaultModel != "claude-3-opus" {
		t.Errorf("Expected model to be overridden, got %s", result.DefaultModel)
	}
	if len(result.AllowedTools) != 2 {
		t.Errorf("Expected 2 allowed tools, got %d", len(result.AllowedTools))
	}
	if len(result.ExtraEnv) != 2 {
		t.Errorf("Expected 2 env vars, got %d", len(result.ExtraEnv))
	}
	if result.Timeout != 60000000000 {
		t.Errorf("Expected timeout from overlay")
	}
}

func TestMergeStringSlices(t *testing.T) {
	tests := []struct {
		name     string
		base     []string
		overlay  []string
		expected int
	}{
		{"both empty", nil, nil, 0},
		{"base only", []string{"a", "b"}, nil, 2},
		{"overlay only", nil, []string{"c", "d"}, 2},
		{"both with overlap", []string{"a", "b"}, []string{"b", "c"}, 3},
		{"no overlap", []string{"a"}, []string{"b"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mergeStringSlices(tt.base, tt.overlay)
			if len(result) != tt.expected {
				t.Errorf("Expected %d items, got %d: %v", tt.expected, len(result), result)
			}
		})
	}
}

func TestProviderType_Valid(t *testing.T) {
	tests := []struct {
		pt       ProviderType
		expected bool
	}{
		{ProviderTypeClaudeCode, true},
		{ProviderTypeOpenCode, true},
		{ProviderType("invalid"), false},
		{ProviderType(""), false},
		{ProviderType("claude"), false},
	}

	for _, tt := range tests {
		t.Run(string(tt.pt), func(t *testing.T) {
			if got := tt.pt.Valid(); got != tt.expected {
				t.Errorf("ProviderType(%q).Valid() = %v, want %v", tt.pt, got, tt.expected)
			}
		})
	}
}

func TestProviderConfig_Validate(t *testing.T) {
	enabledTrue := true
	tests := []struct {
		name    string
		cfg     ProviderConfig
		wantErr bool
	}{
		{
			name:    "valid claude-code config",
			cfg:     ProviderConfig{Type: ProviderTypeClaudeCode, Enabled: &enabledTrue},
			wantErr: false,
		},
		{
			name:    "valid opencode config",
			cfg:     ProviderConfig{Type: ProviderTypeOpenCode, Enabled: &enabledTrue},
			wantErr: false,
		},
		{
			name:    "empty type",
			cfg:     ProviderConfig{Type: "", Enabled: &enabledTrue},
			wantErr: true,
		},
		{
			name:    "invalid type",
			cfg:     ProviderConfig{Type: ProviderType("invalid"), Enabled: &enabledTrue},
			wantErr: true,
		},
		{
			name: "negative port",
			cfg: ProviderConfig{
				Type:    ProviderTypeOpenCode,
				Enabled: &enabledTrue,
				OpenCode: &OpenCodeConfig{
					Port: -1,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMergeProviderConfigs_ExplicitDisable(t *testing.T) {
	enabled := true
	base := ProviderConfig{
		Type:         ProviderTypeClaudeCode,
		Enabled:      &enabled,
		DefaultModel: "claude-3-5-sonnet",
	}

	// Test: overlay with ExplicitDisable=true should disable
	overlay := ProviderConfig{
		Type:            ProviderTypeClaudeCode,
		ExplicitDisable: true,
	}
	result := MergeProviderConfigs(base, overlay)
	if BoolValue(result.Enabled, true) {
		t.Error("Expected Enabled=false when ExplicitDisable=true in overlay")
	}

	// Test: overlay without ExplicitDisable should inherit base.Enabled
	overlay2 := ProviderConfig{
		Type: ProviderTypeClaudeCode,
	}
	result2 := MergeProviderConfigs(base, overlay2)
	if !BoolValue(result2.Enabled, false) {
		t.Error("Expected Enabled=true inherited from base when overlay has no ExplicitDisable")
	}

	enabledTrue := true
	overlay3 := ProviderConfig{
		Type:    ProviderTypeClaudeCode,
		Enabled: &enabledTrue,
	}
	result3 := MergeProviderConfigs(base, overlay3)
	if !BoolValue(result3.Enabled, false) {
		t.Error("Expected Enabled=true when overlay.Enabled=true")
	}
}

// assertContains checks if a string slice contains a specific string.
func assertContains(t *testing.T, slice []string, want string) {
	t.Helper()
	for _, s := range slice {
		if s == want {
			return
		}
	}
	t.Errorf("slice %v does not contain %q", slice, want)
}
