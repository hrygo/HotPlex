package worker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidatePermissionMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"", false}, // empty = "worker default" (valid; no explicit override)
		{PermissionModeReadOnly, false},
		{PermissionModeWorkspace, false},
		{PermissionModeAutoEdit, false},
		{PermissionModeBypass, false},
		{"plan", true},        // legacy value, no longer a valid tier
		{"auto-accept", true}, // legacy value
		{"default", true},     // legacy value
		{"bogus", true},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			t.Parallel()
			err := ValidatePermissionMode(tt.mode)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidPermissionMode)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNormalizePermissionMode(t *testing.T) {
	t.Parallel()
	// r3 (#804): empty maps to workspace (NOT bypass) so an operator who explicitly
	// sets default_permission_mode: "" lands on the tightened default — bypass would
	// be injected and override restricted worker configs (the r2 P1 escalation).
	require.Equal(t, PermissionModeWorkspace, NormalizePermissionMode(""))
	// Valid tiers pass through unchanged.
	require.Equal(t, PermissionModeReadOnly, NormalizePermissionMode(PermissionModeReadOnly))
	require.Equal(t, PermissionModeWorkspace, NormalizePermissionMode(PermissionModeWorkspace))
	require.Equal(t, PermissionModeAutoEdit, NormalizePermissionMode(PermissionModeAutoEdit))
	require.Equal(t, PermissionModeBypass, NormalizePermissionMode(PermissionModeBypass))
	// Unknown values pass through unchanged — ValidatePermissionMode is the gatekeeper.
	require.Equal(t, "weird", NormalizePermissionMode("weird"))
}
