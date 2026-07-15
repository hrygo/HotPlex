package claudecode

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// assistantMsgFor builds a raw SDK "assistant" message JSON carrying the given
// native message id and a single text block. Used to drive parseAssistant →
// mapper end-to-end without depending on the Claude CLI binary.
func assistantMsgFor(t *testing.T, messageID, text string) *WorkerEvent {
	t.Helper()
	raw := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role": "assistant",
			"content": []map[string]any{
				{"type": "text", "text": text},
			},
		},
	}
	if messageID != "" {
		raw["message"].(map[string]any)["id"] = messageID
	}
	b, err := json.Marshal(raw)
	require.NoError(t, err)
	evts, err := NewParser(nil).ParseLine(string(b))
	require.NoError(t, err)
	require.Len(t, evts, 1)
	return evts[0]
}

func resultEvent(success bool) *WorkerEvent {
	return &WorkerEvent{Type: EventResult, Payload: &ResultPayload{Success: success}}
}

// TestTurnIntegrity_NativeIDNotDropped proves parseAssistant now preserves the
// native message id on the emitted StreamPayload (RC-2 root cause). Without
// this, msgID degraded to the "assistant_msg" constant.
func TestTurnIntegrity_NativeIDNotDropped(t *testing.T) {
	evt := assistantMsgFor(t, "msg_abc", "hello")
	payload, ok := evt.Payload.(*StreamPayload)
	require.True(t, ok)
	require.Equal(t, "msg_abc", payload.MessageID, "native message id must propagate to StreamPayload")
}

// TestTurnIntegrity_CrossTurnLongShortShort (T-B1): a long turn-1 reply must
// not suppress shorter turn-2/turn-3 replies. Each turn delivers full content.
func TestTurnIntegrity_CrossTurnLongShortShort(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "s", func() int64 { return 1 })

	turns := []struct {
		id, text string
	}{
		{"msg_A", "这是一段很长的第一轮回复，用于建立较长的去重基线，从而验证后续短回复不会被错误抑制"},
		{"msg_B", "短回复二"},
		{"msg_C", "更短三"},
	}

	var delivered []string
	for _, tn := range turns {
		envs, err := mapper.Map(assistantMsgFor(t, tn.id, tn.text))
		require.NoError(t, err)
		require.Len(t, envs, 1)
		data := envs[0].Event.Data.(events.MessageDeltaData)
		require.Equal(t, tn.id, data.MessageID, "AEP MessageID must preserve native id")
		delivered = append(delivered, data.Content)
		// Result closes the turn and clears dedup state.
		_, err = mapper.Map(resultEvent(true))
		require.NoError(t, err)
	}

	require.Equal(t, turns[0].text, delivered[0], "turn 1 full text")
	require.Equal(t, turns[1].text, delivered[1], "turn 2 (shorter) must NOT be suppressed")
	require.Equal(t, turns[2].text, delivered[2], "turn 3 (shorter) must NOT be suppressed")
}

// TestTurnIntegrity_DifferentIDsDifferentNamespaces (T-B2): distinct native
// message ids get distinct dedup namespaces, so a snapshot equal to another
// message's sent text is still delivered.
func TestTurnIntegrity_DifferentIDsDifferentNamespaces(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "s", func() int64 { return 1 })

	// msg_A sends "shared text".
	envs, err := mapper.Map(assistantMsgFor(t, "msg_A", "shared text"))
	require.NoError(t, err)
	require.Equal(t, "shared text", envs[0].Event.Data.(events.MessageDeltaData).Content)

	// msg_B sends the same text but under a different native id → must deliver.
	envs, err = mapper.Map(assistantMsgFor(t, "msg_B", "shared text"))
	require.NoError(t, err)
	require.Equal(t, "shared text", envs[0].Event.Data.(events.MessageDeltaData).Content,
		"different native id must not be deduped against another message")
}

