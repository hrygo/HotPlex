# AgentConfig Safe Self-Configuration Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development, superpowers:api-and-interface-design, superpowers:security-and-hardening, and superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an Agent inspect and propose changes to its own HotPlex AgentConfig, while ensuring that only an authenticated human host can approve and apply an exact, unexpired, conflict-free change.

**Architecture:** Add a Gateway-owned `internal/agentcontrol` service with four operations—inspect, plan, apply, verify—backed by immutable short-lived change records. File targets use durable atomic replacement; Workspace overrides use the existing optimistic CAS. Admin/WebChat supplies human approval. A session-bound, authenticated Streamable HTTP MCP server exposes only the current Bot or Workspace scope to the Agent and can apply only a separately approved change ID.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk/mcp` v1.7.0 Streamable HTTP server, HMAC-SHA-256 session tokens, testify/require, SQLite/PostgreSQL CAS stores, Next.js/TypeScript, React i18next, Markdown documentation.

**Design dependency:** `docs/superpowers/specs/2026-08-22-hotplex-agent-metacognition-tools-design.md`

**Protocol references:**
- Official Go SDK: `https://github.com/modelcontextprotocol/go-sdk`
- Streamable HTTP transport: `https://modelcontextprotocol.io/specification/2025-11-25/basic/transports`

## Threat Model and Non-Negotiable Invariants

- The model is an untrusted proposer, never an approver.
- Session MCP authority is limited to the current Bot scope for messaging sessions or current Workspace scope for WebChat/API sessions. Cron and unbound sessions receive no self-configuration MCP server.
- Platform and global scopes are Admin-only and never reachable through session MCP tools.
- Canonical writable files are exactly `SOUL.md`, `AGENTS.md`, `TOOLS.md`, `USER.md`, and `MEMORY.md`; `SKILLS.md` is read-only legacy provenance and never a write target.
- Every plan binds target scope, target ID, canonical filename, base fingerprint, proposed-content hash, actor, session, creation time, and expiry.
- Approval is a separate host-authenticated operation bound to the immutable change ID and proposed-content hash. MCP cannot manufacture or self-approve it.
- Apply rechecks identity, scope, approval, expiry, base fingerprint, filename rules, size limits, AgentConfig validation, and target existence.
- Change records are memory-only, bounded, and short-lived. Gateway restart invalidates them safely.
- File writes are temp-write → file sync → close → atomic rename → parent-directory sync where supported.
- Workspace writes use the existing `updated_at` optimistic CAS in both SQLite and PostgreSQL.
- Logs and audit records contain change ID, target identity, canonical filename, hashes, outcome, and error code only—never config bodies, diffs, prompt bodies, tokens, or raw backend errors.
- Apply persists a change but does not silently reset a live session. Activation occurs on a separately requested `/reset` or a new session.
- Verify distinguishes persisted, effective, activated, and delivered state; it never equates a write, HTTP 200, or hash match with model compliance.
- Real Agent Skills discovery, Admin Skills API, WebChat Skills pages, Worker `/skills`, and AEP `skills` fields are out of scope and unchanged.

## Stable Error Contract

| Code | HTTP | Meaning |
|---|---:|---|
| `AGENT_CONFIG_UNKNOWN_FILE` | 400 | File is not a canonical AgentConfig target |
| `AGENT_CONFIG_SCOPE_FORBIDDEN` | 403 | Actor/session cannot target the requested scope |
| `AGENT_CONFIG_LEGACY_CONFLICT` | 409 | Canonical and legacy Tools files conflict at the target scope |
| `AGENT_CONFIG_STALE_BASE` | 409 | Current base fingerprint differs from the plan |
| `AGENT_CONFIG_APPROVAL_REQUIRED` | 428 | Change is valid but lacks independent host approval |
| `AGENT_CONFIG_CHANGE_EXPIRED` | 410 | Change or approval is expired |
| `AGENT_CONFIG_VALIDATION_FAILED` | 422 | Content, size, schema, or effective prompt validation failed |
| `SYSTEM_PROMPT_UNSUPPORTED` | 409 | Target session cannot activate the resulting prompt |

