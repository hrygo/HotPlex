# System Prompt Delivery Capability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:test-driven-development and superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every Worker explicitly declare how HotPlex system prompts are delivered, negotiate the ACP extension before sending private AgentConfig content, and expose bounded delivery evidence without ever falling back to an ordinary user message.

**Architecture:** Add a required static `SystemPromptDeliveryMode` capability and a runtime `SystemPromptDeliveryStatus` projection to the Worker contract. Native adapters report delivery only after their native boundary accepts the prompt; ACP advertises and verifies a namespaced extension before invoking it. Bridge treats unsupported delivery as an explicit state, while Admin health, session debug, prompt preview, doctor, and WebChat display the same bounded vocabulary.

**Tech Stack:** Go 1.26, JSON-RPC 2.0/ACP v1 extension negotiation, testify/require, Next.js/TypeScript, React i18next, Markdown documentation.

**Design dependency:** `docs/superpowers/specs/2026-08-22-hotplex-agent-metacognition-tools-design.md`

## Contract and Safety Invariants

- Delivery modes are exactly `native`, `negotiated_extension`, and `unsupported`.
- Runtime states are exactly `not_requested`, `pending`, `delivered`, `unsupported`, and `failed`.
- `delivered` means the adapter successfully crossed its native transport boundary; it does not claim that the model obeyed the prompt.
- Claude Code uses `--append-system-prompt-file`, Codex app-server uses `baseInstructions`, and OpenCode Server uses its native `system` field.
- ACP sends a prompt only after both peers advertise `_hotplex.systemPrompt` version 1; the method is `_hotplex/session/set_system_prompt`.
- Full prompt content never appears in Worker health, Admin health, session debug, logs, metrics, error messages, or audit events. Only SHA-256 hashes and bounded status/error codes may leave the adapter.
- Unsupported or failed delivery never falls back to prefixing an ordinary user prompt.
- Empty prompt input is `not_requested`, not `delivered`.
- Reset reloads the prompt before creating the next Worker context and updates delivery status for that context.
- Existing real Agent Skills capability and dispatch contracts remain unchanged.

---

### Task 1: Define and freeze the Worker capability vocabulary

**Files:**
- Modify: `internal/worker/worker.go`
- Modify: `internal/worker/noop/worker.go`
- Modify: `internal/e2econtract/manifest.go`
- Modify: `internal/e2econtract/manifest_test.go`
- Modify: `internal/gateway/contracttest/worker_probe.go`
- Modify: `internal/gateway/contracttest/worker_probe_test.go`
- Modify: Worker mocks reported by the compiler under `internal/**/_test.go`

**Interfaces:**
- Produces: `SystemPromptDeliveryMode`, `SystemPromptDeliveryState`, `SystemPromptDeliveryStatus`, `Capabilities.SystemPromptDeliveryMode()`, and `WorkerHealth.SystemPromptDelivery`.
- Consumes: the embedded `Capabilities` contract already required by every `worker.Worker`.

- [ ] **Step 1: Add failing manifest and adapter contract tests**

Extend `WorkerProfile` with `SystemPromptDelivery` and freeze this matrix:

```go
claude_code      -> native
codex_cli        -> native
opencode_server  -> native
acp              -> negotiated_extension
```

Add a contract assertion that each concrete adapter's `SystemPromptDeliveryMode()` equals its manifest value. Add a noop assertion for `unsupported`.

- [ ] **Step 2: Run the focused tests and confirm RED**

```bash
go test ./internal/e2econtract ./internal/gateway/contracttest ./internal/worker/noop -run 'SystemPrompt|ExactCapabilities|CapabilitiesMatchAdapters' -count=1
```

Expected: compile/test failure because the capability types and methods do not exist.

- [ ] **Step 3: Add the shared types and validation**

Define typed constants in `internal/worker/worker.go` and reject unknown values in a small `ValidateSystemPromptDeliveryMode` helper. Add:

