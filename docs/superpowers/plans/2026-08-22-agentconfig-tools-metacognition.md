# AgentConfig TOOLS and Metacognition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the AgentConfig tool-guidance slot with canonical `TOOLS.md`, restore a stable always-on HotPlex metacognition kernel, and preserve the independent real Agent Skills system.

**Architecture:** Introduce a logical Tools slot with canonical and legacy basenames, resolve it through the existing per-scope fallback using explicit missing/empty/value states, and assemble it as `<tool-guidance>` rather than a Skill catalog. Keep `/admin/api/skills`, Worker Skills commands, and `SKILL.md` discovery unchanged while migrating only AgentConfig DTOs, editors, templates, diagnostics, and current documentation.

**Tech Stack:** Go 1.26, testify/require, Cobra diagnostics, Next.js/TypeScript, React i18next, Markdown documentation.

**Spec:** `docs/superpowers/specs/2026-08-22-hotplex-agent-metacognition-tools-design.md`

## Global Constraints

- Canonical AgentConfig files are exactly `SOUL.md`, `AGENTS.md`, `TOOLS.md`, `USER.md`, and `MEMORY.md`.
- `SKILLS.md` is a one-minor compatibility alias for the logical Tools slot; all new writes and templates use `TOOLS.md`.
- Scope precedence is bot/workspace → platform → global; within one scope `TOOLS.md` takes precedence over legacy `SKILLS.md`.
- Missing means inherit, present empty means explicitly clear and terminate fallback, and present non-empty means replace.
- `inject_exclude` accepts either Tools basename and excludes the one logical slot.
- `.agents/skills/**/SKILL.md`, `/admin/api/skills`, WebChat `/admin/skills`, Worker `/skills`, and AEP `skills` fields retain their existing meaning.
- Complete AgentConfig or META content must never be prefixed to an ordinary ACP user prompt.
- META contains no credentials, absolute runtime paths, fixed timeouts, issue numbers, or dynamic tool lists.
- Existing archive documents remain historical; update current architecture, reference, guide, and tutorial documents only.
- Every behavior change is test-first; Go tests use `require`, table-driven cases, `t.Parallel()` where isolation permits, and no `time.Sleep`.

---

### Task 1: Freeze the real Agent Skills contract

**Files:**
- Modify: `internal/admin/skill_handlers_test.go`
- Modify: `internal/skills/crud_test.go`
- Inspect only: `webchat/lib/api/admin-skills.ts`

**Interfaces:**
- Consumes: `AdminAPI.HandleListSkills(http.ResponseWriter, *http.Request)` and `skills.Locator.ListWorkspaceInstalled(context.Context, string)`.
- Produces: regression tests proving real Skills still serialize under `skills` and still use `.agents/skills/<name>/SKILL.md`.

- [ ] **Step 1: Add a response-shape regression test**

Add a focused assertion to `TestAdminAPI_HandleListSkills_PaginationAndSearch` after decoding the response:

```go
skillsList, ok := res["skills"].([]any)
require.True(t, ok)
require.NotNil(t, skillsList)
require.NotContains(t, res, "tools")
```

- [ ] **Step 2: Add a managed Skill path regression test**

In the workspace-installed CRUD test, retain the existing `SKILL.md` fixture and assert its returned path:

```go
require.Len(t, got, 1)
require.Equal(t, "SKILL.md", filepath.Base(got[0].Path))
require.Contains(t, got[0].Path, filepath.Join(".agents", "skills"))
```

- [ ] **Step 3: Run the regression tests**

Run:

```bash
go test ./internal/admin ./internal/skills -run 'TestAdminAPI_HandleListSkills|TestLocator_ListWorkspaceInstalled' -count=1
```

Expected: PASS before and after the AgentConfig migration.

- [ ] **Step 4: Commit the contract tests**

```bash
git add internal/admin/skill_handlers_test.go internal/skills/crud_test.go
git commit -m "test: freeze real agent skills contracts"
```

