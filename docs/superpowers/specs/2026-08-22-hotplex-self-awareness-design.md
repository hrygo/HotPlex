# HotPlex Self-Awareness Architecture Design

**Date:** 2026-08-22
**Status:** Proposed, revised after cross-client Agent Skills review
**Scope:** Stable metacognition, trusted runtime facts, AgentConfig correction, capability routing, and built-in HotPlex Agent Skills
**Related design:** `docs/superpowers/specs/2026-08-22-hotplex-agent-metacognition-tools-design.md`

## 1. Problem

HotPlex has separated the AgentConfig `TOOLS.md` slot from Agent Skills, but the Agent still lacks a complete and evidence-based model of HotPlex itself.

The current state has five gaps:

1. `META-COGNITION.md` describes identity and configuration boundaries but receives no trusted per-session runtime facts.
2. The default `TOOLS.md` names capabilities without a complete routing model or a safe minimum execution contract.
3. The default `SOUL.md`, `AGENTS.md`, and `MEMORY.md` contain claims that are not universally true, such as fixed interaction, retry, and memory-write behavior.
4. HotPlex CLI guidance is split between Cobra help, a repository-local `hotplex-cli` Skill, and legacy embedded manuals. The repository copy is useful for development but is not a product distribution mechanism.
5. `~/.hotplex/skills` can be scanned by HotPlex, but that does not make a Skill visible or invocable in every Worker. The current flat `cron.md`, `phrases.md`, and `db-stats.md` files are manuals rather than valid `SKILL.md` packages.

This creates two unsafe inferences:

- documentation may be treated as proof that a capability is available;
- a filesystem definition may be treated as callable merely because HotPlex discovered it.

## 2. Decision Summary

The optimized design makes the following decisions:

1. Keep stable product identity in embedded META, session-specific facts in a small trusted JSON block, editable preferences in the five AgentConfig files, and procedural workflows in Agent Skills.
2. Use native progressive disclosure. A native Worker owns its Skill catalog disclosure and activation; HotPlex does not inject a duplicate catalog into that Worker's system prompt.
3. Make Cobra commands and validators the authority for CLI syntax and behavior. Skills explain intent, routing, safety, and verification; generated references provide exact command inventory.
4. Ship two bounded built-in packages:
   - `hotplex-cli` for Cron, explicitly requested Slack operations, and read-only diagnostics;
   - `hotplex-operator` for service, install, update, Admin, audit, and other privileged host operations.
5. Keep built-ins embedded in the binary and optionally release them to a HotPlex-owned inventory. Project them into Worker-native roots only through an explicit, inspectable synchronization workflow.
6. Make Gateway startup read-only with respect to `~/.claude/skills`, `~/.agents/skills`, and other Worker-owned roots.
7. Preserve `discoverable` versus `callable` as an evidence-based runtime decision, and require listing and dispatch to use the same decision.
8. Defer migration of `phrases.md` and `db-stats.md` into separate delivery phases so the core self-awareness change is not coupled to unrelated operational domains.

### 2.1 Alternatives considered

- **Inject every scanned Skill into the Gateway system prompt:** rejected because native Workers already perform progressive disclosure, catalogs can exceed prompt budgets, and a listed filesystem package may lack an activation path.
- **Copy built-ins into every native root at Gateway startup:** rejected because startup would mutate Worker-owned configuration, conflict handling would be invisible, and cross-platform replacement is not reliably atomic.
- **Put all HotPlex operations in one `hotplex-cli` Skill:** rejected because ordinary Cron/Slack guidance would carry unrelated host/Admin procedures and trigger too broadly.
- **Treat the handwritten Skill as the CLI source of truth:** rejected because command syntax and validation can drift from executable Cobra behavior.
- **Add `source: builtin`:** rejected because `source` is an existing wire-compatibility scope field; optional built-in provenance is safer.

## 3. Goals

1. Give every Agent a stable and accurate model of HotPlex, Gateway, Worker, AgentConfig, commands, MCP, Admin APIs, and Agent Skills.
2. Expose trusted per-session facts without leaking identifiers, credentials, arbitrary environment values, or catalogs.
3. Make HotPlex CLI guidance available as valid, portable Agent Skills that Workers can actually discover and activate.
4. Keep CLI references synchronized with the running command tree and validators.
5. Preserve user-owned Skills and require an explicit operation before modifying Worker-native Skill roots.
6. Distinguish filesystem discovery, Worker advertisement, and current-session callability across APIs, UI, prompts, and dispatch.
7. Correct the five default AgentConfig templates without overwriting existing user configuration.
8. Provide deterministic degradation: query the live surface, use `hotplex <domain> --help`, or report an explicit unsupported state.

## 4. Non-Goals

