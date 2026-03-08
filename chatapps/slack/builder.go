// Package slack provides the Slack adapter implementation for the hotplex engine.
// Detailed message construction logic using Slack Block Kit.
//
// =============================================================================
// API Contract - MessageBuilder
// =============================================================================
//
// This file provides two usage patterns for building Slack messages:
//
// Pattern 1: Build Routing (Recommended)
//
//	blocks := builder.Build(msg) // Automatically routes to correct sub-builder
//
// Pattern 2: Direct Sub-Builder Calls (For specialized control)
//
//	blocks := builder.ToolMessageBuilder.BuildToolUseMessage(msg)
//	blocks := builder.AnswerMessageBuilder.BuildAnswerMessage(msg)
//
// Architecture:
//
//	┌─────────────────────────────────────────────────────────────┐
//	│                     MessageBuilder                            │
//	│                   (Routing Layer - 171 lines)                │
//	└─────────────────────────┬───────────────────────────────────┘
//	                          │ Routes based on msg.Type
//	      ┌───────────────────┼───────────────────┐
//	      ▼                   ▼                   ▼
//	┌───────────┐       ┌───────────┐       ┌───────────┐
//	│   Tool    │       │  Answer   │       │   Plan    │
//	│  Builder  │       │  Builder  │       │  Builder  │
//	└───────────┘       └───────────┘       └───────────┘
//	      │                   │                   │
//	      └───────────────────┼───────────────────┘
//	                          ▼
//	                ┌─────────────────┐
//	                │  slack.Block   │
//	                └─────────────────┘
//
// Public API (MessageBuilder):
//   - Build(msg *base.ChatMessage) []slack.Block (main entry)
//   - ToolMessageBuilder, AnswerMessageBuilder, PlanMessageBuilder,
//     InteractiveMessageBuilder, StatsMessageBuilder, SystemMessageBuilder
//
// Internal API (Sub-builders):
//   - ToolMessageBuilder: BuildToolUseMessage, BuildToolResultMessage
//   - AnswerMessageBuilder: BuildAnswerMessage, BuildErrorMessage
//   - PlanMessageBuilder: BuildPlanModeMessage, BuildExitPlanModeMessage, BuildAskUserQuestionMessage
//   - InteractiveMessageBuilder: BuildDangerBlockMessage, BuildPermissionRequestMessageFromChat
//   - StatsMessageBuilder: BuildSessionStatsMessage, BuildCommandProgressMessage, BuildCommandCompleteMessage
//   - SystemMessageBuilder: BuildSystemMessage, BuildUserMessage, BuildStepStartMessage,
//     BuildStepFinishMessage, BuildRawMessage, BuildUserMessageReceivedMessage
//
// Note: Sub-builders are internal (lowercase) and accessed via MessageBuilder delegates.
// =============================================================================
package slack

import (
	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/provider"
	"github.com/slack-go/slack"
)

// MessageBuilder translates platform-agnostic base.ChatMessage objects into
// rich Slack Block Kit structures, ensuring consistent UX across different message types.
// Now delegates to specialized sub-builders for better maintainability.
type MessageBuilder struct {
	config      *Config
	formatter   *MrkdwnFormatter
	tool        *ToolMessageBuilder
	answer      *AnswerMessageBuilder
	plan        *PlanMessageBuilder
	interactive *InteractiveMessageBuilder
	stats       *StatsMessageBuilder
	system      *SystemMessageBuilder
}

// NewMessageBuilder creates a new MessageBuilder
func NewMessageBuilder(config *Config) *MessageBuilder {
	formatter := NewMrkdwnFormatter()
	return &MessageBuilder{
		config:      config,
		formatter:   formatter,
		tool:        NewToolMessageBuilder(formatter),
		answer:      NewAnswerMessageBuilder(config, formatter),
		plan:        NewPlanMessageBuilder(),
		interactive: NewInteractiveMessageBuilder(),
		stats:       NewStatsMessageBuilder(),
		system:      NewSystemMessageBuilder(),
	}
}

