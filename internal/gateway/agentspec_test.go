package gateway

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/worker"
)

// TestBuildWebChatInput_WSRESTEquivalence proves the F4 point at the gateway
// layer: the WS and REST entries share BuildWebChatInput, so semantically-equal
// requests map to equal Inputs, and the only first-cut divergence (AllowedTools:
// WS carries it, REST has no source) is contained to that single field.
func TestBuildWebChatInput_WSRESTEquivalence(t *testing.T) {
	t.Parallel()

	wsIn := BuildWebChatInput(worker.TypeClaudeCode, []string{"Bash"}, "u1", "ws1")
	restIn := BuildWebChatInput(worker.TypeClaudeCode, nil, "u1", "ws1")

	// Both are webchat-platform inputs.
	require.Equal(t, platformWebChat, wsIn.Platform)
	require.Equal(t, platformWebChat, restIn.Platform)

	// The ONLY divergence is AllowedTools (WS has it, REST nil).
	require.Equal(t, []string{"Bash"}, wsIn.InitMeta.AllowedTools)
	require.Nil(t, restIn.InitMeta.AllowedTools)

	// Zero out the divergent field → the two Inputs are identical.
	wsIn.InitMeta.AllowedTools = nil
	require.Equal(t, wsIn, restIn, "WS and REST inputs must be equal once the AllowedTools divergence is removed")
}

// TestBuildWebChatInput_Deterministic: equal requests produce equal Inputs (the
// foundation of WS≡REST AgentSpec equivalence via the pure Resolver).
func TestBuildWebChatInput_Deterministic(t *testing.T) {
	t.Parallel()
	a := BuildWebChatInput(worker.TypeCodexCLI, []string{"Read"}, "u1", "ws1")
	b := BuildWebChatInput(worker.TypeCodexCLI, []string{"Read"}, "u1", "ws1")
	require.Equal(t, a, b)
	require.Equal(t, "codex_cli", a.InitMeta.WorkerType)
	require.Equal(t, "u1", a.UserID)
	require.Equal(t, "ws1", a.WorkspaceID)
}

// TestShadowCompareStartParams_NilLogger: a nil logger must be a safe no-op.
func TestShadowCompareStartParams_NilLogger(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		ShadowCompareStartParams(nil,
			BuildWebChatInput(worker.TypeClaudeCode, nil, "u1", "ws1"),
			worker.SessionStartParams{WorkerType: worker.TypeClaudeCode})
	})
}

// TestShadowCompareStartParams_ResolveDivergence: when the resolver's boundary
// rejects a worker type the live path tolerates (here simulated by the empty
// test-binary registry), the shadow logs a quiet resolve divergence and never
// panics nor emits a field-mismatch Warn.
func TestShadowCompareStartParams_ResolveDivergence(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	require.NotPanics(t, func() {
		ShadowCompareStartParams(log,
			BuildWebChatInput(worker.WorkerType("not-registered-in-test"), nil, "u1", "ws1"),
			worker.SessionStartParams{WorkerType: worker.WorkerType("not-registered-in-test")})
	})

	out := buf.String()
	require.Contains(t, out, "resolve divergence", "expected a quiet resolve-divergence log")
	require.False(t, strings.Contains(out, "worker_type divergence"),
		"field-mismatch Warn must not fire on the resolve-divergence path")
}

// TestShadowResolvePlan_NilLogger: a nil logger must be a safe no-op.
func TestShadowResolvePlan_NilLogger(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		ShadowResolvePlan(nil, BuildWebChatInput(worker.TypeClaudeCode, nil, "u1", "ws1"))
	})
}

// TestShadowResolvePlan_Blocked: the empty test-binary registry rejects any
// non-empty worker type fail-closed; the shadow logs the bounded blocked codes
// (never a silent success) and never panics.
func TestShadowResolvePlan_Blocked(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	require.NotPanics(t, func() {
		ShadowResolvePlan(log,
			BuildWebChatInput(worker.WorkerType("not-registered-in-test"), nil, "u1", "ws1"))
	})

	out := buf.String()
	require.Contains(t, out, "runtime plan shadow: blocked")
	require.Contains(t, out, agentspec.BlockUnknownWorkerType)
}

// TestShadowResolvePlan_OkWithWarnings: an empty worker type resolves (the
// registry decides at dispatch) but the plan warns the desired state is
// unproven; the shadow logs the warning codes with the plan hash.
func TestShadowResolvePlan_OkWithWarnings(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	require.NotPanics(t, func() {
		ShadowResolvePlan(log, BuildWebChatInput(worker.WorkerType(""), nil, "u1", "ws1"))
	})

	out := buf.String()
	require.Contains(t, out, "runtime plan shadow: warnings")
	require.Contains(t, out, agentspec.WarnWorkerTypeUnresolved)
	require.Contains(t, out, "plan_hash")
	require.False(t, strings.Contains(out, "runtime plan shadow: blocked"),
		"an ok plan must not log the blocked path")
}
