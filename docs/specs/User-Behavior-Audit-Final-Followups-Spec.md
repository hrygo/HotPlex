# User Behavior Audit Final Follow-ups Spec

| Item | Value |
|---|---|
| Status | Implemented in PR #854 |
| Date | 2026-07-07 |
| Issue | #833 |
| Base | main after PR #845 |
| PR | https://github.com/hrygo/hotplex/pull/854 |

## 1. Goal

Issue #833 has delivered the audit core through P1 and P2. This PR closes the remaining operational gaps in one reviewable package:

- admin UI timeline for `user_activity`
- cross-channel user activity lookup through explicit identity links
- SIEM/export posture using the built-in sink/export surfaces
- cold archive posture through downloadable JSON/CSV activity exports
- PostgreSQL follow-up note for monthly partitioning
- legacy `admin_audit` slog retirement plan

The PR must not expand #833 into a full alerting rules engine. Rules, deduplication, alert lifecycle, and channel-specific alert delivery remain a separate subsystem behind the existing `AlertSink` contract.

## 2. Current State

Already complete on main:

- append-only `user_activity` table with hash chain and checkpoint GC
- zero-loss collector with spill WAL
- auth/session/message/tool/admin write points
- `NoopSink`, `LogSink`, `WebhookSink`, and sink registry
- `/admin/activity`, `/admin/users/{id}/activity`, and JSON/CSV export endpoints
- admin workspace console from issue #807
- `audit.full_content_retention`
- `system.audit_config_changed` meta-audit

This spec covers the final operational access and identity resolution layer.

## 3. Design

### 3.1 Admin Activity Timeline

Add `/admin/activity` to the embedded webchat admin console.

The page is a dense operational timeline, not a marketing page:

- filter by user ID, principal user ID, action, outcome, and time range
- list activity rows with timestamp, user, platform, action, resource, outcome, and detail summary
- provide JSON/CSV export links for the current filter
- keep all strings in `webchat/locales/{zh-CN,en}/admin.json`

The page consumes existing admin endpoints. It does not add write behavior.

### 3.2 Cross-channel Identity Links

Platform native IDs remain the stored audit subject. Cross-channel lookup is an explicit admin-managed mapping, not automatic email/name matching.

Add `audit_identity_links`:

- `principal_user_id`: canonical local user ID
- `provider`: `webchat`, `feishu`, `slack`, `api`, `cron`, or another stable provider key
- `subject`: platform native user ID
- `subject_type`: `registered`, `platform`, `system`, or `anonymous`
- optional display fields for admin readability

`GET /admin/activity?principal_user_id=<id>` expands to:

- the principal ID itself
- all linked `subject` values for that principal

This preserves hash-chain rows unchanged while allowing an investigator to search one person across channels.

### 3.3 Identity Link Admin API

Add small admin endpoints:

- `GET /admin/audit/identity-links?principal_user_id=...`
- `POST /admin/audit/identity-links`
- `DELETE /admin/audit/identity-links/{id}`

Writes go through existing admin middleware and therefore produce `admin.*` audit rows.

### 3.4 SIEM And Cold Archive

No new alert rules are added. SIEM export is satisfied by the existing `WebhookSink`, which posts signed audit events to an external collector. Cold archive is satisfied by JSON/CSV activity export for selected filters; this PR documents that route and exposes it in the UI.

### 3.5 PostgreSQL Partitioning

The current table already exists as a normal table. A safe partition migration requires a table replacement plan and staging smoke test. This PR records that decision instead of attempting an unsafe in-place conversion on unknown production data.

### 3.6 Legacy `admin_audit` Slog

The old slog path remains as a compatibility stream until dashboards migrate to `user_activity`. This PR updates the issue spec to stop describing slog-only audit as a future task. Full removal is a breaking observability change and should be versioned separately.

## 4. Acceptance Criteria

- ✅ Admin can open `/admin/activity` and inspect audit rows.
- ✅ Filters include user ID, principal user ID, action, outcome, and time range.
- ✅ Export buttons preserve the active filters.
- ✅ Admin can create, list, and delete identity links via API.
- ✅ `principal_user_id` queries return activity for the principal plus linked subjects.
- ✅ SQLite and PostgreSQL migrations both create/drop `audit_identity_links`.
- ✅ Existing activity endpoints keep backward-compatible responses.
- ✅ Tests cover identity link persistence and principal expansion.

## 5. Implementation Status

| Area | Status | Landing |
|---|---|---|
| Spec update | ✅ | `docs/specs/User-Behavior-Audit-Final-Followups-Spec.md`, `docs/specs/User-Behavior-Audit-Spec.md` |
| Identity link schema | ✅ | `internal/session/sql/migrations/024_audit_identity_links.sql`, `internal/session/sql/migrations-postgres/024_audit_identity_links.pg.sql` |
| Store/API support | ✅ | `internal/audit/store.go`, `internal/admin/activity_handlers.go`, `internal/admin/audit_service.go` |
| Principal expansion | ✅ | `/admin/activity?principal_user_id=...` |
| Admin write audit labels | ✅ | `audit.identity_link.create`, `audit.identity_link.delete` in `internal/admin/audit.go` |
| Admin activity UI | ✅ | `webchat/app/admin/activity/page.tsx` |
| i18n | ✅ | `webchat/locales/en/admin.json`, `webchat/locales/zh-CN/admin.json` |
| API docs | ✅ | `docs/reference/admin-api.md` |

## 6. Validation

- ✅ `go test -count=1 ./...`
- ✅ `go test -race -count=1 ./internal/audit ./internal/admin ./internal/security ./cmd/hotplex`
- ✅ `cd webchat && npx tsc --noEmit`
- ✅ `cd webchat && npx eslint app/admin/activity/page.tsx lib/api/admin-activity.ts lib/types/admin.ts components/admin/admin-nav.tsx`
- ✅ `make check`
- ✅ pre-push hook: formatting, vet, `go mod verify`, lint, build, tests
- ✅ GitHub PR checks on #854: Branch Naming, Issue Link, Large File Guard, Build, Test

## 7. Deferred Boundaries

- Full alert rule engine remains out of #833 and should be specified separately.
- PostgreSQL monthly partition conversion remains deferred because safe conversion of an existing `user_activity` table requires a staged table replacement plan and staging smoke test.
- Full removal of legacy `admin_audit` slog remains deferred as a versioned observability compatibility change.

## 8. Non-goals

- Automatic identity matching.
- Full alert rule engine.
- Backfilling historical orphaned events.
- Unsafe conversion of the existing PostgreSQL `user_activity` table into a partitioned table.
