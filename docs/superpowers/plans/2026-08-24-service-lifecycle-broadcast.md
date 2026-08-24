# Service Lifecycle Broadcast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Broadcast a best-effort stopping notice to every connected external messaging conversation and a paired started notice to the same recipients after the next Gateway startup.

**Architecture:** Add a platform-neutral proactive-send capability to the three messaging adapters. A Gateway lifecycle broadcaster snapshots deduplicated active session IDs before shutdown, sends the stopping notice, then claims the file after startup, restores routing from the session store, and sends the started notice through the owning Bot.

**Tech Stack:** Go 1.24, `log/slog`, Slack Go SDK, Feishu/Lark SDK, Pulsar, `testify/require`, race-enabled Go tests.

**Spec:** `docs/superpowers/specs/2026-08-24-service-lifecycle-broadcast-design.md`

## Global Constraints

- Messages are exactly `⚠️ HotPlex 服务即将停止。` and `✅ HotPlex 服务已启动。`.
- Scope is connected Slack, Feishu, and Yuanxin sessions; WebChat and disconnected history are excluded.
- Persist only version, UTC timestamp, and opaque session IDs; never persist routing metadata or credentials.
- Snapshot input is limited to 64 KiB, expires after 24 hours, and is capped by normalized `pool.max_size`.
- Each phase has a five-second total timeout and at most eight concurrent sends.
- Delivery is best-effort and cannot change lifecycle outcomes.
- Multi-Bot routing resolves Bot name, then Bot ID, then an unambiguous sole-Bot fallback.
- No database migration, AEP change, external API change, or new configuration key.
- Preserve all unrelated dirty-worktree changes and stage only feature-owned hunks.

## File Structure

- `internal/messaging/platform_interfaces.go` defines `ProactiveMessageSender`.
- Slack, Feishu, and Yuanxin adapter files implement generic proactive delivery while preserving cron delivery.
- `cmd/hotplex/lifecycle_broadcast.go` owns target discovery, deduplication, Bot routing, snapshot persistence, claim, and fan-out.
- `cmd/hotplex/lifecycle_broadcast_test.go` tests the lifecycle module through narrow fakes.
- `cmd/hotplex/gateway_run.go` invokes the two lifecycle seams.
- `docs/reference/cli.md` and `CHANGELOG.md` document the behavior.

---

### Task 1: Platform-Neutral Proactive Message Capability

**Files:**

- Modify: `internal/messaging/platform_interfaces.go`
- Modify: `internal/messaging/slack/adapter.go`
- Modify: `internal/messaging/slack/adapter_test.go`
- Modify: `internal/messaging/feishu/adapter.go`
- Modify: `internal/messaging/feishu/interaction_test.go`
- Modify: `internal/messaging/yuanxin/adapter.go`
- Modify: `internal/messaging/yuanxin/adapter_test.go`

**Interfaces:**

- Produces: `SendProactiveMessage(context.Context, string, map[string]string) error`.
- Preserves: `SendCronResult(context.Context, string, map[string]string) error` by delegation.

- [ ] **Step 1: Write failing compile-time and validation tests**

Add `var _ messaging.ProactiveMessageSender = (*Adapter)(nil)` in each adapter package test and invoke the new method with missing target fields. Slack uses the existing `stubSlackClient`; Feishu covers missing `chat_id` and nil client; Yuanxin covers missing `message_id` and nil producer. Add a cron delegation assertion for the same validation error.

```go
func TestAdapter_SendProactiveMessage_MissingTarget(t *testing.T) {
	t.Parallel()
	a := &Adapter{}
	err := a.SendProactiveMessage(context.Background(), "hello", map[string]string{})
	require.Error(t, err)
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/messaging/slack ./internal/messaging/feishu ./internal/messaging/yuanxin -run 'ProactiveMessage|CronResult' -count=1
```

Expected: build failure because the interface and methods do not exist.

- [ ] **Step 3: Add the additive interface**

