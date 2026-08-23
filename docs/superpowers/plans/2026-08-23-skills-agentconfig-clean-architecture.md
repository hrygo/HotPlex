# Skills and AgentConfig Clean Architecture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove all legacy Skills/AgentConfig compatibility behavior in the approved scope and reduce `.agents/skills` to five non-overlapping, verifiable Skills.

**Architecture:** AgentConfig accepts only five canonical filenames and resolves only bot, platform, and global exact paths. Real Skills remain `<name>/SKILL.md` packages; embedded `hotplex-cli` and `hotplex-operator` are canonical and atomically mirrored into `.agents/skills`, while three repository-only specialist Skills remain independently maintained.

**Tech Stack:** Go 1.26, Cobra, testify/require, Next.js/TypeScript, Vitest, YAML frontmatter, Markdown documentation generator.

**Spec:** `docs/superpowers/specs/2026-08-23-skills-agentconfig-clean-architecture-design.md`

## Global Constraints

- Remove compatibility only in the approved Skills/AgentConfig scope; preserve Worker adapters, AEP behavior, and Linux/macOS/Windows support.
- Do not migrate, delete, or rewrite user-owned legacy files.
- AgentConfig recognizes only `SOUL.md`, `AGENTS.md`, `TOOLS.md`, `USER.md`, and `MEMORY.md`.
- A real Agent Skill is recognized only as `<skills-root>/<name>/SKILL.md`.
- `internal/skills/builtin` is the only source for the two embedded built-in packages.
- Follow TDD for every behavioral change and commit each independently reviewable task.
- Current CLI help and source are authoritative; do not hard-code mutable command counts, version locations, or line numbers.
- Remote pushes, tags, Issues, PRs, releases, Admin mutations, and host mutations require the authority already present in the active user request.

---

### Task 1: Make AgentConfig Core Strictly Canonical

**Files:**
- Modify: `internal/agentconfig/loader.go`
- Modify: `internal/agentconfig/writer.go`
- Modify: `internal/agentconfig/validate.go`
- Modify: `internal/agentconfig/loader_test.go`
- Modify: `internal/agentconfig/writer_test.go`
- Modify: `internal/agentconfig/validate_test.go`
- Modify: `internal/agentconfig/AGENTS.md`

**Interfaces:**
- Consumes: existing `configFiles`, `resolveFile`, `ValidateOverrides`, `ResolvedLocation`, and `WriteFile` APIs.
- Produces: exact canonical filename validation and the unchanged three-scope precedence `bot -> platform -> global`.

- [ ] **Step 1: Replace compatibility-positive tests with strict failing contracts**

Add or update tests so the expected behavior is explicit:

```go
func TestLoadIgnoresLegacySkillsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "SKILLS.md", "legacy tools")

	cfg, err := Load(dir, "", "")
	require.NoError(t, err)
	require.Empty(t, cfg.Tools)
}

func TestLoadIgnoresLegacyDefaultDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, dir, "slack/default/TOOLS.md", "legacy tools")

	cfg, err := Load(dir, "slack", "")
	require.NoError(t, err)
	require.Empty(t, cfg.Tools)
}

func TestValidateOverridesRejectsSkillsFilename(t *testing.T) {
	t.Parallel()
	_, err := ValidateOverrides(`{"SKILLS.md":"legacy"}`)
	require.ErrorIs(t, err, ErrUnknownConfigFile)
}
```

Change `inject_exclude` expectations so `SKILLS.md` does not exclude `TOOLS.md`.

- [ ] **Step 2: Run the focused tests and verify the legacy contracts fail**

Run:

```bash
go test ./internal/agentconfig -run 'Test(LoadIgnoresLegacy|ValidateOverridesRejectsSkills|LoadWithInjectExclude|ResolvedSource)' -count=1
```

Expected: failures show that `SKILLS.md` and `platform/default/` are still read or normalized.

- [ ] **Step 3: Remove core compatibility branches**

Make these concrete changes:

```go
const (
	FileSoul   = "SOUL.md"
	FileAgents = "AGENTS.md"
	FileTools  = "TOOLS.md"
	FileUser   = "USER.md"
	FileMemory = "MEMORY.md"
)
```

