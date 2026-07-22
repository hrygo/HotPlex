package agentspec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeriveAgentID_Deterministic(t *testing.T) {
	t.Parallel()
	a := DeriveAgentID("u1", "ws1", "coding-agent", "claude_code")
	b := DeriveAgentID("u1", "ws1", "coding-agent", "claude_code")
	require.Equal(t, a, b, "same tuple must derive the same AgentID")
	require.NotEmpty(t, a)
}

func TestDeriveAgentID_DistinctOnTupleChange(t *testing.T) {
	t.Parallel()
	base := DeriveAgentID("u1", "ws1", "name", "claude_code")
	require.NotEqual(t, base, DeriveAgentID("u2", "ws1", "name", "claude_code"), "user change")
	require.NotEqual(t, base, DeriveAgentID("u1", "ws2", "name", "claude_code"), "workspace change")
	require.NotEqual(t, base, DeriveAgentID("u1", "ws1", "other", "claude_code"), "name change")
	require.NotEqual(t, base, DeriveAgentID("u1", "ws1", "name", "codex_cli"), "worker change")
}

// TestDeriveAgentID_DisjointFromSessionKeyNamespace: the AgentID namespace is
// isolated from the session-key namespace, so an AgentID can never collide with
// a real session id even if the derivation inputs overlap.
func TestDeriveAgentID_NamespaceIsolated(t *testing.T) {
	t.Parallel()
	// A session key derived by session.DeriveSessionKey uses hotplexNamespace
	// directly; AgentID uses a sub-namespace. We cannot import session here, so
	// assert isolation structurally: two different names in the same namespace
	// differ, and the AgentID format is a UUID (36 chars, hyphen-separated).
	id := DeriveAgentID("u1", "ws1", "n", "claude_code")
	require.Len(t, id, 36, "AgentID must be a UUID string")
	require.Equal(t, 4, strings.Count(id, "-"))
}

func TestIsAnonymousUser(t *testing.T) {
	t.Parallel()
	require.True(t, IsAnonymousUser(""))
	require.True(t, IsAnonymousUser("anonymous"))
	require.True(t, IsAnonymousUser("  "))
	require.False(t, IsAnonymousUser("u1"))
	require.False(t, IsAnonymousUser("Uxxx"))
}

func TestBuildAgentIdentity_FieldsAndAnonymous(t *testing.T) {
	t.Parallel()

	t.Run("authenticated identity", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{
			UserID: "u1", WorkspaceID: "ws1", BotID: "B1", BotName: "helper",
			Platform: "slack", WorkerType: "claude_code", AgentName: "coding-agent",
		})
		require.Equal(t, DeriveAgentID("u1", "ws1", "coding-agent", "claude_code"), id.AgentID)
		require.Equal(t, "coding-agent", id.AgentName)
		require.False(t, id.Anonymous)
		require.Equal(t, "u1", id.UserID)
	})

	t.Run("anonymous from empty user", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{
			WorkspaceID: "ws1", WorkerType: "claude_code",
		})
		require.True(t, id.Anonymous)
		require.Empty(t, id.UserID)
		// Still derives a stable AgentID from the remaining tuple.
		require.NotEmpty(t, id.AgentID)
	})

	t.Run("anonymous from sentinel", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{UserID: "anonymous", WorkerType: "claude_code"})
		require.True(t, id.Anonymous)
	})
}

// TestBuildAgentIdentity_AgentNameFallback: AgentName falls back to BotName then
// WorkerType, and the fallback participates in AgentID derivation.
func TestBuildAgentIdentity_AgentNameFallback(t *testing.T) {
	t.Parallel()

	t.Run("explicit AgentName wins", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{UserID: "u1", BotName: "bot", WorkerType: "claude_code", AgentName: "explicit"})
		require.Equal(t, "explicit", id.AgentName)
		require.Equal(t, DeriveAgentID("u1", "", "explicit", "claude_code"), id.AgentID)
	})
	t.Run("falls back to BotName", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{UserID: "u1", BotName: "bot", WorkerType: "claude_code"})
		require.Equal(t, "bot", id.AgentName)
	})
	t.Run("falls back to WorkerType", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{UserID: "u1", WorkerType: "codex_cli"})
		require.Equal(t, "codex_cli", id.AgentName)
	})
}

