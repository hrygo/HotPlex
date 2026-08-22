# HotPlex Self-Awareness Phase A Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every live HotPlex Worker a small, trusted, versioned runtime-facts block and correct embedded metacognition/default AgentConfig templates without rewriting user files or expanding the Agent Skills wire contract.

**Architecture:** `internal/agentconfig` owns the facts contract, normalization, JSON rendering, and prompt schema v3. `internal/gateway` translates the selected `worker.Worker`, resolved permission mode, session scope, and injected Gateway environment into those facts; initial launch and `/reset` share that builder. Existing merged Skill catalog/dispatch receives regression verification, not redesign.

**Tech Stack:** Go 1.24, `encoding/json`, embedded Markdown, `testify/require`, table-driven parallel tests.

**Spec:** `docs/superpowers/specs/2026-08-22-hotplex-self-awareness-design.md`

## Global Constraints

- Phase A only: do not implement built-in Skill packages, native-root synchronization, `hotplex skills` commands, receipts, or legacy manual migration.
- Preserve `BuildSystemPrompt(configs *AgentConfigs) string`; it delegates with empty facts.
- Facts are declarations, not proof of external Worker enforcement.
- Facts never include identity values, work directory, Skill/tool catalogs, MCP configuration, credentials, arbitrary environment names, or environment values.
- Only allowlisted `GATEWAY_*` key names with non-empty values may appear.
- Native Workers retain ownership of Skill `name`/`description` disclosure; do not inject a duplicate Gateway catalog.
- Do not change existing AEP `SkillStatus` or `SkillEntry.source` semantics.
- Do not rewrite existing `~/.hotplex/agent-configs` files; template changes affect onboarding only.
- Follow table-driven TDD, `testify/require`, `t.Parallel()`, `gofmt`, and race-enabled package verification.

---

### Task 1: Runtime-facts contract and prompt schema v3

**Files:**

- Create: `internal/agentconfig/runtime_facts.go`
- Modify: `internal/agentconfig/prompt.go`
- Modify: `internal/agentconfig/prompt_test.go`
- Modify: `internal/agentconfig/loader_test.go`

**Interfaces:**

- Produces: `RuntimeFacts`, `RuntimeScopeKind`, `RuntimeCapability`, `RuntimeQuerySurface`, `SkillCatalogOwner`.
- Produces: `BuildSystemPromptWithRuntime(configs *AgentConfigs, facts RuntimeFacts) string`.
- Preserves: `BuildSystemPrompt(configs *AgentConfigs) string` and runtime-neutral Admin preview.

- [ ] **Step 1: Write failing prompt and normalization tests**

Use this representative input:

```go
facts := RuntimeFacts{
	SchemaVersion:             1,
	Platform:                  "slack",
	WorkerType:                "claude_code",
	ScopeKind:                 RuntimeScopeBot,
	DeclaredPermissionMode:    "workspace",
	DeclaredCapabilities:      []RuntimeCapability{CapabilityTools, CapabilityResume, CapabilityTools},
	DeclaredQuerySurfaces:     []RuntimeQuerySurface{QuerySkills, QueryMCP},
	DeclaredSkillCatalogOwner: SkillCatalogOwnerWorker,
	PresentGatewayEnvKeys:     []string{"GATEWAY_SESSION_ID", "GATEWAY_PLATFORM"},
}
prompt := BuildSystemPromptWithRuntime(&AgentConfigs{Tools: "Use current capabilities."}, facts)
require.Contains(t, prompt, `<agent-configuration schema-version="3">`)
require.Contains(t, prompt, `<runtime-facts format="application/json" schema-version="1">`)
require.Less(t, strings.Index(prompt, "<runtime-facts"), strings.Index(prompt, "<directives>"))
```

Also assert: empty facts omit the block; `BuildSystemPrompt(nil)` remains empty; arrays are sorted/de-duplicated; invalid enums are dropped; scalars and collections are bounded; control characters cannot create XML; repeated calls are byte-identical; neither Skill metadata nor environment values appear.

