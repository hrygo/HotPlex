package slack

import (
	"context"
	"time"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/toolfmt"
	"github.com/hrygo/hotplex/pkg/events"
)

// envelopeSendFunc sends an envelope-based interaction or status event to Slack.
type envelopeSendFunc func(ctx context.Context, env *events.Envelope) error

// notifyStatusFromEvent maps AEP events to processing status indicators.
func (c *SlackConn) notifyStatusFromEvent(ctx context.Context, env *events.Envelope) {
	switch env.Event.Type {
	case events.Reasoning:
		_ = c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, StatusThinking, "Thinking...")
	case events.ToolCall:
		name, input := extractCallNameInput(env)
		text := toolfmt.FormatCall(name, input)
		if text == "" {
			text = name
		}
		_ = c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, StatusToolUse, truncateWithSuffix(text, statusTextLimit))
		c.adapter.statusMgr.SetLastTool(c.channelID, c.threadTS, name)
		if text == name {
			c.adapter.statusMgr.LogOnceUnregistered(name)
		}
	case events.ToolResult:
		toolName := c.adapter.statusMgr.LastTool(c.channelID, c.threadTS)
		output, errMsg := extractResultFields(env)
		text := toolfmt.FormatResult(toolName, output, errMsg)
		if text == "" {
			text = "Tool completed"
		}
		_ = c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, StatusToolResult, truncateWithSuffix(c.adapter.statusMgr.shortenPaths(text), statusTextLimit))
	default:
		if env.Event.Type == events.MessageDelta {
			_ = c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, StatusAnswering, "Composing response...")
		}
	}
}

func (c *SlackConn) handleDone(ctx context.Context, env *events.Envelope) error {
	c.clearStatus(ctx)
	c.adapter.Interactions.CancelAll(env.SessionID)
	c.paraBreaker.Reset()

	var fullText string
	if w := c.getStreamWriter(); w != nil {
		fullText = w.Content()
	}
	c.closeStreamWriter()

	if c.adapter.turnSummaryEnabled {
		go c.sendTurnSummary(ctx, env)
	}
	if c.adapter.ttsPipeline != nil && c.voiceTriggered.Load() {
		if fullText != "" {
			ttsCtx, ttsCancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
			go func() {
				defer ttsCancel()
				c.adapter.ttsPipeline.Process(ttsCtx, fullText, c.channelID, c.threadTS)
			}()
		}
		c.voiceTriggered.Store(false)
	}
	return nil
}

func (c *SlackConn) handleError(ctx context.Context, env *events.Envelope) error {
	c.clearStatus(ctx)
	c.adapter.Interactions.CancelAll(env.SessionID)
	c.paraBreaker.Reset()
	c.closeStreamWriter()

	if errMsg := messaging.ExtractErrorMessage(env); errMsg != "" {
		go func() { _ = c.writeWithPostMessage(ctx, FormatMrkdwn(errPrefix+errMsg), false) }()
	} else {
		// Worker produced an error event with an empty message — still
		// notify the user so they know something went wrong, rather than
		// seeing the status vanish with no explanation.
		go func() {
			_ = c.writeWithPostMessage(ctx, FormatMrkdwn(errPrefix+"An internal error occurred. Please try again or use /reset."), false)
		}()
	}
	return nil
}

func (c *SlackConn) handleInteraction(ctx context.Context, env *events.Envelope, statusText string, send envelopeSendFunc) error {
	c.closeStreamWriter()
	c.notifyStatus(ctx, statusText)
	err := send(ctx, env)
	c.clearStatus(ctx)
	return err
}

func (c *SlackConn) handleNotifyAndSend(ctx context.Context, env *events.Envelope, statusText string, send envelopeSendFunc) error {
	c.notifyStatus(ctx, statusText)
	err := send(ctx, env)
	c.clearStatus(ctx)
	return err
}

func (c *SlackConn) handleSkillsList(ctx context.Context, env *events.Envelope) error {
	c.notifyStatus(ctx, "Loading skills...")
	err := c.sendSkillsList(ctx, env)
	c.clearStatus(ctx)
	if err == nil || !isInvalidBlocksError(err) {
		return err
	}
	c.adapter.Log.Warn("slack: skills blocks rejected, falling back to plain text", "err", err)
	return c.postSkillsMessageFallback(ctx, env)
}

func (c *SlackConn) handleStandaloneMessage(ctx context.Context, env *events.Envelope) error {
	// Supplement path: gateway accepted a mid-turn supplement and signals via
	// metadata. Render the i18n busy text synchronously and return before the
	// normal display path, which would silently drop the (empty) Content.
	// Synchronous (not goroutine-launch) so callers see send failures and tests
	// can observe the reached path; the supplement text is a one-shot short msg.
	if mode, _ := env.Metadata["supplement_mode"].(string); mode != "" {
		return c.writeWithPostMessage(ctx, slackSupplementText(mode), false)
	}

	var text string
	if msgData, ok := env.Event.Data.(events.MessageData); ok && msgData.Content != "" {
		text = messaging.SanitizeText(msgData.Content)
	} else if m, ok := env.Event.Data.(map[string]any); ok {
		if content, ok := m["content"].(string); ok && content != "" {
			text = messaging.SanitizeText(content)
		}
	}
	if text != "" {
		go func() {
			if err := c.writeWithPostMessage(ctx, FormatMrkdwn(text), false); err != nil {
				c.adapter.Log.Debug("slack: failed to send message event", "err", err)
			}
		}()
	}
	return nil
}

// slackSupplementText returns the English i18n text for a mid-turn supplement
// accepted by the gateway's busy branch. `injected` means the supplement was
// merged into the running turn; any other mode (typically `buffered`) means it
// was queued for the next turn. The fallback is the buffered text — promising
// future handling — so an unexpected mode never falsely implies the supplement
// is already being processed.
func slackSupplementText(mode string) string {
	if mode == "injected" {
		return "⏳ Got it — processing within the current task."
	}
	return "⏳ Got it — will process automatically once the current task finishes."
}

func (c *SlackConn) handleDefaultText(ctx context.Context, env *events.Envelope) error {
	text, ok := messaging.ExtractResponseText(env)
	if !ok {
		return nil
	}
	text = messaging.SanitizeText(text)

	if env.Event.Type == events.MessageDelta {
		if c.paraBreaker.Add(text) {
			text += "\n\n"
		}
	}

	// Try file upload for document paths generated by AI.
	if env.Event.Type != events.MessageDelta {
		if uploaded := c.tryFileUpload(ctx, text); uploaded {
			return nil
		}
	}

	if env.Event.Type == events.MessageDelta || env.Event.Type == "text" {
		if err := c.writeWithStreaming(ctx, text); err != nil {
			c.adapter.Log.Info("slack: streaming failed, falling back to PostMessage",
				"channel", c.channelID, "err", err)
			return c.writeWithPostMessage(ctx, text, env.Event.Type == events.MessageDelta)
		}
		return nil
	}

	return c.writeWithPostMessage(ctx, text, false)
}
