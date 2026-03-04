package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/slack-go/slack"
)

func (a *Adapter) SendMessage(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	return a.sender.SendMessage(ctx, sessionID, msg)
}

func (a *Adapter) SetSender(fn func(ctx context.Context, sessionID string, msg *base.ChatMessage) error) {
	a.sender.SetSender(fn)
}

// defaultSender sends message via Slack API using MessageBuilder
func (a *Adapter) defaultSender(ctx context.Context, sessionID string, msg *base.ChatMessage) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	channelID := a.extractChannelID(sessionID, msg)
	if channelID == "" {
		return fmt.Errorf("channel_id not found in session")
	}

	threadTS := ""
	if msg.Metadata != nil {
		if ts, ok := msg.Metadata["thread_ts"].(string); ok {
			threadTS = ts
		}
	}

	// Check if this is a message update (has message_ts in metadata)
	var messageTS string
	if msg.Metadata != nil {
		if ts, ok := msg.Metadata["message_ts"].(string); ok {
			messageTS = ts
		}
	}

	if msg.RichContent != nil && len(msg.RichContent.Attachments) > 0 {
		for _, attachment := range msg.RichContent.Attachments {
			if err := a.SendAttachmentSDK(ctx, channelID, threadTS, attachment); err != nil {
				return fmt.Errorf("failed to send attachment: %w", err)
			}
		}

		if msg.Content != "" {
			return a.SendToChannelSDK(ctx, channelID, msg.Content, threadTS)
		}
		return nil
	}

	if a.messageBuilder != nil {
		blocks := a.messageBuilder.Build(msg)
		if len(blocks) > 0 {

			fallbackText := msg.Content
			if fallbackText == "" {

				switch msg.Type {
				case base.MessageTypeToolUse:
					fallbackText = "Using tool..."
				case base.MessageTypeToolResult:
					fallbackText = "Tool completed"
				case base.MessageTypeThinking:
					fallbackText = "Thinking..."
				case base.MessageTypeError:
					fallbackText = "Error occurred"
				default:
					fallbackText = "Message"
				}
			}

			if messageTS != "" {
				err := a.UpdateMessageSDK(ctx, channelID, messageTS, blocks, fallbackText)
				if err == nil {
					return nil
				}

				a.Logger().Warn("Failed to update message, falling back to new message", "error", err, "ts", messageTS)
			}

			ts, err := a.sendBlocksSDK(ctx, channelID, blocks, threadTS, fallbackText)
			if err != nil {
				return err
			}

			if ts != "" && msg.Metadata != nil {
				msg.Metadata["message_ts"] = ts
			}
			return nil
		}
	}

	return a.SendToChannelSDK(ctx, channelID, msg.Content, threadTS)
}

// SendAttachment sends an attachment to a Slack channel
func (a *Adapter) SendAttachment(ctx context.Context, channelID, threadTS string, attachment base.Attachment) error {

	payload := map[string]any{
		"channel": channelID,
	}

	if attachment.URL != "" {
		payload["url"] = attachment.URL
		payload["title"] = attachment.Title
		if threadTS != "" {
			payload["thread_ts"] = threadTS
		}
		return a.sendFileFromURL(ctx, payload)
	}

	a.Logger().Debug("Attachment received", "type", attachment.Type, "title", attachment.Title)
	return nil
}

// sendFileFromURL sends a file from URL to Slack
func (a *Adapter) sendFileFromURL(ctx context.Context, payload map[string]any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/files.upload", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("file upload failed: %d %s", resp.StatusCode, string(respBody))
	}

	var slackResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&slackResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !slackResp.OK {
		return fmt.Errorf("slack API error: %s", slackResp.Error)
	}

	return nil
}

