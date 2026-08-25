# Stop Worker Run Quiescence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `control.stop` acknowledge success only after the stopped session's Worker run, frozen connection, and forwarder are permanently isolated and quiescent.

**Architecture:** Serialize primary input dispatch and stop per session with a fixed striped gate. Bind every local Worker run to a private lifecycle object that owns an event read/write barrier, an irreversible stopping flag, the frozen `SessionConn`, and a forwarder completion channel; the stop path commits the barrier only after provider cancellation succeeds, disposes the wrapper/process, waits for forwarder cleanup, and fails closed on timeout.

**Tech Stack:** Go 1.26, `sync`/typed atomics, HotPlex Gateway/Worker interfaces, SQLite-backed contract harness, `testify/require`, Go race detector.

**Spec:** `docs/specs/Stop-Worker-Run-Quiescence-Spec.md`

## Global Constraints

- Preserve the AEP `control.stop` and `done(reason="stopped_by_user")` wire shapes; do not change SDKs or WebChat protocol code.
- Do not change the public `worker.Worker` or `worker.SessionConn` interfaces.
- Do not terminate the shared Codex app-server or OpenCode server; only release the stopped session wrapper/subscription/connection.
- Keep `terminate`, `reset`, `delete`, GC, crash recovery, execution owner lease, and input idempotency behavior unchanged.
- Persist only execution fingerprints/status metadata; never persist prompts, metadata values, credentials, or raw Worker errors.
- Use explicit mutex fields, preserve the existing `m.mu -> ms.mu` lock order, and never hold the run event write lock across teardown, session-manager calls, or forwarder waiting.
- Tests use `testify/require`, deterministic channel/`require.Eventually` synchronization, `t.Parallel()` where state is isolated, and no `time.Sleep` for asynchronous coordination.
- Targeted Gateway/Worker modules must pass with `-count=1 -race -shuffle=on` and remain within the repository's five-second per-module budget.

---

### Task 1: Extend the contract harness and reproduce the quiescence failures

**Files:**
- Modify: `internal/gateway/contracttest/harness.go`
- Modify: `internal/gateway/contracttest/worker_probe.go`
- Modify: `internal/gateway/stop_contract_test.go`

**Interfaces:**
- Consumes: the existing `contracttest.NewHarness`, `WorkerProbe`, observer connection, execution store, and real per-profile parser/mapper fixtures.
- Produces: controllable provider-stop/terminate/kill/wait phases, late-event injection, probe replacement inspection, and stored-event queries used by C06-C10.

- [ ] **Step 1: Add observable lifecycle controls to `WorkerProbe`**

Add atomics and one-shot channel gates without changing the production Worker interface:

```go
type WorkerProbe struct {
	*noop.Worker
	// existing fields...
	terminateCalls atomic.Int32
	killCalls      atomic.Int32
	failTerminate atomic.Bool
	failKill      atomic.Bool
	stopEntered   chan struct{}
	allowStop     chan struct{}
	waitEntered   chan struct{}
	allowWait     chan struct{}
	blockStop     atomic.Bool
	blockWait     atomic.Bool
	lateOnDispose atomic.Bool
}
```

Implement `Terminate`, `Kill`, and `Wait` so the normal path closes the probe connection, injected terminate failure leaves Kill as the fallback, and blocked Wait exits only when the test releases its channel. Add getters and arm/release methods whose names state the phase they control.

- [ ] **Step 2: Inject representative late events during disposal**

When `lateOnDispose` is armed, enqueue envelopes carrying unique `late_run_*` markers before the frozen connection closes:

```go
[]events.Kind{
	events.MessageDelta,
	events.Message,
	events.Reasoning,
	events.ToolCall,
	events.PermissionRequest,
	events.State,
	events.Done,
	events.Error,
}
```

The probe records all attempted writes in its private log even after connection closure, while only open-connection writes reach `Recv`; this distinguishes Worker emission from Gateway forwarding.

- [ ] **Step 3: Enable durable event assertions in the harness**

