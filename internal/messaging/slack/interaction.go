package slack

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// interactionActionPrefix is used to identify interaction button actions.
const interactionActionPrefix = "hp_interact"

// handleInteractionEvent processes Slack interactive component callbacks.
func (a *Adapter) handleInteractionEvent(ctx context.Context, evt socketmode.Event) {
	callback, ok := evt.Data.(slack.InteractionCallback)
	if !ok {
		a.Log.Debug("slack: interaction event: unexpected data type", "type", fmt.Sprintf("%T", evt.Data))
		return
	}

	// Only handle block kit button actions
	if callback.Type != slack.InteractionTypeBlockActions {
		a.Log.Debug("slack: interaction event: ignoring non-block-actions type", "type", callback.Type)
		return
	}

	for _, action := range callback.ActionCallback.BlockActions {
		if !strings.HasPrefix(action.ActionID, interactionActionPrefix+"/") {
			continue
		}

		// Parse: "hp_interact/<type>/<requestID>"
		parts := strings.SplitN(action.ActionID, "/", 3)
		if len(parts) != 3 {
			continue
		}

		interactionType := parts[1]
		requestID := parts[2]
		channelID := callback.Channel.ID
		threadTS := callback.MessageTs
		userID := callback.User.ID

		a.Log.Info("slack: interaction callback",
			"type", interactionType,
			"request_id", requestID,
			"user_id", userID,
			"value", action.Value)

		// Acknowledge the interaction to Slack
		_ = a.socketMode.Ack(*evt.Request)

		// Validate ownership BEFORE removing from the pending map to prevent
		// a non-owner from consuming the interaction and blocking the real owner.
		pi, ok := a.Interactions.Get(requestID)
		if !ok {
			a.Log.Debug("slack: interaction not found or expired", "request_id", requestID)
			continue
		}
		if pi.OwnerID != "" && pi.OwnerID != userID {
			a.Log.Warn("slack: interaction user mismatch",
				"request_id", requestID, "expected_owner", pi.OwnerID, "actual_user", userID)
			continue
		}
		var (
			metadata map[string]any
			ackText  string
		)

		switch interactionType {
		case "allow":
			reason := ""
			if callback.BlockActionState != nil {
				if blocks, ok := callback.BlockActionState.Values["reason_block"]; ok {
					if act, ok := blocks["reason"]; ok {
						reason = act.Value
					}
				}
			}
			metadata = map[string]any{
				"permission_response": map[string]any{
					"request_id": requestID,
					"allowed":    true,
					"reason":     reason,
				},
			}
			ackText = fmt.Sprintf("_Allowed by <@%s>_", userID)
			if reason != "" {
				ackText += fmt.Sprintf("\n> *Reason:* %s", reason)
			}

		case "deny":
			reason := ""
			if callback.BlockActionState != nil {
				if blocks, ok := callback.BlockActionState.Values["reason_block"]; ok {
					if act, ok := blocks["reason"]; ok {
						reason = act.Value
					}
				}
			}
			if reason == "" {
				reason = "user denied"
			}
			metadata = map[string]any{
				"permission_response": map[string]any{
					"request_id": requestID,
					"allowed":    false,
					"reason":     reason,
				},
			}
			ackText = fmt.Sprintf("_Denied by <@%s>_", userID)
			if reason != "" && reason != "user denied" {
				ackText += fmt.Sprintf("\n> *Reason:* %s", reason)
			}

		case "answer":
			answers, order := slackQuestionAnswers(pi.Questions, callback.BlockActionState, action.Value)
			metadata = messaging.BuildQuestionResponseOptionsWithOrder(requestID, answers, order)
			ackText = fmt.Sprintf("_Answered by <@%s>_", userID)

		case "accept":
			comment := ""
			if callback.BlockActionState != nil {
				if blocks, ok := callback.BlockActionState.Values["elicitation_comment_block"]; ok {
					if act, ok := blocks["comment"]; ok {
						comment = act.Value
					}
				}
			}
			content := map[string]any{}
			if comment != "" {
				content["comment"] = comment
			}
			metadata = map[string]any{
				"elicitation_response": map[string]any{
					"id":      requestID,
					"action":  "accept",
					"content": content,
				},
			}
			ackText = fmt.Sprintf("_Accepted by <@%s>_", userID)
			if comment != "" {
				ackText += fmt.Sprintf("\n> *Comment:* %s", comment)
			}

		case "decline":
			comment := ""
			if callback.BlockActionState != nil {
				if blocks, ok := callback.BlockActionState.Values["elicitation_comment_block"]; ok {
					if act, ok := blocks["comment"]; ok {
						comment = act.Value
					}
				}
			}
			content := map[string]any{}
			if comment != "" {
				content["comment"] = comment
			}
			metadata = map[string]any{
				"elicitation_response": map[string]any{
					"id":      requestID,
					"action":  "decline",
					"content": content,
				},
			}
			ackText = fmt.Sprintf("_Declined by <@%s>_", userID)
			if comment != "" {
				ackText += fmt.Sprintf("\n> *Comment:* %s", comment)
			}
		}

		if metadata == nil {
			continue
		}
		if err := a.deliverInteractionResponse(ctx, requestID, metadata); err != nil {
			a.Log.Warn("slack: interaction response delivery failed",
				"request_id", requestID,
				"type", interactionType,
				"user_id", userID,
				"err", err)
			if len(callback.Message.Blocks.BlockSet) > 0 {
				blocks := append([]slack.Block(nil), callback.Message.Blocks.BlockSet...)
				blocks = append(blocks, slack.NewContextBlock("interaction_delivery_error",
					slack.NewTextBlockObject(slack.MarkdownType,
						":warning: Response was not submitted. Please retry.", false, false),
				))
				_, _, _, _ = a.client.UpdateMessageContext(ctx, channelID, threadTS,
					slack.MsgOptionBlocks(blocks...))
			}
			continue
		}

		var updateErr error
		if len(callback.Message.Blocks.BlockSet) > 0 {
			var updatedBlocks []slack.Block
			for _, b := range callback.Message.Blocks.BlockSet {
				if b.BlockType() != slack.MBTAction && b.BlockType() != slack.MBTInput {
					updatedBlocks = append(updatedBlocks, b)
				}
			}
			ackSection := slack.NewContextBlock("",
				slack.NewTextBlockObject(slack.MarkdownType, ackText, false, false),
			)
			updatedBlocks = append(updatedBlocks, ackSection)
			_, _, _, updateErr = a.client.UpdateMessageContext(ctx, channelID, threadTS,
				slack.MsgOptionBlocks(updatedBlocks...),
			)
		} else {
			_, _, _, updateErr = a.client.UpdateMessageContext(ctx, channelID, threadTS,
				slack.MsgOptionText(ackText, false),
			)
		}
		if updateErr != nil {
			a.Log.Debug("slack: update interaction message", "err", updateErr)
		}

		_ = threadTS // thread context
	}
}

