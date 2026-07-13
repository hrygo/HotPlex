# Execution Ingress Ledger Review Fixes

**Date:** 2026-07-13  
**Scope:** PR #872 follow-up fixes only

## Goal

Close the three merge-blocking review gaps in the durable input acknowledgement
feature without expanding the PR into the execution queue work tracked by #851.

## Invariants

- A duplicate ordinary input is side-effect free: it may read the session and
  replay the stored ACK, but it must not cancel LLM retry, resume a Worker,
  transition session state, or invoke `Worker.Input`.
- A WebChat retry after an ambiguous transport failure reuses the original
  client message ID. A retry after a definitive `SESSION_BUSY` rejection uses a
  new ID.
- Repeating the same terminal store transition returns success without changing
  status, error code, `updated_at`, or `delivered_at`.
- A different terminal transition remains rejected, and no raw input content or
  metadata is added to the execution ledger.

## Design

### Gateway ordering

Keep interaction responses and control/worker commands on their existing paths.
For ordinary input, load the session for validation, then call the execution
store before any state-changing operation. If the record is a duplicate, replay
its persisted ACK and return immediately. Only a newly accepted record may
cancel an active LLM retry, resume a terminated session, perform the
`IDLE -> RUNNING` transition, and call `Worker.Input`.

This preserves the existing command and interaction semantics while making the
ledger the idempotency gate for ordinary input delivery.

### WebChat retry identity

Represent `pendingInput` with both the content and its stable client message ID.
Construct input envelopes through a helper that can either allocate a new ID or
reuse an existing one. After reconnect initialization succeeds, resend a pending
input with its stored ID because the previous delivery outcome is ambiguous.

For `SESSION_BUSY`, replace the pending ID before resending because the server
has definitively rejected that attempt. Guard reconnect resends so each
successful reconnect sends the pending input at most once.

### SQL terminal idempotency

Attempt the terminal update only from `accepted`. If no row is updated, read the
current status: return success when it already equals the requested status and
return `ErrNotFound` for a missing record or a different terminal status. The
same-status path performs no write, preserving the original timestamps and
error code.

## Error Handling

- Persistence failure before dispatch returns `INTERNAL_ERROR` and causes no
  Worker-side mutation.
- Payload conflict remains `INVALID_MESSAGE`.
- ACK delivery failure remains logged; the durable record allows a safe replay.
- Reconnect resend exceptions follow the existing WebSocket error path without
  discarding the pending ID.

## Tests

- Gateway regression: duplicate input does not call retry cancellation or
  session resume and only emits the stored ACK.
- WebChat regression: reconnect reuses the pending ID; `SESSION_BUSY` allocates
  a different ID.
- Store regression: a repeated `delivered` transition preserves both
  `updated_at` and `delivered_at`; a different terminal transition is rejected.
- Run targeted Go tests with `-race`, the standalone Go client suite, WebChat
  tests/build, formatting/lint checks, and an independent code review.

## Non-goals

- Execution completion correlation, automatic retries of `unknown`, distributed
  scheduling, and a general execution queue remain outside this PR.
