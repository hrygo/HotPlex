package acp

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestPermissionModeToACPApprove(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode string
		want bool
	}{
		{worker.PermissionModeReadOnly, false},
		{worker.PermissionModeWorkspace, false},
		{worker.PermissionModeAutoEdit, true},
		{worker.PermissionModeBypass, true},
		{"", true}, // empty → default auto-approve (backward compat)
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, permissionModeToACPApprove(tt.mode))
		})
	}
}

func TestACPPermissionCeilingRejectsEscalation(t *testing.T) {
	t.Parallel()
	w := &Worker{}
	require.NoError(t, w.permissionCeiling.Capture(worker.PermissionModeWorkspace))

	resp, err := w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "plan"})
	require.NoError(t, err)
	require.Equal(t, worker.PermissionModeReadOnly, resp["mode"])
	require.False(t, w.autoApprove.Load())

	resp, err = w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "acceptEdits"})
	require.NoError(t, err)
	require.Equal(t, worker.PermissionModeWorkspace, resp["mode"])
	require.False(t, w.autoApprove.Load())

	_, err = w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "auto-accept"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation)

	_, err = w.SendControlRequest(t.Context(), "set_permission_mode", map[string]any{"mode": "bypassPermissions"})
	require.ErrorIs(t, err, worker.ErrPermissionEscalation)
}
