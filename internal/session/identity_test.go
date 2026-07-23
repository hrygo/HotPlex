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

func identityDB(t *testing.T) *SQLiteStore {
	t.Helper()
	ctx := t.Context()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "identity.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	cfg.DB.WALMode = true
	store, err := NewSQLiteStore(ctx, cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func sampleIdentity() *agentspec.AgentIdentity {
	id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
		UserID: "u1", WorkspaceID: "ws1", BotID: "B1", BotName: "helper",
		Platform: "webchat", WorkerType: "claude_code",
	})
	return &id
}

// TestSessionStore_IdentityRoundTrip: a bound identity persists under
// context_json and restores into the typed field on Get.
func TestSessionStore_IdentityRoundTrip(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-id-rt", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Identity: sampleIdentity(),
	}))

	got, err := store.Get(ctx, "sess-id-rt")
	require.NoError(t, err)
	require.NotNil(t, got.Identity, "identity must round-trip")
	require.Equal(t, *sampleIdentity(), *got.Identity)
	// The reserved key is NOT left in Context (it lives in the typed field).
	require.NotContains(t, got.Context, agentspec.IdentityContextKey)
}

// TestSessionStore_IdentityLegacyBackwardsCompat: a session written without an
// identity (pre-#848 shape) remains readable, with Identity nil and no reserved
// key leaking into Context.
func TestSessionStore_IdentityLegacyBackwardsCompat(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-legacy", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
	}))

	got, err := store.Get(ctx, "sess-legacy")
	require.NoError(t, err)
	require.Nil(t, got.Identity, "legacy session has no bound identity")
	require.Empty(t, got.Context, "legacy session has no context")
}

// TestSessionStore_IdentityCoexistsWithContext: a real runtime context and the
// bound identity both round-trip independently; the reserved key is popped from
// the observable Context.
func TestSessionStore_IdentityCoexistsWithContext(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-ctx", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:  map[string]any{"turn": 3, "topic": "refactor"},
		Identity: sampleIdentity(),
	}))

	got, err := store.Get(ctx, "sess-ctx")
	require.NoError(t, err)
	require.NotNil(t, got.Identity)
	require.Equal(t, "refactor", got.Context["topic"], "real context survives")
	require.EqualValues(t, 3, got.Context["turn"], "real context survives (json → float64)")
	require.NotContains(t, got.Context, agentspec.IdentityContextKey)
}

// TestSessionStore_IdentitySurvivesContextReset: /reset clears Context but the
// bound identity re-persists on the next Upsert (identity is session-level).
func TestSessionStore_IdentitySurvivesContextReset(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	// Create with context + identity.
	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-reset", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:  map[string]any{"turn": 3},
		Identity: sampleIdentity(),
	}))
	// Reset: clear context, keep identity, re-upsert.
	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-reset", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:  map[string]any{}, // cleared
		Identity: sampleIdentity(),
	}))

	got, err := store.Get(ctx, "sess-reset")
	require.NoError(t, err)
	require.NotNil(t, got.Identity, "identity must survive a context reset")
	require.Empty(t, got.Context, "context was cleared")
}

// TestSessionStore_IdentitySecretFree: the persisted context_json blob never
// contains a credential, even with a populated identity.
func TestSessionStore_IdentitySecretFree(t *testing.T) {
	t.Parallel()
	store := identityDB(t)
	ctx := context.Background()
	now := time.Now()

	id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
		UserID: "u1", WorkspaceID: "ws1", BotID: "B1",
		Platform: "slack", Provider: "anthropic", WorkerType: "claude_code",
	})
	require.NoError(t, store.Upsert(ctx, &SessionInfo{
		ID: "sess-secret", UserID: "u1", WorkerType: "claude_code",
		State: events.StateCreated, CreatedAt: now, UpdatedAt: now,
		Context:  map[string]any{"note": "harmless"},
		Identity: &id,
	}))

	// Read the raw column to assert over the on-disk blob, not the typed view.
	var raw string
	require.NoError(t, store.db.QueryRowContext(ctx,
		`SELECT context_json FROM sessions WHERE id = ?`, "sess-secret").Scan(&raw))
	lower := strings.ToLower(raw)
	for _, secret := range []string{"token", "secret", "password", "credential", "sk-", "xox"} {
		require.NotContains(t, lower, secret, "context_json leaked a secret-shaped token: %q", secret)
	}
	require.Contains(t, raw, agentspec.IdentityContextKey, "identity must be folded into the blob")
}