MCP tools return the same code in structured error data. Messages remain bounded and contain no body/diff/raw error.

---

### Task 1: Define the control-plane domain and fingerprints

**Files:**
- Create: `internal/agentcontrol/types.go`
- Create: `internal/agentcontrol/errors.go`
- Create: `internal/agentcontrol/fingerprint.go`
- Create: `internal/agentcontrol/fingerprint_test.go`
- Modify: `internal/agentconfig/prompt.go`
- Modify: `internal/agentconfig/prompt_test.go`

**Interfaces:**
- Produces: typed scopes, canonical targets, inspect/plan/apply/verify DTOs, stable errors, base fingerprints, and prompt-section manifests.
- Consumes: canonical AgentConfig names, `ResolvedLocation`, `ValidateOverrides`, and `BuildSystemPrompt`.

- [ ] **Step 1: Write failing fingerprint and manifest tests**

Cover distinctions that must never hash alike:

- missing vs present-empty;
- canonical `TOOLS.md` vs legacy `SKILLS.md` with equal content;
- bot vs platform vs global target;
- Workspace override key missing vs present-empty;
- equal Workspace content with different `updated_at` versions;
- one changed prompt section changes only that section hash and the full prompt hash.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/agentcontrol ./internal/agentconfig -run 'Fingerprint|PromptManifest' -count=1
```

- [ ] **Step 3: Define exact public DTOs**

Use typed structures rather than `map[string]any`:

```go
type ScopeKind string // workspace|bot|platform|global

type Target struct {
    Scope       ScopeKind `json:"scope"`
    WorkspaceID string    `json:"workspace_id,omitempty"`
    Platform    string    `json:"platform,omitempty"`
    BotName     string    `json:"bot_name,omitempty"`
    File        string    `json:"file"`
}

type InspectResult struct {
    Target          Target `json:"target"`
    EffectiveSource string `json:"effective_source"`
    PhysicalFile    string `json:"physical_file,omitempty"`
    Present         bool   `json:"present"`
    Content         string `json:"content"`
    BaseHash        string `json:"base_hash"`
    ContentHash     string `json:"content_hash"`
}
```

`PlanRequest` accepts one full replacement `content` plus the exact `base_hash`; v1 deliberately excludes free-form filesystem paths and patch execution.

- [ ] **Step 4: Implement canonical fingerprint encoding**

Hash a versioned length-prefixed tuple, not ambiguous string concatenation. For files include target identity, presence, physical basename, and content bytes. For Workspace include target identity, override key presence/value, and `updated_at`. Use SHA-256 hex.

- [ ] **Step 5: Add a prompt manifest**

Refactor prompt assembly so `BuildSystemPrompt` and a new `BuildSystemPromptManifest` share one ordered section model. The manifest exposes only section names and hashes plus the full prompt hash; it must not duplicate content in logs or diagnostics.

- [ ] **Step 6: Run focused tests**

```bash
go test ./internal/agentcontrol ./internal/agentconfig -count=1
```

- [ ] **Step 7: Commit domain contracts**

```bash
git add internal/agentcontrol internal/agentconfig
git commit -m "feat(agentcontrol): define safe change contracts"
```

### Task 2: Make filesystem writes durable and Workspace writes target-specific

**Files:**
- Modify: `internal/agentconfig/writer.go`
- Modify: `internal/agentconfig/writer_test.go`
- Create: `internal/agentconfig/dirsync_unix.go`
- Create: `internal/agentconfig/dirsync_windows.go`
- Modify: `internal/session/multitenancy_store.go`
- Modify: `internal/session/multitenancy_pg_store.go`
- Modify: `internal/session/coverage_test.go`
- Modify: `internal/session/pg_multitenancy_test.go`

**Interfaces:**
- Produces: durable canonical file replacement and a narrow Workspace AgentConfig CAS method.
- Consumes: `WriteFile`, `UserWorkspaceStore`, and existing `updated_at` conflict semantics.

- [ ] **Step 1: Add failing durability and CAS tests**

Test temp cleanup on write/sync/rename errors through injectable filesystem seams, target mode preservation/new-file mode, canonical-only filenames, and no partial target content. For SQLite and PostgreSQL prove that updating only AgentConfig overrides:

- succeeds with the expected `updated_at`;
- returns `ErrWorkspaceConflict` on stale version;
- does not overwrite concurrently changed name/work_dir/permission fields;
- preserves explicit empty overrides.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/agentconfig ./internal/session -run 'WriteFile|AgentConfigCAS' -count=1
```

