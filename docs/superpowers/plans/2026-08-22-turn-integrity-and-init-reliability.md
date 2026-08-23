# Turn Integrity and Initialization Reliability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix WebSocket initialization loops, missing/empty turns, stop isolation, event ordering, and prompt leakage while preserving AEP v1 compatibility.

**Architecture:** Three isolated Wave 1 branches implement connection reliability, durable turn/terminal integrity, and prompt isolation. After review and integration, Wave 2 changes the shared session event ordering path and adds capability negotiation. All behavioral fixes follow red-green TDD and use additive protocol fields.

**Tech Stack:** Go 1.24, Gorilla WebSocket, PostgreSQL EventStore interfaces, TypeScript, React, Vitest, pnpm.

**Spec:** `docs/superpowers/specs/2026-08-22-turn-integrity-and-init-reliability-design.md`

## Global Constraints

- Do not guess or reset sequence state when EventStore hydration fails.
- Existing AEP v1 fields remain parseable; new protocol fields are optional and additive.
- New clients make retry decisions from `code` and `retryable`, not lifecycle `state`.
- A Gateway-accepted user input is durable in the execution-ingress ledger before worker delivery; the conversation turn is materialized for successful or explicitly unknown delivery, not confirmed hard rejection.
- Every accepted execution emits exactly one terminal result.
- Every event carrying session `seq` uses one ordered delivery path.
- Do not log complete AgentConfig, system prompts, skill bodies, user profiles, or memories.
- Preserve unrelated user changes and do not refactor outside the named files.

---

### Task 1: Bound initialization retries in BrowserClient

**Files:**
- Modify: `webchat/lib/ai-sdk-transport/client/browser-client.ts`
- Test: `webchat/lib/ai-sdk-transport/client/browser-client.test.ts`

**Interfaces:**
- Consumes: current `InitAckData` fields `error`, `code`, and `state`.
- Produces: init errors routed through `_scheduleReconnect`; no recursive `_doConnect`; legacy deleted ACK is terminal.

- [ ] **Step 1: Add failing init-error tests**

Add Vitest cases using the existing fake WebSocket harness:

```ts
it("does not reconnect recursively for SESSION_NOT_FOUND", async () => {
  const client = createClient();
  await beginConnect(client);
  serverInitAck({ error: "missing", code: "SESSION_NOT_FOUND", state: "deleted" });
  await flushPromises();
  expect(createdSockets()).toHaveLength(1);
  expect(client.state).not.toBe("connected");
});

it("does not accept a legacy deleted init ack as success", async () => {
  const client = createClient();
  await beginConnect(client);
  serverInitAck({ state: "deleted" });
  await flushPromises();
  expect(client.state).not.toBe("connected");
});
```

- [ ] **Step 2: Run the focused test and record RED**

Run:

```bash
cd webchat
pnpm exec vitest run lib/ai-sdk-transport/client/browser-client.test.ts
```

Expected: the new connection-count or deleted-state assertion fails against the current recursive/success path.

- [ ] **Step 3: Implement bounded init handling**

Replace the `SESSION_NOT_FOUND` recursive `_doConnect` branch with terminal error handling. Reject `state === "deleted"` when no success state is present. Keep retryable initialization errors routed through the existing bounded reconnect scheduler; never create a new socket directly from the init handler.

- [ ] **Step 4: Run focused tests and record GREEN**

Run the command from Step 2. Expected: all BrowserClient tests pass.

- [ ] **Step 5: Commit the client slice**

```bash
git add webchat/lib/ai-sdk-transport/client/browser-client.ts webchat/lib/ai-sdk-transport/client/browser-client.test.ts
git commit -m "fix: bound websocket initialization retries"
```

### Task 2: Correct Gateway session initialization ordering

**Files:**
- Modify: `internal/gateway/conn.go`
- Modify: `internal/gateway/init.go`
- Modify: `internal/gateway/hub.go`
- Test: `internal/gateway/conn_test.go`
- Test: `internal/gateway/seq_test.go`

**Interfaces:**
- Consumes: session manager lifecycle lookup and Hub sequence hydrator.
- Produces: resolved durable session before hydration; structured retry metadata; no cleanup sequence after failed init.

- [ ] **Step 1: Add failing Gateway initialization tests**

Add tests that configure the production-style sequence existence callback:

```go
func TestInitCreatesMissingSessionBeforeSeqHydration(t *testing.T) {
    // Arrange a missing session and a hydrator that succeeds after creation.
    // Act through the init handler.
    // Assert one successful init_ack and that creation precedes hydration.
}

func TestInitHydrationFailureDoesNotEmitCleanupSeq(t *testing.T) {
    // Arrange an active session and a failing LatestSeq/FlushSession.
    // Assert one error init_ack, closed connection, and zero NextSeq/state broadcasts.
}

func TestBuildInitAckInternalErrorIsRetryableWithoutDeletedLifecycle(t *testing.T) {
    ack := BuildInitAckError("s", ErrInitInternal)
    require.Equal(t, "INTERNAL_ERROR", ack.Code)
    require.True(t, ack.Retryable)
    require.NotEqual(t, events.StateDeleted, ack.State)
}
```