// sendEphemeralMessage sends a message visible only to the user who issued the command
// via the Slack response_url (typically used in slash command responses)
func (a *Adapter) sendEphemeralMessage(responseURL, text string) error {
	payload := map[string]any{
		"response_type": "ephemeral",
		"text":          text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		a.Logger().Error("Failed to marshal ephemeral message", "error", err)
		return err
	}

	resp, err := http.Post(responseURL, "application/json", bytes.NewReader(body))
	if err != nil {
		a.Logger().Error("Failed to send ephemeral message", "error", err)
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	return nil
}

// sendCommandResponse sends a response to a command, using response_url if available,
// or falling back to sending directly to the channel.
// This is used when commands can be triggered from both slash commands (with response_url)
// and thread messages (without response_url).
func (a *Adapter) sendCommandResponse(responseURL, channelID, threadTS, text string) error {

	if responseURL != "" {
		return a.sendEphemeralMessage(responseURL, text)
	}

	if channelID == "" {
		return fmt.Errorf("cannot send response: both response_url and channel_id are empty")
	}

	a.Logger().Debug("No response_url, sending to channel directly", "channel_id", channelID)

	return a.SendToChannel(context.Background(), channelID, text, threadTS)
}

// extractChannelID extracts channel_id from session or message metadata
func (a *Adapter) extractChannelID(_ string, msg *base.ChatMessage) string {
	if msg.Metadata == nil {
		return ""
	}
	if channelID, ok := msg.Metadata["channel_id"].(string); ok {
		return channelID
	}
	return ""
}

func (a *Adapter) SendToChannel(ctx context.Context, channelID, text, threadTS string) error {

	return a.SendToChannelSDK(ctx, channelID, text, threadTS)
}

// SendToChannelSDK sends a text message using Slack SDK
func (a *Adapter) SendToChannelSDK(ctx context.Context, channelID, text, threadTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}

	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	_, _, err := a.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return fmt.Errorf("post message: %w", err)
	}

	a.Logger().Debug("Message sent via SDK", "channel", channelID)
	return nil
}

// sendBlocksSDK sends blocks using Slack SDK and returns message timestamp
func (a *Adapter) sendBlocksSDK(ctx context.Context, channelID string, blocks []slack.Block, threadTS, fallbackText string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fallbackText, false),
	}

	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}

	channel, ts, err := a.client.PostMessageContext(ctx, channelID, opts...)
	if err != nil {
		return "", fmt.Errorf("post blocks: %w", err)
	}

	a.Logger().Debug("Blocks sent via SDK", "channel", channel, "ts", ts)
	return ts, nil
}

// UpdateMessageSDK updates an existing message using Slack SDK
func (a *Adapter) UpdateMessageSDK(ctx context.Context, channelID, messageTS string, blocks []slack.Block, fallbackText string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, _, _, err := a.client.UpdateMessageContext(ctx, channelID, messageTS,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(fallbackText, false),
	)
	if err != nil {
		return fmt.Errorf("update message: %w", err)
	}

	a.Logger().Debug("Message updated via SDK", "channel", channelID, "ts", messageTS)
	return nil
}

// PostTypingIndicator sends a visual indicator that the bot is processing
// Per spec: Triggered when user message received, during processing
// Note: Uses ephemeral context message as typing indicator alternative
func (a *Adapter) PostTypingIndicator(ctx context.Context, channelID, threadTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}
	if channelID == "" {
		return fmt.Errorf("channel_id is required for typing indicator")
	}

	a.Logger().Debug("Typing indicator requested (using reactions/status instead)", "channel", channelID)
	return nil
}

// SendTypingIndicatorForSession sends typing indicator for a session
// Uses session to resolve channel ID
func (a *Adapter) SendTypingIndicatorForSession(ctx context.Context, sessionID string) error {

	session, ok := a.GetSession(sessionID)
	if !ok || session == nil {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	a.Logger().Debug("Typing indicator for session (no-op)", "session_id", sessionID)
	return nil
}

// SendAttachmentSDK sends an attachment using Slack SDK
// Note: Simplified implementation - uses existing custom method
func (a *Adapter) SendAttachmentSDK(ctx context.Context, channelID, threadTS string, attachment base.Attachment) error {

	return a.SendAttachment(ctx, channelID, threadTS, attachment)
}

// DeleteMessageSDK deletes a message using Slack SDK
func (a *Adapter) DeleteMessageSDK(ctx context.Context, channelID, messageTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, _, err := a.client.DeleteMessageContext(ctx, channelID, messageTS)
	if err != nil {
		return fmt.Errorf("delete message: %w", err)
	}

	a.Logger().Debug("Message deleted via SDK", "channel", channelID, "ts", messageTS)
	return nil
}

// PostEphemeralSDK posts an ephemeral message using Slack SDK
func (a *Adapter) PostEphemeralSDK(ctx context.Context, channelID, userID, text string, blocks []slack.Block) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	opts := []slack.MsgOption{
		slack.MsgOptionText(text, false),
	}
	if len(blocks) > 0 {
		opts = append(opts, slack.MsgOptionBlocks(blocks...))
	}

	_, err := a.client.PostEphemeralContext(ctx, channelID, userID, opts...)
	if err != nil {
		return fmt.Errorf("post ephemeral: %w", err)
	}

	a.Logger().Debug("Ephemeral message sent via SDK", "channel", channelID, "user", userID)
	return nil
}

