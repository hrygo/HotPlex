package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
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

func (s *stubWSStore) ListAllWorkspaces(_ context.Context) ([]*session.Workspace, error) {
	if s.ws != nil {
		return []*session.Workspace{s.ws}, nil
	}
	return nil, nil
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
		require.Nil(t, b.resolveWorkspaceOverrides(context.Background(), ""))
	})

	t.Run("nil wsStore returns nil", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{log: testLogger(t)} // wsStore zero-value nil
		require.Nil(t, b.resolveWorkspaceOverrides(context.Background(), "ws-1"))
	})

	t.Run("valid overrides parsed", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: `{"SOUL.md":"x","USER.md":"y"}`}
		b := newBridgeForOverrideTest(t, ws, nil)
		got := b.resolveWorkspaceOverrides(context.Background(), "ws-1")
		require.Equal(t, map[string]string{"SOUL.md": "x", "USER.md": "y"}, got)
	})

	t.Run("empty overrides string returns nil", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: ""}
		b := newBridgeForOverrideTest(t, ws, nil)
		require.Nil(t, b.resolveWorkspaceOverrides(context.Background(), "ws-1"))
	})

	t.Run("store error degrades to nil", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, nil, errors.New("boom"))
		require.Nil(t, b.resolveWorkspaceOverrides(context.Background(), "ws-1"))
	})

	t.Run("invalid JSON degrades to nil", func(t *testing.T) {
		t.Parallel()
		ws := &session.Workspace{ID: "ws-1", AgentConfigOverrides: `{bad`}
		b := newBridgeForOverrideTest(t, ws, nil)
		require.Nil(t, b.resolveWorkspaceOverrides(context.Background(), "ws-1"))
	})

	t.Run("warn dedup: repeated degrade warns once, then re-arms on success (#749)", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		ws := &session.Workspace{ID: "ws-dedup", AgentConfigOverrides: `{bad`}
		b := &Bridge{
			log:     slog.New(h),
			wsStore: &stubWSStore{ws: ws},
		}
		// Three consecutive degrade calls — only first should warn.
		b.resolveWorkspaceOverrides(context.Background(), "ws-dedup")
		b.resolveWorkspaceOverrides(context.Background(), "ws-dedup")
		b.resolveWorkspaceOverrides(context.Background(), "ws-dedup")
		warnCount := strings.Count(buf.String(), "level=WARN")
		require.Equal(t, 1, warnCount, "expected exactly 1 warn for repeated degrade, got %d: %s", warnCount, buf.String())

		// Fix overrides → valid resolution clears the flag.
		ws.AgentConfigOverrides = `{"SOUL.md":"fixed"}`
		b.resolveWorkspaceOverrides(context.Background(), "ws-dedup")
		// No new warn from success path.
		require.Equal(t, 1, strings.Count(buf.String(), "level=WARN"))

		// Break again → new warn appears (flag was cleared).
		ws.AgentConfigOverrides = `{broken`
		b.resolveWorkspaceOverrides(context.Background(), "ws-dedup")
		require.Equal(t, 2, strings.Count(buf.String(), "level=WARN"),
			"expected warn re-armed after success, got: %s", buf.String())
	})
}