```go
type ProactiveMessageSender interface {
	SendProactiveMessage(ctx context.Context, text string, platformKey map[string]string) error
}
```

Keep it separate from `PlatformAdapterInterface` so third-party or mock adapters are not broken.

- [ ] **Step 4: Implement all three adapters**

Move each current `SendCronResult` body into `SendProactiveMessage`, retaining required target validation, `messaging.SanitizeText`, existing SDK calls, Yuanxin locking, and safe error wrapping. Make each cron method delegate directly to the new method.

- [ ] **Step 5: Format and verify focused tests**

```bash
gofmt -w internal/messaging/platform_interfaces.go internal/messaging/slack/adapter.go internal/messaging/slack/adapter_test.go internal/messaging/feishu/adapter.go internal/messaging/feishu/interaction_test.go internal/messaging/yuanxin/adapter.go internal/messaging/yuanxin/adapter_test.go
go test ./internal/messaging/slack ./internal/messaging/feishu ./internal/messaging/yuanxin -run 'ProactiveMessage|CronResult' -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/messaging/platform_interfaces.go internal/messaging/slack/adapter.go internal/messaging/slack/adapter_test.go internal/messaging/feishu/adapter.go internal/messaging/feishu/interaction_test.go internal/messaging/yuanxin/adapter.go internal/messaging/yuanxin/adapter_test.go
git commit -m "feat(messaging): add proactive message delivery"
```

---

### Task 2: Lifecycle Recipient Snapshot and Broadcaster

**Files:**

- Create: `cmd/hotplex/lifecycle_broadcast.go`
- Create: `cmd/hotplex/lifecycle_broadcast_test.go`

**Interfaces:**

- Consumes: `messaging.ProactiveMessageSender`, `messaging.BotEntry`, `session.SessionInfo`, `ListActive`, `Get`, and `HasActiveConn`.
- Produces: `newLifecycleBroadcaster(*GatewayDeps) *lifecycleBroadcaster`, `BroadcastStopping()`, and `BroadcastStarted()`.
- Persists: `$HOTPLEX_HOME/.pids/gateway.lifecycle-broadcast.json` plus a same-directory claimed sibling.

- [ ] **Step 1: Write failing target-discovery tests**

Create fakes for session access, connection checks, Bot lookup, and send. Add table-driven tests proving WebChat and disconnected sessions are excluded; required platform keys are enforced; user ID is excluded from deduplication; distinct threads remain distinct; and one representative valid UUID remains per visible target.

- [ ] **Step 2: Run the target tests to verify failure**

```bash
go test ./cmd/hotplex -run 'TestLifecycleTargets' -count=1
```

Expected: build failure because lifecycle types do not exist.

- [ ] **Step 3: Implement target validation and deduplication**

```go
func lifecycleTargetKey(si *session.SessionInfo) (string, bool)
func collectLifecycleTargets(sessions []*session.SessionInfo, hasActive func(string) bool) []*session.SessionInfo
```

Keys include Bot name or Bot ID plus Slack team/channel/thread, Feishu chat/thread, or Yuanxin message/reply-user/system fields. Copy each selected `SessionInfo` so concurrent session mutation cannot change a send.

- [ ] **Step 4: Write failing snapshot tests**

Cover owner-only permissions on Unix, exact JSON fields, temp-file cleanup, 64 KiB limit, version validation, 24-hour expiry, invalid UUID rejection, count rejection above `pool.max_size`, empty-save cleanup, atomic claim, and a second claim returning no work.

- [ ] **Step 5: Run snapshot tests to verify failure**

```bash
go test ./cmd/hotplex -run 'TestLifecycleSnapshot' -count=1
```

Expected: FAIL because snapshot persistence is absent.

- [ ] **Step 6: Implement snapshot persistence and claim**

