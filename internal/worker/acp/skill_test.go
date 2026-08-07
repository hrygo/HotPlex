package acp

import (
	"encoding/json"
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
