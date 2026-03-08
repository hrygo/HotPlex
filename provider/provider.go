package provider

import (
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// PtrBool returns a pointer to the given bool value.
func PtrBool(b bool) *bool {
	return &b
}

// ProviderType defines the type of AI CLI provider.
type ProviderType string

const (
	ProviderTypeClaudeCode ProviderType = "claude-code"
	ProviderTypeOpenCode   ProviderType = "opencode"
	ProviderTypePi         ProviderType = "pi"
)

// Valid checks if the provider type is registered in the global factory.
// This method delegates to IsRegistered for consistency with the plugin system.
// Deprecated: Use IsRegistered() instead for clearer semantics.
func (t ProviderType) Valid() bool {
	return t.IsRegistered()
}

// IsRegistered checks if the provider type is registered in the global factory.
// This is the preferred method for checking provider availability.
// It checks both built-in providers and registered plugins.
func (t ProviderType) IsRegistered() bool {
	// First check built-in types for fast path
	switch t {
	case ProviderTypeClaudeCode, ProviderTypeOpenCode, ProviderTypePi:
		return true
	}
	// Then check plugin registry
	return IsPluginRegistered(t)
}

// Provider defines the interface for AI CLI agent providers.
// Each provider (Claude Code, OpenCode) implements this interface to handle
// its specific CLI protocol, argument construction, and event parsing.
//
// The interface follows the Strategy Pattern, allowing HotPlex Engine to
// switch between different AI CLI tools without modifying core logic.
type Provider interface {
	// Metadata returns the provider's identity and capabilities.
	Metadata() ProviderMeta

	// BuildCLIArgs constructs the command-line arguments for starting the CLI process.
	// The sessionID is the internal SDK identifier, providerSessionID is the
	// provider-specific persistent session identifier (e.g., Claude's --session-id).
	BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string

	// BuildInputMessage constructs the stdin message payload for sending user input.
	// This handles provider-specific input formatting (e.g., stream-json for Claude).
	BuildInputMessage(prompt string, taskInstructions string) (map[string]any, error)

	// ParseEvent parses a raw output line into one or more normalized ProviderEvents.
	// Returns empty slice if the line should be ignored (e.g., system messages).
	// Returns an error if parsing fails critically.
	ParseEvent(line string) ([]*ProviderEvent, error)

	// DetectTurnEnd checks if the event indicates the end of a turn.
	// Different providers signal turn completion differently:
	// - Claude Code: type="result"
	// - OpenCode: step-finish or specific completion marker
	DetectTurnEnd(event *ProviderEvent) bool

	// ValidateBinary checks if the CLI binary is available and returns its path.
	ValidateBinary() (string, error)

	// CleanupSession cleans up provider-specific session files from disk (e.g. for /reset).
	CleanupSession(providerSessionID string, workDir string) error

	// Name returns the provider name for logging and identification.
	Name() string
}

// ProviderMeta contains metadata about a provider.
type ProviderMeta struct {
	Type        ProviderType // Provider type identifier
	DisplayName string       // Human-readable name (e.g., "Claude Code")
	BinaryName  string       // CLI binary name (e.g., "claude", "opencode")
	InstallHint string       // Installation hint (e.g., "npm install -g @anthropic-ai/claude-code")
	Version     string       // CLI version (if available)
	Features    ProviderFeatures
}

// ProviderFeatures describes the capabilities of a provider.
type ProviderFeatures struct {
	SupportsResume             bool // Can resume existing sessions (e.g., --resume)
	SupportsStreamJSON         bool // Supports stream-json input/output format
	SupportsSSE                bool // Supports Server-Sent Events output
	SupportsHTTPAPI            bool // Has HTTP API mode
	SupportsSessionID          bool // Supports explicit session ID assignment
	SupportsPermissions        bool // Supports permission modes
	MultiTurnReady             bool // Can handle multiple turns in one session
	RequiresInitialPromptAsArg bool // Requires first prompt to be passed via CLI args instead of stdin
}

// ProviderSessionOptions configures a provider session.
// This is the provider-specific subset of session configuration,
// extracted from the global EngineOptions and per-request Config.
type ProviderSessionOptions struct {
	// Working directory for the CLI process
	WorkDir string

	// Permission mode (e.g., "bypass-permissions", "auto-accept")
	PermissionMode string

	// DangerouslySkipPermissions bypasses all permission checks
	DangerouslySkipPermissions bool

	// Tool restrictions
	AllowedTools    []string
	DisallowedTools []string

	// System prompts
	BaseSystemPrompt string // Engine-level foundational prompt
	TaskInstructions string // Per-task instructions (persisted per session)
	InitialPrompt    string // First prompt for cold start (sent as CLI arg if needed)

	// Session management
	SessionID         string // Internal SDK session ID
	ProviderSessionID string // Provider-specific persistent session ID
	ResumeSession     bool   // Whether to resume an existing session

	// Provider-specific flags
	// Claude Code specific
	Model string // Model override (e.g., "claude-3-5-sonnet")

	// OpenCode specific
	PlanMode bool // Use planning mode instead of build mode
	Port     int  // Port for HTTP API mode (if applicable)
}

// ProviderConfig defines the configuration for a specific provider instance.
// This is used in the layered configuration system.
type ProviderConfig struct {
	// Type identifies the provider (required)
	Type ProviderType `json:"type" koanf:"type"`

	// Enabled controls whether this provider is available
	Enabled *bool `json:"enabled" koanf:"enabled"`

	// ExplicitDisable explicitly disables the provider, overriding base config's Enabled=true.
	// This is needed because bool zero value (false) cannot be distinguished from "not set"
	// in config merging. Use this when you want to disable a provider in overlay config.
	ExplicitDisable bool `json:"explicit_disable,omitempty" koanf:"explicit_disable"`

	// BinaryPath overrides the default binary lookup path
	BinaryPath string `json:"binary_path,omitempty" koanf:"binary_path"`

	// DefaultModel is the default model to use
	DefaultModel string `json:"default_model,omitempty" koanf:"default_model"`

	// DefaultPermissionMode is the default permission mode
	DefaultPermissionMode string `json:"default_permission_mode,omitempty" koanf:"default_permission_mode"`

	// DangerouslySkipPermissions bypasses all permission checks.
	// Equivalent to --permission-mode bypassPermissions but skips permission prompts entirely.
	// Recommended only for sandboxes with no internet access.
	DangerouslySkipPermissions *bool `json:"dangerously_skip_permissions,omitempty" koanf:"dangerously_skip_permissions"`

	// AllowedTools restricts available tools (provider-level override)
	AllowedTools []string `json:"allowed_tools,omitempty" koanf:"allowed_tools"`

	// DisallowedTools blocks specific tools (provider-level override)
	DisallowedTools []string `json:"disallowed_tools,omitempty" koanf:"disallowed_tools"`

	// ExtraArgs are additional CLI arguments
	ExtraArgs []string `json:"extra_args,omitempty" koanf:"extra_args"`

	// ExtraEnv are additional environment variables
	ExtraEnv map[string]string `json:"extra_env,omitempty" koanf:"extra_env"`

	// Timeout overrides the default execution timeout
	Timeout time.Duration `json:"timeout,omitempty" koanf:"timeout"`

	// OpenCode-specific options
	OpenCode *OpenCodeConfig `json:"opencode,omitempty" koanf:"opencode"`

	// Pi-specific options
	Pi *PiConfig `json:"pi,omitempty" koanf:"pi"`
}

// OpenCodeConfig contains OpenCode-specific configuration.
type OpenCodeConfig struct {
	// UseHTTPAPI enables HTTP API mode instead of CLI mode
	UseHTTPAPI bool `json:"use_http_api,omitempty" koanf:"use_http_api"`

	// Port for HTTP API server
	Port int `json:"port,omitempty" koanf:"port"`

	// PlanMode enables planning mode
	PlanMode bool `json:"plan_mode,omitempty" koanf:"plan_mode"`

	// Provider is the LLM provider to use
	Provider string `json:"provider,omitempty" koanf:"provider"`

	// Model is the model ID
	Model string `json:"model,omitempty" koanf:"model"`
}

// PiConfig contains pi-mono (pi-coding-agent) specific configuration.
// Pi supports multiple LLM providers through a unified API.
type PiConfig struct {
	// Provider is the LLM provider to use (anthropic, openai, google, etc.)
	Provider string `json:"provider,omitempty" koanf:"provider"`

	// Model is the model ID or pattern (supports provider/id format)
	Model string `json:"model,omitempty" koanf:"model"`

	// Thinking level: off, minimal, low, medium, high, xhigh
	Thinking string `json:"thinking,omitempty" koanf:"thinking"`

	// UseRPC enables RPC mode for process integration (stdin/stdout)
	UseRPC bool `json:"use_rpc,omitempty" koanf:"use_rpc"`

	// SessionDir custom session storage directory
	SessionDir string `json:"session_dir,omitempty" koanf:"session_dir"`

	// NoSession enables ephemeral mode (don't save session)
	NoSession bool `json:"no_session,omitempty" koanf:"no_session"`
}

// Validate validates the provider configuration.
// Returns an error if required fields are missing or invalid.
func (c *ProviderConfig) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("provider type is required")
	}
	if !c.Type.Valid() {
		return fmt.Errorf("invalid provider type: %s", c.Type)
	}
	if c.OpenCode != nil && c.OpenCode.Port < 0 {
		return fmt.Errorf("invalid port number: %d", c.OpenCode.Port)
	}
	if c.Pi != nil && c.Pi.Thinking != "" {
		validThinking := map[string]bool{
			"off": true, "minimal": true, "low": true,
			"medium": true, "high": true, "xhigh": true,
		}
		if !validThinking[c.Pi.Thinking] {
			return fmt.Errorf("invalid thinking level: %s", c.Pi.Thinking)
		}
	}
	return nil
}

