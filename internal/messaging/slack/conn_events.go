package slack

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/toolfmt"
	"github.com/hrygo/hotplex/internal/observability"
	"github.com/hrygo/hotplex/pkg/events"
)

// envelopeSendFunc sends an envelope-based interaction or status event to Slack.
type envelopeSendFunc func(ctx context.Context, env *events.Envelope) error

const (
	// terminalDeliveryFallbackText is the ONLY terminal fallback the conn
	// sends when the streamed body was not presented: a fixed short text.
	// Full answers and raw worker errors are never re-sent here.
	terminalDeliveryFallbackText   = "⚠️ Reply delivery failed. Please try again."
	defaultTerminalDeliveryTimeout = 5 * time.Second
)

// terminalDeliveryContext bounds terminal close + fallback with one shared
// budget: an existing caller deadline is kept (never extended), otherwise a
// default 5s timeout applies.
func terminalDeliveryContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, defaultTerminalDeliveryTimeout)
}

// recordTerminalFailure increments the platform streaming terminal failure
// counter with the platform and fallback result attributes.
func (c *SlackConn) recordTerminalFailure(ctx context.Context, result string) {
	observability.StreamingTerminalFailures().Add(ctx, 1,
		metric.WithAttributes(
			attribute.String("platform", "slack"),
			attribute.String("fallback_result", result),
		))
}

// terminalFailureResult maps a terminal close error to the fallback_result
// attribute for paths that cannot attempt a fallback send themselves
// (conn Close, control-command abort): the body-presented case skips any
// re-delivery, everything else counts as failed delivery.
func terminalFailureResult(err error) string {
	var terminalErr *StreamTerminalError
	if errors.As(err, &terminalErr) && terminalErr.ContentPresented {
		return "skipped_body_presented"
	}
	return "failed"
}

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

// handleTerminalDeliveryError preserves the finalization error for upstream
// visibility. It sends one short, independent terminal message only when the
// stream did not present the body; decoration-only failures must never
// duplicate the full response. A successful fallback send also increments the
// platform terminal fallback counter.
func (c *SlackConn) handleTerminalDeliveryError(ctx context.Context, closeErr error) error {
	var terminalErr *StreamTerminalError
	if !errors.As(closeErr, &terminalErr) {
		return closeErr
	}

	if terminalErr.ContentPresented {
		c.adapter.Log.Warn("slack: terminal decoration failed after body was presented", "err", closeErr)
		c.recordTerminalFailure(ctx, "skipped_body_presented")
		return closeErr
	}

	if err := c.writeWithPostMessage(ctx, terminalDeliveryFallbackText, false); err != nil {
		c.adapter.Log.Warn("slack: terminal fallback delivery failed", "err", err)
		c.recordTerminalFailure(ctx, "failed")
		return errors.Join(closeErr, fmt.Errorf("slack: terminal fallback delivery: %w", err))
	}

	c.adapter.Log.Warn("slack: terminal fallback delivered after streaming close failure", "err", closeErr)
	c.recordTerminalFailure(ctx, "sent")
	observability.PlatformTerminalFallback().Add(ctx, 1)
	return closeErr
}

func (c *SlackConn) handleDone(ctx context.Context, env *events.Envelope) error {
	terminalCtx, terminalCancel := terminalDeliveryContext(ctx)
	defer terminalCancel()

	c.clearStatus(terminalCtx)
	c.adapter.Interactions.CancelAll(env.SessionID)
	c.paraBreaker.Reset()

	// Read Content() before closing so the terminal decision uses the full
	// accumulated text; close synchronously so close and fallback share the
	// terminal deadline.
	var fullText string
	if w := c.getStreamWriter(); w != nil {
		fullText = w.Content()
	}
	closeErr := c.closeStreamWriter(terminalCtx)
	if closeErr != nil {
		closeErr = c.handleTerminalDeliveryError(terminalCtx, closeErr)
	}

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
	return closeErr
}

func (c *SlackConn) handleError(ctx context.Context, env *events.Envelope) error {
	terminalCtx, terminalCancel := terminalDeliveryContext(ctx)
	defer terminalCancel()

	c.clearStatus(terminalCtx)
	c.adapter.Interactions.CancelAll(env.SessionID)
	c.paraBreaker.Reset()

	// Close synchronously, then send the controlled error text synchronously
	// so send failures surface here instead of being swallowed by a goroutine.
	closeErr := c.closeStreamWriter(terminalCtx)
	if closeErr != nil {
		closeErr = c.handleTerminalDeliveryError(terminalCtx, closeErr)
	}

	var sendErr error
	if errMsg := messaging.ExtractErrorMessage(env); errMsg != "" {
		sendErr = c.writeWithPostMessage(terminalCtx, FormatMrkdwn(errPrefix+errMsg), false)
	} else {
		// Worker produced an error event with an empty message — still
		// notify the user so they know something went wrong, rather than
		// seeing the status vanish with no explanation.
		sendErr = c.writeWithPostMessage(terminalCtx, FormatMrkdwn(errPrefix+"An internal error occurred. Please try again or use /reset."), false)
	}
	return errors.Join(closeErr, sendErr)
}

func (c *SlackConn) handleInteraction(ctx context.Context, env *events.Envelope, statusText string, send envelopeSendFunc) error {
	closeErr := c.closeStreamWriter(ctx)
	c.notifyStatus(ctx, statusText)
	err := send(ctx, env)
	c.clearStatus(ctx)
	return errors.Join(closeErr, err)
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
	// metadata. Only buffered supplements (queued for the next turn) get a
	// user-facing ack — injected supplements are merged into the running turn
	// silently (the result appears in the current reply, so a confirmation
	// would be redundant). slackSupplementText returns "" for silent modes.
	// Synchronous (not goroutine-launch) so callers see send failures and tests
	// can observe the reached path; the supplement text is a one-shot short msg.
	if mode, _ := env.Metadata["supplement_mode"].(string); mode != "" {
		text := slackSupplementText(mode)
		if text == "" {
			return nil // injected: merged into current turn — silent
		}
		return c.writeWithPostMessage(ctx, text, false)
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
// accepted by the gateway's busy branch. A buffered supplement (queued for the
// next turn) returns a user-facing ack so the user knows their message wasn't
// lost; any unrecognized mode falls back to this text as a safe default. An
// injected supplement (merged into the running turn) returns "" to stay silent
// — the result will appear as part of the current reply, so a confirmation is
// redundant noise. Callers must treat "" as "do not send".
func slackSupplementText(mode string) string {
	if mode == "injected" {
		return "" // merged into current turn — silent
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
