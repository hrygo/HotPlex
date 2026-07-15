# PR #886 Review Fixes Design

**Date:** 2026-07-15  
**Scope:** Event persistence correctness, replay ordering, reconnect recovery, and diagnostics

## Context

PR #886 makes persisted events the source of truth for session replay. Review found that the first implementation can delete valid legacy events, allocate duplicate sequence numbers around reconnects, mix ID ordering with sequence-number cursors, persist accumulated deltas out of order, leave WebChat silently disconnected after repeated workspace failures, and omit debug fields when database reads fail.

The repair must preserve all existing event rows and keep replay behavior deterministic across SQLite and PostgreSQL.

## Design

### 1. Preserve legacy duplicates while enforcing uniqueness for new events

Migration 028 will add a non-null `seq_guard_id` column with a default of `0`. Every row that predates the migration is assigned its own row ID as the guard value. The unique index becomes `(session_id, seq_guard_id, seq)`.

Existing rows are therefore never deleted, including rows that share `(session_id, seq)`. New inserts omit the guard column and receive `0`, so duplicate sequence numbers remain rejected among post-migration writes. The down migration removes the index and compatibility column without deleting event data.

This is preferred over resequencing history because sequence numbers may already be referenced by protocol metadata, turns, or external diagnostics. It is also preferred over removing the constraint because the database should continue detecting new allocation defects.

### 2. Tie sequence allocation to the session, not the connection

The Hub will retain an initialized per-session sequence counter when the last WebSocket connection leaves. A reconnect in the same process therefore continues from the highest allocated value, including values still queued in the asynchronous collector.

On first use after process startup, hydration reads the committed database maximum. Hydration errors are returned to the connection handshake; the system must not silently initialize at `1`. A physical session deletion may explicitly forget the counter, but ordinary disconnects must not.

The collector still treats uniqueness violations as errors. Tests will verify that reconnects cannot cause an entire batch to be rejected through a duplicate sequence allocation.

### 3. Use database IDs as replay cursors end to end

Persisted events will expose their database row ID. Latest, before, and after queries will all order and filter by this ID. Page metadata will expose ID cursors while retaining event sequence numbers as event metadata only.

This keeps pagination coherent when legacy rows contain duplicate or non-monotonic sequence numbers. SQLite and PostgreSQL queries, mocks, API models, and tests will be updated together.

### 4. Preserve delta order across every flush path

Reasoning and message accumulators may coexist for one session. Before appending to one accumulator, direct string-capture paths will flush the other accumulator. Timer and size-triggered flushes will submit pending requests in `firstSeq` order.

The ordering rule applies equally to JSON capture, direct string capture, timer flushes, and size-threshold flushes. Tests will cover alternating reasoning/message deltas and concurrent timer boundaries.

### 5. Make WebChat recovery bounded and visible

Recovery state will be tracked per workspace instead of relying on one global cooldown timestamp. A failed workspace is excluded while another candidate is attempted. Only one recovery attempt may be in flight.

When every candidate fails, the UI enters an explicit terminal recovery state with a localized explanation and retry action. Retry clears the failed set, reloads workspaces, and remounts the transport. Chinese and English translation resources will be changed together.

### 6. Stabilize debug response shape

Debug responses will initialize `db_turn_count` and `db_last_seq` to `null`. Successful database reads replace those values; failed reads retain `null` and emit structured warning logs with the session ID and operation.

## Compatibility and rollout

- Migration behavior is identical for SQLite and PostgreSQL.
- No legacy event row is removed or rewritten.
- API cursor semantics change from sequence number to row ID; field names and tests will make that distinction explicit.
- Event sequence numbers remain monotonic for newly allocated events and remain available in every event payload.
- WebChat adds localized user-visible recovery copy without changing the default successful path.

## Verification

The implementation is complete only after:

1. migration up/down tests prove duplicate legacy rows survive;
2. reconnect and hydration-error tests prove sequence allocation is fail-closed and monotonic;
3. SQLite and PostgreSQL pagination tests cover non-monotonic and duplicate legacy sequences;
4. collector race tests cover timer, size, JSON, and direct-string ordering;
5. WebChat tests cover multiple workspace failures and retry;
6. admin handler tests assert stable nullable debug fields;
7. `make check`, targeted `go test -race`, TypeScript checking, and Vitest pass;
8. an independent code review finds no unresolved P0/P1 issues.