- Delete `LegacyDefaultBotName`, `LegacyFileSkills`, `fileState.legacy`, `readAliases`, and legacy logging.
- Make `readLogicalFile` read exactly the requested canonical basename once.
- Remove the `platform/default/` branch from `resolveFile` and `ResolvedLocation`.
- Make `canonicalFileName` recognize only entries in `configFiles`; do not map aliases.
- Remove `ErrConflictingConfigFiles` and its special validation branches.
- Keep present-empty semantics, size limits, path validation, atomic writes, and bot/platform/global ordering unchanged.

- [ ] **Step 4: Run AgentConfig tests**

Run:

```bash
go test ./internal/agentconfig -count=1 -race -shuffle=on
```

Expected: PASS.

- [ ] **Step 5: Commit the strict core**

```bash
git add internal/agentconfig
git commit -m "refactor(agentconfig): remove legacy compatibility"
```

---

### Task 2: Remove Legacy AgentConfig API and WebChat Shapes

**Files:**
- Modify: `internal/admin/bot_config_provider.go`
- Modify: `internal/admin/bot_config_handlers.go`
- Modify: `internal/admin/bot_config_handlers_test.go`
- Modify: `cmd/hotplex/bot_config_adapter.go`
- Modify: `cmd/hotplex/bot_config_adapter_test.go`
- Modify: `cmd/hotplex/gateway_run_agentconfig_test.go`
- Modify: `webchat/lib/agent-config-overrides.ts`
- Modify: `webchat/lib/agent-config-overrides.test.ts`
- Modify: `webchat/lib/types/admin.ts`
- Modify: `webchat/components/admin/agent-config-editor.tsx`
- Modify: `internal/cli/checkers/agentconfig.go`
- Modify: `internal/cli/checkers/agentconfig_test.go`
- Modify: `internal/cli/onboard/wizard_coverage_test.go`

**Interfaces:**
- Consumes: strict canonical functions from Task 1.
- Produces: Admin and WebChat contracts containing only `soul`, `agents`, `tools`, `user`, and `memory` AgentConfig metadata.

- [ ] **Step 1: Write strict Admin and WebChat tests**

Add Admin handler coverage equivalent to:

```go
func TestGetAgentConfigFileRejectsSkillsAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin/bots/demo/config/SKILLS.md", nil)
	req.SetPathValue("name", "demo")
	req.SetPathValue("file", "SKILLS.md")
	rr := httptest.NewRecorder()

	handler.HandleGetAgentConfigFile(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
}
```

Replace WebChat normalization tests with a canonical filter contract:

```ts
expect(prepareAgentConfigOverrides({
  'TOOLS.md': 'canonical',
  'SKILLS.md': 'legacy',
})).toEqual({ 'TOOLS.md': 'canonical' });
```

- [ ] **Step 2: Run focused tests and verify failure**

Run:

```bash
go test ./internal/admin ./internal/cli/checkers ./cmd/hotplex -run 'Test.*(AgentConfig|SkillsAlias|Suffix)' -count=1
```

```bash
pnpm exec vitest run lib/agent-config-overrides.test.ts
```

Expected: legacy API/type/normalization behavior causes at least one failure before implementation.

- [ ] **Step 3: Simplify public contracts**

- Delete `AgentConfigLegacySkills`, `AgentConfigSummary.LegacySkills`, and `IsReadableConfigFile` alias behavior.
- Use `ValidConfigFiles` for both reads and writes.
- Remove physical legacy basename handling from `bot_config_adapter` summaries and getters.
- Remove deprecated `skills` from TypeScript `AgentConfigSummary`.
- Delete `normalizeLegacyToolsOverride`; initialize the editor directly from `prepareAgentConfigOverrides`.
- Make `prepareAgentConfigOverrides` copy only the five canonical keys.
- Remove doctor checks, registry entries, and fix hints dedicated to `SKILLS.md` and deprecated suffix files.
- Delete `warnDeprecatedSuffixFiles`, its Gateway call site, `agentConfigSuffixChecker`, and their tests; the source audit confirmed this path exists only for unsupported suffix-layout migration guidance.

- [ ] **Step 4: Run focused Go and WebChat gates**

Run:

```bash
go test ./internal/admin ./internal/cli/checkers ./cmd/hotplex -count=1 -race -shuffle=on
```

```bash
pnpm exec vitest run lib/agent-config-overrides.test.ts
pnpm exec tsc --noEmit
```

Expected: PASS.

- [ ] **Step 5: Commit the contract cleanup**