// buildPermissionFallbackText creates plain-text fallback for permission request.
// AC-3.6: Permission request has a fallbackText that conveys same info without blocks
func buildPermissionFallbackText(data *events.PermissionRequestData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*Tool Approval Required*\nClaude Code requests permission to run: `%s`\n", data.ToolName)

	if data.Description != "" && data.Description != data.ToolName {
		fmt.Fprintf(&sb, "Description: %s\n", data.Description)
	}

	if len(data.Args) > 0 && data.Args[0] != `{}` {
		preview := data.Args[0]
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		fmt.Fprintf(&sb, "Args: %s\n", preview)
	}

	fmt.Fprintf(&sb, "\nReply with 'allow %s' or 'deny %s'", data.ID, data.ID)
	return sb.String()
}

// sendPermissionRequest posts a permission request UI to Slack.
func (c *SlackConn) sendPermissionRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractPermissionData(env)
	if err != nil {
		return fmt.Errorf("slack: extract permission data: %w", err)
	}

	// Build the header text
	headerText := fmt.Sprintf("*Tool Approval Required*\nClaude Code requests permission to run:\n`%s`", data.ToolName)
	if data.Description != "" && data.Description != data.ToolName {
		headerText += fmt.Sprintf("\n> %s", data.Description)
	}

	// Show args preview if available
	if len(data.Args) > 0 && data.Args[0] != `{}` {
		preview := data.Args[0]
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		// Strip triple backticks to prevent nested code blocks in Block Kit.
		preview = strings.ReplaceAll(preview, "```", "")
		headerText += fmt.Sprintf("\n```%s```", preview)
	}

	reasonInput := slack.NewPlainTextInputBlockElement(
		slack.NewTextBlockObject(slack.PlainTextType, "请填写可选的留言/反馈/拒绝理由...", false, false),
		"reason",
	)
	reasonBlock := slack.NewInputBlock(
		"reason_block",
		slack.NewTextBlockObject(slack.PlainTextType, "留言/反馈理由 (可选)", false, false),
		nil,
		reasonInput,
	)
	reasonBlock.Optional = true

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, headerText, false, false),
			nil, nil,
		),
		reasonBlock,
		slack.NewActionBlock(
			"permission_actions",
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/allow/"+data.ID,
				"allow",
				slack.NewTextBlockObject(slack.PlainTextType, "Allow", false, true),
			).WithStyle(slack.StylePrimary),
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/deny/"+data.ID,
				"deny",
				slack.NewTextBlockObject(slack.PlainTextType, "Deny", false, true),
			).WithStyle(slack.StyleDanger),
		),
	}

	// Sanitize blocks before sending
	blocks = SanitizeBlocks(blocks)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}

	_, msgTS, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		// AC-3.5: On invalid_blocks API error, message is resent as plain text
		if isInvalidBlocksError(err) {
			fallbackText := buildPermissionFallbackText(data)
			fallbackOpts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if c.threadTS != "" {
				fallbackOpts = append(fallbackOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, msgTS, err = c.adapter.client.PostMessageContext(ctx, c.channelID, fallbackOpts...)
			if err != nil {
				return fmt.Errorf("slack: post permission request (fallback): %w", err)
			}
			c.adapter.Log.Warn("slack: sent permission request as plain text fallback", "request_id", data.ID)
		} else {
			return fmt.Errorf("slack: post permission request: %w", err)
		}
	}

	// Register the pending interaction with timeout
	c.adapter.registerInteraction(data.ID, env.SessionID, env.OwnerID, events.PermissionRequest, msgTS, c)

	c.adapter.Log.Info("slack: permission request posted",
		"request_id", data.ID,
		"tool_name", data.ToolName,
		"channel", c.channelID,
		"thread", c.threadTS)

	return nil
}

