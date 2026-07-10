package claudecode

import (
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
		{"explicit session tier wins over operator", worker.PermissionModeAutoEdit, worker.PermissionModeWorkspace, worker.PermissionModeAutoEdit},
		{"session tier wins over empty operator", worker.PermissionModeReadOnly, "", worker.PermissionModeReadOnly},
		{"operator fallback on empty session (platform/cron)", "", worker.PermissionModeWorkspace, worker.PermissionModeWorkspace},
		{"operator bypass honored on empty session", "", worker.PermissionModeBypass, worker.PermissionModeBypass},
		{"both empty → empty (CC maps to bypass)", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolvePermissionMode(tt.sessionMode, tt.operatorMode)
			require.Equal(t, tt.want, got)
		})
	}
}
