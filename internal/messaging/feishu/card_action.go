package feishu

import (
	"context"
	"fmt"
	"runtime/debug"

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

func (a *Adapter) handleCardActionTrigger(_ context.Context, event *callback.CardActionTriggerEvent) (resp *callback.CardActionTriggerResponse, err error) {
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

	openID := ""
	if event.Event.Operator != nil {
		openID = event.Event.Operator.OpenID
	}

	var (
		metadata      map[string]any
		resolvedLabel string
		resolvedColor string
	)

	switch actionType {
	case cardActionAllow:
		metadata = messaging.BuildPermissionResponse(requestID, true, "")
		resolvedLabel = "✅ 已允许"
		resolvedColor = "green"

	case cardActionDeny:
		metadata = messaging.BuildPermissionResponse(requestID, false, "user denied")
		resolvedLabel = "🚫 已拒绝"
		resolvedColor = "red"

	case cardActionAnswer:
		answer, _ := val["answer"].(string)
		if answer == "" {
			answer, _ = val["label"].(string)
		}
		metadata = messaging.BuildQuestionResponse(requestID, answer)
		resolvedLabel = "✅ 已回答"
		resolvedColor = "green"

	case cardActionAccept:
		metadata = messaging.BuildElicitationResponse(requestID, "accept")
		resolvedLabel = "✅ 已接受"
		resolvedColor = "green"

	case cardActionDecline:
		metadata = messaging.BuildElicitationResponse(requestID, "decline")
		resolvedLabel = "🚫 已拒绝"
		resolvedColor = "red"

	default:
		a.Log.Warn("feishu: unknown card action type", "action", actionType, "request_id", requestID)
		return wrapResolvedCard(buildResolvedCard("deny", "未知操作", headerGrey)), nil
	}

	// Owner check BEFORE Complete — preserves the interaction for non-owner
	// clicks. If we Completed first, the original watchTimeout goroutine
	// (still running) would race the re-Registered entry, eventually firing
	// on the new one and dispatching an auto-deny through the stale
	// SendResponse closure. The legitimate owner would then be denied by
	// their own timeout.
	pending, exists := a.Interactions.Get(requestID)
	if !exists {
		resp = wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", ""))
		return
	}
	if pending.OwnerID != "" && pending.OwnerID != openID {
		// Silent ignore: non-owner click leaves the interaction pending so
		// the rightful owner can still respond.
		return nil, nil
	}

	pi, ok := a.Interactions.Complete(requestID)
	if !ok {
		resp = wrapResolvedCard(buildResolvedCard("deny", "已过期或已响应", ""))
		return
	}

	if pi.SendResponse != nil {
		pi.SendResponse(metadata)
	}

	a.Log.Info("feishu: interaction resolved via card button",
		"request_id", requestID,
		"action", actionType,
		"operator", openID)

	return wrapResolvedCard(buildResolvedCard(actionType, resolvedLabel, resolvedColor)), nil
}

func wrapResolvedCard(card map[string]any) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{
		Card: &callback.Card{
			Type: "card_json",
			Data: card,
		},
	}
}