### Task 2: Add the logical Tools slot and three-state resolver

**Files:**
- Modify: `internal/agentconfig/loader.go`
- Modify: `internal/agentconfig/loader_test.go`
- Modify: `internal/agentconfig/validate.go`
- Modify: `internal/agentconfig/validate_test.go`
- Modify: `internal/agentconfig/writer.go`
- Modify: `internal/agentconfig/writer_test.go`

**Interfaces:**
- Produces: `FileTools`, `LegacyFileSkills`, `AgentConfigs.Tools`, canonical `KnownFiles()`, compatibility-aware `ValidateExcludeList`, `Load`, `LoadForWorkspace`, and source resolution.
- Consumes: existing `ValidateBotName`, `stripFrontmatter`, file-size budgets, platform/bot/global scope order.

- [ ] **Step 1: Write failing resolver-matrix tests**

Add table cases to `TestLoad` using isolated temp directories:

```go
tests := []struct {
	name       string
	files      map[string]string
	want       string
	wantLegacy bool
}{
	{name: "canonical tools", files: map[string]string{"TOOLS.md": "use native tools"}, want: "use native tools"},
	{name: "legacy fallback", files: map[string]string{"SKILLS.md": "legacy tool notes"}, want: "legacy tool notes", wantLegacy: true},
	{name: "canonical wins same scope", files: map[string]string{"TOOLS.md": "new", "SKILLS.md": "old"}, want: "new"},
	{name: "canonical empty masks legacy", files: map[string]string{"TOOLS.md": "", "SKILLS.md": "old"}, want: ""},
}
```

Add separate scope cases proving bot legacy beats platform canonical, bot canonical empty masks platform/global content, and both basenames in `inject_exclude` clear the logical slot.

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./internal/agentconfig -run 'TestLoad|TestShouldExclude|TestLoadWithInjectExclude' -count=1
```

Expected: FAIL because `TOOLS.md`, `AgentConfigs.Tools`, and explicit empty-state resolution do not exist.

- [ ] **Step 3: Define canonical names and resolution state**

Add to `loader.go`:

```go
const (
	FileSoul          = "SOUL.md"
	FileAgents        = "AGENTS.md"
	FileTools         = "TOOLS.md"
	LegacyFileSkills  = "SKILLS.md"
	FileUser          = "USER.md"
	FileMemory        = "MEMORY.md"
)

type AgentConfigs struct {
	Soul   string
	Agents string
	Tools  string
	User   string
	Memory string
}

