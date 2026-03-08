package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/internal"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
	intengine "github.com/hrygo/hotplex/internal/engine"
	"github.com/hrygo/hotplex/provider"
	"github.com/hrygo/hotplex/types"
)

const (
	// Metadata keys
	MetadataKeyTransient = "is_transient"
)

// UI Status indicator labels for AI Native experience
const (
	StatusThinkingLabel       = "🧠 深度推演规划中..."
	StatusToolExecutingLabel  = "🛠️ 正在执行工具 [%s]..."
	StatusResultParsingLabel  = "🧠 正在解析执行结果..."
	StatusErrorLabel          = "❌ 执行过程中发生错误"
	StatusDangerBlockLabel    = "⚠️ 拦截到高危操作，等待确认..."
	StatusProgressLabel       = "⏳ 正在执行后台任务..."
	StatusProgressDoneLabel   = "✅ 后台任务执行完毕"
	StatusStepStartLabel      = "🔍 正在分析执行轨迹..."
	StatusStepFinishLabel     = "✅ 当前任务阶段构建完成"
	StatusPlanModeLabel       = "📝 正在制定作战计划..."
	StatusExitPlanModeLabel   = "📝 作战计划就绪，等待您的批准..."
	StatusAskUserLabel        = "⏳ 等待您提供更多信息..."
	StatusEngineStartingLabel = "🚀 正在唤醒推演引擎..."
	StatusPermissionLabel     = "🛡️ 拦截到高危操作，等待提权审批..."

	// Contextual labels
	StatusToolResultThinkingLabel = "🧠 正在解析执行结果 (耗时: %dms)..."
	StatusSessionStartColdLabel   = "🚀 正在初始化上下文..."
	StatusSessionStartHotLabel    = "🚀 重新连接并恢复上下文..."
	StatusAnswerLabel             = "✍️ 正在生成回答..."
)

// sessionWrapper wraps intengine.Session to implement chatapps.Session interface
type sessionWrapper struct {
	sess *intengine.Session
}

func (w *sessionWrapper) ID() string {
	if w.sess == nil {
		return ""
	}
	return w.sess.ID
}

func (w *sessionWrapper) Status() string {
	if w.sess == nil {
		return ""
	}
	return string(w.sess.GetStatus())
}

func (w *sessionWrapper) CreatedAt() time.Time {
	if w.sess == nil {
		return time.Time{}
	}
	return w.sess.CreatedAt
}

func (w *sessionWrapper) IsResumed() bool {
	if w.sess == nil {
		return false
	}
	return w.sess.IsResuming
}

// engineWrapper wraps engine.Engine to implement chatapps.Engine interface
type engineWrapper struct {
	eng *engine.Engine
}

func (w *engineWrapper) Execute(ctx context.Context, cfg *types.Config, prompt string, callback event.Callback) error {
	return w.eng.Execute(ctx, cfg, prompt, callback)
}

func (w *engineWrapper) CheckDanger(prompt string) (blocked bool, operation, reason string) {
	return w.eng.CheckDanger(prompt)
}

func (w *engineWrapper) GetSession(sessionID string) (Session, bool) {
	sess, ok := w.eng.GetSession(sessionID)
	if !ok || sess == nil {
		return nil, false
	}
	return &sessionWrapper{sess: sess}, true
}

func (w *engineWrapper) Close() error {
	return w.eng.Close()
}

func (w *engineWrapper) GetSessionStats(sessionID string) *SessionStats {
	stats := w.eng.GetSessionStats(sessionID)
	if stats == nil {
		return nil
	}
	return &SessionStats{
		SessionID:     stats.SessionID,
		Status:        stats.SessionID,
		TotalTokens:   int64(stats.InputTokens + stats.OutputTokens + stats.CacheReadTokens + stats.CacheWriteTokens),
		InputTokens:   int64(stats.InputTokens),
		OutputTokens:  int64(stats.OutputTokens),
		CacheRead:     int64(stats.CacheReadTokens),
		CacheWrite:    int64(stats.CacheWriteTokens),
		TotalCost:     0,
		Duration:      time.Duration(stats.TotalDurationMs) * time.Millisecond,
		ToolCallCount: int(stats.ToolCallCount),
		ErrorCount:    0,
	}
}

func (w *engineWrapper) ValidateConfig(cfg *types.Config) error {
	return w.eng.ValidateConfig(cfg)
}

func (w *engineWrapper) StopSession(sessionID string, reason string) error {
	return w.eng.StopSession(sessionID, reason)
}

func (w *engineWrapper) ResetSessionProvider(sessionID string) {
	w.eng.ResetSessionProvider(sessionID)
}

func (w *engineWrapper) SetDangerAllowPaths(paths []string) {
	w.eng.SetDangerAllowPaths(paths)
}

func (w *engineWrapper) SetDangerBypassEnabled(token string, enabled bool) error {
	return w.eng.SetDangerBypassEnabled(token, enabled)
}

func (w *engineWrapper) SetAllowedTools(tools []string) {
	w.eng.SetAllowedTools(tools)
}

func (w *engineWrapper) SetDisallowedTools(tools []string) {
	w.eng.SetDisallowedTools(tools)
}

func (w *engineWrapper) GetAllowedTools() []string {
	return w.eng.GetAllowedTools()
}

func (w *engineWrapper) GetDisallowedTools() []string {
	return w.eng.GetDisallowedTools()
}

func (w *engineWrapper) GetOptions() engine.EngineOptions {
	return w.eng.GetOptions()
}

// EngineHolder holds the Engine instance and configuration for ChatApps integration
type EngineHolder struct {
	engine           Engine
	logger           *slog.Logger
	adapters         *AdapterManager
	defaultWorkDir   string
	defaultTaskInstr string
}

// NewEngineHolder creates a new EngineHolder with the given options
func NewEngineHolder(opts EngineHolderOptions) (*EngineHolder, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.IdleTimeout == 0 {
		opts.IdleTimeout = 30 * time.Minute
	}

	engineOpts := engine.EngineOptions{
		Timeout:                    opts.Timeout,
		IdleTimeout:                opts.IdleTimeout,
		Namespace:                  opts.Namespace,
		PermissionMode:             opts.PermissionMode,
		DangerouslySkipPermissions: opts.DangerouslySkipPermissions,
		AllowedTools:               opts.AllowedTools,
		DisallowedTools:            opts.DisallowedTools,
		Logger:                     logger,
	}

	eng, err := engine.NewEngine(engineOpts)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	// Wrap engine.Engine to implement chatapps.Engine interface
	wrappedEngine := &engineWrapper{eng: eng}

	return &EngineHolder{
		engine:           wrappedEngine,
		logger:           logger,
		adapters:         opts.Adapters,
		defaultWorkDir:   opts.DefaultWorkDir,
		defaultTaskInstr: opts.DefaultTaskInstr,
	}, nil
}

// EngineHolderOptions configures the EngineHolder
type EngineHolderOptions struct {
	Logger                     *slog.Logger
	Adapters                   *AdapterManager
	Timeout                    time.Duration
	IdleTimeout                time.Duration
	Namespace                  string
	PermissionMode             string
	DangerouslySkipPermissions bool
	AllowedTools               []string
	DisallowedTools            []string
	DefaultWorkDir             string
	DefaultTaskInstr           string
}

