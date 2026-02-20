package hotplex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"
)

// EngineOptions defines the configuration parameters for initializing a new HotPlex Engine.
// It allows customization of timeouts, logging, and foundational security boundaries
// that apply to all sessions managed by this engine instance.
type EngineOptions struct {
	Timeout time.Duration // Maximum time to wait for a single execution turn to complete
	Logger  *slog.Logger  // Optional logger instance; defaults to slog.Default()

	// Namespace is used to generate isolated, deterministic UUID v5 Session IDs.
	// This ensures that the same Conversation ID creates an isolated but persistent sandbox,
	// preventing cross-application or cross-user session leaks.
	Namespace string

	// Foundational Security & Context (Engine-level boundaries)
	PermissionMode   string   // Controls CLI permissions (e.g., "bypass-permissions"). Defaults to strict mode.
	BaseSystemPrompt string   // Foundational instructions injected at CLI startup for all sessions.
	AllowedTools     []string // Explicit list of tools allowed (whitelist). If empty, all tools are allowed.
	DisallowedTools  []string // Explicit list of tools forbidden (blacklist).
}

// Engine is the unified process integration layer for Hot-Multiplexing.
// Configured as a long-lived Singleton, it provides a persistent execution engine that manages
// a pool of active Claude Code CLI processes, applying security rules (WAF) and routing traffic.
type Engine struct {
	opts           EngineOptions
	cliPath        string
	logger         *slog.Logger
	manager        SessionManager
	dangerDetector *Detector
	// Session stats for the last execution (thread-safe)
	statsMu      sync.RWMutex
	currentStats *SessionStats
}

// NewEngine creates a new HotPlex Engine instance.
func NewEngine(options EngineOptions) (HotPlexClient, error) {
	cliPath, err := exec.LookPath("claude")
	if err != nil {
		return nil, fmt.Errorf("claude Code CLI not found: %w", err)
	}

	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}

	if options.Namespace == "" {
		options.Namespace = "default"
	}

	// Initialize danger detector for security
	dangerDetector := NewDetector(logger)

	return &Engine{
		opts:           options,
		cliPath:        cliPath,
		logger:         logger,
		manager:        NewSessionPool(logger, 30*time.Minute, options, cliPath), // Default 30m idle timeout
		dangerDetector: dangerDetector,
	}, nil
}

// Close terminates all active sessions managed by this runner and cleans up resources.
// It triggers Graceful Shutdown by cascading termination signals down to the SessionManager,
// which drops the entire process group (PGID) to prevent zombie processes.
func (r *Engine) Close() error {
	r.logger.Info("Closing Engine and sweeping all active pgid sessions",
		"namespace", r.opts.Namespace)

	r.manager.Shutdown()

	return nil
}

// Execute runs Claude Code CLI with the given configuration and streams
func (r *Engine) Execute(ctx context.Context, cfg *Config, prompt string, callback Callback) error {
	// Security check: Detect dangerous operations before execution
	// All prompts now undergo WAF checking regardless of origin
	if dangerEvent := r.dangerDetector.CheckInput(prompt); dangerEvent != nil {
		r.logger.Warn("Dangerous operation blocked by regex firewall",
			"operation", dangerEvent.Operation,
			"reason", dangerEvent.Reason,
			"level", dangerEvent.Level,
		)
		// Send danger block event to client (non-critical - error already being returned)
		if callbackSafe := WrapSafe(r.logger, callback); callbackSafe != nil {
			_ = callbackSafe("danger_block", dangerEvent)
		}
		return ErrDangerBlocked
	}

	// Validate configuration
	if err := r.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Ensure working directory exists
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Initialize session stats for observability
	stats := &SessionStats{
		SessionID: cfg.SessionID,
		StartTime: time.Now(),
	}

	// Send thinking event
	if callbackSafe := WrapSafe(r.logger, callback); callbackSafe != nil {
		meta := &EventMeta{
			Status:          "running",
			TotalDurationMs: 0,
		}
		_ = callbackSafe("thinking", NewEventWithMeta("thinking", "ai.thinking", meta))
	}

	r.logger.Info("Engine: starting execution pipeline",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID,
	)

	// Execute via multiplexed persistent session
	if err := r.executeWithMultiplex(ctx, cfg, prompt, callback, stats); err != nil {
		r.logger.Error("Engine: execution failed",
			"namespace", r.opts.Namespace,
			"session_id", cfg.SessionID,
			"error", err)
		return err
	}

	// Finalize and save session stats
	if stats.TotalDurationMs <= 1 {
		measuredDuration := time.Since(stats.StartTime).Milliseconds()
		if measuredDuration > stats.TotalDurationMs {
			stats.TotalDurationMs = measuredDuration
		}
	}
	r.statsMu.Lock()
	r.currentStats = stats
	r.statsMu.Unlock()

	r.logger.Info("Engine: Session completed",
		"namespace", r.opts.Namespace,
		"session_id", stats.SessionID,
		"total_duration_ms", stats.TotalDurationMs,
		"tool_duration_ms", stats.ToolDurationMs,
		"tool_calls", stats.ToolCallCount,
		"tools_used", len(stats.ToolsUsed))

	return nil
}