- Implementing `SystemPromptDelivery` evidence.
- Implementing the AgentConfig inspect/plan/approve/apply/verify control plane.
- Automatically rewriting existing `~/.hotplex/agent-configs/*.md` files.
- Teaching ACP to load filesystem Skills that its underlying Agent does not advertise.
- Turning Admin Skills APIs or WebChat Skills into AgentConfig management; they continue to represent Agent Skills.
- Turning repository release, documentation patrol, or contributor workflows into runtime Bot capabilities.
- Making a Skill instruction a substitute for code-enforced authorization.

## 5. Design Principles

### 5.1 Stable knowledge and dynamic evidence are separate

Long-lived identity belongs in embedded metacognition. Per-session declarations belong in trusted runtime facts. User and Bot preferences belong in editable AgentConfig. Detailed workflows belong in on-demand Skills.

### 5.2 Documentation never proves availability

`TOOLS.md`, Skill files, and examples describe how to use a capability. They do not prove that the selected Worker exposes it, that the current identity is authorized, or that an operation succeeded.

### 5.3 Discovery, advertisement, and callability are different evidence

- `discoverable`: HotPlex found a valid Skill definition, but current native invocation is not confirmed.
- `callable`: the Worker advertised the Skill or the adapter has a tested native activation contract for it.
- `unavailable`: an authoritative Worker catalog explicitly excludes the Skill.

Only `callable` entries may be dispatched as Skills.

### 5.4 Progressive disclosure has one owner

For a native Skills Worker, the Worker owns disclosure of `name`, `description`, and, where applicable, `path`, followed by on-demand loading of `SKILL.md` and referenced resources. HotPlex must not duplicate that catalog in the AgentConfig system prompt.

If a future adapter has no native catalog, HotPlex may disclose a bounded catalog only when the same adapter also supplies a real activation mechanism. A catalog without activation is not allowed.

### 5.5 The executable interface is the authority

Cobra commands, flags, parsers, and validators are the source of truth for HotPlex CLI behavior. Handwritten Skill content is the source of truth only for decision rules, safety boundaries, and postcondition checks. Exact command inventories are generated from code.

### 5.6 Model context is not an authority boundary

Prompt text, Skill metadata, or a model decision cannot grant permissions, select Admin scope, mark a projection as trusted, or convert `discoverable` into `callable`.

### 5.7 Host mutations are explicit and recoverable

Gateway startup performs discovery and drift reporting only. Installation, synchronization, repair, and removal are explicit CLI or onboarding actions with dry-run support, conflict preservation, and rollback data.

## 6. Four-Layer Self-Awareness Model

```text
┌─────────────────────────────────────────────────────────────┐
│ 1. Embedded META                                            │
│    Stable identity, trust model, configuration, degradation │
├─────────────────────────────────────────────────────────────┤
│ 2. Gateway Runtime Facts                                    │
│    Current platform, Worker, scope and declared capability  │
├─────────────────────────────────────────────────────────────┤
│ 3. Editable AgentConfig                                     │
│    SOUL / AGENTS / TOOLS / USER / MEMORY                    │
├─────────────────────────────────────────────────────────────┤
│ 4. Agent Skills                                             │
│    Native progressive disclosure and on-demand workflows    │
└─────────────────────────────────────────────────────────────┘
```

### 6.1 Layer 1: Embedded META

`META-COGNITION.md` remains concise and is always injected first. It defines only durable invariants:

- HotPlex Gateway owns transport, routing, session lifecycle, persistence, retry policy, and protocol state.
- Worker owns task reasoning and Worker-native execution.
- The Agent runs inside HotPlex but is not the Gateway and must not claim control over host state it cannot observe.
- AgentConfig has five editable slots: `SOUL.md`, `AGENTS.md`, `TOOLS.md`, `USER.md`, and `MEMORY.md`; embedded META is not an editable slot.
- Directives override context. Each editable file resolves independently through global, platform, and Bot scope; a matching file terminates fallback for that file.
- AgentConfig changes take effect on a new Session or `/reset`, not retroactively in the current turn.
- `TOOLS.md` is persistent operational guidance. Agent Skills are `SKILL.md` workflow packages loaded on demand. No wording such as “真实 Skills” is necessary.
- Admin API `skills`, WebChat Skills, Worker `/skills`, and AEP Skill entries describe Agent Skills, not the AgentConfig `TOOLS.md` slot.
- HotPlex capabilities may be exposed as Gateway commands, Worker commands, CLI, MCP, Admin APIs, or Agent Skills; those surfaces are not interchangeable.
- Discovery follows this order:

```text
trusted runtime facts
→ live Worker/Gateway capability query
→ current /skills or /mcp surface
→ activated Agent Skill
→ TOOLS routing
→ hotplex <domain> --help
→ explicit unsupported/degraded result
```

