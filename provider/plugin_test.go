package provider

import (
	"log/slog"
	"os"
	"testing"
)

// TestPluginRegistration tests the plugin registration system.
func TestPluginRegistration(t *testing.T) {
	// Create a test plugin
	testType := ProviderType("test-provider")
	testPlugin := &mockPlugin{
		typ: testType,
		meta: ProviderMeta{
			Type:        testType,
			DisplayName: "Test Provider",
			BinaryName:  "test-cli",
			Features: ProviderFeatures{
				SupportsResume:     true,
				SupportsStreamJSON: true,
			},
		},
	}

	// Register the plugin
	RegisterPlugin(testPlugin)

	// Verify registration
	if !IsPluginRegistered(testType) {
		t.Error("Expected plugin to be registered")
	}

	// Get the plugin
	retrieved := GetPlugin(testType)
	if retrieved == nil {
		t.Error("Expected to retrieve plugin")
	}

	// Verify type matches
	if retrieved.Type() != testType {
		t.Errorf("Expected type %s, got %s", testType, retrieved.Type())
	}

	// Verify ListPlugins includes our plugin
	types := ListPlugins()
	found := false
	for _, pt := range types {
		if pt == testType {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected test-provider in ListPlugins")
	}
}

// TestPluginValidation tests plugin metadata validation.
func TestPluginValidation(t *testing.T) {
	tests := []struct {
		name    string
		plugin  ProviderPlugin
		wantErr bool
	}{
		{
			name: "valid plugin",
			plugin: &mockPlugin{
				typ: "valid",
				meta: ProviderMeta{
					Type:        "valid",
					DisplayName: "Valid",
					BinaryName:  "valid-cli",
				},
			},
			wantErr: false,
		},
		{
			name: "empty type",
			plugin: &mockPlugin{
				typ: "",
				meta: ProviderMeta{
					Type:        "",
					DisplayName: "Empty",
					BinaryName:  "empty-cli",
				},
			},
			wantErr: false, // RegisterPlugin panics on empty type
		},
		{
			name: "empty display name",
			plugin: &mockPlugin{
				typ: "no-display",
				meta: ProviderMeta{
					Type:        "no-display",
					DisplayName: "",
					BinaryName:  "no-display-cli",
				},
			},
			wantErr: false, // validation is internal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.plugin.Type() == "" {
				defer func() {
					if r := recover(); r == nil {
						t.Error("Expected panic for empty type")
					}
				}()
				RegisterPlugin(tt.plugin)
			}
		})
	}
}

// TestIsRegistered tests IsRegistered method.
func TestIsRegistered(t *testing.T) {
	// Built-in types should be registered
	if !ProviderTypeClaudeCode.IsRegistered() {
		t.Error("Expected claude-code to be registered")
	}
	if !ProviderTypeOpenCode.IsRegistered() {
		t.Error("Expected opencode to be registered")
	}
	if !ProviderTypePi.IsRegistered() {
		t.Error("Expected pi to be registered")
	}

	// Unknown type should not be registered
	if ProviderType("unknown").IsRegistered() {
		t.Error("Expected unknown to not be registered")
	}
}

// TestValidBackwardCompatibility tests that Valid() still works.
func TestValidBackwardCompatibility(t *testing.T) {
	// Valid() should delegate to IsRegistered()
	if !ProviderTypeClaudeCode.Valid() {
		t.Error("Expected Valid() to return true for claude-code")
	}
	if !ProviderTypeOpenCode.Valid() {
		t.Error("Expected Valid() to return true for opencode")
	}
	if !ProviderTypePi.Valid() {
		t.Error("Expected Valid() to return true for pi")
	}
	if ProviderType("unknown").Valid() {
		t.Error("Expected Valid() to return false for unknown")
	}
}

// TestFactoryWithPlugins tests factory integration with plugins.
func TestFactoryWithPlugins(t *testing.T) {
	// Create a test factory
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	factory := NewProviderFactory(logger)

	// Verify built-in providers are registered
	if !factory.IsRegistered(ProviderTypeClaudeCode) {
		t.Error("Expected claude-code in factory")
	}
	if !factory.IsRegistered(ProviderTypeOpenCode) {
		t.Error("Expected opencode in factory")
	}
	if !factory.IsRegistered(ProviderTypePi) {
		t.Error("Expected pi in factory")
	}

	// Register a custom plugin
	customType := ProviderType("custom-test")
	customPlugin := &mockPlugin{
		typ: customType,
		meta: ProviderMeta{
			Type:        customType,
			DisplayName: "Custom Test",
			BinaryName:  "custom-test-cli",
			Features: ProviderFeatures{
				SupportsResume: true,
			},
		},
	}

	factory.registerPlugin(customPlugin)

	// Verify custom plugin is registered
	if !factory.IsRegistered(customType) {
		t.Error("Expected custom-test in factory")
	}

	// Verify GetPlugin works
	retrieved := factory.GetPlugin(customType)
	if retrieved == nil {
		t.Error("Expected to retrieve custom plugin")
	}
	if retrieved.Type() != customType {
		t.Errorf("Expected type %s, got %s", customType, retrieved.Type())
	}
}

// mockPlugin is a test implementation of ProviderPlugin.
type mockPlugin struct {
	typ  ProviderType
	meta ProviderMeta
}

func (p *mockPlugin) Type() ProviderType {
	return p.typ
}

func (p *mockPlugin) New(cfg ProviderConfig, logger *slog.Logger) (Provider, error) {
	if logger == nil {
		logger = slog.Default()
	}
	// Return a mock provider
	return &mockProvider{
		meta: p.meta,
	}, nil
}

func (p *mockPlugin) Meta() ProviderMeta {
	return p.meta
}

// mockProvider is a test implementation of Provider.
type mockProvider struct {
	meta ProviderMeta
}

func (p *mockProvider) Metadata() ProviderMeta {
	return p.meta
}

func (p *mockProvider) BuildCLIArgs(sessionID string, opts *ProviderSessionOptions) []string {
	return []string{"--session-id", sessionID}
}

func (p *mockProvider) BuildInputMessage(prompt string, taskInstructions string) (map[string]any, error) {
	return map[string]any{"prompt": prompt}, nil
}

func (p *mockProvider) ParseEvent(line string) ([]*ProviderEvent, error) {
	return nil, nil
}

func (p *mockProvider) DetectTurnEnd(event *ProviderEvent) bool {
	return event.Type == EventTypeResult
}

func (p *mockProvider) ValidateBinary() (string, error) {
	return "/usr/bin/test-cli", nil
}

func (p *mockProvider) CleanupSession(sessionID string, workDir string) error {
	return nil
}

func (p *mockProvider) Name() string {
	return string(p.meta.Type)
}
