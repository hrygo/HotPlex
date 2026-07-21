package worker

import (
	"errors"
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

func TestNormalizeRuntimePermissionMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"read-only", PermissionModeReadOnly},
		{"READONLY", PermissionModeReadOnly},
		{"plan", PermissionModeReadOnly},
		{"workspace", PermissionModeWorkspace},
		{"acceptEdits", PermissionModeWorkspace},
		{"accept-edits", PermissionModeWorkspace},
		{"default", PermissionModeWorkspace},
		{"auto-edit", PermissionModeAutoEdit},
		{"AUTO", PermissionModeAutoEdit},
		{"auto-accept", PermissionModeAutoEdit},
		{"bypass", PermissionModeBypass},
		{"bypassPermissions", PermissionModeBypass},
		{"dangerously-skip-permissions", PermissionModeBypass},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeRuntimePermissionMode(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}

	for _, input := range []string{"", "unknown", "plan-ish"} {
		t.Run("invalid_"+input, func(t *testing.T) {
			t.Parallel()
			_, err := NormalizeRuntimePermissionMode(input)
			require.ErrorIs(t, err, ErrInvalidPermissionMode)
		})
	}
}

func TestPermissionCeilingTransitionMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		ceiling   string
		requested string
		want      string
		wantErr   error
	}{
		{"read-only remains read-only", PermissionModeReadOnly, "plan", PermissionModeReadOnly, nil},
		{"read-only rejects workspace", PermissionModeReadOnly, PermissionModeWorkspace, "", ErrPermissionEscalation},
		{"read-only rejects auto-edit", PermissionModeReadOnly, PermissionModeAutoEdit, "", ErrPermissionEscalation},
		{"read-only rejects bypass", PermissionModeReadOnly, "bypassPermissions", "", ErrPermissionEscalation},
		{"workspace tightens to read-only", PermissionModeWorkspace, "PLAN", PermissionModeReadOnly, nil},
		{"workspace restores to ceiling", PermissionModeWorkspace, "acceptEdits", PermissionModeWorkspace, nil},
		{"workspace rejects auto-edit", PermissionModeWorkspace, "auto", "", ErrPermissionEscalation},
		{"workspace rejects bypass", PermissionModeWorkspace, PermissionModeBypass, "", ErrPermissionEscalation},
		{"bypass accepts every tier", PermissionModeBypass, PermissionModeAutoEdit, PermissionModeAutoEdit, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var ceiling PermissionCeiling
			require.NoError(t, ceiling.Capture(tt.ceiling))
			got, err := ceiling.Check(tt.requested)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPermissionCeilingIsImmutable(t *testing.T) {
	t.Parallel()
	var ceiling PermissionCeiling
	require.NoError(t, ceiling.Capture(PermissionModeWorkspace))
	require.NoError(t, ceiling.Capture(PermissionModeBypass))

	mode, ok := ceiling.Mode()
	require.True(t, ok)
	require.Equal(t, PermissionModeWorkspace, mode)

	_, err := ceiling.Check(PermissionModeAutoEdit)
	require.ErrorIs(t, err, ErrPermissionEscalation)
	require.False(t, errors.Is(err, ErrPermissionCeilingUnset))
}

func TestPermissionCeilingRejectsCheckBeforeCapture(t *testing.T) {
	t.Parallel()
	var ceiling PermissionCeiling
	_, err := ceiling.Check(PermissionModeReadOnly)
	require.ErrorIs(t, err, ErrPermissionCeilingUnset)
}