- Configuration or external mutations require the authority appropriate to the operation and must be verified through an independent read path.
- META contains no concrete Skill catalog, exhaustive CLI flags, fixed retry counts, timeouts, versions, or platform-specific promises.

### 6.2 Layer 2: Trusted Runtime Facts

Gateway creates a typed declaration before prompt assembly:

```go
type RuntimeFacts struct {
    SchemaVersion              int
    Platform                   string
    WorkerType                 worker.WorkerType
    ScopeKind                  RuntimeScopeKind
    DeclaredPermissionMode     string
    DeclaredCapabilities       []RuntimeCapability
    DeclaredQuerySurfaces      []RuntimeQuerySurface
    DeclaredSkillCatalogOwner  SkillCatalogOwner
    PresentGatewayEnvKeys      []string
}
```

Rules:

- `RuntimeScopeKind` is a closed enum such as `bot`, `workspace`, or `unbound`.
- `DeclaredCapabilities` and `DeclaredQuerySurfaces` are closed, sorted, de-duplicated enums.
- Names use `Declared*` because they describe Gateway/adapter declarations, not observed enforcement by an external Worker process.
- `DeclaredSkillCatalogOwner` is `worker`, `gateway`, or `none`; the first delivery uses `worker` or `none` only.
- `PresentGatewayEnvKeys` lists allowlisted variable names whose values are present, never the values themselves.
- Bot name, Bot ID, user ID, session ID, workspace/channel/thread/team ID, work directory, credentials, connection strings, Skill names, Skill descriptions, tool names, MCP configuration, and arbitrary environment data are excluded.
- Each scalar and collection has a hard size/count limit.

The initial allowlist may include these environment key names:

- `GATEWAY_PLATFORM`
- `GATEWAY_BOT_ID`
- `GATEWAY_BOT_NAME`
- `GATEWAY_USER_ID`
- `GATEWAY_SESSION_ID`
- `GATEWAY_WORK_DIR`
- `GATEWAY_CHANNEL_ID`
- `GATEWAY_THREAD_ID`
- `GATEWAY_TEAM_ID`

The Agent can read a value from its own environment only when a task needs it and policy allows it; the prompt does not copy those values.

### 6.3 Layer 3: Editable AgentConfig

- `SOUL.md`: persona and communication style.
- `AGENTS.md`: behavioral rules and work constraints.
- `TOOLS.md`: capability routing, operational preferences, and safety boundaries.
- `USER.md`: user-supplied facts and preferences.
- `MEMORY.md`: host-supplied historical context; it is not self-writing without an authorized control interface.

Templates describe scope and fallback accurately but never promise that a named Skill or tool is available.

### 6.4 Layer 4: Agent Skills

Agent Skills use the standard directory form:

```text
<name>/
├── SKILL.md
├── references/   # optional, loaded only when relevant
├── scripts/      # optional deterministic helpers
└── assets/       # optional templates or resources
```

`SKILL.md` contains valid YAML frontmatter with a precise `name` and trigger-oriented `description`. The body remains a concise router and procedure; long references are loaded only when the selected task needs them.

## 7. Prompt Assembly

### 7.1 Additive API

Keep the runtime-neutral API:

```go
func BuildSystemPrompt(configs *AgentConfigs) string
```

Add:

```go
func BuildSystemPromptWithRuntime(configs *AgentConfigs, facts RuntimeFacts) string
```

`BuildSystemPrompt` delegates with empty facts. Admin preview therefore remains compatible and never invents a live Session.

### 7.2 Versioned, deterministic facts

The outer prompt moves to schema version 3. Runtime facts are canonical JSON embedded as XML text:

```xml
<agent-configuration schema-version="3">
  <runtime-facts format="application/json" schema-version="1">{...canonical JSON...}</runtime-facts>
  <directives>
    <hotplex>...</hotplex>
    <persona>...</persona>
    <rules>...</rules>
    <tool-guidance>...</tool-guidance>
  </directives>
  <context>
    <user-data>...</user-data>
    <memory-data>...</memory-data>
  </context>
</agent-configuration>
```

Serialization uses a fixed field order, sorted arrays, explicit enum validation, JSON encoding, and existing XML sanitization. Empty facts omit the entire `runtime-facts` element rather than emitting an ambiguous empty block.

### 7.3 Catalog disclosure rule

For Claude Code, Codex CLI, and OpenCode, HotPlex relies on the native Worker to expose the installed Skill catalog and load the selected `SKILL.md`. HotPlex does not append Skill metadata or bodies to `<runtime-facts>`, `<directives>`, or `<context>`.

This is a non-duplication rule, not a claim that Skill metadata never enters model context: the native Worker normally discloses `name` and `description`, and Codex may also disclose the path. If a future Gateway-owned catalog is added, it must be a separate bounded structure with filtering, deterministic precedence, a real activation tool, and its own schema/budget tests.

