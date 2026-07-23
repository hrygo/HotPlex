package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// snapshotDBAt opens a SQLiteStore at the given path (so a test can reopen the
// same file to simulate a gateway restart). Mirrors identityDB but parameterized.
func snapshotDBAt(t *testing.T, path string) *SQLiteStore {
	t.Helper()
	ctx := t.Context()
	cfg := config.Default()
	cfg.DB.Path = path
	cfg.DB.SQLite.Path = path
	cfg.DB.WALMode = true
	store, err := NewSQLiteStore(ctx, cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sampleSnapshot() *agentspec.EffectiveAgentSpecSnapshot {
	s := agentspec.SnapshotFromSpec(agentspec.AgentSpec{
		Worker: agentspec.WorkerSpec{Type: "claude_code"},
		Policy: agentspec.PolicySpec{PermissionMode: "workspace", AllowedTools: []string{"git", "grep"}},
	})
	return &s
}

// TestSessionStore_SpecSnapshotRoundTrip: a bound snapshot persists under
// context_json, restores into the typed field on Get, AND repopulates the
// otherwise in-memory-only AllowedTools (the named #866 persistence gap).
// Store-level tests set SpecSnapshot explicitly to exercise the persistence
// mechanism; the Manager.Create binding is covered separately.
func TestSessionStore_SpecSnapshotRoundTrip(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-snap-rt", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		SpecSnapshot: sampleSnapshot(),
	}))

	got, err := store.Get(ctx, "sess-snap-rt")
	require.NoError(t, err)
	require.NotNil(t, got.SpecSnapshot, "snapshot must round-trip")
	require.Equal(t, *sampleSnapshot(), *got.SpecSnapshot)
	require.Equal(t, []string{"git", "grep"}, got.AllowedTools, "AllowedTools restored from snapshot")
	require.NotContains(t, got.Context, agentspec.SnapshotContextKey, "reserved key must not leak into Context")
}