// ProviderBase provides common functionality for provider implementations.
// Embed this struct to reduce boilerplate in concrete providers.
type ProviderBase struct {
	meta       ProviderMeta
	binaryPath string
	logger     *slog.Logger
}

// Name returns the provider name.
func (p *ProviderBase) Name() string {
	return string(p.meta.Type)
}

// Metadata returns the provider metadata.
func (p *ProviderBase) Metadata() ProviderMeta {
	return p.meta
}

// ValidateBinary checks if the CLI binary exists and returns its path.
// It uses BinaryPath from config if set, otherwise looks up the binary in PATH.
// Returns a helpful error message with install hint if the binary is not found.
func (p *ProviderBase) ValidateBinary() (string, error) {
	if p.binaryPath != "" {
		return p.binaryPath, nil
	}
	path, err := exec.LookPath(p.meta.BinaryName)
	if err != nil {
		if p.meta.InstallHint != "" {
			return "", fmt.Errorf("%s CLI not found: install with '%s': %w",
				p.meta.DisplayName, p.meta.InstallHint, err)
		}
		return "", fmt.Errorf("%s CLI not found in PATH: %w", p.meta.DisplayName, err)
	}
	return path, nil
}

// CleanupSession provides a default no-op implementation for cleaning up session files.
// Providers that store session files on disk (like Claude Code) should override this.
func (p *ProviderBase) CleanupSession(providerSessionID string, workDir string) error {
	return nil
}

