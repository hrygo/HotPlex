package agentspec

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
)

// SnapshotVersion is the schema version of an EffectiveAgentSpecSnapshot. It is
// bumped on any breaking change to the persisted shape, so a future migration can
// detect, reject, or upgrade a legacy snapshot rather than silently
// misinterpreting it. Persisted alongside the snapshot under the Version field.
const SnapshotVersion = 1

// SnapshotContextKey is the reserved key under session context_json where the
// effective AgentSpec snapshot is persisted (#866). The leading underscore marks
// it as a reserved, system-owned key (same convention as IdentityContextKey),
// avoiding collision with future user-facing context entries. It folds into the
// existing context_json TEXT column — no migration.
const SnapshotContextKey = "_agent_spec"

// MetadataKeySpecVersion / MetadataKeySpecHash are the audit/runtime metadata
// keys for the effective-spec fingerprint (issue #866: "record snapshot
// version/hash in audit and runtime metadata"). Carried in audit detail_json so
// a session correlates to the exact effective policy that governed it, without
// persisting the full snapshot blob in every surface. spec_hash is medium
// cardinality — acceptable in audit detail, but MUST NOT be a metric label.
const (
	MetadataKeySpecVersion = "spec_version"
	MetadataKeySpecHash    = "spec_hash"
)

// EffectiveAgentSpecSnapshot is the versioned, secret-free projection of the
// effective AgentSpec that governed a session at start time (#866). It is
// persisted in session context_json so resume/restart/audit reconstruct the same
// runtime contract even if mutable upstream configuration drifts afterwards —
// i.e. a config change does NOT silently alter an existing session's effective
// policy (issue #866 acceptance criterion 2).
//
// Secret-free invariant (mirrors AgentSpec / AgentIdentity): no field carries an
// API key, env value, or credential. Only policy/tool/budget fields are
// captured; provider tokens and credential-bearing worker settings deliberately
// stay out. A snapshot is therefore always safe to persist, log, and audit.
//
// Relationship to AgentSpec: AgentSpec (#847) is the live, resolved view;
// EffectiveAgentSpecSnapshot (#866) is its persisted, hash-stamped form. The
// snapshot drops IdentityRefs (the bound AgentIdentity #848 is persisted
// separately under its own reserved key) and adds Version + Hash.
type EffectiveAgentSpecSnapshot struct {
	// Version is SnapshotVersion at persist time. Consumers MUST refuse to apply
	// (or flag for re-resolution) a snapshot whose version they do not understand.
	Version int `json:"v"`

	// WorkerType is the resolved worker type (claude_code|opencode_server|
	// codex_cli|acp). Low cardinality — label-safe.
	WorkerType string `json:"worker_type,omitempty"`
	// Model is the resolved/default model (contract; informational).
	Model string `json:"model,omitempty"`

	// PermissionMode is the normalized 4-tier ceiling
	// (read-only|workspace|auto-edit|bypass). Empty = worker default.
	PermissionMode string `json:"permission_mode,omitempty"`
	// SkipPermissions bypasses all permission checks.
	SkipPermissions bool `json:"skip_permissions,omitempty"`
	// AllowedTools is the tool whitelist. Non-nil/non-empty is the value that
	// MUST be restored on resume (the named persistence gap of #866).
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// DisallowedTools is the tool blacklist.
	DisallowedTools []string `json:"disallowed_tools,omitempty"`

	// SandboxMode is the sandbox mode (read-only|workspace-write|danger-full-access).
	SandboxMode string `json:"sandbox_mode,omitempty"`
	// AllowedDirs lists additional directories the worker may access.
	AllowedDirs []string `json:"allowed_dirs,omitempty"`

	// MaxTurns is the turn budget ceiling.
	MaxTurns int `json:"max_turns,omitempty"`
	// MaxBudgetUSD is the spend budget ceiling.
	MaxBudgetUSD float64 `json:"max_budget_usd,omitempty"`

	// Hash is a short, stable content hash (first 16 hex chars of SHA-256 over
	// the canonical snapshot body) used to correlate audit/runtime metadata with
	// the exact effective policy without persisting the full blob in every
	// surface. Computed by SnapshotFromSpec; empty only for hand-built test
	// values.
	Hash string `json:"hash,omitempty"`
}

