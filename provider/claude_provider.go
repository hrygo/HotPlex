package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/hrygo/hotplex/internal/persistence"
)

// ClaudeCodeProvider implements the Provider interface for Claude Code CLI.
// This is the default provider and maintains full backward compatibility
// with the existing HotPlex implementation.
type ClaudeCodeProvider struct {
	ProviderBase
	opts        ProviderConfig
	markerStore persistence.SessionMarkerStore
}

// NewClaudeCodeProvider creates a new Claude Code provider instance.
func NewClaudeCodeProvider(cfg ProviderConfig, logger *slog.Logger) (*ClaudeCodeProvider, error) {
	if logger == nil {
		logger = slog.Default()
	}

	meta := ProviderMeta{
		Type:        ProviderTypeClaudeCode,
		DisplayName: "Claude Code",
		BinaryName:  "claude",
		Features: ProviderFeatures{
			SupportsResume:      true,
			SupportsStreamJSON:  true,
			SupportsSSE:         false,
			SupportsHTTPAPI:     false,
			SupportsSessionID:   true,
			SupportsPermissions: true,
			MultiTurnReady:      true,
		},
	}

	// Determine binary path
	binaryPath := cfg.BinaryPath
	if binaryPath == "" {
		if path, err := exec.LookPath(meta.BinaryName); err == nil {
			binaryPath = path
		}
	}

	// Initialize marker store for session persistence
	markerStore := persistence.NewDefaultFileMarkerStore()

	return &ClaudeCodeProvider{
		ProviderBase: ProviderBase{
			meta:       meta,
			binaryPath: binaryPath,
			logger:     logger.With("provider", "claude-code"),
		},
		opts:        cfg,
		markerStore: markerStore,
	}, nil
}

// BuildCLIArgs constructs Claude Code CLI arguments.
func (p *ClaudeCodeProvider) BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string {
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--include-partial-messages", // Enable streaming of partial content (thinking, etc.)
	}

	// Session management
	if opts.ResumeSession {
		args = append(args, "--resume", providerSessionID)
		p.logger.Debug("Resuming existing Claude Code session", "session_id", providerSessionID)
	} else {
		args = append(args, "--session-id", providerSessionID)
		// Create marker for persistence detection
		if err := p.markerStore.Create(providerSessionID); err != nil {
			p.logger.Warn("Failed to create session marker", "session_id", providerSessionID, "error", err)
		}
		p.logger.Debug("Creating new Claude Code session", "session_id", providerSessionID)
	}

	// Permission mode
	permMode := opts.PermissionMode
	if permMode == "" && p.opts.DefaultPermissionMode != "" {
		permMode = p.opts.DefaultPermissionMode
	}
	if permMode != "" {
		args = append(args, "--permission-mode", permMode)
	}

	// Tool restrictions (merge provider-level and session-level)
	allowedTools := mergeStringSlices(p.opts.AllowedTools, opts.AllowedTools)
	if len(allowedTools) > 0 {
		args = append(args, "--allowed-tools", strings.Join(allowedTools, ","))
	}

	disallowedTools := mergeStringSlices(p.opts.DisallowedTools, opts.DisallowedTools)
	if len(disallowedTools) > 0 {
		args = append(args, "--disallowed-tools", strings.Join(disallowedTools, ","))
	}

	// System prompt (base level only - task prompt is injected per-turn)
	if opts.BaseSystemPrompt != "" {
		args = append(args, "--append-system-prompt", opts.BaseSystemPrompt)
	}

	// Model override
	model := opts.Model
	if model == "" && p.opts.DefaultModel != "" {
		model = p.opts.DefaultModel
	}
	if model != "" {
		args = append(args, "--model", model)
	}

	// Extra arguments from config
	if len(p.opts.ExtraArgs) > 0 {
		args = append(args, p.opts.ExtraArgs...)
	}

	return args
}