func TestSessionStore_InvalidSpecSnapshotFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "invalid inner shape",
			raw:  `{"_agent_spec":{"v":1,"worker_type":"claude_code","allowed_tools":"Read","hash":"bad"}}`,
		},
		{
			name: "unknown version",
			raw:  `{"_agent_spec":{"v":2,"worker_type":"claude_code","allowed_tools":["Read"],"hash":"bad"}}`,
		},
		{
			name: "hash mismatch",
			raw:  `{"_agent_spec":{"v":1,"worker_type":"claude_code","allowed_tools":["Bash"],"hash":"0000000000000000"}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := identityDB(t)
			ctx := context.Background()
			now := time.Now()
			id := "sess-invalid-snapshot-" + strings.ReplaceAll(tc.name, " ", "-")

			require.NoError(t, store.Upsert(ctx, &SessionInfo{
				ID: id, UserID: "u1", WorkerType: worker.TypeClaudeCode,
				State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
			}))
			_, err := store.db.ExecContext(ctx,
				`UPDATE sessions SET context_json = ? WHERE id = ?`, tc.raw, id)
			require.NoError(t, err)

			_, err = store.Get(ctx, id)
			require.Error(t, err,
				"an invalid persisted policy must not be treated as an unrestricted legacy session")
		})
	}
}

func TestSQLiteStore_UpdateSpecSnapshotPreservesOtherFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := identityDB(t)
	now := time.Now()
	identity := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
		UserID: "u1", WorkspaceID: "ws1", WorkerType: string(worker.TypeClaudeCode),
	})
	stale := &SessionInfo{
		ID: "sess-targeted-snapshot", UserID: "u1", WorkerType: worker.TypeClaudeCode,
		WorkspaceID: "ws1", State: events.StateRunning, Title: "keep-title",
		Context: map[string]any{"keep": "value"}, Identity: &identity,
		CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, store.Upsert(ctx, stale))

	snapshot := agentspec.SnapshotFromSpec(agentspec.AgentSpec{
		Worker: agentspec.WorkerSpec{Type: string(worker.TypeClaudeCode)},
		Policy: agentspec.PolicySpec{
			PermissionMode: worker.PermissionModeWorkspace,
			AllowedTools:   []string{"Read"},
		},
	})
	require.NoError(t, store.UpdateSpecSnapshot(ctx, "sess-targeted-snapshot", &snapshot))

	// Simulate a lifecycle write that captured SessionInfo before the targeted
	// snapshot update. Its full-row Upsert must advance state/context without
	// restoring the stale nil snapshot over the database's current contract.
	stale.State = events.StateIdle
	stale.Context = map[string]any{"keep": "updated"}
	require.NoError(t, store.Upsert(ctx, stale))

	got, err := store.Get(ctx, "sess-targeted-snapshot")
	require.NoError(t, err)
	require.Equal(t, events.StateIdle, got.State)
	require.Equal(t, "keep-title", got.Title)
	require.Equal(t, "updated", got.Context["keep"])
	require.NotNil(t, got.Identity)
	require.Equal(t, identity.AgentID, got.Identity.AgentID)
	require.NotNil(t, got.SpecSnapshot)
	require.Equal(t, snapshot.Hash, got.SpecSnapshot.Hash)
}

// TestSessionStore_SpecSnapshotLegacyBackwardsCompat: a session with no tool
// whitelist (the common case, and all pre-#866 rows) binds no snapshot, remains
// fully readable, and AllowedTools stays empty (= no restriction).
func TestSessionStore_SpecSnapshotLegacyBackwardsCompat(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-snap-legacy", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
	}))

	got, err := store.Get(ctx, "sess-snap-legacy")
	require.NoError(t, err)
	require.Nil(t, got.SpecSnapshot, "no whitelist → no snapshot")
	require.Empty(t, got.AllowedTools)
	require.Empty(t, got.Context, "no reserved key, no context bloat")
}

// TestSessionStore_SpecSnapshotSecretFree: the persisted context_json blob never
// contains a credential, even with a populated snapshot.
func TestSessionStore_SpecSnapshotSecretFree(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-snap-secret", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:      map[string]any{"note": "harmless"},
		SpecSnapshot: sampleSnapshot(),
	}))

	var raw string
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT context_json FROM sessions WHERE id = ?`, "sess-snap-secret").Scan(&raw))
	lower := strings.ToLower(raw)
	for _, secret := range []string{"token", "secret", "password", "credential", "sk-", "xox", "apikey"} {
		require.NotContains(t, lower, secret, "context_json leaked a secret-shaped token: %q", secret)
	}
	require.Contains(t, raw, agentspec.SnapshotContextKey, "snapshot must be folded into the blob")
}

// TestSessionStore_SpecSnapshotSurvivesContextReset: /reset clears Context but the
// snapshot re-persists on the next Upsert (the policy is session-level, like identity).
func TestSessionStore_SpecSnapshotSurvivesContextReset(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-snap-reset", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:      map[string]any{"turn": 3},
		SpecSnapshot: sampleSnapshot(),
	}))
	// After a snapshot is bound on the first Upsert, subsequent Upserts must keep
	// carrying it. Simulate the post-/reset path: caller re-reads, clears Context,
	// but the in-memory info still carries SpecSnapshot and re-persists it.
	got, err := store.Get(ctx, "sess-snap-reset")
	require.NoError(t, err)
	got.Context = map[string]any{} // /reset clears runtime context
	require.NoError(t, store.Upsert(ctx, got))

	got2, err := store.Get(ctx, "sess-snap-reset")
	require.NoError(t, err)
	require.NotNil(t, got2.SpecSnapshot, "snapshot must survive a context reset")
	require.Empty(t, got2.Context, "context was cleared")
	require.Equal(t, []string{"git", "grep"}, got2.AllowedTools)
}