Add a harness option that constructs an `eventstore.SQLiteStore` and `eventstore.Collector` over the harness's existing migrated SQLite database only when a test requests durable-event assertions. Inject the collector through `gateway.BridgeDeps`, close it before the shared DB, and expose:

```go
func (h *Harness) StoredEvents(t testing.TB) []eventstore.StoredEvent
func (h *Harness) Probes() []*WorkerProbe
```

`StoredEvents` must flush the current session and query with `eventstore.CursorLatest` using a bounded limit.
Existing matrix tests keep the collector disabled so their seq behavior and runtime remain unchanged; C06 opts in explicitly.

- [ ] **Step 4: Write C06-C10 failing contract tests**

Add deterministic tests with the following observable assertions:

```go
// C06: late_run_* markers exist in WorkerProbe.Events(), but in neither
// Harness observer output nor StoredEvents(); exactly one stopped done exists.

// C07: while Wait is blocked, no stopped done is visible; after ReleaseWait,
// exactly one stopped done appears.

// C08: while StopCurrentTurn is blocked, the concurrent next input has not
// reached the old probe; after release, Harness.Worker() is a new pointer,
// CurrentWorkerBinding returns a new run ID, and the next input reaches it once.

// C09: injected Terminate failure increments TerminateCalls and KillCalls once,
// closes the frozen connection, and still yields one stopped done after quiet.

// C10: a short injected stop budget plus blocked Wait returns an error,
// emits no stopped done, removes the old binding, and leaves the session Idle.
```

- [ ] **Step 5: Run the RED tests**

Run:

```bash
go test ./internal/gateway -run 'WorkerRunQuiescenceContract/(C0[6-9]|C10)' -count=1 -race -shuffle=on
```

Expected: C06 exposes forwarded late markers, C07 sees the synthetic done before Wait release, C08 can dispatch to the old run, C09 observes no teardown call, and C10 lacks bounded quiescence failure behavior.

---

### Task 2: Add the session dispatch gate and per-run event barrier

**Files:**
- Modify: `internal/gateway/handler.go`
- Modify: `internal/gateway/bridge.go`
- Modify: `internal/gateway/bridge_worker.go`
- Modify: `internal/gateway/bridge_forward.go`
- Modify: `internal/gateway/bridge_retry.go`
- Test: `internal/gateway/handler_test.go`
- Test: `internal/gateway/bridge_test.go`

**Interfaces:**
- Consumes: `workerRunBinding`, frozen `SessionConn`, `forwardEvents`, `processForwardedEvent`, retry cancellation, and CAS cleanup.
- Produces: `sessionDispatchGate`, `workerRunLifecycle`, full private binding lookup, and nil-safe event-side-effect admission.

- [ ] **Step 1: Test fixed-stripe dispatch serialization**

Add tests that hold one session lock and prove a second acquisition for the same session blocks until release, while a deliberately selected non-colliding session can proceed. Use channels for entered/released signals and never sleep.

- [ ] **Step 2: Implement the zero-value session gate**

Add a 64-stripe gate with a local allocation-free FNV-1a byte loop:

```go
type sessionDispatchGate struct {
	stripes [64]sync.Mutex
}

func (g *sessionDispatchGate) Lock(sessionID string) func()
```

Store it directly on `Handler`. In `deliverToWorkerWithBusyHandling`, acquire it before the first session read and keep it through acceptance, binding selection, `MarkRunning`, `stopFence.BeginTurn`, and `Worker.Input`/native invocation return. If acceptance reports `execution.ErrSessionBusy`, release the dispatch gate before calling `handleSupplementOnBusy`; that function can re-enter normal delivery and would otherwise self-deadlock.

- [ ] **Step 3: Add the private run lifecycle**

Extend the private binding only:

```go
type workerRunLifecycle struct {
	eventMu  sync.RWMutex
	stopping atomic.Bool
	done     chan struct{}
	conn     worker.SessionConn
}

type workerRunBinding struct {
	worker    worker.Worker
	id        string
	lifecycle *workerRunLifecycle
}
```

