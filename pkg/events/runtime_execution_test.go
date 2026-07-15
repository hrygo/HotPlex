package events

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntimeExecutionKinds_AreAdditiveForOldClients(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		kind Kind
		data RuntimeExecutionData
	}{
		{
			name: "started",
			kind: RuntimeExecutionStarted,
			data: RuntimeExecutionData{ExecutionID: "exec_test", Status: "running"},
		},
		{
			name: "completed",
			kind: RuntimeExecutionCompleted,
			data: RuntimeExecutionData{ExecutionID: "exec_test", Status: "completed"},
		},
		{
			name: "failed",
			kind: RuntimeExecutionFailed,
			data: RuntimeExecutionData{ExecutionID: "exec_test", Status: "failed", ErrorCode: "TOOL_ERROR"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			env := NewEnvelope("msg_test", "session_test", 1, tc.kind, tc.data)
			env.Timestamp = 1700000000000

			raw, err := json.Marshal(env)
			require.NoError(t, err)

			var decoded map[string]any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.Equal(t, string(tc.kind), decoded["event"].(map[string]any)["type"])

			var reEnv Envelope
			require.NoError(t, json.Unmarshal(raw, &reEnv))
			require.Equal(t, tc.kind, reEnv.Event.Type)
		})
	}
}

func TestRuntimeExecutionKinds_KindStrings(t *testing.T) {
	t.Parallel()

	require.Equal(t, "runtime.execution.started", string(RuntimeExecutionStarted))
	require.Equal(t, "runtime.execution.completed", string(RuntimeExecutionCompleted))
	require.Equal(t, "runtime.execution.failed", string(RuntimeExecutionFailed))
}

func TestRuntimeExecutionData_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := RuntimeExecutionData{
		ExecutionID: "exec_abc123",
		Status:      "completed",
		StartedAt:   1700000000000,
		FinishedAt:  1700000005000,
	}

	raw, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded RuntimeExecutionData
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Equal(t, original.ExecutionID, decoded.ExecutionID)
	require.Equal(t, original.Status, decoded.Status)
	require.Equal(t, original.StartedAt, decoded.StartedAt)
	require.Equal(t, original.FinishedAt, decoded.FinishedAt)
}

func TestEnvelopeWithUnknownKind_DecodesWithoutError(t *testing.T) {
	t.Parallel()

	raw := []byte(`{"id":"msg_x","version":"aep/v1","session_id":"s1","seq":1,"timestamp":1700000000000,"event":{"type":"some.future.unknown.kind","data":{"foo":"bar"}}}`)

	var env Envelope
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, Kind("some.future.unknown.kind"), env.Event.Type)
}
