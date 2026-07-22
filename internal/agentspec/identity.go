package agentspec

import (
	"crypto/sha1"
	"encoding/json"
	"maps"
	"strings"

	"github.com/google/uuid"
)

// AgentIdentity is the stable, secret-free identity value object bound to a
// runtime session (#848). It is the canonical identity context propagated —
// under unified key names — to AEP metadata, audit detail_json, and trace
// attributes, so session/worker/event/audit/trace can be correlated by agent
// identity.
//
// Secret-free invariant: no field carries an API key, env value, or credential.
// AgentID is a deterministic hash of non-secret references. An AgentIdentity is
// therefore always safe to log, audit, and emit as event/trace metadata.
//
// Relationship to IdentityRefs: IdentityRefs (on AgentSpec, #847) holds the raw
// ID references gathered during normalization; AgentIdentity (#848) is the
// bound, persisted value object — it adds the derived AgentID, an explicit
// Anonymous marker, and a Provider hint, and is the form that flows to
// observability surfaces.
type AgentIdentity struct {
	// AgentID is the deterministic identifier derived from the identity tuple
	// (DeriveAgentID). Stable across reconnects/resume for the same logical
	// agent, so it is the primary correlation key. It is NOT a global registry
	// id — two sessions with the same tuple intentionally share an AgentID.
	AgentID string `json:"agent_id,omitempty"`
	// AgentName is the human-readable agent name (bot name / workspace name /
	// worker type fallback). Informational only.
	AgentName string `json:"agent_name,omitempty"`
	// UserID is the authenticated session owner (the current auth/session owner;
	// existing owner checks remain authoritative — #848 does not change them).
	UserID string `json:"user_id,omitempty"`
	// WorkspaceID is the multitenancy anchor (WebChat). Empty for platform/cron
	// sessions, which are not workspace-bound.
	WorkspaceID string `json:"workspace_id,omitempty"`
	// BotID is the bot identity (Slack UserID / Feishu OpenID / WebChat bot id).
	BotID string `json:"bot_id,omitempty"`
	// Platform is the originating platform (webchat|feishu|slack|yuanxin|cron).
	Platform string `json:"platform,omitempty"`
	// Provider is a best-effort provider hint (worker-specific config / adapter).
	// First-cut leaves it empty unless the caller supplies it; it is informational
	// and never a credential.
	Provider string `json:"provider,omitempty"`
	// WorkerType is the normalized worker type (claude_code|opencode_server|
	// codex_cli|acp). Low cardinality — usable as a metric label.
	WorkerType string `json:"worker_type,omitempty"`
	// Anonymous marks an unauthenticated/anonymous identity. Anonymous sessions
	// still get a deterministic AgentID (from the remaining tuple) so they
	// correlate, but consumers MUST surface Anonymous=true to avoid implying a
	// known principal.
	Anonymous bool `json:"anonymous,omitempty"`
}

// AnonymousUserID is the canonical anonymous-user marker. It mirrors the
// literal "anonymous" used by the REST entry (api.go) and audit (AnonymousUserID)
// without importing those packages — all three are the same convention.
const AnonymousUserID = "anonymous"

// Metadata key names propagated to AEP metadata, audit detail_json, and trace
// attributes. All three surfaces use the SAME keys so a single value correlates
// across them (API-DESIGN §Metadata 要求). High-cardinality keys (agent_id,
// user_id, workspace_id) are allowed in trace/event/audit but MUST NOT be used
// as metric labels; worker_type and platform are low-cardinality and label-safe.
const (
	MetadataKeyAgentID     = "agent_id"
	MetadataKeyUserID      = "user_id"
	MetadataKeyWorkspaceID = "workspace_id"
	MetadataKeyWorkerType  = "worker_type"
	MetadataKeyPlatform    = "platform"
)

// IdentityContextKey is the reserved key under session context_json where the
// bound AgentIdentity is persisted (#848). The leading underscore marks it as a
// reserved, system-owned key to avoid collision with future user-facing context
// entries. It folds into the existing context_json TEXT column — no migration.
const IdentityContextKey = "_agent_identity"

// agentIdentityNamespace isolates AgentID derivation from session-key namespaces
// (mirrors session.CronNamespace's sub-namespace isolation pattern), so an
// AgentID can never collide with or be mistaken for a session key.
var agentIdentityNamespace = uuid.NewHash(
	sha1.New(),
	uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8"), // DNS namespace (parent)
	[]byte("hotplex:agent-identity"),                       // sub-namespace seed
	5,
)