Create a fresh lifecycle after Worker start/reset has established its connection. Pass the same pointer to the binding and forwarder; never re-read `Worker.Conn()` in the forwarder or stop path.

- [ ] **Step 4: Close lifecycle completion after all cleanup**

For both normal launch and connection-replacing reset, register goroutine defers in LIFO order so the effective order is:

```text
launchForwarderLocked returns
-> clearWorkerRun(expected worker + run)
-> close(lifecycle.done)
-> fwdWg.Done()
```

This makes `done` proof that `handleWorkerExit`, CAS detach/Idle transition, and binding cleanup have completed.

- [ ] **Step 5: Guard every old-run side-effect outlet**

At the beginning of `processForwardedEvent`, acquire the lifecycle read lock and reject `stopping` before LastIO, seq, runtime, retry, Hub, or event-store work. Use the same read-side admission around:

- panic synthetic error;
- turn-timeout synthetic error/capture/terminate;
- post-Recv pending-error flush;
- each auto-retry notification and actual replay input (not the backoff wait);
- the post-`Wait` crash fallback, synthetic crash event, detach, and Idle cleanup section.

Direct unit tests that construct `forwardContext` without a lifecycle remain supported by a nil-safe admission helper.

- [ ] **Step 6: Run focused barrier and existing forwarder tests**

Run:

```bash
go test ./internal/gateway -run 'DispatchGate|WorkerRun|ForwardEvents|ProcessForwardedEvent|C06' -count=1 -race -shuffle=on
```

Expected: the new gate/barrier tests and C06 pass; existing forwarding, seq, retry, reset-generation, and stale-forwarder tests remain green.

---

### Task 3: Orchestrate provider cancellation, disposal, quiescence, and stop acknowledgement

**Files:**
- Modify: `internal/gateway/deps.go`
- Modify: `internal/gateway/bridge.go`
- Modify: `internal/gateway/bridge_worker.go`
- Modify: `internal/gateway/commands.go`
- Modify: `internal/gateway/handler.go`
- Test: `internal/gateway/ctrl_test.go`
- Test: `internal/gateway/stop_contract_test.go`

**Interfaces:**
- Consumes: full `workerRunBinding`, `StopCurrentTurn`, `Terminate`, `Kill`, frozen `SessionConn.Close`, `DetachWorkerIf`, `clearWorkerRun`, and `finishRuntimeOnStop`.
- Produces: `Bridge.StopAndDisposeCurrentRun(ctx, sessionID, expectedRunID) error` plus internal error categories that tell the Handler whether provider cancellation took effect.

- [ ] **Step 1: Add injectable server-side stop budgets**

Add optional `BridgeDeps` durations and normalized Bridge fields:

```go
StopTeardownTimeout  time.Duration // default 8s
StopForwarderTimeout time.Duration // default 1s after teardown budget
```

Zero values select production defaults. Contract harness options inject short non-zero values only for C10.

- [ ] **Step 2: Implement stop/dispose with an irreversible barrier**

Implement the sequence below, using `context.WithTimeout(context.WithoutCancel(ctx), stopTeardownTimeout)` so client cancellation cannot abandon cleanup:

```go
binding := currentWorkerRunBinding(sessionID, expectedRunID)
binding.lifecycle.eventMu.Lock()
revalidate the exact worker/run/lifecycle binding
err := binding.worker.StopCurrentTurn(stopCtx)
if err != nil {
	binding.lifecycle.eventMu.Unlock()
	return errWorkerStopNotApplied
}
binding.lifecycle.stopping.Store(true)
binding.lifecycle.eventMu.Unlock()

terminateErr := binding.worker.Terminate(stopCtx)
if terminateErr != nil {
	killErr := binding.worker.Kill()
	// Kill success recovers Terminate failure; Kill failure is teardown failure.
}
_ = binding.lifecycle.conn.Close()
wait for lifecycle.done until the teardown deadline, then for the extra
forwarder-settle timeout
```