```go
type SystemPromptDeliveryStatus struct {
    Mode       SystemPromptDeliveryMode  `json:"mode"`
    State      SystemPromptDeliveryState `json:"state"`
    PromptHash string                    `json:"prompt_hash,omitempty"`
    ErrorCode  string                    `json:"error_code,omitempty"`
}
```

Add `SystemPromptDeliveryMode() SystemPromptDeliveryMode` to `Capabilities` and `SystemPromptDelivery SystemPromptDeliveryStatus` to `WorkerHealth`. Update all compile-time mocks rather than weakening the required interface.

- [ ] **Step 4: Implement noop and profile values**

Noop reports mode/state `unsupported`; the four production profiles report only their static mode at this stage.

- [ ] **Step 5: Run contract tests**

```bash
go test ./internal/worker/... ./internal/e2econtract ./internal/gateway/contracttest -run 'SystemPrompt|ExactCapabilities|CapabilitiesMatchAdapters' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit the shared contract**

```bash
git add internal/worker internal/e2econtract internal/gateway/contracttest
git commit -m "feat(worker): define system prompt delivery capability"
```

### Task 2: Record bounded delivery evidence in native adapters

**Files:**
- Modify: `internal/worker/base/worker.go`
- Modify: `internal/worker/claudecode/worker.go`
- Modify: `internal/worker/claudecode/worker_test.go`
- Modify: `internal/worker/codexcli/worker.go`
- Modify: `internal/worker/codexcli/worker_test.go`
- Modify: `internal/worker/opencodeserver/worker.go`
- Modify: `internal/worker/opencodeserver/worker_test.go`

**Interfaces:**
- Produces: concurrency-safe status transitions and SHA-256 prompt fingerprints.
- Consumes: each adapter's existing native system-prompt boundary and `WorkerHealth` response.

- [ ] **Step 1: Write failing state-transition tests**

For each native adapter prove:

- empty prompt yields `not_requested` and no hash;
- a non-empty prompt begins `pending`;
- successful native boundary acceptance yields `delivered` with the correct SHA-256 hash;
- a native boundary error yields `failed` with a stable bounded code and no content;
- `Health()` returns a snapshot safe under concurrent reads.

Add negative assertions that serialized health never contains a known private prompt fixture.

- [ ] **Step 2: Run focused native tests and confirm RED**

```bash
go test ./internal/worker/claudecode ./internal/worker/codexcli ./internal/worker/opencodeserver -run 'SystemPromptDelivery' -count=1 -race
```

- [ ] **Step 3: Add a reusable status holder**

Add a mutex-protected holder in `internal/worker/base/worker.go` with methods to set `not_requested`, `pending`, `delivered`, and `failed`. Hash the prompt once with SHA-256; never retain content in the status value and never accept arbitrary error text as `ErrorCode`.

- [ ] **Step 4: Wire Claude Code**

Set `pending` when `SessionInfo.SystemPrompt` is non-empty. Mark `delivered` only after the prompt temp file has been written and the process has successfully started with `--append-system-prompt-file` (or the explicit replace-file boundary). File/write/start failures use stable codes such as `prompt_file_failed` and `worker_start_failed`.

- [ ] **Step 5: Wire Codex app-server**

Mark `delivered` only after `thread/start` containing `baseInstructions` succeeds. A failed `thread/start` records `thread_start_failed`.

- [ ] **Step 6: Wire OpenCode Server**

The connection stores the native `system` field per message. Keep the status `pending` after session construction/update and mark it `delivered` only after an HTTP prompt request containing that field is accepted. A rejected request records `prompt_request_failed`.

- [ ] **Step 7: Run native adapter suites**

```bash
go test ./internal/worker/claudecode ./internal/worker/codexcli ./internal/worker/opencodeserver -count=1 -race -shuffle=on
```

Expected: PASS.

- [ ] **Step 8: Commit native delivery evidence**

```bash
git add internal/worker/base internal/worker/claudecode internal/worker/codexcli internal/worker/opencodeserver
git commit -m "feat(worker): report native system prompt delivery"
```

### Task 3: Negotiate and implement the ACP system-prompt extension

**Files:**
- Modify: `internal/worker/acp/client.go`
- Modify: `internal/worker/acp/client_test.go`
- Modify: `internal/worker/acp/worker.go`
- Modify: `internal/worker/acp/worker_enhance_test.go`
- Modify: `internal/worker/acp/worker_phase2_test.go`

**Interfaces:**
- Produces: explicit `_hotplex.systemPrompt` v1 capability negotiation and `_hotplex/session/set_system_prompt` JSON-RPC invocation.
- Consumes: `InitializeResult.AgentCapabilities`, the ACP session ID returned by new/load/fork, and the stored system prompt.

- [ ] **Step 1: Add handshake and leakage regression tests**

Cover all cases:

| Client advertises | Agent response | Expected |
|---|---|---|
| v1 | matching v1 | extension call, then `delivered` |
| v1 | missing | no extension call, `unsupported` |
| v1 | false/malformed | no extension call, `unsupported` |
| v1 | unsupported version | no extension call, `unsupported` |
| v1 | matching v1 but RPC fails | `failed`, session start/reset fails closed |

Retain and extend `TestACPFirstOrdinaryInputExcludesFullSystemPrompt`: neither negotiated nor unnegotiated paths may include the system prompt in `session/prompt` content.

- [ ] **Step 2: Run ACP tests and confirm RED**

```bash
go test ./internal/worker/acp -run 'SystemPrompt|Initialize|FirstOrdinaryInput' -count=1
```

- [ ] **Step 3: Advertise the client extension**

Send this exact namespaced capability in `initialize`:

```json
{
  "clientCapabilities": {
    "_hotplex.systemPrompt": {"version": 1}
  }
}
```

Do not alter ACP's standard `protocolVersion`.

- [ ] **Step 4: Parse the peer capability fail-closed**

Only the exact object form with numeric version `1` enables delivery. Missing, boolean, string, unknown version, or malformed data is `unsupported`; do not reuse the current permissive `supportsCapability` behavior for this private extension.

- [ ] **Step 5: Add the namespaced RPC method**

Add an ACP client method that sends:

```json
{
  "method": "_hotplex/session/set_system_prompt",
  "params": {
    "sessionId": "<acp-session-id>",
    "schemaVersion": 1,
    "prompt": "<full-system-prompt>"
  }
}
```

Invoke it after every successful new/load/fork session and after reset creates its replacement session. Never log params or response bodies.

- [ ] **Step 6: Define start/reset behavior**

- Empty prompt: continue with `not_requested`.
- Unnegotiated prompt: session may run, but state is explicitly `unsupported` and the stable code is `SYSTEM_PROMPT_UNSUPPORTED`.
- Negotiated RPC failure: fail the start/reset operation and record `SYSTEM_PROMPT_DELIVERY_FAILED`; do not run with a partially configured context.

- [ ] **Step 7: Run ACP suite with race detection**

```bash
go test ./internal/worker/acp -count=1 -race -shuffle=on
```

Expected: PASS, including the ordinary-input non-leakage tests.

- [ ] **Step 8: Commit the ACP extension**

```bash
git add internal/worker/acp
git commit -m "feat(acp): negotiate system prompt delivery extension"
```

### Task 4: Make Bridge orchestration capability-aware

**Files:**
- Modify: `internal/gateway/bridge_worker.go`
- Modify: `internal/gateway/bridge.go`
- Modify: `internal/gateway/bridge_test.go`

**Interfaces:**
- Produces: deterministic injection/reset behavior that never silently mistakes stored prompt text for delivered prompt state.
- Consumes: `Capabilities.SystemPromptDeliveryMode`, `SystemPromptUpdater`, `WorkerHealth.SystemPromptDelivery`, and `agentconfig.BuildSystemPrompt`.

- [ ] **Step 1: Add failing Bridge tests**

Prove that:

- start passes the assembled prompt once and preserves its hash;
- reset reloads AgentConfig, calls `UpdateSystemPrompt`, then resets the Worker;
- a Worker declaring `unsupported` remains observable and receives no ordinary-input fallback;
- a reset delivery failure is returned to the caller and does not report success;
- all-empty external AgentConfig still carries the embedded META prompt.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/gateway -run 'AgentConfig|ResetSession|SystemPromptDelivery' -count=1
```