### 7.4 Bridge ownership

Bridge builds facts from session metadata, the selected Worker, declared Worker capabilities, and resolved policy. Worker adapters may provide declared capability evidence but do not edit HotPlex facts directly.

`/reset` rebuilds the prompt from fresh AgentConfig and fresh declarations. A future `SystemPromptDelivery` feature can add delivery evidence without weakening the distinction between declaration and observation.

## 8. Default AgentConfig Corrections

### 8.1 `SOUL.md`

Remove universal claims that every Worker is non-interactive, permission requests always use a fixed timeout, or transient failures always retry a fixed number of times. Treat these as runtime or platform policy.

### 8.2 `AGENTS.md`

Remove blanket authorization for dependency installation, Git merge, service changes, and other mutations unrelated to a current user request. Preserve scoped authorization and verification requirements.

### 8.3 `TOOLS.md`

Make it a compact operational router rather than a command manual. It covers:

- filesystem discovery, Worker advertisement, and `discoverable`/`callable`/`unavailable` status;
- Gateway command routing, including `/help`, `/stop`, `/reset`, `/new`, `/gc`, `/park`, `/cd`, `/skills`, `/mcp`, and `/worker`;
- activation of `hotplex-cli` for applicable CLI tasks;
- Slack and Feishu separation (`lark-cli` is outside `hotplex-cli`);
- a minimal Cron safety contract and post-create verification;
- read-only diagnostics, scoped mutations, and privileged host operations;
- STT/TTS dependence on current runtime capability;
- AgentConfig activation on a new Session or `/reset`.

It does not copy exhaustive flags or reference pages.

### 8.4 `USER.md`

Keep only user-supplied background and preferences. Remove operational advice presented as runtime fact.

### 8.5 `MEMORY.md`

Remove the claim that the Agent automatically manages this file. State that changes require an authorized host/control interface; until then it is read-only context from the Agent's perspective.

### 8.6 Upgrade behavior

Updated templates affect new onboarding and an explicit template reset only. Upgrade never overwrites existing AgentConfig files. Existing installations still receive updated embedded META and runtime logic from the binary.

## 9. Built-In HotPlex Skills

### 9.1 Portable frontmatter

Cross-client built-ins use only the portable Agent Skills fields needed by all target Workers. Vendor extensions such as invocation controls or experimental tool grants are not used as security boundaries.

The runtime package is:

```yaml
---
name: hotplex-cli
description: Use HotPlex CLI for Cron jobs, explicitly requested Slack operations, and read-only status, doctor, security, or config diagnostics. Do not use for Feishu, releases, service installation, binary updates, or Admin mutations.
compatibility: Requires the hotplex CLI and a runtime identity authorized for the requested operation.
---
```

The privileged package is:

```yaml
---
name: hotplex-operator
description: Operate a HotPlex host: install or restart services, update binaries, change host configuration, inspect audit state, or perform Admin mutations. Use only in an explicitly authorized operator context.
compatibility: Requires local host access, the hotplex CLI, and explicit operator or Admin authority.
---
```

Loading a Skill does not grant the authority described by `compatibility`; code and runtime policy still enforce it.

### 9.2 Package structure and progressive disclosure

```text
internal/skills/builtin/
├── hotplex-cli/
│   ├── SKILL.md
│   └── references/
│       ├── cron.md
│       ├── slack.md
│       ├── diagnostics.md
│       └── cli-surface.generated.md
└── hotplex-operator/
    ├── SKILL.md
    └── references/
        ├── service-lifecycle.md
        ├── install-update.md
        ├── configuration.md
        └── admin-audit.md
```

Each `SKILL.md` is a short router containing:

- exact positive and negative activation scope;
- the read-only/scoped-mutation/privileged-operation decision;
- which reference to load for the current task;
- the rule that the running binary and its help output win when documentation differs;
- authorization and postcondition requirements;
- a prohibition on printing tokens or arbitrary environment values.

Detailed command catalogs do not live in the body.

### 9.3 CLI source of truth and generated reference

The Cobra tree and domain validators are authoritative. A deterministic Go generator walks only public commands and flags and produces `cli-surface.generated.md` with:

- command path;
- one-line purpose;
- public long flags and value shape;
- applicable aliases;
- no hidden commands, runtime values, machine paths, or secrets.

CI regenerates the file and fails on a diff. Handwritten references link to the relevant generated section but focus on decisions, invariants, safe examples, and verification. At runtime, `hotplex <domain> --help` remains the final authority for the installed binary.

The repository `.agents/skills/hotplex-cli` development package is generated from or checked byte-for-byte against the embedded canonical package; it is never an independently edited copy.

### 9.4 Cron workflow

`references/cron.md` covers only the non-obvious operational contract:

- accepted schedules: `cron:`, `every:`, `at:<RFC3339>`, and `at:+<duration>`;
- isolated versus `--attach` execution and when each is appropriate;
- isolated prompts must be self-contained;
- runtime identity and delivery routing use present `GATEWAY_*` keys without printing unrelated values;
- recurring jobs require valid lifecycle limits such as `--max-runs` or `--expires-at` where enforced by the binary;
- the semantics of `--silent`, `--delete-after-run`, retries, timeout, and allowed tools are verified against current validators;
- creation is followed by `cron get` through an independent read path;
- portable examples avoid shell-specific date arithmetic.

Exact flag lists and subcommands are generated. The legacy Cron manual becomes a generated compatibility artifact rather than a second authored source.

### 9.5 Slack and diagnostics

Slack guidance requires an explicit request for every external message, upload, update, deletion, scheduling action, bookmark change, or reaction. Read-only lookup does not authorize a later mutation.

Diagnostics guidance covers version, status, doctor, security inspection, config validation, service status/logs, and Cron list/get/history. It routes service mutation, update, Admin mutation, audit rebase, and runtime-fence decisions to `hotplex-operator` rather than embedding those privileges in the broadly available runtime Skill.

### 9.6 Deferred legacy domains

`phrases.md` and `db-stats.md` are assessed and migrated independently after the core delivery. If they become Skills, they receive separate names, descriptions, permissions, tests, and distribution choices. They are not bundled into `hotplex-cli` merely because they currently live under `~/.hotplex/skills`.

Repository-only Skills such as release and documentation patrol remain repository scoped and are never projected into runtime Bot homes.

## 10. Distribution and Synchronization

### 10.1 Canonical and installed inventory

The binary embeds the canonical packages and their generated manifests. An explicit synchronization operation may release an immutable inventory to:

```text
$HOTPLEX_HOME/skills/builtin/<package-version>/<name>/
```

This answers the `~/.hotplex/skills` question precisely:

- HotPlex can discover and display the built-in inventory directly.
- The inventory path alone does not make a Skill visible to a Worker.
- A Worker uses the Skill only after a native projection exists and the adapter has current callability evidence.

The inventory is read-only through managed-Skill CRUD. User-managed packages continue to use their existing roots and semantics.

The built-in registry reads embedded manifests and installation receipts directly; it does not depend on the generic filesystem scanner recursively walking the versioned inventory. The generic scanner keeps bounded, shallow discovery for user Skills.

### 10.2 Explicit lifecycle

Add these host commands:

```text
hotplex skills status
hotplex skills sync [--profile runtime|operator] [--worker <type>...] [--dry-run]
hotplex skills remove [--profile runtime|operator] [--worker <type>...] [--dry-run]
```

- `status` is read-only and reports inventory, projections, conflicts, drift, and Worker evidence.
- `sync --dry-run` computes the exact change set without writing.
- Without `--worker`, synchronization targets only Worker types enabled in the resolved HotPlex configuration; it never writes every known root merely because the directory exists.
- `sync --profile runtime` installs `hotplex-cli` into the selected native roots.
- `sync --profile operator` additionally installs `hotplex-operator`; onboarding never selects this profile implicitly for a Bot service account.
- `remove` deletes only unchanged projections owned by matching receipts. Missing provenance, modified content, or ambiguous roots are preserved and reported.
- `onboard` may offer runtime synchronization because the user is already performing installation, but it shows the target roots and outcome first.
- `update` refreshes embedded product assets but does not silently rewrite native Skill roots. It may offer an explicit `--sync-skills` step.
- `doctor` reports drift and may direct the user to `skills sync`; any `--fix` path must use the same reconciliation engine and explicit authorization.
- Gateway startup only reads registry/projection state and emits bounded drift diagnostics.

All three commands use one typed reconciliation report rather than parsing human text. Each target returns an action (`none`, `install`, `update`, `remove`), an outcome (`unchanged`, `changed`, `conflict`, `drift`, `failed`), and a stable reason code. Sync and remove are idempotent: repeating a successful operation produces `unchanged`. Validation, conflict, unsupported Worker, and I/O failures remain distinguishable, while human-readable paths and messages are presentation fields rather than control inputs.

### 10.3 Worker-native projections

| Worker | Native projection | Callable evidence |
|---|---|---|
| Claude Code | `~/.claude/skills/<name>/` | adapter launch contract guarantees this exact native root and an integration test verifies invocation |
| Codex CLI | `~/.agents/skills/<name>/` | app-server `skills/list` advertises the projected package/path |
| OpenCode Server | `~/.agents/skills/<name>/` | native Skill/command catalog advertises it |
| ACP | no filesystem assumption | `available_commands_update` or equivalent advertises a supported activation command |

Projection is an installation mechanism, not proof by itself. ACP implementations backed by Claude, Codex, or OpenCode remain uncallable until the ACP surface advertises activation.