// GetSessionStats returns a copy of the current session stats.
func (r *Engine) GetSessionStats() *SessionStats {
	r.statsMu.RLock()
	defer r.statsMu.RUnlock()

	if r.currentStats == nil {
		return nil
	}

	// Finalize any ongoing phases before copying
	return r.currentStats.FinalizeDuration()
}

// ValidateConfig validates the Config.
func (r *Engine) ValidateConfig(cfg *Config) error {
	if cfg.WorkDir == "" {
		return fmt.Errorf("%w: work_dir is required", ErrInvalidConfig)
	}
	if cfg.SessionID == "" {
		return fmt.Errorf("%w: session_id is required", ErrInvalidConfig)
	}
	return nil
}

// executeWithMultiplex uses the SessionManager for persistent process Hot-Multiplexing.
// Instead of repeatedly spawning heavy Node.js CLI processes, it looks up the deterministic SessionID.
// If missing, it performs a Cold Start. If present, it directly pipes the `prompt` via Stdin (Hot-Multiplexing).
// System prompt is injected only at cold startup; subsequent turns send user messages via stdin.
func (r *Engine) executeWithMultiplex(
	ctx context.Context,
	cfg *Config,
	prompt string,
	callback Callback,
	stats *SessionStats,
) error {
	smCfg := Config{
		WorkDir: cfg.WorkDir,
	}

	// GetOrCreateSession reuses existing process or starts a new one
	sess, err := r.manager.GetOrCreateSession(ctx, cfg.SessionID, smCfg)
	if err != nil {
		return fmt.Errorf("get or create session: %w", err)
	}

	r.logger.Info("Engine: session pipeline ready for hot-multiplexing",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID,
		"cc_session_id", sess.CCSessionID)

	// Wait for session to be ready (process fully started)
	readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer readyCancel()
	for {
		status := sess.GetStatus()
		if status == SessionStatusReady || status == SessionStatusBusy {
			break
		}
		if status == SessionStatusDead {
			return fmt.Errorf("session %s is dead, cannot execute", cfg.SessionID)
		}
		select {
		case <-readyCtx.Done():
			return fmt.Errorf("session %s not ready within 10s (status: %s)", cfg.SessionID, status)
		case <-time.After(200 * time.Millisecond):
			// poll again
		}
	}

	// Create doneChan for this turn
	doneChan := make(chan struct{})

	// Bridge callback: wraps the caller's Callback with metadata enrichment
	bridge := func(eventType string, data any) error {
		if eventType == "runner_exit" {
			select {
			case <-doneChan:
			default:
				close(doneChan)
			}
			return nil
		}

		if eventType == "raw_line" {
			line, ok := data.(string)
			if !ok {
				return nil
			}

			var msg StreamMessage
			if err := json.Unmarshal([]byte(line), &msg); err != nil {
				// Not JSON, handle gracefully
				if callbackSafe := WrapSafe(r.logger, callback); callbackSafe != nil {
					_ = callbackSafe("answer", line)
				}
				return nil
			}

			// Handle result message — extract stats and send session_stats event
			if msg.Type == "result" {
				r.handleResultMessage(msg, stats, cfg, callback)
				select {
				case <-doneChan:
				default:
					close(doneChan)
				}
				return nil
			}

			if msg.Type == "error" {
				select {
				case <-doneChan:
				default:
					close(doneChan)
				}
			}

			// Silently consume system messages (init, hooks)
			if msg.Type == "system" {
				return nil
			}

			// Dispatch all other events (assistant, tool_use, error, etc.) with metadata
			if callback != nil {
				return r.dispatchCallback(msg, callback, stats)
			}
			return nil
		}

		// Legacy path for pre-parsed
		msg, ok := data.(StreamMessage)
		if !ok {
			callbackSafe := WrapSafe(r.logger, callback)
			if callbackSafe != nil {
				_ = callbackSafe(eventType, data)
			}
			return nil
		}

		if msg.Type == "result" {
			r.handleResultMessage(msg, stats, cfg, callback)
			return nil
		}

		if msg.Type == "system" {
			return nil
		}

		if callback != nil {
			return r.dispatchCallback(msg, callback, stats)
		}
		return nil
	}

	sess.SetCallback(bridge)

	// Inject Task-level constraints into the prompt for Hot-Multiplexing
	finalPrompt := prompt
	if cfg.TaskSystemPrompt != "" {
		finalPrompt = fmt.Sprintf("[%s]\n\n%s", cfg.TaskSystemPrompt, prompt)
	}

	// Build stream-json user message payload
	msgPayload := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "text", "text": finalPrompt},
			},
		},
	}

	// Send user message to CLI stdin
	if err := sess.WriteInput(msgPayload); err != nil {
		return fmt.Errorf("write input: %w", err)
	}

	// Wait for turn completion with timeout
	timer := time.NewTimer(r.opts.Timeout)
	defer timer.Stop()

	select {
	case <-doneChan:
		// Turn completed successfully
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("execution timeout after %v", r.opts.Timeout)
	}
}

