package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func newCardActionTestAdapter() *Adapter {
	return &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
				Interactions: messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil))),
			},
		},
	}
}

func registerTestPermission(t *testing.T, a *Adapter, requestID, ownerID string) chan map[string]any {
	t.Helper()
	ch := make(chan map[string]any, 1)
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        requestID,
		OwnerID:   ownerID,
		SessionID: "sess-1",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(meta map[string]any) {
			ch <- meta
		},
	})
	return ch
}

func registerTestQuestion(t *testing.T, a *Adapter, requestID, ownerID string) chan map[string]any {
	t.Helper()
	ch := make(chan map[string]any, 1)
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        requestID,
		OwnerID:   ownerID,
		SessionID: "sess-1",
		Type:      events.QuestionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(meta map[string]any) {
			ch <- meta
		},
	})
	return ch
}

func registerTestElicitation(t *testing.T, a *Adapter, requestID, ownerID string) chan map[string]any {
	t.Helper()
	ch := make(chan map[string]any, 1)
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        requestID,
		OwnerID:   ownerID,
		SessionID: "sess-1",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(meta map[string]any) {
			ch <- meta
		},
	})
	return ch
}

func newCardActionTriggerEvent(openID, requestID, action string, extras map[string]interface{}) *callback.CardActionTriggerEvent {
	value := map[string]interface{}{
		"action":     action,
		"request_id": requestID,
	}
	for k, v := range extras {
		value[k] = v
	}
	return &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: openID},
			Action:   &callback.CallBackAction{Value: value},
		},
	}
}

func TestHandleCardAction_PermissionAllow(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	respCh := registerTestPermission(t, a, "req-perm-1", "u-owner")

	event := newCardActionTriggerEvent("u-owner", "req-perm-1", "allow", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", got.Card.Data)
	assert.Contains(t, card, "header")

	select {
	case meta := <-respCh:
		perm, ok := meta["permission_response"].(map[string]any)
		require.True(t, ok, "expected permission_response key in meta")
		assert.Equal(t, "req-perm-1", perm["request_id"])
		assert.Equal(t, true, perm["allowed"])
	case <-time.After(2 * time.Second):
		t.Fatal("SendResponse was not called within 2s")
	}
	assert.Equal(t, 0, a.Interactions.Len(), "interaction should be completed")
}

func TestHandleCardAction_PermissionDeny(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	respCh := registerTestPermission(t, a, "req-perm-2", "u-owner")

	event := newCardActionTriggerEvent("u-owner", "req-perm-2", "deny", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, card, "header")

	select {
	case meta := <-respCh:
		perm, ok := meta["permission_response"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "req-perm-2", perm["request_id"])
		assert.Equal(t, false, perm["allowed"])
		assert.Equal(t, "user denied", perm["reason"])
	case <-time.After(2 * time.Second):
		t.Fatal("SendResponse was not called within 2s")
	}
	assert.Equal(t, 0, a.Interactions.Len())
}

func TestHandleCardAction_QuestionAnswer(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	respCh := registerTestQuestion(t, a, "req-q-1", "u-owner")

	event := newCardActionTriggerEvent("u-owner", "req-q-1", "answer", map[string]interface{}{"answer": "Yes"})

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, card, "header")

	select {
	case meta := <-respCh:
		qr, ok := meta["question_response"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "req-q-1", qr["id"])
		answers, ok := qr["answers"].(map[string]string)
		require.True(t, ok)
		assert.Equal(t, "Yes", answers["_"])
	case <-time.After(2 * time.Second):
		t.Fatal("SendResponse was not called within 2s")
	}
	assert.Equal(t, 0, a.Interactions.Len())
}

func TestHandleCardAction_ElicitationAccept(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	respCh := registerTestElicitation(t, a, "req-e-1", "u-owner")

	event := newCardActionTriggerEvent("u-owner", "req-e-1", "accept", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, card, "header")

	select {
	case meta := <-respCh:
		er, ok := meta["elicitation_response"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "req-e-1", er["id"])
		assert.Equal(t, "accept", er["action"])
	case <-time.After(2 * time.Second):
		t.Fatal("SendResponse was not called within 2s")
	}
	assert.Equal(t, 0, a.Interactions.Len())
}

func TestHandleCardAction_ElicitationDecline(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	respCh := registerTestElicitation(t, a, "req-e-2", "u-owner")

	event := newCardActionTriggerEvent("u-owner", "req-e-2", "decline", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, card, "header")

	select {
	case meta := <-respCh:
		er, ok := meta["elicitation_response"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "req-e-2", er["id"])
		assert.Equal(t, "decline", er["action"])
	case <-time.After(2 * time.Second):
		t.Fatal("SendResponse was not called within 2s")
	}
	assert.Equal(t, 0, a.Interactions.Len())
}

func TestHandleCardAction_NonOwner(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	_ = registerTestPermission(t, a, "req-no-1", "u1")

	event := newCardActionTriggerEvent("u2", "req-no-1", "allow", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	assert.Nil(t, got, "non-owner should return nil response")
	assert.Equal(t, 1, a.Interactions.Len(), "non-owner click must leave the interaction pending for the legitimate owner")
}

func TestHandleCardAction_ExpiredOrMissing(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()
	_ = registerTestPermission(t, a, "req-exp-1", "u-owner")
	_, ok := a.Interactions.Complete("req-exp-1")
	require.True(t, ok, "precondition: interaction must be completed")

	event := newCardActionTriggerEvent("u-owner", "req-exp-1", "allow", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got, "expired/missing should still return a resolved card")
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", got.Card.Data)
	assert.Contains(t, card, "header")
	header, ok := card["header"].(map[string]any)
	require.True(t, ok)
	title, ok := header["title"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "已过期或已响应", title["content"])
}

func TestHandleCardAction_NilAction(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()

	event := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "u-owner"},
			Action:   nil,
		},
	}

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 0, a.Interactions.Len())
}

func TestHandleCardAction_EmptyValue(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()

	event := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: "u-owner"},
			Action:   &callback.CallBackAction{Value: nil},
		},
	}

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.Equal(t, 0, a.Interactions.Len())
}

func TestHandleCardAction_UnknownAction(t *testing.T) {
	t.Parallel()
	a := newCardActionTestAdapter()

	event := newCardActionTriggerEvent("u-owner", "req-unknown-1", "foo", nil)

	got, err := a.handleCardActionTrigger(context.Background(), event)
	require.NoError(t, err)
	require.NotNil(t, got, "unknown action should return a resolved card for user feedback")
	require.NotNil(t, got.Card)
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, card, "header")
	header := card["header"].(map[string]any)
	title := header["title"].(map[string]any)
	assert.Equal(t, "未知操作", title["content"])
	assert.Equal(t, headerGrey, header["template"])
}