// TestSessionInfo_EffectiveIdentity: a legacy SessionInfo (no bound Identity)
// derives an equivalent identity from its existing fields; the derived AgentID
// matches what BuildAgentIdentity produces from the same tuple.
func TestSessionInfo_EffectiveIdentity(t *testing.T) {
	t.Parallel()
	legacy := SessionInfo{
		UserID: "u1", WorkspaceID: "ws1", BotID: "B1", BotName: "helper",
		Platform: "webchat", WorkerType: "claude_code",
	}
	got := legacy.EffectiveIdentity()
	require.True(t, got.Anonymous == false)
	require.NotEmpty(t, got.AgentID)
	want := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
		UserID: "u1", WorkspaceID: "ws1", BotID: "B1", BotName: "helper",
		Platform: "webchat", WorkerType: "claude_code",
	})
	require.Equal(t, want.AgentID, got.AgentID, "legacy derivation must match a fresh build")

	// A bound identity short-circuits derivation.
	bound := agentspec.BuildAgentIdentity(agentspec.IdentityInput{UserID: "other", WorkerType: "codex_cli"})
	legacy.Identity = &bound
	require.Equal(t, bound, legacy.EffectiveIdentity(), "bound identity wins over derivation")
}

// TestSessionInfo_EffectiveIdentity_Anonymous: an unauthenticated session is
// marked anonymous but still derives a stable AgentID.
func TestSessionInfo_EffectiveIdentity_Anonymous(t *testing.T) {
	t.Parallel()
	anon := SessionInfo{WorkerType: "claude_code", Platform: "webchat"}.EffectiveIdentity()
	require.True(t, anon.Anonymous)
	require.NotEmpty(t, anon.AgentID)
}

// TestAuditDetailFields: the audit detail map carries the unified identity
// correlation keys under the SAME names as AEP metadata / trace attributes, so
// a single agent correlates across all three surfaces. Empty values are omitted.
func TestAuditDetailFields(t *testing.T) {
	t.Parallel()

	t.Run("workspace session emits all unified keys + bot_id", func(t *testing.T) {
		t.Parallel()
		id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
			UserID: "u1", WorkspaceID: "ws1", BotID: "B1",
			Platform: "slack", WorkerType: "claude_code",
		})
		d := AuditDetailFields("B1", "claude_code", id)
		require.Equal(t, id.AgentID, d[agentspec.MetadataKeyAgentID])
		require.Equal(t, "claude_code", d[agentspec.MetadataKeyWorkerType])
		require.Equal(t, "u1", d[agentspec.MetadataKeyUserID])
		require.Equal(t, "ws1", d[agentspec.MetadataKeyWorkspaceID])
		require.Equal(t, "slack", d[agentspec.MetadataKeyPlatform])
		require.Equal(t, "B1", d["bot_id"], "bot_id is an audit-specific key")
		require.NotContains(t, d, "anonymous")
	})

	t.Run("platform session omits empty workspace_id", func(t *testing.T) {
		t.Parallel()
		id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
			UserID: "u1", Platform: "slack", WorkerType: "claude_code",
		})
		d := AuditDetailFields("", "claude_code", id)
		require.Contains(t, d, agentspec.MetadataKeyAgentID)
		_, hasWS := d[agentspec.MetadataKeyWorkspaceID]
		require.False(t, hasWS)
		_, hasBot := d["bot_id"]
		require.False(t, hasBot, "empty bot_id is omitted")
	})

	t.Run("anonymous session carries the anonymous marker", func(t *testing.T) {
		t.Parallel()
		id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
			Platform: "webchat", WorkerType: "claude_code",
		})
		d := AuditDetailFields("", "claude_code", id)
		require.Equal(t, true, d["anonymous"])
		_, hasUser := d[agentspec.MetadataKeyUserID]
		require.False(t, hasUser, "anonymous session omits user_id")
	})

	t.Run("secret-free", func(t *testing.T) {
		t.Parallel()
		id := agentspec.BuildAgentIdentity(agentspec.IdentityInput{
			UserID: "u1", WorkspaceID: "ws1", Provider: "anthropic",
			Platform: "slack", WorkerType: "claude_code",
		})
		b, err := json.Marshal(AuditDetailFields("B1", "claude_code", id))
		require.NoError(t, err)
		lower := strings.ToLower(string(b))
		for _, secret := range []string{"token", "secret", "password", "credential", "sk-", "xox", "provider"} {
			require.NotContains(t, lower, secret, "audit detail leaked a secret-shaped token: %q", secret)
		}
	})
}

