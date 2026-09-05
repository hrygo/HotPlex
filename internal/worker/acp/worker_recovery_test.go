package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

func newSessionRPCClient(t *testing.T, results map[string]any) (*ACPClient, <-chan string) {
	t.Helper()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(t.Context())
	client.StartReadLoop(ctx)

	calls := make(chan string, 4)
	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		for scanner.Scan() {
			var req JSONRPCRequest
			if json.Unmarshal(scanner.Bytes(), &req) != nil {
				return
			}
			calls <- req.Method
			result, ok := results[req.Method]
			if !ok {
				return
			}
			response := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
			}
			if rpcErr, ok := result.(*JSONRPCError); ok {
				response.Error = rpcErr
			} else {
				response.Result = mustMarshal(result)
			}
			if WriteMessage(agentStdoutW, response) != nil {
				return
			}
		}
	}()

	t.Cleanup(func() {
		cancel()
		_ = agentStdinR.Close()
		_ = agentStdinW.Close()
		_ = agentStdoutR.Close()
		_ = agentStdoutW.Close()
	})
	return client, calls
}

func TestEstablishSession_NoLoadSessionSeedsAndInjectsHistoryOnce(t *testing.T) {
	t.Parallel()

	client, calls := newSessionRPCClient(t, map[string]any{
		"session/new": SessionResult{SessionID: "fresh-acp-session"},
	})
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.Mu.Lock()
	w.initResult = &InitializeResult{
		AgentCapabilities: map[string]any{"loadSession": false},
	}
	w.Mu.Unlock()

	history := []worker.ConversationTurn{
		{Role: "user", Content: "此前的问题"},
		{Role: "assistant", Content: "此前的回答"},
	}
	lost, err := w.establishSession(t.Context(), client, worker.SessionInfo{
		SessionID:           "session-1",
		ProjectDir:          "/tmp",
		WorkerSessionID:     "old-acp-session",
		ConversationHistory: history,
	}, nil)
	require.NoError(t, err)
	require.True(t, lost)
	require.Equal(t, "fresh-acp-session", w.GetWorkerSessionID())

	w.Mu.Lock()
	gotHistory := append([]worker.ConversationTurn(nil), w.pendingHistory...)
	w.Mu.Unlock()
	require.Equal(t, history, gotHistory)
	require.Equal(t, "session/new", <-calls)

	first := w.injectHistoryPrefix("当前问题")
	require.Contains(t, first, "CONVERSATION_HISTORY_RECOVERY_START")
	require.Contains(t, first, "此前的问题")
	require.Contains(t, first, "当前问题")
	require.Equal(t, "后续问题", w.injectHistoryPrefix("后续问题"))
}

func TestEstablishSession_NativeLoadDoesNotSeedHistory(t *testing.T) {
	t.Parallel()

	client, calls := newSessionRPCClient(t, map[string]any{
		"session/load": SessionResult{},
	})
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.Mu.Lock()
	w.initResult = &InitializeResult{
		AgentCapabilities: map[string]any{"loadSession": true},
	}
	w.Mu.Unlock()

	lost, err := w.establishSession(t.Context(), client, worker.SessionInfo{
		SessionID:           "session-1",
		ProjectDir:          "/tmp",
		WorkerSessionID:     "old-acp-session",
		ConversationHistory: []worker.ConversationTurn{{Role: "user", Content: "不会注入"}},
	}, nil)
	require.NoError(t, err)
	require.False(t, lost)
	require.Equal(t, "old-acp-session", w.GetWorkerSessionID())

	w.Mu.Lock()
	require.Empty(t, w.pendingHistory)
	w.Mu.Unlock()
	require.Equal(t, "当前问题", w.injectHistoryPrefix("当前问题"))
	require.Equal(t, "session/load", <-calls)
}

func TestEstablishSession_FreshWithStoredHistorySeedsHistory(t *testing.T) {
	t.Parallel()

	client, calls := newSessionRPCClient(t, map[string]any{
		"session/new": SessionResult{SessionID: "fresh-acp-session"},
	})
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	history := []worker.ConversationTurn{{Role: "user", Content: "重建记录中的历史"}}
	lost, err := w.establishSession(t.Context(), client, worker.SessionInfo{
		SessionID:           "session-1",
		ProjectDir:          "/tmp",
		ConversationHistory: history,
	}, nil)
	require.NoError(t, err)
	require.True(t, lost)
	require.Equal(t, "fresh-acp-session", w.GetWorkerSessionID())

	w.Mu.Lock()
	gotHistory := append([]worker.ConversationTurn(nil), w.pendingHistory...)
	w.Mu.Unlock()
	require.Equal(t, history, gotHistory)
	require.Contains(t, w.injectHistoryPrefix("当前问题"), "重建记录中的历史")
	require.Equal(t, "session/new", <-calls)
}

func TestEstablishSession_NewFailureDoesNotSeedHistory(t *testing.T) {
	t.Parallel()

	client, calls := newSessionRPCClient(t, map[string]any{
		"session/new": &JSONRPCError{Code: -32000, Message: "new session unavailable"},
	})
	w := &Worker{BaseWorker: base.NewBaseWorker(nil, nil)}
	w.Mu.Lock()
	w.initResult = &InitializeResult{
		AgentCapabilities: map[string]any{"loadSession": false},
	}
	w.Mu.Unlock()

	_, err := w.establishSession(t.Context(), client, worker.SessionInfo{
		SessionID:           "session-1",
		ProjectDir:          "/tmp",
		WorkerSessionID:     "old-acp-session",
		ConversationHistory: []worker.ConversationTurn{{Role: "user", Content: "失败时不可注入"}},
	}, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "new session unavailable")
	w.Mu.Lock()
	require.Empty(t, w.pendingHistory)
	w.Mu.Unlock()
	require.Equal(t, "", w.GetWorkerSessionID())
	require.Equal(t, "session/new", <-calls)
}

func TestNewHistoryLostEnvelope_IsInformationalMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		hasHistory bool
		want       string
	}{
		{
			name:       "with text context",
			hasHistory: true,
			want:       "Previous conversation history could not be restored; starting a new session with prior messages supplied as text context.",
		},
		{
			name: "without text context",
			want: "Previous conversation history could not be restored; starting a new session without prior text context.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := newHistoryLostEnvelope(newTestMapper(), tt.hasHistory)
			require.Equal(t, events.Message, env.Event.Type)
			require.NotEqual(t, events.Error, env.Event.Type)

			data, ok := env.Event.Data.(events.MessageData)
			require.True(t, ok)
			require.Equal(t, "assistant", data.Role)
			require.Equal(t, "text", data.ContentType)
			require.Equal(t, tt.want, data.Content)
		})
	}
}