// SetAssistantStatus sets the native assistant status text at the bottom of the thread
// Used for driving dynamic status prompts (e.g., "Thinking...", "Searching code...")
// Slack API: assistant.threads.setStatus
func (a *Adapter) SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	params := slack.AssistantThreadsSetStatusParameters{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		Status:    status,
	}

	return a.client.SetAssistantThreadsStatusContext(ctx, params)
}

// SetStatus implements base.StatusProvider
// Tries native Slack status first, falls back to bubble message
func (a *Adapter) SetStatus(ctx context.Context, channelID, threadTS string, status base.StatusType, text string) error {

	if a.client != nil {
		err := a.SetAssistantStatus(ctx, channelID, threadTS, text)
		if err == nil {
			return nil
		}
		a.Logger().Debug("Native status failed, falling back to bubble", "error", err)
	}

	return a.sendStatusBubble(ctx, channelID, threadTS, status, text)
}

// ClearStatus implements base.StatusProvider
func (a *Adapter) ClearStatus(ctx context.Context, channelID, threadTS string) error {

	if a.client != nil {
		err := a.SetAssistantStatus(ctx, channelID, threadTS, "")
		if err == nil {
			return nil
		}
		a.Logger().Debug("Native status clear failed, falling back", "error", err)
	}

	return nil
}

// StartStream starts a native streaming message and returns message_ts as anchor for subsequent updates
// Slack API: via slack-go library's StartStreamContext
func (a *Adapter) StartStream(ctx context.Context, channelID, threadTS string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	a.Logger().Debug("Starting native stream", "channel_id", channelID, "thread_ts", threadTS)

	options := []slack.MsgOption{slack.MsgOptionText(" ", false)}
	if threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	_, ts, err := a.client.StartStreamContext(ctx, channelID, options...)
	if err != nil {
		a.Logger().Error("StartStream failed", "channel_id", channelID, "error", err)
		return "", fmt.Errorf("start stream: %w", err)
	}

	a.Logger().Debug("Native stream started", "channel_id", channelID, "message_ts", ts)
	return ts, nil
}

// AppendStream incrementally pushes content to an existing stream
// Slack API: via slack-go library's AppendStreamContext
func (a *Adapter) AppendStream(ctx context.Context, channelID, messageTS, content string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	a.Logger().Debug("Appending to stream", "channel_id", channelID, "message_ts", messageTS, "content_len", len(content))

	_, _, err := a.client.AppendStreamContext(ctx, channelID, messageTS,
		slack.MsgOptionText(content, false),
	)
	if err != nil {
		a.Logger().Error("AppendStream failed", "channel_id", channelID, "message_ts", messageTS, "error", err)
		return fmt.Errorf("append stream: %w", err)
	}

	return nil
}

// StopStream ends the stream and finalizes the message
// Slack API: via slack-go library's StopStreamContext
func (a *Adapter) StopStream(ctx context.Context, channelID, messageTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, _, err := a.client.StopStreamContext(ctx, channelID, messageTS)
	if err != nil {
		return fmt.Errorf("stop stream: %w", err)
	}

	return nil
}

// NewStreamWriter creates a platform-specific streaming writer
// Returns StreamWriter interface for platform-agnostic abstraction
func (a *Adapter) NewStreamWriter(ctx context.Context, channelID, threadTS string) base.StreamWriter {
	return NewNativeStreamingWriter(ctx, a, channelID, threadTS, nil)
}
