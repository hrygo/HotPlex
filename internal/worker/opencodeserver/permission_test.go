package opencodeserver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestPermissionModeToOCS(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want string
	}{
		{worker.PermissionModeReadOnly, "plan"},
		{worker.PermissionModeWorkspace, "acceptEdits"},
		{worker.PermissionModeAutoEdit, "acceptEdits"},
		{worker.PermissionModeBypass, "bypassPermissions"},
		{"", "bypassPermissions"}, // empty → default bypass
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, permissionModeToOCS(tt.mode))
		})
	}
}

func TestOpenCodeServerPermissionCeilingPublicPath(t *testing.T) {
	t.Parallel()
	w := New()
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))

	request, err := w.preparePermissionModeRequest(map[string]any{"mode": "plan"})
	require.NoError(t, err)
	require.Equal(t, "plan", request["mode"])
	require.Equal(t, worker.PermissionModeReadOnly, request["permission_tier"])

	request, err = w.preparePermissionModeRequest(map[string]any{"mode": "acceptEdits"})
	require.NoError(t, err)
	require.Equal(t, "acceptEdits", request["mode"])
	require.Equal(t, worker.PermissionModeWorkspace, request["permission_tier"])

	_, err = w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "auto"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation)

	_, err = w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "bypassPermissions"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation)
}