type fileState struct {
	content string
	found   bool
	legacy  bool
}
```

Keep `KnownFiles()` canonical. Add an internal alias normalizer:

```go
func canonicalFileName(name string) (string, bool) {
	switch {
	case strings.EqualFold(name, LegacyFileSkills):
		return FileTools, true
	case slices.ContainsFunc(configFiles, func(v string) bool { return strings.EqualFold(v, name) }):
		for _, v := range configFiles {
			if strings.EqualFold(v, name) {
				return v, false
			}
		}
	}
	return "", false
}
```

- [ ] **Step 4: Implement presence-aware reads**

Replace the string-only internal read contract with:

```go
func readFileState(dir, name string) (fileState, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return fileState{}, nil
		}
		return fileState{}, fmt.Errorf("agentconfig: read %s: %w", name, err)
	}
	content := stripFrontmatter(string(data))
	if len(content) > MaxFileChars {
		content = content[:MaxFileChars]
	}
	return fileState{content: content, found: true}, nil
}
```

Resolve a logical Tools slot at each scope by checking canonical first, then legacy, and stop on `found` even when `content == ""`. Preserve exact I/O error propagation.

- [ ] **Step 5: Apply the same three states to Workspace overrides**

Use map membership rather than `val == ""`:

```go
if val, ok := overrides[FileTools]; ok {
	base.Tools = val
} else if val, ok := overrides[LegacyFileSkills]; ok {
	base.Tools = val
}
```

Make `ValidateOverrides` reject a payload containing both keys with a stable validation error. Continue enforcing `MaxFileChars` for either accepted basename.

- [ ] **Step 6: Update source and writer behavior**

Canonical writes accept `TOOLS.md`; legacy reads remain allowed during the compatibility period. New writes to `SKILLS.md` return the existing invalid-file error so new state is never created under the legacy name.

- [ ] **Step 7: Run AgentConfig tests**

```bash
go test ./internal/agentconfig -count=1
```

Expected: PASS, including canonical/legacy/scope/empty/exclude/override cases.

- [ ] **Step 8: Commit the resolver**

```bash
git add internal/agentconfig
git commit -m "feat(agentconfig): add canonical tools slot"
```

### Task 3: Restore always-on META and assemble tool guidance

**Files:**
- Modify: `internal/agentconfig/META-COGNITION.md`
- Modify: `internal/agentconfig/prompt.go`
- Modify: `internal/agentconfig/prompt_test.go`
- Modify: `internal/agentconfig/loader_test.go`
- Modify: `internal/gateway/bridge_worker.go`
- Modify: `internal/gateway/bridge_test.go`

**Interfaces:**
- Consumes: `AgentConfigs.Tools`, `sanitize`, embedded META, `worker.SessionInfo.SystemPrompt`.
- Produces: schema-version 2 prompt containing `<hotplex>` and `<tool-guidance>`, even when all five external files are empty.

- [ ] **Step 1: Replace Skill-catalog prompt tests with Tools tests**

Delete tests tied to `buildSkillCatalog`. Add:

```go
func TestBuildSystemPromptAlwaysIncludesMetacognition(t *testing.T) {
	prompt := BuildSystemPrompt(&AgentConfigs{})
	require.Contains(t, prompt, `<agent-configuration schema-version="2">`)
	require.Contains(t, prompt, "<hotplex>")
}