### 10.4 Reconciliation record

Do not treat a marker file as a security boundary. Maintain two pieces of provenance:

1. a package manifest embedded with the binary, listing relative paths, sizes, and SHA-256 hashes;
2. an installation receipt under `$HOTPLEX_HOME/state/skills/`, recording package version, canonical target path, previous installed manifest hash, and projection profile.

An optional `.hotplex-builtin.json` inside the projection is a human/debug hint only. The synchronizer validates the embedded manifest, receipt, canonical root, current tree hash, and path containment before changing anything.

### 10.5 Safe reconciliation

For each target:

1. Canonicalize and validate the approved native root. A package target that is itself a symlink is a conflict, not an update target.
2. Missing target: build a sibling staging directory, verify its manifest, then rename it into place.
3. Existing target with a matching receipt and unchanged previous tree hash: stage the new package, rename the old directory to a versioned backup, rename staging into place, and roll back the backup if the second rename fails.
4. Missing receipt, invalid receipt, unexpected symlink, or changed tree hash: preserve the target as user-owned or user-modified and report a conflict.
5. If canonicalized native roots resolve to the same directory, reconcile once.
6. Remove the backup only after the new tree is verified and the receipt is durably updated. Interrupted operations are recovered or reported on the next `status/sync`.

This two-rename transaction is used instead of assuming that replacing a non-empty directory with one rename is portable across macOS, Linux, and Windows.

### 10.6 Collision policy

User and project Skills are never overwritten. If `hotplex-cli` or `hotplex-operator` already exists without matching managed provenance, synchronization reports a collision and does not project the built-in under an alternate hidden name.

The live Worker catalog decides what is visible. HotPlex surfaces duplicate-name or shadowing diagnostics and does not guess which package the Worker selected.

### 10.7 API provenance

Keep existing `source` values for compatibility. Add optional provenance rather than introducing a new enum value that older clients may reject:

```json
{
  "source": "global",
  "managed": false,
  "builtin": true,
  "builtin_package_version": 1
}
```

`builtin` and `builtin_package_version` are optional/omitted when false or absent. Admin API and WebChat may display built-ins but may not update or delete them through user-managed Skill CRUD. If AEP Skill entries gain these fields, the change follows the full AEP compatibility process: Go SDK, example SDKs, protocol documentation, and bidirectional tests.

## 11. Discovery, Disclosure, and Dispatch

### 11.1 Catalog merge

The session catalog preserves evidence for each candidate:

```text
Gateway fixed command
Worker-advertised Skill/command
adapter-verified native projection
filesystem-only Skill
```

These evidence classes are not interchangeable. A filesystem-only package is `discoverable`; a Worker advertisement or adapter-verified native activation path is required for `callable`.

Project-level Skill content is potentially untrusted. HotPlex does not project it into global roots, and a future Gateway-owned catalog must apply workspace trust before disclosing it to the model.

### 11.2 `/skills` is the session observation surface

`/skills`, Admin API `skills`, and WebChat Skills expose real Agent Skill metadata and status. They do not expose AgentConfig `TOOLS.md` as a Skill and do not imply that listing alone grants invocation.

The session surface returns name, description, source, optional built-in provenance, status, and bounded diagnostics. Only `callable` entries may be executed as Skills.

### 11.3 Native catalog disclosure

- Claude Code, Codex, and OpenCode disclose the native catalog to their own model context using their native progressive-disclosure mechanism.
- HotPlex records and displays the catalog but does not duplicate it in the AgentConfig prompt.
- ACP uses its advertised command mechanism.
- If no activation mechanism exists, HotPlex exposes no model-facing catalog for that adapter.

### 11.4 Unified dispatch

Short `/name`, explicit `/worker <name>`, WebChat invocation, and any structured AEP Skill item resolve through the same session catalog decision used by `/skills`:

- `callable`: invoke through the selected Worker adapter.
- `discoverable` only: return `NOT_SUPPORTED` and explain how to synchronize or select a compatible Worker.
- `unavailable`: return a bounded unsupported result based on the authoritative Worker catalog.
- missing, ambiguous, shadowed, or stale: return the existing bounded error class without filesystem fallback.

The raw filesystem short-command path must not bypass this decision.

## 12. Security and Authority

Trust boundaries are explicit:

- **Assets:** AgentConfig directives, user-managed Skills, Worker-native homes, runtime identity, Admin authority, and external messaging/scheduling state.
- **Untrusted inputs:** project Skill files and metadata, user-authored Skill packages, model-generated CLI arguments, filesystem links, stale Worker catalogs, and external command output.
- **Primary abuse cases:** a project Skill shadows a built-in, a symlink redirects synchronization outside the approved root, a modified package is overwritten, Skill text induces an unauthorized mutation, catalog drift enables the wrong command, or diagnostics leak runtime identity.
- **Control boundary:** manifests and receipts provide integrity/provenance evidence, not authentication against an attacker who already controls the same host account. Authorization remains in HotPlex, the Worker sandbox, Admin middleware, and the host OS.

