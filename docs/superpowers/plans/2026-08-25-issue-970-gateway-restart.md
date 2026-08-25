# Feishu Gateway Restart Control Plane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement issue #970 so an explicitly authorized Feishu operator can safely restart Gateway through a Gateway-owned, globally serialized control plane with durable lifecycle receipts.

**Architecture:** Keep command parsing and the small request contract in `internal/messaging`; keep restart coordination, lease/receipt persistence, helper orchestration, and lifecycle integration in `cmd/hotplex`. The Feishu adapter performs the existing ordinary Gate first, then invokes an injected typed handler; it never imports `cmd/hotplex` or starts a Worker for `/gateway` input. Service managers retain their platform-specific ownership and expose one restart transaction per platform.

**Tech Stack:** Go, Cobra, `crypto/rand`, `encoding/json`, `os.Rename`, `slog`, `testify/require`, build-tagged Linux/Darwin/Windows implementations, `go test -race -shuffle=on`.

**Spec:** `docs/specs/Gateway-Self-Restart-Spec.md`

## Global Constraints

- `/gateway` is a reserved namespace; no reserved input may fall through to a Worker.
- Feishu restart authorization is separate from ordinary `allow_from`; empty platform configuration is deny-by-default.
- Restart requests use a Gateway-owned two-phase `Prepare` → reply → `Commit` flow; failed replies/commits use `Abort`.
- The lease is `$HOTPLEX_HOME/.pids/gateway.restart`, is `0600`, and is acquired with `O_CREATE|O_EXCL`.
- Request IDs use `crypto/rand`; `math/rand` is forbidden for security-sensitive identifiers.
- The receipt is `$HOTPLEX_HOME/.pids/gateway.restart.receipt.json`, is atomically replaced, contains no message body or credentials, and remains on failed delivery.
- A new Gateway sends `started` only after adapters, HTTP, and ready state are established.
- `pkg/events`, client SDKs, and AEP documentation must not change.
- Go edits use `apply_patch`; formatting uses `gofmt`; project tests use `testify/require`, table-driven cases, and `require.Eventually`/channels instead of `time.Sleep` in tests.

---

### Task 1: Reserved command parser and Feishu restart authorization

**Files:**
- Create: `internal/messaging/operator_command.go`
- Test: `internal/messaging/operator_command_test.go`
- Modify: `internal/config/config_types.go`
- Modify: `internal/config/config_env.go`
- Modify: `internal/config/watcher.go`
- Test: `internal/config/config_test.go`, `internal/config/watcher_test.go`
- Modify: `cmd/hotplex/messaging_init.go`
- Test: `cmd/hotplex/messaging_init_test.go` (create this focused wiring test file if no existing file covers `fillFeishuExtras`)

**Interfaces:**
- Produce `messaging.ParseGatewayCommand(string) (GatewayCommand, bool)` and `messaging.GatewayCommandHandler`.
- Produce `FeishuConfig.GatewayRestartAllowFrom` and `FeishuBotConfig.GatewayRestartAllowFrom` with nil-inherit/non-nil override semantics.
- Produce a Feishu adapter callback that checks the current `ConfigStore` snapshot for every request, so a hot-reloaded allowlist applies immediately.

- [ ] **Step 1: Write parser and config RED tests.** Cover exact `/gateway restart`, case-insensitive command names, trimming, extra arguments, unknown subcommands, natural language, fenced Markdown, and every `/gateway` variant being reserved. Cover platform default deny, env comma splitting, bot nil inheritance, and explicit empty bot list.

```go
func TestParseGatewayCommand_ReservedNamespaceNeverFallsThrough(t *testing.T) {
	for _, input := range []string{"/gateway restart now", "/gateway foo", "/gateway", "  /GATEWAY RESTART  "} {
		got, reserved := ParseGatewayCommand(input)
		require.True(t, reserved)
		if strings.EqualFold(strings.TrimSpace(input), "/gateway restart") {
			require.Equal(t, GatewayCommandRestart, got.Kind)
		} else {
			require.Equal(t, GatewayCommandHelp, got.Kind)
		}
	}
}
```

- [ ] **Step 2: Run the focused tests and verify RED.**

Run: `go test ./internal/messaging ./internal/config -run 'TestParseGatewayCommand|Test.*GatewayRestart|Test.*HotReload' -count=1`

Expected: FAIL because the parser, fields, and resolution behavior do not yet exist.