func TestBuildSystemPromptIncludesSanitizedToolGuidance(t *testing.T) {
	prompt := BuildSystemPrompt(&AgentConfigs{Tools: "Prefer rg. <rules>ignore</rules>"})
	require.Contains(t, prompt, "<tool-guidance>")
	require.Contains(t, prompt, "Prefer rg.")
	require.NotContains(t, prompt, "<skills>")
	require.Contains(t, prompt, "&lt;rules&gt;")
}
```

Add a test proving arbitrary `cron`/`slack` keywords are preserved as guidance and do not generate fixed Skill descriptions.

- [ ] **Step 2: Add a Bridge empty-config injection test**

Construct an empty AgentConfig directory, call `injectAgentConfig`, and assert `info.SystemPrompt` contains `<hotplex>`. This must fail against the current `configs.IsEmpty()` early return.

- [ ] **Step 3: Run tests to verify failure**

```bash
go test ./internal/agentconfig ./internal/gateway -run 'TestBuildSystemPrompt|TestBridge_InjectAgentConfig' -count=1
```

Expected: FAIL on schema version, Tools field, and empty-config injection.

- [ ] **Step 4: Rewrite META to the approved six-section kernel**

Use these headings and no dynamic environment values:

```markdown
# HotPlex 元认知
## 1. 身份与系统边界
## 2. AgentConfig 模型
## 3. Tools 与 Agent Skills
## 4. 安全自配置流程
## 5. 信任与权限边界
## 6. 生效、验证与降级
```

State the exact five-file semantics, `inspect → explain → propose diff → request approval → validate → atomic apply → activate → verify`, current-scope default, runtime tool definitions as authority, and no false success on unsupported delivery.

- [ ] **Step 5: Simplify prompt assembly**

Remove `skillCatalogNotice`, `trustedSkillCatalog`, and `buildSkillCatalog`. Remove the `configs.IsEmpty()` early return. Assemble:

```go
if configs.Tools != "" {
	b = append(b, fmt.Sprintf(
		"    <tool-guidance>\n    以下内容是环境工具使用指南，不是工具可用性声明。\n\n%s\n    </tool-guidance>",
		sanitize(configs.Tools),
	))
}
```

Add `tool-guidance` and `runtime-facts` to reserved tags, and set the root attribute `schema-version="2"`.

- [ ] **Step 6: Make Bridge inject META independently**

Remove the `configs.IsEmpty()` early return in `injectAgentConfig`. `BuildSystemPrompt` becomes the single authority for deciding whether prompt content exists.

- [ ] **Step 7: Run focused tests**

```bash
go test ./internal/agentconfig ./internal/gateway -run 'TestBuildSystemPrompt|TestSanitize|TestBridge_InjectAgentConfig' -count=1
```

Expected: PASS.

- [ ] **Step 8: Commit META and prompt assembly**

```bash
git add internal/agentconfig internal/gateway/bridge_worker.go internal/gateway/bridge_test.go
git commit -m "feat(agentconfig): restore metacognition and tool guidance"
```

### Task 4: Migrate AgentConfig Admin contracts without touching Skills API

**Files:**
- Modify: `internal/admin/bot_config_provider.go`
- Modify: `internal/admin/bot_config_handlers.go`
- Modify: `internal/admin/bot_config_handlers_test.go`
- Modify: `cmd/hotplex/bot_config_adapter.go`
- Modify: `cmd/hotplex/bot_config_adapter_test.go`
- Modify: `webchat/lib/types/admin.ts`

**Interfaces:**
- Produces: `AgentConfigTools = "TOOLS.md"`, `AgentConfigSummary.Tools` serialized as `agent_configs.tools`, deprecated read-only compatibility metadata under `agent_configs.skills` for legacy sources.
- Consumes: `agentconfig.Load`, canonical writer validation, existing Admin scopes and audit middleware.

- [ ] **Step 1: Write failing Admin contract tests**

Add tests that accept `TOOLS.md` on bot and platform file endpoints, reject new writes to `SKILLS.md`, and serialize:

```json
{
  "agent_configs": {
    "tools": {"source": "bot", "size": 12}
  }
}
```

Retain the Task 1 assertion that `/admin/api/skills` has a top-level `skills` field and no `tools` field.

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./internal/admin ./cmd/hotplex -run 'AgentConfig|PlatformAgentConfig|HandleListSkills' -count=1
```

Expected: FAIL because the whitelist and summary still use `SKILLS.md`/`skills`.

- [ ] **Step 3: Migrate AgentConfig-specific DTOs**

Use:

```go
const AgentConfigTools AgentConfigFileName = "TOOLS.md"

type AgentConfigSummary struct {
	Soul         *AgentConfigMeta `json:"soul,omitempty"`
	Agents       *AgentConfigMeta `json:"agents,omitempty"`
	Tools        *AgentConfigMeta `json:"tools,omitempty"`
	LegacySkills *AgentConfigMeta `json:"skills,omitempty"` // deprecated AgentConfig alias only
	User         *AgentConfigMeta `json:"user,omitempty"`
	Memory       *AgentConfigMeta `json:"memory,omitempty"`
}
```

Populate `LegacySkills` only when the effective Tools value came from the legacy basename; never populate it from the real Skills locator.

- [ ] **Step 4: Update handler documentation and TypeScript types**

OpenAPI enums list `TOOLS.md`; document `SKILLS.md` only as a read-compatible deprecated basename if the read endpoint supports it. Update `AgentConfigSummary` in TypeScript with `tools?: AgentConfigMeta` and deprecated `skills?: AgentConfigMeta`.

- [ ] **Step 5: Run Admin and adapter tests**

