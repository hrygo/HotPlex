package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// PiProvider implements the Provider interface for pi-coding-agent CLI.
// Pi is a minimal terminal coding harness that supports 15+ LLM providers
// through a unified API. It outputs events in JSON Lines format.
//
// Key features:
//   - Multi-provider support (Anthropic, OpenAI, Google, etc.)
//   - JSON mode for structured output
//   - RPC mode for process integration
//   - Session management with JSONL storage
type PiProvider struct {
	ProviderBase
	opts          ProviderConfig
	piCfg         *PiConfig
	pkgName       string // npm package name for pi CLI
	promptBuilder *PromptBuilder
}

// Pi event types from the JSON output stream.
// Reference: https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/json.md
const (
	PiEventTypeAgentStart          = "agent_start"
	PiEventTypeAgentEnd            = "agent_end"
	PiEventTypeTurnStart           = "turn_start"
	PiEventTypeTurnEnd             = "turn_end"
	PiEventTypeMessageStart        = "message_start"
	PiEventTypeMessageUpdate       = "message_update"
	PiEventTypeMessageEnd          = "message_end"
	PiEventTypeToolExecutionStart  = "tool_execution_start"
	PiEventTypeToolExecutionUpdate = "tool_execution_update"
	PiEventTypeToolExecutionEnd    = "tool_execution_end"
	PiEventTypeSession             = "session"
	PiEventTypeAutoCompactionStart = "auto_compaction_start"
	PiEventTypeAutoCompactionEnd   = "auto_compaction_end"
	PiEventTypeAutoRetryStart      = "auto_retry_start"
	PiEventTypeAutoRetryEnd        = "auto_retry_end"
)

// Pi content block types.
const (
	PiContentTypeText     = "text"
	PiContentTypeThinking = "thinking"
	PiContentTypeToolCall = "toolCall"
	PiContentTypeImage    = "image"
)

// PiSessionEvent represents the session header event.
type PiSessionEvent struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

// PiAgentEvent represents agent lifecycle events.
type PiAgentEvent struct {
	Type     string           `json:"type"`
	Messages []PiAgentMessage `json:"messages,omitempty"`
	Message  *PiAgentMessage  `json:"message,omitempty"`
}

// PiAgentMessage represents a message in the pi event stream.
type PiAgentMessage struct {
	Role         string           `json:"role"`
	Content      []PiContentBlock `json:"content,omitempty"`
	Timestamp    int64            `json:"timestamp,omitempty"`
	Provider     string           `json:"provider,omitempty"`
	Model        string           `json:"model,omitempty"`
	API          string           `json:"api,omitempty"`
	Usage        *PiUsage         `json:"usage,omitempty"`
	StopReason   string           `json:"stopReason,omitempty"`
	ErrorMessage string           `json:"errorMessage,omitempty"`
}

// PiContentBlock represents a content block in a pi message.
type PiContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Arguments map[string]any `json:"arguments,omitempty"`
	Data      string         `json:"data,omitempty"`
	MimeType  string         `json:"mimeType,omitempty"`
}

// PiUsage represents token usage information.
type PiUsage struct {
	InputTokens  int32 `json:"input_tokens"`
	OutputTokens int32 `json:"output_tokens"`
}

// PiAssistantMessageEvent represents message update events.
type PiAssistantMessageEvent struct {
	Type  string `json:"type"`
	Delta string `json:"delta,omitempty"`
}

// PiMessageUpdateEvent represents a message_update event.
type PiMessageUpdateEvent struct {
	Type                  string                   `json:"type"`
	Message               *PiAgentMessage          `json:"message"`
	AssistantMessageEvent *PiAssistantMessageEvent `json:"assistantMessageEvent,omitempty"`
}

// PiToolExecutionEvent represents tool execution events.
type PiToolExecutionEvent struct {
	Type          string         `json:"type"`
	ToolCallID    string         `json:"toolCallId"`
	ToolName      string         `json:"toolName"`
	Args          map[string]any `json:"args,omitempty"`
	Result        any            `json:"result,omitempty"`
	PartialResult any            `json:"partialResult,omitempty"`
	IsError       bool           `json:"isError,omitempty"`
}

// Compile-time interface verification.
var _ Provider = (*PiProvider)(nil)