- [ ] **Step 3: Harden `WriteFile`**

Write the temp file in the target directory, apply the intended mode, call `Sync`, close, rename atomically, then sync the parent directory on supported platforms. Ensure every failure removes only the temp file. Never delete or truncate the existing target before rename.

- [ ] **Step 4: Add a narrow store method**

Extend `UserWorkspaceStore` with:

```go
UpdateWorkspaceAgentConfig(ctx context.Context, id, overrides string, expectedUpdatedAt, now int64) error
```

Implement paired SQLite/PostgreSQL statements updating only `agent_config_overrides` and `updated_at` under the existing optimistic CAS. Add paired SQL query files if this repository's query loader requires them.

Because `updated_at` is currently second-granularity, compute the next value as `max(now, expectedUpdatedAt+1)` so every successful write advances the CAS token even when two writes occur within one second.

- [ ] **Step 5: Run store and writer suites**

```bash
go test ./internal/agentconfig ./internal/session -count=1 -race -shuffle=on
```

- [ ] **Step 6: Commit durable persistence**

```bash
git add internal/agentconfig internal/session
git commit -m "feat(agentcontrol): add durable config persistence"
```

### Task 3: Implement the bounded change store and independent approval

**Files:**
- Create: `internal/agentcontrol/store.go`
- Create: `internal/agentcontrol/store_test.go`
- Create: `internal/agentcontrol/approval.go`
- Create: `internal/agentcontrol/approval_test.go`

**Interfaces:**
- Produces: immutable change IDs, TTL/capacity eviction, actor/session binding, and separate approval records.
- Consumes: `crypto/rand`, injected clock, target/base/proposed hashes, and stable errors.

- [ ] **Step 1: Write failing lifecycle tests**