// handleResultMessage processes the result message from CLI, extracts statistics,
// and sends session_stats event to frontend.
func (r *Engine) handleResultMessage(msg StreamMessage, stats *SessionStats, cfg *Config, callback Callback) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	// Update final duration from CLI report
	if msg.Duration > 0 {
		stats.TotalDurationMs = int64(msg.Duration)
	}

	// Update token usage from CLI report
	if msg.Usage != nil {
		stats.InputTokens = msg.Usage.InputTokens
		stats.OutputTokens = msg.Usage.OutputTokens
		stats.CacheWriteTokens = msg.Usage.CacheWriteInputTokens
		stats.CacheReadTokens = msg.Usage.CacheReadInputTokens
	}

	// Collect tools used (convert map to slice)
	toolsUsed := make([]string, 0, len(stats.ToolsUsed))
	for tool := range stats.ToolsUsed {
		toolsUsed = append(toolsUsed, tool)
	}

	// Collect file paths (with deduplication)
	filePathsSet := make(map[string]bool, len(stats.FilePaths))
	for _, path := range stats.FilePaths {
		if path != "" {
			filePathsSet[path] = true
		}
	}
	filePaths := make([]string, 0, len(filePathsSet))
	for path := range filePathsSet {
		filePaths = append(filePaths, path)
	}

	// Use cost reported by CLI directly (authoritative source)
	totalCostUSD := msg.TotalCostUSD

	// Log session completion stats with explicit performance markers
	r.logger.Info("Engine: multiplexed turn completed",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID,
		"duration_ms", stats.TotalDurationMs,
		"input_tokens", stats.InputTokens,
		"output_tokens", stats.OutputTokens,
		"total_cost_usd", msg.TotalCostUSD,
		"tool_calls", stats.ToolCallCount,
		"files_modified", stats.FilesModified)

	// Send session_stats event to frontend (non-critical)
	if callback != nil {
		callbackSafe := WrapSafe(r.logger, callback)
		_ = callbackSafe("session_stats", &SessionStatsData{
			SessionID:            cfg.SessionID,
			StartTime:            stats.StartTime.Unix(),
			EndTime:              time.Now().Unix(),
			TotalDurationMs:      stats.TotalDurationMs,
			ThinkingDurationMs:   stats.ThinkingDurationMs,
			ToolDurationMs:       stats.ToolDurationMs,
			GenerationDurationMs: stats.GenerationDurationMs,
			InputTokens:          stats.InputTokens,
			OutputTokens:         stats.OutputTokens,
			CacheWriteTokens:     stats.CacheWriteTokens,
			CacheReadTokens:      stats.CacheReadTokens,
			TotalTokens:          stats.InputTokens + stats.OutputTokens,
			ToolCallCount:        stats.ToolCallCount,
			ToolsUsed:            toolsUsed,
			FilesModified:        stats.FilesModified,
			FilePaths:            filePaths,
			ModelUsed:            "claude-code",
			TotalCostUSD:         totalCostUSD,
			IsError:              msg.IsError,
			ErrorMessage:         msg.Error,
		})
	}
}

