package feishu

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
)

type gatewayCommandRecorder struct {
	calls atomic.Int32
	seen  chan messaging.GatewayRestartRequest
}

func (r *gatewayCommandRecorder) HandleGatewayCommand(_ context.Context, _ messaging.GatewayCommand, request messaging.GatewayRestartRequest) error {
	r.calls.Add(1)
	r.seen <- request
	return nil
}

func TestFeishuGatewayCommand_ReservedPathNeverReachesWorker(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	adapter.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(adapter.chatQueue.Close)
	recorder := &gatewayCommandRecorder{seen: make(chan messaging.GatewayRestartRequest, 1)}
	require.NoError(t, adapter.ConfigureWith(messaging.AdapterConfig{
		BotName: "ops",
		Extras:  map[string]any{"gateway_command_handler": recorder},
	}))

	message := larkim.NewEventMessageBuilder().
		MessageId("gateway-reserved").
		MessageType("text").
		Content(`{"text":"/gateway restart"}`).
		ChatId("oc_chat").
		ChatType("p2p").
		ParentId("om_parent").
		Build()
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("ou_operator").Build()).
		SenderType("user").
		Build()

	require.NoError(t, adapter.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: message},
	}))
	select {
	case request := <-recorder.seen:
		require.Equal(t, "ou_operator", request.ActorID)
		require.Equal(t, "ops", request.BotName)
		require.Equal(t, "om_parent", request.ReplyToMessage)
		require.Equal(t, "oc_chat", request.PlatformKey["chat_id"])
	case <-time.After(time.Second):
		t.Fatal("reserved Gateway command did not reach the coordinator")
	}
	require.Equal(t, int32(1), recorder.calls.Load())
}

func TestFeishuGatewayCommand_OrdinaryGateRunsFirst(t *testing.T) {
	t.Parallel()

	adapter := newTestAdapter(t)
	adapter.chatQueue = NewChatQueue(discardLogger)
	t.Cleanup(adapter.chatQueue.Close)
	recorder := &gatewayCommandRecorder{seen: make(chan messaging.GatewayRestartRequest, 1)}
	require.NoError(t, adapter.ConfigureWith(messaging.AdapterConfig{
		Gate:   messaging.NewGate("allowlist", "open", false, []string{"ou_allowed"}, nil, nil),
		Extras: map[string]any{"gateway_command_handler": recorder},
	}))

	message := larkim.NewEventMessageBuilder().
		MessageId("gateway-gated").
		MessageType("text").
		Content(`{"text":"/gateway restart"}`).
		ChatId("oc_chat").
		ChatType("p2p").
		Build()
	sender := larkim.NewEventSenderBuilder().
		SenderId(larkim.NewUserIdBuilder().OpenId("ou_denied").Build()).
		SenderType("user").
		Build()

	require.NoError(t, adapter.handleMessage(context.Background(), &larkim.P2MessageReceiveV1{
		Event: &larkim.P2MessageReceiveV1Data{Sender: sender, Message: message},
	}))
	require.Zero(t, recorder.calls.Load())
}
