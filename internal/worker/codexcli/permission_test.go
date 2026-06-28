package codexcli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestPermissionModeFromSession(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode     string
		sandbox  string
		approval string
		ok       bool
	}{
		{worker.PermissionModeReadOnly, "read-only", "untrusted", true},
		{worker.PermissionModeWorkspace, "workspace-write", "on-request", true},
		{worker.PermissionModeAutoEdit, "workspace-write", "never", true},
		{worker.PermissionModeBypass, "danger-full-access", "never", true},
		{"", "", "", false}, // empty → caller falls back to config defaults (YOLO)
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			sb, ap, ok := permissionModeFromSession(tt.mode)
			require.Equal(t, tt.sandbox, sb)
			require.Equal(t, tt.approval, ap)
			require.Equal(t, tt.ok, ok)
		})
	}
}
