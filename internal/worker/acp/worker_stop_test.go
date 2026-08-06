package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker/base"
)

// TestWorker_Input_ClearsStoppedOnlyForPrimaryTurn verifies the user-stop
// marker is scoped to the current turn: interaction-response metadata
// (handled by DispatchMetadata) must NOT clear it, while the next primary
// content (a real session/prompt RPC) must clear it before the send.
func TestWorker_Input_ClearsStoppedOnlyForPrimaryTurn(t *testing.T) {
	t.Parallel()

	t.Run("interaction response metadata keeps stopped", func(t *testing.T) {
		t.Parallel()

		w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
		w.client = NewACPClient(io.Discard, strings.NewReader(""), nil)
		w.pendingRequests.Store("q_stop_1", &JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      mustMarshal(1),
			Method:  "session/request_question",
		})
		w.MarkStopped()

		md := map[string]any{
			"question_response": map[string]any{
				"id":      "q_stop_1",
				"answers": map[string]string{"q1": "yes"},
			},
		}
		err := w.Input(context.Background(), "", md)
		require.NoError(t, err)
		require.True(t, w.IsStopped(), "handled metadata must not clear the stopped marker")
	})

	t.Run("next primary content clears stopped before prompt", func(t *testing.T) {
		t.Parallel()

		agentStdinR, agentStdinW := io.Pipe()
		agentStdoutR, agentStdoutW := io.Pipe()
		defer agentStdinW.Close()
		defer agentStdoutW.Close()

		client := NewACPClient(agentStdinW, agentStdoutR, nil)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		client.StartReadLoop(ctx)

		// Fake agent: answer session/prompt with end_turn so Input completes.
		go func() {
			scanner := bufio.NewScanner(agentStdinR)
			if scanner.Scan() {
				var req struct {
					ID json.RawMessage `json:"id"`
				}
				_ = json.Unmarshal(scanner.Bytes(), &req)
				resp := &JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result:  mustMarshal(PromptResult{StopReason: "end_turn"}),
				}
				_ = WriteMessage(agentStdoutW, resp)
			}
		}()

		w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
		w.client = client
		w.mapper = newTestMapper()
		w.conn = newACPConn("user_1", "sess_123", slog.Default())
		w.SetWorkerSessionID("sess_123")
		w.drainCh = make(chan struct{}, 1)
		w.drainDoneCh = make(chan struct{})
		close(w.drainDoneCh) // no readLoop in this test: the drain is a no-op
		w.MarkStopped()

		err := w.Input(ctx, "hello again", nil)
		require.NoError(t, err)

		require.False(t, w.IsStopped(), "a new primary turn must clear the stopped marker")
		// The protocol fake observed the primary send (session/prompt RPC):
		// the conn cached the prompt content for crash recovery.
		require.Equal(t, "hello again", w.conn.LastInput())
	})
}