// NewPiProvider creates a new pi provider instance.
func NewPiProvider(cfg ProviderConfig, logger *slog.Logger) (*PiProvider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	meta := ProviderMeta{
		Type:        ProviderTypePi,
		DisplayName: "Pi (pi-coding-agent)",
		BinaryName:  "pi",
		InstallHint: "npm install -g @mariozechner/pi-coding-agent",
		Features: ProviderFeatures{
			SupportsResume:             true,
			SupportsStreamJSON:         true,
			SupportsSSE:                false,
			SupportsHTTPAPI:            false,
			SupportsSessionID:          true,
			SupportsPermissions:        false,
			MultiTurnReady:             true,
			RequiresInitialPromptAsArg: true,
		},
	}

	// Resolve binary path using helper
	binaryPath, err := ResolveBinaryPath(cfg, meta)
	if err != nil {
		return nil, err
	}

	// Extract pi-specific config
	var piCfg *PiConfig
	if cfg.Pi != nil {
		piCfg = cfg.Pi
	} else {
		piCfg = &PiConfig{}
	}

	return &PiProvider{
		ProviderBase: ProviderBase{
			meta:       meta,
			binaryPath: binaryPath,
			logger:     logger.With("provider", "pi"),
		},
		opts:          cfg,
		piCfg:         piCfg,
		pkgName:       "@mariozechner/pi-coding-agent",
		promptBuilder: NewPromptBuilder(false), // Pi doesn't need CDATA
	}, nil
}

// BuildCLIArgs constructs pi CLI arguments.
func (p *PiProvider) BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string {
	args := []string{}

	// Use JSON mode for structured output
	args = append(args, "--mode", "json")

	// Session management
	if providerSessionID != "" {
		// Check if we should resume or continue
		if opts != nil && opts.ResumeSession {
			args = append(args, "--session", providerSessionID)
		}
	}

	// Provider and model configuration
	provider := p.piCfg.Provider
	model := p.piCfg.Model
	if opts != nil && opts.Model != "" {
		model = opts.Model
	}
	if model == "" {
		model = p.opts.DefaultModel
	}

	if provider != "" {
		args = append(args, "--provider", provider)
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Thinking level
	if p.piCfg.Thinking != "" {
		args = append(args, "--thinking", p.piCfg.Thinking)
	}

	// Session directory
	if p.piCfg.SessionDir != "" {
		args = append(args, "--session-dir", p.piCfg.SessionDir)
	}

	// Ephemeral mode
	if p.piCfg.NoSession {
		args = append(args, "--no-session")
	}

	// Extra arguments from config
	if len(p.opts.ExtraArgs) > 0 {
		args = append(args, p.opts.ExtraArgs...)
	}

	// Initial prompt (required for cold start)
	if opts != nil && opts.InitialPrompt != "" {
		finalPrompt := opts.InitialPrompt
		if opts.TaskInstructions != "" {
			finalPrompt = fmt.Sprintf("<context>\n%s\n</context>\n\n<user_query>\n%s\n</user_query>",
				opts.TaskInstructions, opts.InitialPrompt)
		}
		args = append(args, finalPrompt)
	}

	return args
}

// BuildInputMessage constructs the input for pi.
// Note: Pi typically takes the prompt as a CLI argument, not stdin.
func (p *PiProvider) BuildInputMessage(prompt string, taskInstructions string) (map[string]any, error) {
	finalPrompt := p.promptBuilder.Build(prompt, taskInstructions)

	return map[string]any{
		"prompt": finalPrompt,
	}, nil
}

// ParseEvent parses a pi JSON output line into one or more ProviderEvents.
func (p *PiProvider) ParseEvent(line string) ([]*ProviderEvent, error) {
	// Try to parse as a generic event first to get the type
	var baseEvent struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &baseEvent); err != nil {
		// Not valid JSON, return as raw content
		return []*ProviderEvent{{
			Type:    EventTypeRaw,
			RawType: "raw",
			Content: line,
			RawLine: line,
		}}, nil
	}

	// Parse based on event type
	switch baseEvent.Type {
	case PiEventTypeSession:
		return p.parseSessionEvent(line)

	case PiEventTypeAgentStart:
		return []*ProviderEvent{{
			Type:    EventTypeSystem,
			RawType: baseEvent.Type,
			RawLine: line,
		}}, nil

	case PiEventTypeAgentEnd:
		return []*ProviderEvent{{
			Type:    EventTypeResult,
			RawType: baseEvent.Type,
			RawLine: line,
		}}, nil

	case PiEventTypeTurnStart:
		return []*ProviderEvent{{
			Type:    EventTypeSystem,
			RawType: baseEvent.Type,
			Status:  "running",
			RawLine: line,
		}}, nil

	case PiEventTypeTurnEnd:
		return []*ProviderEvent{{
			Type:    EventTypeResult,
			RawType: baseEvent.Type,
			Status:  "completed",
			RawLine: line,
		}}, nil

	case PiEventTypeMessageStart, PiEventTypeMessageEnd:
		return p.parseMessageEvent(line, baseEvent.Type)

	case PiEventTypeMessageUpdate:
		return p.parseMessageUpdateEvent(line)

	case PiEventTypeToolExecutionStart:
		return p.parseToolExecutionStart(line)

	case PiEventTypeToolExecutionEnd:
		return p.parseToolExecutionEnd(line)

	case PiEventTypeAutoCompactionStart, PiEventTypeAutoCompactionEnd,
		PiEventTypeAutoRetryStart, PiEventTypeAutoRetryEnd:
		// System events, just log
		return []*ProviderEvent{{
			Type:    EventTypeSystem,
			RawType: baseEvent.Type,
			RawLine: line,
		}}, nil

	default:
		// Unknown event type, try to extract content
		return []*ProviderEvent{{
			Type:    EventTypeRaw,
			RawType: baseEvent.Type,
			Content: line,
			RawLine: line,
		}}, nil
	}
}

