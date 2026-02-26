package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/slack"
	"github.com/hrygo/hotplex/engine"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
	"github.com/hrygo/hotplex/types"
)

// EngineHolder holds the Engine instance and configuration for ChatApps integration
type EngineHolder struct {
	engine           *engine.Engine
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
		Timeout:         opts.Timeout,
		IdleTimeout:     opts.IdleTimeout,
		Namespace:       opts.Namespace,
		PermissionMode:  opts.PermissionMode,
		AllowedTools:    opts.AllowedTools,
		DisallowedTools: opts.DisallowedTools,
		Logger:          logger,
	}

	eng, err := engine.NewEngine(engineOpts)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	return &EngineHolder{
		engine:           eng,
		logger:           logger,
		adapters:         opts.Adapters,
		defaultWorkDir:   opts.DefaultWorkDir,
		defaultTaskInstr: opts.DefaultTaskInstr,
	}, nil
}

// EngineHolderOptions configures the EngineHolder
type EngineHolderOptions struct {
	Logger           *slog.Logger
	Adapters         *AdapterManager
	Timeout          time.Duration
	IdleTimeout      time.Duration
	Namespace        string
	PermissionMode   string
	AllowedTools     []string
	DisallowedTools  []string
	DefaultWorkDir   string
	DefaultTaskInstr string
}

// GetEngine returns the underlying Engine instance
func (h *EngineHolder) GetEngine() *engine.Engine {
	return h.engine
}

// GetAdapterManager returns the AdapterManager for sending messages
func (h *EngineHolder) GetAdapterManager() *AdapterManager {
	return h.adapters
}

// StreamCallback implements event.Callback to receive Engine events and forward to ChatApp
type StreamCallback struct {
	ctx          context.Context
	sessionID    string
	platform     string
	adapters     *AdapterManager
	logger       *slog.Logger
	mu           sync.Mutex
	isFirst      bool
	thinkingSent bool            // Tracks if thinking message was sent
	metadata     map[string]any  // Original message metadata (channel_id, thread_ts, etc.)
	processor    *ProcessorChain // Message processor chain
	blockBuilder *slack.BlockBuilder

	// Stream state for throttled updates
	streamState *StreamState
}

// StreamState tracks the state for streaming updates
type StreamState struct {
	ChannelID   string
	MessageTS   string
	LastUpdated time.Time
	mu          sync.Mutex
}

// NewStreamCallback creates a new StreamCallback
func NewStreamCallback(ctx context.Context, sessionID, platform string, adapters *AdapterManager, logger *slog.Logger, metadata map[string]any) *StreamCallback {
	cb := &StreamCallback{
		ctx:          ctx,
		sessionID:    sessionID,
		platform:     platform,
		adapters:     adapters,
		logger:       logger,
		isFirst:      true,
		metadata:     metadata,
		processor:    NewDefaultProcessorChain(logger),
		blockBuilder: slack.NewBlockBuilder(),
	}

	// Set callback as the sender for aggregated messages
	cb.processor.SetAggregatorSender(cb)

	return cb
}

// SendAggregatedMessage implements AggregatedMessageSender interface
// This is called by the MessageAggregatorProcessor when timer flushes buffered messages
func (c *StreamCallback) SendAggregatedMessage(ctx context.Context, msg *ChatMessage) error {
	c.logger.Info("SendAggregatedMessage called", "session_id", c.sessionID, "content_len", len(msg.Content))

	if c.adapters == nil {
		c.logger.Warn("No adapters in SendAggregatedMessage", "platform", c.platform)
		return nil
	}

	return c.adapters.SendMessage(ctx, c.platform, c.sessionID, msg)
}

// Handle implements event.Callback
func (c *StreamCallback) Handle(eventType string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

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
	// Extract thinking content from EventWithMeta
	var thinkingContent string
	if m, ok := data.(*event.EventWithMeta); ok {
		// Use EventData directly (contains thinking content from CLI)
		thinkingContent = m.EventData
	}

	// Log what we received
	c.logger.Debug("handleThinking received",
		"data_type", fmt.Sprintf("%T", data),
		"event_data", thinkingContent,
		"is_first", c.isFirst,
		"thinking_sent", c.thinkingSent)

	// Skip empty thinking content
	if thinkingContent == "" {
		return nil
	}

	if c.isFirst {
		// First thinking event - create new message
		c.isFirst = false
		c.thinkingSent = true

		blocks := c.blockBuilder.BuildThinkingBlock(thinkingContent)
		return c.sendBlockMessage(string(provider.EventTypeThinking), blocks, true)
	} else if c.thinkingSent {
		// Subsequent thinking event - update the existing message
		// This allows streaming thinking content updates
		blocks := c.blockBuilder.BuildThinkingBlock(thinkingContent)
		return c.sendBlockMessage(string(provider.EventTypeThinking), blocks, false)
	}

	return nil
}

