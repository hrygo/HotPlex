package agentspec

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func sampleSpec() AgentSpec {
	return AgentSpec{
		Worker: WorkerSpec{Type: "claude_code", Model: "claude-sonnet-5"},
		Policy: PolicySpec{
			PermissionMode:  "workspace",
			AllowedTools:    []string{"git", "grep"},
			DisallowedTools: []string{"shell:rm"},
		},
		Sandbox: SandboxSpec{Mode: "workspace-write", AllowedDirs: []string{"/tmp"}},
		Budget:  BudgetSpec{MaxTurns: 20, MaxBudgetUSD: 5.0},
	}
}

// TestSnapshotFromSpec: the constructor projects every effective field, stamps
// the schema version, and computes a non-empty hash.
func TestSnapshotFromSpec(t *testing.T) {
	t.Parallel()
	s := SnapshotFromSpec(sampleSpec())
	require.Equal(t, SnapshotVersion, s.Version)
	require.Equal(t, "claude_code", s.WorkerType)
	require.Equal(t, "claude-sonnet-5", s.Model)
	require.Equal(t, "workspace", s.PermissionMode)
	require.Equal(t, []string{"git", "grep"}, s.AllowedTools)
	require.Equal(t, []string{"shell:rm"}, s.DisallowedTools)
	require.Equal(t, "workspace-write", s.SandboxMode)
	require.Equal(t, []string{"/tmp"}, s.AllowedDirs)
	require.Equal(t, 20, s.MaxTurns)
	require.EqualValues(t, 5.0, s.MaxBudgetUSD)
	require.NotEmpty(t, s.Hash, "hash must be computed")
	require.Len(t, s.Hash, 16, "hash is the first 16 hex chars")
}

// TestSnapshotHashStable: identical effective policies hash identically (so
// audit/metadata correlate sessions with the same spec), and any policy change
// changes the hash.
func TestSnapshotHashStable(t *testing.T) {
	t.Parallel()
	a := SnapshotFromSpec(sampleSpec())
	b := SnapshotFromSpec(sampleSpec())
	require.Equal(t, a.Hash, b.Hash, "same spec → same hash")

	changed := sampleSpec()
	changed.Policy.AllowedTools = []string{"git", "grep", "edit"}
	require.NotEqual(t, a.Hash, SnapshotFromSpec(changed).Hash, "different tools → different hash")

	changedMode := sampleSpec()
	changedMode.Policy.PermissionMode = "auto-edit"
	require.NotEqual(t, a.Hash, SnapshotFromSpec(changedMode).Hash, "different permission mode → different hash")

	// nil vs empty slice hash identically (both = "not set"); the omitempty body
	// is the canonical form.
	emptySpec := AgentSpec{Worker: WorkerSpec{Type: "claude_code"}}
	nilSpec := AgentSpec{Worker: WorkerSpec{Type: "claude_code"}, Policy: PolicySpec{AllowedTools: nil}}
	require.Equal(t, SnapshotFromSpec(emptySpec).Hash, SnapshotFromSpec(nilSpec).Hash)
}

// TestSnapshotFromSpec_OwnsSlices: the snapshot's slices are defensive copies,
// so mutating the source AgentSpec after construction cannot corrupt the
// persisted snapshot.
func TestSnapshotFromSpec_OwnsSlices(t *testing.T) {
	t.Parallel()
	spec := sampleSpec()
	s := SnapshotFromSpec(spec)
	spec.Policy.AllowedTools[0] = "MUTATED"
	spec.Policy.AllowedTools = append(spec.Policy.AllowedTools, "extra")
	require.Equal(t, []string{"git", "grep"}, s.AllowedTools, "snapshot slices are independent of the source")
}

// TestSnapshotSecretFree: the persisted/serialized form never contains a
// credential-shaped token, even with a populated policy.
func TestSnapshotSecretFree(t *testing.T) {
	t.Parallel()
	s := SnapshotFromSpec(sampleSpec())
	b, err := json.Marshal(s)
	require.NoError(t, err)
	lower := strings.ToLower(string(b))
	for _, secret := range []string{"token", "secret", "password", "credential", "sk-", "xox", "apikey"} {
		require.NotContains(t, lower, secret, "snapshot leaked a secret-shaped token: %q", secret)
	}
}

// TestMergeExtractSnapshot: the snapshot folds into a context map and round-trips
// back out, with the reserved key popped so callers see only real context.
func TestMergeExtractSnapshot(t *testing.T) {
	t.Parallel()
	snap := SnapshotFromSpec(sampleSpec())

	t.Run("round-trip pops reserved key", func(t *testing.T) {
		t.Parallel()
		ctx := MergeSnapshotIntoContext(map[string]any{"turn": 3}, &snap)
		require.Contains(t, ctx, SnapshotContextKey)
		got, err := ExtractSnapshotFromContext(ctx)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, snap, *got)
		require.NotContains(t, ctx, SnapshotContextKey, "reserved key popped after extract")
		require.EqualValues(t, 3, ctx["turn"], "real context survives")
	})

	t.Run("nil snapshot is a no-op", func(t *testing.T) {
		t.Parallel()
		ctx := map[string]any{"turn": 3}
		// nil snapshot returns ctx unchanged (same reference, no copy, no new key).
		out := MergeSnapshotIntoContext(ctx, nil)
		require.Len(t, out, 1)
		require.EqualValues(t, 3, out["turn"])
		require.NotContains(t, out, SnapshotContextKey)
		got, err := ExtractSnapshotFromContext(ctx)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("nil ctx with snapshot yields a single-key map", func(t *testing.T) {
		t.Parallel()
		ctx := MergeSnapshotIntoContext(nil, &snap)
		require.Len(t, ctx, 1)
		require.Contains(t, ctx, SnapshotContextKey)
	})

	t.Run("absent key returns nil (legacy)", func(t *testing.T) {
		t.Parallel()
		for _, ctx := range []map[string]any{{"turn": 3}, {}, nil} {
			got, err := ExtractSnapshotFromContext(ctx)
			require.NoError(t, err)
			require.Nil(t, got)
		}
	})
}

func TestEffectiveAgentSpecSnapshot_Validate(t *testing.T) {
	t.Parallel()

	valid := SnapshotFromSpec(sampleSpec())
	require.NoError(t, valid.Validate())

	unknown := valid
	unknown.Version++
	require.ErrorIs(t, unknown.Validate(), ErrInvalidSnapshot)

	tampered := valid
	tampered.AllowedTools = append(tampered.AllowedTools, "MUTATED")
	require.ErrorIs(t, tampered.Validate(), ErrInvalidSnapshot)

	missingHash := valid
	missingHash.Hash = ""
	require.ErrorIs(t, missingHash.Validate(), ErrInvalidSnapshot)
}

// TestRestoreAllowedTools: the restore helper returns the whitelist (or nil for
// "no restriction"), always as a defensive copy.
func TestRestoreAllowedTools(t *testing.T) {
	t.Parallel()
	snap := SnapshotFromSpec(sampleSpec())
	tools := snap.RestoreAllowedTools()
	require.Equal(t, []string{"git", "grep"}, tools)
	tools[0] = "MUTATED"
	require.Equal(t, []string{"git", "grep"}, snap.AllowedTools, "restore returns a copy")

	none := SnapshotFromSpec(AgentSpec{Worker: WorkerSpec{Type: "claude_code"}})
	require.Nil(t, none.RestoreAllowedTools(), "no whitelist → nil (no restriction)")
	require.Nil(t, (*EffectiveAgentSpecSnapshot)(nil).RestoreAllowedTools(), "nil-safe")
}