// GetEngine returns the underlying Engine instance
func (h *EngineHolder) GetEngine() Engine {
	return h.engine
}

// GetAdapterManager returns the AdapterManager for sending messages
func (h *EngineHolder) GetAdapterManager() *AdapterManager {
	return h.adapters
}

// StreamCallback implements event.Callback to receive Engine events and forward to ChatApp
type StreamCallback struct {
	ctx       context.Context
	sessionID string
	platform  string
	adapters  *AdapterManager
	logger    *slog.Logger
	engine    Engine
	mu        sync.Mutex
	isHot     bool // Tracks if session is hot-multiplexed (active)
	isFirst   bool
	metadata  map[string]any  // Original message metadata (channel_id, thread_ts, etc.)
	processor *ProcessorChain // Message processor chain

	// Session lifecycle state - ensures correct event ordering
	sessionStartSent bool // Tracks if session_start event has been sent

	lastToolName string // Track the last tool name for error context

	// Status message state for dynamic event type indicator
	currentStatus base.MessageType // Current status type (thinking, tool_use, answer)

	lastStatusUpdate time.Time // Throttle tracker for thinking/status messages

	// Cleanup records for sliding window management and final deletion
	cleanupMsgRecords []msgRecord

	// Platform-specific operations (dependency injection for testability and platform agnosticism)
	messageOps MessageOperations
	sessionOps SessionOperations
	statusMgr  *internal.StatusManager // Status notification manager

	// Native streaming state - platform-agnostic streaming output
	streamWriter       base.StreamWriter // Platform-agnostic streaming writer
	streamWriterActive bool              // Whether native streaming is active

	// Fallback mechanism - accumulate content when streaming unavailable
	accumulatedContent strings.Builder // Accumulated answer content for fallback

	// Idle state detection
	idleTimer  *time.Timer
	isFinished bool
}

// Close releases resources held by the callback
func (c *StreamCallback) Close() {
	if c.processor != nil {
		c.processor.Close()
	}

	// Important: Finalize native stream and stop idle timer
	c.mu.Lock()
	c.isFinished = true
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	writer := c.streamWriter
	c.mu.Unlock()

	if writer != nil {
		if err := writer.Close(); err != nil {
			c.logger.Warn("Failed to close stream writer", "error", err)
		}
	}
}

// msgRecord tracks a sent message for later deletion or sliding window management.
type msgRecord struct {
	ChannelID string
	MessageTS string
	EventType string // Event type (tool_use, session_start, etc.) to protection markers
}

// NewStreamCallback creates a new StreamCallback with injected platform-specific operations
func NewStreamCallback(
	ctx context.Context,
	sessionID string,
	platform string,
	adapters *AdapterManager,
	logger *slog.Logger,
	engine Engine,
	isHot bool,
	metadata map[string]any,
	messageOps MessageOperations,
	sessionOps SessionOperations,
) *StreamCallback {
	cb := &StreamCallback{
		ctx:              ctx,
		sessionID:        sessionID,
		platform:         platform,
		adapters:         adapters,
		logger:           logger,
		engine:           engine,
		isHot:            isHot,
		isFirst:          true,
		metadata:         metadata,
		messageOps:       messageOps,
		sessionOps:       sessionOps,
		lastStatusUpdate: time.Now(),
		processor:        NewDefaultProcessorChain(ctx, logger),
	}

	// Initialize StatusManager if StatusProvider is available
	if adapters != nil {
		if statusProvider := adapters.GetStatusProvider(platform); statusProvider != nil {
			cb.statusMgr = internal.NewStatusManager(statusProvider, logger)
			logger.Debug("StatusManager initialized", "platform", platform)
		}
	}

	return cb
}

func (c *StreamCallback) getEngine() Engine {
	return c.engine
}

// resetIdleTimer resets the 3-second generic thinking fallback timer
func (c *StreamCallback) resetIdleTimer() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isFinished {
		return
	}

	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}

	c.idleTimer = time.AfterFunc(3*time.Second, func() {
		c.mu.Lock()
		finished := c.isFinished
		c.mu.Unlock()

		if finished {
			return
		}

		c.logger.Debug("Session idle for 3s, sending fallback thinking status", "session_id", c.sessionID)
		if err := c.updateStatusMessage(base.MessageTypeThinking, StatusThinkingLabel); err != nil {
			c.logger.Warn("Failed to update status for idle thinking fallback", "error", err)
		}
	})
}

// Handle implements event.Callback
func (c *StreamCallback) Handle(eventType string, data any) error {
	// Note: No mutex lock here - individual handlers manage their own locking
	// This prevents deadlock when handlers need to access shared state

	// Reset idle timer on every event
	c.resetIdleTimer()

	switch provider.ProviderEventType(eventType) {
	case provider.EventTypeThinking:
		return c.handleThinking(data)
	case provider.EventTypeToolUse:
		return c.handleToolUse(data)
	case provider.EventTypeToolResult:
		return c.handleToolResult(data)
	case provider.EventTypeAnswer:
		return c.handleAnswer(data)
	case provider.EventTypeError:
		return c.handleError(data)
	case provider.EventTypePlanMode:
		return c.handlePlanMode(data)
	case provider.EventTypeExitPlanMode:
		return c.handleExitPlanMode(data)
	case provider.EventTypeAskUserQuestion:
		return c.handleAskUserQuestion(data)
	case provider.EventTypeResult:
		return c.handleSessionStats(data)
	case provider.EventTypeCommandProgress:
		return c.handleCommandProgress(data)
	case provider.EventTypeCommandComplete:
		return c.handleCommandComplete(data)
	case provider.EventTypeSystem:
		// [Black Hole] Absolute silent drop: internal system messages are noise for developers
		return nil
	case provider.EventTypeUser:
		// [Black Hole] Absolute silent drop: user message reflection is redundant on Slack
		return nil
	case provider.EventTypeStepStart:
		return c.handleStepStart(data)
	case provider.EventTypeStepFinish:
		return c.handleStepFinish(data)
	case provider.EventTypeRaw:
		// [Black Hole] Absolute silent drop: unparsed raw output is noise for developers
		return nil
	case provider.EventTypeSessionStart:
		return c.handleSessionStart(data)
	case provider.EventTypeEngineStarting:
		return c.handleEngineStarting(data)
	case provider.EventTypeUserMessageReceived:
		// [Black Hole] Absolute silent drop: no "message received" acknowledgment needed for CLI agents
		return nil
	case provider.EventTypePermissionRequest:
		return c.handlePermissionRequest(data)
	default:
		// Check for specific engine/extended events
		if eventType == "danger_block" {
			return c.handleDangerBlock(data)
		}
		if eventType == "session_stats" {
			return c.handleSessionStats(data)
		}
		c.logger.Debug("Ignoring unknown event", "type", eventType)
	}
	return nil
}

func (c *StreamCallback) handleThinking(data any) error {
	var content string
	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
	}

	// Apply default status if empty
	if content == "" {
		content = StatusThinkingLabel
	} else if !strings.HasPrefix(content, "🧠") {
		content = "🧠 " + content
	}

	return c.updateStatusMessage(base.MessageTypeThinking, content)
}