- [ ] **Step 2: Run red test**

Run: `go test ./internal/agentconfig -run 'TestBuildSystemPromptWithRuntime|TestRuntimeFacts' -count=1`

Expected: compile failure because the new contract does not exist.

- [ ] **Step 3: Implement the closed contract**

Define these portable values in `runtime_facts.go`:

```go
const (
	RuntimeFactsSchemaVersion = 1
	RuntimeScopeBot RuntimeScopeKind = "bot"
	RuntimeScopeWorkspace RuntimeScopeKind = "workspace"
	RuntimeScopeUnbound RuntimeScopeKind = "unbound"
	CapabilityResume RuntimeCapability = "resume"
	CapabilityStreaming RuntimeCapability = "streaming"
	CapabilityTools RuntimeCapability = "tools"
	QuerySkills RuntimeQuerySurface = "skills"
	QueryMCP RuntimeQuerySurface = "mcp"
	QueryWorker RuntimeQuerySurface = "worker"
	SkillCatalogOwnerWorker SkillCatalogOwner = "worker"
	SkillCatalogOwnerGateway SkillCatalogOwner = "gateway"
	SkillCatalogOwnerNone SkillCatalogOwner = "none"
)
```

Use JSON tags from the spec. Validate enum membership, sort/de-duplicate slices, cap collections at 16 entries and scalars at 128 UTF-8 bytes without splitting a rune, and use `encoding/json` plus XML escaping. Maps and arbitrary extensions are not allowed.

Refactor `BuildSystemPrompt` to call `BuildSystemPromptWithRuntime(configs, RuntimeFacts{})`. The facts block precedes directives, outer schema is `3`, and empty facts preserve existing directives/context content apart from the intentional outer version.

- [ ] **Step 4: Run green package test**

Run: `gofmt -w internal/agentconfig/runtime_facts.go internal/agentconfig/prompt.go internal/agentconfig/prompt_test.go internal/agentconfig/loader_test.go`

Run: `go test ./internal/agentconfig -count=1 -race -shuffle=on`

Expected: PASS.

- [ ] **Step 5: Commit**

Run: `git add internal/agentconfig/runtime_facts.go internal/agentconfig/prompt.go internal/agentconfig/prompt_test.go internal/agentconfig/loader_test.go`

Run: `git commit -m "feat(agentconfig): add trusted runtime facts"`

### Task 2: Gateway facts builder and launch/reset wiring

**Files:**

- Create: `internal/gateway/runtime_facts.go`
- Create: `internal/gateway/runtime_facts_test.go`
- Modify: `internal/gateway/bridge_worker.go`
- Modify: `internal/gateway/bridge.go`
- Modify: `internal/gateway/bridge_test.go`

**Interfaces:**

- Consumes: Task 1 facts and prompt builder.
- Produces: `buildRuntimeFacts(w worker.Worker, info worker.SessionInfo, platform string, scope agentconfig.RuntimeScopeKind) agentconfig.RuntimeFacts`.
- Produces: one runtime-aware `injectAgentConfig` path for initial launch and reset.

- [ ] **Step 1: Write failing builder tests**

Cover Claude Code, Codex CLI, OpenCode Server, ACP, and noop/fake Workers. Use an environment containing allowed keys and a secret-shaped non-allowlisted key:

```go
facts := buildRuntimeFacts(w, worker.SessionInfo{
	PermissionMode: "workspace",
	Env: map[string]string{
		"GATEWAY_PLATFORM": "slack",
		"GATEWAY_SESSION_ID": "private-session-value",
		"OPENAI_API_KEY": "must-not-appear",
	},
}, "slack", agentconfig.RuntimeScopeBot)
require.Equal(t, "claude_code", facts.WorkerType)
require.Equal(t, []string{"GATEWAY_PLATFORM", "GATEWAY_SESSION_ID"}, facts.PresentGatewayEnvKeys)
```

