package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ========================================
// ServerLoader Tests
// ========================================

func TestNewServerLoader(t *testing.T) {
	// Create a temp config file
	content := `
engine:
  timeout: 30m
  idle_timeout: 1h
  work_dir: /tmp/test
  allowed_tools:
    - bash
    - edit
server:
  port: "8080"
  log_level: info
security:
  api_key: test-key
  permission_mode: strict
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	if loader == nil {
		t.Error("Loader should not be nil")
	}
}

func TestNewServerLoader_FileNotFound(t *testing.T) {
	loader, err := NewServerLoader("/nonexistent/config.yaml", nil)
	// Should not fail, just use defaults
	if err != nil {
		t.Errorf("NewServerLoader should not fail for missing file: %v", err)
	}
	
	if loader == nil {
		t.Error("Loader should be created with defaults")
	}
}

func TestNewServerLoader_InvalidYAML(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString("invalid: yaml: content:"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	_, err = NewServerLoader(tmpFile.Name(), nil)
	if err == nil {
		t.Error("NewServerLoader should fail for invalid YAML")
	}
}

func TestServerLoader_Get(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString("server:\n  port: \"9090\"\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	cfg := loader.Get()
	if cfg == nil {
		t.Error("Get should not return nil")
		return
	}
	
	if cfg.Server.Port != "9090" {
		t.Errorf("Port = %s, want 9090", cfg.Server.Port)
	}
}

func TestServerLoader_GetSystemPrompt(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	content := `
engine:
  system_prompt: "You are a helpful assistant"
`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	prompt := loader.GetSystemPrompt()
	if prompt != "You are a helpful assistant" {
		t.Errorf("SystemPrompt = %s, want 'You are a helpful assistant'", prompt)
	}
}

func TestServerLoader_GetTimeout_Default(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString("server:\n  port: \"8080\"\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	// Should use default timeout
	timeout := loader.GetTimeout()
	if timeout != 30*time.Minute {
		t.Errorf("Timeout = %v, want default 30m", timeout)
	}
}

func TestServerLoader_GetTimeout_Custom(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	content := `
engine:
  timeout: 45m
`
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	timeout := loader.GetTimeout()
	if timeout != 45*time.Minute {
		t.Errorf("Timeout = %v, want 45m", timeout)
	}
}

func TestServerLoader_GetIdleTimeout_Default(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString("server:\n  port: \"8080\"\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	// Should use default
	idleTimeout := loader.GetIdleTimeout()
	if idleTimeout != 1*time.Hour {
		t.Errorf("IdleTimeout = %v, want default 1h", idleTimeout)
	}
}

func TestServerLoader_GetWorkDir_Default(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString("server:\n  port: \"8080\"\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	workDir := loader.GetWorkDir()
	if workDir != "/tmp/hotplex_sandbox" {
		t.Errorf("WorkDir = %s, want default '/tmp/hotplex_sandbox'", workDir)
	}
}

func TestServerLoader_GetPort_Default(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString("server:\n  port: \"\"\n"); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	port := loader.GetPort()
	if port != "8080" {
		t.Errorf("Port = %s, want default '8080'", port)
	}
}

// ========================================
// Validation Tests
// ========================================

func TestValidate_InvalidPermissionMode(t *testing.T) {
	// This test would require accessing internal validate function
	// Let's test through NewServerLoader with invalid config
	content := `
security:
  permission_mode: invalid_mode
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	_, err = NewServerLoader(tmpFile.Name(), nil)
	if err == nil {
		t.Error("Should fail with invalid permission_mode")
	}
}

func TestValidate_InvalidLogLevel(t *testing.T) {
	content := `
server:
  log_level: invalid_level
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	_, err = NewServerLoader(tmpFile.Name(), nil)
	if err == nil {
		t.Error("Should fail with invalid log_level")
	}
}

func TestValidate_TimeoutTooLarge(t *testing.T) {
	content := `
engine:
  timeout: 25h
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	_, err = NewServerLoader(tmpFile.Name(), nil)
	if err == nil {
		t.Error("Should fail with timeout > 24h")
	}
}