// buildQuestionFallbackText creates plain-text fallback for question request.
// AC-3.7: Question request has a fallbackText with numbered options
func buildQuestionFallbackText(data *events.QuestionRequestData) string {
	var sb strings.Builder
	sb.WriteString("*Question Request*\n")

	for i, q := range data.Questions {
		headerLabel := q.Header
		if headerLabel == "" {
			headerLabel = "Question"
		}
		fmt.Fprintf(&sb, "\n*%s %d:* %s\n", headerLabel, i+1, q.Question)

		if len(q.Options) > 0 {
			sb.WriteString("Options:\n")
			for j, opt := range q.Options {
				label := opt.Label
				if opt.Description != "" {
					label += " — " + opt.Description
				}
				fmt.Fprintf(&sb, "  %d. %s\n", j+1, label)
			}
		}
	}

	fmt.Fprintf(&sb, "\nReply with the option number for request %s", data.ID)
	return sb.String()
}

// sendQuestionRequest posts a question request UI to Slack.
func (c *SlackConn) sendQuestionRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractQuestionData(env)
	if err != nil {
		return fmt.Errorf("slack: extract question data: %w", err)
	}

	var blocks []slack.Block
	useForm := len(data.Questions) > 1
	for _, question := range data.Questions {
		if question.MultiSelect {
			useForm = true
			break
		}
	}

	for index, q := range data.Questions {
		headerLabel := q.Header
		if headerLabel == "" {
			headerLabel = "Question"
		}

		blocks = append(blocks, slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, fmt.Sprintf("*%s*\n%s", headerLabel, q.Question), false, false),
			nil, nil,
		))

		if useForm {
			blockID := fmt.Sprintf("question_answer_%d", index)
			actionID := fmt.Sprintf("answer_%d", index)
			var element slack.BlockElement
			if len(q.Options) == 0 {
				element = slack.NewPlainTextInputBlockElement(
					slack.NewTextBlockObject(slack.PlainTextType, "Enter your answer", false, false),
					actionID,
				)
			} else {
				options := make([]*slack.OptionBlockObject, 0, len(q.Options))
				for _, option := range q.Options {
					options = append(options, slack.NewOptionBlockObject(
						option.Label,
						slack.NewTextBlockObject(slack.PlainTextType, truncateSlackLabel(option.Label), false, false),
						nil,
					))
				}
				placeholder := slack.NewTextBlockObject(slack.PlainTextType, "Select an answer", false, false)
				if q.MultiSelect {
					element = slack.NewOptionsMultiSelectBlockElement(slack.MultiOptTypeStatic, placeholder, actionID, options...)
				} else {
					element = slack.NewOptionsSelectBlockElement(slack.OptTypeStatic, placeholder, actionID, options...)
				}
			}
			input := slack.NewInputBlock(
				blockID,
				slack.NewTextBlockObject(slack.PlainTextType, headerLabel, false, false),
				nil,
				element,
			)
			input.Optional = true
			blocks = append(blocks, input)
			continue
		}

		// Single-choice single-question requests keep the compact button UI.
		var buttons []slack.BlockElement
		for _, opt := range q.Options {
			label := opt.Label
			if opt.Description != "" {
				label += " — " + opt.Description
			}
			if len(label) > 75 {
				label = label[:72] + "..."
			}
			buttons = append(buttons, slack.NewButtonBlockElement(
				interactionActionPrefix+"/answer/"+data.ID,
				opt.Label,
				slack.NewTextBlockObject(slack.PlainTextType, label, false, true),
			))
		}

		if len(buttons) > 0 {
			blocks = append(blocks, slack.NewActionBlock(
				fmt.Sprintf("question_%s", data.ID),
				buttons...,
			))
		}

		customAnswerInput := slack.NewPlainTextInputBlockElement(
			slack.NewTextBlockObject(slack.PlainTextType, "或者在此输入自定义答案...", false, false),
			"custom_answer",
		)
		customAnswerBlock := slack.NewInputBlock(
			"question_custom_block",
			slack.NewTextBlockObject(slack.PlainTextType, "自定义答案 (可选)", false, false),
			nil,
			customAnswerInput,
		)
		customAnswerBlock.Optional = true
		blocks = append(blocks, customAnswerBlock)
	}
	if useForm {
		blocks = append(blocks, slack.NewActionBlock(
			"question_submit",
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/answer/"+data.ID,
				"submit",
				slack.NewTextBlockObject(slack.PlainTextType, "Submit answers", false, true),
			).WithStyle(slack.StylePrimary),
		))
	}

	// Sanitize blocks before sending
	blocks = SanitizeBlocks(blocks)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}

	_, msgTS, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		// AC-3.5: On invalid_blocks API error, message is resent as plain text
		if isInvalidBlocksError(err) {
			fallbackText := buildQuestionFallbackText(data)
			fallbackOpts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if c.threadTS != "" {
				fallbackOpts = append(fallbackOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, msgTS, err = c.adapter.client.PostMessageContext(ctx, c.channelID, fallbackOpts...)
			if err != nil {
				return fmt.Errorf("slack: post question request (fallback): %w", err)
			}
			c.adapter.Log.Warn("slack: sent question request as plain text fallback", "request_id", data.ID)
		} else {
			return fmt.Errorf("slack: post question request: %w", err)
		}
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, env.OwnerID, events.QuestionRequest, msgTS, c, data.Questions...)

	c.adapter.Log.Info("slack: question request posted",
		"request_id", data.ID,
		"questions", len(data.Questions))

	return nil
}