```go
type lifecycleSnapshot struct {
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"created_at"`
	SessionIDs []string  `json:"session_ids"`
}
```

Write with `os.CreateTemp`, `Chmod(0o600)`, JSON encode, `Sync`, `Close`, and same-directory `os.Rename`. Read through a 64 KiB-plus-one-byte limit. Claim by renaming pending to a fixed `.claimed` sibling; always remove the claim after one startup attempt.

- [ ] **Step 7: Write failing Bot-routing tests**

Cover exact platform/name resolution, Bot ID fallback, sole-running-Bot legacy fallback, ambiguous fallback rejection, stopped Bot rejection, missing adapter, and adapter without `ProactiveMessageSender`.

- [ ] **Step 8: Implement exact Bot routing**

```go
type lifecycleBotRegistry interface {
	Get(platform messaging.PlatformType, name string) (*messaging.BotEntry, bool)
	ListByPlatform(platform messaging.PlatformType) []*messaging.BotEntry
}
```

Only `BotStatusRunning` entries qualify. Never route by platform alone when more than one running Bot exists.

- [ ] **Step 9: Write failing fan-out and lifecycle tests**

Cover snapshot-before-stopping-send ordering, startup claim and `Get` restoration, injected short timeout, maximum concurrency, per-target failure isolation, aggregate counts, at-most-once startup attempts, and absence of secrets in file/log evidence. Use channels rather than `time.Sleep`.

- [ ] **Step 10: Implement bounded fan-out and lifecycle methods**

Define `lifecycleBroadcastTimeout = 5 * time.Second` and `lifecycleBroadcastConcurrency = 8`. Use `context.WithTimeout(context.Background(), lifecycleBroadcastTimeout)`, an eight-slot semaphore, `sync.WaitGroup`, and a protected summary. `BroadcastStopping` saves IDs before sends. `BroadcastStarted` claims first, restores sessions, deduplicates again, sends once, and removes the claim in a defer. Log only sanitized target identity and aggregate snake_case fields.

- [ ] **Step 11: Format and run race-enabled tests**

```bash
gofmt -w cmd/hotplex/lifecycle_broadcast.go cmd/hotplex/lifecycle_broadcast_test.go
go test ./cmd/hotplex -run 'TestLifecycle' -count=1 -race -shuffle=on
```

Expected: PASS within the module five-second target.

- [ ] **Step 12: Commit**

```bash
git add cmd/hotplex/lifecycle_broadcast.go cmd/hotplex/lifecycle_broadcast_test.go
git commit -m "feat(gateway): add lifecycle broadcast snapshot"
```

---

### Task 3: Gateway Lifecycle Integration

**Files:**

- Modify: `cmd/hotplex/gateway_run.go`
- Modify: `cmd/hotplex/lifecycle_broadcast_test.go`

**Interfaces:**

- Consumes the broadcaster from Task 2.
- Preserves SIGHUP reload, server-error exit, cancellation, and ordered shutdown behavior.

- [ ] **Step 1: Write failing lifecycle-seam tests**

Test a small extracted exit classifier and call-order hook: `SIGINT`, `SIGTERM`, and `stopCh` select controlled broadcast; `SIGHUP` continues reload; server errors skip lifecycle broadcast; and stopping broadcast occurs before cancellation/shutdown.

- [ ] **Step 2: Run tests to verify failure**

```bash
go test ./cmd/hotplex -run 'TestGatewayLifecycle' -count=1
```

Expected: FAIL because lifecycle wiring is absent.

- [ ] **Step 3: Wire the startup seam**

Construct the broadcaster after `startMessagingAdapters`. Call `BroadcastStarted` after routes, Gateway/Admin server goroutines, and startup banner are initialized. Ignore its result for startup outcome.

- [ ] **Step 4: Wire the controlled-shutdown seam**

Track controlled loop exit for `SIGINT`, `SIGTERM`, and `stopCh`. Call `BroadcastStopping` before `cancel()` and `shutdownGateway`. Keep SIGHUP and the immediate server-error path unchanged.

- [ ] **Step 5: Format and run focused tests**

```bash
gofmt -w cmd/hotplex/gateway_run.go cmd/hotplex/lifecycle_broadcast_test.go
go test ./cmd/hotplex -run 'TestGatewayLifecycle|TestLifecycle' -count=1 -race -shuffle=on
go test ./cmd/hotplex ./internal/messaging/... -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add cmd/hotplex/gateway_run.go cmd/hotplex/lifecycle_broadcast_test.go
git commit -m "feat(gateway): broadcast service lifecycle notices"
```

---

### Task 4: Documentation and Release Note

**Files:**

- Modify: `docs/reference/cli.md`
- Modify: `CHANGELOG.md`

**Interfaces:** Documents exact scope, fixed text, best-effort semantics, controlled shutdown, and excluded crash/cold-start behavior.

- [ ] **Step 1: Update CLI lifecycle documentation**

Under `hotplex gateway restart` and `hotplex service restart`, state that controlled shutdown sends the stopping notice to connected Slack/Feishu/Yuanxin conversations and the next startup sends the paired started notice to the same deduplicated targets. State that WebChat, crashes, and cold starts are excluded.

- [ ] **Step 2: Add the release note**

Add above version `1.42.0`:

```markdown
## [Unreleased]