- Runtime facts are allowlisted, schema-validated, size-bounded, and sanitized.
- Prompt facts and Skill examples never contain environment values, tokens, credentials, connection strings, or private repository URLs.
- Skill metadata is untrusted catalog data. Native Workers decide how to render it; HotPlex UI escapes it as data.
- Built-in content originates from `go:embed` and a verified manifest; no archive extraction is required.
- Discovery and manifests have hard depth, file-count, per-file, and total-size limits.
- Native-root writes occur only during explicit synchronization and fail closed on path, symlink, provenance, or hash uncertainty.
- Project Skills are not promoted to global roots.
- Model-generated names, profiles, Worker types, paths, and command arguments are validated against closed enums or the real Cobra boundary; they are never concatenated into an internal shell command.
- `hotplex-cli` may describe a mutation but performs it only for a matching user request and verifies the result.
- `hotplex-operator` is not automatically projected for Bot service accounts and never substitutes for host/Admin authorization.
- Worker permission controls and HotPlex authorization remain code-enforced even if a Skill contains contrary text.
- Stale or absent callability evidence fails closed.

## 13. Compatibility

### 13.1 Prompt and AgentConfig

`BuildSystemPrompt` remains available. No editable slot is added. `TOOLS.md` remains the canonical filename; legacy `SKILLS.md` compatibility follows the previously approved migration design. Existing AgentConfig files are not overwritten.

### 13.2 Skills APIs

Admin Skills APIs and WebChat Skills retain their Agent Skills meaning. New built-in provenance is optional and additive. Existing consumers that understand only `global` and `project` source values continue to work.

### 13.3 Legacy manuals

The flat Cron manual remains for one compatibility minor as a generated artifact and never enters the catalog. The `phrases.md` and `db-stats.md` migration is deferred; existing behavior remains unchanged until their independent designs are approved.

### 13.4 Rollback

- Omitting runtime facts falls back to the compatible runtime-neutral prompt path.
- A rollback does not delete synchronized packages automatically.
- Installation receipts and backups allow explicit recovery without touching user-owned Skills.
- Older binaries may ignore optional marker/provenance fields safely.
- No rollback requires restoring overwritten AgentConfig or user-managed Skill content because the design never overwrites them.

## 14. Verification Strategy

### 14.1 Runtime facts and prompt

- outer schema ordering and runtime-facts schema version;
- deterministic canonical JSON;
- empty/partial facts and enum rejection;
- scalar/count limits and XML/control-character sanitization;
- secret-shaped values excluded by construction;
- Slack, Feishu, WebChat, Cron, Bot, Workspace, and unbound matrices;
- Admin preview remains runtime-neutral;
- native Worker prompts contain no duplicate Gateway-injected Skill catalog.

### 14.2 META and templates

- complete HotPlex ownership and configuration fallback invariants;
- no fixed timeout/retry or automatic MEMORY-write claims;
- no unconditional tool or Skill availability;
- correct `TOOLS.md` versus Agent Skills semantics;
- Admin/WebChat Skills remain Agent Skills;
- correct Workspace and `/reset` activation language;
- complete Gateway command routing anchors without exhaustive CLI duplication.

### 14.3 Skill quality

- strict Agent Skills frontmatter validation and directory-name match;
- concise, discriminating positive and negative descriptions;
- reference-link closure and one-level progressive disclosure;
- portable frontmatter across Claude, Codex, and OpenCode;
- generated CLI surface has no hidden commands or sensitive values;
- handwritten examples parse against Cobra without executing mutations;
- Cron schedules and lifecycle rules use the real parser and validators;
- positive activation cases, negative cases, and near-boundary cases for each Skill description.

### 14.4 Synchronization

- read-only `status`, sync/remove dry-run equivalence, and receipt-scoped removal;
- initial install, unchanged install, update, interrupted update, and rollback;
- user collision and user-modified copy preservation;
- receipt corruption, manifest mismatch, symlink target, path traversal, and canonical-root deduplication;
- macOS, Linux, and Windows rename/recovery behavior;
- Gateway startup performs no writes to native Skill roots.

### 14.5 Worker matrix

- Claude native discovery and invocation contract;
- Codex `skills/list` path confirmation and structured activation;
- OpenCode native catalog and activation;
- ACP advertised and unadvertised cases;
- filesystem-only definitions never become callable;
- all invocation entry points share the same catalog decision;
- collision and stale-catalog behavior fails closed.

### 14.6 Interface compatibility

