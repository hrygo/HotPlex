package provider

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProviderEventType defines the normalized event types across all providers.
// These types abstract away provider-specific event names to provide a unified
// event model for the HotPlex Engine and downstream consumers.
type ProviderEventType string

const (
	// EventTypeThinking indicates the AI is reasoning or thinking.
	// Claude Code: type="thinking" or type="status"
	// OpenCode: Part.Type="reasoning"
	EventTypeThinking ProviderEventType = "thinking"

	// EventTypeAnswer indicates text output from the AI.
	// Claude Code: type="assistant" with text blocks
	// OpenCode: Part.Type="text"
	EventTypeAnswer ProviderEventType = "answer"

	// EventTypeToolUse indicates a tool invocation is starting.
	// Claude Code: type="tool_use"
	// OpenCode: Part.Type="tool"
	EventTypeToolUse ProviderEventType = "tool_use"

	// EventTypeToolResult indicates a tool execution result.
	// Claude Code: type="tool_result"
	// OpenCode: Part.Type="tool" with result content
	EventTypeToolResult ProviderEventType = "tool_result"

	// EventTypeError indicates an error occurred.
	EventTypeError ProviderEventType = "error"

	// EventTypeResult indicates the turn has completed with final result.
	// Claude Code: type="result"
	// OpenCode: step-finish or completion marker
	EventTypeResult ProviderEventType = "result"

	// EventTypeSystem indicates a system-level message (often filtered).
	EventTypeSystem ProviderEventType = "system"

	// EventTypeUser indicates a user message reflection (often filtered).
	EventTypeUser ProviderEventType = "user"

	// EventTypeStepStart indicates a new step/milestone (OpenCode specific).
	EventTypeStepStart ProviderEventType = "step_start"

	// EventTypeStepFinish indicates a step/milestone completed (OpenCode specific).
	EventTypeStepFinish ProviderEventType = "step_finish"

	// EventTypeRaw indicates unparsed raw output (fallback).
	EventTypeRaw ProviderEventType = "raw"

	// EventTypePermissionRequest indicates a permission request from Claude Code.
	// This event type is used when Claude Code requests user approval for tool execution.
	// Format: {"type":"permission_request","session_id":"...","permission":{"name":"Bash","input":"..."}}
	EventTypePermissionRequest ProviderEventType = "permission_request"

	// EventTypePlanMode indicates Claude is in Plan Mode and generating a plan.
	// Claude Code: type="thinking" with subtype="plan_generation"
	// In this mode, Claude analyzes and plans but does not execute any tools.
	EventTypePlanMode ProviderEventType = "plan_mode"

	// EventTypeExitPlanMode indicates Claude has completed planning and requests user approval.
	// Claude Code: type="tool_use" with name="ExitPlanMode"
	// The plan content is in input.plan field.
	EventTypeExitPlanMode ProviderEventType = "exit_plan_mode"

	// EventTypeAskUserQuestion indicates Claude is asking a clarifying question.
	// Claude Code: type="tool_use" with name="AskUserQuestion"
	// Note: This feature is primarily for interactive mode; headless mode may not support stdin responses.
	// HotPlex handles this as a degraded text prompt (user replies via message).
	EventTypeAskUserQuestion ProviderEventType = "ask_user_question"

	// EventTypeCommandProgress indicates a slash command is executing with progress updates.
	// Used by /reset, /dc, and future slash commands to show step-by-step execution status.
	EventTypeCommandProgress ProviderEventType = "command_progress"

	// EventTypeCommandComplete indicates a slash command has completed successfully.
	EventTypeCommandComplete ProviderEventType = "command_complete"

	// EventTypeSessionStart indicates a new session is starting (cold start).
	// Sent when user sends first message or CLI needs cold start.
	EventTypeSessionStart ProviderEventType = "session_start"

	// EventTypeEngineStarting indicates the engine is starting up.
	// Sent during CLI cold start when engine is being initialized.
	EventTypeEngineStarting ProviderEventType = "engine_starting"

	// EventTypeUserMessageReceived indicates user message has been received.
	// Sent immediately after user message is received to acknowledge receipt.
	EventTypeUserMessageReceived ProviderEventType = "user_message_received"
)