```bash
git add internal/admin internal/cli/checkers internal/cli/onboard cmd/hotplex webchat/lib webchat/components/admin/agent-config-editor.tsx
git commit -m "refactor(agentconfig): expose canonical API only"
```

---

### Task 3: Remove Flat Pseudo-Skill Publishers

**Files:**
- Delete: `internal/cron/skill.go`
- Delete: `internal/cron/cron-skill-manual.md`
- Modify: `internal/cron/cron.go`
- Delete: `internal/messaging/phrases/skill.go`
- Delete: `internal/messaging/phrases/phrases.md`
- Modify: `cmd/hotplex/messaging_init.go`
- Delete: `internal/dbutil/skill.go`
- Delete: `internal/dbutil/db-stats-skill-manual.md`
- Modify: `cmd/hotplex/gateway_run.go`
- Create: `internal/skills/legacy_publishers_test.go`

**Interfaces:**
- Consumes: canonical built-in Cron reference already embedded in `hotplex-cli`.
- Produces: startup paths with no writes of flat Markdown files under `$HOTPLEX_HOME/skills`.

- [ ] **Step 1: Add a source-level publisher boundary test**

Create `internal/skills/legacy_publishers_test.go`. Locate the repository root from the test file and assert the legacy assets are absent:

```go
for _, rel := range []string{
	"internal/cron/skill.go",
	"internal/cron/cron-skill-manual.md",
	"internal/messaging/phrases/skill.go",
	"internal/messaging/phrases/phrases.md",
	"internal/dbutil/skill.go",
	"internal/dbutil/db-stats-skill-manual.md",
} {
	require.NoFileExists(t, filepath.Join(repoRoot, rel))
}
```

Read `internal/cron/cron.go`, `cmd/hotplex/messaging_init.go`, and `cmd/hotplex/gateway_run.go`; assert they do not contain `ReleaseSkillManual`, `releaseDBStatsManual`, or `SkillManual()` calls. This protects the architectural boundary without adding a production no-op.

- [ ] **Step 2: Run focused tests and verify legacy writes are observable**

Run:

```bash
go test ./internal/cron ./internal/messaging/phrases ./cmd/hotplex -run 'Test.*(SkillManual|FlatManual|Startup)' -count=1
```

Expected: FAIL because legacy publisher files and call sites still exist.

- [ ] **Step 3: Delete publishers and call sites**

- Remove `ReleaseSkillManual` from Cron startup.
- Remove `phrases.ReleaseSkillManual` from messaging initialization.
- Remove `releaseDBStatsManual` and its Gateway startup call.
- Delete embed-only wrappers and manual assets listed above.
- Remove imports that existed only for these publishers.
- Keep `$HOTPLEX_HOME/skills` as the legitimate global real-Skill root; do not delete or clean it.

- [ ] **Step 4: Prove production source has no legacy publisher**

Run:

```bash
rg -n 'ReleaseSkillManual|SkillManual\(|cron\.md|phrases\.md|db-stats\.md' internal cmd --glob '*.go' --glob '!**/*_test.go'
```

Expected: no production match. Test fixtures may name prohibited artifacts only to assert their absence.

Run:

```bash
go test ./internal/cron ./internal/messaging/phrases ./internal/dbutil ./cmd/hotplex -count=1 -race -shuffle=on
```

Expected: PASS.

- [ ] **Step 5: Commit publisher removal**

```bash
git add internal/cron internal/messaging/phrases internal/dbutil cmd/hotplex
git commit -m "refactor(skills): remove flat manual publishers"
```

---

### Task 4: Generate Both Built-in Repository Mirrors

**Files:**
- Modify: `cmd/gen-builtin-skills/main.go`
- Modify: `cmd/gen-builtin-skills/main_test.go`
- Modify: `internal/skills/builtin/manifest_test.go`
- Create generated tree: `.agents/skills/hotplex-operator/`
- Regenerate: `.agents/skills/hotplex-cli/`
- Regenerate: `internal/skills/builtin/manifest.generated.go`

**Interfaces:**
- Consumes: canonical `packageSpecs` for `hotplex-cli` and `hotplex-operator`.
- Produces: `mirrorPackage(sourceRoot, targetRoot) error` applied atomically to both package names.

- [ ] **Step 1: Write a two-mirror failing generator test**

Change the test configuration to use a mirror parent and assert both trees:

```go
require.FileExists(t, filepath.Join(mirrorRoot, "hotplex-cli", "SKILL.md"))
require.FileExists(t, filepath.Join(mirrorRoot, "hotplex-operator", "SKILL.md"))
requireTreesEqual(t,
	filepath.Join(canonicalRoot, "hotplex-cli"),
	filepath.Join(mirrorRoot, "hotplex-cli"),
)
requireTreesEqual(t,
	filepath.Join(canonicalRoot, "hotplex-operator"),
	filepath.Join(mirrorRoot, "hotplex-operator"),
)
```

Add an atomic failure test proving a failed mirror replacement preserves that package's previous complete target. Each package update is independently atomic; no target may contain a partial tree.

- [ ] **Step 2: Run generator tests and verify failure**

Run:

```bash
go test ./cmd/gen-builtin-skills -count=1
```

Expected: FAIL because only `hotplex-cli` is mirrored.

- [ ] **Step 3: Generalize generator configuration**

Replace the single-package mirror contract with a parent directory:

```go
type generatorConfig struct {
	canonicalRoot  string
	manifestOutput string
	mirrorRoot     string // parent containing one directory per mirrored package
}

for _, spec := range packageSpecs {
	source := filepath.Join(canonicalRoot, spec.name)
	target := filepath.Join(mirrorRoot, spec.name)
	if err := mirrorPackage(source, target); err != nil {
		return fmt.Errorf("mirror %s: %w", spec.name, err)
	}
}
```

Set the default `--mirror` to `.agents/skills`. Preserve atomic replace and rollback behavior.

- [ ] **Step 4: Regenerate twice and prove idempotence**

Run twice:

```bash
go run ./cmd/gen-builtin-skills
```

Then run:

```bash
go test ./cmd/gen-builtin-skills ./internal/skills/builtin -count=1
git diff --check
```

Expected: tests pass and the second generator run introduces no additional diff.

- [ ] **Step 5: Commit two-mirror generation**

```bash
git add cmd/gen-builtin-skills internal/skills/builtin/manifest.generated.go .agents/skills/hotplex-cli .agents/skills/hotplex-operator
git commit -m "feat(skills): generate builtin repository mirrors"
```

---

### Task 5: Consolidate Operator Guidance and Remove Setup/Update Skills

**Files:**
- Modify: `internal/skills/builtin/hotplex-operator/SKILL.md`
- Modify: `internal/skills/builtin/hotplex-operator/references/install-update.md`
- Modify: `internal/skills/builtin/hotplex-operator/references/service-lifecycle.md`
- Modify: `internal/skills/builtin/hotplex-operator/references/configuration.md`
- Modify: `internal/skills/builtin/hotplex-operator/references/admin-audit.md`
- Delete: `.agents/skills/hotplex-setup/`
- Delete: `.agents/skills/hotplex-update/`
- Modify: `internal/skills/builtin/quality_test.go`
- Modify: `cmd/gen-builtin-skills/main_test.go`

**Interfaces:**
- Consumes: current `hotplex update`, `hotplex service`, `hotplex onboard`, `hotplex doctor`, and `hotplex skills` command surfaces.
- Produces: one operator Skill covering authorized host mutations without manual binary replacement or duplicated setup/update routers.

- [ ] **Step 1: Add quality assertions before rewriting content**

Add checks equivalent to:

```go
require.NoDirExists(t, filepath.Join(repoRoot, ".agents", "skills", "hotplex-setup"))
require.NoDirExists(t, filepath.Join(repoRoot, ".agents", "skills", "hotplex-update"))

operator := readSkillTree(t, filepath.Join(repoRoot, "internal", "skills", "builtin", "hotplex-operator"))
for _, prohibited := range []string{
	"curl -fsSL", "| bash", "cp -f ./bin/hotplex", "sleep 2", "git checkout <previous",
} {
	require.NotContains(t, operator, prohibited)
}
```

Assert the operator references contain the canonical commands `hotplex onboard`, `hotplex doctor`, `hotplex update`, `hotplex service restart`, and `hotplex skills status`.

- [ ] **Step 2: Run the focused test and verify failure**

Run:

```bash
go test ./internal/skills/builtin -run 'Test.*(Operator|RepositorySkillPortfolio|Quality)' -count=1
```

Expected: FAIL while setup/update directories and prohibited instructions remain.