func (c *StreamCallback) handleToolUse(data any) error {
	c.logger.Debug("[TOOL] handleToolUse called", "data_type", fmt.Sprintf("%T", data))

	toolName := string(provider.EventTypeToolUse)
	input := ""
	truncated := false

	if m, ok := data.(*event.EventWithMeta); ok {
		c.logger.Debug("[TOOL] handleToolUse EventWithMeta",
			"event_data", m.EventData,
			"meta_tool_name", m.Meta.ToolName,
			"meta_input_summary", m.Meta.InputSummary)
		if m.Meta != nil && m.Meta.ToolName != "" {
			toolName = m.Meta.ToolName
		}
		if m.EventData != "" {
			input = m.EventData
			if len(input) > 100 {
				truncated = true
			}
		}
	}

	c.logger.Debug("[TOOL] handleToolUse sending", "tool_name", toolName, "input_len", len(input))
	blocks := c.blockBuilder.BuildToolUseBlock(toolName, input, truncated)
	return c.sendBlockMessage(toolName, blocks, false)
}

func (c *StreamCallback) handleToolResult(data any) error {
	c.logger.Debug("[TOOL] handleToolResult called", "data_type", fmt.Sprintf("%T", data))

	success := true
	var durationMs int64
	var toolName string
	output := ""

	_ = toolName // used in BuildToolResultBlock

	if m, ok := data.(*event.EventWithMeta); ok {
		c.logger.Debug("[TOOL] handleToolResult EventWithMeta",
			"event_data", m.EventData,
			"meta_tool_name", m.Meta.ToolName,
			"meta_duration_ms", m.Meta.DurationMs,
			"meta_status", m.Meta.Status)
		if m.Meta != nil {
			if m.Meta.Status == "error" {
				success = false
			}
			if m.Meta.ErrorMsg != "" {
				output = m.Meta.ErrorMsg
			}
			durationMs = m.Meta.DurationMs
			toolName = m.Meta.ToolName
		}
		if m.EventData != "" {
			output = m.EventData
		}
	}

	c.logger.Debug("[TOOL] handleToolResult sending", "success", success, "duration_ms", durationMs, "output_len", len(output))
	blocks := c.blockBuilder.BuildToolResultBlock(success, durationMs, output, false, toolName)
	return c.sendBlockMessage(string(provider.EventTypeToolResult), blocks, false)
}

func (c *StreamCallback) handleAnswer(data any) error {
	// Clear thinking state on first non-thinking event
	if c.thinkingSent {
		c.thinkingSent = false
		c.logger.Debug("Clearing thinking state for answer")
	}

	var answerContent string
	switch v := data.(type) {
	case string:
		answerContent = v
	case *event.EventWithMeta:
		answerContent = v.EventData
	default:
		answerContent = fmt.Sprintf("%v", data)
	}

	if answerContent == "" {
		return nil
	}

	// Use throttled streaming update if we have a message to update
	if c.streamState != nil {
		return c.streamState.updateThrottled(c.ctx, c.adapters, c.platform, c.sessionID, answerContent, c.blockBuilder, c.metadata)
	}

	// Otherwise send as new message
	blocks := c.blockBuilder.BuildAnswerBlock(answerContent)
	return c.sendBlockMessage(string(provider.EventTypeAnswer), blocks, false)
}

func (c *StreamCallback) handleError(data any) error {
	// Clear thinking state on first non-thinking event
	if c.thinkingSent {
		c.thinkingSent = false
		c.logger.Debug("Clearing thinking state for error")
	}

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

	blocks := c.blockBuilder.BuildErrorBlock(errMsg, false)
	return c.sendBlockMessage(string(provider.EventTypeError), blocks, true)
}

func (c *StreamCallback) handleDangerBlock(data any) error {
	var reason string
	switch v := data.(type) {
	case string:
		reason = v
	default:
		reason = "security_block"
	}
	blocks := c.blockBuilder.BuildErrorBlock(reason, true)
	return c.sendBlockMessage("security_block", blocks, true)
}

// handleSessionStats handles session statistics events
func (c *StreamCallback) handleSessionStats(data any) error {
	stats, ok := data.(*event.SessionStatsData)
	if !ok {
		c.logger.Debug("session_stats: invalid data type", "type", fmt.Sprintf("%T", data))
		return nil
	}

	// Build rich session stats UI using card style (recommended default)
	blocks := c.blockBuilder.BuildSessionStatsBlock(stats, slack.StatsStyleCompact)
	if len(blocks) == 0 {
		return nil
	}

	// Send stats as informational message (not final)
	return c.sendBlockMessage("session_stats", blocks, false)
}