Test create/get, cryptographically opaque IDs, immutability, TTL expiry, maximum-entry eviction, restart-as-empty-store behavior, approval by a different authenticated channel, denied approval, actor/target/hash mismatch, double apply, and concurrent approve/apply under `-race`.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/agentcontrol -run 'ChangeStore|Approval' -count=1 -race
```

- [ ] **Step 3: Implement the bounded store**

Use one explicit mutex and injected clock. Generate at least 128 bits of randomness for `change_id`. Set a documented short default TTL and hard capacity in configuration, with validated minimum/maximum bounds. Store proposed content only in memory; expose copies, never mutable internal pointers.

- [ ] **Step 4: Implement independent approval**

Approval records bind `change_id`, proposal hash, approving user ID, approval time, and expiry. The approval method requires a host-authenticated principal and must reject an MCP/session-token principal even if user IDs match.

- [ ] **Step 5: Run lifecycle and race tests**

```bash
go test ./internal/agentcontrol -run 'ChangeStore|Approval' -count=1 -race -shuffle=on
```

- [ ] **Step 6: Commit plan/approval state**

```bash
git add internal/agentcontrol
git commit -m "feat(agentcontrol): add expiring approved changes"
```

### Task 4: Implement inspect, plan, apply, and verify services

**Files:**
- Create: `internal/agentcontrol/service.go`
- Create: `internal/agentcontrol/service_test.go`
- Create: `internal/agentcontrol/file_target.go`
- Create: `internal/agentcontrol/workspace_target.go`

**Interfaces:**
- Produces: the four control-plane operations with one authorization and validation path for HTTP and MCP callers.
- Consumes: AgentConfig loader/writer, Workspace store, change store, principal/target authorizer, prompt manifest, and optional live delivery reporter.

- [ ] **Step 1: Write the authorization matrix tests**

Freeze this matrix:

| Caller | Workspace current | Bot current | Platform | Global |
|---|---:|---:|---:|---:|
| session MCP, WebChat/API | allow | deny | deny | deny |
| session MCP, Slack/Feishu | deny | allow | deny | deny |
| session MCP, cron/unbound | deny | deny | deny | deny |
| workspace owner HTTP | own only | deny | deny | deny |
| admin HTTP | allow | allow | allow | allow |

Also test canonical file enumeration, legacy collision, stale base, expired/denied/missing approval, invalid content, oversize content, and concurrent target mutation.

- [ ] **Step 2: Run service tests and confirm RED**

```bash
go test ./internal/agentcontrol -run 'Service|Authorization|Inspect|Plan|Apply|Verify' -count=1
```

- [ ] **Step 3: Implement `Inspect`**

Resolve the current effective content and writable target separately. Return the effective source and current writable-base fingerprint. For Tools, if canonical and legacy files coexist at the writable scope, return `AGENT_CONFIG_LEGACY_CONFLICT` rather than guessing.

- [ ] **Step 4: Implement `Plan`**

Require an exact base hash and canonical file. Re-read the target, validate content and the fully assembled effective prompt, compute content/diff hashes, store the immutable change, and return `change_id`, expiry, target, hashes, and a unified diff for the authenticated caller. Never put the diff in logs or audit.

- [ ] **Step 5: Implement `Apply`**

Load the immutable change, validate independent approval and expiry, reauthorize the current principal, recompute base fingerprint, and revalidate proposed content. Apply with durable file replacement or Workspace CAS. Mark the change consumed only after persistence succeeds; retries after an uncertain error re-inspect before deciding whether the desired content already landed.

- [ ] **Step 6: Implement `Verify`**

Return:

```go
type VerifyResult struct {
    ChangeID         string                            `json:"change_id"`
    Persisted        bool                              `json:"persisted"`
    Effective        bool                              `json:"effective"`
    EffectiveSource  string                            `json:"effective_source"`
    EffectiveHash    string                            `json:"effective_hash"`
    PromptSectionHash string                           `json:"prompt_section_hash"`
    Activation       string                            `json:"activation"` // new_session_or_reset|active|unknown
    Delivery         worker.SystemPromptDeliveryStatus `json:"delivery"`
}
```

`active` requires a live session whose delivered full-prompt hash matches the newly assembled prompt hash. Otherwise return `new_session_or_reset` or `unknown`; never infer activation from persistence alone.

- [ ] **Step 7: Run service tests with race detection**

```bash
go test ./internal/agentcontrol -count=1 -race -shuffle=on
```

- [ ] **Step 8: Commit the service**

```bash
git add internal/agentcontrol internal/agentconfig
git commit -m "feat(agentcontrol): implement safe config transactions"
```

### Task 5: Add host-facing Admin and Workspace APIs with body-free audit

**Files:**
- Create: `internal/admin/agent_config_change_handlers.go`
- Create: `internal/admin/agent_config_change_handlers_test.go`
- Modify: `internal/admin/admin.go`
- Modify: `internal/admin/audit.go`
- Modify: `internal/admin/models.go`
- Modify: `internal/gateway/workspace_handlers.go`
- Modify: `internal/gateway/workspace_handlers_test.go`
- Modify: `internal/audit/types.go`
- Modify: `cmd/hotplex/routes.go`
- Create: `cmd/hotplex/agent_config_change_routes_test.go`

**Interfaces:**
- Produces: authenticated inspect/plan/approve/apply/verify endpoints for admins and Workspace owners.
- Consumes: the shared `agentcontrol.Service`, existing Admin scopes/cookie auth/CSRF middleware, Workspace ownership checks, `AdminAudit`, and durable audit collector.

- [ ] **Step 1: Freeze HTTP contracts with failing tests**

Admin routes:

```text
GET  /admin/agent-config/inspect
POST /admin/agent-config/changes
POST /admin/agent-config/changes/{id}/approve
POST /admin/agent-config/changes/{id}/apply
GET  /admin/agent-config/changes/{id}/verify
```

Workspace-owner routes:

```text
GET  /api/workspaces/{wid}/agent-config/{file}
POST /api/workspaces/{wid}/agent-config/changes
POST /api/workspaces/{wid}/agent-config/changes/{id}/approve
POST /api/workspaces/{wid}/agent-config/changes/{id}/apply
GET  /api/workspaces/{wid}/agent-config/changes/{id}/verify
```

Assert scope enforcement, CSRF on writes, stable status/error mapping, no legacy write alias, and no config body in captured logs/audit.

- [ ] **Step 2: Run focused HTTP tests and confirm RED**

```bash
go test ./internal/admin ./internal/gateway ./cmd/hotplex -run 'AgentConfigChange|AgentConfigInspect|AgentConfigApproval|Routes' -count=1
```

- [ ] **Step 3: Add typed handlers and route wiring**

Admin inspect/plan require `admin:read`; approve/apply require `admin:write`. Workspace routes require owner or admin, and write routes use existing CSRF middleware. Handlers pass typed principals into the service; they never perform an alternate authorization implementation.

- [ ] **Step 4: Add stable audit actions**

Add actions for `agent_config.inspect`, `agent_config.plan`, `agent_config.approve`, `agent_config.apply`, and `agent_config.verify`. Durable detail contains only target IDs, canonical filename, change/base/content hashes, delivery mode/state, and stable outcome/error code.

- [ ] **Step 5: Run HTTP and audit suites**

```bash
go test ./internal/admin ./internal/gateway ./internal/audit ./cmd/hotplex -count=1 -race -shuffle=on
```

- [ ] **Step 6: Commit host APIs**

```bash
git add internal/admin internal/gateway internal/audit cmd/hotplex
git commit -m "feat(admin): add approved agent config changes"
```

### Task 6: Expose a session-bound MCP surface without broadening authority

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/agentcontrol/token.go`
- Create: `internal/agentcontrol/token_test.go`
- Create: `internal/agentcontrol/mcp.go`
- Create: `internal/agentcontrol/mcp_test.go`
- Modify: `internal/config/config_types.go`
- Modify: `internal/config/config_types_test.go`
- Modify: `internal/gateway/deps.go`
- Modify: `internal/gateway/bridge.go`
- Modify: `internal/gateway/bridge_test.go`
- Modify: `internal/worker/claudecode/worker.go`
- Modify: `internal/worker/claudecode/worker_test.go`
- Modify: `internal/worker/codexcli/worker.go`
- Modify: `internal/worker/codexcli/worker_test.go`
- Modify: `internal/worker/opencodeserver/worker.go`
- Modify: `internal/worker/opencodeserver/worker_test.go`
- Modify: `internal/worker/acp/worker.go`
- Modify: `internal/worker/acp/worker_phase2_test.go`
- Modify: `cmd/hotplex/gateway_run.go`
- Modify: `cmd/hotplex/mcp_config_test.go`
- Modify: `cmd/hotplex/routes.go`

