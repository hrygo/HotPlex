# HotPlex Self-Awareness Architecture Design

**Date:** 2026-08-22  
**Status:** Proposed  
**Scope:** Stable metacognition, trusted runtime facts, AgentConfig template correction, capability routing, and built-in HotPlex Agent Skills  
**Related design:** `docs/superpowers/specs/2026-08-22-hotplex-agent-metacognition-tools-design.md`

## 1. Problem

HotPlex now distinguishes the AgentConfig `TOOLS.md` slot from Agent Skills, but the Agent still lacks a complete and reliable model of HotPlex itself.

The current state has five gaps:

1. `META-COGNITION.md` describes identity and configuration boundaries but receives no trusted per-session runtime facts.
2. The default `TOOLS.md` names several capabilities without giving a complete routing model or a safe minimum execution contract.
3. The default `SOUL.md`, `AGENTS.md`, and `MEMORY.md` contain claims that are not universally true, such as every session being non-interactive, fixed permission/retry timing, and Agent-managed memory writes.
4. Detailed HotPlex CLI guidance is split between a repository-local `hotplex-cli` Skill, Cobra help, and legacy embedded manuals. The repository-local Skill is not a product distribution mechanism.
5. `~/.hotplex/skills` is scanned by HotPlex, but discovery is not equivalent to Worker callability. The existing flat `cron.md`, `phrases.md`, and `db-stats.md` manuals have no valid Skill frontmatter and therefore are not discovered by the current scanner at all.

This produces two dangerous false inferences:

- a documented capability may be treated as available even when the current Worker cannot invoke it;
- a filesystem Skill may be treated as callable merely because HotPlex found its Markdown file.

## 2. Goals

1. Give every Agent a stable, accurate model of HotPlex, Gateway, Worker, AgentConfig, tools, commands, and Agent Skills.
2. Inject trusted per-session facts without exposing credentials or treating dynamic catalogs as permanent prompt content.
3. Make `hotplex-cli` a formal built-in Agent Skill distributed with the product.
4. Keep the `hotplex-cli` Skill aligned with the actual Cobra command tree and validators.
5. Make built-in Skills available through Worker-native Skill roots without overwriting user-owned content.
6. Preserve the difference between `discoverable` and `callable` across listing and dispatch.
7. Correct all five default AgentConfig templates without overwriting existing user configuration during upgrade.
8. Provide deterministic degradation: query current capabilities, use CLI help where applicable, or report `unsupported`.

## 3. Non-Goals

- Implementing the planned `SystemPromptDelivery` capability contract.
- Implementing the planned AgentConfig inspect/plan/approve/apply/verify control plane.
- Injecting a dynamic Skill catalog or Skill bodies into the system prompt.
- Automatically rewriting existing `~/.hotplex/agent-configs/*.md` files.
- Making ACP load filesystem Skills that its underlying Agent has not advertised.
- Changing the domain semantics of Admin Skills APIs, WebChat Skills pages, Worker `/skills`, or AEP `skills` fields.
- Turning release, documentation patrol, or repository-contributor workflows into runtime Bot capabilities.

## 4. Design Principles

### 4.1 Stable knowledge and dynamic facts are different layers

Long-lived system identity belongs in embedded metacognition. Per-session facts belong in a separately generated runtime section. Detailed operational workflows belong in Agent Skills. User preferences remain in editable AgentConfig slots.

### 4.2 Availability is never inferred from documentation

`TOOLS.md`, Skill files, and CLI examples describe how to use a capability. They do not prove that the current Worker or host exposes it.

### 4.3 Discovery is not callability

A filesystem definition is `discoverable`. It becomes `callable` only when the current Worker adapter can route it through its supported Skill mechanism.

### 4.4 The model is not a capability authority

Model text cannot promote a Skill, grant permission, select an Admin scope, or prove that a mutation succeeded.

### 4.5 Product knowledge must have one canonical source

