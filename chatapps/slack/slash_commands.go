package slack

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hrygo/hotplex/chatapps/base"
	"github.com/hrygo/hotplex/chatapps/command"
	"github.com/hrygo/hotplex/event"
	"github.com/hrygo/hotplex/internal/panicx"
)

// SlashCommand represents a Slack slash command
type SlashCommand struct {
	Command     string
	Text        string
	UserID      string
	ChannelID   string
	ThreadTS    string // For thread support (#command)
	ResponseURL string
}

// SetSlashCommandHandler sets the handler for slash commands
func (a *Adapter) SetSlashCommandHandler(fn func(cmd SlashCommand)) {
	a.slashCommandHandler = fn
}

// handleSlashCommand processes incoming slash commands
func (a *Adapter) handleSlashCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		a.Logger().Error("Parse slash command form failed", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	cmd := SlashCommand{
		Command:     r.FormValue("command"),
		Text:        r.FormValue("text"),
		UserID:      r.FormValue("user_id"),
		ChannelID:   r.FormValue("channel_id"),
		ResponseURL: r.FormValue("response_url"),
	}

	a.Logger().Debug("Slash command received",
		"command", cmd.Command,
		"text", cmd.Text,
		"user", cmd.UserID)

	if !a.rateLimiter.Allow(cmd.UserID) {
		a.Logger().Warn("Rate limit exceeded", "user_id", cmd.UserID)
		_ = a.sendEphemeralMessage(cmd.ResponseURL, "⚠️ Rate limit exceeded. Please wait a moment.")
		return
	}

	// Check owner policy permission
	if !a.config.CanRespond(cmd.UserID) {
		a.Logger().Warn("Unauthorized slash command attempt",
			"user_id", cmd.UserID,
			"command", cmd.Command,
			"policy", a.config.GetOwnerPolicy())
		_ = a.sendEphemeralMessage(cmd.ResponseURL, "🚫 You are not authorized to use this command.")
		return
	}

	w.WriteHeader(http.StatusOK)

	go a.processSlashCommand(cmd)
}

// processSlashCommand handles the slash command logic
func (a *Adapter) processSlashCommand(cmd SlashCommand) {

	baseSession := a.FindSessionByUserAndChannel(cmd.UserID, cmd.ChannelID)
	var sessionID string
	if baseSession != nil {
		sessionID = baseSession.SessionID
	}

	req := &command.Request{
		Command:     cmd.Command,
		Text:        cmd.Text,
		UserID:      cmd.UserID,
		ChannelID:   cmd.ChannelID,
		ThreadTS:    cmd.ThreadTS,
		SessionID:   sessionID,
		ResponseURL: cmd.ResponseURL,
	}

	// Create callback for progress events
	var progressTS string
	callback := func(eventType string, data any) error {
		return a.handleCommandProgress(cmd.ChannelID, cmd.ThreadTS, &progressTS, eventType, data)
	}

	_, err := a.cmdRegistry.Execute(context.Background(), req, callback)
	if err != nil {
		a.Logger().Error("Command execution failed", "command", cmd.Command, "error", err)
		_ = a.sendCommandResponse(cmd.ResponseURL, cmd.ChannelID, cmd.ThreadTS, "Command execution failed: "+err.Error())
		return
	}
}

const (
	// CommandReset represents the /reset command
	CommandReset = "/reset"
	// CommandDisconnect represents the /dc command
	CommandDisconnect = "/dc"
)

// SUPPORTED_COMMANDS lists all slash commands supported by the system.
// Used for matching #<command> prefix in messages (thread support).
var SUPPORTED_COMMANDS = []string{CommandReset, CommandDisconnect}

// isSupportedCommand checks if a command (with / prefix) is in the supported commands list.
func isSupportedCommand(cmd string) bool {
	return slices.Contains(SUPPORTED_COMMANDS, cmd)
}

// convertHashPrefixToSlash checks if the message starts with #<command>
// and converts it to /<command> if the command is supported.
// Returns the converted text and true if conversion happened,
// otherwise returns original text and false.
func convertHashPrefixToSlash(text string) (string, bool) {
	if !strings.HasPrefix(text, "#") {
		return text, false
	}

	rest := text[1:]
	if rest == "" {
		return text, false
	}

	potentialCmd, _, _ := strings.Cut(rest, " ")

	cmdWithSlash := "/" + potentialCmd
	if isSupportedCommand(cmdWithSlash) {

		return "/" + rest, true
	}

	return text, false
}