// buildElicitationFallbackText creates plain-text fallback for elicitation request.
// AC-3.8: Elicitation request has a fallbackText
func buildElicitationFallbackText(data *events.ElicitationRequestData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "*MCP Server Request*\n`%s` requests your input:\n%s\n",
		data.MCPServerName, data.Message)

	if data.URL != "" {
		fmt.Fprintf(&sb, "\nOpen external form: %s\n", data.URL)
	}

	fmt.Fprintf(&sb, "\nReply with 'accept %s' or 'decline %s'", data.ID, data.ID)
	return sb.String()
}

// sendElicitationRequest posts an MCP elicitation request UI to Slack.
func (c *SlackConn) sendElicitationRequest(ctx context.Context, env *events.Envelope) error {
	data, err := messaging.ExtractElicitationData(env)
	if err != nil {
		return fmt.Errorf("slack: extract elicitation data: %w", err)
	}

	headerText := fmt.Sprintf("*MCP Server Request*\n`%s` requests your input:\n%s",
		data.MCPServerName, data.Message)

	if data.URL != "" {
		headerText += fmt.Sprintf("\n<%s|Open external form>", data.URL)
	}

	commentInput := slack.NewPlainTextInputBlockElement(
		slack.NewTextBlockObject(slack.PlainTextType, "请填写附加注释/自定义输入...", false, false),
		"comment",
	)
	commentBlock := slack.NewInputBlock(
		"elicitation_comment_block",
		slack.NewTextBlockObject(slack.PlainTextType, "附加注释/输入 (可选)", false, false),
		nil,
		commentInput,
	)
	commentBlock.Optional = true

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject(slack.MarkdownType, headerText, false, false),
			nil, nil,
		),
		commentBlock,
		slack.NewActionBlock(
			"elicitation_actions",
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/accept/"+data.ID,
				"accept",
				slack.NewTextBlockObject(slack.PlainTextType, "Accept", false, true),
			).WithStyle(slack.StylePrimary),
			slack.NewButtonBlockElement(
				interactionActionPrefix+"/decline/"+data.ID,
				"decline",
				slack.NewTextBlockObject(slack.PlainTextType, "Decline", false, true),
			).WithStyle(slack.StyleDanger),
		),
	}

	// Sanitize blocks before sending
	blocks = SanitizeBlocks(blocks)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(blocks...)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}

	_, msgTS, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		// AC-3.5: On invalid_blocks API error, message is resent as plain text
		if isInvalidBlocksError(err) {
			fallbackText := buildElicitationFallbackText(data)
			fallbackOpts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if c.threadTS != "" {
				fallbackOpts = append(fallbackOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, msgTS, err = c.adapter.client.PostMessageContext(ctx, c.channelID, fallbackOpts...)
			if err != nil {
				return fmt.Errorf("slack: post elicitation request (fallback): %w", err)
			}
			c.adapter.Log.Warn("slack: sent elicitation request as plain text fallback", "request_id", data.ID)
		} else {
			return fmt.Errorf("slack: post elicitation request: %w", err)
		}
	}

	c.adapter.registerInteraction(data.ID, env.SessionID, env.OwnerID, events.ElicitationRequest, msgTS, c)

	return nil
}

