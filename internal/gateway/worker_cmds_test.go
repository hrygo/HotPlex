package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

type permissionModeRecorder struct {
	body map[string]any
}

func (r *permissionModeRecorder) SendControlRequest(_ context.Context, subtype string, body map[string]any) (map[string]any, error) {
	if subtype == "set_permission_mode" {
		r.body = body
	}
	return map[string]any{}, nil
}

func TestNormalizeRequestedPermissionMode_EntryPointsMatch(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		args  string
		extra map[string]any
		want  string
	}{
		{"slash canonical", "workspace", nil, worker.PermissionModeWorkspace},
		{"slash native alias preserves case", "bypassPermissions", nil, worker.PermissionModeBypass},
		{"structured canonical", "", map[string]any{"mode": "auto-edit"}, worker.PermissionModeAutoEdit},
		{"structured native alias", "", map[string]any{"mode": "PLAN"}, worker.PermissionModeReadOnly},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeRequestedPermissionMode(tt.args, tt.extra)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeRequestedPermissionMode_RejectsUnknown(t *testing.T) {
	t.Parallel()
	for _, input := range []string{"", "unknown", "workspace-plus"} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := normalizeRequestedPermissionMode(input, nil)
			require.ErrorIs(t, err, worker.ErrInvalidPermissionMode)
		})
	}
}

func TestHandleSetPermMode_ForwardsCanonicalModeFromBothEntryPoints(t *testing.T) {
	t.Parallel()
	h := &Handler{}
	env := &events.Envelope{}

	for _, tt := range []struct {
		name  string
		args  string
		extra map[string]any
	}{
		{"slash", "bypassPermissions", nil},
		{"structured", "", map[string]any{"mode": "BYPASS"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := &permissionModeRecorder{}
			err := h.handleSetPermMode(t.Context(), env, recorder, tt.args, tt.extra)
			require.NoError(t, err)
			require.Equal(t, worker.PermissionModeBypass, recorder.body["mode"])
		})
	}
}

func TestClassifyWorkerError_PermissionModeUnsupported(t *testing.T) {
	t.Parallel()
	require.Equal(t, events.ErrCodeNotSupported, classifyWorkerError(worker.ErrNotImplemented))
	require.Equal(t, events.ErrCodeNotSupported, classifyWorkerError(worker.ErrSkillNotSupported))
}

func TestClassifyWorkerError_CapabilityRejectionMessages(t *testing.T) {
	t.Parallel()
	for _, message := range []string{
		"claudecode: rewind: control: request failed: File rewinding is not enabled.",
		"codexcli: set_model not supported",
		"acp: unsupported control request: compact",
	} {
		t.Run(message, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, events.ErrCodeNotSupported, classifyWorkerError(errors.New(message)))
		})
	}
	require.NotEqual(t, events.ErrCodeNotSupported, classifyWorkerError(errors.New("worker process not started")))
}
