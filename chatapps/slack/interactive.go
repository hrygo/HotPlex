package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/slack-go/slack"
)

func (a *Adapter) handleInteractive(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		a.Logger().Error("Read body failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	defer func() { _ = r.Body.Close() }()

	payload := r.FormValue("payload")
	if payload == "" {

		payload = string(body)
	}

	var callback SlackInteractionCallback
	if err := json.Unmarshal([]byte(payload), &callback); err != nil {
		a.Logger().Error("Parse callback failed", "error", err)
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	if len(callback.Actions) == 0 {
		a.Logger().Warn("No actions in callback")
		w.WriteHeader(http.StatusOK)
		return
	}

	a.Logger().Debug("Interaction callback parsed",
		"type", callback.Type,
		"user", callback.User.ID,
		"channel", callback.Channel.ID,
		"action_id", callback.Actions[0].ActionID,
		"block_id", callback.Actions[0].BlockID,
		"value", callback.Actions[0].Value,
	)

	switch callback.Type {
	case "block_actions":
		a.handleBlockActions(&callback, w)
	default:
		a.Logger().Warn("Unknown interaction type", "type", callback.Type)
		w.WriteHeader(http.StatusOK)
	}
}

// handleBlockActions handles Slack block_actions callbacks (button clicks, etc.)
func (a *Adapter) handleBlockActions(callback *SlackInteractionCallback, w http.ResponseWriter) {
	action := callback.Actions[0]
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	_ = messageTS

	a.Logger().Debug("Block action received",
		"action_id", action.ActionID,
		"block_id", action.BlockID,
		"value", action.Value,
		"user_id", userID,
		"channel_id", channelID,
	)

	actionID := action.ActionID

	if strings.HasPrefix(actionID, "perm_allow:") || strings.HasPrefix(actionID, "perm_deny:") {
		a.handlePermissionCallback(callback, action, w)
		return
	}

	if actionID == "plan_approve" || actionID == "plan_modify" || actionID == "plan_cancel" {
		a.handlePlanModeCallback(callback, action, w)
		return
	}

	if strings.HasPrefix(actionID, "danger_confirm") || strings.HasPrefix(actionID, "danger_cancel") {
		a.handleDangerBlockCallback(callback, action, w)
		return
	}

	if strings.HasPrefix(actionID, "question_option_") {
		a.handleAskUserQuestionCallback(callback, action, w)
		return
	}

	a.Logger().Info("Unhandled block action",
		"action_id", actionID,
		"value", action.Value,
	)

	w.WriteHeader(http.StatusOK)
}

// handlePermissionCallback handles permission approval/denial button clicks
// ActionID format: perm_allow:{sessionID}:{messageID} or perm_deny:{sessionID}:{messageID}
// Value format: "allow" or "deny"
func (a *Adapter) handlePermissionCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	actionID := action.ActionID

	a.Logger().Info("Permission callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"action_id", actionID,
	)

	parts := strings.Split(actionID, ":")
	if len(parts) < 3 {
		a.Logger().Error("Invalid permission action_id format", "action_id", actionID)
		w.WriteHeader(http.StatusOK)
		return
	}

	behavior := parts[0]
	sessionID := parts[1]
	messageID := parts[2]

	// Map behavior to actual permission response
	var permissionBehavior string
	if strings.HasSuffix(behavior, "allow") {
		permissionBehavior = "allow"
	} else {
		permissionBehavior = "deny"
	}

	if a.eng != nil {
		if sess, ok := a.eng.GetSession(sessionID); ok {
			response := map[string]any{
				"type":       "permission_response",
				"message_id": messageID,
				"behavior":   permissionBehavior,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send permission response to engine", "error", err)
			} else {
				a.Logger().Info("Sent permission response to engine",
					"session_id", sessionID,
					"behavior", permissionBehavior)
			}
		} else {
			a.Logger().Warn("Session not found for permission response", "session_id", sessionID)
		}
	}

	// Use MessageBuilder for creating response blocks
	var slackBlocks []slack.Block
	if permissionBehavior == "allow" {
		slackBlocks = a.messageBuilder.BuildPermissionApprovedMessage("", "")
	} else {
		slackBlocks = a.messageBuilder.BuildPermissionDeniedMessage("", "", "User denied permission")
	}

	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	a.Logger().Info("Permission request processed",
		"behavior", permissionBehavior,
		"session_id", sessionID,
		"message_id", messageID,
	)

	w.WriteHeader(http.StatusOK)
}

// handlePlanModeCallback handles plan mode approval/denial button clicks
// Value format: approve:{sessionID} or deny:{sessionID}
func (a *Adapter) handlePlanModeCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	value := action.Value

	a.Logger().Info("Plan mode callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"value", value,
		"action_id", action.ActionID,
	)

	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		a.Logger().Error("Invalid plan mode button value", "value", value)
		w.WriteHeader(http.StatusOK)
		return
	}

	actionType := parts[0]
	sessionID := parts[1]

	// Determine behavior for engine response
	var behavior string
	switch actionType {
	case "approve":
		behavior = "allow"
	case "deny", "cancel":
		behavior = "deny"
	case "modify":
		behavior = "deny"
	default:
		behavior = "deny"
	}

	if a.eng != nil {
		if sess, ok := a.eng.GetSession(sessionID); ok {
			response := map[string]any{
				"type":     "plan_response",
				"behavior": behavior,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send plan response to engine", "error", err)
			} else {
				a.Logger().Info("Sent plan response to engine",
					"session_id", sessionID,
					"behavior", behavior)
			}
		} else {
			a.Logger().Warn("Session not found for plan response", "session_id", sessionID)
		}
	}

	// Use MessageBuilder for creating response blocks
	var slackBlocks []slack.Block
	switch actionType {
	case "approve":
		slackBlocks = a.messageBuilder.BuildPlanApprovedBlock()
	case "modify":
		slackBlocks = a.messageBuilder.BuildPlanCancelledBlock("User requested changes")
	case "deny", "cancel":
		slackBlocks = a.messageBuilder.BuildPlanCancelledBlock("User cancelled")
	}

	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	a.Logger().Info("Plan mode request processed",
		"action", actionType,
		"session_id", sessionID,
	)

	w.WriteHeader(http.StatusOK)
}

