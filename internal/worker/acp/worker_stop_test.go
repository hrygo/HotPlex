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
		require.Equal(t, acpCompatibilityRules+"\n\nhello again", w.conn.LastInput())
	})
}

// TestWorker_StopCurrentTurn_CancelMethodNotFoundDegradesToKill verifies that a
// JSON-RPC -32601 (Method not found) response to session/cancel — an ACP agent
// that does not implement the mandatory cancel method — degrades to a
// process-level stop instead of surfacing a perpetual INTERNAL_ERROR to the
// client: StopCurrentTurn returns nil and the stopped marker stays set so the
// bridge never misreads the process exit as a crash and re-delivers the input.
func TestWorker_StopCurrentTurn_CancelMethodNotFoundDegradesToKill(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	// Agent goroutine: answer session/cancel with -32601 Method not found.
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
				Error:   &JSONRPCError{Code: -32601, Message: "Method not found"},
			}
			_ = WriteMessage(agentStdoutW, resp)
		}
	}()

	w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}
	w.client = client
	w.SetWorkerSessionID("sess_123")

	err := w.StopCurrentTurn(ctx)
	require.NoError(t, err, "agent without session/cancel must degrade to process kill, not fail the stop")
	require.True(t, w.IsStopped(), "stopped marker must stay set after the degraded stop")
}

// TestWorker_StopCurrentTurn_CancelErrorStillFails verifies that non -32601
// cancel failures keep the original failed-stop semantics: the error is
// returned and the stopped marker is cleared so the gateway can roll back its
// stop fence and let the user retry.
func TestWorker_StopCurrentTurn_CancelErrorStillFails(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	// Agent goroutine: answer session/cancel with a non -32601 error.
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
				Error:   &JSONRPCError{Code: -32602, Message: "Invalid params"},
			}
			_ = WriteMessage(agentStdoutW, resp)
		}
	}()

	w := &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}
	w.client = client
	w.SetWorkerSessionID("sess_123")

	err := w.StopCurrentTurn(ctx)
	require.Error(t, err, "non -32601 cancel failures must keep the failed-stop semantics")
	require.Contains(t, err.Error(), "acp cancel")
	require.False(t, w.IsStopped(), "stopped marker must clear so the gateway can roll back its stop fence")
}