The built-in `hotplex-cli` package is the canonical product source. Repository development copies and runtime materializations are derived artifacts verified by hashes and tests.

## 5. Four-Layer Self-Awareness Model

```text
┌─────────────────────────────────────────────────────────────┐
│ 1. Embedded META                                            │
│    Stable identity, trust model, discovery and degradation  │
├─────────────────────────────────────────────────────────────┤
│ 2. Gateway Runtime Facts                                    │
│    Current platform, Worker, scope and declared capability  │
├─────────────────────────────────────────────────────────────┤
│ 3. Editable AgentConfig                                     │
│    SOUL / AGENTS / TOOLS / USER / MEMORY                    │
├─────────────────────────────────────────────────────────────┤
│ 4. Agent Skills                                             │
│    On-demand workflows, Worker-authoritative callability    │
└─────────────────────────────────────────────────────────────┘
```

### 5.1 Layer 1: Embedded META

`META-COGNITION.md` remains small and always injected. It defines:

- Gateway owns transport, routing, session state, permissions, retries, and protocol state.
- Worker owns task reasoning and current Worker-native execution.
- HotPlex capability domains include Gateway commands, Worker commands, CLI, MCP, Admin APIs, and Agent Skills.
- `TOOLS.md` provides operational guidance and boundaries; Agent Skills provide on-demand workflows through `SKILL.md`.
- Capability discovery follows this order:

```text
structured runtime capability
→ trusted runtime facts
→ current /skills catalog
→ activated Agent Skill
→ TOOLS routing
→ hotplex <domain> --help
→ explicit unsupported/degraded result
```

- Only entries reported `callable` may be invoked as Skills.
- Configuration and external mutations require the authority appropriate to that operation.
- No concrete Skill names, Skill descriptions, CLI flags, fixed timeouts, or Worker versions are embedded in META.

The section title is “TOOLS 与 Agent Skills 的关系”; the word “真实” is not used.

### 5.2 Layer 2: Trusted Runtime Facts

Gateway produces a typed `RuntimeFacts` value before system-prompt assembly:

```go
type RuntimeFacts struct {
    Platform       string
    WorkerType     worker.WorkerType
    ScopeKind      RuntimeScopeKind
    BotName        string
    WorkspaceID    string
    PermissionMode string
    SupportsResume bool
    SupportsStream bool
    SupportsTools  bool
    Modalities     []string
    GatewayEnvKeys []string
    SkillsQuery    bool
    MCPStatusQuery bool
}
```

`RuntimeScopeKind` is one of `bot`, `workspace`, or `unbound`.

Only non-empty facts are rendered. All strings are size-bounded and XML-sanitized. `GatewayEnvKeys` contains names whose values are present, never their values. It may include:

- `GATEWAY_PLATFORM`
- `GATEWAY_BOT_ID`
- `GATEWAY_BOT_NAME`
- `GATEWAY_USER_ID`
- `GATEWAY_SESSION_ID`
- `GATEWAY_WORK_DIR`
- `GATEWAY_CHANNEL_ID`
- `GATEWAY_THREAD_ID`
- `GATEWAY_TEAM_ID`

Runtime facts never contain:

- credentials, tokens, connection strings, or arbitrary environment values;
- Skill names, descriptions, bodies, or filesystem paths;
- complete tool names or MCP server configuration;
- claims that a system prompt was delivered;
- observed state that is unavailable before Worker startup.

### 5.3 Layer 3: Editable AgentConfig

The five editable slots keep their existing domains:

- `SOUL.md`: persona and communication style.
- `AGENTS.md`: behavioral rules and work constraints.
- `TOOLS.md`: capability routing, operational preferences, and safety boundaries.
- `USER.md`: user facts and preferences.
- `MEMORY.md`: historical context supplied by the host or an authorized future control plane.

Templates describe global, platform, Bot, and Workspace behavior accurately. They do not promise that a named Skill exists.

### 5.4 Layer 4: Agent Skills

