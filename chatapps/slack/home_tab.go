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

// HomeTabManager manages Slack App Home Tab
// Provides persistent UI beyond messages for session management and stats
type HomeTabManager struct {
	config *Config
	logger *slog.Logger
}

// NewHomeTabManager creates a new HomeTabManager
func NewHomeTabManager(config *Config, logger *slog.Logger) *HomeTabManager {
	return &HomeTabManager{
		config: config,
		logger: logger,
	}
}

// HomeTabView represents a Home Tab view configuration
type HomeTabView struct {
	Type       string           `json:"type"`
	Blocks     []map[string]any `json:"blocks,omitempty"`
	CallbackID string           `json:"callback_id,omitempty"`
}

// SessionInfo represents session information for Home Tab
type SessionInfo struct {
	SessionID    string `json:"session_id"`
	ChannelID    string `json:"channel_id"`
	UserID       string `json:"user_id"`
	Status       string `json:"status"`
	StartTime    string `json:"start_time,omitempty"`
	Duration     string `json:"duration,omitempty"`
	MessageCount int    `json:"message_count"`
	ToolCount    int    `json:"tool_count"`
}

// BuildHomeTabView builds the Home Tab view with session info and stats
func (h *HomeTabManager) BuildHomeTabView(sessions []SessionInfo, stats map[string]any) *HomeTabView {
	var blocks []map[string]any

	// Header
	blocks = append(blocks, map[string]any{
		"type": "header",
		"text": map[string]any{
			"type":  "plain_text",
			"text":  "HotPlex 🤖",
			"emoji": true,
		},
	})

	// Welcome message
	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": "Welcome to HotPlex! Your AI-powered development assistant.",
		},
	})

	// Divider
	blocks = append(blocks, map[string]any{
		"type": "divider",
	})

	// Active Sessions
	if len(sessions) > 0 {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "*Active Sessions*",
			},
		})

		// Session list
		for _, session := range sessions {
			fields := []map[string]any{
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Channel:*\n%s", session.ChannelID),
				},
				{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Status:*\n%s", session.Status),
				},
			}

			blocks = append(blocks, map[string]any{
				"type":   "section",
				"fields": fields,
			})
		}
	}

	// Stats (if available)
	if stats != nil {
		blocks = append(blocks, map[string]any{
			"type": "divider",
		})

		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": "*Statistics*",
			},
		})

		var statsText string
		for k, v := range stats {
			statsText += fmt.Sprintf("• %s: %v\n", k, v)
		}

		if statsText != "" {
			blocks = append(blocks, map[string]any{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": statsText,
				},
			})
		}
	}

	// Help section
	blocks = append(blocks, map[string]any{
		"type": "divider",
	})

	blocks = append(blocks, map[string]any{
		"type": "section",
		"text": map[string]any{
			"type": "mrkdwn",
			"text": "*Commands*\n" +
				"• `/reset` - Reset current session\n" +
				"• `/dc` - Disconnect session\n" +
				"• `@hotplex` - Mention to get help",
		},
	})

	// Quick actions
	blocks = append(blocks, map[string]any{
		"type": "actions",
		"elements": []map[string]any{
			{
				"type": "button",
				"text": map[string]any{
					"type":  "plain_text",
					"text":  "New Session",
					"emoji": true,
				},
				"action_id": "home_new_session",
				"style":     "primary",
			},
			{
				"type": "button",
				"text": map[string]any{
					"type":  "plain_text",
					"text":  "View Docs",
					"emoji": true,
				},
				"action_id": "home_view_docs",
				"url":       "https://hotplex.dev/docs",
			},
		},
	})

	return &HomeTabView{
		Type:       "home",
		Blocks:     blocks,
		CallbackID: "home_tab",
	}
}

// PublishHomeTab publishes the Home Tab view for a user
func (h *HomeTabManager) PublishHomeTab(ctx context.Context, userID string, view *HomeTabView) error {
	if h.config.BotToken == "" {
		return fmt.Errorf("slack bot token not configured")
	}

	payload := map[string]any{
		"user_id": userID,
		"view":    view,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/views.publish", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("publish home tab failed: %s", result.Error)
	}

	h.logger.Debug("Home tab published", "user_id", userID)
	return nil
}

// OpenHomeTab opens the Home Tab for a user (alternative method)
func (h *HomeTabManager) OpenHomeTab(ctx context.Context, userID string, view *HomeTabView) error {
	if h.config.BotToken == "" {
		return fmt.Errorf("slack bot token not configured")
	}

	payload := map[string]any{
		"user_id":    userID,
		"view":       view,
		"trigger_id": "", // Should be provided by the triggering interaction
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://slack.com/api/views.open", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.config.BotToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	var result struct {
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("open home tab failed: %s", result.Error)
	}

	return nil
}