- [ ] **Step 3: Rewrite operator content around supported primitives**

Keep `SKILL.md` as a short router. In references, encode these exact decision boundaries:

- Install/configure: inspect `hotplex onboard --help`, then run only the requested mode; follow with `hotplex doctor`.
- Update: use `hotplex update`; add `--restart` and `--sync-skills` only when explicitly requested.
- Pure restart: use `hotplex service restart`; never split it into stop/sleep/start.
- Built-in Skills: begin with `hotplex skills status`; mutations use explicit profile/worker flags and report collisions or drift.
- Admin/audit: read operations may diagnose; writes require Admin authority and must be reported.
- Never load or print credentials merely to demonstrate an API call.

Delete `.agents/skills/hotplex-setup` and `.agents/skills/hotplex-update`, regenerate mirrors, and verify the canonical operator tree equals its mirror.

- [ ] **Step 4: Run quality and generator tests**

Run:

```bash
go run ./cmd/gen-builtin-skills
go test ./internal/skills/builtin ./cmd/gen-builtin-skills -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit portfolio consolidation**

```bash
git add internal/skills/builtin .agents/skills cmd/gen-builtin-skills
git commit -m "refactor(skills): consolidate operator workflows"
```

---

### Task 6: Refactor Repository-Only Skills and Add Directory Quality Gates

**Files:**
- Modify: `.agents/skills/hotplex-diagnostics/SKILL.md`
- Create: `.agents/skills/hotplex-diagnostics/references/runtime-diagnosis.md`
- Create: `.agents/skills/hotplex-diagnostics/references/feedback-chain.md`
- Modify: `.agents/skills/hotplex-release/SKILL.md`
- Modify: `.agents/skills/hotplex-release/references/troubleshooting.md`
- Modify: `.agents/skills/hotplex-docs-patrol/SKILL.md`
- Modify: `.agents/skills/hotplex-docs-patrol/references/doc-registry.md`
- Create: `internal/skills/repository_quality_test.go`
- Create: `.agents/skills/hotplex-diagnostics/evals/trigger_evals.json`
- Create: `.agents/skills/hotplex-docs-patrol/evals/trigger_evals.json`
- Modify: `.agents/skills/hotplex-release/evals/trigger_evals.json`

**Interfaces:**
- Consumes: five-Skill target portfolio from Tasks 4 and 5.
- Produces: repository-wide Skill validation independent of embedded package validation.

- [ ] **Step 1: Write a failing repository Skill quality test**

The test must discover `.agents/skills/*/SKILL.md` and assert:

```go
wantNames := []string{
	"hotplex-cli",
	"hotplex-diagnostics",
	"hotplex-docs-patrol",
	"hotplex-operator",
	"hotplex-release",
}
require.Equal(t, wantNames, discoveredNames)
```

For every Skill:

- parse YAML frontmatter with `yaml.Decoder.KnownFields(true)` against this portable schema:

```go
type repositorySkillFrontmatter struct {
	Name          string         `yaml:"name"`
	Description   string         `yaml:"description"`
	Compatibility string         `yaml:"compatibility,omitempty"`
	License       string         `yaml:"license,omitempty"`
	AllowedTools  string         `yaml:"allowed-tools,omitempty"`
	Metadata      map[string]any `yaml:"metadata,omitempty"`
}
```

- require exactly one opening and closing frontmatter delimiter;
- require frontmatter `name` equals directory name;
- require non-empty, discriminating description;
- require every relative Markdown link remains within the Skill directory and exists;
- require every file under `references/` is reachable from `SKILL.md` or another reachable reference;
- reject unfinished scaffold markers and the literals `SKILLS.md`, `TOK=$(grep`, `| bash`, `sleep 2`, and `git checkout <previous` in current Skill trees;
- require `hotplex-operator`, `hotplex-release`, and `hotplex-docs-patrol` entrypoints to state their explicit authorization boundary before any host or remote mutation.

- [ ] **Step 2: Run the quality test and verify failure**

Run:

```bash
go test ./internal/skills -run TestRepositorySkillQuality -count=1
```

Expected: FAIL on the existing portfolio, oversized entrypoints, or prohibited content.

- [ ] **Step 3: Refactor diagnostics, release, and docs patrol**

- Diagnostics: keep only symptom routing, read-only authority, stop conditions, and reference links in `SKILL.md`; move the process/session/log workflow and feedback-chain details to separate references. Replace direct `.env` token extraction with “use an already-authorized Admin client or user-provided credential context.” Issue creation is offered only after an explicit user request.
- Release: keep release intent, main/non-main boundary, authorization gates, and narrow reference routing in `SKILL.md`. Move detailed recovery into references. Never tag, push, edit a release, or delete a tag without explicit release authorization.
- Docs patrol: keep baseline selection, semantic impact mapping, current-doc scope, and validation. Remove automatic branch/Issue/PR/push requirements; delivery follows the active repository and user authorization. Preserve the mandatory local baseline update after a completed patrol.
- Ensure descriptions do not overlap: ordinary read-only CLI diagnostics route to `hotplex-cli`; deep incident analysis routes to `hotplex-diagnostics`; host mutation routes to `hotplex-operator`; version publication routes to `hotplex-release`; documentation impact maintenance routes to `hotplex-docs-patrol`.

- [ ] **Step 4: Update trigger evaluations**

Use the repository's existing per-Skill boolean schema:

```json
{
  "skill_name": "hotplex-diagnostics",
  "trigger_evals": [
    {
      "query": "检查 Gateway 为什么 Worker 在运行但用户收不到增量反馈",
      "should_trigger": true,
      "reason": "Deep feedback-chain diagnosis"
    },
    {
      "query": "运行 hotplex status 看服务是否存活",
      "should_trigger": false,
      "reason": "Ordinary read-only CLI check belongs to hotplex-cli"
    },
    {
      "query": "更新 HotPlex 二进制并重启服务",
      "should_trigger": false,
      "reason": "Host mutation belongs to hotplex-operator"
    }
  ]
}
```

Create the same structure for `hotplex-docs-patrol`, with a positive code-to-doc impact review and negatives for ordinary document editing and release publication. Add release negatives for host binary update, deep runtime diagnosis, and docs patrol. Operator and CLI routing remain covered by the built-in description quality table because their `.agents` trees are generated package mirrors and do not carry repository-only eval assets.

- [ ] **Step 5: Run repository Skill quality tests**

Run:

```bash
go test ./internal/skills/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit repository Skill cleanup**

```bash
git add .agents/skills internal/skills
git commit -m "refactor(skills): enforce clean repository portfolio"
```

---

### Task 7: Update Current Documentation and API Specification

**Files:**
- Modify: `AGENTS.md`
- Modify: `internal/agentconfig/META-COGNITION.md`
- Modify: `docs/architecture/Agent-Config-Design.md`
- Modify: `docs/explanation/agent-config-system.md`
- Modify: `docs/explanation/cron-design.md`
- Modify: `docs/guides/contributor/architecture.md`
- Modify: `docs/guides/developer/cron-automation.md`
- Modify: `docs/guides/enterprise/multi-tenant.md`
- Modify: `docs/guides/user/commands-cheatsheet.md`
- Modify: `docs/reference/admin-api.md`
- Modify: `docs/reference/cli.md`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/reference/glossary.md`
- Modify: `docs/tutorials/agent-personality.md`
- Modify: `docs/tutorials/phrases-customization.md`
- Modify: `docs/tutorials/skills-setup.md`
- Modify: `docs/swagger/swagger.json`
- Modify: `internal/skills/builtin/quality_test.go`

**Interfaces:**
- Consumes: final strict runtime/API/Skill behavior from Tasks 1–6.
- Produces: current BFS-reachable documentation with no compatibility claims.

- [ ] **Step 1: Add failing documentation boundary assertions**

Extend the existing documentation quality test to require current docs to contain the strict model and exclude compatibility language:

```go
for _, path := range currentAgentConfigDocs {
	content := readRepoFile(t, path)
	require.NotContains(t, content, "SKILLS.md")
	require.NotContains(t, content, "platform/default/")
	require.NotContains(t, content, "ReleaseSkillManual")
}
```

Historical `docs/archive`, `docs/specs`, and `docs/superpowers` records are excluded from this negative assertion.

- [ ] **Step 2: Run the documentation test and verify failure**

Run:

```bash
go test ./internal/skills/builtin -run 'Test.*(Documentation|Legacy|Quality)' -count=1
```

Expected: FAIL on current compatibility wording.

- [ ] **Step 3: Rewrite current docs to the strict model**

Apply these exact content decisions:

- Describe only five canonical AgentConfig files and exact three-scope resolution.
- Remove all `SKILLS.md` migration/alias/coexistence sections and deprecated Admin response fields.
- Remove `platform/default/` fallback and suffix-layout migration guidance.
- Remove statements that Gateway/Cron/messaging publish flat manuals.
- Describe both generated built-in repository mirrors and the five-Skill portfolio.
- Keep the distinction between Admin/WebChat public built-in discovery and Session `/skills` Worker/filesystem evidence.
- Keep `TOOLS.md` separate from real Agent Skills without framing it as an active compatibility transition.
- Update Swagger models so AgentConfig metadata has no deprecated `skills` property.

- [ ] **Step 4: Build and validate docs**

Run:

```bash
go test ./internal/skills/builtin -count=1
go run cmd/build-docs/main.go
```

Expected: tests pass, docs build succeeds, and no generated content escapes `internal/docs/out`.

- [ ] **Step 5: Commit docs**

```bash
git add AGENTS.md internal/agentconfig/META-COGNITION.md docs internal/skills/builtin/quality_test.go
git commit -m "docs: document strict skills architecture"
```

---

### Task 8: Full Review, Verification, and Delivery

**Files:**
- Modify only files required to fix defects found by the gates.
- Update local ignored state: `.docs-patrol-baseline` after final committed HEAD.

**Interfaces:**
- Consumes: all prior task deliverables.
- Produces: a clean, pushed branch whose remote HEAD matches local HEAD.

- [ ] **Step 1: Audit forbidden compatibility remnants**

Run bounded searches over production and current docs:

```bash
rg -n 'LegacyFileSkills|AgentConfigLegacySkills|LegacyDefaultBotName|ReleaseSkillManual|normalizeLegacyToolsOverride|platform/default/' internal cmd webchat .agents/skills docs --glob '!docs/archive/**' --glob '!docs/specs/**' --glob '!docs/superpowers/**'
```

Expected: no match.

Search `SKILLS.md` separately and allow it only in historical records or tests that assert rejection/absence:

```bash
rg -n 'SKILLS\.md' internal cmd webchat .agents/skills docs AGENTS.md --glob '!docs/archive/**' --glob '!docs/specs/**' --glob '!docs/superpowers/**'
```

Expected: production/current-doc matches are absent; strict negative tests may remain.

- [ ] **Step 2: Run focused race tests**

```bash
go test ./internal/agentconfig ./internal/admin ./internal/cli/checkers ./internal/cron ./internal/messaging/phrases ./internal/dbutil ./internal/skills/... ./cmd/hotplex -count=1 -race -shuffle=on
```

Expected: PASS.

- [ ] **Step 3: Run full Go gates**

```bash
go test ./... -count=1
go vet ./...
```

Expected: PASS.

- [ ] **Step 4: Run WebChat gates**

From `webchat/`:

```bash
pnpm exec vitest run --passWithNoTests
pnpm exec tsc --noEmit
pnpm lint
```

Expected: PASS.

- [ ] **Step 5: Prove generator stability and mirror parity**

Run the CLI surface generator, then the built-in generator twice:

```bash
go run ./cmd/hotplex --internal-generate-cli-surface --output internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md
go run ./cmd/gen-builtin-skills
go run ./cmd/gen-builtin-skills
```

Verify:

```bash
git diff --exit-code -- .agents/skills/hotplex-cli .agents/skills/hotplex-operator internal/skills/builtin/manifest.generated.go
```

Expected: no diff after committed generated outputs.

- [ ] **Step 6: Run docs and diff gates**

```bash
go run cmd/build-docs/main.go
git diff --check
git status --short
```

Expected: docs build succeeds, diff check passes, and the worktree is clean after any defect-fix commits.

- [ ] **Step 7: Perform final code review**

Review the complete design-to-HEAD diff for correctness, security, public API breakage, user-file preservation, test quality, and documentation truthfulness. Fix every P0/P1 and valuable P2 finding, rerun the proportional gates, and commit the fixes.

- [ ] **Step 8: Update docs patrol baseline and push**

Set `.docs-patrol-baseline` to the final committed HEAD without staging it. Push the current non-main branch, allow the repository pre-push hook to finish, then verify:

```bash
git rev-parse HEAD
git rev-parse origin/fix/turn-integrity-init-reliability
git status --short
```

Expected: both hashes match and the worktree is clean.
