package gateway

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
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
	b := &Bridge{
		log:     testLogger(t),
		wsStore: &stubWSStore{ws: ws, err: err},
	}
	// r3 (#804): resolveWorkspacePermissionMode now consumes the bridge default
	// when a workspace has no explicit override, mirroring NewBridge (bridge.go).
	// Seed the r3 production default so tests that hit the no-override branch see
	// "workspace"; tests needing a different value Store their own.
	b.defaultPermissionMode.Store(worker.PermissionModeWorkspace)
	return b
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

func TestResolveWorkspacePermissionMode(t *testing.T) {
	t.Parallel()

	t.Run("empty workspace id returns empty (platform/cron sessions, #789 P1)", func(t *testing.T) {
		t.Parallel()
		// Sessions without a workspace must NOT get bypass injected — each worker
		// applies its own default/config (codex honors cfg.Sandbox, ACP honors cfg.AutoApprove).
		b := newBridgeForOverrideTest(t, &session.Workspace{PermissionMode: "bypass"}, nil)
		require.Equal(t, "", b.resolveWorkspacePermissionMode(""))
	})

	t.Run("nil wsStore returns empty", func(t *testing.T) {
		t.Parallel()
		b := &Bridge{log: testLogger(t)}
		require.Equal(t, "", b.resolveWorkspacePermissionMode("ws-1"))
	})

	t.Run("workspace override wins", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, &session.Workspace{PermissionMode: "read-only"}, nil)
		require.Equal(t, "read-only", b.resolveWorkspacePermissionMode("ws-1"))
	})

	t.Run("workspace unset returns bridge default (r3: global default now injected, #804)", func(t *testing.T) {
		t.Parallel()
		// r3: a workspace with no explicit override returns the bridge's global
		// default (config.worker.default_permission_mode). The helper seeds
		// "workspace" — the r3 production default. Because the default dropped from
		// bypass→workspace in the same revision that injection was enabled, this is
		// a tightening (CC acceptEdits, codex workspace-write, ACP approve=false),
		// not an escalation — see Workspace-Permission-Mode-Admin-Only-Revision-Spec §2.
		b := newBridgeForOverrideTest(t, &session.Workspace{PermissionMode: ""}, nil)
		require.Equal(t, "workspace", b.resolveWorkspacePermissionMode("ws-1"))
	})

	t.Run("custom global default is injected for workspace without override (r3, #804)", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, &session.Workspace{PermissionMode: ""}, nil)
		b.defaultPermissionMode.Store("read-only")
		// r3: operators tune config.worker.default_permission_mode (or call
		// UpdateDefaultPermissionMode); a workspace without explicit override
		// returns that configured default rather than "".
		require.Equal(t, "read-only", b.resolveWorkspacePermissionMode("ws-1"))
	})

	t.Run("hot-reload default via UpdateDefaultPermissionMode applies to next resolve (r3, #804)", func(t *testing.T) {
		t.Parallel()
		b := newBridgeForOverrideTest(t, &session.Workspace{PermissionMode: ""}, nil)
		require.Equal(t, "workspace", b.resolveWorkspacePermissionMode("ws-1"))
		b.UpdateDefaultPermissionMode("auto-edit")
		require.Equal(t, "auto-edit", b.resolveWorkspacePermissionMode("ws-1"))
	})

	t.Run("fetch error degrades to bridge default, not bypass (r3 #804 fail-closed)", func(t *testing.T) {
		t.Parallel()
		// Fail-closed: a transient DB outage must NOT degrade to "" (which CC/OCS
		// map to bypass, re-opening the r2 P1 escalation). The helper seeds
		// "workspace" — the r3 bridge default — so a fetch failure keeps the
		// session bounded at workspace, the same tier a no-override workspace gets.
		b := newBridgeForOverrideTest(t, nil, errors.New("db down"))
		require.Equal(t, "workspace", b.resolveWorkspacePermissionMode("ws-1"))
	})

	t.Run("warn dedup: success re-arms warning (#789 P2, mirrors #749)", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		store := &stubWSStore{}
		b := &Bridge{log: slog.New(h), wsStore: store}

		// Fetch fails twice — only the first warns.
		store.err = errors.New("boom")
		b.resolveWorkspacePermissionMode("ws-dedup")
		b.resolveWorkspacePermissionMode("ws-dedup")
		require.Equal(t, 1, strings.Count(buf.String(), "level=WARN"),
			"expected 1 warn for repeated degrade, got: %s", buf.String())

		// Fetch succeeds → Delete clears the flag (P2 fix), no new warn.
		store.err = nil
		store.ws = &session.Workspace{PermissionMode: "read-only"}
		b.resolveWorkspacePermissionMode("ws-dedup")
		require.Equal(t, 1, strings.Count(buf.String(), "level=WARN"), "success path must not warn")

		// Fail again → new warn appears (flag was cleared by the success path).
		store.err = errors.New("boom2")
		store.ws = nil
		b.resolveWorkspacePermissionMode("ws-dedup")
		require.Equal(t, 2, strings.Count(buf.String(), "level=WARN"),
			"expected warn re-armed after success, got: %s", buf.String())
	})
}

// replayNativeWorker records InvokeNativeCommand calls; mockWorkerForHandler
// alone does NOT implement worker.NativeCommandInvoker.
type replayNativeWorker struct {
	mockWorkerForHandler
	invoked worker.NativeCommandInvocation
}

func (w *replayNativeWorker) InvokeNativeCommand(_ context.Context, invocation worker.NativeCommandInvocation) error {
	w.invoked = invocation
	return nil
}

func TestDeliverInputReplayUsesNativeInvokerWhenAvailable(t *testing.T) {
	t.Parallel()

	b := &Bridge{log: testLogger(t)}
	w := &replayNativeWorker{}
	replay := worker.InputReplay{
		Content: "/oracle-dba 10.102.78.1",
		Skill:   &worker.NativeCommandInvocation{Name: "oracle-dba", Args: "10.102.78.1"},
	}

	require.NoError(t, b.deliverInputReplay(t.Context(), w, replay))
	require.Equal(t, *replay.Skill, w.invoked)
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
}

func TestDeliverInputReplayFallsBackToTextWithoutSkillInvoker(t *testing.T) {
	t.Parallel()

	// Regression: a replacement worker without SkillInvoker (e.g. worker_type
	// changed during recovery) used to drop the pending Skill input entirely.
	// It must degrade to the reconstructed slash text instead.
	b := &Bridge{log: testLogger(t)}
	w := new(mockWorkerForHandler)
	w.On("Input", mock.Anything, "/oracle-dba 10.102.78.1", mock.Anything).Return(nil).Once()
	replay := worker.InputReplay{
		Content: "/oracle-dba 10.102.78.1",
		Skill:   &worker.NativeCommandInvocation{Name: "oracle-dba", Args: "10.102.78.1"},
	}

	require.NoError(t, b.deliverInputReplay(t.Context(), w, replay))
	w.AssertExpectations(t)
}

func TestDeliverInputReplayErrorsWhenNoInvokerAndNoText(t *testing.T) {
	t.Parallel()

	b := &Bridge{log: testLogger(t)}
	w := new(mockWorkerForHandler)
	replay := worker.InputReplay{
		Skill: &worker.NativeCommandInvocation{Name: "oracle-dba"},
	}

	err := b.deliverInputReplay(t.Context(), w, replay)
	require.ErrorIs(t, err, worker.ErrSkillNotSupported)
	w.AssertNotCalled(t, "Input", mock.Anything, mock.Anything, mock.Anything)
}