// sendMessageAndGetTS sends a message and populates message_ts in metadata
func (c *StreamCallback) sendMessageAndGetTS(msg *ChatMessage) error {
	if c.adapters == nil {
		c.logger.Debug("No adapters, skipping message send", "platform", c.platform)
		return nil
	}

	// Process message through processor chain
	processedMsg, err := c.processor.Process(c.ctx, msg)
	if err != nil {
		c.logger.Error("Message processing failed",
			"platform", c.platform,
			"session_id", c.sessionID,
			"error", err)
		processedMsg = msg
	}

	if processedMsg == nil {
		c.logger.Debug("Message dropped by processor",
			"platform", c.platform,
			"session_id", c.sessionID)
		return nil
	}

	// Send the message - adapter will populate message_ts in metadata
	if err := c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, processedMsg); err != nil {
		return err
	}

	// Automatic tracking via zone metadata (Thinking or Action zones)
	c.trackMessage(processedMsg)

	// Copy message_ts back to original msg metadata if present in processedMsg
	// Use mutex for thread-safe metadata modification
	if processedMsg.Metadata != nil {
		ts, ok1 := processedMsg.Metadata["message_ts"].(string)
		channelID, ok2 := processedMsg.Metadata["channel_id"].(string)

		if (ok1 && ts != "") || (ok2 && channelID != "") {
			c.mu.Lock()
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]any)
			}
			if ok1 && ts != "" {
				msg.Metadata["message_ts"] = ts
			}
			if ok2 && channelID != "" {
				msg.Metadata["channel_id"] = channelID
			}
			c.mu.Unlock()
		}
	}

	return nil
}

// buildChatMessage creates a ChatMessage with merged metadata and sends it
// Helper function to reduce duplication in event handlers
func (c *StreamCallback) buildChatMessage(msgType base.MessageType, content string, extraMetadata map[string]any) error {
	msg := &base.ChatMessage{
		Type:     msgType,
		Content:  content,
		Metadata: extraMetadata,
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(c.convertToChatMessage(msg))
}

// setReaction sets a reaction on the user's trigger message.
// Removes previous reaction before adding new one for clean status transitions.

// trackMessage records a message for sliding window management and final cleanup.
// Only tracks transient messages that should be auto-deleted at session end.
// Transient status is determined by the "is_transient" metadata flag.
func (c *StreamCallback) trackMessage(msg *base.ChatMessage) {
	if msg == nil || msg.Metadata == nil {
		return
	}

	// Only track messages explicitly marked as transient.
	isTransient, _ := msg.Metadata[MetadataKeyTransient].(bool)
	if !isTransient {
		return
	}

	ts, _ := msg.Metadata["message_ts"].(string)
	ch, _ := msg.Metadata["channel_id"].(string)
	eventType, _ := msg.Metadata["event_type"].(string)
	if ts == "" || ch == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if this TS is already tracked to handle in-place updates gracefully
	for _, rec := range c.cleanupMsgRecords {
		if rec.MessageTS == ts {
			return
		}
	}

	c.logger.Debug("Tracking transient message for cleanup", "ts", ts, "type", eventType)
	// Record the message for cleanup
	c.cleanupMsgRecords = append(c.cleanupMsgRecords, msgRecord{
		ChannelID: ch,
		MessageTS: ts,
		EventType: eventType,
	})
}

// scheduleDeleteActionMessages schedules 3-second delayed deletion
// of all tracked messages.
// This version is safe for external calls.
func (c *StreamCallback) scheduleDeleteActionMessages() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scheduleDeleteActionMessagesLocked()
}

// scheduleDeleteActionMessagesLocked schedules deletion without locking.
// Caller MUST hold c.mu.
func (c *StreamCallback) scheduleDeleteActionMessagesLocked() {
	if len(c.cleanupMsgRecords) == 0 {
		return
	}
	records := c.cleanupMsgRecords
	c.cleanupMsgRecords = nil

	time.AfterFunc(3*time.Second, func() {
		if c.messageOps == nil {
			c.logger.Debug("Message operations not supported", "platform", c.platform)
			return
		}
		for _, rec := range records {
			c.logger.Debug("Cleaning up transient message", "platform", c.platform, "ts", rec.MessageTS, "type", rec.EventType)
			if err := c.messageOps.DeleteMessage(c.ctx, rec.ChannelID, rec.MessageTS); err != nil {
				c.logger.Debug("Failed to delete tracked message", "ts", rec.MessageTS, "error", err)
			}
		}
	})
}

// updateStatusMessage updates the status indicator message in-place
// It uses StatusManager if available, otherwise falls back to bubble message
// updateStatusMessage updates the status indicator message
// Exclusively uses native StatusProvider/StatusManager.
func (c *StreamCallback) updateStatusMessage(statusType base.MessageType, displayText string) error {
	if c.statusMgr == nil {
		c.logger.Debug("StatusManager not initialized, skipping status update", "type", statusType)
		return nil
	}
	return c.updateStatusMessageViaManager(statusType, displayText)
}

// updateStatusMessageViaManager uses StatusManager for status notifications
func (c *StreamCallback) updateStatusMessageViaManager(statusType base.MessageType, displayText string) error {
	// Convert MessageType to StatusType
	status := base.MessageTypeToStatusType(statusType)

	// Get channel and thread info from metadata
	c.mu.Lock()
	channelID, _ := c.metadata["channel_id"].(string)
	threadTS, _ := c.metadata["thread_ts"].(string)

	// Throttle repetitive status updates
	// Note: StatusManager also handles deduplication internally
	if c.currentStatus == statusType && time.Since(c.lastStatusUpdate) < time.Second {
		c.mu.Unlock()
		return nil
	}
	c.lastStatusUpdate = time.Now()

	// Update local status tracking
	if c.isFirst {
		c.isFirst = false
	}
	c.currentStatus = statusType
	c.mu.Unlock()

	// Use StatusManager for status notification (handles deduplication internally)
	err := c.statusMgr.Notify(c.ctx, channelID, threadTS, status, displayText)
	if err != nil {
		c.logger.Warn("StatusManager Notify failed", "error", err, "status", status)
		// Don't return error - StatusManager handles fallback internally
	}
	return nil
}

// convertToChatMessage converts base.ChatMessage to ChatMessage (local type)
// It ensures that the current session's platform and sessionID are correctly inherited
func (c *StreamCallback) convertToChatMessage(msg *base.ChatMessage) *ChatMessage {
	if msg.Platform == "" {
		msg.Platform = c.platform
	}
	if msg.SessionID == "" {
		msg.SessionID = c.sessionID
	}
	return &ChatMessage{
		Type:        msg.Type,
		Platform:    msg.Platform,
		SessionID:   msg.SessionID,
		UserID:      msg.UserID,
		Content:     msg.Content,
		MessageID:   msg.MessageID,
		Timestamp:   msg.Timestamp,
		Metadata:    msg.Metadata,
		RichContent: msg.RichContent,
	}
}