**Interfaces:**
- Produces: authenticated Streamable HTTP MCP tools `agent_config_inspect`, `agent_config_plan`, `agent_config_apply`, and `agent_config_verify`.
- Consumes: official MCP Go SDK, Gateway session metadata, HMAC signer, `agentcontrol.Service`, and per-session Worker MCP configuration.

- [ ] **Step 1: Add failing token and tool-boundary tests**

Test token signature, expiry, session/user/workspace/bot binding, key rotation/restart invalidation, wrong audience, malformed token, and constant-time signature validation. Test that tools:

- expose exactly four names;
- derive scope from token claims and accept only canonical `file` plus operation fields;
- reject arbitrary workspace/bot/platform/path arguments;
- cannot approve a change;
- return `AGENT_CONFIG_APPROVAL_REQUIRED` until a host endpoint approves;
- never include token/config body in logs or MCP errors.

- [ ] **Step 2: Add the official SDK and confirm RED**

```bash
go get github.com/modelcontextprotocol/go-sdk@v1.7.0
go test ./internal/agentcontrol -run 'Token|MCP' -count=1
```

- [ ] **Step 3: Implement short-lived session tokens**

Sign a versioned canonical claim set with HMAC-SHA-256. Claims include audience, session ID, user ID, platform, current Workspace/Bot identity, issued-at, and expiry. The signing key is process-local cryptographic randomness, never persisted or logged; restart invalidates all tokens.