Assert capabilities come only from `worker.Capabilities`; unsupported capabilities are absent; declared query surfaces normalize to `mcp`, `skills`, `worker`; noop uses owner `none`; normal Workers use owner `worker`; no environment value survives.

- [ ] **Step 2: Write failing launch/reset tests**

Capture the prompt passed to Worker start and `SystemPromptUpdater`. Assert both contain platform/Worker/scope/permission declarations, reset reflects a changed resolved permission mode, and neither contains Bot/user/session/workspace IDs, work directory, channel values, or secret-shaped environment values.

- [ ] **Step 3: Run red tests**

Run: `go test ./internal/gateway -run 'TestBuildRuntimeFacts|TestBridge_InjectAgentConfig|TestResetSession_.*RuntimeFacts' -count=1`

Expected: compile failure because the builder and runtime-aware injection do not exist.

- [ ] **Step 4: Implement the translation boundary**

Use this exact allowlist and include a key only when `info.Env[key] != ""`:

```go
var gatewayRuntimeEnvKeys = [...]string{
	"GATEWAY_PLATFORM", "GATEWAY_BOT_ID", "GATEWAY_BOT_NAME",
	"GATEWAY_USER_ID", "GATEWAY_SESSION_ID", "GATEWAY_WORK_DIR",
	"GATEWAY_CHANNEL_ID", "GATEWAY_THREAD_ID", "GATEWAY_TEAM_ID",
}
```

Derive capabilities through `SupportsResume`, `SupportsStreaming`, and `SupportsTools`; never include tool names or modalities. Scope is workspace when `WorkspaceID` is non-empty, Bot when Bot identity is non-empty, otherwise unbound.

Change `injectAgentConfig` to receive facts and call `BuildSystemPromptWithRuntime`. In `createAndLaunchWorker`, build facts after `NewWorker` and before `startFn`. In `ResetSession`, rebuild key presence with the existing `injectGatewayContext` inputs, then rebuild facts from the current Worker/session. Do not call `prepareWorkerInfo` from reset because it has history-compression side effects.

- [ ] **Step 5: Run green Gateway test**

Run: `gofmt -w internal/gateway/runtime_facts.go internal/gateway/runtime_facts_test.go internal/gateway/bridge_worker.go internal/gateway/bridge.go internal/gateway/bridge_test.go`

Run: `go test ./internal/gateway -count=1 -race -shuffle=on`

Expected: PASS within the existing package budget.

- [ ] **Step 6: Commit**

Run: `git add internal/gateway/runtime_facts.go internal/gateway/runtime_facts_test.go internal/gateway/bridge_worker.go internal/gateway/bridge.go internal/gateway/bridge_test.go`

Run: `git commit -m "feat(gateway): inject declared session facts"`

### Task 3: Embedded META and five onboarding templates

**Files:**

- Modify: `internal/agentconfig/META-COGNITION.md`
- Modify: `internal/cli/onboard/templates/SOUL.md`
- Modify: `internal/cli/onboard/templates/AGENTS.md`
- Modify: `internal/cli/onboard/templates/TOOLS.md`
- Modify: `internal/cli/onboard/templates/USER.md`
- Modify: `internal/cli/onboard/templates/MEMORY.md`
- Modify: `internal/agentconfig/prompt_test.go`
- Create: `internal/cli/onboard/agentconfig_templates_test.go`

**Interfaces:**

- Consumes: Tasks 1–2 terminology.
- Produces: corrected embedded defaults for new sessions/onboarding only.

- [ ] **Step 1: Write failing content invariants**

Reject these stale claims:

```go
for _, forbidden := range []string{
	"非交互式后台运行", "权限请求 5 分钟", "最多 3 次",
	"Git commit / branch / merge", "安装任务所需依赖",
	"auto-managed by the agent", "真实 Agent Skills",
} {
	require.NotContains(t, joinedTemplates, forbidden)
}
```

