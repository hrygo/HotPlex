package chatapps

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/slack"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/provider"
)

// CommandCallback handles slash command progress events
type CommandCallback struct {
	ctx          context.Context
	platform     string
	sessionID    string
	adapters     *AdapterManager
	blockBuilder *slack.BlockBuilder
	logger       *slog.Logger
	metadata     map[string]any

	mu        sync.Mutex
	messageTS string
	channelID string
	title     string
}

// NewCommandCallback creates a new command callback
func NewCommandCallback(ctx context.Context, platform, sessionID string, adapters *AdapterManager, logger *slog.Logger, metadata map[string]any) *CommandCallback {
	return &CommandCallback{
		ctx:          ctx,
		platform:     platform,
		sessionID:    sessionID,
		adapters:     adapters,
		blockBuilder: slack.NewBlockBuilder(),
		logger:       logger,
		metadata:     metadata,
	}
}

// Handle implements event.Callback interface
func (c *CommandCallback) Handle(eventType string, data any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch provider.ProviderEventType(eventType) {
	case provider.EventTypeCommandProgress:
		return c.handleProgress(data)
	case provider.EventTypeCommandComplete:
		return c.handleComplete(data)
	default:
		c.logger.Debug("Unknown command event type", "type", eventType)
	}
	return nil
}

func (c *CommandCallback) handleProgress(data any) error {
	meta, ok := data.(*event.EventWithMeta)
	if !ok {
		return nil
	}

	// Extract title from event data
	title := meta.EventData
	if title == "" {
		title = "Processing..."
	}

	// Save title for final message
	c.title = title

	// Extract progress from metadata
	progress := meta.Meta.Progress
	totalSteps := meta.Meta.TotalSteps
	currentStep := meta.Meta.CurrentStep

	// Build step list from current state
	var steps []map[string]any
	if totalSteps > 0 {
		// Generate placeholder steps based on totalSteps
		stepNames := []string{"Finding session", "Deleting session file", "Deleting marker", "Terminating process"}
		for i := 0; i < int(totalSteps) && i < len(stepNames); i++ {
			status := "pending"
			if int(currentStep) > i {
				status = "success"
			} else if int(currentStep) == i+1 {
				status = "running"
			}
			steps = append(steps, map[string]any{
				"name":    stepNames[i],
				"message": stepNames[i],
				"status":  status,
			})
		}
	}

	c.logger.Debug("Command progress", "title", title, "progress", progress, "current_step", currentStep, "total_steps", totalSteps)

	// Build blocks
	blocks := c.blockBuilder.BuildCommandProgressBlock(title, steps, progress)

	// Send or update message
	return c.sendOrUpdate(blocks)
}

func (c *CommandCallback) handleComplete(data any) error {
	meta, ok := data.(*event.EventWithMeta)
	if !ok {
		return nil
	}

	// Build completion block
	blocks := c.blockBuilder.BuildCommandCompleteBlock(c.title, meta.EventData)

	return c.sendOrUpdate(blocks)
}

func (c *CommandCallback) sendOrUpdate(blocks []map[string]any) error {
	// Convert blocks to []any
	var blocksAny []any
	for _, b := range blocks {
		blocksAny = append(blocksAny, b)
	}

	msg := &ChatMessage{
		Platform:  c.platform,
		SessionID: c.sessionID,
		Metadata:  c.copyMetadata(),
		RichContent: &base.RichContent{
			Blocks: blocksAny,
		},
	}

	// If we have a message TS, update; otherwise create new
	if c.messageTS != "" && c.channelID != "" {
		msg.Metadata["message_ts"] = c.messageTS
		msg.Metadata["channel_id"] = c.channelID
	}

	// Send message
	if err := c.adapters.SendMessage(c.ctx, c.platform, c.sessionID, msg); err != nil {
		return fmt.Errorf("send command message: %w", err)
	}

	// Save TS for future updates
	if ts, ok := msg.Metadata["message_ts"].(string); ok && ts != "" {
		c.messageTS = ts
	}
	if ch, ok := msg.Metadata["channel_id"].(string); ok && ch != "" {
		c.channelID = ch
	}

	return nil
}

func (c *CommandCallback) copyMetadata() map[string]any {
	metadata := make(map[string]any)
	for k, v := range c.metadata {
		metadata[k] = v
	}
	metadata["stream"] = true
	metadata["event_type"] = "command"
	return metadata
}