// DeriveAgentID derives a deterministic, collision-resistant AgentID from the
// identity tuple, mirroring DeriveSessionKey's UUIDv5 approach (same tuple →
// same id). This avoids a global agent registry: the same logical agent across
// reconnects/resume derives the same AgentID.
//
// Empty fields participate as empty strings (deterministic). Anonymous
// identities (empty/"anonymous" UserID) still derive a stable AgentID from the
// remaining tuple — they are NOT randomized — and carry Anonymous=true at the
// value-object layer (BuildAgentIdentity) so consumers can mark them.
func DeriveAgentID(userID, workspaceID, agentName, workerType string) string {
	name := userID + "|" + workspaceID + "|" + agentName + "|" + workerType
	return uuid.NewHash(sha1.New(), agentIdentityNamespace, []byte(name), 5).String()
}

// IdentityInput carries the raw references BuildAgentIdentity normalizes into an
// AgentIdentity. Every field is a non-secret reference gathered by the caller
// (session creation / resume) — BuildAgentIdentity itself stays a pure function.
type IdentityInput struct {
	UserID      string
	WorkspaceID string
	BotID       string
	BotName     string
	Platform    string
	Provider    string // best-effort; "" if unknown (first-cut)
	WorkerType  string
	AgentName   string // optional display name; falls back to BotName → WorkerType
}

// BuildAgentIdentity normalizes the raw references into a bound AgentIdentity:
// it resolves the display AgentName (AgentName → BotName → WorkerType fallback),
// derives the deterministic AgentID, and sets the explicit Anonymous marker.
func BuildAgentIdentity(in IdentityInput) AgentIdentity {
	name := in.AgentName
	if name == "" {
		name = in.BotName
	}
	if name == "" {
		name = in.WorkerType
	}
	return AgentIdentity{
		AgentID:     DeriveAgentID(in.UserID, in.WorkspaceID, name, in.WorkerType),
		AgentName:   name,
		UserID:      in.UserID,
		WorkspaceID: in.WorkspaceID,
		BotID:       in.BotID,
		Platform:    in.Platform,
		Provider:    in.Provider,
		WorkerType:  in.WorkerType,
		Anonymous:   IsAnonymousUser(in.UserID),
	}
}

// IsAnonymousUser reports whether uid denotes an anonymous principal. Both the
// empty string (no authenticated owner) and the canonical AnonymousUserID
// marker are anonymous.
func IsAnonymousUser(uid string) bool {
	return strings.TrimSpace(uid) == "" || uid == AnonymousUserID
}

// MergeIntoContext returns a copy of ctx with the identity folded under
// IdentityContextKey. The original ctx is never mutated. A nil id or nil ctx
// with a nil id yields ctx unchanged (nil identity is not persisted, so legacy
// sessions without an identity remain byte-identical — backwards compatible).
//
// Used by the session store at marshal time to persist the bound identity into
// the existing context_json column without a migration.
func MergeIntoContext(ctx map[string]any, id *AgentIdentity) map[string]any {
	if id == nil {
		return ctx
	}
	out := make(map[string]any, len(ctx)+1)
	maps.Copy(out, ctx)
	out[IdentityContextKey] = *id
	return out
}

// ExtractFromContext removes and returns the identity folded under
// IdentityContextKey (if present), mutating ctx in place to drop the reserved
// key so callers reading ctx see only real context data. Returns nil when the
// key is absent (legacy session → no bound identity).
//
// Used by the session store at scan time: after json round-trip the folded
// value is a generic map[string]any, so it is re-encoded then decoded into the
// typed AgentIdentity.
func ExtractFromContext(ctx map[string]any) *AgentIdentity {
	if ctx == nil {
		return nil
	}
	raw, ok := ctx[IdentityContextKey]
	if !ok {
		return nil
	}
	// After a json round-trip a folded struct arrives as map[string]any; re-encode
	// and decode into the typed value. If the value is already an AgentIdentity
	// (in-memory, not round-tripped) json.Marshal handles it identically.
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var id AgentIdentity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil
	}
	delete(ctx, IdentityContextKey)
	return &id
}

// MetadataMap returns the identity projected onto the unified AEP/audit/trace
// metadata keys. Non-empty fields only (omitempty at the map level), so an
// anonymous/platform session does not emit an empty workspace_id. Consumers
// merge this into Envelope.Metadata / audit DetailJSON / span attributes.
func (id AgentIdentity) MetadataMap() map[string]any {
	m := map[string]any{
		MetadataKeyAgentID:    id.AgentID,
		MetadataKeyWorkerType: id.WorkerType,
	}
	if id.UserID != "" {
		m[MetadataKeyUserID] = id.UserID
	}
	if id.WorkspaceID != "" {
		m[MetadataKeyWorkspaceID] = id.WorkspaceID
	}
	if id.Platform != "" {
		m[MetadataKeyPlatform] = id.Platform
	}
	return m
}