// parseSessionEvent parses a session header event.
func (p *PiProvider) parseSessionEvent(line string) ([]*ProviderEvent, error) {
	var event PiSessionEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parse session event: %w", err)
	}

	return []*ProviderEvent{{
		Type:      EventTypeSystem,
		RawType:   event.Type,
		SessionID: event.ID,
		RawLine:   line,
	}}, nil
}

// parseMessageEvent parses message_start and message_end events.
func (p *PiProvider) parseMessageEvent(line string, eventType string) ([]*ProviderEvent, error) {
	var event PiAgentEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parse message event: %w", err)
	}

	if event.Message == nil {
		return []*ProviderEvent{{
			Type:    EventTypeSystem,
			RawType: eventType,
			RawLine: line,
		}}, nil
	}

	var events []*ProviderEvent

	// Parse content blocks
	for _, block := range event.Message.Content {
		pe := p.parseContentBlock(block, line)
		if pe != nil {
			events = append(events, pe)
		}
	}

	// If no content blocks found, return system event
	if len(events) == 0 {
		events = append(events, &ProviderEvent{
			Type:    EventTypeSystem,
			RawType: eventType,
			RawLine: line,
		})
	}

	return events, nil
}

// parseMessageUpdateEvent parses message_update events for streaming.
func (p *PiProvider) parseMessageUpdateEvent(line string) ([]*ProviderEvent, error) {
	var event PiMessageUpdateEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parse message update event: %w", err)
	}

	// Check for assistant message event (text_delta, etc.)
	if event.AssistantMessageEvent != nil {
		switch event.AssistantMessageEvent.Type {
		case "text_delta":
			return []*ProviderEvent{{
				Type:    EventTypeAnswer,
				RawType: "text_delta",
				Content: event.AssistantMessageEvent.Delta,
				RawLine: line,
			}}, nil

		case "thinking_delta":
			return []*ProviderEvent{{
				Type:    EventTypeThinking,
				RawType: "thinking_delta",
				Content: event.AssistantMessageEvent.Delta,
				RawLine: line,
			}}, nil
		}
	}

	// Fallback: parse full message content
	if event.Message != nil && len(event.Message.Content) > 0 {
		var events []*ProviderEvent
		for _, block := range event.Message.Content {
			pe := p.parseContentBlock(block, line)
			if pe != nil {
				events = append(events, pe)
			}
		}
		if len(events) > 0 {
			return events, nil
		}
	}

	return []*ProviderEvent{{
		Type:    EventTypeSystem,
		RawType: event.Type,
		RawLine: line,
	}}, nil
}

// parseToolExecutionStart parses tool_execution_start events.
func (p *PiProvider) parseToolExecutionStart(line string) ([]*ProviderEvent, error) {
	var event PiToolExecutionEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parse tool execution start: %w", err)
	}

	return []*ProviderEvent{{
		Type:      EventTypeToolUse,
		RawType:   event.Type,
		ToolName:  event.ToolName,
		ToolID:    event.ToolCallID,
		ToolInput: event.Args,
		Status:    "running",
		RawLine:   line,
	}}, nil
}