- [ ] **Step 2: Run focused Gateway tests and record RED**

```bash
go test ./internal/gateway -run 'TestInit(CreatesMissingSessionBeforeSeqHydration|HydrationFailureDoesNotEmitCleanupSeq)|TestBuildInitAckInternalError' -count=1
```

Expected: ordering and lifecycle-state assertions fail.

- [ ] **Step 3: Implement lifecycle resolution before hydration**

Refactor init so validation/authentication are followed by explicit session classification/creation, then sequence hydration, then worker start/resume. Mark a failed init so `ReadPump` cleanup cannot transition state or allocate seq. Keep deleted sessions terminal unless the explicit recreate path is selected.

- [ ] **Step 4: Extend the init error data contract**

Add optional JSON fields to the existing init ACK data type:

```go
Retryable   bool `json:"retryable,omitempty"`
RetryAfterMS int `json:"retry_after_ms,omitempty"`
```

Map session, authentication, protocol, busy, and hydration failures according to the design spec. Preserve legacy fields for parsing compatibility.

- [ ] **Step 5: Run Gateway focused tests and record GREEN**

Run the command from Step 2, followed by:

```bash
go test ./internal/gateway -run 'Test.*EnsureSeqHydrated|Test.*Init' -count=1
```

- [ ] **Step 6: Commit the Gateway initialization slice**

```bash
git add internal/gateway/conn.go internal/gateway/init.go internal/gateway/hub.go internal/gateway/conn_test.go internal/gateway/seq_test.go
git commit -m "fix: resolve sessions before sequence hydration"
```

### Task 3: Persist accepted user turns across worker timeout

**Files:**
- Modify: `internal/gateway/handler.go`
- Test: `internal/gateway/input_execution_test.go`
- Test: `internal/gateway/bridge_forward_identity_test.go`

**Interfaces:**
- Consumes: durable ingress acceptance and `CaptureInbound`.
- Produces: exactly one durable user turn even when `Worker.Input` returns `ErrKindTimeout`.

- [ ] **Step 1: Add the failing timeout regression test**

```go
func TestInputExecutionTimeoutStillCapturesInboundTurn(t *testing.T) {
    // Fake worker Input returns worker.ErrKindTimeout.
    // The fake worker later emits assistant Message and Done.
    // QueryLatestTurns must return one user turn and one assistant turn
    // with the same generation and in user-before-assistant order.
}
```

- [ ] **Step 2: Run the focused test and record RED**

```bash
go test ./internal/gateway -run TestInputExecutionTimeoutStillCapturesInboundTurn -count=1
```

Expected: only the assistant turn is durable.

- [ ] **Step 3: Move inbound capture to durable acceptance**

Capture the user turn once before uncertain `Worker.Input` delivery. Keep an execution delivery state of unknown for timeout. Ensure success and retry paths cannot capture the same user turn twice.

- [ ] **Step 4: Run focused tests and record GREEN**

```bash
go test ./internal/gateway -run 'TestInputExecution|Test.*CaptureInbound|Test.*Identity' -count=1
```

- [ ] **Step 5: Commit the durable-turn slice**

```bash
git add internal/gateway/handler.go internal/gateway/input_execution_test.go internal/gateway/bridge_forward_identity_test.go
git commit -m "fix: persist user turns before uncertain worker delivery"
```

### Task 4: Isolate AgentConfig and ACP user input

**Files:**
- Modify: `internal/agentconfig/prompt.go`
- Modify: `internal/agentconfig/META-COGNITION.md`
- Modify: `internal/worker/acp/worker.go`
- Test: `internal/agentconfig/prompt_test.go`
- Test: `internal/worker/acp/worker_enhance_test.go`
- Test: `internal/worker/acp/skill_test.go`

**Interfaces:**
- Consumes: loaded `AgentConfig` sections and ACP prompt request.
- Produces: compact system directives, data-marked user/memory context, metadata-only skill catalog, and ACP input without the full prompt prefix.

- [ ] **Step 1: Add prompt-boundary and ACP failing tests**

