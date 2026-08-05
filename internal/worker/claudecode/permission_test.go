package claudecode

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestPermissionModeToCCArg(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode    string
		permArg string
		skip    bool
	}{
		{worker.PermissionModeReadOnly, "plan", false},
		{worker.PermissionModeWorkspace, "acceptEdits", false},
		{worker.PermissionModeAutoEdit, "auto", false},
		{worker.PermissionModeBypass, "", true},
		{"", "", true},     // empty → CC bypass ("worker default": CC applies bypass when no override)
		{"plan", "", true}, // legacy value → bypass (no longer a valid tier)
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			permArg, skip := permissionModeToCCArg(tt.mode)
			require.Equal(t, tt.permArg, permArg)
			require.Equal(t, tt.skip, skip)
		})
	}
}

func TestResolvePermissionMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		sessionMode  string
		operatorMode string
		want         string
	}{
		{"operator fallback on empty session (platform/cron)", "", worker.PermissionModeWorkspace, worker.PermissionModeWorkspace},
		{"operator bypass honored on empty session", "", worker.PermissionModeBypass, worker.PermissionModeBypass},
		{"both empty → empty (CC maps to bypass)", "", "", ""},
		{"session below operator ceiling wins (more restrictive)", worker.PermissionModeReadOnly, worker.PermissionModeWorkspace, worker.PermissionModeReadOnly},
		{"session at operator ceiling unchanged", worker.PermissionModeWorkspace, worker.PermissionModeWorkspace, worker.PermissionModeWorkspace},
		{"session above operator ceiling clamped down", worker.PermissionModeAutoEdit, worker.PermissionModeWorkspace, worker.PermissionModeWorkspace},
		{"session bypass clamped to operator workspace", worker.PermissionModeBypass, worker.PermissionModeWorkspace, worker.PermissionModeWorkspace},
		{"empty operator never clamps (session wins)", worker.PermissionModeAutoEdit, "", worker.PermissionModeAutoEdit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := worker.ResolvePermissionMode(tt.sessionMode, tt.operatorMode)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestBuildCLIArgs_RestrictedModesIgnoreCallerAllowedTools(t *testing.T) {
	origOperatorMode := operatorPermissionMode.Load()
	operatorPermissionMode.Store("")
	t.Cleanup(func() { operatorPermissionMode.Store(origOperatorMode) })

	for _, mode := range []string{
		worker.PermissionModeReadOnly,
		worker.PermissionModeWorkspace,
	} {
		t.Run(mode, func(t *testing.T) {
			w := New()
			args, err := w.buildCLIArgs(worker.SessionInfo{
				SessionID:      "permission-ceiling-" + mode,
				PermissionMode: mode,
				AllowedTools:   []string{"Read", "Write", "Bash", "NotebookEdit"},
			}, false)
			require.NoError(t, err)
			require.NotContains(t, args, "--allowed-tools")
		})
	}
}

func TestBuildCLIArgs_PermissiveModesRetainCallerAllowedTools(t *testing.T) {
	origOperatorMode := operatorPermissionMode.Load()
	operatorPermissionMode.Store("")
	t.Cleanup(func() { operatorPermissionMode.Store(origOperatorMode) })

	w := New()
	args, err := w.buildCLIArgs(worker.SessionInfo{
		SessionID:      "permission-ceiling-auto-edit",
		PermissionMode: worker.PermissionModeAutoEdit,
		AllowedTools:   []string{"Read", "Write"},
	}, false)
	require.NoError(t, err)
	require.Contains(t, args, "--allowed-tools")
}

func TestClaudePermissionCeilingRejectsEscalation(t *testing.T) {
	t.Parallel()
	w := New()
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))

	body, err := w.preparePermissionModeRequest(map[string]any{"mode": "plan"})
	require.NoError(t, err)
	require.Equal(t, "plan", body["mode"])

	body, err = w.preparePermissionModeRequest(map[string]any{"mode": "acceptEdits"})
	require.NoError(t, err)
	require.Equal(t, "acceptEdits", body["mode"])

	_, err = w.preparePermissionModeRequest(map[string]any{"mode": "auto"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation)

	_, err = w.preparePermissionModeRequest(map[string]any{"mode": "bypassPermissions"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation)

	_, err = w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "bypassPermissions"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation,
		"the public ControlRequester path must enforce the same ceiling")
}

func TestClaudePermissionCeilingSurvivesRestartInputs(t *testing.T) {
	t.Parallel()
	w := New()
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))

	require.Equal(t, worker.PermissionModeWorkspace, w.effectivePermissionMode(worker.SessionInfo{
		PermissionMode: worker.PermissionModeBypass,
	}), "a reset/resume input must not replace the first captured ceiling")
}

func TestClaudePermissionRejectionRedactsUntrustedMode(t *testing.T) {
	t.Parallel()
	const sentinel = "bypass-SECRET-token-916"
	var logs bytes.Buffer
	w := New()
	w.Log = slog.New(slog.NewJSONHandler(&logs, nil))
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))

	_, err := w.preparePermissionModeRequest(map[string]any{"mode": sentinel})
	require.ErrorIs(t, err, worker.ErrInvalidPermissionMode)
	require.NotContains(t, err.Error(), sentinel)
	require.NotContains(t, logs.String(), sentinel)
	require.Contains(t, logs.String(), `"reason":"invalid_mode"`)
	require.False(t, strings.Contains(strings.ToLower(logs.String()), "secret"))
}