// Build builds Slack blocks from a ChatMessage based on its type
// Delegates to specialized sub-builders
func (b *MessageBuilder) Build(msg *base.ChatMessage) []slack.Block {
	switch msg.Type {
	case base.MessageTypeThinking:
		return nil // Handled by status
	case base.MessageTypeToolUse:
		return b.tool.BuildToolUseMessage(msg)
	case base.MessageTypeToolResult:
		return b.tool.BuildToolResultMessage(msg)
	case base.MessageTypeAnswer:
		return b.answer.BuildAnswerMessage(msg)
	case base.MessageTypeError:
		return b.answer.BuildErrorMessage(msg)
	case base.MessageTypePlanMode:
		return b.plan.BuildPlanModeMessage(msg)
	case base.MessageTypeExitPlanMode:
		return b.plan.BuildExitPlanModeMessage(msg)
	case base.MessageTypeAskUserQuestion:
		return b.plan.BuildAskUserQuestionMessage(msg)
	case base.MessageTypeDangerBlock:
		return b.interactive.BuildDangerBlockMessage(msg)
	case base.MessageTypeSessionStats:
		return b.stats.BuildSessionStatsMessage(msg)
	case base.MessageTypeCommandProgress:
		return b.stats.BuildCommandProgressMessage(msg)
	case base.MessageTypeCommandComplete:
		return b.stats.BuildCommandCompleteMessage(msg)
	case base.MessageTypeSystem:
		return b.system.BuildSystemMessage(msg)
	case base.MessageTypeUser:
		return b.system.BuildUserMessage(msg)
	case base.MessageTypeStepStart:
		return b.system.BuildStepStartMessage(msg)
	case base.MessageTypeStepFinish:
		return b.system.BuildStepFinishMessage(msg)
	case base.MessageTypeRaw:
		return b.system.BuildRawMessage(msg)
	case base.MessageTypeSessionStart:
		return nil // Handled by status
	case base.MessageTypeEngineStarting:
		return nil // Handled by status
	case base.MessageTypeUserMessageReceived:
		return b.system.BuildUserMessageReceivedMessage(msg)
	case base.MessageTypePermissionRequest:
		return b.interactive.BuildPermissionRequestMessageFromChat(msg)
	default:
		// Default to answer message for unknown types
		return b.answer.BuildAnswerMessage(msg)
	}
}

// BuildSessionStatsMessage builds a message for session statistics (backward compatibility)
func (b *MessageBuilder) BuildSessionStatsMessage(msg *base.ChatMessage) []slack.Block {
	return b.stats.BuildSessionStatsMessage(msg)
}

// =============================================================================
// Helper: Extract tool metadata from provider event
// =============================================================================

// ExtractToolInfo extracts tool name and input from ChatMessage metadata
func ExtractToolInfo(msg *base.ChatMessage) (toolName, input string) {
	toolName = msg.Content

	if msg.Metadata != nil {
		if name, ok := msg.Metadata["tool_name"].(string); ok {
			toolName = name
		}
		if in, ok := msg.Metadata["input"].(string); ok {
			input = in
		}
	}

	return toolName, input
}

// =============================================================================
// Constants for compatibility
// =============================================================================

// ToolResultDurationThreshold is the threshold for showing duration
const ToolResultDurationThreshold = 500 // ms

// IsLongRunningTool checks if a tool is considered long-running
func IsLongRunningTool(durationMs int64) bool {
	return durationMs > ToolResultDurationThreshold
}

// ParseProviderEventType converts provider event type to base message type
func ParseProviderEventType(eventType provider.ProviderEventType) base.MessageType {
	switch eventType {
	case provider.EventTypeThinking:
		return base.MessageTypeThinking
	case provider.EventTypeToolUse:
		return base.MessageTypeToolUse
	case provider.EventTypeToolResult:
		return base.MessageTypeToolResult
	case provider.EventTypeAnswer:
		return base.MessageTypeAnswer
	case provider.EventTypeError:
		return base.MessageTypeError
	case provider.EventTypePlanMode:
		return base.MessageTypePlanMode
	case provider.EventTypeExitPlanMode:
		return base.MessageTypeExitPlanMode
	case provider.EventTypeAskUserQuestion:
		return base.MessageTypeAskUserQuestion
	case provider.EventTypeResult:
		return base.MessageTypeSessionStats
	case provider.EventTypeCommandProgress:
		return base.MessageTypeCommandProgress
	case provider.EventTypeCommandComplete:
		return base.MessageTypeCommandComplete
	case provider.EventTypeSystem:
		return base.MessageTypeSystem
	case provider.EventTypeUser:
		return base.MessageTypeUser
	case provider.EventTypeStepStart:
		return base.MessageTypeStepStart
	case provider.EventTypeStepFinish:
		return base.MessageTypeStepFinish
	case provider.EventTypeRaw:
		return base.MessageTypeRaw
	case provider.EventTypeSessionStart:
		return base.MessageTypeSessionStart
	case provider.EventTypeEngineStarting:
		return base.MessageTypeEngineStarting
	case provider.EventTypeUserMessageReceived:
		return base.MessageTypeUserMessageReceived
	default:
		return base.MessageTypeAnswer
	}
}