```go
func TestBuildSystemPromptSeparatesInstructionsFromData(t *testing.T) {
    cfg := Config{Skills: "PRIVATE_SKILL_SENTINEL", User: "user data", Memory: "memory data"}
    prompt := BuildSystemPrompt(cfg)
    require.NotContains(t, prompt, "PRIVATE_SKILL_SENTINEL")
    require.Contains(t, prompt, "<user-data>")
    require.Contains(t, prompt, "<memory-data>")
    require.Contains(t, prompt, "must not disclose")
}

func TestACPFirstOrdinaryInputExcludesFullSystemPrompt(t *testing.T) {
    // Start an ACP worker with a system prompt containing PRIVATE_PROMPT_SENTINEL.
    // Send ordinary input "hello" and assert the ACP prompt text is exactly "hello"
    // or contains only the documented minimal compatibility instruction.
}
```

- [ ] **Step 2: Run focused tests and record RED**

```bash
go test ./internal/agentconfig ./internal/worker/acp -run 'TestBuildSystemPromptSeparatesInstructionsFromData|TestACPFirstOrdinaryInputExcludesFullSystemPrompt' -count=1
```

- [ ] **Step 3: Minimize and separate the prompt**

Replace implementation/SOP content in `META-COGNITION.md` with runtime invariants and a non-disclosure rule. Treat `USER.md` and `MEMORY.md` as data. Convert the always-on skill section to catalog metadata already available from configured skills; do not place full selected skill bodies into an ordinary turn.

- [ ] **Step 4: Fix ACP compatibility injection**

Remove the full `[SYSTEM INSTRUCTIONS]...` prefix from ordinary ACP input. Use an ACP-native system field if exposed by the existing client; otherwise send only the minimal non-disclosure/behavior compatibility text through the supported initialization boundary, never concatenated with the user's first message.

- [ ] **Step 5: Preserve explicit skill behavior**

Run and extend `skill_test.go` so `/skill args` or structured `InvokeSkill` still transmits the selected skill, while `hello` does not.

- [ ] **Step 6: Run focused tests and record GREEN**

```bash
go test ./internal/agentconfig ./internal/worker/acp -count=1
```

- [ ] **Step 7: Commit the prompt-isolation slice**

```bash
git add internal/agentconfig/prompt.go internal/agentconfig/META-COGNITION.md internal/agentconfig/prompt_test.go internal/worker/acp/worker.go internal/worker/acp/worker_enhance_test.go internal/worker/acp/skill_test.go
git commit -m "fix: isolate agent configuration from ordinary input"
```

### Task 5: Enforce one terminal and ordered session events

**Files:**
- Modify: `internal/gateway/hub.go`
- Modify: `internal/gateway/bridge_forward.go`
- Modify: `internal/gateway/commands.go`
- Test: `internal/gateway/webchat_worker_matrix_test.go`
- Test: `internal/gateway/stop_contract_test.go`
- Test: `internal/gateway/bridge_forward_fallback_test.go`

**Interfaces:**
- Consumes: Gateway broadcasts, priority control events, stop fences, and worker Done/error events.
- Produces: one terminal per execution and strictly increasing delivery for all seq-bearing events.

- [ ] **Step 1: Make the sequence race deterministic in a regression test**

Extend `C04-double-stop` with a controlled blocked broadcast writer so an earlier worker event and a later `PriorityControl` ACK are released concurrently. Assert the received seq list is strictly increasing.

- [ ] **Step 2: Run repeated test and record RED**

```bash
go test ./internal/gateway -run 'TestWorkerLifecycleContract/codex_cli/C04-double-stop' -count=20
```

Expected: at least one strict-order assertion fails before the fix; retain the deterministic barrier if probabilistic repetition alone is insufficient.

- [ ] **Step 3: Route all seq-bearing events through one ordered writer**

Change priority handling so events are prioritized before sequence assignment and all assigned events traverse the same per-session writer. Do not send an assigned control seq directly to the connection outside that writer.

- [ ] **Step 4: Add explicit incomplete-stream terminals**

Ensure forwarder exit without Done maps to one of `TURN_STREAM_INCOMPLETE`, `WORKER_DISCONNECTED`, or `WORKER_TIMEOUT`. Preserve `PROVIDER_EMPTY_SUCCESS` only for a confirmed successful Done with no text/tools. Reuse the existing execution terminal guard so stop and crash cannot emit two terminals.

- [ ] **Step 5: Run concurrency and terminal tests**

```bash
go test ./internal/gateway -run 'TestWorkerLifecycleContract|Test.*Stop|Test.*DoneFallback|Test.*TurnIntegrity' -count=10
go test -race ./internal/gateway -run 'TestWorkerLifecycleContract|Test.*Stop' -count=1
```

- [ ] **Step 6: Commit ordered delivery and terminals**

```bash
git add internal/gateway/hub.go internal/gateway/bridge_forward.go internal/gateway/commands.go internal/gateway/webchat_worker_matrix_test.go internal/gateway/stop_contract_test.go internal/gateway/bridge_forward_fallback_test.go
git commit -m "fix: order session events and complete every execution"
```