// handleDangerBlockCallback handles danger block confirmation button clicks
// Value format: confirm:{sessionID} or cancel:{sessionID}
func (a *Adapter) handleDangerBlockCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	actionID := action.ActionID
	value := action.Value

	a.Logger().Info("Danger block callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"action_id", actionID,
		"value", value,
	)

	parts := strings.Split(value, ":")
	if len(parts) < 2 {
		a.Logger().Error("Invalid danger button value", "value", value)
		w.WriteHeader(http.StatusOK)
		return
	}

	actionType := parts[0]
	sessionID := parts[1]

	// Map behavior to actual response
	var permissionBehavior string
	if actionType == "confirm" {
		permissionBehavior = "allow"
	} else {
		permissionBehavior = "deny"
	}

	if a.eng != nil {
		if sess, ok := a.eng.GetSession(sessionID); ok {
			response := map[string]any{
				"type":     "danger_response",
				"behavior": permissionBehavior,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send danger response to engine", "error", err)
			} else {
				a.Logger().Info("Sent danger response to engine",
					"session_id", sessionID,
					"behavior", permissionBehavior)
			}
		} else {
			a.Logger().Warn("Session not found for danger response", "session_id", sessionID)
		}
	}

	if permissionBehavior == "allow" {

		a.Logger().Info("Danger block confirmed, message will continue processing",
			"session_id", sessionID)

	} else {

		a.Logger().Warn("Danger block cancelled, triggering security audit",
			"session_id", sessionID,
			"user_id", userID)

	}

	statusText := ":white_check_mark: Confirmed"
	if permissionBehavior == "deny" {
		statusText = ":x: Cancelled"
	}
	statusObj := slack.NewTextBlockObject("mrkdwn", statusText, false, false)
	slackBlocks := []slack.Block{slack.NewSectionBlock(statusObj, nil, nil)}

	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

// handleAskUserQuestionCallback handles ask user question option selection
// ActionID format: question_option_{i}
func (a *Adapter) handleAskUserQuestionCallback(callback *SlackInteractionCallback, action SlackAction, w http.ResponseWriter) {
	userID := callback.User.ID
	channelID := callback.Channel.ID
	messageTS := callback.Message.Ts
	actionID := action.ActionID
	value := action.Value

	a.Logger().Info("Ask user question callback received",
		"user_id", userID,
		"channel_id", channelID,
		"message_ts", messageTS,
		"action_id", actionID,
		"value", value,
	)

	selectedOption := value
	if selectedOption == "" {

		if opt, found := strings.CutPrefix(actionID, "question_option_"); found {
			selectedOption = opt
		}
	}

	baseSession := a.FindSessionByUserAndChannel(userID, channelID)
	if baseSession == nil {
		a.Logger().Warn("No active session found for question response",
			"user_id", userID,
			"channel_id", channelID)
	} else if a.eng != nil {
		if sess, ok := a.eng.GetSession(baseSession.SessionID); ok {
			response := map[string]any{
				"type":    "question_response",
				"option":  selectedOption,
				"user_id": userID,
			}
			if err := sess.WriteInput(response); err != nil {
				a.Logger().Error("Failed to send question response to engine", "error", err)
			} else {
				a.Logger().Info("Sent question response to engine",
					"session_id", baseSession.SessionID,
					"option", selectedOption)
			}
		}
	}

	statusText := fmt.Sprintf(":white_check_mark: Selected: %s", selectedOption)
	statusObj := slack.NewTextBlockObject("mrkdwn", statusText, false, false)
	slackBlocks := []slack.Block{slack.NewSectionBlock(statusObj, nil, nil)}

	if err := a.UpdateMessageSDK(context.Background(), channelID, messageTS, slackBlocks, ""); err != nil {
		a.Logger().Error("Update message failed", "error", err)
	}

	w.WriteHeader(http.StatusOK)
}

// SlackInteractionCallback represents a Slack interaction callback payload.
type SlackInteractionCallback struct {
	Type        string          `json:"type"`
	User        CallbackUser    `json:"user"`
	Channel     CallbackChannel `json:"channel"`
	Message     CallbackMessage `json:"message"`
	ResponseURL string          `json:"response_url"`
	TriggerID   string          `json:"trigger_id"`
	Actions     []SlackAction   `json:"actions"`
	Team        CallbackTeam    `json:"team"`
}

// CallbackUser represents the user in a Slack callback.
type CallbackUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// CallbackChannel represents the channel in a Slack callback.
type CallbackChannel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// CallbackMessage represents the message in a Slack callback.
type CallbackMessage struct {
	Ts   string `json:"ts"`
	Type string `json:"type"`
	Text string `json:"text"`
}

// CallbackTeam represents the team in a Slack callback.
type CallbackTeam struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
}

// SlackAction represents an action within a Slack interaction callback.
type SlackAction struct {
	ActionID string `json:"action_id"`
	BlockID  string `json:"block_id"`
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Style    string `json:"style"`
}