Agent Skills remain on-demand workflow packages. Their frontmatter is used by the Skills catalog and Worker-native discovery, not promoted into the system prompt.

The system prompt contains only the rule to query current Skills and honor their `callable`/`discoverable` status.

## 6. Prompt Assembly

### 6.1 Additive API

Preserve the existing function for callers that have no live session:

```go
func BuildSystemPrompt(configs *AgentConfigs) string
```

Add:

```go
func BuildSystemPromptWithRuntime(configs *AgentConfigs, facts RuntimeFacts) string
```

`BuildSystemPrompt` delegates with empty facts. Admin Bot preview therefore remains compatible and does not invent a session context.

### 6.2 Schema version 3

The assembled prompt becomes:

```xml
<agent-configuration schema-version="3">
  <runtime-facts>
    ...trusted bounded facts...
  </runtime-facts>
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

`runtime-facts` is trusted declarative data, not editable instructions. It precedes directives so its provenance is unambiguous, but directives still define behavior.

### 6.3 Bridge ownership

Bridge builds facts from the current session metadata, selected Worker, Worker `Capabilities`, and already resolved permission mode. The Worker adapters do not build or mutate HotPlex runtime facts.

Reset reconstructs the prompt with fresh AgentConfig and the same live-session fact builder. A later `SystemPromptDelivery` implementation may add delivery evidence without changing this fact model.

## 7. Default Template Corrections

### 7.1 `SOUL.md`

Remove universal claims that every Worker is non-interactive, permission requests always time out after five minutes, and transient failures always retry three times. State that interaction and retry behavior are runtime/platform facts.

### 7.2 `AGENTS.md`

Remove blanket authorization for dependency installation, Git merge, and other mutations unrelated to a specific user request. Use scoped authorization rules consistent with the host.

### 7.3 `TOOLS.md`

Make it an operational router rather than an incomplete command list. It covers:

- capability discovery and `callable` versus `discoverable`;
- Gateway commands: `/help`, `/stop`, `/reset`, `/new`, `/gc`, `/park`, `/cd`, `/skills`, `/mcp`, and `/worker`;
- HotPlex CLI routing to the `hotplex-cli` Skill;
- Slack and Feishu separation (`lark-cli` is not part of `hotplex-cli`);
- Cron minimum execution contract and post-create verification;
- read-only diagnostics versus scoped mutations versus privileged service operations;
- STT/TTS runtime dependence;
- configuration activation on new Session or `/reset`.

It does not duplicate every CLI flag or reference page.

### 7.4 `USER.md`

Keep only user-supplied background and preferences. Remove advice that masquerades as runtime fact.

### 7.5 `MEMORY.md`

Remove the claim that the Agent currently auto-manages the file. State that memory changes require an authorized host/control interface; until that interface exists, the file is context only.

### 7.6 Upgrade behavior

Template updates affect new onboarding and explicit template reset only. Upgrade never overwrites existing AgentConfig files. Existing instances still receive the updated embedded META, runtime facts, and product built-in Skills.

## 8. `hotplex-cli` Built-In Agent Skill

### 8.1 Canonical package

The canonical product package is embedded from a Go-owned asset directory:

```text
internal/skills/builtin/hotplex-cli/
├── SKILL.md
└── references/
    ├── cron.md
    ├── slack.md
    ├── diagnostics.md
    ├── service-update.md
    └── admin-audit-runtime.md