// SnapshotFromSpec projects a resolved AgentSpec into a versioned, hash-stamped
// EffectiveAgentSpecSnapshot. Pure function (no globals, no I/O) so it is safe
// to call at any layer and trivially table-testable. The hash makes two sessions
// governed by the same effective policy correlate via a shared spec_hash.
func SnapshotFromSpec(spec AgentSpec) EffectiveAgentSpecSnapshot {
	s := EffectiveAgentSpecSnapshot{
		Version:         SnapshotVersion,
		WorkerType:      spec.Worker.Type,
		Model:           spec.Worker.Model,
		PermissionMode:  spec.Policy.PermissionMode,
		SkipPermissions: spec.Policy.SkipPermissions,
		AllowedTools:    cloneStrings(spec.Policy.AllowedTools),
		DisallowedTools: cloneStrings(spec.Policy.DisallowedTools),
		SandboxMode:     spec.Sandbox.Mode,
		AllowedDirs:     cloneStrings(spec.Sandbox.AllowedDirs),
		MaxTurns:        spec.Budget.MaxTurns,
		MaxBudgetUSD:    spec.Budget.MaxBudgetUSD,
	}
	s.Hash = computeSnapshotHash(s)
	return s
}

// cloneStrings returns a defensive copy so the snapshot owns its slices
// independent of the source AgentSpec (the snapshot is persisted and outlives
// the in-memory spec). A nil source yields nil (not an empty slice) so omitempty
// keeps a tool-less session out of the persisted blob.
func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

// computeSnapshotHash returns the first 16 hex chars of SHA-256 over the
// snapshot's canonical JSON body (Version + all effective fields, Hash zeroed).
// The body is deterministic for a given effective policy: Go's json.Marshal emits
// struct fields in declaration order, and omitempty drops empty/zero fields, so
// nil vs empty slices hash identically (both mean "not set"). 16 hex chars
// (64 bits) is ample to avoid collision across the policy space a single
// deployment exercises.
func computeSnapshotHash(s EffectiveAgentSpecSnapshot) string {
	body := s
	body.Hash = "" // hash is over the body, not itself
	b, err := json.Marshal(body)
	if err != nil {
		// EffectiveAgentSpecSnapshot only holds JSON-safe scalars and []string, so
		// a marshal failure is impossible in practice. Falling back to an empty
		// hash keeps callers safe rather than panicking during persistence.
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// MergeSnapshotIntoContext returns a copy of ctx with the snapshot folded under
// SnapshotContextKey. The original ctx is never mutated. A nil snapshot or nil
// snapshot with nil ctx yields ctx unchanged (no snapshot is persisted, so legacy
// sessions without a snapshot remain byte-identical — backwards compatible).
//
// Used by the session store at marshal time to persist the effective spec into
// the existing context_json column without a migration. Mirrors
// MergeIntoContext (#848) for the identity value object.
func MergeSnapshotIntoContext(ctx map[string]any, snap *EffectiveAgentSpecSnapshot) map[string]any {
	if snap == nil {
		return ctx
	}
	out := make(map[string]any, len(ctx)+1)
	maps.Copy(out, ctx)
	out[SnapshotContextKey] = *snap
	return out
}

// ExtractSnapshotFromContext removes and returns the snapshot folded under
// SnapshotContextKey (if present), mutating ctx in place to drop the reserved
// key so callers reading ctx see only real context data. Returns nil when the
// key is absent (legacy session → no persisted snapshot).
//
// Used by the session store at scan time: after a json round-trip the folded
// value is a generic map[string]any, so it is re-encoded then decoded into the
// typed EffectiveAgentSpecSnapshot. Mirrors ExtractFromContext (#848).
func ExtractSnapshotFromContext(ctx map[string]any) *EffectiveAgentSpecSnapshot {
	if ctx == nil {
		return nil
	}
	raw, ok := ctx[SnapshotContextKey]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var snap EffectiveAgentSpecSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil
	}
	delete(ctx, SnapshotContextKey)
	return &snap
}

// RestoreAllowedTools reports the tools the snapshot says this session was
// started with, or nil when the snapshot records no tool whitelist (nil = "no
// restriction" in AgentSpec semantics). The session store uses this at scan time
// to repopulate SessionInfo.AllowedTools — the one effective-policy field that is
// in-memory only and would otherwise be lost across a gateway restart (#866 AC1).
func (s *EffectiveAgentSpecSnapshot) RestoreAllowedTools() []string {
	if s == nil {
		return nil
	}
	return cloneStrings(s.AllowedTools)
}

// StampMetadata merges the snapshot fingerprint (version + hash) into m and
// returns m for chaining. Nil-safe: a nil snapshot or nil map leaves m unchanged.
// Used by audit detail_json (and, later, runtime event metadata) so a session
// correlates to the exact effective policy that governed it.
func (s *EffectiveAgentSpecSnapshot) StampMetadata(m map[string]any) map[string]any {
	if s == nil || m == nil {
		return m
	}
	m[MetadataKeySpecVersion] = s.Version
	if s.Hash != "" {
		m[MetadataKeySpecHash] = s.Hash
	}
	return m
}