- [ ] **Step 4: Implement stateless Streamable HTTP MCP**

Use `mcp.NewStreamableHTTPHandler` in stateless JSON-response mode. Authenticate before the SDK handler, attach typed claims to context, cap request bodies, enforce loopback-origin deployment defaults, and apply Gateway HTTP timeouts. The MCP layer calls the same service methods as HTTP handlers.

- [ ] **Step 5: Extend MCP server config with headers**

Add `Headers map[string]string` to `config.MCPServerConfig` and validate header names/values. Inject a per-session server entry pointing to the internal MCP URL with `Authorization: Bearer <token>`. Implement and test remote MCP header preservation in all four Worker adapters; do not advertise the self-configuration surface for an adapter until this contract passes.

- [ ] **Step 6: Preserve existing MCP discovery semantics**

Refactor `buildMCPConfigJSON`/`Bridge.buildWorkerInfo` so adding the built-in server does not accidentally enable `StrictMCPConfig` when the operator had no configured MCP allowlist. Cron and sessions without a writable current Bot/Workspace receive no built-in entry. User-configured servers and their existing strictness retain current behavior.

- [ ] **Step 7: Mount and lifecycle-manage the endpoint**

Mount `/internal/mcp/agent-config` behind token middleware, wire the service through Gateway dependencies, and close/drain the handler during normal shutdown. Do not expose the endpoint through Admin cookie fallback.

- [ ] **Step 8: Run MCP, config, Bridge, and adapter tests**

```bash
go test ./internal/agentcontrol ./internal/config ./internal/gateway ./internal/worker/... ./cmd/hotplex -run 'MCP|AgentConfig|Token|Headers' -count=1 -race -shuffle=on
```

- [ ] **Step 9: Commit the session MCP adapter**

```bash
git add go.mod go.sum internal/agentcontrol internal/config internal/gateway internal/worker cmd/hotplex
git commit -m "feat(agentcontrol): expose session-bound MCP tools"
```

### Task 7: Add WebChat review and approval UX

**Files:**
- Create: `webchat/lib/api/agent-config-changes.ts`
- Create: `webchat/lib/api/agent-config-changes.test.ts`
- Create: `webchat/components/admin/agent-config-change-review.tsx`
- Create: `webchat/components/admin/agent-config-change-review.test.tsx`
- Modify: `webchat/components/admin/agent-config-editor.tsx`
- Modify: `webchat/app/components/chat/settings-modal/ai-config-tab.tsx`
- Modify: `webchat/app/components/chat/settings-modal/tab-panel.tsx`
- Modify: `webchat/app/admin/workspaces/page.tsx`
- Modify: `webchat/lib/types/admin.ts`
- Modify: `webchat/locales/en/admin.json`
- Modify: `webchat/locales/zh-CN/admin.json`

**Interfaces:**
- Produces: an authenticated human review surface showing exact target, base/content hashes, unified diff, expiry, and activation consequence before approval.
- Consumes: host-facing APIs from Task 5; never the MCP token.

- [ ] **Step 1: Add failing API and component tests**