### Added

- **Messaging/Gateway**: Controlled Gateway stop/restart broadcasts stopping and started notices to the same deduplicated connected Slack, Feishu, and Yuanxin conversations with exact multi-Bot routing and no lifecycle blocking on delivery failure.
```

- [ ] **Step 3: Verify and commit only feature hunks**

```bash
git diff --check -- docs/reference/cli.md CHANGELOG.md
rg -n "服务即将停止|服务已启动|lifecycle|生命周期" docs/reference/cli.md CHANGELOG.md
git add -p docs/reference/cli.md
git add CHANGELOG.md
git diff --cached --check
git diff --cached
git commit -m "docs: document lifecycle broadcasts"
```

Because `docs/reference/cli.md` already contains unrelated changes, the staged diff must be inspected before commit.

---

### Task 5: Review, Full Verification, and Completion Audit

**Files:** Review every file committed by Tasks 1-4; modify only feature-scoped defects found by review.

**Interfaces:** Proves every acceptance criterion in the approved spec.

- [ ] **Step 1: Inspect the complete feature diff**

```bash
git diff b9472cc4..HEAD -- cmd/hotplex internal/messaging docs/reference/cli.md CHANGELOG.md docs/superpowers/plans/2026-08-24-service-lifecycle-broadcast.md
```

Review shutdown order, race safety, file cleanup, secret handling, multi-Bot routing, errors, and cron compatibility.

- [ ] **Step 2: Run focused race-enabled tests**

```bash
go test ./cmd/hotplex ./internal/messaging/slack ./internal/messaging/feishu ./internal/messaging/yuanxin -count=1 -race -shuffle=on
```

Expected: PASS within project module timing targets.

- [ ] **Step 3: Run project quality gates**

```bash
make test-short
make lint
```

Expected: PASS. If unrelated dirty-worktree changes fail a gate, isolate the failing package and report the evidence boundary without changing unrelated files.

- [ ] **Step 4: Audit static invariants**

```bash
rg -n "ProactiveMessageSender|BroadcastStopping|BroadcastStarted|gateway.lifecycle-broadcast" cmd/hotplex internal/messaging
rg -n "PlatformKey|secret|token" cmd/hotplex/lifecycle_broadcast.go cmd/hotplex/lifecycle_broadcast_test.go
git log --oneline b9472cc4..HEAD
git diff --name-only b9472cc4..HEAD
git status --short
```

Verify the snapshot schema contains only approved fields, all adapters implement the capability, feature commits exclude unrelated changes, and pre-existing worktree changes remain intact.

- [ ] **Step 5: Commit review fixes only when needed**

Stage only the specific feature files corrected during review, inspect the staged diff, and commit them with `git commit -m "fix(gateway): harden lifecycle broadcasts"`. Do not create an empty commit when review finds no defect.