Assert META/TOOLS cover five slots, per-file fallback, missing-inherits/present-empty-clears, directives-over-context, new Session or `/reset`, Gateway/Worker ownership, existing Skill statuses, Admin/WebChat Skills meaning Agent Skills, all Gateway commands, `hotplex-cli` routing, Cron post-create verification, and no unconditional capability promise.

- [ ] **Step 2: Run red test**

Run: `go test ./internal/agentconfig ./internal/cli/onboard -run 'Test.*(Meta|Template|DefaultTemplates)' -count=1`

Expected: FAIL on current timeout/retry, blanket authorization, auto-memory, and “真实 Agent Skills” text.

- [ ] **Step 3: Rewrite content minimally**

Keep META limited to stable identity, ownership, AgentConfig fallback/activation, capability surfaces, native Skill progressive disclosure, authority, verification, and degradation. Do not embed Skill catalogs or exhaustive CLI flags.

Make TOOLS the compact router from the spec. Keep SOUL persona-only, make AGENTS request-scoped, keep USER as preferences/examples without runtime assertions, and make MEMORY host-supplied read-only context. Increment every changed template frontmatter version.

- [ ] **Step 4: Run green tests**

Run: `go test ./internal/agentconfig ./internal/cli/onboard -count=1 -race -shuffle=on`

Expected: PASS.

- [ ] **Step 5: Commit**

Run: `git add internal/agentconfig/META-COGNITION.md internal/agentconfig/prompt_test.go internal/cli/onboard/templates internal/cli/onboard/agentconfig_templates_test.go`

Run: `git commit -m "docs(agentconfig): correct self-awareness defaults"`

### Task 4: Unify Skill callability across listing and dispatch

**Files:**

- Modify: `internal/gateway/handler.go`
- Modify: `internal/gateway/bridge.go`
- Modify: `internal/gateway/bridge_worker.go`
- Modify: `internal/gateway/skill_dispatch.go`
- Modify: `internal/gateway/worker_cmds.go`
- Modify: `internal/gateway/bridge_worker_test.go`
- Modify: `internal/gateway/skill_dispatch_handler_test.go`
- Modify: `internal/gateway/native_command_contract_test.go`
- Modify if required by shared helper coverage: `internal/gateway/skill_dispatch_test.go`

**Interfaces:**

- Preserves: `callable`, `discoverable`, `unavailable` wire values.
- Preserves: Gateway fixed > Worker authoritative > filesystem discovery precedence.
- Changes: every invocation entry point must consume the same evidence-based callability decision as `/skills`; filesystem discovery alone is never invocation authority.

- [ ] **Step 1: Add failing dispatch-parity tests**

Add focused tests proving:

- a filesystem-only Skill remains `discoverable` in `/skills` and both short `/name` and explicit `/worker <name>` reject it with `NOT_SUPPORTED`, including a native-invoker Worker that has no authoritative catalog provider;
- a Worker-advertised Skill remains callable through both short and explicit forms and uses the authoritative descriptor path/mode;
- authoritative catalog failure, stale replay metadata, and structured invocation cannot fall back to filesystem-only authority;
- ordinary non-slash text still skips catalog lookup;
- unknown, ambiguous, and shadowed slash names preserve bounded error behavior and never reach the Worker wire.

Update the native contract fixture: its short-form success case must be backed by Worker advertisement, never only by `shortFormPath` from the filesystem fixture.

Run the new focused tests before production edits and confirm the filesystem-only cases fail for the expected reason.

- [ ] **Step 2: Extract one shared callability classifier**

Refactor the existing `/skills` classification into a small shared helper that classifies a merged descriptor using its evidence origin, filesystem match, current Worker, and authoritative lookup result. `buildSkillEntriesFromCatalog` remains the wire mapper and delegates to that helper; do not introduce a new AEP enum or source value.

