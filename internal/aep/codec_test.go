package aep

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"hotplex-worker/pkg/events"
)

func TestNewID(t *testing.T) {
	t.Parallel()

	id := NewID()
	require.True(t, strings.HasPrefix(id, "evt_"))
	require.Len(t, id, len("evt_")+36) // uuid is 36 chars
}

func TestNewSessionID(t *testing.T) {
	t.Parallel()

	id := NewSessionID()
	require.True(t, strings.HasPrefix(id, "sess_"))
	require.Len(t, id, len("sess_")+36)
}

func TestNewID_Uniqueness(t *testing.T) {
	t.Parallel()

	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := NewID()
		require.False(t, ids[id], "duplicate ID generated: %s", id)
		ids[id] = true
	}
}

func TestEncodeDecode(t *testing.T) {
	t.Parallel()

	env := events.NewEnvelope(
		NewID(),
		"sess_test",
		42,
		events.State,
		events.StateData{State: events.StateRunning},
	)

	var sb strings.Builder
	err := Encode(&sb, env)
	require.NoError(t, err)

	decoded, err := Decode(strings.NewReader(sb.String()))
	require.NoError(t, err)
	require.Equal(t, env.ID, decoded.ID)
	require.Equal(t, env.Seq, decoded.Seq)
	require.Equal(t, env.Event.Type, decoded.Event.Type)
}

func TestEncodeChunk(t *testing.T) {
	t.Parallel()

	env := events.NewEnvelope(
		NewID(),
		"sess_chunk",
		1,
		events.Input,
		events.InputData{Content: "hello"},
	)

	var sb strings.Builder
	err := EncodeChunk(&sb, env)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(sb.String(), "\n"))

	// Decode and verify
	decoded, err := Decode(strings.NewReader(sb.String()))
	require.NoError(t, err)
	require.Equal(t, env.SessionID, decoded.SessionID)
}

func TestDecode_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := Decode(strings.NewReader(`{invalid}`))
	require.Error(t, err)
}

func TestDecodeLine_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := DecodeLine([]byte(`not json`))
	require.Error(t, err)
}

func TestValidate_MissingVersion(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "version")
}

func TestValidate_MissingID(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "id")
}

func TestValidate_MissingSessionID(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session_id")
}

func TestValidate_NonPositiveSeq(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       0,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "seq")
}

func TestValidate_NonPositiveTimestamp(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 0,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "timestamp")
}

func TestValidate_MissingEventType(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: "", Data: nil},
	}

	err := Validate(env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "event.kind")
}

func TestValidate_ValidEnvelope(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Version:   events.Version,
		ID:        NewID(),
		Seq:       1,
		SessionID: "sess_123",
		Timestamp: 1700000000000,
		Event:     events.Event{Type: events.State, Data: events.StateData{State: events.StateRunning}},
	}

	err := Validate(env)
	require.NoError(t, err)
}

func TestEncodeJSON(t *testing.T) {
	t.Parallel()

	env := events.NewEnvelope(
		NewID(),
		"sess_json",
		1,
		events.Done,
		events.DoneData{Success: true},
	)

	data, err := EncodeJSON(env)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	require.True(t, strings.HasPrefix(string(data), `{"version":"aep/v1"`))
}

func TestMustMarshal(t *testing.T) {
	t.Parallel()

	env := events.NewEnvelope(NewID(), "sess_must", 1, events.Input, events.InputData{Content: "test"})
	data := MustMarshal(env)
	require.NotEmpty(t, data)
}

func TestIsSessionBusy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  *events.Envelope
		want bool
	}{
		{
			name: "session busy error",
			env: events.NewEnvelope(
				NewID(), "sess_123", 1, events.Error,
				// IsSessionBusy checks map[string]any, so use map here
				map[string]any{"code": string(events.ErrCodeSessionBusy), "message": "busy"},
			),
			want: true,
		},
		{
			name: "other error",
			env: events.NewEnvelope(
				NewID(), "sess_123", 1, events.Error,
				map[string]any{"code": string(events.ErrCodeInternalError), "message": "internal"},
			),
			want: false,
		},
		{
			name: "not an error event",
			env: events.NewEnvelope(
				NewID(), "sess_123", 1, events.Input,
				events.InputData{Content: "hello"},
			),
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsSessionBusy(tt.env)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestIsTerminalEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind    events.Kind
		terminal bool
	}{
		{events.Done, true},
		{events.Error, true},
		{events.Input, false},
		{events.State, false},
		{events.ToolCall, false},
		{events.Ping, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.kind), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.terminal, IsTerminalEvent(tt.kind))
		})
	}
}

func TestParseSessionID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"sess_abc123", "abc123"},
		{"abc123", "abc123"},
		{"sess_", ""},
		{"", ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := ParseSessionID(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSeqKey(t *testing.T) {
	t.Parallel()

	key := SeqKey("sess_123", "evt_abc")
	require.Equal(t, "sess_123:evt_abc", key)
}