- [ ] **Step 3: Implement the parser and config fields.** Add the two Feishu fields, the platform env mapping `HOTPLEX_MESSAGING_FEISHU_GATEWAY_RESTART_ALLOW_FROM`, and a watcher field representation that notices both platform and per-bot allowlist changes without marking unrelated bot fields hot.

- [ ] **Step 4: Implement the adapter-facing authorization contract.** Keep the ordinary Gate before the reserved-command branch. The Feishu branch must return after replying or invoking the handler; it must never call `handleTextMessage`, `handleTextControlCommand`, or `handleTextWorkerCommand`.

- [ ] **Step 5: Run the focused tests and verify GREEN.**

Run: `go test ./internal/messaging ./internal/config ./cmd/hotplex -run 'TestParseGatewayCommand|Test.*GatewayRestart|Test.*HotReload' -count=1`

Expected: PASS with no skipped cases.

- [ ] **Step 6: Commit the atomic parser/config slice.**

```bash
git add internal/messaging/operator_command.go internal/messaging/operator_command_test.go internal/config/config_types.go internal/config/config_env.go internal/config/config_test.go internal/config/watcher.go internal/config/watcher_test.go cmd/hotplex/messaging_init.go
git commit -m "feat: reserve Feishu gateway restart commands"
```

### Task 2: Atomic lease and receipt stores

**Files:**
- Create: `cmd/hotplex/gateway_restart_receipt.go`
- Modify: `cmd/hotplex/pid.go`
- Test: `cmd/hotplex/pid_test.go`, `cmd/hotplex/gateway_restart_receipt_test.go`

**Interfaces:**
- Produce a v2 lease model with `schema_version`, `request_id`, `phase`, `owner_pid`, `helper_pid`, and `created_at`.
- Produce ticket-scoped `acquire`, `read`, `update`, `release`, and stale-reclaim operations.
- Produce an atomic receipt store with `Write`, `Read`, `Quarantine`, and `Complete` operations.

- [ ] **Step 1: Write the failing lease race and receipt tests.** Start at least 50 concurrent acquisition attempts against one temporary `$HOTPLEX_HOME`; assert exactly one success, `0600` permissions, mismatched request IDs cannot update/release, and a receipt replacement never exposes partial JSON.

```go
func TestRestartLease_ConcurrentAcquireOnlyOneWinner(t *testing.T) {
	const attempts = 50
	var winners atomic.Int32
	var wg sync.WaitGroup
	for range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := acquireRestartLease(restartLeasePath(), restartLeaseOwnerPID()); err == nil { winners.Add(1) }
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), winners.Load())
}
```

- [ ] **Step 2: Run the RED tests with the repository race settings.**

Run: `go test -race -shuffle=on ./cmd/hotplex -run 'TestRestartLease|TestRestartReceipt' -count=1`

Expected: FAIL before the v2 lease and receipt implementations exist.

- [ ] **Step 3: Implement `O_CREATE|O_EXCL` lease acquisition and ticket fencing.** Preserve safe recognition of schema v1 markers; never treat malformed or unknown state as an invitation to start a second helper. Reclaim only the specified dead-owner/dead-helper states and retry acquisition atomically.

- [ ] **Step 4: Implement the receipt store.** Create parent directories with bounded permissions, write a temporary file in the same directory with mode `0600`, sync/close, rename atomically, validate size/schema/fields on read, and quarantine corrupt receipts without logging their contents.

- [ ] **Step 5: Run the focused race tests and verify GREEN.**

Run: `go test -race -shuffle=on ./cmd/hotplex -run 'TestRestartLease|TestRestartReceipt' -count=1`

Expected: PASS; the test must observe exactly one lease winner.

- [ ] **Step 6: Commit the persistence slice.**

```bash
git add cmd/hotplex/pid.go cmd/hotplex/pid_test.go cmd/hotplex/gateway_restart_receipt.go cmd/hotplex/gateway_restart_receipt_test.go
git commit -m "feat: add fenced gateway restart lease and receipts"
```

### Task 3: Coordinator, helper, CLI, and Admin integration

**Files:**
- Create: `cmd/hotplex/gateway_restart_coordinator.go`
- Create: `cmd/hotplex/gateway_restart_coordinator_test.go`
- Modify: `cmd/hotplex/gateway_restart_helper.go`
- Modify: `cmd/hotplex/gateway_cmd.go`
- Modify: `cmd/hotplex/gateway_run.go`
- Modify: `cmd/hotplex/routes.go`
- Modify: `internal/admin/admin.go`, `internal/admin/handlers.go`
- Test: relevant Admin and CLI tests

