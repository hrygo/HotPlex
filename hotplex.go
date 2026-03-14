// Package hotplex provides a production-ready execution environment for AI CLI agents.
package hotplex

import (
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
	"github.com/hrygo/hotplex/types"
)

var (
	// Version can be overridden via ldflags: -X github.com/hrygo/hotplex.Version=1.2.3
	Version      = "0.28.1"
	VersionMajor = 0
	VersionMinor = 28
	VersionPatch = 1
)

// Compile-time interface verification
var _ HotPlexClient = (*Engine)(nil)

// ===== Engine Types =====

// Engine is the core Control Plane for AI CLI agent integration.
type Engine = engine.Engine

// EngineOptions defines the configuration parameters for initializing a new HotPlex Engine.
type EngineOptions = engine.EngineOptions

// SessionStats collects session-level statistics.
type SessionStats = engine.SessionStats

// ===== Event Types =====

// Callback is the function signature for event streaming.
type Callback = event.Callback

// EventMeta contains metadata for stream events.
type EventMeta = event.EventMeta

// EventWithMeta wraps event data with metadata.
type EventWithMeta = event.EventWithMeta

// SessionStatsData contains comprehensive session statistics.
type SessionStatsData = event.SessionStatsData

// ===== Base Types =====

// Config represents the configuration for a single HotPlex execution session.
type Config = types.Config

// StreamMessage represents a message from the CLI stream.
type StreamMessage = types.StreamMessage

// ContentBlock represents a content block in a message.
type ContentBlock = types.ContentBlock

// UsageStats represents token usage statistics.
type UsageStats = types.UsageStats

// AssistantMessage represents a message from the assistant.
type AssistantMessage = types.AssistantMessage

// ===== Errors =====

var (
	// ErrDangerBlocked is returned when a dangerous operation is blocked by the WAF.
	ErrDangerBlocked = types.ErrDangerBlocked
	// ErrInvalidConfig is returned when the configuration is invalid.
	ErrInvalidConfig = types.ErrInvalidConfig
	// ErrSessionNotFound is returned when the requested session does not exist.
	ErrSessionNotFound = types.ErrSessionNotFound
	// ErrSessionDead is returned when the session is no longer alive.
	ErrSessionDead = types.ErrSessionDead
	// ErrTimeout is returned when an operation times out.
	ErrTimeout = types.ErrTimeout
	// ErrInputTooLarge is returned when input exceeds maximum size.
	ErrInputTooLarge = types.ErrInputTooLarge
	// ErrProcessStart is returned when the CLI process fails to start.
	ErrProcessStart = types.ErrProcessStart
	// ErrPipeClosed is returned when the pipe is closed.
	ErrPipeClosed = types.ErrPipeClosed
)

// ===== Provider Types =====

// Provider defines the interface for AI CLI agent providers.
type Provider = provider.Provider

// ProviderConfig defines the configuration for a specific provider instance.
type ProviderConfig = provider.ProviderConfig

// OpenCodeConfig contains OpenCode-specific configuration.
type OpenCodeConfig = provider.OpenCodeConfig

// ProviderSessionOptions configures a provider session.
type ProviderSessionOptions = provider.ProviderSessionOptions

// ProviderEvent represents a normalized event from any AI CLI provider.
type ProviderEvent = provider.ProviderEvent

// ProviderMeta contains metadata about a provider.
type ProviderMeta = provider.ProviderMeta

// ProviderFeatures describes the capabilities of a provider.
type ProviderFeatures = provider.ProviderFeatures

// ProviderType defines the type of AI CLI provider.
type ProviderType = provider.ProviderType

// ClaudeCodeProvider implements the Provider interface for Claude Code CLI.
type ClaudeCodeProvider = provider.ClaudeCodeProvider

// Provider constants
const (
	ProviderTypeClaudeCode = provider.ProviderTypeClaudeCode
	ProviderTypeOpenCode   = provider.ProviderTypeOpenCode
)

// ===== Functions =====

var (
	// NewEngine creates a new HotPlex Engine instance.
	NewEngine = engine.NewEngine
	// WrapSafe wraps a callback to make it safe for concurrent use.
	WrapSafe = event.WrapSafe
	// NewEventWithMeta creates a new EventWithMeta.
	NewEventWithMeta = event.NewEventWithMeta
	// TruncateString truncates a string to the given length.
	TruncateString = types.TruncateString
	// SummarizeInput creates a summary of input data.
	SummarizeInput = types.SummarizeInput
	// NewClaudeCodeProvider creates a new Claude Code provider instance.
	NewClaudeCodeProvider = provider.NewClaudeCodeProvider
	// NewOpenCodeProvider creates a new OpenCode provider instance.
	NewOpenCodeProvider = provider.NewOpenCodeProvider
)