func (c *StreamCallback) handleToolUse(data any) error {
	toolName := string(provider.EventTypeToolUse)
	if m, ok := data.(*event.EventWithMeta); ok {
		if m.Meta != nil && m.Meta.ToolName != "" {
			toolName = m.Meta.ToolName
			c.mu.Lock()
			c.lastToolName = toolName
			c.mu.Unlock()
		}
	}

	// UI Native: tool_use is now status-only
	statusText := fmt.Sprintf(StatusToolExecutingLabel, toolName)
	if err := c.updateStatusMessage(base.MessageTypeToolUse, statusText); err != nil {
		c.logger.Warn("Failed to update status for tool_use", "error", err)
	}

	c.logger.Debug("Processed tool use (status-only)", "tool_name", toolName)
	return nil
}

func (c *StreamCallback) handleToolResult(data any) error {
	c.logger.Debug("Tool result handler called", "data_type", fmt.Sprintf("%T", data))

	success := true
	var durationMs int64
	var toolName string
	var filePath string
	output := ""

	var contentLength int64
	if m, ok := data.(*event.EventWithMeta); ok {
		c.logger.Debug("Tool result event data",
			"event_data_len", len(m.EventData),
			"meta_tool_name", m.Meta.ToolName,
			"meta_duration_ms", m.Meta.DurationMs,
			"meta_status", m.Meta.Status,
			"meta_file_path", m.Meta.FilePath)
		if m.Meta != nil {
			if m.Meta.Status == "error" {
				success = false
			}
			if m.Meta.ErrorMsg != "" {
				output = m.Meta.ErrorMsg
			}
			durationMs = m.Meta.DurationMs
			toolName = m.Meta.ToolName
			filePath = m.Meta.FilePath
		}

		// Calculate real content length even if we use a placeholder
		contentLength = int64(len(m.EventData))

		// =====================================================================
		// Space Folding: Large outputs (>2KB) are diverted to thread replies
		// The main channel gets a compact indicator instead of the raw output.
		// =====================================================================
		const spaceFoldingThreshold = 2048 // 2KB

		if contentLength > spaceFoldingThreshold && m.EventData != "" && c.messageOps != nil {
			// Send full output as a thread reply (folded into the conversation thread)
			c.mu.Lock()
			channelID, _ := c.metadata["channel_id"].(string)
			threadTS, _ := c.metadata["thread_ts"].(string)
			c.mu.Unlock()

			if channelID != "" && threadTS != "" {
				// Truncate extremely large outputs (>32KB) even in thread replies
				threadContent := m.EventData
				if len(threadContent) > 32768 {
					threadContent = threadContent[:32768] + "\n\n... (output truncated at 32KB)"
				}

				// Send to thread as a code block for readability
				threadMsg := fmt.Sprintf("📋 *%s* 完整输出 (%s):\n```\n%s\n```", toolName, formatDataLength(contentLength), threadContent)
				go func() {
					// Use a 30s timeout context for background thread reply
					ctxBody, cancel := context.WithTimeout(c.ctx, 30*time.Second)
					defer cancel()
					if err := c.messageOps.SendThreadReply(ctxBody, channelID, threadTS, threadMsg); err != nil {
						c.logger.Warn("Space Folding: failed to send thread reply", "error", err)
					}
				}()

				output = "📋 输出过长，已收纳至回复"
			} else {
				output = "Output generated"
			}
		} else if output == "" && m.EventData != "" {
			output = "Output generated"
		}
	}

	// Skip empty tool_result events (no output, no error, no length)
	if output == "" && toolName == "" && contentLength == 0 {
		c.logger.Debug("Skipping tool result: empty output and tool name")
		return nil
	}

	c.logger.Debug("Sending tool result message",
		"tool_name", toolName,
		"success", success,
		"duration_ms", durationMs,
		"output_len", len(output))

	// Update status indicator to show tool execution result with 🧠 emoji (AI processing state)
	statusText := fmt.Sprintf(StatusToolResultThinkingLabel, durationMs)
	if err := c.updateStatusMessage(base.MessageTypeToolResult, statusText); err != nil {
		c.logger.Warn("Failed to update status for tool_result", "error", err)
	}

	// Silent Success: only send message to main channel if there's an error
	if !success {
		return c.buildChatMessage(base.MessageTypeToolResult, output, map[string]any{
			"success":        success,
			"duration_ms":    durationMs,
			"tool_name":      toolName,
			"file_path":      filePath,
			"content_length": contentLength,
			"event_type":     string(provider.EventTypeToolResult),
			"stream":         true,
		})
	}

	c.logger.Debug("Tool result processed silently (success)", "tool_name", toolName)
	return nil
}

func (c *StreamCallback) handleAnswer(data any) error {
	// Capture answer content
	var content string
	switch v := data.(type) {
	case *event.EventWithMeta:
		content = v.EventData
	case string:
		content = v
	}

	if content == "" {
		return nil
	}

	c.mu.Lock()

	// Always accumulate content for potential fallback
	c.accumulatedContent.WriteString(content)

	// Guard: If content is pure whitespace, skip native stream update to avoid Slack API errors.
	// We still accumulate it, so it will be included in the NEXT real text chunk or final fallback.
	if strings.TrimSpace(content) == "" {
		c.logger.Debug("Accumulating whitespace-only chunk without stream update", "len", len(content))
		c.mu.Unlock()
		return nil
	}

	// Initialize native streaming on the FIRST real answer chunk
	if !c.streamWriterActive && c.streamWriter == nil {
		channelID := ""
		threadTS := ""
		userID := ""
		if c.metadata != nil {
			if ch, ok := c.metadata["channel_id"].(string); ok {
				channelID = ch
			}
			if ts, ok := c.metadata["thread_ts"].(string); ok {
				threadTS = ts
			}
			if uid, ok := c.metadata["user_id"].(string); ok {
				userID = uid
			}
		}
		if channelID != "" {
			c.streamWriter = c.adapters.NewStreamWriter(c.ctx, c.platform, userID, channelID, threadTS)
			if c.streamWriter != nil {
				c.streamWriterActive = true
				c.logger.Debug("Native streaming initialized", "channel_id", channelID)

				// Clear the status indicator immediately now that text is physically appearing in the chat box.
				// This is only called ONCE upon initialization to avoid useless API calls per byte.
				go func() {
					if err := c.updateStatusMessage(base.MessageTypeAnswer, StatusAnswerLabel); err != nil {
						c.logger.Warn("Failed to update status for answer", "error", err)
					}
				}()
			} else {
				c.logger.Warn("Native streaming unavailable, will use fallback at session end",
					"channel_id", channelID)
			}
		}
	}

	// Schedule deletion of all tracked Thinking/Action messages (3s delay)
	c.scheduleDeleteActionMessagesLocked()

	// Capture writer state and unlock before slow I/O to avoid deadlock during network calls
	writer := c.streamWriter
	active := c.streamWriterActive
	c.mu.Unlock()

	// Use native streaming if available (Pure Pipeline Mode)
	// NOTE: We call writer.Write OUTSIDE the lock to avoid holding the mutex during slow I/O
	if active && writer != nil {
		// PRIORITIZE UI: Write to stream immediately so user sees progress
		n, err := writer.Write([]byte(content))
		if err != nil {
			c.logger.Error("Failed to write to native stream, attempting fallback", "error", err)

			// Fallback: try to send as non-streaming message
			// Check if writer has buffered content from failed StartStream
			var bufferedContent string
			if sw, ok := writer.(interface{ BufferContent() string }); ok {
				bufferedContent = sw.BufferContent()
			}

			// Combine buffered content with current content
			fullContent := bufferedContent + content

			// Send as non-streaming message via adapter
			msg := &base.ChatMessage{
				Platform:  c.platform,
				SessionID: c.sessionID,
				Content:   fullContent,
			}
			if sendErr := c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, msg); sendErr != nil {
				c.logger.Error("Fallback also failed", "fallback_error", sendErr)
				return fmt.Errorf("streaming failed: %w, fallback failed: %v", err, sendErr)
			}

			// CRITICAL: Mark fallback as used to prevent duplicate sends from handleSessionStats
			// This coordinates with the Close() fallback mechanism
			c.mu.Lock()
			c.streamWriterActive = false
			c.accumulatedContent.Reset() // Clear accumulated content to prevent re-sending
			c.mu.Unlock()

			c.logger.Info("Fallback to non-streaming succeeded", "content_runes", len([]rune(fullContent)))
			return nil
		}

		c.logger.Debug("Successfully wrote to native stream", "bytes", n)
		return nil
	}

	// Streaming unavailable - content is accumulated for fallback at session end
	c.logger.Debug("Answer chunk accumulated for fallback",
		"chunk_len", len(content))
	return nil
}