Cover loading/expired/stale/approval-required/applied states, diff rendering, explicit approve then apply actions, Workspace owner vs Admin scope, and the `new_session_or_reset` activation result. Assert that the UI never sends approval automatically on page load or plan creation.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
cd webchat && pnpm exec vitest run lib/api/agent-config-changes.test.ts components/admin/agent-config-change-review.test.tsx
```

- [ ] **Step 3: Implement typed client and review UI**

Render canonical filename/scope, old/new hashes, full unified diff, expiry countdown, and warnings. Require a deliberate approve action followed by apply. On stale base, require re-inspection/re-plan; never offer force overwrite.

- [ ] **Step 4: Integrate verification and activation guidance**

After apply, call verify and show persisted/effective/activation/delivery separately. Offer the existing explicit reset action when authorized; do not reset automatically.

- [ ] **Step 5: Run WebChat gates**

```bash
cd webchat && pnpm exec tsc --noEmit
cd webchat && pnpm exec eslint lib/api/agent-config-changes.ts components/admin/agent-config-change-review.tsx
cd webchat && pnpm exec vitest run
```

- [ ] **Step 6: Commit the review UX**

```bash
git add webchat
git commit -m "feat(webchat): review agent config changes"
```

### Task 8: Document and security-verify the control plane

**Files:**
- Modify: `docs/architecture/Agent-Config-Design.md`
- Modify: `docs/explanation/agent-config-system.md`
- Modify: `docs/reference/admin-api.md`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/tutorials/agent-personality.md`
- Modify: `docs/swagger/swagger.json`

**Interfaces:**
- Produces: operator/user guidance for inspect → plan → host approve → apply → reset/new session → verify.
- Consumes: finalized HTTP/MCP schemas, error codes, scope matrix, activation semantics, and audit fields.

- [ ] **Step 1: Document the transaction and authority boundaries**

Explain that `META-COGNITION.md` teaches the workflow but grants no authority; MCP tools are the constrained mechanism; host approval is external to the model; platform/global remain Admin-only; and Skills are unrelated.

- [ ] **Step 2: Add abuse-case integration tests**

Add an integration table covering replayed change IDs, token replay across sessions/users, concurrent Workspace update, canonical/legacy collision, expired approval, oversized body, unknown file, path traversal, unauthorized platform/global scope, Gateway restart, MCP self-approval attempt, and unsupported system-prompt activation.

- [ ] **Step 3: Run dependency and leakage review**

Review the pinned official SDK release and `go mod` diff. Search logs, audit fixtures, HTTP/MCP errors, traces, snapshots, and metrics for private fixture text and bearer tokens. Verify that only authenticated inspect/plan responses and the review UI contain content/diffs.

- [ ] **Step 4: Run full project verification**

```bash
go test ./internal/agentcontrol ./internal/agentconfig ./internal/session ./internal/admin ./internal/gateway ./internal/worker/... ./cmd/hotplex -count=1 -race -shuffle=on
cd webchat && pnpm exec tsc --noEmit
cd webchat && pnpm exec vitest run
make docs-build
make lint
make test-short
```

Expected: all checks PASS without skipped security cases or disabled hooks.

- [ ] **Step 5: Commit documentation and generated artifacts**

```bash
git add docs internal webchat cmd go.mod go.sum
git commit -m "docs(agentcontrol): document safe self-configuration"
```

## Acceptance Criteria

- An Agent can inspect and plan only its current writable Bot or Workspace AgentConfig.
- No Agent/MCP call can approve its own plan or target platform/global scope.
- A host reviews the exact immutable proposal and approves it through an authenticated non-MCP channel.
- Apply is rejected on missing approval, expiry, stale base, invalid content, legacy collision, or lost authority.
- Files use durable atomic replacement; Workspace overrides use narrow SQLite/PostgreSQL CAS updates.
- Restart invalidates tokens and pending plans without leaving partial state.
- Verify reports persistence, effective resolution, activation, and system-prompt delivery as separate evidence.
- Audit is complete but body-free; config content, diffs, tokens, and raw errors do not leak.
- Activation requires explicit reset or a new session; no live context changes silently.
- Real Agent Skills behavior and naming remain unchanged.