```bash
go test ./internal/admin ./cmd/hotplex -run 'AgentConfig|PlatformAgentConfig|HandleListSkills' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit Admin contracts**

```bash
git add internal/admin cmd/hotplex/bot_config_adapter.go cmd/hotplex/bot_config_adapter_test.go webchat/lib/types/admin.ts
git commit -m "feat(admin): expose tools agent config slot"
```

### Task 5: Migrate AgentConfig editors and templates

**Files:**
- Modify: `webchat/components/admin/agent-config-file-list.tsx`
- Modify: `webchat/components/admin/agent-config-editor.tsx`
- Modify: `webchat/components/admin/bot-config-editor.tsx`
- Modify: `webchat/components/admin/channel-config-editor.tsx`
- Modify: `webchat/locales/en/admin.json`
- Modify: `webchat/locales/zh-CN/admin.json`
- Modify: `internal/cli/onboard/agentconfig_templates.go`
- Rename: `internal/cli/onboard/templates/SKILLS.md` → `internal/cli/onboard/templates/TOOLS.md`
- Modify: `cmd/hotplex/onboard.go`

**Interfaces:**
- Consumes: canonical `TOOLS.md` AgentConfig API.
- Produces: new editors and onboard runs only write `TOOLS.md`; independent Skills pages/locales remain unchanged.

- [ ] **Step 1: Add or update UI/config template tests**

Where no component test exists, use the TypeScript compiler plus Go embed test. Update `TestStepAgentConfig` to assert `TOOLS.md` exists and `SKILLS.md` is not newly generated.

- [ ] **Step 2: Run the template test to verify failure**

```bash
go test ./internal/cli/onboard -run TestStepAgentConfig -count=1
```

Expected: FAIL because the embed list still generates `SKILLS.md`.

- [ ] **Step 3: Rename the template and UI key**

Set the fifth file definition to:

```ts
{ key: 'tools', file: 'TOOLS.md', label: 'Tools', description: 'Tool usage guidance' }
```

Replace only `admin:bots.config_files.skills` references in AgentConfig editors with `admin:bots.config_files.tools`. Do not edit `admin:skills`, `chat:skills`, the Admin Skills page, or Skills API types.

- [ ] **Step 4: Preserve legacy Workspace overrides on edit**

When opening an existing override map, normalize `SKILLS.md` to the `TOOLS.md` editor only if `TOOLS.md` is absent. Saving writes `TOOLS.md` and removes the legacy key from the submitted override object; if both are present, show the server conflict instead of silently merging.

- [ ] **Step 5: Run Go and WebChat checks**

```bash
go test ./internal/cli/onboard ./cmd/hotplex -run 'AgentConfig|Onboard' -count=1
cd webchat && pnpm typecheck && pnpm test --run
```

Expected: PASS.

- [ ] **Step 6: Commit UI and templates**

```bash
git add webchat/components/admin webchat/locales internal/cli/onboard cmd/hotplex/onboard.go
git commit -m "feat(agentconfig): migrate editors and templates to tools"
```

### Task 6: Add doctor diagnostics for legacy and empty-state migration

**Files:**
- Modify: `internal/cli/checkers/agentconfig.go`
- Modify: `internal/cli/checkers/agentconfig_test.go`

**Interfaces:**
- Produces: read-only warnings for legacy `SKILLS.md`, same-scope basename collision, and present-empty files whose semantics change to explicit clear.
- Consumes: AgentConfig directory traversal and existing `cli.CheckResult` statuses.

- [ ] **Step 1: Add failing checker cases**

Create fixtures for:

```text
SKILLS.md only                      → WARN legacy basename
TOOLS.md + SKILLS.md same scope     → WARN collision; TOOLS wins
TOOLS.md present and empty          → WARN explicit-clear migration
SOUL.md only                        → PASS
```

Assert messages identify relative scope and basename without printing file contents.

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./internal/cli/checkers -run AgentConfig -count=1
```

Expected: FAIL because legacy/collision/empty diagnostics do not exist.

- [ ] **Step 3: Implement bounded diagnostics**

Extend the existing directory checker to classify basenames, use `os.Stat`/bounded reads, and return actionable FixHint text directing users to create `TOOLS.md` and preserve a backup. Do not implement destructive migration in this core plan.