func (c *StreamCallback) handleError(data any) error {
	c.mu.Lock()
	c.isFinished = true
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}
	c.mu.Unlock()

	// Clear thinking state on first non-thinking event

	// Cleanup any lingering intermediate messages on error
	c.scheduleDeleteActionMessages()

	var errMsg string
	switch v := data.(type) {
	case string:
		errMsg = v
	case error:
		errMsg = v.Error()
	case *event.EventWithMeta:
		errMsg = v.EventData
		if errMsg == "" && v.Meta != nil {
			errMsg = v.Meta.ErrorMsg
		}
	default:
		errMsg = fmt.Sprintf("%v", data)
	}

	// Localize and enrich timeout error messages for a better user experience
	if strings.Contains(strings.ToLower(errMsg), "timeout") {
		waitTime := "30分钟" // Default standard
		if engine := c.getEngine(); engine != nil {
			waitTime = engine.GetOptions().Timeout.String()
		}

		if c.lastToolName != "" {
			errMsg = fmt.Sprintf("抱歉，由于任务过于复杂，处理超出了预设的时间（%s）。机器人卡在执行 `%s` 工具的过程中。建议您可以尝试拆解任务后再重试。", waitTime, c.lastToolName)
		} else {
			errMsg = fmt.Sprintf("抱歉，任务执行时间过长（超过 %s），为了系统安全已自动终止。建议您可以尝试缩小任务范围后再试一次。", waitTime)
		}
	}

	// Update status to show error state for better visibility
	if err := c.updateStatusMessage(base.MessageTypeError, StatusErrorLabel); err != nil {
		c.logger.Warn("Failed to update status for handle_error", "error", err)
	}

	// Use buildChatMessage helper for consistency
	if err := c.buildChatMessage(base.MessageTypeError, errMsg, map[string]any{
		"event_type": string(provider.EventTypeError),
	}); err != nil {
		return err
	}

	// Clear processor session state on error
	c.processor.ResetSession(c.platform, c.sessionID)

	return nil
}

func (c *StreamCallback) handleDangerBlock(data any) error {
	var reason string

	// Extract reason from data — supports multiple formats:
	// 1. string (legacy/simple)
	// 2. map[string]any from WAF pre-flight (contains "operation" and "reason")
	// 3. *security.DangerBlockEvent from Engine-level WAF
	switch v := data.(type) {
	case string:
		reason = v
	case map[string]any:
		if r, ok := v["reason"].(string); ok && r != "" {
			reason = r
		}
		if op, ok := v["operation"].(string); ok && op != "" {
			if reason != "" {
				reason = reason + "\n" + op
			} else {
				reason = op
			}
		}
		if reason == "" {
			reason = "security_block"
		}
	default:
		reason = "security_block"
	}

	// Log danger block event (INFO level for security events)
	c.logger.Info("Danger block detected",
		"session_id", c.sessionID,
		"reason", reason)

	// Update status indicator — AI is waiting for user decision
	if err := c.updateStatusMessage(base.MessageTypeDangerBlock, StatusDangerBlockLabel); err != nil {
		c.logger.Warn("Failed to update status for danger_block", "error", err)
	}

	// Use buildChatMessage helper for consistency
	return c.buildChatMessage(base.MessageTypeDangerBlock, reason, map[string]any{
		"event_type": "security_block",
		"session_id": c.sessionID,
	})
}