- [ ] **Step 3: Enforce the orchestration order**

Keep prompt assembly Gateway-owned. For reset, load and validate config, update the Worker prompt, invoke `ResetContext`, then read the resulting status. Do not introduce a second prompt builder inside adapters.

- [ ] **Step 4: Surface stable failures**

Map unsupported/failure conditions to `SYSTEM_PROMPT_UNSUPPORTED` and `SYSTEM_PROMPT_DELIVERY_FAILED`. Return bounded errors without prompt content or raw backend bodies.

- [ ] **Step 5: Run Gateway tests**

```bash
go test ./internal/gateway -count=1 -race -shuffle=on
```

Expected: PASS.

- [ ] **Step 6: Commit Bridge orchestration**

```bash
git add internal/gateway
git commit -m "feat(gateway): enforce system prompt delivery state"
```

### Task 5: Expose delivery status through Admin, debug, preview, and doctor

**Files:**
- Modify: `internal/admin/models.go`
- Modify: `internal/admin/handlers.go`
- Modify: `internal/admin/handlers_test.go`
- Modify: `internal/admin/bot_config_provider.go`
- Modify: `internal/admin/bot_config_handlers.go`
- Modify: `internal/admin/bot_config_handlers_test.go`
- Modify: `cmd/hotplex/bot_config_adapter.go`
- Modify: `internal/cli/checkers/agentconfig.go`
- Modify: `internal/cli/checkers/agentconfig_test.go`

