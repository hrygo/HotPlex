# Input Lifecycle Code Review Fixes

**Date:** 2026-07-13
**Scope:** Fixes for the 15 findings from the max-effort code review of branch `feat/execution-ingress-ledger` (durable idempotent input acknowledgements + webchat retry/toggle fixes).
**Status:** Design approved 2026-07-13

## Goal

Close every confirmed and plausible defect from the code review without reintroducing the toggle bug that this branch already fixed. The dominant theme is the webchat `pendingInput` state machine: it lacks forced-reset paths, so the client can be permanently locked out of sending whenever the gateway emits a terminal outcome the client does not clear.

## Invariants

- A single private method `_settlePending(outcome)` is the only place `pendingInput` is cleared or settled. Every terminal path routes through it.
- The client can never be permanently blocked from sending: every `pendingInput` is bounded by either a terminal event (Done / terminal InputAck / Error / disconnect) or a grace timer.
- The gateway's `input.ack` always reflects the intended terminal status, independent of whether the `SetStatus` DB write succeeded.
- No input content or transport metadata (`platform_msg_id`) is included in the idempotency payload hash; the hash depends only on user content + metadata.
- The existing tool-call expand/collapse toggle behavior (fixed in commits `6aea34a7` / `b9099272`) is preserved.

## Design

### A. `browser-client.ts` — centralized pendingInput lifecycle

Introduce one settle point:

```ts
type SettleOutcome =
  | { kind: 'delivered' }              // resolve + clear
  | { kind: 'done' }                   // resolve + clear
  | { kind: 'failed'; error: Error }   // reject  + clear
  | { kind: 'unknown'; error: Error }  // reject  + tombstone (bounded)
  | { kind: 'reset'; error: Error };   // reject  + clear (error/disconnect/timeout)

private _settlePending(outcome: SettleOutcome): void
```

Behavior:
- `delivered` / `done` / `failed` / `reset`: clear the retry timer, clear the 300s timer, settle the promise, set `pendingInput = null`.
- `unknown`: clear timers, reject the promise, mark `pendingInput.tombstone = true`, arm a **120s grace timer** that calls `_settlePending({kind:'reset', error})` if no Done arrives. Done / disconnect / reconnect_failed clear the tombstone early.

Routing changes:
- **InputAck handler**: `delivered` → settle `delivered` (fixes C3); `unknown` → settle `unknown`; `failed` (non-SESSION_BUSY) → settle `failed`; `accepted` → no-op.
- **Error handler**: every non-SESSION_BUSY error → `_settlePending({kind:'reset', error})` (fixes S1). SESSION_BUSY → `_handleSessionBusy()` unchanged.
- **`_handleClose`**: both the `reconnect_failed` branch and the `disconnected` branch call `_settlePending({kind:'reset', error})` (fixes S2).
- **`disconnect()`**: route the existing reject through `_settlePending`.
- **300s timer**: store the handle on the pending object; every settle path clears it (fixes Q8).
- **sync `sendInput`**: arm a 300s timer (same value as `sendInputAsync`) that forces `_settlePending({kind:'reset', error})`. Its `resolve`/`reject` are no-ops but the timer still clears `pendingInput`, so sync sends can never block forever (fixes C2 sync path).
- **SESSION_BUSY retry race (C6)**: because `_settlePending` is the single clearing point and the SESSION_BUSY path is handled distinctly (it does not settle — it reschedules), the ordering fragility is removed by construction.

### B. `hotplex-runtime-adapter.ts`

- Subscribe to `client.on('inputAck', ...)`. On `unknown` / `failed`, surface a localized toast via i18n offering "retry with a new ID" (which clears `pendingInput` and lets the user send again). Fixes S4 (UI gets feedback for ambiguous outcomes).
- `sendInput` catch (C4): branch on the error message. For "Input already pending", show the localized `chat:input_still_processing` text and **do not** remove the user's message. For genuine connection errors, keep the existing behavior (remove message + connection error text).

### C. `handler.go` (backend)