- existing Skills API fixtures remain valid without new fields;
- new built-in provenance round-trips when present;
- if AEP changes, update all required SDKs, example SDKs, protocol docs, and bidirectional tests;
- generated CLI reference is reproducible and CI fails on drift.

## 15. Delivery Phases

### Phase A: Core self-awareness

- revise embedded META;
- add bounded runtime facts and prompt schema v3;
- correct the five default templates;
- unify callability decisions across listing and dispatch;
- update core documentation and tests.

### Phase B: Built-in Skill framework and CLI Skills

- add embedded package registry and manifests;
- implement `hotplex skills status/sync` and reconciliation receipts;
- add `hotplex-cli` and `hotplex-operator`;
- generate the public CLI surface from Cobra;
- verify Claude, Codex, OpenCode, and ACP behavior.

### Phase C: Legacy operational manuals

- assess `phrases.md` and `db-stats.md` independently;
- migrate only if each domain benefits from a Skill and has a valid permission/distribution model;
- retire legacy compatibility files on an announced schedule.

Each phase has its own implementation plan and can be rolled back independently. Phase A does not depend on native-root writes. Phase B does not depend on Phase C.

## 16. Documentation Impact

Implementation updates:

- `docs/explanation/agent-config-system.md`
- `docs/explanation/cron-design.md`
- `docs/tutorials/agent-personality.md`
- `docs/tutorials/skills-setup.md`
- `docs/tutorials/cron-scheduled-tasks.md`
- `docs/guides/user/commands-cheatsheet.md`
- `docs/reference/cli.md`
- `docs/reference/configuration.md`
- `docs/reference/admin-api.md` if built-in provenance is exposed

Historical archive/spec documents are not rewritten.

## 17. Acceptance Criteria

1. A new Session can identify its platform, Worker, scope kind, declared permission mode, declared capabilities, declared query surfaces, and declared Skill catalog owner without shell inspection.
2. Runtime facts contain no identity values, credentials, arbitrary environment values, Skill catalog, tool catalog, or MCP configuration.
3. META accurately explains HotPlex ownership, AgentConfig fallback/activation, capability surfaces, and `TOOLS.md` versus Agent Skills without using redundant “真实 Skills” wording.
4. Native Workers own `name`/`description` progressive disclosure; HotPlex does not inject a duplicate catalog into their AgentConfig prompt.
5. `hotplex-cli` is a valid portable Skill for Cron, explicitly requested Slack operations, and read-only diagnostics.
6. Privileged host/Admin guidance is isolated in `hotplex-operator` and is not automatically installed for runtime Bot accounts.
7. Exact CLI surface is generated from public Cobra commands, and handwritten workflow examples/tests agree with the running parsers and validators.
8. `~/.hotplex/skills/builtin` is an inventory, not an assumed Worker root; synchronized native projections are required for Worker use.
9. Gateway startup never writes Worker-native Skill roots. `status`, dry-run, sync, conflicts, receipts, backup, and rollback are observable and tested.
10. Synchronization never overwrites an unowned, user-modified, symlinked, ambiguous, or out-of-root package.
11. `/skills`, Admin API Skills, and WebChat Skills represent Agent Skills and report evidence-based status without conflating AgentConfig `TOOLS.md`.
12. `/name`, `/worker`, WebChat, and structured invocation cannot bypass the session catalog's `callable` decision.
13. Claude, Codex, and OpenCode use native discovery; ACP requires explicit advertisement.
14. Existing AgentConfig and user-managed Skills remain unchanged during upgrade.
15. Cron creation is guided by the activated `hotplex-cli` Skill or, if unavailable, by the installed binary's help with an explicit degraded status; creation is verified with `cron get`.
16. Phase A, Phase B, and Phase C remain independently implementable and reversible.

## 18. Best-Practice Basis

This revision is grounded in primary product and standard documentation reviewed on 2026-08-22:

- [OpenAI: Build skills](https://developers.openai.com/codex/skills) — progressive disclosure, concise trigger descriptions, Codex catalog budgeting, local Skill roots, and plugin distribution guidance.
- [Agent Skills specification](https://agentskills.io/specification) — portable `SKILL.md` format, frontmatter constraints, directory structure, references, and validation.
- [Agent Skills client implementation guide](https://agentskills.io/client-implementation/adding-skills-support) — catalog/activation coupling, deterministic precedence, project trust, built-in packaging, and bounded discovery.
- [Anthropic: Extend Claude with skills](https://code.claude.com/docs/en/skills) — native Skill lifecycle, concise bodies, supporting files, and explicit control for side-effecting workflows.
- [OpenCode: Agent Skills](https://opencode.ai/docs/skills) — native roots, on-demand Skill tool loading, permission filtering, and omission of unavailable catalogs.

Where clients differ, this design uses the portable standard as the package baseline and treats client-specific behavior as adapter evidence rather than a universal promise.