// registerInteraction registers a pending interaction with the adapter's manager.
func (a *Adapter) registerInteraction(requestID, sessionID, ownerID string, kind events.Kind, _ string, conn *SlackConn, questions ...events.Question) {
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:               requestID,
		SessionID:        sessionID,
		OwnerID:          ownerID,
		Type:             kind,
		CreatedAt:        getTimeNow(),
		Timeout:          messaging.DefaultInteractionTimeout,
		Questions:        append([]events.Question(nil), questions...),
		SendResponse:     messaging.NewSendResponseFunc(a.Log, a.Bridge(), requestID, sessionID, ownerID, conn),
		SendResponseSync: messaging.NewSendResponseSyncFunc(a.Bridge(), requestID, sessionID, ownerID, conn),
	})
}

func truncateSlackLabel(label string) string {
	label = messaging.SanitizeText(label)
	runes := []rune(label)
	if len(runes) > 75 {
		return string(runes[:72]) + "..."
	}
	return label
}

func slackQuestionKey(question events.Question) string {
	if question.ID != "" {
		return question.ID
	}
	if question.Question != "" {
		return question.Question
	}
	return "_"
}

func slackQuestionAnswers(questions []events.Question, state *slack.BlockActionStates, fallback string) (map[string][]string, []string) {
	answers := make(map[string][]string)
	order := make([]string, 0, len(questions))
	for index, question := range questions {
		key := slackQuestionKey(question)
		order = append(order, key)
		if state == nil {
			continue
		}
		blockID := fmt.Sprintf("question_answer_%d", index)
		actionID := fmt.Sprintf("answer_%d", index)
		block, ok := state.Values[blockID]
		if !ok {
			continue
		}
		action, ok := block[actionID]
		if !ok {
			continue
		}
		values := make([]string, 0, len(action.SelectedOptions))
		for _, option := range action.SelectedOptions {
			if option.Value != "" {
				values = append(values, option.Value)
			}
		}
		switch {
		case len(values) > 0:
			answers[key] = values
		case action.SelectedOption.Value != "":
			answers[key] = []string{action.SelectedOption.Value}
		case strings.TrimSpace(action.Value) != "":
			answers[key] = []string{strings.TrimSpace(action.Value)}
		}
	}
	if len(questions) == 1 {
		key := slackQuestionKey(questions[0])
		if state != nil {
			if block, ok := state.Values["question_custom_block"]; ok {
				if action, ok := block["custom_answer"]; ok && strings.TrimSpace(action.Value) != "" {
					answers[key] = []string{strings.TrimSpace(action.Value)}
				}
			}
		}
		if len(answers[key]) == 0 && fallback != "" && fallback != "submit" {
			answers[key] = []string{fallback}
		}
	}
	if len(questions) == 0 && fallback != "" && fallback != "submit" {
		answers["_"] = []string{fallback}
		order = []string{"_"}
	}
	return answers, order
}