- **`finishInputExecution` (C1)**: mutate `record.Status` / `record.ErrorCode` to the intended terminal values **before** calling `SetStatus`. The ack therefore always carries the intended status. On `SetStatus` error, the existing defer safety-net still marks the record `unknown` in the DB; the in-memory record already reflects the intended outcome so the ack is correct.
- **`finishRejected` → `finishOutcome(status, code)` (Q2)**: generalize the closure to accept a status. Route all terminal paths (resume-failed, not-active, worker-nil, transition-failed, worker-timeout, worker-error, delivered) through it. Eliminates 4 inline duplicates and makes `finalized` reliable.
- **`acceptInputExecution` (Q1)**: replace `sha256.Sum256` + `fmt.Sprintf("%x")` with the existing same-package `sha256Hex` helper (`tool_audit.go`).
- **Payload hash (C7)**: strip `platform_msg_id` from the data map before marshalling/hashing. The hash depends only on user content + metadata; the idempotency key (`client_message_id` / `platform_msg_id` / `env.ID`) is independent of the payload.
- **`clientMessageID` (Q16)**: stop writing back to `env.ID`. Return the value from a local; assign `env.ID` only inside `acceptInputExecution` when a record is actually being created (or not at all — the envelope ID is already set by the client/adapter).
- **`env.Metadata` injection (Q15)**: remove the write-only `execution_id` / `client_message_id` injection (no consumer reads it).
- **`cancelRetryIfNeeded` (C5)**: restore the call on the genuine-new-input early-return paths — session-not-found and execution-store persistence failure. Keep it skipped for payload-conflict and malformed-data (those are not real new inputs).

### D. `AssistantMessage.tsx` + tool components

- Keep `ext.content.map()`. Extract each branch into a keyed, `React.memo`-ized sub-component: `<ToolCallPart>`, `<TextPart>`, `<ReasoningPart>`, `<ToolSummaryPart>`. Key by `toolCallId || partIndex`. This restores per-part memoization lost when `MessagePrimitive.Parts` was removed (fixes Q5) without reintroducing the toggle bug.
- `hasSubsequentActiveContent` (Q12): compute once via a single reverse pre-pass producing a boolean per part index, O(n) total.
- Remove the dead `MessagePrimitive` import (Q6).

### E. `input_execution_test.go`

- Add `t.Parallel()` to all four test functions (Q7).
- Harden `fakeExecutionStore`: record the `AcceptRequest` arguments for assertion; add a test mode where `SetStatus` returns a DB error, exercising the C1 fix path (the ack must still carry the intended terminal status).

## Error Handling

- `_settlePending` is idempotent: calling it on an already-null `pendingInput` is a no-op; calling it twice on the same pending settles the promise once (Promise semantics) and clears once.
- The 120s tombstone grace timer is cleared on every settle path; it never leaks.
- `finishInputExecution` no longer depends on DB success for the ack to be correct; DB failure degrades to the defer marking the record `unknown` in the DB (durable truth) while the client already saw the intended terminal status.

## Tests

- **browser-client** (vitest): extend the existing retry-identity suite with cases for — (a) delivered InputAck clears pending; (b) non-SESSION_BUSY Error clears pending; (c) `disconnected` close rejects pending; (d) 300s timer is cleared on settle (no leak); (e) unknown tombstone auto-clears after the grace period; (f) sync sendInput unblocks after its timer.
- **gateway handler** (Go): add a case where `fakeExecutionStore.SetStatus` returns an error — assert the emitted `input.ack` still carries the intended terminal status (C1 regression guard).
- **execution store**: existing SQLStore tests unchanged.
- **runtime-adapter**: add a test that an `inputAck(unknown)` event surfaces the retry affordance (S4).
- Run targeted Go tests with `-race -count=1`, the standalone Go client suite, webchat `vitest` + `tsc --noEmit`, and lint.

## Non-goals

- Execution completion correlation, automatic retries of `unknown`, distributed scheduling, and a general execution queue remain outside this scope (tracked by #849 / #851).
- Redesigning the AEP `input.ack` protocol shape.