// ============================================================================
// Prompt Builder
// ============================================================================

// PromptBuilder helps construct prompts with task instructions.
// It provides a consistent format across all providers.
type PromptBuilder struct {
	useCDATA bool // Whether to wrap content in CDATA sections
}

// NewPromptBuilder creates a new PromptBuilder.
func NewPromptBuilder(useCDATA bool) *PromptBuilder {
	return &PromptBuilder{useCDATA: useCDATA}
}

// Build constructs the final prompt with task instructions.
// If taskInstructions is empty, returns the prompt unchanged.
func (b *PromptBuilder) Build(prompt, taskInstructions string) string {
	if taskInstructions == "" {
		return prompt
	}
	if b.useCDATA {
		return fmt.Sprintf("<context>\n<![CDATA[\n%s\n]]>\n</context>\n\n<user_query>\n<![CDATA[\n%s\n]]>\n</user_query>",
			taskInstructions, prompt)
	}
	return fmt.Sprintf("<context>\n%s\n</context>\n\n<user_query>\n%s\n</user_query>",
		taskInstructions, prompt)
}

// BuildInputMessage creates a standard input message map for JSON serialization.
func (b *PromptBuilder) BuildInputMessage(prompt, taskInstructions string) map[string]any {
	return map[string]any{
		"prompt":            prompt,
		"task_instructions": taskInstructions,
		"final_prompt":      b.Build(prompt, taskInstructions),
	}
}

// ============================================================================
// Binary Resolution Helper
// ============================================================================

// ResolveBinaryPath resolves the binary path from config or PATH lookup.
// This is a shared helper for all provider implementations.
func ResolveBinaryPath(cfg ProviderConfig, meta ProviderMeta) (string, error) {
	if cfg.BinaryPath != "" {
		return cfg.BinaryPath, nil
	}
	path, err := exec.LookPath(meta.BinaryName)
	if err != nil {
		if meta.InstallHint != "" {
			return "", fmt.Errorf("%s CLI not found: install with '%s': %w",
				meta.DisplayName, meta.InstallHint, err)
		}
		return "", fmt.Errorf("%s CLI not found in PATH: %w", meta.DisplayName, err)
	}
	return path, nil
}