// handleSessionStats handles session statistics events
// Implements EventTypeResult (Turn Complete)
func (c *StreamCallback) handleSessionStats(data any) error {

	stats, ok := data.(*event.SessionStatsData)
	if !ok {
		c.logger.Debug("session_stats: invalid data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	// Close native streaming and stop timers if active
	c.mu.Lock()
	c.isFinished = true
	if c.idleTimer != nil {
		c.idleTimer.Stop()
	}

	// Capture accumulated content and streaming state BEFORE closing writer
	accumulatedContent := c.accumulatedContent.String()
	streamWasActive := c.streamWriterActive
	streamWriter := c.streamWriter

	if streamWriter != nil {
		if err := streamWriter.Close(); err != nil {
			c.logger.Error("Failed to close stream during finalization", "error", err)
			c.mu.Unlock()
			return fmt.Errorf("failed to close stream: %w", err)
		}
		c.streamWriter = nil
		c.streamWriterActive = false
	}
	c.mu.Unlock()

	// Check if Close() already triggered its fallback mechanism
	// This prevents duplicate sends when both Close() and handleSessionStats could trigger
	streamUsedFallback := streamWriter != nil && streamWriter.FallbackUsed()

	// Fallback mechanism: if streaming was never active but we have accumulated content,
	// send the content via direct message.
	//
	// NOTE: This fallback handles the case where StartStream() failed or streaming was unavailable.
	// The NativeStreamingWriter.Close() handles a DIFFERENT case: when streaming was active but
	// failed mid-stream (integrity check failure or StopStream error). These two fallbacks are
	// MUTUALLY EXCLUSIVE - this one triggers when !streamWasActive, Close() triggers when started.
	// Additionally, we check FallbackUsed() to handle the edge case where handleAnswer already
	// sent a fallback message before the stream was properly initialized.
	// This prevents duplicate message sends.
	// Fallback mechanism for inactive streams
	if !streamWasActive && !streamUsedFallback && strings.TrimSpace(accumulatedContent) != "" {
		c.logger.Info("Streaming was inactive, sending accumulated content via fallback",
			"content_len", len(accumulatedContent))

		channelID, _ := c.metadata["channel_id"].(string)
		threadTS, _ := c.metadata["thread_ts"].(string)

		if channelID != "" && c.messageOps != nil {
			if err := c.messageOps.SendThreadReply(c.ctx, channelID, threadTS, accumulatedContent); err != nil {
				c.logger.Error("Final fallback message send failed",
					"channel_id", channelID,
					"content_len", len(accumulatedContent),
					"error", err)
				return fmt.Errorf("final fallback failed: %w", err)
			}
			c.logger.Info("Fallback message sent successfully",
				"channel_id", channelID,
				"content_len", len(accumulatedContent))
		}
	} else if len(accumulatedContent) > 0 && strings.TrimSpace(accumulatedContent) == "" {
		c.logger.Debug("Skipping fallback for pure whitespace content", "len", len(accumulatedContent))
	}

	// Final cleanup of transient transition messages (Thinking + Action Zone)
	// This applies a 3s delayed deletion for a clean end-state UX
	c.scheduleDeleteActionMessages()

	// Final cleanup: clear status indicator
	if err := c.updateStatusMessage(base.MessageTypeSessionStats, ""); err != nil {
		c.logger.Warn("Failed to clear status for session_stats", "error", err)
	}

	// Use buildChatMessage helper for consistency
	if err := c.buildChatMessage(base.MessageTypeSessionStats, "", map[string]any{
		"event_type":           "session_stats",
		"session_id":           stats.SessionID,
		"total_duration_ms":    stats.TotalDurationMs,
		"thinking_duration_ms": stats.ThinkingDurationMs,
		"tool_duration_ms":     stats.ToolDurationMs,
		"input_tokens":         int64(stats.InputTokens),
		"output_tokens":        int64(stats.OutputTokens),
		"total_tokens":         stats.TotalTokens,
		"tool_call_count":      int64(stats.ToolCallCount),
		"tools_used":           stats.ToolsUsed,
		"files_modified":       int64(stats.FilesModified),
		"file_paths":           stats.FilePaths,
	}); err != nil {
		return err
	}

	// Final cleanup: ensure all processor state is reset
	c.processor.ResetSession(c.platform, c.sessionID)

	return nil
}

// handleCommandProgress handles command progress events
// Implements EventTypeCommandProgress per spec
func (c *StreamCallback) handleCommandProgress(data any) error {
	var title string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		title = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"duration_ms":    m.Meta.DurationMs,
				"total_steps":    int(m.Meta.TotalSteps),
				"current_step":   int(m.Meta.CurrentStep),
				"progress":       int(m.Meta.Progress),
				"tool_name":      m.Meta.ToolName,
				"status":         m.Meta.Status,
				"input_summary":  m.Meta.InputSummary,
				"output_summary": m.Meta.OutputSummary,
			}
		}
	} else if s, ok := data.(string); ok {
		title = s
	} else {
		title = fmt.Sprintf("Command Progress: %v", data)
	}

	// Update status indicator
	if err := c.updateStatusMessage(base.MessageTypeCommandProgress, StatusProgressLabel); err != nil {
		c.logger.Warn("Failed to update status for command_progress", "error", err)
	}

	// Add event_type to metadata
	metadata["event_type"] = string(provider.EventTypeCommandProgress)

	// Use buildChatMessage helper for consistency
	return c.buildChatMessage(base.MessageTypeCommandProgress, title, metadata)
}

// handleCommandComplete handles command completion events
// Implements EventTypeCommandComplete per spec
func (c *StreamCallback) handleCommandComplete(data any) error {
	var title string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		title = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"duration_ms":       m.Meta.DurationMs,
				"total_duration_ms": m.Meta.TotalDurationMs,
				"completed_steps":   int(m.Meta.CurrentStep),
				"total_steps":       int(m.Meta.TotalSteps),
				"tool_name":         m.Meta.ToolName,
				"status":            m.Meta.Status,
				"input_tokens":      int(m.Meta.InputTokens),
				"output_tokens":     int(m.Meta.OutputTokens),
			}
		}
	} else if s, ok := data.(string); ok {
		title = s
	} else {
		title = fmt.Sprintf("Command Complete: %v", data)
	}

	// Update status to show command completion
	if err := c.updateStatusMessage(base.MessageTypeCommandComplete, StatusProgressDoneLabel); err != nil {
		c.logger.Warn("Failed to update status for command_complete", "error", err)
	}

	// Add event_type to metadata
	metadata["event_type"] = string(provider.EventTypeCommandComplete)

	// Use buildChatMessage helper for consistency
	return c.buildChatMessage(base.MessageTypeCommandComplete, title, metadata)
}

// handleStepStart handles step start events (OpenCode specific)
// Implements EventTypeStepStart per spec
func (c *StreamCallback) handleStepStart(data any) error {
	var content string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"step":          int(m.Meta.CurrentStep),
				"total":         int(m.Meta.TotalSteps),
				"duration_ms":   m.Meta.DurationMs,
				"tool_name":     m.Meta.ToolName,
				"input_summary": m.Meta.InputSummary,
			}
		}
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = fmt.Sprintf("Step Start: %v", data)
	}

	// Update status indicator
	if err := c.updateStatusMessage(base.MessageTypeStepStart, StatusStepStartLabel); err != nil {
		c.logger.Warn("Failed to update status for step_start", "error", err)
	}

	// Add event_type to metadata
	metadata["event_type"] = string(provider.EventTypeStepStart)

	// Use buildChatMessage helper for consistency
	return c.buildChatMessage(base.MessageTypeStepStart, content, metadata)
}

// handleStepFinish handles step finish events (OpenCode specific)
// Implements EventTypeStepFinish per spec
func (c *StreamCallback) handleStepFinish(data any) error {
	var content string
	var metadata map[string]any

	if m, ok := data.(*event.EventWithMeta); ok {
		content = m.EventData
		if m.Meta != nil {
			metadata = map[string]any{
				"step":           int(m.Meta.CurrentStep),
				"total":          int(m.Meta.TotalSteps),
				"duration_ms":    m.Meta.DurationMs,
				"tool_name":      m.Meta.ToolName,
				"status":         m.Meta.Status,
				"output_summary": m.Meta.OutputSummary,
			}
		}
	} else if s, ok := data.(string); ok {
		content = s
	} else {
		content = fmt.Sprintf("Step Finish: %v", data)
	}

	// Update status to show step completion
	if err := c.updateStatusMessage(base.MessageTypeStepFinish, StatusStepFinishLabel); err != nil {
		c.logger.Warn("Failed to update status for step_finish", "error", err)
	}

	// Add event_type to metadata
	metadata["event_type"] = string(provider.EventTypeStepFinish)

	// Use buildChatMessage helper for consistency
	return c.buildChatMessage(base.MessageTypeStepFinish, content, metadata)
}

// mergeMetadata merges the callback's stored metadata with the provided metadata
// NOTE: message_ts is intentionally NOT copied because it refers to the user's message,
// not the bot's message. Copying it causes Slack API errors (cant_update_message).
func (c *StreamCallback) mergeMetadata(metadata map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range metadata {
		result[k] = v
	}

	// Copy over important metadata from stored metadata
	if c.metadata != nil {
		if channelID, ok := c.metadata["channel_id"]; ok {
			result["channel_id"] = channelID
		}
		if channelType, ok := c.metadata["channel_type"]; ok {
			result["channel_type"] = channelType
		}
		if threadTS, ok := c.metadata["thread_ts"]; ok {
			result["thread_ts"] = threadTS
		}
		if userID, ok := c.metadata["user_id"]; ok {
			result["user_id"] = userID
		}
		if messageID, ok := c.metadata["message_id"]; ok {
			result["message_id"] = messageID
		}
		// Do NOT copy message_ts - it refers to the user's message, not the bot's message.
	}
	return result
}