- [ ] **Step 4: Run checker tests**

```bash
go test ./internal/cli/checkers -run AgentConfig -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit diagnostics**

```bash
git add internal/cli/checkers
git commit -m "feat(doctor): diagnose legacy agent tools config"
```

### Task 7: Update current documentation and verify the core migration

**Files:**
- Modify: `docs/architecture/Agent-Config-Design.md`
- Modify: `docs/explanation/agent-config-system.md`
- Modify: `docs/guides/contributor/architecture.md`
- Modify: `docs/guides/developer/context-window.md`
- Modify: `docs/guides/enterprise/multi-tenant.md`
- Modify: `docs/reference/admin-api.md`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/reference/glossary.md`
- Modify: `docs/tutorials/agent-personality.md`
- Modify: `docs/specs/Per-Bot-Agent-Config-Spec.md`
- Modify: `docs/specs/WebChat-Multitenancy-PerWorkspace-AgentConfigs-Design-Spec.md`
- Do not modify: `docs/archive/**`

**Interfaces:**
- Consumes: implemented canonical names and compatibility behavior.
- Produces: current documentation consistently distinguishes AgentConfig Tools from Agent Skills.

- [ ] **Step 1: Update current documents semantically**

Describe `TOOLS.md` as tool guidance, `SKILL.md` as real on-demand Skills, the three-state resolver, one-minor legacy compatibility, session/reset activation, and META-always-on behavior. Historical archives retain their original terminology.

- [ ] **Step 2: Run exhaustive terminology audit**

```bash
rg -n 'SKILLS\.md|<skills>|AgentConfigSkills|configs\.Skills' internal cmd webchat docs configs \
  --glob '!docs/archive/**' \
  --glob '!docs/superpowers/plans/**' \
  --glob '!docs/superpowers/specs/2026-08-22-hotplex-agent-metacognition-tools-design.md'
```

Expected: remaining hits are explicitly marked legacy-compatibility tests/docs or belong to true Skills fields; no current document calls AgentConfig Tools a Skill catalog.

- [ ] **Step 3: Run focused quality gates**

```bash
gofmt -w internal/agentconfig internal/admin internal/cli/checkers internal/cli/onboard cmd/hotplex
go test ./internal/agentconfig ./internal/admin ./internal/skills ./internal/cli/checkers ./internal/cli/onboard ./internal/gateway ./cmd/hotplex -count=1
cd webchat && pnpm typecheck && pnpm test --run
git diff --check
```

Expected: PASS.

- [ ] **Step 4: Run full repository gates**

```bash
make lint
make test-short
```

Expected: PASS. If the known Slack race reproduces, capture the exact race stacks and keep it separate from AgentConfig changes; do not bypass the hook.

- [ ] **Step 5: Commit documentation and final core integration**

```bash
git add docs internal cmd webchat configs
git commit -m "docs: distinguish agent tools from skills"
```

- [ ] **Step 6: Push the branch after all hooks pass**

```bash
git push origin fix/turn-integrity-init-reliability
```

Expected: pre-push validation passes and the upstream branch reaches the local HEAD.

## Self-Review Results

- Spec coverage: this plan covers the core Tools rename, legacy resolver, META restoration, prompt schema, Admin/WebChat AgentConfig boundaries, templates, diagnostics, current docs, and real Skills regression contracts.
- Deferred by intentional decomposition: Worker-wide `SystemPromptDelivery` capability negotiation and the `inspect/plan/apply/verify` self-configuration control plane each require their own implementation plan after this core plan lands.
- Placeholder scan: no deferred implementation markers or unspecified error-handling steps remain in this plan.
- Type consistency: canonical names are `FileTools`, `LegacyFileSkills`, `AgentConfigs.Tools`, `AgentConfigTools`, `AgentConfigSummary.Tools`, UI key `tools`, and XML tag `tool-guidance` throughout.