- Gateway fixed commands are callable only through their existing Gateway handler.
- Worker-advertised commands/Skills are callable when the authoritative lookup succeeds.
- adapter-verified native activation may be callable only when represented by explicit adapter evidence; `NativeInvoker` support or a filesystem path alone is insufficient.
- filesystem-only entries and any entry whose authoritative lookup cannot be confirmed are discoverable, not callable.

- [ ] **Step 3: Route short and explicit invocation through the shared decision**

Replace `resolveSkillForSession`'s direct filesystem resolution with merged session-catalog resolution. Preserve canonical and legacy compact parsing, but build the `NativeCommandInvocation` from the selected callable descriptor. A discoverable-only match returns `NOT_SUPPORTED` with bounded remediation guidance; it must not fall through as ordinary prompt text.

Apply the same classifier in `tryExplicitNativeCommand` before calling the native invoker. Keep the fixed-command reservation and capability gates unchanged.

Replay and structured invocation must revalidate against current session evidence before wire delivery. A stashed path is correlation data, not callability authority. Reuse the shared lookup/validation path where practical and keep the existing bounded catalog timeout.

- [ ] **Step 4: Run focused and full Skill suites**

Run: `go test ./internal/gateway -run 'Test(HandleInputKnownSkillRequiresWorkerAdvertisement|HandleInputFilesystemOnlySkill|ExplicitWorkerFilesystemOnlySkill|SkillsListEntriesClassifyMergedCatalog|NativeDispatchCoversThreeChannelsFourWorkers|HandleSkillsListLookupErrorNoCallable|Replay|Structured)' -count=1 -race -shuffle=on`

Expected: PASS. Adjust the regex only to the exact new test names if necessary; do not weaken the asserted cases.

Run: `go test ./internal/gateway -run 'Skill|NativeCatalog|NativeDispatch' -count=1 -race -shuffle=on`

Expected: PASS with no AEP enum/source changes.

- [ ] **Step 5: Commit**

Run: `git add internal/gateway/handler.go internal/gateway/skill_dispatch.go internal/gateway/worker_cmds.go internal/gateway/*skill*test.go internal/gateway/native_command_contract_test.go`

Run: `git commit -m "fix(gateway): enforce session skill callability"`

### Task 5: Current documentation and final verification

**Files:**

- Modify: `docs/explanation/agent-config-system.md`
- Modify: `docs/tutorials/agent-personality.md`
- Modify: `docs/tutorials/skills-setup.md`
- Modify: `docs/reference/configuration.md`

**Interfaces:**

- Documents only shipped Phase A behavior; Phase B remains unimplemented.

- [ ] **Step 1: Update current docs**

Document schema v3 facts, declared-versus-observed semantics, excluded sensitive values, native Worker ownership of Skill metadata disclosure, AgentConfig fallback and `/reset`, and corrected templates. Do not document `hotplex skills sync`, built-in packages, or operator profiles.

- [ ] **Step 2: Run documentation gate**

Run: `make docs-lint`

Expected: documentation build and link validation PASS.

- [ ] **Step 3: Run focused quality gates**

Run: `go test ./internal/agentconfig ./internal/cli/onboard ./internal/gateway -count=1 -race -shuffle=on`

Run: `make fmt`

Run: `git diff --check`

Run: `go vet ./...`

Run: `go mod verify`

Run: `make lint`

Run: `go build ./...`

Expected: all PASS; no formatter-generated diff remains.

- [ ] **Step 4: Commit docs**

Run: `git add docs/explanation/agent-config-system.md docs/tutorials/agent-personality.md docs/tutorials/skills-setup.md docs/reference/configuration.md`

Run: `git commit -m "docs: explain runtime self-awareness"`

- [ ] **Step 5: Final scope audit**

Verify the Phase A diff does not contain Phase B/C implementation or public AEP Skill changes; no private values entered tests/docs; both launch and reset share the facts builder; Admin preview is runtime-neutral; `git status --short` has no unrelated files.
