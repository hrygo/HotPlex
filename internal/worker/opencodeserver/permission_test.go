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