// sendBlockMessage sends a message with Slack blocks
func (c *StreamCallback) sendBlockMessage(content string, blocks []map[string]any, isFinal bool) error {
	if c.adapters == nil {
		c.logger.Debug("No adapters, skipping message send", "platform", c.platform)
		return nil
	}

	// Build metadata with original message's platform-specific data
	metadata := c.copyMessageMetadata()
	metadata["stream"] = true
	metadata["event_type"] = content
	metadata["is_final"] = isFinal

	// Initialize stream state on first non-thinking message
	if content != string(provider.EventTypeThinking) && c.streamState == nil {
		c.streamState = &StreamState{}
	}

	// Convert blocks to []any for RichContent
	var blocksAny []any
	for _, b := range blocks {
		blocksAny = append(blocksAny, b)
	}

	msg := &ChatMessage{
		Platform:  c.platform,
		SessionID: c.sessionID,
		Content:   content,
		Metadata:  metadata,
		RichContent: &base.RichContent{
			Blocks: blocksAny,
		},
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

	return c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, processedMsg)
}

// copyMessageMetadata copies important metadata from original message
func (c *StreamCallback) copyMessageMetadata() map[string]any {
	metadata := make(map[string]any)
	if c.metadata != nil {
		if channelID, ok := c.metadata["channel_id"]; ok {
			metadata["channel_id"] = channelID
		}
		if channelType, ok := c.metadata["channel_type"]; ok {
			metadata["channel_type"] = channelType
		}
		if threadTS, ok := c.metadata["thread_ts"]; ok {
			metadata["thread_ts"] = threadTS
		}
		if userID, ok := c.metadata["user_id"]; ok {
			metadata["user_id"] = userID
		}
		if messageID, ok := c.metadata["message_id"]; ok {
			metadata["message_id"] = messageID
		}
	}
	return metadata
}

// EngineMessageHandler implements MessageHandler and integrates with Engine
type EngineMessageHandler struct {
	engine         *engine.Engine
	adapters       *AdapterManager
	workDirFn      func(sessionID string) string
	taskInstrFn    func(sessionID string) string
	systemPromptFn func(sessionID, platform string) string
	configLoader   *ConfigLoader
	logger         *slog.Logger
}

// NewEngineMessageHandler creates a new EngineMessageHandler
func NewEngineMessageHandler(engine *engine.Engine, adapters *AdapterManager, opts ...EngineMessageHandlerOption) *EngineMessageHandler {
	h := &EngineMessageHandler{
		engine:   engine,
		adapters: adapters,
		logger:   slog.Default(),
	}

	for _, opt := range opts {
		opt(h)
	}

	return h
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

	// Create stream callback with original message metadata
	callback := NewStreamCallback(ctx, msg.SessionID, msg.Platform, h.adapters, h.logger, msg.Metadata)
	wrappedCallback := func(eventType string, data any) error {
		return callback.Handle(eventType, data)
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

		// Send error message back
		if h.adapters != nil {
			errMsg := &ChatMessage{
				Platform:  msg.Platform,
				SessionID: msg.SessionID,
				Content:   err.Error(),
				Metadata: map[string]any{
					"event_type": string(provider.EventTypeError),
				},
			}
			if err := h.adapters.SendMessage(ctx, msg.Platform, msg.SessionID, errMsg); err != nil {
				h.logger.Error("Failed to send error message", "session_id", msg.SessionID, "error", err)
			}
		}
		return err
	}

	return nil
}

// updateThrottled sends throttled streaming updates to Slack
// Limits updates to 1 per second to avoid rate limiting
func (s *StreamState) updateThrottled(ctx context.Context, adapters *AdapterManager, platform, sessionID, content string, blockBuilder *slack.BlockBuilder, metadata map[string]any) error {
	s.mu.Lock()

	// Throttle: max 1 update per second
	if time.Since(s.LastUpdated) < time.Second {
		s.mu.Unlock()
		return nil
	}
	s.LastUpdated = time.Time{} // Mark as updating
	s.mu.Unlock()

	// Build blocks with content
	blocks := blockBuilder.BuildAnswerBlock(content)

	// Convert blocks to []any
	var blocksAny []any
	for _, b := range blocks {
		blocksAny = append(blocksAny, b)
	}

	// Create chat message for update
	msg := &ChatMessage{
		Platform:  platform,
		SessionID: sessionID,
		Content:   content,
		RichContent: &RichContent{
			Blocks: blocksAny,
		},
		Metadata: make(map[string]any),
	}

	// Copy metadata
	for k, v := range metadata {
		msg.Metadata[k] = v
	}

	// Add thread_ts if present
	if threadTS, ok := metadata["thread_ts"]; ok {
		msg.Metadata["thread_ts"] = threadTS
	}

	// Update existing message
	if s.ChannelID != "" && s.MessageTS != "" {
		msg.Metadata["message_ts"] = s.MessageTS
		msg.Metadata["channel_id"] = s.ChannelID
	}

	// Send update
	err := adapters.SendMessage(ctx, platform, sessionID, msg)

	// Update timestamp on success
	s.mu.Lock()
	if err == nil {
		s.LastUpdated = time.Now()
	} else {
		s.LastUpdated = time.Time{} // Reset on error to allow retry
	}
	s.mu.Unlock()

	return err
}