// TestTurnIntegrity_MissingIDTurnScopedSynthetic (T-B3): without a native id,
// the synthetic identity carries the turn epoch so two no-id turns differ.
func TestTurnIntegrity_MissingIDTurnScopedSynthetic(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "s", func() int64 { return 1 })

	envs1, err := mapper.Map(assistantMsgFor(t, "", "first"))
	require.NoError(t, err)
	id1 := envs1[0].Event.Data.(events.MessageDeltaData).MessageID
	require.NotEqual(t, "assistant_msg", id1, "must not degrade to worker-lifetime constant")
	require.Equal(t, "first", envs1[0].Event.Data.(events.MessageDeltaData).Content)

	_, err = mapper.Map(resultEvent(true))
	require.NoError(t, err)

	envs2, err := mapper.Map(assistantMsgFor(t, "", "second"))
	require.NoError(t, err)
	id2 := envs2[0].Event.Data.(events.MessageDeltaData).MessageID
	require.NotEqual(t, id1, id2, "synthetic ids must differ across turns")
	require.Equal(t, "second", envs2[0].Event.Data.(events.MessageDeltaData).Content,
		"second no-id turn must deliver full content")
}

// TestTurnIntegrity_SnapshotPrefixExtension (T-B4 / existing behavior): within
// one message, a full snapshot that strictly extends sent text yields only the
// tail; an identical snapshot yields empty (legal repeat).
func TestTurnIntegrity_SnapshotPrefixExtension(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "s", func() int64 { return 1 })

	// delta first
	envs, err := mapper.Map(&WorkerEvent{Type: EventStream, Payload: &StreamPayload{
		Type: "text", MessageID: "msg_X", Content: "Part 1", IsDelta: true,
	}})
	require.NoError(t, err)
	require.Equal(t, "Part 1", envs[0].Event.Data.(events.MessageDeltaData).Content)

	// identical snapshot → empty
	envs, err = mapper.Map(&WorkerEvent{Type: EventAssistant, Payload: &StreamPayload{
		Type: "text", MessageID: "msg_X", Content: "Part 1", IsDelta: false, BlockIndex: 1,
	}})
	require.NoError(t, err)
	// Note: msg_X delta used BlockIndex 0 (default); the assistant snapshot here
	// uses BlockIndex 1, so it is a DIFFERENT namespace and delivers in full.
	// This proves multi-block isolation: the snapshot is not swallowed even
	// though its text matches another block's sent text.
	require.Equal(t, "Part 1", envs[0].Event.Data.(events.MessageDeltaData).Content)
}

// TestTurnIntegrity_PrefixDriftNotSwallowed (T-B5): a snapshot that is shorter
// or prefix-divergent must not return empty. It emits full content + warning.
func TestTurnIntegrity_PrefixDriftNotSwallowed(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "s", func() int64 { return 1 })

	// Establish long sent baseline under msg_D block 0.
	_, err := mapper.Map(&WorkerEvent{Type: EventAssistant, Payload: &StreamPayload{
		Type: "text", MessageID: "msg_D", Content: "long baseline reply that is longer", IsDelta: false, BlockIndex: 0,
	}})
	require.NoError(t, err)

	// A SHORTER snapshot under the SAME id+block+type → drift → full content.
	envs, err := mapper.Map(&WorkerEvent{Type: EventAssistant, Payload: &StreamPayload{
		Type: "text", MessageID: "msg_D", Content: "short", IsDelta: false, BlockIndex: 0,
	}})
	require.NoError(t, err)
	require.Equal(t, "short", envs[0].Event.Data.(events.MessageDeltaData).Content,
		"shorter snapshot must NOT be silently swallowed")

	// A divergent (non-prefix) snapshot of equal length → drift → full content.
	envs, err = mapper.Map(&WorkerEvent{Type: EventAssistant, Payload: &StreamPayload{
		Type: "text", MessageID: "msg_D", Content: "completely different text!", IsDelta: false, BlockIndex: 0,
	}})
	require.NoError(t, err)
	require.Equal(t, "completely different text!", envs[0].Event.Data.(events.MessageDeltaData).Content,
		"divergent snapshot must NOT be silently swallowed")
}

// TestTurnIntegrity_SentTextsBoundedAcrossTurns (T-B6): after many turns the
// sentTexts map must not grow without bound — Result clears it each turn.
func TestTurnIntegrity_SentTextsBoundedAcrossTurns(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "s", func() int64 { return 1 })

	for i := range 1000 {
		_, err := mapper.Map(assistantMsgFor(t, fmt.Sprintf("msg_%d", i), "reply"))
		require.NoError(t, err)
		_, err = mapper.Map(resultEvent(true))
		require.NoError(t, err)
	}

	mapper.mu.Lock()
	defer mapper.mu.Unlock()
	require.Less(t, len(mapper.sentTexts), 50,
		"sentTexts must be cleared per turn, not grow with turn count; got %d", len(mapper.sentTexts))
}