// TestSessionStore_SpecSnapshotCoexistsWithIdentityAndContext: identity, snapshot,
// and real runtime context all round-trip independently under their own reserved
// keys, none leaking into the observable Context.
func TestSessionStore_SpecSnapshotCoexistsWithIdentityAndContext(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-snap-coexist", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:      map[string]any{"topic": "refactor"},
		Identity:     sampleIdentity(),
		SpecSnapshot: sampleSnapshot(),
	}))

	got, err := store.Get(ctx, "sess-snap-coexist")
	require.NoError(t, err)
	require.NotNil(t, got.Identity, "identity survives")
	require.NotNil(t, got.SpecSnapshot, "snapshot survives")
	require.Equal(t, "refactor", got.Context["topic"], "real context survives")
	require.Equal(t, []string{"git", "grep"}, got.AllowedTools)
	require.NotContains(t, got.Context, agentspec.IdentityContextKey)
	require.NotContains(t, got.Context, agentspec.SnapshotContextKey)
}

// TestSessionStore_SpecSnapshotRestoreAcrossRestart: the canonical resume test
// (#866 AC1/AC2). A session created with an effective tool whitelist is read
// back from a FRESH store instance on the same DB file (simulating a gateway
// restart). The whitelist is restored from the persisted snapshot — not lost,
// and not re-derived from any (potentially drifted) config.
func TestSessionStore_SpecSnapshotRestoreAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now()
	path := filepath.Join(t.TempDir(), "restart.db")

	// Phase 1: create + persist with an effective tool whitelist.
	restartSnap := agentspec.SnapshotFromSpec(agentspec.AgentSpec{
		Worker: agentspec.WorkerSpec{Type: "claude_code"},
		Policy: agentspec.PolicySpec{AllowedTools: []string{"git", "grep", "edit"}},
	})
	s1 := snapshotDBAt(t, path)
	require.NoError(t, s1.Upsert(ctx, &SessionInfo{
		ID: "sess-restart", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		SpecSnapshot: &restartSnap,
	}))
	// Close the first store so the second opens a genuinely independent handle.
	require.NoError(t, s1.Close())

	// Phase 2: a fresh store on the same file = gateway restarted.
	s2 := snapshotDBAt(t, path)
	got, err := s2.Get(ctx, "sess-restart")
	require.NoError(t, err)
	require.NotNil(t, got.SpecSnapshot, "snapshot survived restart")
	require.Equal(t, []string{"git", "grep", "edit"}, got.AllowedTools,
		"AllowedTools restored from snapshot across restart (AC1)")
}

// TestManager_CreateBindsSpecSnapshot: CreateWithBot binds the snapshot when an
// effective tool whitelist is supplied, and it is PERSISTED — read back directly
// from the store (bypassing the in-memory map).
func TestManager_CreateBindsSpecSnapshot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "mgr-snap.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	store, err := NewSQLiteStore(ctx, cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	m, err := NewManager(ctx, nil, cfg, nil, store)
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	info, err := m.Create(ctx, "sess-mgr-snap", "user1", worker.TypeClaudeCode, []string{"git", "grep"}, "", "title")
	require.NoError(t, err)
	require.NotNil(t, info.SpecSnapshot, "Create must bind a snapshot when tools are supplied")
	require.Equal(t, []string{"git", "grep"}, info.SpecSnapshot.AllowedTools)
	require.NotEmpty(t, info.SpecSnapshot.Hash)

	// Read directly from the store to prove persistence under context_json.
	got, err := store.Get(ctx, "sess-mgr-snap")
	require.NoError(t, err)
	require.NotNil(t, got.SpecSnapshot, "snapshot persisted under context_json")
	require.Equal(t, info.SpecSnapshot.Hash, got.SpecSnapshot.Hash)
	require.Equal(t, []string{"git", "grep"}, got.AllowedTools)
	require.NotContains(t, got.Context, agentspec.SnapshotContextKey)

	// A tool-unrestricted session binds NO snapshot (no bloat, legacy-equivalent).
	info2, err := m.Create(ctx, "sess-mgr-nosnap", "user1", worker.TypeClaudeCode, nil, "", "title")
	require.NoError(t, err)
	require.Nil(t, info2.SpecSnapshot, "no whitelist → no snapshot")
}