// BuildInputMessage constructs the stream-json input message.
func (p *ClaudeCodeProvider) BuildInputMessage(prompt string, taskInstructions string) (map[string]any, error) {
	// Inject task-level constraints into the prompt for Hot-Multiplexing using XML tags and CDATA.
	// This follows Anthropic's best practices for clear delineation.
	finalPrompt := prompt
	if taskInstructions != "" {
		finalPrompt = fmt.Sprintf("<context>\n<![CDATA[\n%s\n]]>\n</context>\n\n<user_query>\n<![CDATA[\n%s\n]]>\n</user_query>",
			taskInstructions, prompt)
	}

	return map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": finalPrompt},
			},
		},
	}, nil
}

// ParseEvent parses a Claude Code stream-json line into a ProviderEvent.
func (p *ClaudeCodeProvider) ParseEvent(line string) (*ProviderEvent, error) {
	// Debug: Log raw event for tool-related events
	if strings.Contains(line, "tool") {
		lineLen := len(line)
		if lineLen > 500 {
			lineLen = 500
		}
		p.logger.Debug("[PROVIDER] Raw tool event from CLI", "line", line[:lineLen])
	}

	var msg StreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Not valid JSON, return as raw content
		return &ProviderEvent{
			Type:    EventTypeRaw,
			RawType: "raw",
			Content: line,
			RawLine: line,
		}, nil
	}

	event := &ProviderEvent{
		RawType:   msg.Type,
		SessionID: msg.SessionID,
		Status:    msg.Status,
		Error:     msg.Error,
		IsError:   msg.IsError,
		RawLine:   line,
	}

	// Map Claude Code types to normalized types
	switch msg.Type {
	case "result":
		event.Type = EventTypeResult
		event.Content = msg.Result
		if msg.Usage != nil {
			event.Metadata = &ProviderEventMeta{
				DurationMs:       int64(msg.Duration),
				InputTokens:      msg.Usage.InputTokens,
				OutputTokens:     msg.Usage.OutputTokens,
				CacheWriteTokens: msg.Usage.CacheWriteInputTokens,
				CacheReadTokens:  msg.Usage.CacheReadInputTokens,
				TotalCostUSD:     msg.TotalCostUSD,
			}
		}

	case "error":
		event.Type = EventTypeError
		event.Content = msg.Error

	case "thinking", "status":
		event.Type = EventTypeThinking
		p.logger.Info("[PROVIDER] Received thinking event from CLI",
			"has_content", len(msg.Content) > 0,
			"status", msg.Status,
			"subtype", msg.Subtype)
		// Check both msg.Content (direct) and msg.Message.Content (nested)
		allBlocks := msg.GetContentBlocks()
		p.logger.Debug("[PROVIDER] Raw thinking event",
			"has_direct_content", len(msg.Content) > 0,
			"has_message_content", msg.Message != nil && len(msg.Message.Content) > 0,
			"direct_content", msg.Content,
			"blocks_count", len(allBlocks))

		// Try to extract text from blocks
		for _, block := range allBlocks {
			p.logger.Debug("[PROVIDER] Thinking block",
				"type", block.Type,
				"text", block.Text)
			if block.Text != "" {
				event.Content = block.Text
				break
			}
		}

		// Fallback 1: Check status field
		if event.Content == "" && msg.Status != "" {
			event.Content = msg.Status
			p.logger.Debug("[PROVIDER] Using status as thinking content", "status", msg.Status)
		}

		// Fallback 2: Check subtype field
		if event.Content == "" && msg.Subtype != "" {
			event.Content = msg.Subtype
			p.logger.Debug("[PROVIDER] Using subtype as thinking content", "subtype", msg.Subtype)
		}

		// Final fallback: use generic thinking text
		if event.Content == "" {
			event.Content = "Thinking..."
			p.logger.Debug("[PROVIDER] Using default thinking text")
		}

	case "tool_use":
		event.Type = EventTypeToolUse
		event.ToolName = msg.Name
		event.Status = "running"
		p.logger.Debug("[PROVIDER] Parsed tool_use", "name", msg.Name, "has_blocks", len(msg.GetContentBlocks()) > 0)
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "tool_use" {
				event.ToolID = block.ID
				event.ToolInput = block.Input
				break
			}
		}

	case "tool_result":
		event.Type = EventTypeToolResult
		event.Status = "success"
		event.Content = msg.Output
		p.logger.Debug("[PROVIDER] Parsed tool_result",
			"output_len", len(msg.Output),
			"has_blocks", len(msg.GetContentBlocks()) > 0,
			"name", msg.Name) // Check if name is in the message itself

		// Extract tool info from content blocks
		for _, block := range msg.GetContentBlocks() {
			p.logger.Debug("[PROVIDER] tool_result block",
				"type", block.Type,
				"name", block.Name,
				"id", block.ID)
			if block.Type == "tool_result" {
				event.ToolID = block.GetUnifiedToolID()
				if block.Name != "" {
					event.ToolName = block.Name
				}
				event.IsError = block.IsError
				if event.IsError {
					event.Status = "error"
				}
				break
			}
		}

		// Fallback: use msg.Name if block didn't have it
		if event.ToolName == "" && msg.Name != "" {
			event.ToolName = msg.Name
			p.logger.Debug("[PROVIDER] Using msg.Name as tool_name", "name", msg.Name)
		}

		// Final fallback: try to extract tool name from content
		if event.ToolName == "" && event.Content != "" {
			// Try to detect tool type from content
			content := event.Content
			if len(content) > 100 {
				content = content[:100]
			}
			p.logger.Debug("[PROVIDER] Could not extract tool_name", "content_preview", content)
		}

	case "assistant", "message", "content", "text", "delta":
		event.Type = EventTypeAnswer
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				event.Content = block.Text
				event.Blocks = append(event.Blocks, ProviderContentBlock{
					Type: block.Type,
					Text: block.Text,
				})
			} else if block.Type == "tool_use" {
				// Embedded tool use in assistant message
				event.Blocks = append(event.Blocks, ProviderContentBlock{
					Type:  block.Type,
					Name:  block.Name,
					ID:    block.ID,
					Input: block.Input,
				})
			}
		}

	case "system":
		event.Type = EventTypeSystem
		// System messages are typically filtered out

	case "user":
		event.Type = EventTypeUser
		// Extract tool results from user message reflections
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "tool_result" {
				event.Type = EventTypeToolResult
				event.ToolID = block.GetUnifiedToolID()
				event.ToolName = block.Name
				event.Content = block.Content
				break
			}
		}

	default:
		// Unknown type, try to extract text content
		event.Type = EventTypeAnswer
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				event.Content = block.Text
				break
			}
		}
	}

	return event, nil
}

// DetectTurnEnd checks if the event signals turn completion.
func (p *ClaudeCodeProvider) DetectTurnEnd(event *ProviderEvent) bool {
	return event != nil && (event.Type == EventTypeResult || event.Type == EventTypeError)
}

// ValidateBinary checks if the Claude CLI is available.
func (p *ClaudeCodeProvider) ValidateBinary() (string, error) {
	if p.binaryPath != "" {
		return p.binaryPath, nil
	}
	path, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude Code CLI not found: %w", err)
	}
	return path, nil
}

// GetMarkerDir returns the session marker directory path.
func (p *ClaudeCodeProvider) GetMarkerDir() string {
	return p.markerStore.Dir()
}

// CheckSessionMarker checks if a session marker exists for the given ID.
func (p *ClaudeCodeProvider) CheckSessionMarker(providerSessionID string) bool {
	return p.markerStore.Exists(providerSessionID)
}

// mergeStringSlices merges two string slices with deduplication.
func mergeStringSlices(base, overlay []string) []string {
	if len(base) == 0 {
		return overlay
	}
	if len(overlay) == 0 {
		return base
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(base)+len(overlay))

	for _, s := range base {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range overlay {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}