// EngineMessageHandler implements MessageHandler and integrates with Engine
type EngineMessageHandler struct {
	engine         Engine
	adapters       *AdapterManager
	workDirFn      func(sessionID string) string
	taskInstrFn    func(sessionID string) string
	systemPromptFn func(sessionID, platform string) string
	configLoader   *ConfigLoader
	logger         *slog.Logger
	pendingStore   *base.PendingMessageStore // Store for pending danger block approvals
}

// NewEngineMessageHandler creates a new EngineMessageHandler
func NewEngineMessageHandler(engine Engine, adapters *AdapterManager, opts ...EngineMessageHandlerOption) *EngineMessageHandler {
	h := &EngineMessageHandler{
		engine:       engine,
		adapters:     adapters,
		logger:       slog.Default(),
		pendingStore: base.NewPendingMessageStore(5 * time.Minute),
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
}

// Close releases resources held by the handler
func (h *EngineMessageHandler) Close() {
	if h.pendingStore != nil {
		h.pendingStore.Stop()
	}
}

// EngineMessageHandlerOption configures the EngineMessageHandler
type EngineMessageHandlerOption func(*EngineMessageHandler)

func WithWorkDirFn(fn func(sessionID string) string) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.workDirFn = fn
	}
}

func WithTaskInstrFn(fn func(sessionID string) string) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.taskInstrFn = fn
	}
}

func WithLogger(logger *slog.Logger) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.logger = logger
	}
}

func WithConfigLoader(loader *ConfigLoader) EngineMessageHandlerOption {
	return func(h *EngineMessageHandler) {
		h.configLoader = loader
	}
}

// Handle implements EngineMessageHandler
func (h *EngineMessageHandler) Handle(ctx context.Context, msg *ChatMessage) error {
	// Determine work directory
	workDir := ""
	if h.workDirFn != nil {
		workDir = h.workDirFn(msg.SessionID)
	}
	if workDir == "" {
		workDir = "/tmp/hotplex-chatapps"
	}

	// Determine task instructions
	taskInstr := ""
	if h.taskInstrFn != nil {
		taskInstr = h.taskInstrFn(msg.SessionID)
	}
	if taskInstr == "" && h.configLoader != nil {
		taskInstr = h.configLoader.GetTaskInstructions(msg.Platform)
	}
	if taskInstr == "" {
		taskInstr = "You are a helpful AI assistant. Execute user commands and provide clear feedback."
	}

	// Determine system prompt
	systemPrompt := ""
	if h.systemPromptFn != nil {
		systemPrompt = h.systemPromptFn(msg.SessionID, msg.Platform)
	}
	if systemPrompt == "" && h.configLoader != nil {
		systemPrompt = h.configLoader.GetSystemPrompt(msg.Platform)
	}

	// Combine task instructions with system prompt
	fullInstructions := taskInstr
	if systemPrompt != "" {
		fullInstructions = systemPrompt + "\n\n" + taskInstr
	}

	// Build config
	cfg := &types.Config{
		WorkDir:          workDir,
		SessionID:        msg.SessionID,
		TaskInstructions: fullInstructions,
	}

	// Get platform-specific operations from AdapterManager
	messageOps := h.adapters.GetMessageOperations(msg.Platform)
	sessionOps := h.adapters.GetSessionOperations(msg.Platform)

	// Check if session is already active (hot multiplexing)
	// If session exists in engine and is active (ready/busy), it's a hot start
	sess, exists := h.engine.GetSession(msg.SessionID)
	isHot := exists && sess != nil && (sess.Status() == "ready" || sess.Status() == "busy")

	// Create stream callback with injected dependencies
	callback := NewStreamCallback(ctx, msg.SessionID, msg.Platform, h.adapters, h.logger, h.engine, isHot, msg.Metadata, messageOps, sessionOps)
	defer callback.Close() // Ensure processor resources are released

	// Send user_message_received event FIRST to set initial reaction (📥 inbox_tray)
	// This must happen before engine.Execute to ensure reaction lifecycle starts correctly
	if err := callback.Handle(string(provider.EventTypeUserMessageReceived), nil); err != nil {
		h.logger.Warn("Failed to send user_message_received event", "error", err)
	}

	wrappedCallback := func(eventType string, data any) error {
		return callback.Handle(eventType, data)
	}

	// ========================================
	// WAF Pre-flight Check (Option C)
	// Check for dangerous prompts BEFORE calling Engine.Execute().
	// If blocked: render card → block on approval channel → resume or abort.
	// ========================================
	if blocked, operation, reason := h.engine.CheckDanger(msg.Content); blocked {
		h.logger.Warn("WAF pre-flight: dangerous prompt intercepted",
			"session_id", msg.SessionID,
			"operation", operation,
			"reason", reason)

		// Render danger block card via the callback (reuses existing handleDangerBlock)
		dangerData := map[string]any{
			"operation": operation,
			"reason":    reason,
		}
		if err := callback.Handle("danger_block", dangerData); err != nil {
			h.logger.Error("Failed to render danger block card", "error", err)
		}

		// Register pending approval and block on user decision
		approvalCh := base.GlobalDangerRegistry.Register(msg.SessionID)
		defer base.GlobalDangerRegistry.Cancel(msg.SessionID)

		h.logger.Info("WAF pre-flight: blocking on user approval",
			"session_id", msg.SessionID)

		select {
		case approved := <-approvalCh:
			if approved {
				h.logger.Info("WAF pre-flight: user approved dangerous prompt",
					"session_id", msg.SessionID)
				cfg.WAFApproved = true
				// Fall through to Engine.Execute() below
			} else {
				h.logger.Info("WAF pre-flight: user denied dangerous prompt",
					"session_id", msg.SessionID)
				// Clear assistant status — operation cancelled
				_ = callback.updateStatusMessage(base.MessageTypeSessionStats, "")
				return nil // User denied — no error, operation simply cancelled
			}
		case <-ctx.Done():
			h.logger.Warn("WAF pre-flight: context cancelled while waiting for approval",
				"session_id", msg.SessionID)
			return ctx.Err()
		}
	}

	// Execute with Engine
	h.logger.Info("Executing prompt via Engine",
		"session_id", msg.SessionID,
		"platform", msg.Platform,
		"prompt_len", len(msg.Content))

	err := h.engine.Execute(ctx, cfg, msg.Content, wrappedCallback)
	if err != nil {
		h.logger.Error("Engine execution failed",
			"session_id", msg.SessionID,
			"error", err)

		// Delegate to callback error handler for consistent UI (status, reaction, message)
		_ = callback.Handle(string(provider.EventTypeError), err)
		return err
	}

	return nil
}

// =============================================================================
// Plan Mode Event Handlers
// =============================================================================