// parseToolExecutionEnd parses tool_execution_end events.
func (p *PiProvider) parseToolExecutionEnd(line string) ([]*ProviderEvent, error) {
	var event PiToolExecutionEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return nil, fmt.Errorf("parse tool execution end: %w", err)
	}

	// Convert result to string
	resultStr := ""
	if event.Result != nil {
		switch v := event.Result.(type) {
		case string:
			resultStr = v
		default:
			if b, err := json.Marshal(v); err == nil {
				resultStr = string(b)
			}
		}
	}

	status := "success"
	if event.IsError {
		status = "error"
	}

	return []*ProviderEvent{{
		Type:     EventTypeToolResult,
		RawType:  event.Type,
		ToolName: event.ToolName,
		ToolID:   event.ToolCallID,
		Content:  resultStr,
		Status:   status,
		IsError:  event.IsError,
		RawLine:  line,
	}}, nil
}

// parseContentBlock parses a pi content block into a ProviderEvent.
func (p *PiProvider) parseContentBlock(block PiContentBlock, rawLine string) *ProviderEvent {
	switch block.Type {
	case PiContentTypeText:
		if block.Text == "" {
			return nil
		}
		return &ProviderEvent{
			Type:    EventTypeAnswer,
			RawType: block.Type,
			Content: block.Text,
			RawLine: rawLine,
		}

	case PiContentTypeThinking:
		if block.Thinking == "" {
			return nil
		}
		return &ProviderEvent{
			Type:    EventTypeThinking,
			RawType: block.Type,
			Content: block.Thinking,
			RawLine: rawLine,
		}

	case PiContentTypeToolCall:
		return &ProviderEvent{
			Type:      EventTypeToolUse,
			RawType:   block.Type,
			ToolName:  block.Name,
			ToolID:    block.ID,
			ToolInput: block.Arguments,
			Status:    "running",
			RawLine:   rawLine,
		}

	case PiContentTypeImage:
		return &ProviderEvent{
			Type:    EventTypeSystem,
			RawType: block.Type,
			Content: "[Image]",
			RawLine: rawLine,
		}

	default:
		return nil
	}
}

// DetectTurnEnd checks if the event signals turn completion.
func (p *PiProvider) DetectTurnEnd(event *ProviderEvent) bool {
	if event == nil {
		return false
	}

	// turn_end and agent_end signal completion
	if event.RawType == PiEventTypeTurnEnd || event.RawType == PiEventTypeAgentEnd {
		return true
	}

	// Error events also end the turn
	if event.Type == EventTypeError {
		return true
	}

	// Result type signals completion
	if event.Type == EventTypeResult {
		return true
	}

	return false
}

// ValidateBinary checks if the pi CLI is available.
func (p *PiProvider) ValidateBinary() (string, error) {
	if p.binaryPath != "" {
		return p.binaryPath, nil
	}

	// Try to find pi in PATH
	path, err := exec.LookPath("pi")
	if err != nil {
		return "", fmt.Errorf("pi CLI not found: install with 'npm install -g %s': %w", p.pkgName, err)
	}
	return path, nil
}

// CleanupSession cleans up pi session files.
// Pi stores sessions in ~/.pi/agent/sessions/ as JSONL files.
func (p *PiProvider) CleanupSession(providerSessionID string, workDir string) error {
	if providerSessionID == "" {
		return nil
	}

	// Pi stores sessions in ~/.pi/agent/sessions/
	// Session files are named with timestamp and UUID
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home directory: %w", err)
	}

	sessionDir := filepath.Join(homeDir, ".pi", "agent", "sessions")
	if p.piCfg.SessionDir != "" {
		sessionDir = p.piCfg.SessionDir
	}

	// Find and remove session file matching the ID
	// Session files follow pattern: <timestamp>_<uuid>.jsonl
	pattern := filepath.Join(sessionDir, "*"+providerSessionID+"*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("find session files: %w", err)
	}

	for _, match := range matches {
		if err := removeFile(match); err != nil {
			p.logger.Warn("Failed to remove session file", "file", match, "error", err)
		}
	}

	return nil
}

// removeFile is a helper to remove a file.
func removeFile(path string) error {
	return os.Remove(path)
}