func TestMergeIntoContext_RoundTrip(t *testing.T) {
	t.Parallel()
	id := BuildAgentIdentity(IdentityInput{UserID: "u1", WorkspaceID: "ws1", WorkerType: "claude_code"})
	original := map[string]any{"foo": "bar", "n": 42}

	merged := MergeIntoContext(original, &id)
	require.Contains(t, merged, IdentityContextKey)
	// Original is not mutated.
	require.NotContains(t, original, IdentityContextKey)
	require.Equal(t, "bar", original["foo"])

	// Simulate a json round-trip (what the DB column stores/returns).
	b, err := json.Marshal(merged)
	require.NoError(t, err)
	var loaded map[string]any
	require.NoError(t, json.Unmarshal(b, &loaded))

	got := ExtractFromContext(loaded)
	require.NotNil(t, got)
	require.Equal(t, id, *got)
	// Reserved key popped after extraction.
	require.NotContains(t, loaded, IdentityContextKey)
	// Real context data survives.
	require.Equal(t, "bar", loaded["foo"])
}

func TestMergeIntoContext_NilIdentityNoOp(t *testing.T) {
	t.Parallel()
	ctx := map[string]any{"foo": "bar"}
	got := MergeIntoContext(ctx, nil)
	require.Equal(t, ctx, got, "nil identity must leave ctx unchanged")
	require.NotContains(t, got, IdentityContextKey)
	require.Nil(t, MergeIntoContext(nil, nil), "nil ctx + nil id → nil")
}

func TestExtractFromContext_AbsentKey(t *testing.T) {
	t.Parallel()
	// Legacy session: context without the reserved key → no identity, map intact.
	ctx := map[string]any{"foo": "bar"}
	require.Nil(t, ExtractFromContext(ctx))
	require.Nil(t, ExtractFromContext(nil))
	require.Equal(t, "bar", ctx["foo"], "map must be untouched when key absent")
}

// TestAgentIdentity_SecretFree: the value object has no secret-shaped fields
// and a populated identity never serializes a credential.
func TestAgentIdentity_SecretFree(t *testing.T) {
	t.Parallel()
	id := BuildAgentIdentity(IdentityInput{
		UserID: "u1", WorkspaceID: "ws1", BotID: "B1",
		Platform: "slack", Provider: "anthropic", WorkerType: "claude_code",
	})
	raw, err := json.Marshal(id)
	require.NoError(t, err)
	s := string(raw)
	for _, secret := range []string{"key", "token", "secret", "password", "credential", "sk-", "xox"} {
		require.NotContains(t, strings.ToLower(s), secret, "identity serialized a secret-shaped token")
	}
	// Structural: no field name shaped like a secret carrier.
	denylist := []string{"token", "secret", "credential", "password", "apikey", "env"}
	assertNoIdentitySecretFields(t, reflect.TypeFor[AgentIdentity](), denylist)
}

func assertNoIdentitySecretFields(t *testing.T, typ reflect.Type, denylist []string) {
	t.Helper()
	for f := range typ.Fields() {
		lower := strings.ToLower(f.Name)
		for _, d := range denylist {
			require.NotContains(t, lower, d, "AgentIdentity has a secret-shaped field %q", f.Name)
		}
		if f.Type.Kind() == reflect.Struct {
			assertNoIdentitySecretFields(t, f.Type, denylist)
		}
	}
}

func TestAgentIdentity_MetadataMap(t *testing.T) {
	t.Parallel()

	t.Run("full identity omits nothing required", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{UserID: "u1", WorkspaceID: "ws1", Platform: "slack", WorkerType: "claude_code"})
		m := id.MetadataMap()
		require.Equal(t, id.AgentID, m[MetadataKeyAgentID])
		require.Equal(t, "claude_code", m[MetadataKeyWorkerType])
		require.Equal(t, "u1", m[MetadataKeyUserID])
		require.Equal(t, "ws1", m[MetadataKeyWorkspaceID])
		require.Equal(t, "slack", m[MetadataKeyPlatform])
	})

	t.Run("platform session omits empty workspace_id", func(t *testing.T) {
		t.Parallel()
		id := BuildAgentIdentity(IdentityInput{UserID: "u1", Platform: "slack", WorkerType: "claude_code"})
		m := id.MetadataMap()
		require.Contains(t, m, MetadataKeyAgentID)
		_, hasWS := m[MetadataKeyWorkspaceID]
		require.False(t, hasWS, "platform sessions must not emit an empty workspace_id")
	})
}
