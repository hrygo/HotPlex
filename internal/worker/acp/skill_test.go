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
)

func TestParseAvailableCommands(t *testing.T) {
	t.Parallel()

	got := parseAvailableCommands(json.RawMessage(`{
		"sessionId":"sess-1",
		"update":{
			"sessionUpdate":"available_commands_update",
			"availableCommands":[
				{"name":"oracle-dba","description":"Inspect Oracle"},
				{"name":"test","description":"Run tests"}
			]
		}
	}`))
	require.Equal(t, []worker.SkillDescriptor{
		{Name: "oracle-dba", Description: "Inspect Oracle"},
		{Name: "test", Description: "Run tests"},
	}, got)
}

func TestWorkerListInvokableSkillsUsesAdvertisedCommands(t *testing.T) {
	t.Parallel()

	w := &Worker{availableCommands: map[string]worker.SkillDescriptor{
		"oracle-dba": {Name: "oracle-dba", Description: "Inspect Oracle"},
	}}
	got, err := w.ListInvokableSkills(t.Context(), "/workspace")
	require.NoError(t, err)
	require.Equal(t, []worker.SkillDescriptor{{Name: "oracle-dba", Description: "Inspect Oracle"}}, got)
}

func TestWorkerAvailableCommandsSurviveOtherNotifications(t *testing.T) {
	t.Parallel()

	w := &Worker{}
	w.updateAvailableCommands(json.RawMessage(`{
		"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"oracle-dba"}]}
	}`))
	w.updateAvailableCommands(json.RawMessage(`{"update":{"sessionUpdate":"agent_message_chunk"}}`))

	got, err := w.ListInvokableSkills(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, []worker.SkillDescriptor{{Name: "oracle-dba"}}, got)
}

func TestProcessNotificationUpdatesAvailableCommands(t *testing.T) {
	t.Parallel()

	w := &Worker{
		BaseWorker: base.NewBaseWorker(nil, nil),
		mapper:     NewACPMapper("sess-1", "user-1", nil),
	}
	conn := newACPConn("user-1", "sess-1", nil)
	w.conn = conn
	w.processNotification(t.Context(), &JSONRPCNotification{
		Method: "session/update",
		Params: json.RawMessage(`{
			"sessionId":"sess-1",
			"update":{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"oracle-dba"}]}
		}`),
	}, conn)

	got, err := w.ListInvokableSkills(t.Context(), "")
	require.NoError(t, err)
	require.Equal(t, []worker.SkillDescriptor{{Name: "oracle-dba"}}, got)
}

func TestInvokeSkillPreservesExplicitCommandAfterPromptIsolation(t *testing.T) {
	t.Parallel()

	agentStdinR, agentStdinW := io.Pipe()
	agentStdoutR, agentStdoutW := io.Pipe()
	defer agentStdinW.Close()
	defer agentStdoutW.Close()

	client := NewACPClient(agentStdinW, agentStdoutR, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.StartReadLoop(ctx)

	receivedPrompt := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(agentStdinR)
		if !scanner.Scan() {
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Params struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil || len(req.Params.Prompt) == 0 {
			return
		}
		receivedPrompt <- req.Params.Prompt[0].Text
		_ = WriteMessage(agentStdoutW, &JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshal(PromptResult{StopReason: "end_turn"}),
		})
	}()

	w := &Worker{
		BaseWorker: base.NewBaseWorker(nil, nil),
		availableCommands: map[string]worker.SkillDescriptor{
			"oracle-dba": {Name: "oracle-dba"},
		},
	}
	w.client = client
	w.mapper = newTestMapper()
	w.conn = newACPConn("user-1", "session-1", nil)
	w.SetWorkerSessionID("acp-session-1")
	w.drainCh = make(chan struct{}, 1)
	w.drainDoneCh = make(chan struct{})
	close(w.drainDoneCh)
	w.systemPrompt = "PRIVATE_PROMPT_SENTINEL"

	require.NoError(t, w.InvokeSkill(ctx, worker.SkillInvocation{
		Name: "oracle-dba",
		Args: "--safe",
	}))
	require.Equal(t, "/oracle-dba --safe", <-receivedPrompt)
}
