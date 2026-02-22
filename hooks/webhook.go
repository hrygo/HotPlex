package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type WebhookPayload struct {
	Event     EventType   `json:"event"`
	Timestamp time.Time   `json:"timestamp"`
	Namespace string      `json:"namespace,omitempty"`
	SessionID string      `json:"session_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

type WebhookConfig struct {
	URL          string            `json:"url"`
	Method       string            `json:"method"`
	Headers      map[string]string `json:"headers"`
	Timeout      time.Duration     `json:"timeout"`
	Secret       string            `json:"secret"`
	FilterEvents []EventType       `json:"filter_events"`
}

type WebhookHook struct {
	name   string
	config WebhookConfig
	client *http.Client
	logger *slog.Logger
	events []EventType
}

func NewWebhookHook(name string, config WebhookConfig, logger *slog.Logger) *WebhookHook {
	if config.Method == "" {
		config.Method = "POST"
	}
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}

	events := config.FilterEvents
	if len(events) == 0 {
		events = []EventType{
			EventSessionStart,
			EventSessionEnd,
			EventSessionError,
			EventDangerBlocked,
		}
	}

	return &WebhookHook{
		name:   name,
		config: config,
		client: &http.Client{Timeout: config.Timeout},
		logger: logger,
		events: events,
	}
}

func (h *WebhookHook) Name() string {
	return h.name
}

func (h *WebhookHook) Events() []EventType {
	return h.events
}

func (h *WebhookHook) Handle(ctx context.Context, event *Event) error {
	payload := WebhookPayload{
		Event:     event.Type,
		Timestamp: event.Timestamp,
		Namespace: event.Namespace,
		SessionID: event.SessionID,
		Data:      event.Data,
		Error:     event.Error,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, h.config.Method, h.config.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range h.config.Headers {
		req.Header.Set(k, v)
	}

	if h.config.Secret != "" {
		req.Header.Set("X-HotPlex-Signature", h.computeSignature(body))
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	h.logger.Debug("Webhook sent successfully",
		"hook", h.name,
		"url", h.config.URL,
		"event", event.Type)

	return nil
}

func (h *WebhookHook) computeSignature(body []byte) string {
	mac := hmac.New(sha256.New, []byte(h.config.Secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func (h *WebhookHook) Close() error {
	h.client.CloseIdleConnections()
	return nil
}