// processHashCommand executes a command that was converted from #command to /command
// Returns true if a command was processed, false otherwise
func (a *Adapter) processHashCommand(cmd string, userID, channelID, threadTS string) bool {

	if !isSupportedCommand(cmd) {
		return false
	}

	// Check owner policy permission
	if !a.config.CanRespond(userID) {
		a.Logger().Warn("Unauthorized hash command attempt",
			"user_id", userID,
			"command", cmd,
			"policy", a.config.GetOwnerPolicy())
		return false
	}

	a.Logger().Info("Executing converted command", "command", cmd, "user_id", userID, "channel_id", channelID)

	baseSession := a.FindSessionByUserAndChannel(userID, channelID)
	var sessionID string
	if baseSession != nil {
		sessionID = baseSession.SessionID
	}

	req := &command.Request{
		Command:     cmd,
		UserID:      userID,
		ChannelID:   channelID,
		ThreadTS:    threadTS,
		SessionID:   sessionID,
		ResponseURL: "",
	}

	// Create callback for progress events
	var progressTS string
	callback := func(eventType string, data any) error {
		return a.handleCommandProgress(channelID, threadTS, &progressTS, eventType, data)
	}

	panicx.SafeGo(a.Logger(), func() {

		_, err := a.cmdRegistry.Execute(context.Background(), req, callback)
		if err != nil {
			a.Logger().Error("Command execution failed", "command", cmd, "error", err)
			return
		}
	})

	return true
}

// handleCommandProgress handles progress events from command execution
func (a *Adapter) handleCommandProgress(channelID, threadTS string, progressTS *string, eventType string, data any) error {

	msg := &base.ChatMessage{
		Type:      base.MessageTypeCommandProgress,
		Content:   fmt.Sprintf("%v", data),
		Metadata:  map[string]any{"event_type": eventType},
		Timestamp: time.Now(),
	}

	if ewm, ok := data.(*event.EventWithMeta); ok {
		msg.Content = ewm.EventData
		if ewm.Meta != nil {
			msg.Metadata["progress"] = ewm.Meta.Progress
			msg.Metadata["total_steps"] = ewm.Meta.TotalSteps
			msg.Metadata["current_step"] = ewm.Meta.CurrentStep
		}
	}

	blocks := a.messageBuilder.Build(msg)
	if len(blocks) == 0 {
		a.Logger().Debug("No blocks generated for command progress", "event_type", eventType)
		return nil
	}

	if *progressTS != "" {
		if err := a.UpdateMessageSDK(context.Background(), channelID, *progressTS, blocks, "Command progress"); err != nil {
			a.Logger().Debug("Failed to update progress message", "error", err, "ts", *progressTS)
			return err
		}
		return nil
	}

	ts, err := a.sendBlocksSDK(context.Background(), channelID, blocks, threadTS, "Command progress")
	if err != nil {
		a.Logger().Debug("Failed to send progress message", "error", err)
		return err
	}
	*progressTS = ts
	return nil
}

// the processed text along with metadata additions for the message.
// Returns the processed text and a metadata map.
func preprocessMessageText(originalText string) (string, map[string]any) {
	metadata := make(map[string]any)
	processed, converted := convertHashPrefixToSlash(originalText)
	if converted {
		metadata["converted_from_hash"] = true
		metadata["original_text"] = originalText
	}
	return processed, metadata
}

// sanitizeUserInput removes potentially dangerous characters from user input
// while preserving the core message content. This provides defense-in-depth
// alongside the engine-level WAF.
func sanitizeUserInput(text string) string {
	// Remove null bytes and other control characters except newlines/tabs
	var result strings.Builder
	for _, r := range text {
		switch {
		case r == 0: // null byte
			continue
		case r < 32 && r != '\t' && r != '\n' && r != '\r': // control chars except tab, LF, CR
			continue
		default:
			result.WriteRune(r)
		}
	}
	return result.String()
}