// TestBindIdentity: the helper always re-derives from the current fields, so a
// session first bound without a workspace gets a corrected identity once the
// workspace is late-bound (the WebChat path).
func TestBindIdentity(t *testing.T) {
	t.Parallel()
	info := &SessionInfo{UserID: "u1", BotName: "helper", WorkerType: worker.TypeClaudeCode, Platform: "webchat"}
	require.Nil(t, info.Identity, "no identity before binding")

	bindIdentity(info)
	require.NotNil(t, info.Identity)
	require.False(t, info.Identity.Anonymous)
	require.Equal(t, "helper", info.Identity.AgentName)
	require.Empty(t, info.Identity.WorkspaceID)
	firstID := info.Identity.AgentID
	require.NotEmpty(t, firstID)

	// Late-bind a workspace → identity re-derives and the AgentID changes.
	info.WorkspaceID = "ws1"
	bindIdentity(info)
	require.Equal(t, "ws1", info.Identity.WorkspaceID)
	want := agentspec.DeriveAgentID("u1", "ws1", "helper", "claude_code")
	require.Equal(t, want, info.Identity.AgentID)
	require.NotEqual(t, firstID, info.Identity.AgentID, "workspace binding must change the AgentID")

	// Anonymous marker is honored.
	info2 := &SessionInfo{WorkerType: worker.TypeCodexCLI, Platform: "webchat"}
	bindIdentity(info2)
	require.True(t, info2.Identity.Anonymous)
	require.NotEmpty(t, info2.Identity.AgentID)

	// nil-safe.
	bindIdentity(nil)
}

// TestManager_CreateBindsIdentity: CreateWithBot binds the AgentIdentity and the
// bound identity is PERSISTED under context_json — read back directly from the
// store (bypassing the in-memory cache) to prove it survives Upsert, not just
// that the returned pointer is populated.
func TestManager_CreateBindsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "mgr-identity.db")
	cfg.DB.SQLite.Path = cfg.DB.Path
	store, err := NewSQLiteStore(ctx, cfg, sqlutil.NewWriteMu(sqlutil.DialectSQLite))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	m, err := NewManager(ctx, nil, cfg, nil, store)
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	info, err := m.Create(ctx, "sess-mgr-id", "user1", worker.TypeClaudeCode, nil, "", "title")
	require.NoError(t, err)
	require.NotNil(t, info.Identity, "Create must bind an identity")
	require.Equal(t, "user1", info.Identity.UserID)
	require.Equal(t, "claude_code", info.Identity.WorkerType)
	require.False(t, info.Identity.Anonymous)
	require.NotEmpty(t, info.Identity.AgentID)

	// Read directly from the store (not the in-memory map) to prove persistence.
	got, err := store.Get(ctx, "sess-mgr-id")
	require.NoError(t, err)
	require.NotNil(t, got.Identity, "identity must be persisted under context_json")
	require.Equal(t, info.Identity.AgentID, got.Identity.AgentID)
	require.NotContains(t, got.Context, agentspec.IdentityContextKey, "reserved key must not leak into Context")
}

func TestManager_CreateWithBotBindsWorkspaceBeforeIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := identityDB(t)
	m, err := NewManager(ctx, nil, config.Default(), nil, store)
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	info, err := m.CreateWithBot(
		ctx, "sess-workspace-id", "user1", "bot1", "helper",
		worker.TypeClaudeCode, nil, "webchat", nil, "ws1", t.TempDir(), "title", "client1",
	)
	require.NoError(t, err)
	require.Equal(t, "ws1", info.WorkspaceID)
	require.NotNil(t, info.Identity)
	require.Equal(t, "ws1", info.Identity.WorkspaceID)
	require.Equal(t,
		agentspec.DeriveAgentID("user1", "ws1", "helper", string(worker.TypeClaudeCode)),
		info.Identity.AgentID,
	)

	persisted, err := store.Get(ctx, info.ID)
	require.NoError(t, err)
	require.Equal(t, info.Identity.AgentID, persisted.EffectiveIdentity().AgentID,
		"create audit and later runtime surfaces derive from the same persisted identity")
}
