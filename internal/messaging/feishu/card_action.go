package feishu

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"

	"github.com/hrygo/hotplex/internal/messaging"
)

const (
	cardActionAllow   = "allow"
	cardActionDeny    = "deny"
	cardActionAnswer  = "answer"
	cardActionAccept  = "accept"
	cardActionDecline = "decline"
)

func (a *Adapter) handleCardActionTrigger(ctx context.Context, event *callback.CardActionTriggerEvent) (resp *callback.CardActionTriggerResponse, err error) {
	// Panic recovery: WS handler convention (see ws.go). Named returns are
	// required so the defer can set `err` and prevent a higher-level crash.
	defer func() {
		if r := recover(); r != nil {
			a.Log.Error("feishu: panic in card action handler", "panic", r, "stack", string(debug.Stack()))
			err = fmt.Errorf("feishu card action panic: %v", r)
		}
	}()

	if event == nil || event.Event == nil || event.Event.Action == nil || event.Event.Action.Value == nil {
		return nil, nil
	}

	val := event.Event.Action.Value
	// Card templates (card_template.go) emit the request ID under the
	// "request_id" key — reading "id" here would silently miss every click.
	requestID, _ := val["request_id"].(string)
	actionType, _ := val["action"].(string)
	summary, _ := val["summary"].(string)

	openID := ""
	if event.Event.Operator != nil {
		openID = event.Event.Operator.OpenID
	}

	var (
		metadata       map[string]any
		resolvedLabel  string
		resolvedColor  string
		resolvedReason string
	)

	var formVal map[string]any
	if event.Event.Action != nil {
		formVal = event.Event.Action.FormValue
	}

	switch actionType {
	case cardActionAllow:
		reason, _ := formVal["reason"].(string)
		metadata = messaging.BuildPermissionResponse(requestID, true, reason)
		resolvedLabel = "✅ 已允许"
		resolvedColor = "green"
		resolvedReason = reason

	case cardActionDeny:
		reason, _ := formVal["reason"].(string)
		if reason == "" {
			reason = "user denied"
		}
		metadata = messaging.BuildPermissionResponse(requestID, false, reason)
		resolvedLabel = "🚫 已拒绝"
		resolvedColor = "red"
		resolvedReason = reason

	case cardActionAnswer:
		answers := questionAnswers(formVal, val)
		metadata = messaging.BuildQuestionResponseAnswersWithOrder(requestID, answers, questionAnswerOrder(val, answers))
		resolvedLabel = "✅ 已回答"
		resolvedColor = "green"
		resolvedReason = strings.Join(questionAnswerValues(answers), "、")

	case cardActionAccept:
		comment, _ := formVal["comment"].(string)
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
		resolvedLabel = "✅ 已接受"
		resolvedColor = "green"
		resolvedReason = comment

	case cardActionDecline:
		comment, _ := formVal["comment"].(string)
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
		resolvedLabel = "🚫 已拒绝"
		resolvedColor = "red"
		resolvedReason = comment

	default:
		a.Log.Warn("feishu: unknown card action type", "action", actionType, "request_id", requestID)
		return wrapResolvedCard(buildResolvedCard("deny", "未知操作", headerGrey, summary, "", "")), nil
	}

	// Verify the owner before claiming the request so a non-owner cannot block
	// the legitimate responder from completing it.
	pending, exists := a.Interactions.Get(requestID)
	if !exists {
		resp = wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", "", summary, "", ""))
		return
	}
	if pending.OwnerID != "" && pending.OwnerID != openID {
		return wrapResolvedCard(buildResolvedCard("deny", "仅发起人可操作", headerGrey, "", "", "")), nil
	}

	pi, ok := a.Interactions.Claim(requestID)
	if !ok {
		resp = wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", "", summary, "", ""))
		return
	}

	// Card callbacks must respond within three seconds. Spend at most two
	// seconds delivering to the active worker, leaving room for callback
	// serialization and transport overhead.
	deliveryCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if pi.SendResponseSync != nil {
		err = pi.SendResponseSync(deliveryCtx, metadata)
	} else if pi.SendResponse != nil {
		// Compatibility for manually registered interactions in tests and third
		// party adapters. Native Feishu interactions always provide the
		// synchronous sender above.
		pi.SendResponse(metadata)
	} else {
		err = fmt.Errorf("feishu: interaction has no response sender")
	}
	if err != nil {
		a.Interactions.Release(requestID)
		a.Log.Warn("feishu: interaction response delivery failed",
			"request_id", requestID,
			"action", actionType,
			"operator", openID,
			"err", err)
		return wrapResolvedCard(buildRetryCard(val, summary, err.Error())), nil
	}

	if _, ok := a.Interactions.CompleteClaimed(requestID); !ok {
		return wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", "", summary, "", "")), nil
	}

	a.Log.Info("feishu: interaction resolved via card button",
		"request_id", requestID,
		"action", actionType,
		"operator", openID)

	if actionType == cardActionAllow || actionType == cardActionAnswer || actionType == cardActionAccept {
		resolvedLabel += "，Agent 继续执行"
	}
	return wrapResolvedCard(buildResolvedCard(actionType, resolvedLabel, resolvedColor, summary, openID, resolvedReason)), nil
}

func questionAnswers(formVal, value map[string]any) map[string]string {
	answers := make(map[string]string)
	questionKeys, _ := value["question_keys"].(map[string]any)
	for key, raw := range formVal {
		if key != "custom_answer" && !strings.HasPrefix(key, "answer_") {
			continue
		}
		if answer := formAnswerValue(raw); answer != "" {
			if key == "custom_answer" {
				key = "_"
			} else if question, ok := questionKeys[key].(string); ok && question != "" {
				key = question
			}
			answers[key] = answer
		}
	}
	if len(answers) > 0 {
		return answers
	}
	answer, _ := value["answer"].(string)
	if answer == "" {
		answer, _ = value["label"].(string)
	}
	if answer != "" {
		question, _ := value["question"].(string)
		if question == "" {
			question = "_"
		}
		answers[question] = answer
	}
	return answers
}

func formAnswerValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case []string:
		return strings.Join(v, ", ")
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

func questionAnswerValues(answers map[string]string) []string {
	values := make([]string, 0, len(answers))
	for _, answer := range answers {
		if answer != "" {
			values = append(values, answer)
		}
	}
	return values
}

func questionAnswerOrder(value map[string]any, answers map[string]string) []string {
	raw, _ := value["question_order"].([]any)
	if len(raw) == 0 {
		if order, ok := value["question_order"].([]string); ok {
			return order
		}
		if question, ok := value["question"].(string); ok && question != "" {
			return []string{question}
		}
		return nil
	}
	order := make([]string, 0, len(raw))
	for _, item := range raw {
		if question, ok := item.(string); ok && answers[question] != "" {
			order = append(order, question)
		}
	}
	return order
}

func wrapResolvedCard(card map[string]any) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Card: &callback.Card{
			Type: "raw",
			Data: card,
		},
	}
}
