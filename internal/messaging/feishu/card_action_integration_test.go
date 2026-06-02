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

// TestCardActionFlow_PermissionAllow_EndToEnd exercises the full Feishu
// interactive-card routing loop in a single integration test:
//
//	button click → handleCardActionTrigger → SendResponse → Complete → state verification
//
// It then re-clicks the same request_id to assert idempotent behavior: the
// already-completed interaction yields a resolved "已过期或已响应" card and
// must NOT invoke SendResponse a second time.
func TestCardActionFlow_PermissionAllow_EndToEnd(t *testing.T) {
	t.Parallel()

	// ── Arrange ──────────────────────────────────────────────────────────────
	// Self-contained setup (does not depend on newCardActionTestAdapter / helper
	// in card_action_test.go) so this test reads as a standalone integration
	// spec for the routing loop.
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &Adapter{
		BaseAdapter: messaging.BaseAdapter[*FeishuConn]{
			PlatformAdapter: messaging.PlatformAdapter{
				Log:          log,
				Interactions: messaging.NewInteractionManager(log),
			},
		},
	}

	const (
		requestID = "req-perm-e2e-1"
		ownerID   = "ou_owner"
		sessionID = "sess-e2e-1"
	)

	// Buffered channel: SendResponse writes exactly once on a successful click.
	// A second click must NOT send (idempotency), so the channel will stay empty.
	received := make(chan map[string]any, 1)
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:        requestID,
		OwnerID:   ownerID,
		SessionID: sessionID,
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(meta map[string]any) {
			received <- meta
		},
	})
	require.Equal(t, 1, a.Interactions.Len(),
		"precondition: one interaction must be registered before the click")

	allowEvent := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: ownerID},
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					"action":     "allow",
					"request_id": requestID,
				},
			},
		},
	}

	// ── Act ─────────────────────────────────────────────────────────────────
	got, err := a.handleCardActionTrigger(context.Background(), allowEvent)
	require.NoError(t, err)
	require.NotNil(t, got,
		"handleCardActionTrigger must return a card on successful allow click")
	require.NotNil(t, got.Card)

	// ── Assert: card response structure (header template = green) ───────────
	card, ok := got.Card.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", got.Card.Data)
	require.Contains(t, card, "header", "resolved card must have a header block")

	header, ok := card["header"].(map[string]any)
	require.True(t, ok, "header must be map[string]any, got %T", card["header"])
	assert.Equal(t, "green", header["template"],
		"allow action must produce a green resolved card")

	title, ok := header["title"].(map[string]any)
	require.True(t, ok, "header.title must be map[string]any, got %T", header["title"])
	assert.Equal(t, "✅ 已允许", title["content"],
		"allow action must surface the '已允许' resolved label")

	// ── Assert: SendResponse received with correct payload ──────────────────
	select {
	case meta := <-received:
		perm, ok := meta["permission_response"].(map[string]any)
		require.True(t, ok,
			"expected permission_response key in meta, got: %v", meta)
		assert.Equal(t, requestID, perm["request_id"],
			"permission_response.request_id must echo the registered ID")
		assert.Equal(t, true, perm["allowed"],
			"permission_response.allowed must be true for 'allow' action")
		assert.Equal(t, "", perm["reason"],
			"allow action must not carry a denial reason")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SendResponse was not called within 200ms — routing loop stalled")
	}

	// ── Assert: interaction completed atomically ────────────────────────────
	assert.Equal(t, 0, a.Interactions.Len(),
		"interaction must be removed from the manager after a successful click")

	// ── Act + Assert: second click on the same request_id is idempotent ─────
	// Interactions.Complete returns ok=false for an already-completed ID, so
	// the handler must short-circuit to a resolved "已过期或已响应" card and
	// must NOT call SendResponse a second time.
	secondEvent := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{OpenID: ownerID},
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					"action":     "allow",
					"request_id": requestID,
				},
			},
		},
	}

	got2, err2 := a.handleCardActionTrigger(context.Background(), secondEvent)
	require.NoError(t, err2)
	require.NotNil(t, got2,
		"second click on a completed interaction must still return a resolved card")
	require.NotNil(t, got2.Card)

	card2, ok := got2.Card.Data.(map[string]any)
	require.True(t, ok, "expected map[string]any, got %T", got2.Card.Data)

	header2, ok := card2["header"].(map[string]any)
	require.True(t, ok, "header must be map[string]any, got %T", card2["header"])

	title2, ok := header2["title"].(map[string]any)
	require.True(t, ok, "header.title must be map[string]any, got %T", header2["title"])
	assert.Equal(t, "已过期或已响应", title2["content"],
		"second click must yield the expired/resolved card title")

	// SendResponse must not be invoked a second time. The channel is buffered
	// with capacity 1, so a leak would be observable as a readable value.
	select {
	case extra := <-received:
		t.Fatalf("SendResponse must not fire twice for the same request_id; got: %v", extra)
	case <-time.After(50 * time.Millisecond):
		// Expected: channel remains empty after the first (and only) send.
	}
}
