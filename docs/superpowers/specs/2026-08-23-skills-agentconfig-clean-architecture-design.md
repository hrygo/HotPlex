# Skills and AgentConfig Clean Architecture Design

**Date:** 2026-08-23  
**Status:** Approved in chat; pending document review  
**Scope:** HotPlex Agent Skills, AgentConfig boundaries, and repository Skills under `.agents/skills`

## 1. Objective

HotPlex must expose one unambiguous model:

- AgentConfig is a five-file prompt configuration model.
- Agent Skills are independently discovered `<name>/SKILL.md` packages.
- Built-in Skills have one canonical source and deterministic repository mirrors.
- Historical Skills and AgentConfig layouts are unsupported rather than silently adapted.

This change intentionally removes compatibility behavior in this bounded domain. It is a breaking cleanup, not a migration layer.

## 2. Scope

### Included

- Remove the AgentConfig `SKILLS.md` read alias and all related normalization, metadata, diagnostics, tests, and current documentation.
- Remove the legacy `platform/default/` AgentConfig fallback.
- Remove diagnostics dedicated to deprecated platform-suffix AgentConfig files.
- Remove startup publication of flat `cron.md`, `phrases.md`, and `db-stats.md` manuals under the HotPlex Skills directory.
- Consolidate `.agents/skills` around non-overlapping runtime, operator, diagnostic, release, and documentation responsibilities.
- Generate repository mirrors for both embedded built-in Skills.
- Replace unsafe, stale, or overly prescriptive Skill instructions with capability-oriented references backed by the current CLI and source.

### Excluded

- Worker adapters for Claude Code, Codex CLI, OpenCode Server, and ACP.
- OS support for Linux, macOS, and Windows.
- AEP compatibility outside the Skills/AgentConfig surface.
- Unrelated configuration, protocol, session, database, or messaging compatibility behavior.
- Automatic deletion or rewriting of user-owned legacy files.

## 3. Canonical Domain Model

### 3.1 AgentConfig

AgentConfig recognizes exactly these logical files:

1. `SOUL.md`
2. `AGENTS.md`
3. `TOOLS.md`
4. `USER.md`
5. `MEMORY.md`

`META-COGNITION.md` remains embedded and non-editable. `TOOLS.md` remains environment guidance; it does not declare tools or Agent Skills.

Each editable file resolves independently through only three scopes:

```text
bot:      <config-root>/<platform>/<bot>/<FILE>
platform: <config-root>/<platform>/<FILE>
global:   <config-root>/<FILE>
```

The first present file wins, including a present-empty file. There is no `platform/default/` lookup, filename alias, or suffix-layout lookup.

### 3.2 Agent Skills

A real Agent Skill is recognized only as:

```text
<skills-root>/<name>/SKILL.md
```

Flat Markdown manuals are not Skills. HotPlex does not publish them at startup, convert them, or treat them as AgentConfig. Skill metadata remains progressively disclosed by the native Worker or HotPlex Skill catalog; it is not copied into AgentConfig prompts.

### 3.3 Built-in packages and mirrors

`internal/skills/builtin/{hotplex-cli,hotplex-operator}` is the sole canonical source for embedded built-in packages. `cmd/gen-builtin-skills` generates:

- the embedded manifest;
- `.agents/skills/hotplex-cli`;
- `.agents/skills/hotplex-operator`.

The generated mirrors must be byte-identical to their canonical package trees. Manually editing either mirror is unsupported; changes begin in the canonical package.

## 4. Repository Skill Portfolio

The target `.agents/skills` portfolio contains five Skills.

| Skill | Responsibility | Mutation boundary |
|---|---|---|
| `hotplex-cli` | Cron, explicitly requested Slack operations, and ordinary read-only CLI diagnostics | Mutating Cron/Slack actions require the user's request; no host administration |
| `hotplex-operator` | Installation, configuration, service lifecycle, binary updates, Admin mutations, built-in Skill reconciliation | Requires explicit host/operator/Admin authority and pre-mutation impact disclosure |
| `hotplex-diagnostics` | Deep read-only runtime diagnosis across process, session, feedback, logs, and source evidence | Never infers repair, credential access, Issue creation, or Admin mutation authority |
| `hotplex-release` | Version selection, changelog, release validation, tagging, and GitHub Release publication | Remote writes, tags, and publication require explicit release authorization |
| `hotplex-docs-patrol` | Change-driven maintenance of current BFS-reachable documentation | Does not automatically create branches, Issues, PRs, or pushes without authorization |

`hotplex-setup` and `hotplex-update` are removed. Accurate, non-duplicated operational guidance moves into narrow `hotplex-operator` references. Deep diagnostic knowledge remains separate so a read-only diagnosis cannot route into a privileged operator workflow.

## 5. Compatibility Removal

### 5.1 AgentConfig code and APIs

Remove:

- `LegacyFileSkills` and `AgentConfigLegacySkills` constants;
- `readAliases`, legacy filename canonicalization, coexistence warnings, and legacy override conflict handling;
- `LegacyDefaultBotName` and the `platform/default/` fallback in loader and provenance resolution;
- Admin `agent_configs.skills` and WebChat's deprecated `skills` metadata;
- WebChat `normalizeLegacyToolsOverride` and preservation of legacy override keys;
- doctor checks and fix hints that exist only to identify or migrate `SKILLS.md` or suffix layouts;
- current documentation describing compatibility windows, aliases, or migration behavior.

After the change:

- Admin AgentConfig endpoints accept only canonical file names.
- Workspace override validation rejects `SKILLS.md` as an unknown key.
- `inject_exclude` recognizes only canonical names; `SKILLS.md` has no effect.
- Legacy files already present on disk are neither read nor deleted.

Remove `ErrConflictingConfigFiles`; its only purpose is the canonical/legacy filename collision that this design eliminates.

### 5.2 Flat manual publishers

Remove all startup paths that write:

- `$HOTPLEX_HOME/skills/cron.md`;
- `$HOTPLEX_HOME/skills/phrases.md`;
- `$HOTPLEX_HOME/skills/db-stats.md`.

Remove the corresponding embed wrappers and legacy manual assets. The current source audit found no non-compatibility consumer for those assets. Canonical Cron guidance remains in `hotplex-cli/references/cron.md`. Phrase customization and database diagnostics remain ordinary product documentation or CLI guidance, not pseudo-Skills.

No cleanup routine deletes existing user files. Removing unsupported artifacts from a user's filesystem requires a separate explicit user action.

## 6. Skill Content Architecture

Every retained Skill follows progressive disclosure:

- Frontmatter contains a concise, discriminating `name` and `description`.
- `SKILL.md` contains only routing, authority boundaries, and essential invariants.
- Detailed modes, commands, schemas, and troubleshooting live in linked references.
- Current `hotplex <domain> --help`, current source, and generated CLI surfaces override copied examples.

Content rules:

- Do not read tokens from `.env` into shell variables or print secret-bearing commands.
- Do not pipe remote install scripts directly into a shell in canonical instructions.
- Do not replace binaries with ad hoc `cp`, fixed `sleep`, or checkout-based rollback when `hotplex update` and service primitives exist.
- Do not create Issues, PRs, tags, releases, pushes, or Admin mutations unless the user's request authorizes that side effect.
- Do not hard-code mutable checker counts, artifact counts, version locations, or line numbers when the CLI or repository can report them.
- Do not use slash-prefixed Skill names as an invocation contract.

## 7. Data and Control Flow

### AgentConfig read

```text
requested canonical slot
  -> bot exact path
  -> platform exact path
  -> global exact path
  -> missing
```

No compatibility branch participates in this flow.

### Built-in Skill generation

```text
canonical built-in package trees
  -> validate allowed assets and frontmatter
  -> content-addressed manifest
  -> atomic mirror of hotplex-cli
  -> atomic mirror of hotplex-operator
```

### Skill selection

```text
request intent + authority
  -> one discriminating Skill description
  -> short SKILL.md router
  -> only the needed reference
  -> current CLI/source verification
  -> authorized action or read-only report
```

## 8. Error Handling and Breaking Behavior

- Legacy AgentConfig API names and workspace keys fail with the existing unknown-file validation path.
- A legacy file found only on disk behaves as absent and normal scope fallback continues.
- Generated mirror failure is atomic: preserve the previous complete mirror and fail the generator.
- A missing canonical built-in asset fails generation and tests.
- Unsupported or unauthorized Skill actions stop before external mutation and report the missing authority.

There is no compatibility warning followed by fallback. Errors are either canonical validation failures or normal missing-file behavior.

## 9. Verification Strategy

Implementation follows test-driven development.

### AgentConfig contracts

- Red tests prove `SKILLS.md` workspace/Admin input is rejected.
- Red tests prove a disk-only `SKILLS.md` is ignored.
- Red tests prove `platform/default/` is ignored.
- Existing canonical fallback, present-empty, size, traversal, and atomic-write tests remain green.
- Admin and WebChat types contain only the five canonical AgentConfig fields.

### Legacy publisher removal

- Gateway, Cron, and messaging startup tests prove no flat manual is written.
- No production source references `ReleaseSkillManual`, legacy manual assets, or flat pseudo-Skill filenames.

### Skill quality

- Parse all `.agents/skills/*/SKILL.md` frontmatter with strict YAML.
- Enforce directory/name equality, concise descriptions, reachable contained references, and absence of unfinished placeholders.
- Enforce no legacy `SKILLS.md`, flat manual publisher, secret-extraction, fixed-sleep update, or automatic remote-mutation instructions in current Skill trees.
- Verify canonical/mirror byte equality for both built-in packages.
- Use trigger evaluations for overlap among CLI, operator, diagnostics, release, and docs patrol.

### Project gates

- Focused Go and WebChat tests while iterating.
- Race tests for changed Go packages.
- `go test ./...` and `go vet ./...`.
- WebChat Vitest, TypeScript, and ESLint.
- Built-in generators twice with zero diff.
- Documentation build and link validation.
- `git diff --check` and a clean working tree before completion.

## 10. Documentation and Release Impact

Update only current, BFS-reachable documentation and API specifications. Historical specifications may continue to describe past behavior because they are records, not current guidance.

Current documentation must state the strict model without a compatibility section. The next release changelog must call out:

- removal of `SKILLS.md` AgentConfig support;
- removal of `platform/default/` lookup;
- removal of flat manual publication;
- consolidation of setup/update guidance into `hotplex-operator`.

This design does not itself authorize a release.

## 11. Acceptance Criteria

The change is complete when:

1. Current production code contains no Skills/AgentConfig compatibility branch listed in this design.
2. `.agents/skills` contains exactly the five target Skills.
3. Both built-in mirrors are generated and byte-identical to canonical sources.
4. HotPlex startup performs no legacy flat-manual writes.
5. Admin, workspace, WebChat, docs, and tests expose only canonical AgentConfig names.
6. All focused and full quality gates pass.
7. No user-owned legacy file is deleted or rewritten.