Never hold `eventMu` while calling `Terminate`, `Kill`, `Close`, session manager operations, or waiting. If the forwarder does not complete, CAS-detach only the expected Worker, clear only the expected run binding, and transition to Idle only when this path actually detached that Worker.

- [ ] **Step 3: Distinguish pre-commit and post-commit failures**

Use package-private sentinel categories with lowercase messages:

```go
errWorkerRunChanged
errWorkerStopNotApplied
errWorkerRunTeardown
errWorkerRunQuiescence
```

Only run-changed/provider-stop failures allow `stopFence.Rollback`. Teardown/quiescence failures keep `stopping=true`, finish the execution runtime as failed, return a generic client error, and never send `stopped_by_user`.

- [ ] **Step 4: Reorder `control.stop` under the dispatch gate**

After ownership validation, hold the session dispatch gate across binding/execution resolution, stop-fence claim, retry cancel, stop/dispose, runtime convergence, and terminal send. Resolve duplicate stop after the first stop cleared the binding by using `LatestBySession.WorkerRunID` and the existing composite fence; a duplicate returns silently, while a genuinely unclaimed no-binding stop returns the current no-active-worker error.

Send the synthetic done only after `StopAndDisposeCurrentRun` returns nil:

```go
h.finishRuntimeOnStop(ctx, sessionID, workerRunID, ownerID)
doneEnv := events.NewEnvelope(
	aep.NewID(), sessionID, 0,
	events.Done, events.DoneData{Reason: "stopped_by_user"},
)
return h.hub.SendToSession(ctx, doneEnv)
```

Log stable `stop_phase`/`error_kind` categories and identifiers only; do not log or send raw Worker error strings.

- [ ] **Step 5: Run C07-C10 and the complete stop contract**

Run:

```bash
go test ./internal/gateway -run 'Stop|WorkerRunQuiescenceContract' -count=1 -race -shuffle=on
```

Expected: C04-C10 pass, including duplicate-stop, next-turn replacement, failed-stop retry, late-event quarantine, done-after-quiescence, stop/input race, Kill fallback, and timeout-without-fake-done.

- [ ] **Step 6: Commit the Gateway slice**

Run `git diff --check`, inspect the staged diff for secrets and unrelated changes, then commit:

```bash
git add internal/gateway docs/superpowers/plans/2026-08-25-stop-worker-run-quiescence.md
git commit -m "fix(gateway): quiesce stopped worker runs"
```

---

### Task 4: Pin four Worker teardown/resume and singleton-isolation semantics

**Files:**
- Modify: `internal/worker/claudecode/worker_test.go`
- Modify: `internal/worker/acp/worker_stop_test.go`
- Modify: `internal/worker/codexcli/worker_test.go`
- Modify: `internal/worker/opencodeserver/worker_test.go`

**Interfaces:**
- Consumes: existing adapter `StopCurrentTurn`, `Terminate`, `Kill`, `Wait`, resume-identity, manager/singleton reference-count, unsubscribe, and connection-close behavior.
- Produces: regression proof that stop+teardown releases exactly the local run and that resume uses a new wrapper without losing provider identity.

- [ ] **Step 1: Add per-process adapter composition tests**

For Claude Code and ACP, use existing fake process/client fixtures to verify:

```text
StopCurrentTurn succeeds
-> Terminate is idempotent after stop
-> Conn closes and Wait returns
-> a separately constructed resumed Worker keeps the same provider session ID
```

The tests assert process/wrapper effects, not internal call order.

- [ ] **Step 2: Add Codex two-wrapper isolation**

Create two wrappers sharing the same test manager. Stop and terminate wrapper A, then assert A's thread is unsubscribed/connection closed and manager refcount decreases once, while wrapper B remains subscribed and usable; the manager process is not killed.

- [ ] **Step 3: Add OpenCode Server two-wrapper isolation**

