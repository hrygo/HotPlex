package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// StreamManager manages Slack native streaming API
// Uses chat.startStream / chat.appendMessage / chat.completeExchange
type StreamManager struct {
	config *Config
	client *http.Client
	logger *slog.Logger
}

// NewStreamManager creates a new StreamManager
func NewStreamManager(config *Config, logger *slog.Logger) *StreamManager {
	return &StreamManager{
		config: config,
		client: http.DefaultClient,
		logger: logger,
	}
}

// StreamState represents the state of a stream
type StreamState struct {
	ChannelID  string
	ThreadTS   string
	MessageTS  string
	IsActive   bool
	ExchangeID string // For streaming exchange
}

// StartStreamRequest is the request for chat.startStream
type StartStreamRequest struct {
	ChannelID       string `json:"channel"`
	ThreadTS        string `json:"thread_ts,omitempty"`
	RecipientUserID string `json:"recipient_user_id,omitempty"`
	RecipientTeamID string `json:"recipient_team_id,omitempty"`
}

// StartStreamResponse is the response from chat.startStream
type StartStreamResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	MessageTS  string `json:"message_ts"`
	ExchangeID string `json:"exchange_id"`
	ChannelID  string `json:"channel_id"`
}

// AppendStreamRequest is the request for chat.appendMessage (in-stream)
type AppendStreamRequest struct {
	ChannelID      string `json:"channel"`
	ThreadTS       string `json:"thread_ts,omitempty"`
	ExchangeID     string `json:"exchange_id"`
	Content        string `json:"content"`
	Subtype        string `json:"subtype,omitempty"`
	MRKDOWN        bool   `json:"mrkdwn,omitempty"`
	MRKDWNSections bool   `json:"mrkdwnections,omitempty"`
}

// AppendStreamResponse is the response from chat.appendMessage
type AppendStreamResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	MessageTS string `json:"message_ts"`
	Timestamp string `json:"ts"`
}

// StopStreamRequest is the request for chat.completeExchange
type StopStreamRequest struct {
	ChannelID  string `json:"channel"`
	ThreadTS   string `json:"thread_ts,omitempty"`
	ExchangeID string `json:"exchange_id"`
	Content    string `json:"content,omitempty"`
	MRKDOWN    bool   `json:"mrkdwn,omitempty"`
}

// StopStreamResponse is the response from chat.completeExchange
type StopStreamResponse struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	MessageTS string `json:"message_ts"`
}

// StartStream starts a new streaming message
func (s *StreamManager) StartStream(ctx context.Context, req *StartStreamRequest) (*StreamState, error) {
	if s.config.BotToken == "" {
		return nil, fmt.Errorf("slack bot token not configured")
	}

	payload := map[string]any{
		"channel": req.ChannelID,
	}
	if req.ThreadTS != "" {
		payload["thread_ts"] = req.ThreadTS
	}
	if req.RecipientUserID != "" {
		payload["recipient_user_id"] = req.RecipientUserID
	}
	if req.RecipientTeamID != "" {
		payload["recipient_team_id"] = req.RecipientTeamID
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.startStream", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.config.BotToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var streamResp StartStreamResponse
	if err := json.Unmarshal(respBody, &streamResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if !streamResp.OK {
		return nil, fmt.Errorf("start stream failed: %s", streamResp.Error)
	}

	s.logger.Debug("Stream started",
		"channel", streamResp.ChannelID,
		"message_ts", streamResp.MessageTS,
		"exchange_id", streamResp.ExchangeID)

	return &StreamState{
		ChannelID:  streamResp.ChannelID,
		MessageTS:  streamResp.MessageTS,
		ExchangeID: streamResp.ExchangeID,
		IsActive:   true,
	}, nil
}

// AppendStream appends content to an active stream
func (s *StreamManager) AppendStream(ctx context.Context, state *StreamState, content string) error {
	if !state.IsActive {
		return fmt.Errorf("stream is not active")
	}

	payload := map[string]any{
		"channel":     state.ChannelID,
		"exchange_id": state.ExchangeID,
		"content":     content,
		"mrkdwn":      true,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.appendMessage", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.config.BotToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var appendResp AppendStreamResponse
	if err := json.Unmarshal(respBody, &appendResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !appendResp.OK {
		return fmt.Errorf("append stream failed: %s", appendResp.Error)
	}

	s.logger.Debug("Stream content appended",
		"exchange_id", state.ExchangeID,
		"content_len", len(content))

	return nil
}

// StopStream completes and stops the streaming message
func (s *StreamManager) StopStream(ctx context.Context, state *StreamState, finalContent string) error {
	if !state.IsActive {
		return fmt.Errorf("stream is not active")
	}

	payload := map[string]any{
		"channel":     state.ChannelID,
		"exchange_id": state.ExchangeID,
	}
	if finalContent != "" {
		payload["content"] = finalContent
		payload["mrkdwn"] = true
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/chat.completeExchange", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.config.BotToken)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var stopResp StopStreamResponse
	if err := json.Unmarshal(respBody, &stopResp); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !stopResp.OK {
		return fmt.Errorf("stop stream failed: %s", stopResp.Error)
	}

	state.IsActive = false

	s.logger.Debug("Stream completed",
		"exchange_id", state.ExchangeID,
		"message_ts", stopResp.MessageTS)

	return nil
}

// IsSupported checks if the streaming API is supported for this workspace
// The streaming API requires specific scope and workspace settings
func (s *StreamManager) IsSupported(ctx context.Context) error {
	// Check if bot has the required scope
	// chat:stream is needed for streaming API
	// For now, we'll assume it's supported if we can make API calls
	return nil
}
