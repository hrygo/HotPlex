package gateway

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
)

// testLogger returns a minimal logger for bridge unit tests.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.Default()
}

// stubWSStore implements WorkspaceOverridesReader for bridge helper tests.
type stubWSStore struct {
	ws  *session.Workspace
	err error
}

func (s *stubWSStore) GetWorkspaceByID(_ context.Context, _ string) (*session.Workspace, error) {
	return s.ws, s.err
}

// newBridgeForOverrideTest builds a minimal Bridge with only wsStore + log set,
// enough to exercise resolveWorkspaceOverrides without the full dependency graph.
func newBridgeForOverrideTest(t *testing.T, ws *session.Workspace, err error) *Bridge {
	t.Helper()
	return &Bridge{
		log:     testLogger(t),
		wsStore: &stubWSStore{ws: ws, err: err},
	}
}

func TestResolveWorkspaceOverrides(t *testing.T) {
	t.Parallel()

	t.Run("empty workspace id returns nil", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, nil, nil)
		require.Nil(t, b.resolveWorkspaceOverrides(""))
	})

	t.Run("nil wsStore returns nil", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{log: testLogger(t)} // wsStore zero-value nil
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})

	t.Run("valid overrides parsed", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: `{"SOUL.md":"x","USER.md":"y"}`}
		b := newBridgeForOverrideTest(t, ws, nil)
		got := b.resolveWorkspaceOverrides("ws-1")
		require.Equal(t, map[string]string{"SOUL.md": "x", "USER.md": "y"}, got)
	})

	t.Run("empty overrides string returns nil", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: ""}
		b := newBridgeForOverrideTest(t, ws, nil)
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})

	t.Run("store error degrades to nil", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, nil, errors.New("boom"))
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})

	t.Run("invalid JSON degrades to nil", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: `{bad`}
		b := newBridgeForOverrideTest(t, ws, nil)
		require.Nil(t, b.resolveWorkspaceOverrides("ws-1"))
	})
}