// TestBindSpecSnapshot: the helper binds only when there is an effective tool
// whitelist, captures the permission ceiling when known, and is nil-safe.
func TestBindSpecSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("binds with whitelist + captures ceiling", func(t *testing.T) {
		t.Parallel()
		info := &SessionInfo{WorkerType: worker.TypeClaudeCode, AllowedTools: []string{"git"}, PermissionCeiling: "workspace"}
		require.Nil(t, info.SpecSnapshot)
		bindSpecSnapshot(info)
		require.NotNil(t, info.SpecSnapshot)
		require.Equal(t, "claude_code", info.SpecSnapshot.WorkerType)
		require.Equal(t, "workspace", info.SpecSnapshot.PermissionMode)
		require.Equal(t, []string{"git"}, info.SpecSnapshot.AllowedTools)
		require.NotEmpty(t, info.SpecSnapshot.Hash)
	})

	t.Run("no whitelist → no snapshot", func(t *testing.T) {
		t.Parallel()
		info := &SessionInfo{WorkerType: worker.TypeClaudeCode}
		bindSpecSnapshot(info)
		require.Nil(t, info.SpecSnapshot)
	})

	t.Run("nil-safe", func(t *testing.T) {
		t.Parallel()
		bindSpecSnapshot(nil) // must not panic
	})

	t.Run("owns its slice", func(t *testing.T) {
		t.Parallel()
		tools := []string{"git"}
		info := &SessionInfo{WorkerType: worker.TypeClaudeCode, AllowedTools: tools}
		bindSpecSnapshot(info)
		tools[0] = "MUTATED"
		require.Equal(t, []string{"git"}, info.SpecSnapshot.AllowedTools, "snapshot owns a copy")
	})
}

// TestAuditDetailFields_SpecSnapshot: the audit detail map carries the effective-
// spec fingerprint (spec_version/spec_hash) when a snapshot is bound, and omits
// it when none exists (legacy/anonymous sessions stay minimal).
func TestAuditDetailFields_SpecSnapshot(t *testing.T) {
	t.Parallel()
	id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
		UserID: "u1", Platform: "webchat", WorkerType: "claude_code",
	})

	t.Run("snapshot stamps version + hash", func(t *testing.T) {
		t.Parallel()
		d := AuditDetailFields("B1", "claude_code", id)
		sampleSnapshot().StampMetadata(d)
		require.Equal(t, agentspec.SnapshotVersion, d[agentspec.MetadataKeySpecVersion])
		require.Equal(t, sampleSnapshot().Hash, d[agentspec.MetadataKeySpecHash])
	})

	t.Run("no snapshot → no spec keys", func(t *testing.T) {
		t.Parallel()
		d := AuditDetailFields("B1", "claude_code", id)
		(*agentspec.EffectiveAgentSpecSnapshot)(nil).StampMetadata(d)
		_, hasVer := d[agentspec.MetadataKeySpecVersion]
		require.False(t, hasVer)
		_, hasHash := d[agentspec.MetadataKeySpecHash]
		require.False(t, hasHash)
	})

	t.Run("secret-free", func(t *testing.T) {
		t.Parallel()
		d := AuditDetailFields("B1", "claude_code", id)
		sampleSnapshot().StampMetadata(d)
		b, err := json.Marshal(d)
		require.NoError(t, err)
		lower := strings.ToLower(string(b))
		for _, secret := range []string{"token", "secret", "password", "credential", "sk-", "xox", "apikey"} {
			require.NotContains(t, lower, secret, "audit detail leaked a secret-shaped token: %q", secret)
		}
	})
}