Create two wrappers sharing the singleton fixture. Stop and terminate wrapper A, then assert A's SSE/subscription/connection release and one reference decrement while wrapper B's subscription and shared server remain active.

- [ ] **Step 4: Run the Worker matrix**

Run:

```bash
go test ./internal/worker/claudecode ./internal/worker/acp ./internal/worker/codexcli ./internal/worker/opencodeserver -run 'Stop|Terminate|Kill|Release|Resume' -count=1 -race -shuffle=on
```

Expected: all focused Worker tests pass without affecting shared services or exceeding the module time budget.

- [ ] **Step 5: Commit Worker regression coverage**

```bash
git add internal/worker/claudecode/worker_test.go internal/worker/acp/worker_stop_test.go internal/worker/codexcli/worker_test.go internal/worker/opencodeserver/worker_test.go
git commit -m "test(worker): pin stopped-run teardown isolation"
```

---

### Task 5: Full verification, five-axis self-review, and PR delivery

**Files:**
- Review: every file changed from `origin/main...HEAD`
- Modify only if verification or review finds a scoped defect.

**Interfaces:**
- Consumes: approved spec AC1-AC10, repository quality gates, WebChat stop suite, git history, and GitHub issue #971.
- Produces: fresh verification evidence, review findings resolved, atomic commits, pushed branch, and a PR whose description maps behavior/tests to the issue.

- [ ] **Step 1: Verify graph coverage and blast radius**

Check index coverage for every changed/evidence file, read any missed ranges directly, and inspect inbound impact from the final diff. Confirm no AEP, SDK, migration, config, or WebChat production file changed.

- [ ] **Step 2: Run focused and package-wide race tests**

```bash
go test ./internal/gateway -run 'Stop|WorkerRunQuiescenceContract' -count=1 -race -shuffle=on
go test ./internal/worker/claudecode ./internal/worker/acp ./internal/worker/codexcli ./internal/worker/opencodeserver -run 'Stop|Terminate|Kill|Release|Resume' -count=1 -race -shuffle=on
go test ./internal/gateway/... ./internal/worker/... -count=1 -race -shuffle=on
```

- [ ] **Step 3: Run frontend and repository gates**

```bash
cd webchat && pnpm test
make docs-lint
make quality
make build
```

Record exit codes and failure counts. Do not claim completion from summaries alone; recover bounded diagnostics for any filtered/truncated failure.

- [ ] **Step 4: Perform the five-axis review**

Review tests first, then implementation for correctness, readability, architecture, security/privacy, and performance. Explicitly inspect:

- lock order and absence of lock-held teardown/waits;
- all event/synthetic/retry outlets after `stopping` commits;
- duplicate stop and post-timeout fence semantics;
- CAS protection against replacement Worker cleanup;
- goroutine/channel exit ownership;
- raw-error/prompt/metadata leakage;
- Codex/OCS shared-service isolation;
- dead code and unnecessary interface/dependency growth.

Resolve every Critical/Required finding and rerun the affected verification command after each edit.

- [ ] **Step 5: Reconcile requirements and commits**

Map AC1-AC10 to tests or direct code evidence, run `git diff --check`, inspect `git status`, `git diff --stat`, staged patches, and commit history, then create a final scoped commit only if review fixes remain.

- [ ] **Step 6: Push and create the PR**

Push the non-main branch after the pre-push quality gate succeeds. Create a PR with:

```text
Title: fix(gateway): quiesce stopped worker runs

Summary:
- serialize stop with same-session input dispatch
- quarantine every event/synthetic outlet after provider cancellation
- dispose and await the exact frozen Worker run before stopped_by_user
- preserve provider history and shared Codex/OCS services

Testing:
- focused Gateway stop contract under race/shuffle
- four Worker lifecycle packages under race/shuffle
- Gateway/Worker package-wide race suite
- WebChat unit suite
- docs-lint, quality, build

Closes #971
```

Return the PR URL, branch/commit summary, fresh verification evidence, and any residual risk.
