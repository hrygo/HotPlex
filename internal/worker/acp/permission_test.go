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
