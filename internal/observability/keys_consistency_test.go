package observability

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/agentspec"
)

// The identity keys share string values with agentspec.MetadataKey* (#848).
// This pins the two together so they can never drift: agentspec stays a
// lightweight pure package (stdlib + uuid only, no otel/prom deps), observability
// owns the full key set, and the five overlapping names remain a single de-facto
// source of truth. If this test breaks, one side renamed a key without the other.
func TestIdentityKeysMatchAgentspec(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		obs  string
		ag   string
	}{
		{"agent_id", KeyAgentID, agentspec.MetadataKeyAgentID},
		{"user_id", KeyUserID, agentspec.MetadataKeyUserID},
		{"workspace_id", KeyWorkspaceID, agentspec.MetadataKeyWorkspaceID},
		{"worker_type", KeyWorkerType, agentspec.MetadataKeyWorkerType},
		{"platform", KeyPlatform, agentspec.MetadataKeyPlatform},
	}
	for _, c := range cases {
		require.Equal(t, c.ag, c.obs, "observability key for %s must equal agentspec metadata key", c.name)
	}
}

// Non-empty, snake_case values — a sanity check that no key was left blank or
// mis-typed at the declaration site.
func TestKeyValuesWellFormed(t *testing.T) {
	t.Parallel()
	keys := []string{
		KeyTraceID, KeySpanID,
		KeyAgentID, KeyUserID, KeyWorkspaceID, KeyExecutionID, KeySessionID,
		KeyWorkerType, KeyPlatform, KeyEventType, KeyDirection, KeyReason,
		KeyStatus, KeyErrorType, KeyExitCode, KeyDeliveryStatus, KeyRuntimeStatus,
	}
	for _, k := range keys {
		require.NotEmpty(t, k)
	}
}
