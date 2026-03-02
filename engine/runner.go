package engine

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/event"
	intengine "github.com/hrygo/hotplex/internal/engine"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/telemetry"
	"github.com/hrygo/hotplex/provider"
	"github.com/hrygo/hotplex/types"
)

// EngineOptions defines the configuration parameters for initializing a new HotPlex Engine.
// It allows customization of timeouts, logging, and foundational security boundaries
// that apply to all sessions managed by this engine instance.
type EngineOptions = intengine.EngineOptions

// Engine is the core Control Plane for AI CLI agent integration.
// Configured as a long-lived Singleton, it transforms local CLI tools into production-ready
// services by managing a hot-multiplexed process pool, enforcing security WAF rules,
// and providing a unified event-driven SDK for application integration.
type Engine struct {
	opts           EngineOptions
	cliPath        string
	logger         *slog.Logger
	provider       provider.Provider
	manager        intengine.SessionManager
	dangerDetector *security.Detector
	// mu protects runtime configuration updates (AllowedTools, DisallowedTools)
	mu sync.RWMutex
}

// NewEngine creates a new HotPlex Engine instance.
func NewEngine(options EngineOptions) (*Engine, error) {
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if options.Timeout == 0 {
		options.Timeout = 5 * time.Minute
	}
	if options.IdleTimeout == 0 {
		options.IdleTimeout = 30 * time.Minute
	}

	if options.Namespace == "" {
		options.Namespace = "default"
	}

	// Use provided Provider or create default ClaudeCodeProvider
	prv := options.Provider
	if prv == nil {
		var err error
		prv, err = provider.NewClaudeCodeProvider(provider.ProviderConfig{
			DefaultPermissionMode: options.PermissionMode,
			AllowedTools:          options.AllowedTools,
			DisallowedTools:       options.DisallowedTools,
		}, logger)
		if err != nil {
			return nil, fmt.Errorf("create default provider: %w", err)
		}
	}

	// Validate CLI binary via Provider
	cliPath, err := prv.ValidateBinary()
	if err != nil {
		return nil, fmt.Errorf("cli not found: %w", err)
	}

	// Initialize danger detector for security
	dangerDetector := security.NewDetector(logger)
	if options.AdminToken != "" {
		dangerDetector.SetAdminToken(options.AdminToken)
	}

	return &Engine{
		opts:           options,
		cliPath:        cliPath,
		logger:         logger,
		provider:       prv,
		manager:        intengine.NewSessionPool(logger, options.IdleTimeout, options, cliPath, prv),
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
func (r *Engine) Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error {
	// Security check: Detect dangerous operations before execution
	// All prompts now undergo WAF checking regardless of origin
	if dangerEvent := r.dangerDetector.CheckInput(prompt); dangerEvent != nil {
		r.logger.Warn("Dangerous operation blocked by regex firewall",
			"operation", dangerEvent.Operation,
			"reason", dangerEvent.Reason,
			"level", dangerEvent.Level,
		)
		// Send danger block event to client (non-critical - error already being returned)
		if callbackSafe := event.WrapSafe(r.logger, callback); callbackSafe != nil {
			_ = callbackSafe("danger_block", dangerEvent)
			// Track danger block in telemetry
			telemetry.GetMetrics().IncDangersBlocked()
		}
		return types.ErrDangerBlocked
	}

	// Validate configuration
	if err := r.ValidateConfig(cfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Ensure working directory exists
	if err := os.MkdirAll(cfg.WorkDir, 0755); err != nil {
		return fmt.Errorf("failed to create work directory: %w", err)
	}

	// Send initial thinking event for immediate UX feedback
	// This will be updated when CLI sends actual thinking content
	if callbackSafe := event.WrapSafe(r.logger, callback); callbackSafe != nil {
		meta := &event.EventMeta{
			Status:          "running",
			TotalDurationMs: 0,
		}
		_ = callbackSafe("thinking", event.NewEventWithMeta("thinking", "Thinking...", meta))
	}

	r.logger.Info("Engine: starting execution pipeline",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID)

	// Execute via multiplexed persistent session
	if err := r.executeWithMultiplex(ctx, cfg, prompt, callback); err != nil {
		r.logger.Error("Engine: execution failed",
			"namespace", r.opts.Namespace,
			"session_id", cfg.SessionID,
			"error", err)
		return err
	}

	r.logger.Info("Engine: Session completed",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID)

	return nil
}

// GetSessionStats returns a copy of the accumulated session stats.
func (r *Engine) GetSessionStats(sessionID string) *SessionStats {
	if r.manager == nil {
		return nil
	}
	if sess, ok := r.manager.GetSession(sessionID); ok {
		ext := sess.GetExt()
		if stats, ok := ext.(*SessionStats); ok {
			return stats.FinalizeDuration()
		}
	}
	return nil
}

// ValidateConfig validates the Config.
func (r *Engine) ValidateConfig(cfg *types.Config) error {
	if cfg.WorkDir == "" {
		return fmt.Errorf("%w: work_dir is required", types.ErrInvalidConfig)
	}
	if cfg.SessionID == "" {
		return fmt.Errorf("%w: session_id is required", types.ErrInvalidConfig)
	}

	// Security: Validate WorkDir to prevent path traversal attacks (#8)
	cleanPath := filepath.Clean(cfg.WorkDir)

	// Check for path traversal attempts - block any path containing ".."
	// Note: filepath.Clean resolves ".." in the middle of paths,
	// but we still check to catch edge cases and log attempts
	if strings.Contains(cfg.WorkDir, "..") {
		r.logger.Warn("Path traversal attempt blocked",
			"work_dir", cfg.WorkDir,
			"cleaned_path", cleanPath)
		return fmt.Errorf("%w: work_dir contains path traversal sequence", types.ErrInvalidConfig)
	}

	// Update config with cleaned path
	cfg.WorkDir = cleanPath
	return nil
}

// executeWithMultiplex uses the SessionManager for persistent process Hot-Multiplexing.
// Instead of repeatedly spawning heavy Node.js CLI processes, it looks up the deterministic SessionID.
// If missing, it performs a Cold Start. If present, it directly pipes the `prompt` via Stdin (Hot-Multiplexing).
// System prompt is injected only at cold startup; subsequent turns send user messages via stdin.
func (r *Engine) executeWithMultiplex(
	ctx context.Context,
	cfg *types.Config,
	prompt string,
	callback event.Callback,
) error {
	// Convert to engine session config
	sessionCfg := intengine.SessionConfig{
		WorkDir:          cfg.WorkDir,
		TaskInstructions: cfg.TaskInstructions,
	}

	// GetOrCreateSession reuses existing process or starts a new one
	sess, created, err := r.manager.GetOrCreateSession(ctx, cfg.SessionID, sessionCfg, prompt)
	if err != nil {
		return fmt.Errorf("get or create session: %w", err)
	}

	// Send session lifecycle events for UX feedback
	// These events provide immediate feedback to users about session state
	if callback != nil {
		callbackSafe := event.WrapSafe(r.logger, callback)

		// Send session_start event on Cold Start (new session creation)
		if created && callbackSafe != nil {
			r.logger.Info("Engine: sending session_start event for cold start",
				"namespace", r.opts.Namespace,
				"session_id", cfg.SessionID,
				"resuming", sess.IsResuming)

			startContent := "Starting new session..."
			initContent := "Initializing AI engine..."
			if sess.IsResuming {
				startContent = "Resuming session..."
				initContent = "Re-initializing AI engine..."
			}

			if err := callbackSafe("session_start", event.NewEventWithMeta(
				"session_start",
				startContent,
				&event.EventMeta{Status: "starting"},
			)); err != nil {
				r.logger.Error("Failed to send session_start event", "error", err)
			}

			// Send engine_starting event to indicate CLI is being initialized
			if err := callbackSafe("engine_starting", event.NewEventWithMeta(
				"engine_starting",
				initContent,
				&event.EventMeta{Status: "initializing"},
			)); err != nil {
				r.logger.Error("Failed to send engine_starting event", "error", err)
			}
		} else {
			r.logger.Debug("Skipping session_start/engine_starting events (Hot-Multiplexing or no callbackSafe)",
				"created", created,
				"callbackSafe", callbackSafe != nil)
		}
	}

	// Initialize or fetch persistent SessionStats
	var stats *SessionStats
	if ext := sess.GetExt(); ext != nil {
		if s, ok := ext.(*SessionStats); ok {
			stats = s
		} else {
			// Type mismatch, create new stats
			stats = &SessionStats{
				SessionID: cfg.SessionID,
				StartTime: time.Now(),
				ToolsUsed: make(map[string]bool),
				FilePaths: make([]string, 0),
			}
			sess.SetExt(stats)
		}
	} else {
		stats = &SessionStats{
			SessionID: cfg.SessionID,
			StartTime: time.Now(),
			ToolsUsed: make(map[string]bool),
			FilePaths: make([]string, 0),
		}
		sess.SetExt(stats)
	}

	r.logger.Info("Engine: session pipeline ready for hot-multiplexing",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID,
		"provider_session_id", sess.ProviderSessionID)

	// Update or reuse persistent instructions
	if cfg.TaskInstructions != "" {
		sess.TaskInstructions = cfg.TaskInstructions
	} else {
		cfg.TaskInstructions = sess.TaskInstructions
	}

	if err := r.waitForSession(ctx, sess, cfg.SessionID); err != nil {
		return err
	}

	// Create doneChan for this turn
	doneChan := make(chan struct{})

	sess.SetCallback(intengine.Callback(r.createEventBridge(cfg, callback, stats, doneChan)))

	// Build provider-specific input message payload
	// 2. Send input - Skip if this was a cold start and the provider handles initial prompt as CLI args
	if created && r.provider.Metadata().Features.RequiresInitialPromptAsArg {
		r.logger.Debug("Skipping Stdin injection for cold-start (already passed via CLI args)",
			"namespace", r.opts.Namespace,
			"session_id", cfg.SessionID)
	} else {
		input, err := r.provider.BuildInputMessage(prompt, cfg.TaskInstructions)
		if err != nil {
			return fmt.Errorf("build input message: %w", err)
		}
		if err := sess.WriteInput(input); err != nil {
			return fmt.Errorf("write input: %w", err)
		}
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

func (r *Engine) waitForSession(ctx context.Context, sess *intengine.Session, sessionID string) error {
	for {
		status := sess.GetStatus()
		if status == intengine.SessionStatusReady || status == intengine.SessionStatusBusy {
			return nil
		}
		if status == intengine.SessionStatusDead {
			return fmt.Errorf("session %s is dead, cannot execute", sessionID)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case s := <-sess.GetStatusChange():
			if s == intengine.SessionStatusReady || s == intengine.SessionStatusBusy {
				return nil
			}
			if s == intengine.SessionStatusDead {
				return fmt.Errorf("session %s is dead, cannot execute", sessionID)
			}
		}
	}
}

func (r *Engine) createEventBridge(cfg *types.Config, callback event.Callback, stats *SessionStats, doneChan chan struct{}) event.Callback {
	return func(eventType string, data any) error {
		if eventType == "runner_exit" {
			closeDoneChan(doneChan)
			return nil
		}

		if eventType == "raw_line" {
			line, ok := data.(string)
			if !ok {
				return nil
			}
			return r.handleStreamRawLine(line, cfg, stats, callback, doneChan)
		}

		// Fallback for custom events passed directly to the bridge
		if callbackSafe := event.WrapSafe(r.logger, callback); callbackSafe != nil {
			_ = callbackSafe(eventType, data)
		}

		return nil
	}
}

func (r *Engine) handleStreamRawLine(line string, cfg *types.Config, stats *SessionStats, callback event.Callback, doneChan chan struct{}) error {
	// Use Provider to parse the raw line into normalized ProviderEvents
	pevtSlice, err := r.provider.ParseEvent(line)
	if err != nil {
		r.logger.Warn("Engine: provider failed to parse event", "error", err, "line", line)
		// Fallback: send as raw answer if parsing fails
		if callbackSafe := event.WrapSafe(r.logger, callback); callbackSafe != nil {
			_ = callbackSafe("answer", line)
		}
		return nil
	}

	// Diagnostic: log raw CLI JSON types for protocol analysis
	for _, p := range pevtSlice {
		if p.Type == provider.EventTypeResult {
			r.logger.Debug("[RUNNER] result event metadata",
				"input_tokens", p.Metadata.InputTokens,
				"output_tokens", p.Metadata.OutputTokens,
				"raw_line", p.RawLine)
		} else {
			r.logger.Debug("[RUNNER] parsed raw line", "raw_type", p.RawType, "normalized_type", p.Type, "tool_name", p.ToolName)
		}
	}

	for _, pevt := range pevtSlice {
		// Detect if this event indicates the turn is over
		if r.provider.DetectTurnEnd(pevt) {
			r.handleNormalizedResult(pevt, stats, cfg, callback)
			closeDoneChan(doneChan)
			return nil
		}

		if pevt.Type == provider.EventTypeError {
			closeDoneChan(doneChan)
		}

		if pevt.Type == provider.EventTypeSystem || pevt.Type == provider.EventTypeRaw {
			continue // Skip system/raw events but continue with others in the same line
		}

		if callback != nil {
			if err := r.dispatchNormalizedCallback(pevt, callback, stats); err != nil {
				return err
			}
		}
	}
	return nil
}

func closeDoneChan(doneChan chan struct{}) {
	select {
	case <-doneChan:
	default:
		close(doneChan)
	}
}

// handleNormalizedResult processes the final turn event and finalizes stats
func (r *Engine) handleNormalizedResult(pevt *provider.ProviderEvent, stats *SessionStats, cfg *types.Config, callback event.Callback) {
	stats.mu.Lock()
	defer stats.mu.Unlock()

	// Update final duration and tokens from provider metadata if available
	// Accumulate stats (+=) across hot-multiplexed turns
	if pevt.Metadata != nil {
		if pevt.Metadata.TotalDurationMs > 0 {
			stats.TotalDurationMs += pevt.Metadata.TotalDurationMs
		}
		stats.InputTokens += pevt.Metadata.InputTokens
		stats.OutputTokens += pevt.Metadata.OutputTokens
		stats.CacheWriteTokens += pevt.Metadata.CacheWriteTokens
		stats.CacheReadTokens += pevt.Metadata.CacheReadTokens
	}

	// Prepare final telemetry package
	toolsUsed := make([]string, 0, len(stats.ToolsUsed))
	for tool := range stats.ToolsUsed {
		toolsUsed = append(toolsUsed, tool)
	}

	// Deduplicate file paths
	filePathsSet := make(map[string]bool)
	for _, p := range stats.FilePaths {
		if p != "" {
			filePathsSet[p] = true
		}
	}
	filePaths := make([]string, 0, len(filePathsSet))
	for p := range filePathsSet {
		filePaths = append(filePaths, p)
	}

	costUSD := 0.0
	if pevt.Metadata != nil {
		costUSD = pevt.Metadata.TotalCostUSD
	}

	r.logger.Info("Engine: turn completed with normalized provider event",
		"namespace", r.opts.Namespace,
		"session_id", cfg.SessionID,
		"duration_ms", stats.TotalDurationMs,
		"input_tokens", stats.InputTokens,
		"output_tokens", stats.OutputTokens,
		"cost_usd", costUSD)

	// Dispatch stats event
	if callback != nil {
		callbackSafe := event.WrapSafe(r.logger, callback)
		_ = callbackSafe("session_stats", &event.SessionStatsData{
			SessionID:       cfg.SessionID,
			StartTime:       stats.StartTime.Unix(),
			EndTime:         time.Now().Unix(),
			TotalDurationMs: stats.TotalDurationMs,
			InputTokens:     stats.InputTokens,
			OutputTokens:    stats.OutputTokens,
			TotalTokens:     stats.InputTokens + stats.OutputTokens,
			ToolCallCount:   stats.ToolCallCount,
			ToolsUsed:       toolsUsed,
			FilesModified:   stats.FilesModified,
			FilePaths:       filePaths,
			ModelUsed:       r.provider.Name(),
			TotalCostUSD:    costUSD,
			IsError:         pevt.IsError,
			ErrorMessage:    pevt.Error,
		})
	}

	// Set session back to Ready
	if sess, ok := r.manager.GetSession(cfg.SessionID); ok {
		sess.SetStatus(intengine.SessionStatusReady)
	}
}

// dispatchNormalizedCallback dispatches normalized provider events to the client callback
func (r *Engine) dispatchNormalizedCallback(pevt *provider.ProviderEvent, callback event.Callback, stats *SessionStats) error {
	if stats == nil {
		return nil
	}

	// Diagnostic: log ALL events entering dispatch
	r.logger.Debug("[RUNNER] dispatchNormalizedCallback",
		"event_type", pevt.Type,
		"content_len", len(pevt.Content),
		"tool_name", pevt.ToolName)

	totalDur := time.Since(stats.StartTime).Milliseconds()

	// Debug: Log tool events
	if pevt.Type == provider.EventTypeToolUse || pevt.Type == provider.EventTypeToolResult {
		r.logger.Debug("[RUNNER] Dispatching tool event",
			"type", pevt.Type,
			"tool_name", pevt.ToolName,
			"tool_id", pevt.ToolID,
			"status", pevt.Status)
	}

	switch pevt.Type {
	case provider.EventTypeThinking:
		stats.StartThinking()
		defer stats.EndThinking()
		meta := &event.EventMeta{Status: "running", TotalDurationMs: totalDur}
		r.logger.Debug("[RUNNER] Dispatching thinking event", "content", pevt.Content)
		return callback("thinking", event.NewEventWithMeta("thinking", pevt.Content, meta))

	case provider.EventTypeToolUse:
		stats.EndThinking()
		stats.RecordToolUse(pevt.ToolName, pevt.ToolID)
		// Track tool invocation in telemetry
		telemetry.GetMetrics().IncToolsInvoked()
		meta := &event.EventMeta{
			ToolName:        pevt.ToolName,
			ToolID:          pevt.ToolID,
			Status:          "running",
			TotalDurationMs: totalDur,
			InputSummary:    types.SummarizeInput(pevt.ToolInput),
		}
		return callback("tool_use", event.NewEventWithMeta("tool_use", pevt.ToolName, meta))

	case provider.EventTypeToolResult:
		// Get tool ID for reliable tool name lookup
		toolID := pevt.ToolID
		if toolID == "" {
			toolID = stats.GetCurrentToolID()
		}

		// RecordToolResult now returns both duration and toolName
		// It uses toolID map for reliable lookup when tool_result doesn't include tool_name
		dur, statsToolName := stats.RecordToolResult(toolID)

		// Use provider's tool_name if available, otherwise fallback to stats
		toolName := pevt.ToolName
		if toolName == "" {
			toolName = statsToolName
		}

		// Final fallback: try to get tool name from toolID map
		if toolName == "" && toolID != "" {
			toolName = stats.GetToolNameByToolID(toolID)
		}

		// Default status to "success" if not set
		status := pevt.Status
		if status == "" {
			status = "success"
		}

		r.logger.Debug("[RUNNER] tool_result resolved",
			"tool_name", toolName,
			"stats_tool_name", statsToolName,
			"provider_tool_name", pevt.ToolName,
			"tool_id", toolID,
			"status", status,
			"duration_ms", dur)

		meta := &event.EventMeta{
			ToolName:        toolName,
			ToolID:          toolID,
			Status:          status,
			DurationMs:      dur,
			TotalDurationMs: totalDur,
			OutputSummary:   types.TruncateString(pevt.Content, 500),
		}
		return callback("tool_result", event.NewEventWithMeta("tool_result", pevt.Content, meta))

	case provider.EventTypeAnswer:
		stats.EndThinking()
		stats.StartGeneration()
		meta := &event.EventMeta{TotalDurationMs: totalDur}
		return callback("answer", event.NewEventWithMeta("answer", pevt.Content, meta))

	case provider.EventTypeError:
		return callback("error", pevt.Error)

	case provider.EventTypePermissionRequest:
		// Permission request from Claude Code - route to callback for Slack display
		r.logger.Info("[RUNNER] Permission request received",
			"session_id", pevt.SessionID,
			"tool_name", pevt.ToolName,
			"content_len", len(pevt.Content))
		meta := &event.EventMeta{
			ToolName:        pevt.ToolName,
			ToolID:          pevt.ToolID, // Contains message_id for correlation
			Status:          "pending",
			TotalDurationMs: totalDur,
		}
		// Pass the raw JSON for full permission data access
		return callback("permission_request", event.NewEventWithMeta("permission_request", pevt.RawLine, meta))

	case provider.EventTypePlanMode:
		// Plan Mode event - Claude is generating a plan
		r.logger.Info("[RUNNER] Plan mode event received",
			"session_id", pevt.SessionID,
			"content_len", len(pevt.Content))
		meta := &event.EventMeta{TotalDurationMs: totalDur}
		return callback("plan_mode", event.NewEventWithMeta("plan_mode", pevt.Content, meta))

	case provider.EventTypeExitPlanMode:
		// Exit Plan Mode - Claude requests approval to execute plan
		r.logger.Info("[RUNNER] Exit plan mode event received",
			"session_id", pevt.SessionID,
			"tool_name", pevt.ToolName)
		meta := &event.EventMeta{
			ToolName:        pevt.ToolName,
			ToolID:          pevt.ToolID,
			TotalDurationMs: totalDur,
		}
		return callback("exit_plan_mode", event.NewEventWithMeta("exit_plan_mode", pevt.Content, meta))

	case provider.EventTypeAskUserQuestion:
		// AskUserQuestion - Claude requests clarification (degraded to text prompt)
		r.logger.Info("[RUNNER] Ask user question event received",
			"session_id", pevt.SessionID,
			"tool_name", pevt.ToolName)
		meta := &event.EventMeta{
			ToolName:        pevt.ToolName,
			ToolID:          pevt.ToolID,
			TotalDurationMs: totalDur,
		}
		return callback("ask_user_question", event.NewEventWithMeta("ask_user_question", pevt.Content, meta))

	default:
		// Fallback for other event types
		if pevt.Content != "" {
			meta := &event.EventMeta{TotalDurationMs: totalDur}
			return callback("answer", event.NewEventWithMeta("answer", pevt.Content, meta))
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

// ResetSessionProvider marks a session to get a new ProviderSessionID on restart.
// This is used for /clear command to force a fresh session with new context.
func (r *Engine) ResetSessionProvider(sessionID string) {
	if pool, ok := r.manager.(*intengine.SessionPool); ok {
		pool.ResetProviderSessionID(sessionID)
		r.logger.Info("Engine: marked session for ProviderSessionID reset",
			"session_id", sessionID)
	}
}

// CleanupSessionFiles deletes all session files associated with the provider session.
// This handles the complete removal of context on disk for commands like /reset.
func (r *Engine) CleanupSessionFiles(sessionID string) error {
	if pool, ok := r.manager.(*intengine.SessionPool); ok {
		if sess, exists := pool.GetSession(sessionID); exists {
			// Also delete the hotplex lock/marker so it won't be resumed next time
			if err := pool.DeleteMarker(sess.ProviderSessionID); err != nil {
				r.logger.Warn("Failed to delete session marker", "error", err)
			}
			return pool.CleanupSessionFiles(sess.ProviderSessionID, sess.Config.WorkDir)
		}
	}
	return nil
}

// GetSession retrieves an active session by sessionID.
// Returns the session and true if found, or nil and false if not found.
func (r *Engine) GetSession(sessionID string) (*intengine.Session, bool) {
	if r.manager == nil {
		return nil, false
	}
	return r.manager.GetSession(sessionID)
}

// SetDangerAllowPaths sets the allowed safe paths for the danger detector.
func (r *Engine) SetDangerAllowPaths(paths []string) {
	r.dangerDetector.SetAllowPaths(paths)
}

// SetDangerBypassEnabled enables or disables danger detection bypass.
// WARNING: Only use for Evolution mode (admin only).
func (r *Engine) SetDangerBypassEnabled(token string, enabled bool) error {
	return r.dangerDetector.SetBypassEnabled(token, enabled)
}

// Runtime security configuration update methods for Engine.
// These allow hot reloading of AllowedTools and DisallowedTools at runtime.
// All methods are protected by Engine's internal mutex for concurrent safety.

// SetAllowedTools sets the allowed tools for the engine.
// This affects new sessions created after the call.
// Thread-safe: uses internal mutex.
func (r *Engine) SetAllowedTools(tools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.opts.AllowedTools = tools
	r.logger.Info("Allowed tools updated", "tools", tools)
}

// SetDisallowedTools sets the disallowed tools for the engine.
// This affects new sessions created after the call.
// Thread-safe: uses internal mutex.
func (r *Engine) SetDisallowedTools(tools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.opts.DisallowedTools = tools
	r.logger.Info("Disallowed tools updated", "tools", tools)
}

// GetAllowedTools returns the current allowed tools list.
// Thread-safe: uses internal mutex.
func (r *Engine) GetAllowedTools() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.opts.AllowedTools
}

// GetDisallowedTools returns the current disallowed tools list.
// Thread-safe: uses internal mutex.
func (r *Engine) GetDisallowedTools() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.opts.DisallowedTools
}