func (a *Adapter) deliverInteractionResponse(ctx context.Context, requestID string, metadata map[string]any) error {
	interaction, ok := a.Interactions.Claim(requestID)
	if !ok {
		return fmt.Errorf("slack: interaction %s is expired, completed, or already submitting", requestID)
	}

	deliveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	var err error
	switch {
	case interaction.SendResponseSync != nil:
		err = interaction.SendResponseSync(deliveryCtx, metadata)
	case interaction.SendResponse != nil:
		interaction.SendResponse(metadata)
	default:
		err = fmt.Errorf("slack: interaction has no response sender")
	}
	if err != nil {
		a.Interactions.Release(requestID)
		return err
	}
	if _, ok := a.Interactions.CompleteClaimed(requestID); !ok {
		return fmt.Errorf("slack: interaction %s changed state after delivery", requestID)
	}
	return nil
}

// checkPendingInteraction checks if a text message is a response to a pending
// interaction (permission, question, or elicitation). Returns true if consumed.
//
// Supported patterns (matching the fallback text instructions):
//   - "allow <requestID>" / "deny <requestID>"  — permission response
//   - raw text                                    — question response (any text)
//   - "accept <requestID>" / "decline <requestID>" — elicitation response
func (a *Adapter) checkPendingInteraction(ctx context.Context, text, channelID, threadTS, userID string) bool {
	if a.Interactions.Len() == 0 {
		return false
	}

	normalized := strings.ToLower(strings.TrimSpace(text))
	words := strings.Fields(normalized)

	// Try "<action> <requestID>" pattern first.
	var action, requestID string
	if len(words) >= 2 {
		action = words[0]
		requestID = words[1]
	}

	var matched *messaging.PendingInteraction

	if requestID != "" {
		if pi, ok := a.Interactions.Get(requestID); ok {
			matched = pi
		} else {
			a.Log.Debug("slack: interaction text lookup failed, request_id not in pending map",
				"request_id", requestID,
				"action", action,
				"pending_count", a.Interactions.Len())
		}
	}

	// Fallback: most recent pending interaction.
	if matched == nil {
		candidates := a.Interactions.GetAll()
		if len(candidates) == 0 {
			return false
		}
		candidate := candidates[0]
		// Action keyword + no requestID match: try to match action to interaction type.
		if action != "" {
			if (action == "allow" || action == "deny") && candidate.Type == events.PermissionRequest {
				matched = candidate
			} else if (action == "accept" || action == "decline") && candidate.Type == events.ElicitationRequest {
				matched = candidate
			} else {
				return false
			}
		} else {
			// Raw text (no action keyword) matches question requests only.
			if candidate.Type != events.QuestionRequest {
				return false
			}
			matched = candidate
		}
	}

	if matched.OwnerID != "" && matched.OwnerID != userID {
		return false
	}

	var metadata map[string]any

	switch matched.Type {
	case events.PermissionRequest:
		if action != "allow" && action != "deny" {
			return false
		}
		reason := ""
		allowed := action == "allow"
		if !allowed {
			reason = "user denied"
		}
		metadata = messaging.BuildPermissionResponse(matched.ID, allowed, reason)

	case events.QuestionRequest:
		metadata = messaging.BuildQuestionResponse(matched.ID, text)

	case events.ElicitationRequest:
		if action != "accept" && action != "decline" {
			return false
		}
		metadata = messaging.BuildElicitationResponse(matched.ID, action)

	default:
		return false
	}

	if err := a.deliverInteractionResponse(ctx, matched.ID, metadata); err != nil {
		a.Log.Warn("slack: text interaction response delivery failed",
			"request_id", matched.ID,
			"type", matched.Type,
			"user", userID,
			"err", err)
		opts := []slack.MsgOption{slack.MsgOptionText(":warning: Response was not submitted. Please retry.", false)}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, _, _ = a.client.PostMessageContext(ctx, channelID, opts...)
		return true
	}

	a.Log.Info("slack: interaction response received via text",
		"request_id", matched.ID,
		"type", matched.Type,
		"user", userID)

	ackText := "✅ Response received"
	if matched.Type == events.PermissionRequest {
		if d, ok := metadata["permission_response"].(map[string]any); ok {
			if allowed, _ := d["allowed"].(bool); allowed {
				ackText = "✅ Allowed"
			} else {
				ackText = "🚫 Denied"
			}
		}
	}

	opts := []slack.MsgOption{slack.MsgOptionText(ackText, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, _, _ = a.client.PostMessageContext(ctx, channelID, opts...)

	return true
}

// getTimeNow returns the current time. Extracted for testability.
var getTimeNow = time.Now
