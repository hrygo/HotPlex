# HotPlex Initialization and First-Run UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make first-time Feishu onboarding safe, source-consistent, and understandable to ordinary users.

**Architecture:** Keep `config.yaml` as the structural configuration and `.env` as the runtime override/credential source. Extend the existing `hotplex-cli` runtime package with user-facing guidance; keep operator actions in `hotplex-operator`.

**Tech Stack:** Go 1.26, Cobra, YAML AST helpers, `testify/require`, embedded Skills, Markdown docs.

**Spec:** `docs/superpowers/specs/2026-08-24-onboarding-first-run-ux-design.md`

## Global Constraints

- Preserve existing credentials and unrelated `.env` entries.
- Keep Feishu DM/group defaults as `allowlist`; keep group mention enabled by default.
- Never expose credentials in output, logs, tests, or documentation.
- Use table-driven `testify/require` tests; no `time.Sleep` for async tests.
- Keep canonical built-in Skill sources under `internal/skills/builtin`; regenerate mirrors and manifest.

---

### Task 1: Preserve non-interactive platform state

**Files:**

- Modify: `internal/cli/onboard/wizard.go`
- Test: `internal/cli/onboard/wizard_test.go`

- [x] Add failing tests proving existing Feishu credentials and `ALLOW_FROM` survive a non-interactive enabled-platform rewrite.
- [x] Run `go test ./internal/cli/onboard -run 'BuildEnvContent|NonInteractive' -count=1` and confirm the regression fails.
- [x] Update `.env` generation to preserve existing managed credentials when no replacement was supplied and emit selected `ALLOW_FROM` values.
- [x] Add policy keys to the managed-key set only after preservation is implemented.
- [x] Run focused tests and `gofmt`.

### Task 2: Improve Feishu onboarding and user guidance

**Files:**

- Modify: `internal/cli/onboard/guides/feishu.md`
- Modify: `docs/tutorials/feishu-integration.md`
- Modify: `docs/getting-started.md`
- Modify: `docs/guides/user/chat-with-ai.md`
- Modify: `docs/reference/cli.md`
- Modify: `docs/guides/enterprise/config-management.md`

- [x] Add the OpenID/allowlist bootstrap path, group mention rule, config/.env path rule, and restart/verification commands.
- [x] Correct the statement that installed Gateway binaries never load adjacent `.env` files.
- [x] Document the exact non-interactive Feishu example with `--feishu-allow-from`.
- [x] Build and lint docs.

### Task 3: Extend runtime user guidance Skill

**Files:**

- Modify: `internal/skills/builtin/hotplex-cli/SKILL.md`
- Create: `internal/skills/builtin/hotplex-cli/references/user-guide.md`
- Regenerate: `internal/skills/builtin/manifest.generated.go`, `.agents/skills/hotplex-cli/**`

- [x] Add a read-only ordinary-user guide for chat commands, Feishu mention/allowlist symptoms, and escalation boundaries.
- [x] Regenerate embedded manifest and repository mirror with `go generate ./internal/skills/builtin`.
- [x] Validate the Skill and run builtin manifest/quality tests.

### Task 4: Full verification and review

- [x] Run focused Go tests, relevant package tests, `go test ./...`, `make docs-build`, and `make docs-lint`.
- [x] Review the diff for secrets, source-path contradictions, and operator privilege leakage.
- [ ] Commit atomic changes with descriptive messages and report intentionally untouched follow-ups.