**Interfaces:**
- Produces: one shared public status schema on Worker health and session debug, plus an expected-delivery projection on prompt preview and static doctor diagnostics.
- Consumes: active Worker health, configured worker type, and assembled prompt hash.

- [ ] **Step 1: Add response-shape tests**

Require:

- `/admin/health/workers` includes each active Worker's runtime `system_prompt_delivery`;
- `/admin/debug/sessions/{id}` includes the same runtime object;
- `/admin/bots/{name}/preview` returns `preview`, `prompt_hash`, and `expected_delivery_mode`;
- preview never labels a prompt `delivered`, because it has no active transport evidence;
- doctor reports ACP as negotiated/unsupported based on runtime evidence when available, otherwise as `negotiated_extension` expected mode.

- [ ] **Step 2: Run focused tests and confirm RED**

```bash
go test ./internal/admin ./internal/cli/checkers ./cmd/hotplex -run 'SystemPrompt|WorkerHealth|DebugSession|Preview' -count=1
```

- [ ] **Step 3: Expand typed responses**

Replace the map-only preview response with a typed result carrying the assembled prompt, SHA-256 hash, and expected mode. Preserve the existing `preview` field for compatibility.

- [ ] **Step 4: Keep diagnostic claims evidence-bounded**

Health/debug may show runtime state. Preview and offline doctor may only show expected/static mode unless they have a live Worker status. No surface returns full prompt except the existing admin-only preview.

- [ ] **Step 5: Run Admin and checker suites**

```bash
go test ./internal/admin ./internal/cli/checkers ./cmd/hotplex -count=1 -race -shuffle=on
```

- [ ] **Step 6: Commit observability changes**

```bash
git add internal/admin internal/cli/checkers cmd/hotplex
git commit -m "feat(admin): expose system prompt delivery status"
```

### Task 6: Display delivery state in WebChat Admin

