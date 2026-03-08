package provider

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/persistence"
)

// ClaudeCodeProvider implements the Provider interface for Claude Code CLI.
// This is the default provider and maintains full backward compatibility
// with the existing HotPlex implementation.
type ClaudeCodeProvider struct {
	ProviderBase
	opts          ProviderConfig
	markerStore   persistence.SessionMarkerStore
	promptBuilder *PromptBuilder
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
		InstallHint: "npm install -g @anthropic-ai/claude-code",
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

	// Resolve binary path using helper
	binaryPath, err := ResolveBinaryPath(cfg, meta)
	if err != nil {
		return nil, err
	}

	// Initialize marker store for session persistence
	markerStore := persistence.NewDefaultFileMarkerStore()

	return &ClaudeCodeProvider{
		ProviderBase: ProviderBase{
			meta:       meta,
			binaryPath: binaryPath,
			logger:     logger.With("provider", "claude-code"),
		},
		opts:          cfg,
		markerStore:   markerStore,
		promptBuilder: NewPromptBuilder(true), // Use CDATA for Claude
	}, nil
}

// BuildCLIArgs constructs Claude Code CLI arguments.
func (p *ClaudeCodeProvider) BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string {
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--include-partial-messages",       // Enable streaming of partial content (thinking, etc.)
		"--settings", `{"fastMode":false}`, // Force disable fastMode for Agent SDK compatibility
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

	// DangerouslySkipPermissions bypasses all permission checks
	if opts.DangerouslySkipPermissions || BoolValue(p.opts.DangerouslySkipPermissions, false) {
		args = append(args, "--dangerously-skip-permissions")
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
	finalPrompt := p.promptBuilder.Build(prompt, taskInstructions)

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

// ParseEvent parses a Claude Code stream-json line into one or more ProviderEvents.
func (p *ClaudeCodeProvider) ParseEvent(line string) ([]*ProviderEvent, error) {
	var msg StreamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		// Not valid JSON, return as raw content
		return []*ProviderEvent{{
			Type:    EventTypeRaw,
			RawType: "raw",
			Content: line,
			RawLine: line,
		}}, nil
	}

	// Helper to create a base event from common fields
	newBaseEvent := func(evtType ProviderEventType) *ProviderEvent {
		return &ProviderEvent{
			Type:      evtType,
			RawType:   msg.Type,
			SessionID: msg.SessionID,
			Status:    msg.Status,
			Error:     msg.Error,
			IsError:   msg.IsError,
			RawLine:   line,
			Timestamp: time.Now(),
		}
	}

	var events []*ProviderEvent

	// Map Claude Code types to normalized types
	switch msg.Type {
	case "result":
		event := newBaseEvent(EventTypeResult)
		event.Content = msg.Result
		// Always create metadata for result events to ensure tokens are tracked
		dur := msg.Duration
		if dur == 0 {
			dur = msg.DurationMs
		}
		event.Metadata = &ProviderEventMeta{
			DurationMs:      int64(dur),
			TotalDurationMs: int64(dur),
			TotalCostUSD:    msg.TotalCostUSD,
		}
		// Extract tokens from ModelUsage (new Claude Code versions) or fallback to Usage
		var totalInput, totalOutput, totalCacheWrite, totalCacheRead int32
		var totalCost float64
		hasModelUsage := false
		if len(msg.ModelUsage) > 0 {
			for _, mUsage := range msg.ModelUsage {
				totalInput += mUsage.InputTokens
				totalOutput += mUsage.OutputTokens
				totalCacheWrite += mUsage.CacheCreationInputTokens
				totalCacheRead += mUsage.CacheReadInputTokens
				totalCost += mUsage.CostUSD
			}
			if totalInput > 0 || totalOutput > 0 || totalCacheWrite > 0 || totalCacheRead > 0 {
				hasModelUsage = true
			}
		}

		if hasModelUsage {
			event.Metadata.InputTokens = totalInput
			event.Metadata.OutputTokens = totalOutput
			event.Metadata.CacheWriteTokens = totalCacheWrite
			event.Metadata.CacheReadTokens = totalCacheRead
			if event.Metadata.TotalCostUSD == 0 {
				event.Metadata.TotalCostUSD = totalCost
			}
		} else if msg.Usage != nil {
			event.Metadata.InputTokens = msg.Usage.InputTokens
			event.Metadata.OutputTokens = msg.Usage.OutputTokens
			event.Metadata.CacheWriteTokens = msg.Usage.CacheWriteInputTokens
			event.Metadata.CacheReadTokens = msg.Usage.CacheReadInputTokens
		} else {
			// Debug: log that usage is missing
			p.logger.Warn("[PROVIDER] result event missing usage data", "line", line)
		}
		events = append(events, event)

	case "error":
		event := newBaseEvent(EventTypeError)
		event.Content = msg.Error
		events = append(events, event)

	case "thinking", "status":
		blocks := msg.GetContentBlocks()
		if len(blocks) > 0 {
			for _, block := range blocks {
				switch block.Type {
				case "text":
					if block.Text != "" {
						ev := newBaseEvent(EventTypeThinking)
						if msg.Subtype == "plan_generation" {
							ev.Type = EventTypePlanMode
						}
						ev.Content = block.Text
						events = append(events, ev)
					}
				case "tool_use":
					ev := newBaseEvent(EventTypeToolUse)
					ev.ToolName = block.Name
					ev.ToolID = block.ID
					ev.ToolInput = block.Input
					events = append(events, ev)
				}
			}
		}

		// Fallback for direct content or status if no blocks were processed
		if len(events) == 0 {
			ev := newBaseEvent(EventTypeThinking)
			if msg.Subtype == "plan_generation" {
				ev.Type = EventTypePlanMode
			}
			ev.Content = msg.Status
			if ev.Content == "" {
				ev.Content = "Thinking..."
			}
			events = append(events, ev)
		}

	case "tool_use":
		// Handle special tool types
		switch msg.Name {
		case "ExitPlanMode":
			event := newBaseEvent(EventTypeExitPlanMode)
			event.ToolName = msg.Name
			if msg.Input != nil {
				if plan, ok := msg.Input["plan"].(string); ok {
					event.Content = plan
				}
			}
			events = append(events, event)
		case "AskUserQuestion":
			event := newBaseEvent(EventTypeAskUserQuestion)
			event.ToolName = msg.Name
			if msg.Input != nil {
				if question, ok := msg.Input["question"].(string); ok {
					event.Content = question
				}
				event.ToolInput = msg.Input
			}
			events = append(events, event)
		default:
			// Normal tool use
			event := newBaseEvent(EventTypeToolUse)
			event.ToolName = msg.Name
			event.ToolInput = msg.Input
			// Try to get more info from blocks (Message-based format)
			for _, block := range msg.GetContentBlocks() {
				if block.Type == "tool_use" {
					if event.ToolName == "" {
						event.ToolName = block.Name
					}
					if event.ToolID == "" {
						event.ToolID = block.ID
					}
					if event.ToolInput == nil {
						event.ToolInput = block.Input
					}
				}
			}
			events = append(events, event)
		}

	case "tool_result":
		event := newBaseEvent(EventTypeToolResult)
		event.Content = msg.Output
		event.ToolID = msg.MessageID // Some use MessageID for result
		event.ToolName = msg.Name

		for _, block := range msg.GetContentBlocks() {
			if block.Type == "tool_result" {
				if event.ToolID == "" {
					event.ToolID = block.GetUnifiedToolID()
				}
				if event.ToolName == "" {
					event.ToolName = block.Name
				}
				if event.Content == "" {
					event.Content = block.Content
				}
				if block.IsError {
					event.Status = "error"
					event.IsError = true
				}
			}
		}
		if event.Status == "" {
			event.Status = "success"
		}
		events = append(events, event)

	case "assistant", "message", "content", "text", "delta":
		// Multi-part messages
		blocks := msg.GetContentBlocks()
		for _, block := range blocks {
			switch block.Type {
			case "text":
				if block.Text != "" {
					ev := newBaseEvent(EventTypeAnswer)
					ev.Content = block.Text
					events = append(events, ev)
				}
			case "tool_use":
				ev := newBaseEvent(EventTypeToolUse)
				ev.ToolName = block.Name
				ev.ToolID = block.ID
				ev.ToolInput = block.Input
				events = append(events, ev)
			case "tool_result":
				ev := newBaseEvent(EventTypeToolResult)
				ev.ToolName = block.Name
				ev.ToolID = block.GetUnifiedToolID()
				ev.Content = block.Content
				if block.IsError {
					ev.Status = "error"
					ev.IsError = true
				} else {
					ev.Status = "success"
				}
				events = append(events, ev)
			}
		}
		// Fallback for direct text
		if len(events) == 0 && msg.Result != "" {
			ev := newBaseEvent(EventTypeAnswer)
			ev.Content = msg.Result
			events = append(events, ev)
		}

	case "permission_request":
		event := newBaseEvent(EventTypePermissionRequest)
		if msg.Permission != nil {
			event.ToolName = msg.Permission.Name
			event.Content = msg.Permission.Input
		}
		if msg.Decision != nil {
			event.ToolID = msg.MessageID
			if msg.Decision.Reason != "" {
				event.Content = msg.Decision.Reason + "\n" + event.Content
			}
		}
		events = append(events, event)

	default:
		// Fallback for unknown types - try to extract ANY text
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				ev := newBaseEvent(EventTypeAnswer)
				ev.Content = block.Text
				events = append(events, ev)
			}
		}
	}

	return events, nil
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

// CleanupSession overrides ProviderBase to delete Claude Code's project session file.
// This is necessary when starting a fresh session to prevent "Session ID is already in use" errors
// or when executing a /reset command to completely scrub the context.
func (p *ClaudeCodeProvider) CleanupSession(providerSessionID string, workDir string) error {
	if providerSessionID == "" {
		return nil
	}

	cwd := workDir
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			cwd = os.TempDir()
		}
	}

	// Claude Code stores sessions in ~/.claude/projects/<workspace-key>/<providerSessionID>.jsonl
	projectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")
	workspaceKey := strings.ReplaceAll(cwd, "/", "-")
	sessionPath := filepath.Join(projectsDir, workspaceKey, providerSessionID+".jsonl")

	// Best effort deletion
	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove Claude Code session file: %w", err)
	}

	p.logger.Debug("Cleaned up Claude Code session file", "path", sessionPath)
	return nil
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
