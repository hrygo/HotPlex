package agentconfig

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"
)

// RuntimeFactsSchemaVersion is the version of the bounded facts payload that
// is embedded in the AgentConfig prompt. It is independent from the outer
// prompt schema version so the payload can evolve without changing the
// surrounding directives/context structure.
const RuntimeFactsSchemaVersion = 1

const (
	runtimeFactsMaxCollection  = 16
	runtimeFactsMaxScalarBytes = 128
)

// RuntimeScopeKind describes the scope selected by the Gateway for a session.
// It deliberately contains no scope identity value.
type RuntimeScopeKind string

const (
	RuntimeScopeBot       RuntimeScopeKind = "bot"
	RuntimeScopeWorkspace RuntimeScopeKind = "workspace"
	RuntimeScopeUnbound   RuntimeScopeKind = "unbound"
)

// RuntimeWorkerType is a worker type name without importing the worker
// package. Keeping this contract worker-independent prevents prompt assembly
// from depending on adapter implementation details.
type RuntimeWorkerType string

const (
	RuntimeWorkerClaudeCode  RuntimeWorkerType = "claude_code"
	RuntimeWorkerOpenCodeSrv RuntimeWorkerType = "opencode_server"
	RuntimeWorkerCodexCLI    RuntimeWorkerType = "codex_cli"
	RuntimeWorkerACP         RuntimeWorkerType = "acp"
	RuntimeWorkerUnknown     RuntimeWorkerType = "unknown"
)

// RuntimeCapability is a closed declaration of an adapter capability. These
// values describe what the Gateway can ask the selected adapter to do; they do
// not claim that an external Worker process is currently healthy.
type RuntimeCapability string

const (
	CapabilityResume    RuntimeCapability = "resume"
	CapabilityStreaming RuntimeCapability = "streaming"
	CapabilityTools     RuntimeCapability = "tools"
)

// RuntimeQuerySurface identifies a bounded query surface exposed by the
// Gateway/Worker pair. It is not a catalog of individual commands or tools.
type RuntimeQuerySurface string

const (
	QuerySkills RuntimeQuerySurface = "skills"
	QueryMCP    RuntimeQuerySurface = "mcp"
	QueryWorker RuntimeQuerySurface = "worker"
)

// SkillCatalogOwner identifies who is authoritative for the current session's
// Skill catalog. The first runtime-facts version only emits worker or none.
type SkillCatalogOwner string

const (
	SkillCatalogOwnerWorker  SkillCatalogOwner = "worker"
	SkillCatalogOwnerGateway SkillCatalogOwner = "gateway"
	SkillCatalogOwnerNone    SkillCatalogOwner = "none"
)

// RuntimeFacts is the small, trusted declaration inserted into a live
// AgentConfig prompt. It intentionally excludes identity values, environment
// values, paths, catalogs, and arbitrary extension maps.
type RuntimeFacts struct {
	SchemaVersion             int                   `json:"schema_version"`
	Platform                  string                `json:"platform,omitempty"`
	WorkerType                RuntimeWorkerType     `json:"worker_type,omitempty"`
	ScopeKind                 RuntimeScopeKind      `json:"scope_kind,omitempty"`
	DeclaredPermissionMode    string                `json:"declared_permission_mode,omitempty"`
	DeclaredCapabilities      []RuntimeCapability   `json:"declared_capabilities,omitempty"`
	DeclaredQuerySurfaces     []RuntimeQuerySurface `json:"declared_query_surfaces,omitempty"`
	DeclaredSkillCatalogOwner SkillCatalogOwner     `json:"declared_skill_catalog_owner,omitempty"`
	PresentGatewayEnvKeys     []string              `json:"present_gateway_env_keys,omitempty"`
}

// canonicalRuntimeFacts is kept separate from RuntimeFacts so canonicalizing
// one prompt never mutates a caller-owned slice.
type canonicalRuntimeFacts struct {
	SchemaVersion             int                   `json:"schema_version"`
	Platform                  string                `json:"platform,omitempty"`
	WorkerType                RuntimeWorkerType     `json:"worker_type,omitempty"`
	ScopeKind                 RuntimeScopeKind      `json:"scope_kind,omitempty"`
	DeclaredPermissionMode    string                `json:"declared_permission_mode,omitempty"`
	DeclaredCapabilities      []RuntimeCapability   `json:"declared_capabilities,omitempty"`
	DeclaredQuerySurfaces     []RuntimeQuerySurface `json:"declared_query_surfaces,omitempty"`
	DeclaredSkillCatalogOwner SkillCatalogOwner     `json:"declared_skill_catalog_owner,omitempty"`
	PresentGatewayEnvKeys     []string              `json:"present_gateway_env_keys,omitempty"`
}

var allowedGatewayRuntimeEnvKeys = map[string]struct{}{
	"GATEWAY_PLATFORM":   {},
	"GATEWAY_BOT_ID":     {},
	"GATEWAY_BOT_NAME":   {},
	"GATEWAY_USER_ID":    {},
	"GATEWAY_SESSION_ID": {},
	"GATEWAY_WORK_DIR":   {},
	"GATEWAY_CHANNEL_ID": {},
	"GATEWAY_THREAD_ID":  {},
	"GATEWAY_TEAM_ID":    {},
}

var allowedRuntimeWorkerTypes = map[RuntimeWorkerType]struct{}{
	RuntimeWorkerClaudeCode:  {},
	RuntimeWorkerOpenCodeSrv: {},
	RuntimeWorkerCodexCLI:    {},
	RuntimeWorkerACP:         {},
	RuntimeWorkerUnknown:     {},
}

