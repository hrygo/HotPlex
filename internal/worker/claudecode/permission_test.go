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
		{"", "", true},     // empty → bypass (bridge normalizes beforehand)
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