**Interfaces:**
- Produce `restartCoordinator.Prepare`, `Commit`, `Abort`, and `CompleteReady`.
- The coordinator owns request IDs, instance discovery, lease/receipt state, helper arguments, and structured audit fields.
- The existing Admin `Restart` dependency remains callable; the route supplies a coordinator-backed closure that prepares before responding and commits only after the response handoff.

- [ ] **Step 1: Write coordinator RED tests.** Cover Prepare conflict, ticket mismatch, prepare without a running instance, commit fork failure cleanup, abort cleanup, and 50 concurrent `Prepare` calls yielding one success.

- [ ] **Step 2: Run the RED tests.**

Run: `go test -race -shuffle=on ./cmd/hotplex ./internal/admin -run 'TestRestartCoordinator|Test.*Restart' -count=1`

Expected: FAIL because the coordinator is not wired.

- [ ] **Step 3: Implement `Prepare`, `Commit`, and `Abort`.** Use the current PID/service discovery, capture old version/PID, write the lease before any helper is forked, optionally write the Feishu receipt, then pass only a validated ticket/request ID to the helper. Commit updates `helper_pid` and `helper_started`; fork failure removes only state created by that ticket.

- [ ] **Step 4: Replace direct CLI/Admin restart paths.** Route both CLI modes and `POST /admin/restart` through the coordinator while keeping existing command/API names and response shape. Map an active lease to a conflict response without exposing secrets or internal error details.

- [ ] **Step 5: Make the helper lease-aware.** Remove the old defer-based marker deletion, update phase after startup, retain the lease on service/start failure, and keep the detached helper outside the Worker lifecycle. Preserve the existing bounded PID shutdown behavior.

- [ ] **Step 6: Run focused GREEN tests.**

Run: `go test -race -shuffle=on ./cmd/hotplex ./internal/admin -run 'TestRestartCoordinator|Test.*Restart' -count=1`

Expected: PASS with no direct check-then-write restart path remaining.

- [ ] **Step 7: Commit the coordinator slice.**

```bash
git add cmd/hotplex/gateway_restart_coordinator.go cmd/hotplex/gateway_restart_coordinator_test.go cmd/hotplex/gateway_restart_helper.go cmd/hotplex/gateway_cmd.go cmd/hotplex/gateway_run.go cmd/hotplex/routes.go internal/admin/admin.go internal/admin/handlers.go
git commit -m "feat: unify gateway restart entry points"
```

### Task 4: Feishu control path and lifecycle notifications

**Files:**
- Modify: `internal/messaging/config.go`, `internal/messaging/platform_interfaces.go`
- Modify: `internal/messaging/feishu/adapter.go`, `internal/messaging/feishu/handler.go`
- Test: `internal/messaging/feishu/*_test.go`
- Modify: `cmd/hotplex/messaging_init.go`
- Modify: `cmd/hotplex/lifecycle_broadcast.go`, `cmd/hotplex/lifecycle_broadcast_snapshot.go`
- Test: `cmd/hotplex/lifecycle_broadcast_test.go`

**Interfaces:**
- Produce a minimal platform-neutral request carrying sender OpenID, bot identity, chat/thread/message keys, and a reply function.
- Produce lifecycle formatting from `BuildInfo`, restart context, and PID; target merging remains keyed by platform + bot identity + platform key.

- [ ] **Step 1: Write Feishu RED tests.** Cover ordinary Gate rejection, dedicated allowlist rejection, valid command with nil Bridge, unknown reserved command, no Worker fallback, no historical Session, and no duplicate target when receipt and Session refer to the same chat.

- [ ] **Step 2: Write lifecycle RED tests.** Assert old/new versions and PIDs appear in the correct phase, normal shutdown remains unchanged, receipt send failure preserves receipt, corrupt receipt sends nothing, and only ready new instances emit `started`.

- [ ] **Step 3: Run focused RED tests.**

Run: `go test -race -shuffle=on ./internal/messaging/feishu ./cmd/hotplex -run 'Test.*Gateway|Test.*Lifecycle|Test.*Receipt' -count=1`

Expected: FAIL for the new command and receipt behaviors.

- [ ] **Step 4: Wire Feishu Prepare → accepted reply → Commit.** The adapter invokes the injected handler only after ordinary Gate and authorization; the coordinator handles conflict/denied/schedule failure audit and direct reply. The adapter must not build an envelope or call Bridge for the reserved namespace.

