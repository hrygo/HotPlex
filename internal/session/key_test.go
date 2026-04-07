package session

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/hotplex/hotplex-worker/internal/worker"
)

func TestDeriveSessionKey_Deterministic(t *testing.T) {
	t.Parallel()

	key := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1")
	for i := 0; i < 1000; i++ {
		got := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1")
		require.Equal(t, key, got, "DeriveSessionKey must be deterministic across 1000 calls")
	}
}

func TestDeriveSessionKey_DifferentTriples(t *testing.T) {
	t.Parallel()

	key1 := DeriveSessionKey("u1", worker.TypeClaudeCode, "s1")
	key2 := DeriveSessionKey("u2", worker.TypeClaudeCode, "s1")
	key3 := DeriveSessionKey("u1", worker.TypeOpenCodeCLI, "s1")
	key4 := DeriveSessionKey("u1", worker.TypeClaudeCode, "s2")

	require.NotEqual(t, key1, key2, "different ownerID → different key")
	require.NotEqual(t, key1, key3, "different workerType → different key")
	require.NotEqual(t, key1, key4, "different clientSessionID → different key")
}

func TestDeriveSessionKey_UUIDv5Format(t *testing.T) {
	t.Parallel()

	// UUIDv5 format: xxxxxxxx-xxxx-5xxx-yxxx-xxxxxxxxxxxx
	// y is one of [8, 9, a, b]
	uuidV5Regex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)

	tests := []struct {
		ownerID    string
		workerType worker.WorkerType
		sessionID  string
	}{
		{"u1", worker.TypeClaudeCode, "s1"},
		{"user_long_id", worker.TypeOpenCodeCLI, "my-session-123"},
		{"", worker.TypePimon, ""},
		{"owner", worker.TypeOpenCodeSrv, "session-with-dashes"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.workerType), func(t *testing.T) {
			t.Parallel()
			key := DeriveSessionKey(tt.ownerID, tt.workerType, tt.sessionID)
			require.Regexp(t, uuidV5Regex, key, "output must be valid UUIDv5 format")
		})
	}
}

func TestDeriveSessionKey_EmptyString(t *testing.T) {
	t.Parallel()

	// Empty client_session_id still produces a valid UUIDv5
	key := DeriveSessionKey("u1", worker.TypeClaudeCode, "")
	require.NotEmpty(t, key)
	uuidV5Regex := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	require.Regexp(t, uuidV5Regex, key)
}

func TestDeriveSessionKey_AllWorkerTypes(t *testing.T) {
	t.Parallel()

	sessionID := "test-session"
	for _, wt := range []worker.WorkerType{
		worker.TypeClaudeCode,
		worker.TypeOpenCodeCLI,
		worker.TypeOpenCodeSrv,
		worker.TypePimon,
		worker.TypeUnknown,
	} {
		wt := wt
		t.Run(string(wt), func(t *testing.T) {
			t.Parallel()
			key := DeriveSessionKey("owner1", wt, sessionID)
			require.NotEmpty(t, key)
			// Same triple must produce same key
			key2 := DeriveSessionKey("owner1", wt, sessionID)
			require.Equal(t, key, key2)
		})
	}
}