### Task 6: Add capability negotiation and optimistic message identity

**Files:**
- Modify: `internal/gateway/init.go`
- Modify: `webchat/lib/ai-sdk-transport/client/types.ts`
- Modify: `webchat/lib/adapters/hotplex-runtime-adapter.ts`
- Modify: `webchat/lib/ai-sdk-transport/client/browser-client.ts`
- Test: `webchat/lib/adapters/runtime-adapter.test.ts`
- Test: `webchat/lib/ai-sdk-transport/client/browser-client.test.ts`
- Modify: `docs/architecture/AEP-v1-Protocol.md`

**Interfaces:**
- Produces optional `server_version`, `capabilities`, `retryable`, and `retry_after_ms`; stable `client_message_id` reconciliation.

- [ ] **Step 1: Add failing capability and identity tests**

```ts
it("records advertised gateway capabilities", async () => {
  serverInitAck({ state: "idle", server_version: "v1.test", capabilities: ["control_stop_v1"] });
  expect(client.capabilities.has("control_stop_v1")).toBe(true);
});

it("reconciles optimistic and durable user turns by client_message_id", () => {
  const merged = reconcile(optimisticUser("cm_1"), historyUser("cm_1"));
  expect(merged.filter(m => m.role === "user")).toHaveLength(1);
});
```

- [ ] **Step 2: Run WebChat tests and record RED**

```bash
cd webchat
pnpm exec vitest run lib/ai-sdk-transport/client/browser-client.test.ts lib/adapters/runtime-adapter.test.ts
```

- [ ] **Step 3: Add additive capability fields**

Advertise `control_stop_v1`, `init_retry_v2`, `client_message_id_v1`, and `ordered_session_events_v1`. Parse and retain them in BrowserClient. Do not reject an old server solely because it omits optional fields; gate only behavior that requires a capability.

- [ ] **Step 4: Reconcile messages by stable identity**

Carry `client_message_id` through optimistic user messages and history. Prefer it over role/content signatures. Preserve the optimistic question with `unknown` delivery status on ambiguous network failure.

- [ ] **Step 5: Update AEP documentation and run GREEN tests**

Document additive fields, error classification, compatibility precedence, and ordering. Run the Step 2 tests plus WebChat type checking.

- [ ] **Step 6: Commit capability and identity support**

```bash
git add internal/gateway/init.go webchat/lib/ai-sdk-transport/client/types.ts webchat/lib/ai-sdk-transport/client/browser-client.ts webchat/lib/ai-sdk-transport/client/browser-client.test.ts webchat/lib/adapters/hotplex-runtime-adapter.ts webchat/lib/adapters/runtime-adapter.test.ts docs/architecture/AEP-v1-Protocol.md
git commit -m "feat: negotiate reliability capabilities and message identity"
```

### Task 7: Full verification and release evidence

**Files:**
- Verify only; any regression repair returns to the owning task and repeats its red-green cycle.

- [ ] **Step 1: Run Go formatting and static checks**

```bash
gofmt -w internal/gateway/conn.go internal/gateway/init.go internal/gateway/hub.go internal/gateway/handler.go internal/gateway/bridge_forward.go internal/gateway/commands.go internal/gateway/conn_test.go internal/gateway/seq_test.go internal/gateway/input_execution_test.go internal/gateway/bridge_forward_identity_test.go internal/gateway/webchat_worker_matrix_test.go internal/gateway/stop_contract_test.go internal/gateway/bridge_forward_fallback_test.go internal/agentconfig/prompt.go internal/agentconfig/prompt_test.go internal/worker/acp/worker.go internal/worker/acp/worker_enhance_test.go internal/worker/acp/skill_test.go
go vet ./internal/gateway/... ./internal/agentconfig/... ./internal/worker/acp/...
```

- [ ] **Step 2: Run the full Go suite**

```bash
go test ./...
```

- [ ] **Step 3: Run WebChat verification**

Use the scripts defined in `webchat/package.json`:

```bash
cd webchat
pnpm test
pnpm lint
pnpm exec tsc --noEmit
```

- [ ] **Step 4: Run repeated and race-sensitive contracts**

```bash
go test ./internal/gateway -run 'TestWorkerLifecycleContract|Test.*Stop|Test.*EnsureSeqHydrated' -count=100
go test -race ./internal/gateway -run 'TestWorkerLifecycleContract|Test.*Stop|Test.*EnsureSeqHydrated' -count=1
```

- [ ] **Step 5: Review the complete diff**

```bash
git diff --check
git status --short
git log --oneline --decorate -10
```

Confirm every acceptance criterion in the spec has corresponding test evidence, no test is skipped, and no unrelated file is modified.