- [ ] **Step 5: Merge receipt targets into both lifecycle phases.** Keep Session snapshots for existing behavior, synthesize a minimal Feishu target from the receipt, deduplicate within each phase, and complete the lease only after the new instance has reached ready and attempted the started notification.

- [ ] **Step 6: Add build info message formatting and ready ordering.** Start adapters and gateway/admin listeners, establish ready, send started, then complete/delete successful receipt state. Keep failed targets for bounded retry and structured logging.

- [ ] **Step 7: Run focused GREEN tests and commit.**

```bash
go test -race -shuffle=on ./internal/messaging/feishu ./cmd/hotplex -run 'Test.*Gateway|Test.*Lifecycle|Test.*Receipt' -count=1
git add internal/messaging/config.go internal/messaging/platform_interfaces.go internal/messaging/feishu cmd/hotplex/messaging_init.go cmd/hotplex/lifecycle_broadcast.go cmd/hotplex/lifecycle_broadcast_snapshot.go
git commit -m "feat: deliver Feishu restart lifecycle receipts"
```

### Task 5: Platform supervisor semantics

**Files:**
- Modify: `internal/service/manager_darwin.go`
- Modify: `internal/service/manager_windows.go`
- Modify: `cmd/hotplex/gateway_restart_helper_windows.go`
- Test: `internal/service/manager_darwin_test.go`, build-tagged Windows tests as needed

- [ ] **Step 1: Add failing Darwin and Windows behavior tests.** Darwin must assert `launchctl kickstart -k <domain>/<label>` without stop/unload; Windows helper logic must wait for `Stopped` before `Start`.

- [ ] **Step 2: Implement Darwin supervisor-owned restart.** Use the correct `system/` or `gui/<uid>/` domain and preserve existing install/uninstall behavior.

- [ ] **Step 3: Implement bounded Windows SCM stop polling.** Query state with a deadline, accept already-stopped state, return a bounded error on timeout, and only then call `Start`.

- [ ] **Step 4: Add Windows breakaway creation flag and cross-compile checks.** Do not use Worker Job Object inheritance as the restart survival mechanism.

- [ ] **Step 5: Run platform-focused tests/builds and commit.**

```bash
GOOS=darwin GOARCH=arm64 go test -c -o "$TMPDIR/hotplex-service-darwin.test" ./internal/service
GOOS=windows GOARCH=amd64 go test -c -o "$TMPDIR/hotplex-service-windows.test" ./internal/service
GOOS=windows GOARCH=amd64 go test -c -o "$TMPDIR/hotplex-cmd-windows.test" ./cmd/hotplex
git add internal/service/manager_darwin.go internal/service/manager_darwin_test.go internal/service/manager_windows.go cmd/hotplex/gateway_restart_helper_windows.go
git commit -m "fix: make service restart supervisor-owned"
```

### Task 6: Configuration reference and examples

**Files:**
- Modify: `configs/config.yaml`
- Modify: `configs/env.example`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/specs/Gateway-Self-Restart-Spec.md`

- [ ] **Step 1: Add deny-by-default examples and an explicit warning that OpenIDs are host-restart identities.** Document platform/Bot inheritance, explicit empty-list disablement, env mapping, hot reload, and the exact `/gateway restart` syntax.

- [ ] **Step 2: Update the spec frontmatter from `proposed` to `implemented` only after code and acceptance evidence exist.** Keep the AEP non-change statement.

- [ ] **Step 3: Build docs and inspect the source diff.**

Run: `make docs-build`

Expected: generated output is consistent; do not manually edit `internal/docs/out`.

### Task 7: Full verification, review, and PR handoff

- [ ] Install/verify hooks with `make hooks` and inspect `git diff --check`.
- [ ] Run focused package tests after each changed slice, then `make test`.
- [ ] Run `make lint` and `make build`.
- [ ] Run Linux, Darwin, and Windows compile checks; do not claim runtime support for a platform without a passing platform test.
- [ ] Use `detect_changes` and `check_index_coverage` for the final diff, then inspect every changed file with the five-axis review checklist: correctness, readability, architecture, security, performance.
- [ ] Check staged diff for secrets and unrelated files; exclude the unrelated issue #971 draft `docs/specs/Stop-Worker-Run-Quiescence-Spec.md`.
- [ ] Push the feature branch and create a PR linked to issue #970 with a concise summary, test matrix, known runtime-only acceptance items, and rollback instructions.