// ProviderEvent represents a normalized event from any AI CLI provider.
// This unified model allows the HotPlex Engine to handle events consistently
// regardless of the underlying provider.
type ProviderEvent struct {
	// Type is the normalized event type
	Type ProviderEventType `json:"type"`

	// RawType is the original type string from the provider (for debugging)
	RawType string `json:"raw_type,omitempty"`

	// Timestamp of the event
	Timestamp time.Time `json:"timestamp,omitempty"`

	// SessionID is the provider-specific session identifier
	SessionID string `json:"session_id,omitempty"`

	// Content contains the main event payload
	Content string `json:"content,omitempty"`

	// Blocks contains structured content blocks (if applicable)
	Blocks []ProviderContentBlock `json:"blocks,omitempty"`

	// Tool information (for tool_use and tool_result events)
	ToolName  string         `json:"tool_name,omitempty"`
	ToolID    string         `json:"tool_id,omitempty"`
	ToolInput map[string]any `json:"tool_input,omitempty"`

	// Status indicates operation status ("running", "success", "error")
	Status string `json:"status,omitempty"`

	// Error contains error message if applicable
	Error   string `json:"error,omitempty"`
	IsError bool   `json:"is_error,omitempty"`

	// Metadata contains additional provider-specific information
	Metadata *ProviderEventMeta `json:"metadata,omitempty"`

	// RawLine preserves the original JSON line for debugging
	RawLine string `json:"-"`
}

// ProviderContentBlock represents a structured content block within an event.
type ProviderContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Name      string         `json:"name,omitempty"`
	ID        string         `json:"id,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	Content   string         `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`
}

// ProviderEventMeta contains additional metadata for observability.
type ProviderEventMeta struct {
	// Timing information
	DurationMs      int64 `json:"duration_ms,omitempty"`
	TotalDurationMs int64 `json:"total_duration_ms,omitempty"`

	// Token usage
	InputTokens      int32 `json:"input_tokens,omitempty"`
	OutputTokens     int32 `json:"output_tokens,omitempty"`
	CacheWriteTokens int32 `json:"cache_write_tokens,omitempty"`
	CacheReadTokens  int32 `json:"cache_read_tokens,omitempty"`

	// Cost information
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`

	// Model information
	Model string `json:"model,omitempty"`

	// Progress tracking
	Progress    int32 `json:"progress,omitempty"`
	TotalSteps  int32 `json:"total_steps,omitempty"`
	CurrentStep int32 `json:"current_step,omitempty"`
}

// ToEventWithMeta converts ProviderEvent to the existing EventWithMeta type.
// This provides backward compatibility with the existing event system.
func (e *ProviderEvent) ToEventWithMeta() *EventWithMeta {
	meta := &EventMeta{
		Status:          e.Status,
		ToolName:        e.ToolName,
		ToolID:          e.ToolID,
		ErrorMsg:        e.Error,
		TotalDurationMs: 0,
	}

	if e.Metadata != nil {
		meta.DurationMs = e.Metadata.DurationMs
		meta.TotalDurationMs = e.Metadata.TotalDurationMs
		meta.InputTokens = e.Metadata.InputTokens
		meta.OutputTokens = e.Metadata.OutputTokens
		meta.CacheWriteTokens = e.Metadata.CacheWriteTokens
		meta.CacheReadTokens = e.Metadata.CacheReadTokens
	}

	return NewEventWithMeta(string(e.Type), e.Content, meta)
}

// ToJSON returns the JSON representation of the event.
func (e *ProviderEvent) ToJSON() (string, error) {
	data, err := json.Marshal(e)
	if err != nil {
		return "", fmt.Errorf("marshal provider event: %w", err)
	}
	return string(data), nil
}

// ParseProviderEvent parses a JSON line into a ProviderEvent.
// This is a generic parser; providers should implement custom parsing
// for their specific event formats.
func ParseProviderEvent(line string) (*ProviderEvent, error) {
	var event ProviderEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parse provider event: %w", err)
	}
	event.RawLine = line
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	return &event, nil
}

// IsTerminalEvent returns true if this event indicates the turn is complete.
func (e *ProviderEvent) IsTerminalEvent() bool {
	return e.Type == EventTypeResult || e.Type == EventTypeError
}

// HasToolInfo returns true if this event contains tool information.
func (e *ProviderEvent) HasToolInfo() bool {
	return e.ToolName != "" || e.ToolID != ""
}

// GetFirstTextBlock extracts the first text block content from the event.
func (e *ProviderEvent) GetFirstTextBlock() string {
	for _, block := range e.Blocks {
		if block.Type == "text" && block.Text != "" {
			return block.Text
		}
	}
	return e.Content
}

// ProviderEventParser defines the interface for parsing provider-specific events.
// Each provider implements this to convert their raw output to normalized events.
type ProviderEventParser interface {
	// Parse converts a raw output line to a normalized ProviderEvent.
	Parse(line string) (*ProviderEvent, error)

	// IsTurnEnd returns true if the event signals turn completion.
	IsTurnEnd(event *ProviderEvent) bool

	// ExtractSessionID extracts the provider session ID from an event.
	ExtractSessionID(event *ProviderEvent) string
}