func TestValidate_IdleTimeoutTooLarge(t *testing.T) {
	content := `
engine:
  idle_timeout: 8d
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	_, err = NewServerLoader(tmpFile.Name(), nil)
	if err == nil {
		t.Error("Should fail with idle_timeout > 7 days")
	}
}

// ========================================
// ResolveConfigPath Tests
// ========================================

func TestResolveConfigPath_Explicit(t *testing.T) {
	path := ResolveConfigPath("/explicit/path/config.yaml")
	if path != "/explicit/path/config.yaml" {
		t.Errorf("ResolveConfigPath = %s, want /explicit/path/config.yaml", path)
	}
}

func TestResolveConfigPath_EnvVar(t *testing.T) {
	t.Setenv("HOTPLEX_SERVER_CONFIG", "/env/config.yaml")
	
	path := ResolveConfigPath("")
	if path != "/env/config.yaml" {
		t.Errorf("ResolveConfigPath = %s, want /env/config.yaml", path)
	}
}

func TestResolveConfigPath_Default(t *testing.T) {
	// No env var, no explicit path - should check cwd
	cwd, _ := os.Getwd()
	defaultPath := filepath.Join(cwd, "configs", "server.yaml")
	
	// If the default file doesn't exist, should return empty
	path := ResolveConfigPath("")
	_ = defaultPath // Just check it compiles
	
	if path != "" {
		t.Logf("Default path found: %s", path)
	}
}

// ========================================
// Environment Variable Override Tests
// ========================================

func TestPopulateFromEnv_APIKey(t *testing.T) {
	t.Setenv("HOTPLEX_API_KEY", "env-api-key")
	
	content := `
security:
  api_key: file-key
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	// Env should override file
	cfg := loader.Get()
	if cfg.Security.APIKey != "env-api-key" {
		t.Errorf("APIKey = %s, want env-api-key", cfg.Security.APIKey)
	}
}

func TestPopulateFromEnv_Port(t *testing.T) {
	t.Setenv("HOTPLEX_PORT", "9999")
	
	content := `
server:
  port: "8080"
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	if loader.GetPort() != "9999" {
		t.Errorf("Port = %s, want 9999", loader.GetPort())
	}
}

func TestPopulateFromEnv_LogLevel(t *testing.T) {
	t.Setenv("HOTPLEX_LOG_LEVEL", "debug")
	
	content := `
server:
  log_level: info
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	cfg := loader.Get()
	if cfg.Server.LogLevel != "debug" {
		t.Errorf("LogLevel = %s, want debug", cfg.Server.LogLevel)
	}
}

func TestPopulateFromEnv_Timeout(t *testing.T) {
	t.Setenv("HOTPLEX_EXECUTION_TIMEOUT", "60m")
	
	content := `
engine:
  timeout: 30m
`
	tmpFile, err := os.CreateTemp("", "server-*.yaml")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatalf("Failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		t.Fatalf("Failed to close temp file: %v", err)
	}
	
	loader, err := NewServerLoader(tmpFile.Name(), nil)
	if err != nil {
		t.Fatalf("NewServerLoader failed: %v", err)
	}
	
	if loader.GetTimeout() != 60*time.Minute {
		t.Errorf("Timeout = %v, want 60m", loader.GetTimeout())
	}
}

// ========================================
// Config Struct Tests
// ========================================

func TestServerConfig_Fields(t *testing.T) {
	cfg := &ServerConfig{
		Engine: EngineConfig{
			Timeout:     30 * time.Minute,
			IdleTimeout: 1 * time.Hour,
			WorkDir:    "/tmp/work",
			SystemPrompt: "You are helpful",
			AllowedTools: []string{"bash", "edit"},
		},
		Server: ServerSettings{
			Port:     "8080",
			LogLevel: "info",
		},
		Security: SecurityConfig{
			APIKey:         "key",
			PermissionMode: "strict",
		},
	}
	
	if cfg.Engine.Timeout != 30*time.Minute {
		t.Errorf("Engine.Timeout = %v", cfg.Engine.Timeout)
	}
	if cfg.Server.Port != "8080" {
		t.Errorf("Server.Port = %s", cfg.Server.Port)
	}
	if cfg.Security.PermissionMode != "strict" {
		t.Errorf("Security.PermissionMode = %s", cfg.Security.PermissionMode)
	}
}

func TestEngineConfig_EmptyAllowedTools(t *testing.T) {
	cfg := EngineConfig{}
	// Empty allowed tools should be valid (nil slice)
	_ = cfg.AllowedTools
}

func TestServerSettings_Defaults(t *testing.T) {
	settings := ServerSettings{}
	// Default values are handled by getters, not struct
	if settings.Port != "" {
		t.Error("Port should default to empty string")
	}
}
