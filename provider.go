package hotplex

import (
	"fmt"
	"log/slog"
	"os/exec"
	"time"
)

// ProviderType defines the type of AI CLI provider.
type ProviderType string

const (
	ProviderTypeClaudeCode ProviderType = "claude-code"
	ProviderTypeOpenCode   ProviderType = "opencode"
)

// Valid checks if the provider type is a known valid type.
func (t ProviderType) Valid() bool {
	switch t {
	case ProviderTypeClaudeCode, ProviderTypeOpenCode:
		return true
	default:
		return false
	}
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

	// BuildEnvVars constructs environment variables for the CLI process.
	// Returns additional environment variables to be merged with os.Environ().
	BuildEnvVars(opts *ProviderSessionOptions) []string

	// BuildInputMessage constructs the stdin message payload for sending user input.
	// This handles provider-specific input formatting (e.g., stream-json for Claude).
	BuildInputMessage(prompt string, taskSystemPrompt string) (map[string]any, error)

	// ParseEvent parses a raw output line into a normalized ProviderEvent.
	// Returns nil if the line should be ignored (e.g., system messages).
	// Returns an error if parsing fails critically.
	ParseEvent(line string) (*ProviderEvent, error)

	// DetectTurnEnd checks if the event indicates the end of a turn.
	// Different providers signal turn completion differently:
	// - Claude Code: type="result"
	// - OpenCode: step-finish or specific completion marker
	DetectTurnEnd(event *ProviderEvent) bool

	// ExtractSessionID extracts the provider-specific session ID from CLI output.
	// This is used during session startup to capture persistent session identifiers.
	ExtractSessionID(event *ProviderEvent) string

	// ValidateBinary checks if the CLI binary is available and returns its path.
	ValidateBinary() (string, error)

	// GetVersion returns the CLI version string.
	GetVersion() (string, error)

	// Name returns the provider name for logging and identification.
	Name() string
}

// ProviderMeta contains metadata about a provider.
type ProviderMeta struct {
	Type        ProviderType // Provider type identifier
	DisplayName string       // Human-readable name (e.g., "Claude Code")
	BinaryName  string       // CLI binary name (e.g., "claude", "opencode")
	Version     string       // CLI version (if available)
	Features    ProviderFeatures
}

// ProviderFeatures describes the capabilities of a provider.
type ProviderFeatures struct {
	SupportsResume      bool // Can resume existing sessions (e.g., --resume)
	SupportsStreamJSON  bool // Supports stream-json input/output format
	SupportsSSE         bool // Supports Server-Sent Events output
	SupportsHTTPAPI     bool // Has HTTP API mode
	SupportsSessionID   bool // Supports explicit session ID assignment
	SupportsPermissions bool // Supports permission modes
	MultiTurnReady      bool // Can handle multiple turns in one session
}

// ProviderSessionOptions configures a provider session.
// This is the provider-specific subset of session configuration,
// extracted from the global EngineOptions and per-request Config.
type ProviderSessionOptions struct {
	// Working directory for the CLI process
	WorkDir string

	// Permission mode (e.g., "bypass-permissions", "auto-accept")
	PermissionMode string

	// Tool restrictions
	AllowedTools    []string
	DisallowedTools []string

	// System prompts
	BaseSystemPrompt string // Engine-level foundational prompt
	TaskSystemPrompt string // Per-task prompt (injected per-turn)

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
	Enabled bool `json:"enabled" koanf:"enabled"`

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
func (p *ProviderBase) ValidateBinary() (string, error) {
	if p.binaryPath != "" {
		return p.binaryPath, nil
	}
	return exec.LookPath(p.meta.BinaryName)
}

// GetVersion returns the CLI version by executing --version.
func (p *ProviderBase) GetVersion() (string, error) {
	path, err := p.ValidateBinary()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(path, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}