var allowedRuntimeScopes = map[RuntimeScopeKind]struct{}{
	RuntimeScopeBot:       {},
	RuntimeScopeWorkspace: {},
	RuntimeScopeUnbound:   {},
}

var allowedRuntimeCapabilities = map[RuntimeCapability]struct{}{
	CapabilityResume:    {},
	CapabilityStreaming: {},
	CapabilityTools:     {},
}

var allowedRuntimeQuerySurfaces = map[RuntimeQuerySurface]struct{}{
	QuerySkills: {},
	QueryMCP:    {},
	QueryWorker: {},
}

var allowedSkillCatalogOwners = map[SkillCatalogOwner]struct{}{
	SkillCatalogOwnerWorker:  {},
	SkillCatalogOwnerGateway: {},
	SkillCatalogOwnerNone:    {},
}

// CanonicalJSON validates and serializes facts with stable field order and
// sorted/deduplicated collections. Zero facts return a nil payload so callers
// can omit the entire runtime-facts element.
func (f RuntimeFacts) CanonicalJSON() ([]byte, error) {
	if f.isZero() {
		return nil, nil
	}

	normalized, err := normalizeRuntimeFacts(f)
	if err != nil {
		return nil, err
	}
	return json.Marshal(canonicalRuntimeFacts(normalized))
}

// Validate checks the same constraints used by CanonicalJSON without exposing
// a mutable normalized copy.
func (f RuntimeFacts) Validate() error {
	if f.isZero() {
		return nil
	}
	_, err := normalizeRuntimeFacts(f)
	return err
}

func (f RuntimeFacts) isZero() bool {
	return f.Platform == "" && f.WorkerType == "" && f.ScopeKind == "" &&
		f.DeclaredPermissionMode == "" &&
		len(f.DeclaredCapabilities) == 0 && len(f.DeclaredQuerySurfaces) == 0 &&
		f.DeclaredSkillCatalogOwner == "" && len(f.PresentGatewayEnvKeys) == 0
}

func normalizeRuntimeFacts(f RuntimeFacts) (RuntimeFacts, error) {
	if f.SchemaVersion == 0 {
		f.SchemaVersion = RuntimeFactsSchemaVersion
	}
	if f.SchemaVersion != RuntimeFactsSchemaVersion {
		return RuntimeFacts{}, fmt.Errorf("agentconfig: unsupported runtime facts schema version %d", f.SchemaVersion)
	}

	var err error
	if f.Platform, err = boundRuntimeScalar(f.Platform); err != nil {
		return RuntimeFacts{}, fmt.Errorf("agentconfig: invalid runtime platform: %w", err)
	}
	if f.DeclaredPermissionMode, err = boundRuntimeScalar(f.DeclaredPermissionMode); err != nil {
		return RuntimeFacts{}, fmt.Errorf("agentconfig: invalid declared permission mode: %w", err)
	}
	if f.WorkerType != "" {
		if _, ok := allowedRuntimeWorkerTypes[f.WorkerType]; !ok {
			return RuntimeFacts{}, fmt.Errorf("agentconfig: invalid runtime worker type %q", f.WorkerType)
		}
	}
	if f.ScopeKind != "" {
		if _, ok := allowedRuntimeScopes[f.ScopeKind]; !ok {
			return RuntimeFacts{}, fmt.Errorf("agentconfig: invalid runtime scope %q", f.ScopeKind)
		}
	}
	if f.DeclaredSkillCatalogOwner != "" {
		if _, ok := allowedSkillCatalogOwners[f.DeclaredSkillCatalogOwner]; !ok {
			return RuntimeFacts{}, fmt.Errorf("agentconfig: invalid Skill catalog owner %q", f.DeclaredSkillCatalogOwner)
		}
	}

	f.DeclaredCapabilities, err = normalizeRuntimeEnums(f.DeclaredCapabilities, allowedRuntimeCapabilities, "capability")
	if err != nil {
		return RuntimeFacts{}, err
	}
	f.DeclaredQuerySurfaces, err = normalizeRuntimeEnums(f.DeclaredQuerySurfaces, allowedRuntimeQuerySurfaces, "query surface")
	if err != nil {
		return RuntimeFacts{}, err
	}
	f.PresentGatewayEnvKeys, err = normalizeRuntimeEnvKeys(f.PresentGatewayEnvKeys)
	if err != nil {
		return RuntimeFacts{}, err
	}
	return f, nil
}

func boundRuntimeScalar(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("contains invalid UTF-8")
	}
	if len(value) <= runtimeFactsMaxScalarBytes {
		return value, nil
	}
	cut := value[:runtimeFactsMaxScalarBytes]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut, nil
}

func normalizeRuntimeEnums[T ~string](values []T, allowed map[T]struct{}, label string) ([]T, error) {
	if len(values) > runtimeFactsMaxCollection {
		return nil, fmt.Errorf("agentconfig: too many runtime %ss", label)
	}
	seen := make(map[T]struct{}, len(values))
	result := make([]T, 0, len(values))
	for _, value := range values {
		if _, ok := allowed[value]; !ok {
			return nil, fmt.Errorf("agentconfig: invalid runtime %s %q", label, value)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result, nil
}

func normalizeRuntimeEnvKeys(values []string) ([]string, error) {
	if len(values) > runtimeFactsMaxCollection {
		return nil, fmt.Errorf("agentconfig: too many Gateway environment keys")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := allowedGatewayRuntimeEnvKeys[value]; !ok {
			return nil, fmt.Errorf("agentconfig: environment key %q is not allowlisted", value)
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}
