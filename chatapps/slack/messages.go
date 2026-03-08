package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/types"
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
					// Store bot response for final responses
					if types.MessageType(msg.Type).IsStorable() {
						a.storeBotResponse(ctx, sessionID, channelID, threadTS, fallbackText)
					}
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

			// Store bot response for final responses
			if types.MessageType(msg.Type).IsStorable() {
				a.storeBotResponse(ctx, sessionID, channelID, threadTS, fallbackText)
			}
			return nil
		}
	}

	// Store bot response for plain text messages
	err := a.SendToChannelSDK(ctx, channelID, msg.Content, threadTS)
	if err == nil && types.MessageType(msg.Type).IsStorable() {
		// Only store if message type is storable (user_input or final_response)
		a.storeBotResponse(ctx, sessionID, channelID, threadTS, msg.Content)
	}
	return err
}

// SendAttachment sends an attachment to a Slack channel using the Slack SDK
func (a *Adapter) SendAttachment(ctx context.Context, channelID, threadTS string, attachment base.Attachment) error {
	if attachment.URL == "" {
		a.Logger().Debug("Attachment has no URL, skipping", "title", attachment.Title)
		return nil
	}

	a.Logger().Debug("Sending attachment via Slack SDK", "channel", channelID, "title", attachment.Title)

	// Download the file from URL
	resp, err := http.DefaultClient.Get(attachment.URL)
	if err != nil {
		return fmt.Errorf("failed to download attachment: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("failed to download attachment: HTTP %d", resp.StatusCode)
	}

	// Read the file content
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read attachment content: %w", err)
	}

	// Use Slack SDK to upload the file
	params := slack.UploadFileParameters{
		Filename:        attachment.Title,
		Title:           attachment.Title,
		Reader:          bytes.NewReader(content),
		Channel:         channelID,
		ThreadTimestamp: threadTS,
	}

	file, err := a.client.UploadFileContext(ctx, params)
	if err != nil {
		return fmt.Errorf("failed to upload file via Slack SDK: %w", err)
	}

	a.Logger().Debug("Attachment uploaded successfully", "file_id", file.ID, "title", file.Title)
	return nil
}

func validateResponseURL(responseURL string) error {
	parsedURL, err := url.Parse(responseURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS allowed")
	}
	host := strings.ToLower(parsedURL.Hostname())
	if host == "slack.com" || host == "slack-msgs.com" || strings.HasSuffix(host, ".slack.com") {
		return nil
	}
	return fmt.Errorf("invalid domain: %s", host)
}

var ssrfSafeClient = &http.Client{Timeout: 10 * time.Second}

// sendEphemeralMessage sends a message visible only to the user who issued the command
// via the Slack response_url (typically used in slash command responses)
func (a *Adapter) sendEphemeralMessage(responseURL, text string) error {
	// SSRF protection: validate URL before making request
	if err := validateResponseURL(responseURL); err != nil {
		a.Logger().Error("SSRF validation failed", "error", err)
		return fmt.Errorf("response URL validation failed: %w", err)
	}

	payload := map[string]any{
		"response_type": "ephemeral",
		"text":          text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		a.Logger().Error("Failed to marshal ephemeral message", "error", err)
		return err
	}

	resp, err := ssrfSafeClient.Post(responseURL, "application/json", bytes.NewReader(body))
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
// Exclusively uses native Slack Assistant Status API. No fallback.
func (a *Adapter) SetStatus(ctx context.Context, channelID, threadTS string, status base.StatusType, text string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}
	return a.SetAssistantStatus(ctx, channelID, threadTS, text)
}

// ClearStatus implements base.StatusProvider
func (a *Adapter) ClearStatus(ctx context.Context, channelID, threadTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}
	return a.SetAssistantStatus(ctx, channelID, threadTS, "")
}

// StartStream starts a native streaming message and returns message_ts as anchor for subsequent updates
// Slack API: via slack-go library's StartStreamContext
func (a *Adapter) StartStream(ctx context.Context, channelID, threadTS string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	a.Logger().Debug("Starting native stream", "channel_id", channelID, "thread_ts", threadTS)

	options := []slack.MsgOption{slack.MsgOptionMarkdownText(" ")}
	if threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	if teamID, ok := a.channelToTeam.Load(channelID); ok && teamID != "" {
		options = append(options, slack.MsgOptionRecipientTeamID(teamID.(string)))
	}
	if userID, ok := a.channelToUser.Load(channelID); ok && userID != "" {
		options = append(options, slack.MsgOptionRecipientUserID(userID.(string)))
	}

	a.Logger().Debug("Calling Slack StartStream", "channel_id", channelID, "options_count", len(options))
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

	a.Logger().Debug("Calling Slack AppendStream", "channel_id", channelID, "message_ts", messageTS, "content_len", len(content))
	options := []slack.MsgOption{slack.MsgOptionMarkdownText(content)}
	if teamID, ok := a.channelToTeam.Load(channelID); ok && teamID != "" {
		options = append(options, slack.MsgOptionRecipientTeamID(teamID.(string)))
	}
	if userID, ok := a.channelToUser.Load(channelID); ok && userID != "" {
		options = append(options, slack.MsgOptionRecipientUserID(userID.(string)))
	}

	_, _, err := a.client.AppendStreamContext(ctx, channelID, messageTS, options...)
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

// NewStreamWriter creates a new streaming writer for Slack messages
func (a *Adapter) NewStreamWriter(ctx context.Context, userID, channelID, threadTS string) base.StreamWriter {
	w := NewNativeStreamingWriter(ctx, a, userID, channelID, threadTS, func(messageTS string) {
		// This callback is currently unused, but kept for future potential use
	})

	// Set up storage callback to persist the final response when stream closes
	if a.storePlugin != nil && a.sessionMgr != nil {
		sessionID := a.sessionMgr.GetChatSessionID("slack", userID, a.config.BotUserID, channelID, threadTS)
		w.SetStoreCallback(func(content string) {
			a.storeBotResponse(ctx, sessionID, channelID, threadTS, content)
		})
	}

	return w
}