```

Frontmatter:

```yaml
---
name: hotplex-cli
description: Use HotPlex CLI for Cron, Slack, diagnostics, service lifecycle, configuration, audit, and runtime operations.
---
```

The description is catalog metadata only. It is not injected into AgentConfig prompt content.

### 8.2 Skill routing

`SKILL.md` contains:

- activation conditions;
- a read-only/scoped-mutation/privileged-operation risk matrix;
- the rule that current `hotplex <domain> --help` is authoritative when a reference and binary disagree;
- runtime identity environment-variable usage without printing values;
- which reference file to read for each task;
- postcondition verification requirements.

Reference files contain the detailed workflows.

### 8.3 Cron reference

The Cron reference is reconciled with `newCronCmd`, `newCronCreateCmd`, schedule validation, and lifecycle validation. It covers:

- `cron:`, `every:`, `at:<RFC3339>`, and `at:+<duration>`;
- isolated versus `--attach` execution;
- self-contained isolated prompts;
- `GATEWAY_*` identity and delivery routing;
- recurring `--max-runs` and `--expires-at` requirements;
- `--silent`, `--delete-after-run`, `--max-retries`, `--timeout`, and `--allowed-tools`;
- `list`, `get`, `update`, `delete`, `trigger`, and `history`;
- post-create `get` verification;
- portable examples without shell-specific date arithmetic.

The legacy `internal/cron/cron-skill-manual.md` becomes a generated compatibility copy or is replaced by the canonical Cron reference at build time. It is not maintained independently.

### 8.4 Slack reference

The Slack reference is reconciled with the current Cobra subcommands for messages, scheduled messages, files, channels, bookmarks, and reactions. It distinguishes an explicit user request from an unauthorized external message.

### 8.5 Diagnostics and privileged references

Read-only operations include version, status, doctor, security inspection, config validation, service status/logs, and Cron list/get/history.

State-changing operations such as Cron mutation and Slack delivery require a matching user request. Service installation/restart/uninstall, binary update, Admin mutation, audit rebase, and runtime-fence decisions require explicit host/admin authority.

No Skill example embeds Admin tokens, credentials, private repository URLs, or actual environment values.

## 9. Other Built-In Operational Skills

The same packaging mechanism standardizes the existing non-CLI manuals without merging them into `hotplex-cli`:

```text
internal/skills/builtin/hotplex-phrases/SKILL.md
internal/skills/builtin/hotplex-db-stats/SKILL.md
```

- `hotplex-phrases` manages programmatic messaging phrases.
- `hotplex-db-stats` guides database detection and bounded statistics/diagnostics.

They have independent activation descriptions and security boundaries. Their metadata is also excluded from the system prompt.

Repository-only workflows such as release and documentation patrol remain repository Skills and are not distributed to runtime Bots.

## 10. Built-In Skill Distribution and Materialization

### 10.1 Canonical inventory

At startup/onboard, HotPlex atomically releases canonical packages to:

```text
$HOTPLEX_HOME/skills/builtin/<name>/
```

With the default home this is `~/.hotplex/skills/builtin/<name>/`.

This root is the HotPlex-owned source inventory. It is read-only through Skills management APIs.

### 10.2 Worker-native projections

HotPlex projects built-ins into native roots:

| Worker | Projection root | Callable rule |
|---|---|---|
| Claude Code | `~/.claude/skills/<name>/` | adapter confirms the projected native package |
| Codex CLI | `~/.agents/skills/<name>/` | app-server `skills/list` advertises it |
| OpenCode Server | `~/.agents/skills/<name>/` | `/command` advertises it |
| ACP | no filesystem assumption | `available_commands_update` advertises it |

Materializing both Claude and `.agents` roots also supports ACP implementations backed by those runtimes, but ACP remains non-callable until it advertises the command.

### 10.3 Managed marker

Each projected package contains:

```text
.hotplex-builtin.json
```

With:

```json
{
  "schema_version": 1,
  "name": "hotplex-cli",
  "package_version": 1,
  "source_sha256": "...",
  "installed_sha256": "..."
}
```

The marker contains no credentials or machine paths.

### 10.4 Collision and update rules

For each target:

1. Missing target: write a sibling temp directory, sync files, then rename atomically.
2. Valid HotPlex marker and unmodified installed hash: replace with the new package atomically.
3. Missing/invalid marker: treat as user-owned; do not overwrite.
4. Valid marker but changed installed content: treat as user-modified; do not overwrite.
5. Symlinked Claude/`.agents` roots resolving to the same directory: materialize once.
6. Failure leaves the previous complete package intact and emits a bounded diagnostic.

User/workspace Skills may shadow a built-in by name. HotPlex reports the provenance and does not silently overwrite the user package. `doctor` reports the collision and the built-in remains non-callable unless the Worker advertises the intended package.

### 10.5 Provenance

Skills scanning recognizes the marker and reports built-ins as:

```text
source: builtin
managed: false
```

Admin/WebChat may display built-ins but cannot update or delete them through managed-Skill CRUD. The response extension is additive; existing global/project source values remain valid for non-built-ins.

## 11. Discovery and Dispatch Semantics

### 11.1 No prompt catalog injection

Skill `name`, `description`, body, path, and status are never appended to `<runtime-facts>`, `<directives>`, or `<context>`.

Reasons:

- catalogs change after Session start;
- descriptions are external data and may be instruction-shaped;
- prompt injection would confuse discoverability with callability;
- large catalogs create permanent token cost;
- `/skills` already carries structured status.

### 11.2 `/skills` is the session catalog surface

The session catalog returns names, descriptions, provenance, and status. Only `callable` entries may be executed as Skills.

Filesystem-only entries remain `discoverable`. The Agent may explain that a definition exists, but must not invoke it or claim it is loaded.

### 11.3 Dispatch must use the same status decision

Short `/name` and explicit `/worker <name>` dispatch resolve through the same merged session catalog used by `/skills`.

- `callable`: dispatch through the Worker-native invoker.
- `discoverable`: return `NOT_SUPPORTED` with a bounded message.
- missing/ambiguous/stale: return the existing bounded error class.

The current raw filesystem short-command path must not bypass the Worker-authoritative callability decision.

## 12. CLI Content Drift Prevention

Contract tests in `cmd/hotplex` load the embedded `hotplex-cli` package and validate:

- every documented top-level `hotplex` domain exists in the root Cobra tree;
- every documented Cron and Slack subcommand resolves;
- every documented long flag exists on the referenced Cobra command;
- Cron schedule forms match the schedule parser;
- recurring lifecycle requirements match validation;
- `--attach` environment behavior matches implementation;
- examples contain no shell-specific date calculation, credentials, or absolute private paths;
- the repository development copy matches the canonical package hash.

The test does not require prose to mirror generated `--help` byte-for-byte. It validates executable command/flag anchors while allowing explanatory text to remain readable.

## 13. Compatibility

### 13.1 Prompt callers

`BuildSystemPrompt` remains available. New live-session code uses `BuildSystemPromptWithRuntime`.

### 13.2 AgentConfig

No new editable slot is added. `TOOLS.md` remains canonical, and legacy `SKILLS.md` compatibility remains unchanged.

### 13.3 Skills APIs

Existing fields remain. Built-in provenance is additive. Managed CRUD continues to accept only user-managed `.agents/skills` packages and rejects built-ins as read-only.

### 13.4 Legacy manuals

Flat `~/.hotplex/skills/cron.md`, `phrases.md`, and `db-stats.md` paths remain compatibility copies for one minor release. They do not carry frontmatter, do not enter the catalog, and cannot shadow canonical packages. Doctor warns about direct reliance and points to the package directory.

## 14. Security

- Runtime facts are allowlisted, sanitized, and size-bounded.
- Environment values and tokens never enter prompt facts or Skill examples.
- Skill descriptions remain untrusted catalog data, not directives.
- Materialization never follows an untrusted target symlink outside a validated native root.
- Package extraction is not involved; all built-in files originate from `go:embed`.
- Atomic directory replacement prevents partial packages.
- User-owned collisions are preserved.
- Built-in packages are read-only through Admin/WebChat CRUD.
- State-changing CLI workflows explicitly distinguish user request from host/admin authority.
- Skill invocation fails closed when Worker authority is missing or stale.

## 15. Observability and Diagnostics

Logs and doctor may report:

- built-in package name and version;
- target root class, never private body content;
- source/installed hash prefixes;
- installed, updated, unchanged, collision, user-modified, or failed status;
- Worker catalog state: callable, discoverable, unavailable.

They do not report Skill bodies, environment values, tokens, or arbitrary filesystem content.

## 16. Testing Strategy

### 16.1 Prompt and runtime facts

- schema version 3 ordering;
- empty/partial facts;
- XML/control-character sanitization;
- secret-shaped data excluded by construction;
- Slack, Feishu, WebChat, Cron, Bot, Workspace, and unbound fact matrices;
- Admin preview remains runtime-neutral.

### 16.2 Templates

- no fixed interaction timeout/retry claims;
- no claim of automatic MEMORY writes;
- no unconditional Skill availability;
- correct scope and activation language;
- complete Gateway command routing anchors.

### 16.3 Built-in packages

- embedded package integrity;
- valid frontmatter;
- reference-link closure;
- atomic initial install and update;
- user collision preservation;
- modified managed copy preservation;
- symlink-root deduplication;
- read-only provenance;
- legacy compatibility copies.

### 16.4 Worker matrix

- Claude projected package catalog/invocation path;
- Codex `skills/list` confirmation and structured Skill item;
- OpenCode `/command` confirmation and RPC invocation;
- ACP advertised/unadvertised cases;
- filesystem-only definitions never become callable.

### 16.5 CLI contracts

- Cobra command and flag anchors;
- Cron schedule/lifecycle validation;
- Slack surface;
- diagnostics and privileged-operation classification.

## 17. Documentation Impact

Implementation updates the current documentation center:

- `docs/explanation/agent-config-system.md`
- `docs/explanation/cron-design.md`
- `docs/tutorials/agent-personality.md`
- `docs/tutorials/skills-setup.md`
- `docs/tutorials/cron-scheduled-tasks.md`
- `docs/guides/user/commands-cheatsheet.md`
- `docs/reference/cli.md`
- `docs/reference/configuration.md`
- `docs/reference/admin-api.md` if built-in provenance is exposed there

Historical archive/spec documents are not rewritten.

## 18. Rollout and Rollback

### Rollout

1. Ship runtime facts and corrected META/templates.
2. Ship the built-in package registry and safe materializer.
3. Release `hotplex-cli`, `hotplex-phrases`, and `hotplex-db-stats` canonical packages.
4. Enable unified callable-status dispatch.
5. Update documentation and doctor diagnostics.

### Rollback

- Removing runtime facts falls back to the compatible `BuildSystemPrompt` path.
- Materialized packages remain complete Skills; rollback does not delete them automatically.
- A prior binary may ignore `.hotplex-builtin.json` safely.
- Legacy flat manuals remain available for the compatibility minor.
- No existing AgentConfig or user-managed Skill is overwritten, so rollback does not require restoring user data.

## 19. Acceptance Criteria

1. A new Session can identify its platform, Worker, scope kind, permission mode, and declared capabilities without shell inspection.
2. No Skill metadata or body is present in the assembled system prompt.
3. `hotplex-cli` is shipped as a valid built-in Agent Skill with task-specific references.
4. Its commands, flags, Cron schedules, and lifecycle rules pass contract tests against current code.
5. Built-in materialization never overwrites an unmarked or user-modified Skill.
6. `/skills` reports built-in provenance and distinguishes `callable` from `discoverable`.
7. `/name` cannot execute a filesystem-only discoverable Skill.
8. Claude, Codex, and OpenCode use their native Skill paths; ACP requires explicit advertisement.
9. Default templates contain no false fixed runtime claims and correctly describe Workspace behavior.
10. Existing AgentConfig files and user-managed Skills remain unchanged during upgrade.
11. Cron creation remains possible without prompt catalog injection: activate callable `hotplex-cli`, otherwise use the documented CLI-help degradation path.
12. The separate SystemPromptDelivery and safe self-configuration plans remain deferred and independently implementable.