**Files:**
- Modify: `webchat/lib/types/admin.ts`
- Modify: `webchat/lib/api/admin-bots.ts`
- Modify: `webchat/components/admin/system-prompt-preview.tsx`
- Modify: `webchat/app/admin/sessions/detail/page.tsx`
- Create: `webchat/components/admin/system-prompt-preview.test.tsx`
- Create: `webchat/app/admin/sessions/detail/page.test.tsx`
- Modify: `webchat/locales/en/admin.json`
- Modify: `webchat/locales/zh-CN/admin.json`

**Interfaces:**
- Produces: user-visible expected/runtime delivery badges with explicit unsupported and failed states.
- Consumes: the typed Admin response from Task 5.

- [ ] **Step 1: Add failing component tests**

Cover native delivered, negotiated pending, unsupported, failed, and preview-only expected states. Assert that `delivered` is not rendered from preview data alone.

- [ ] **Step 2: Run focused UI tests and confirm RED**

```bash
cd webchat && pnpm exec vitest run components/admin/system-prompt-preview.test.tsx app/admin/sessions/detail/page.test.tsx
```

- [ ] **Step 3: Add TypeScript contracts and UI states**

Use a discriminated string union matching Go constants. Render mode, state, prompt hash prefix, and bounded error code; never render raw backend errors.

- [ ] **Step 4: Run WebChat verification**

```bash
cd webchat && pnpm exec tsc --noEmit
cd webchat && pnpm exec eslint lib/types/admin.ts lib/api/admin-bots.ts components/admin/system-prompt-preview.tsx app/admin/sessions/detail/page.tsx
cd webchat && pnpm exec vitest run
```

Expected: PASS.

- [ ] **Step 5: Commit WebChat status UX**

```bash
git add webchat
git commit -m "feat(webchat): show system prompt delivery state"
```

### Task 7: Document, verify, and release-gate the capability

**Files:**
- Modify: `docs/architecture/Agent-Config-Design.md`
- Modify: `docs/architecture/Worker-Gateway-Design.md`
- Modify: `docs/explanation/agent-config-system.md`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/specs/ACP-Worker-Spec.md`
- Modify: `docs/swagger/swagger.json`

**Interfaces:**
- Produces: operator guidance that distinguishes expected mode, observed delivery, and model compliance.
- Consumes: finalized status vocabulary and Admin response schema.

- [ ] **Step 1: Document the capability matrix and semantics**

State the three delivery modes, five runtime states, ACP negotiation contract, reset activation point, and the rule that ordinary user content is never a fallback transport.

- [ ] **Step 2: Run focused and full verification**

```bash
go test ./internal/worker/... ./internal/gateway ./internal/admin ./internal/e2econtract -count=1 -race -shuffle=on
cd webchat && pnpm exec tsc --noEmit
cd webchat && pnpm exec vitest run
make docs-build
make lint
make test-short
```

Expected: all checks PASS. If a known flaky test fails, reproduce it independently and rerun the original gate without disabling hooks or tests.

- [ ] **Step 3: Perform security review**

Search serialized responses, logs, errors, fixtures, and traces for the private prompt fixture. Verify ACP unnegotiated/malformed-version cases and every adapter failure path. Confirm no status uses raw error strings.

- [ ] **Step 4: Commit documentation and generated artifacts**

```bash
git add docs webchat internal cmd
git commit -m "docs(worker): document system prompt delivery capability"
```

## Acceptance Criteria

- Every Worker adapter and test mock compiles only after declaring a delivery mode.
- Native adapters report bounded runtime evidence from their actual transport boundary.
- ACP sends private AgentConfig content only after exact v1 bilateral negotiation.
- Unnegotiated ACP and unsupported workers never receive the prompt as ordinary user input.
- Start/reset failures are fail-closed where delivery was negotiated but failed.
- Admin health, session debug, preview, doctor, and WebChat use one consistent vocabulary.
- No public/log/audit surface leaks prompt content; only the existing admin-only preview may return it.
- Real Agent Skills APIs, UI, discovery, and AEP fields remain unchanged.
