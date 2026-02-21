package hotplex

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeCodeProvider implements the Provider interface for Claude Code CLI.
// This is the default provider and maintains full backward compatibility
// with the existing HotPlex implementation.
type ClaudeCodeProvider struct {
	ProviderBase
	opts      ProviderConfig
	markerDir string
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

	// Initialize marker directory for session persistence
	homeDir, err := os.UserHomeDir()
	var markerDir string
	if err == nil {
		markerDir = filepath.Join(homeDir, ".hotplex", "sessions")
	} else {
		markerDir = filepath.Join(os.TempDir(), "hotplex_sessions")
	}
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		return nil, fmt.Errorf("create marker directory: %w", err)
	}

	return &ClaudeCodeProvider{
		ProviderBase: ProviderBase{
			meta:       meta,
			binaryPath: binaryPath,
			logger:     logger.With("provider", "claude-code"),
		},
		opts:      cfg,
		markerDir: markerDir,
	}, nil
}

// BuildCLIArgs constructs Claude Code CLI arguments.
func (p *ClaudeCodeProvider) BuildCLIArgs(providerSessionID string, opts *ProviderSessionOptions) []string {
	args := []string{
		"--print",
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	// Session management
	if opts.ResumeSession {
		args = append(args, "--resume", providerSessionID)
		p.logger.Debug("Resuming existing Claude Code session", "session_id", providerSessionID)
	} else {
		args = append(args, "--session-id", providerSessionID)
		// Create marker file for persistence detection
		markerPath := filepath.Join(p.markerDir, providerSessionID+".lock")
		if err := os.WriteFile(markerPath, []byte(""), 0644); err != nil {
			p.logger.Warn("Failed to create session marker file", "session_id", providerSessionID, "error", err)
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

// BuildEnvVars constructs environment variables for Claude Code.
func (p *ClaudeCodeProvider) BuildEnvVars(opts *ProviderSessionOptions) []string {
	env := []string{
		"CLAUDE_DISABLE_TELEMETRY=1",
	}

	// Add extra environment variables from config
	for k, v := range p.opts.ExtraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// BuildInputMessage constructs the stream-json input message.
func (p *ClaudeCodeProvider) BuildInputMessage(prompt string, taskSystemPrompt string) (map[string]any, error) {
	// Inject task-level constraints into the prompt for Hot-Multiplexing
	finalPrompt := prompt
	if taskSystemPrompt != "" {
		finalPrompt = fmt.Sprintf("[%s]\n\n%s", taskSystemPrompt, prompt)
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
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				event.Content = block.Text
				break
			}
		}

	case "tool_use":
		event.Type = EventTypeToolUse
		event.ToolName = msg.Name
		event.Status = "running"
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
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "tool_result" {
				event.ToolID = block.GetUnifiedToolID()
				event.ToolName = block.Name
				event.IsError = block.IsError
				if event.IsError {
					event.Status = "error"
				}
				break
			}
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

// ExtractSessionID extracts the Claude session ID from events.
func (p *ClaudeCodeProvider) ExtractSessionID(event *ProviderEvent) string {
	if event == nil {
		return ""
	}
	return event.SessionID
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
	return p.markerDir
}

// CheckSessionMarker checks if a session marker exists for the given ID.
func (p *ClaudeCodeProvider) CheckSessionMarker(providerSessionID string) bool {
	markerPath := filepath.Join(p.markerDir, providerSessionID+".lock")
	_, err := os.Stat(markerPath)
	return err == nil
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