// dispatchCallback dispatches stream events to the callback with metadata.
// IMPORTANT: This function is called from stream goroutines. The callback MUST:
// 1. Return quickly (< 5 seconds) to avoid blocking stream processing
// 2. NOT call back into Session/Engine methods (risk of deadlock)
// 3. Be safe for concurrent invocation from multiple goroutines
func (r *Engine) dispatchCallback(msg StreamMessage, callback Callback, stats *SessionStats) error {
	// Skip processing if stats is nil (can happen during session warmup or reuse)
	if stats == nil {
		r.logger.Debug("dispatchCallback: stats is nil, skipping event processing",
			"type", msg.Type, "subtype", msg.Subtype)
		return nil
	}

	// Calculate total duration
	totalDuration := time.Since(stats.StartTime).Milliseconds()

	switch msg.Type {
	case "error":
		if msg.Error != "" {
			return callback("error", msg.Error)
		}
	case "system":
		// System messages (init, hook_started, hook_response) are already handled
		// by SessionMonitor for CLI readiness detection. No additional processing needed.
	case "thinking", "status":
		// Start thinking phase tracking (ended in other cases or by defer)
		stats.StartThinking()
		// Ensure thinking is ended even if we return early from this case
		// Note: if control flows to another case (tool_use, assistant), they will end thinking explicitly
		defer func() {
			stats.EndThinking()
		}()

		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				meta := &EventMeta{
					Status:          "running",
					TotalDurationMs: totalDuration,
				}
				if err := callback("thinking", NewEventWithMeta("thinking", block.Text, meta)); err != nil {
					return err
				}
			}
		}
	case "tool_use":
		// Tool use ends thinking, starts tool execution
		stats.EndThinking()

		if msg.Name != "" {
			// Extract tool ID and input from content blocks
			var toolID string
			var inputSummary string
			var filePath string
			for _, block := range msg.GetContentBlocks() {
				if block.Type == "tool_use" {
					toolID = block.ID
					if block.Input != nil {
						// Create a human-readable summary of the input
						inputSummary = SummarizeInput(block.Input)

						// Extract file path for Write/Edit operations
						if msg.Name == "Write" || msg.Name == "Edit" || msg.Name == "WriteFile" || msg.Name == "EditFile" {
							if path, ok := block.Input["path"].(string); ok {
								filePath = path
							}
						}
					}
				}
			}
			stats.RecordToolUse(msg.Name, toolID)

			// Record file modification for Write/Edit tools
			if filePath != "" {
				stats.RecordFileModification(filePath)
			}

			meta := &EventMeta{
				ToolName:        msg.Name,
				ToolID:          toolID,
				Status:          "running",
				TotalDurationMs: totalDuration,
				InputSummary:    inputSummary,
			}
			r.logger.Debug("Engine: sending tool_use event", "tool_name", msg.Name, "tool_id", toolID)
			if err := callback("tool_use", NewEventWithMeta("tool_use", msg.Name, meta)); err != nil {
				return err
			}
		}
	case "tool_result":
		if msg.Output != "" {
			durationMs := stats.RecordToolResult()

			// Extract tool ID and name from content blocks for matching with tool_use
			// tool_result blocks use tool_use_id to reference the corresponding tool_use
			var toolID string
			var toolName string
			for _, block := range msg.GetContentBlocks() {
				if block.Type == "tool_result" {
					toolID = block.GetUnifiedToolID()
					toolName = block.Name // Tool name from content block
					break
				}
			}

			meta := &EventMeta{
				ToolName:        toolName,
				ToolID:          toolID,
				Status:          "success",
				DurationMs:      durationMs,
				TotalDurationMs: totalDuration,
				OutputSummary:   TruncateString(msg.Output, 500),
			}
			r.logger.Debug("Engine: sending tool_result event", "tool_name", toolName, "tool_id", toolID, "output_length", len(msg.Output), "duration_ms", durationMs)
			if err := callback("tool_result", NewEventWithMeta("tool_result", msg.Output, meta)); err != nil {
				return err
			}
		}
	case "message", "content", "text", "delta", "assistant":
		// Assistant message starts generation phase
		r.logger.Debug("dispatchCallback: processing assistant message",
			"type", msg.Type,
			"has_message", msg.Message != nil,
			"has_direct_content", len(msg.Content) > 0,
			"blocks_count", len(msg.GetContentBlocks()))
		stats.EndThinking()
		stats.StartGeneration()

		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				if err := callback("answer", NewEventWithMeta("answer", block.Text, &EventMeta{TotalDurationMs: totalDuration})); err != nil {
					return err
				}
			} else if block.Type == "tool_use" && block.Name != "" {
				// Tool use is nested inside assistant message content
				// End generation when tool is about to be used
				stats.EndGeneration()

				r.logger.Debug("Engine: processing tool_use block", "tool_name", block.Name, "tool_id", block.ID)

				stats.RecordToolUse(block.Name, block.ID)

				// Record file modification for Write/Edit tools
				if block.Name == "Write" || block.Name == "Edit" || block.Name == "WriteFile" || block.Name == "EditFile" {
					if block.Input != nil {
						if path, ok := block.Input["path"].(string); ok {
							stats.RecordFileModification(path)
						}
					}
				}

				meta := &EventMeta{
					ToolName:        block.Name,
					ToolID:          block.ID,
					Status:          "running",
					TotalDurationMs: totalDuration,
					InputSummary:    SummarizeInput(block.Input),
				}
				if err := callback("tool_use", NewEventWithMeta("tool_use", block.Name, meta)); err != nil {
					return err
				}
				r.logger.Debug("Engine: tool_use callback completed", "tool_name", block.Name, "tool_id", block.ID)
			}
		}
	case "user":
		// Tool results come as type:"user" with nested tool_result blocks
		for _, block := range msg.GetContentBlocks() {
			if block.Type != "tool_result" {
				continue
			}

			durationMs := stats.RecordToolResult()

			// tool_result blocks use tool_use_id to reference the corresponding tool_use
			// The Name field is typically empty in tool_result blocks
			toolID := block.GetUnifiedToolID()

			meta := &EventMeta{
				ToolID:          toolID,     // Use tool_use_id for matching
				ToolName:        block.Name, // May be empty for tool_result blocks
				Status:          "success",
				DurationMs:      durationMs,
				TotalDurationMs: totalDuration,
				OutputSummary:   TruncateString(block.Content, 500),
			}
			r.logger.Debug("Engine: sending tool_result event from user message", "tool_name", block.Name, "tool_id", toolID, "tool_use_id", block.ToolUseID, "duration_ms", durationMs)
			if err := callback("tool_result", NewEventWithMeta("tool_result", block.Content, meta)); err != nil {
				return err
			}
		}
	default:
		// Log unknown message type for debugging
		r.logger.Warn("Engine: unknown message type",
			"type", msg.Type,
			"role", msg.Role,
			"name", msg.Name,
			"has_content", len(msg.Content) > 0,
			"has_message", msg.Message != nil,
			"has_error", msg.Error != "",
			"has_output", msg.Output != "")

		// Try to extract any text content (non-critical - use safe callback)
		callbackSafe := WrapSafe(r.logger, callback)
		for _, block := range msg.GetContentBlocks() {
			if block.Type == "text" && block.Text != "" {
				if callbackSafe != nil {
					_ = callbackSafe("answer", NewEventWithMeta("answer", block.Text, &EventMeta{TotalDurationMs: totalDuration}))
				}
			}
		}
	}
	return nil
}

// GetCLIVersion returns the Claude Code CLI version.
func (r *Engine) GetCLIVersion() (string, error) {
	cmd := exec.Command(r.cliPath, "--version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get CLI version: %w", err)
	}
	return string(output), nil
}

// StopSession terminates a running session by session ID.
// This is the implementation for session.stop from the spec.
func (r *Engine) StopSession(sessionID string, reason string) error {
	r.logger.Info("Engine: stopping session",
		"namespace", r.opts.Namespace,
		"session_id", sessionID,
		"reason", reason)

	return r.manager.TerminateSession(sessionID)
}

// SetDangerAllowPaths sets the allowed safe paths for the danger detector.
func (r *Engine) SetDangerAllowPaths(paths []string) {
	r.dangerDetector.SetAllowPaths(paths)
}

// SetDangerBypassEnabled enables or disables danger detection bypass.
// WARNING: Only use for Evolution mode (admin only).
func (r *Engine) SetDangerBypassEnabled(enabled bool) {
	r.dangerDetector.SetBypassEnabled(enabled)
}

// GetDangerDetector returns the danger detector instance.
func (r *Engine) GetDangerDetector() *Detector {
	return r.dangerDetector
}