// handlePlanMode handles plan generation events (thinking with subtype=plan_generation)
func (c *StreamCallback) handlePlanMode(data any) error {
	var planContent string

	if m, ok := data.(*event.EventWithMeta); ok {
		planContent = m.EventData
		c.logger.Debug("handlePlanMode received",
			"data_type", fmt.Sprintf("%T", data),
			"event_data_len", len(planContent))
	}

	// Skip empty content
	if planContent == "" {
		return nil
	}

	// Update status indicator
	if err := c.updateStatusMessage(base.MessageTypePlanMode, StatusPlanModeLabel); err != nil {
		c.logger.Warn("Failed to update status for plan_mode", "error", err)
	}

	// Send plan mode message with platform-agnostic MessageType
	return c.buildChatMessage(base.MessageTypePlanMode, planContent, map[string]any{
		"event_type": string(provider.EventTypePlanMode),
	})
}

// handleExitPlanMode handles exit plan mode requests (tool_use with name=ExitPlanMode)
func (c *StreamCallback) handleExitPlanMode(data any) error {
	var planSummary string

	if m, ok := data.(*event.EventWithMeta); ok {
		planSummary = m.EventData
		c.logger.Debug("handleExitPlanMode received",
			"data_type", fmt.Sprintf("%T", data),
			"plan_summary_len", len(planSummary))
	}

	// Use default message if empty
	if planSummary == "" {
		planSummary = "Plan content will be executed upon approval."
	}

	// Update status indicator that we're waiting for user
	if err := c.updateStatusMessage(base.MessageTypeExitPlanMode, StatusExitPlanModeLabel); err != nil {
		c.logger.Warn("Failed to update status for exit_plan_mode", "error", err)
	}

	// Send exit plan mode message with platform-agnostic MessageType
	return c.buildChatMessage(base.MessageTypeExitPlanMode, planSummary, map[string]any{
		"event_type": string(provider.EventTypeExitPlanMode),
		"session_id": c.sessionID,
	})
}

// =============================================================================
// AskUserQuestion Event Handler (Degraded Mode)
// =============================================================================

// handleAskUserQuestion handles AskUserQuestion tool (degraded mode)
// Note: AskUserQuestion is not fully supported in headless mode,
// so we display the question as a text prompt
func (c *StreamCallback) handleAskUserQuestion(data any) error {
	var question string

	if m, ok := data.(*event.EventWithMeta); ok {
		question = m.EventData
		c.logger.Debug("handleAskUserQuestion received",
			"data_type", fmt.Sprintf("%T", data),
			"question_len", len(question))
	}

	// Skip empty question
	if question == "" {
		return nil
	}

	// Update status indicator
	if err := c.updateStatusMessage(base.MessageTypeAskUserQuestion, StatusAskUserLabel); err != nil {
		c.logger.Warn("Failed to update status for ask_user_question", "error", err)
	}

	// Send ask user question message with platform-agnostic MessageType
	msg := &base.ChatMessage{
		Type:    base.MessageTypeAskUserQuestion,
		Content: question,
		Metadata: map[string]any{
			"event_type": string(provider.EventTypeAskUserQuestion),
			"session_id": c.sessionID,
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(c.convertToChatMessage(msg))
}

// =============================================================================
// Session Start / Engine Starting / User Message Received Event Handlers
// =============================================================================

// handleSessionStart handles session start events (cold start)
// Implements EventTypeSessionStart per spec (0.4)
// Triggered when user sends first message or CLI needs cold start
func (c *StreamCallback) handleSessionStart(_ any) error {
	c.mu.Lock()
	if c.sessionStartSent {
		c.mu.Unlock()
		return nil
	}
	c.sessionStartSent = true
	c.mu.Unlock()

	// Determine start status message based on whether it is hot or cold
	statusMsg := StatusSessionStartColdLabel
	if c.isHot {
		statusMsg = StatusSessionStartHotLabel
	}

	// ℹ️ DEVELOPER NOTE: Even though this is an "Absolute Black Hole" event (no physical message),
	// we MUST call updateStatusMessage to drive the native Slack Assistant Status bar.
	if err := c.updateStatusMessage(base.MessageTypeSessionStart, statusMsg); err != nil {
		c.logger.Warn("Failed to update status for session_start", "error", err)
	}

	// Dispatch to processor chain to ensure state synchronization (ZoneOrder, etc.)
	// FilterProcessor will prevent this from being physically sent as a message.
	return c.buildChatMessage(base.MessageTypeSessionStart, statusMsg, map[string]any{
		"event_type": "session_start",
	})
}

// handleEngineStarting handles engine starting event
func (c *StreamCallback) handleEngineStarting(_ any) error {
	if err := c.updateStatusMessage(base.MessageTypeEngineStarting, StatusEngineStartingLabel); err != nil {
		c.logger.Warn("Failed to update status for engine_starting", "error", err)
	}

	// Dispatch to processor chain to ensure state synchronization (ZoneOrder, etc.)
	// FilterProcessor will prevent this from being physically sent as a message.
	return c.buildChatMessage(base.MessageTypeEngineStarting, StatusEngineStartingLabel, map[string]any{
		"event_type": "engine_starting",
	})
}

// handlePermissionRequest handles permission request events
// Implements EventTypePermissionRequest per spec (7)
// Triggered when Claude Code requests user approval for tool execution
func (c *StreamCallback) handlePermissionRequest(data any) error {
	var req *provider.PermissionRequest
	var messageID string

	switch v := data.(type) {
	case *provider.PermissionRequest:
		req = v
		if req != nil {
			messageID = req.MessageID
		}
	case *event.EventWithMeta:
		// Try to parse EventData as PermissionRequest
		if v.EventData != "" {
			parsed, err := provider.ParsePermissionRequest([]byte(v.EventData))
			if err == nil {
				req = parsed
				messageID = parsed.MessageID
			}
		}
		// Also check Meta for tool info if parsing failed
		if req == nil && v.Meta != nil {
			// Create a synthetic request from metadata
			req = &provider.PermissionRequest{
				MessageID: c.sessionID + "-" + time.Now().Format("20060102150405"),
			}
			if v.Meta.ToolName != "" {
				messageID = c.sessionID + "-" + v.Meta.ToolID
			}
		}
	default:
		c.logger.Debug("Unknown permission request data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	if req == nil {
		c.logger.Debug("Empty permission request, skipping")
		return nil
	}

	// Get tool and input for display
	tool, input := req.GetToolAndInput()

	// Apply SetAssistantStatus to wait for permission
	// This ensures the AI isn't just hanging silently while waiting
	if err := c.updateStatusMessage(base.MessageTypePermissionRequest, StatusPermissionLabel); err != nil {
		c.logger.Warn("Failed to update status for permission_request", "error", err)
	}

	msg := &base.ChatMessage{
		Type:    base.MessageTypePermissionRequest,
		Content: "", // Content is built by MessageBuilder from metadata
		Metadata: map[string]any{
			"event_type": string(provider.EventTypePermissionRequest),
			"tool_name":  tool,
			"input":      input,
			"message_id": messageID,
			"session_id": c.sessionID,
			"decision":   req.Decision,
			"reason":     req.GetDescription(),
		},
	}
	msg.Metadata = c.mergeMetadata(msg.Metadata)
	return c.sendMessageAndGetTS(c.convertToChatMessage(msg))
}

// formatDataLength formats byte count into human-readable size string
func formatDataLength(bytes int64) string {
	if bytes > 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	} else if bytes > 1024 {
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%d bytes", bytes)
}
