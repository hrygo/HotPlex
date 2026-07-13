# OCS Session Cleanup Outbox Design

## Goal

Preserve OpenCode Server (OCS) conversation context across HotPlex worker and
gateway restarts, while making remote OCS-session deletion durable and safe.
The change addresses two review findings on PR #873:

1. Retention GC can race a resumed session and later delete the remote session
   that the resume needs.
2. A remote deletion failure is only logged today, so it is lost on a gateway
   restart and never retried.

This is deliberately scoped to session cleanup infrastructure and the OCS
cleaner. It does not change session retention policy or worker start/resume
semantics.

## Chosen approach

Persist a cleanup task in the same database transaction as every lifecycle
operation that makes a HotPlex session unavailable (`DELETED`, physical delete,
or terminated-session retention GC). A gateway-owned worker leases due tasks,
calls the existing worker cleanup registry, and removes a task only after the
remote delete is confirmed. `404 Not Found` remains a successful, idempotent
delete for OCS.

The task row is also a deletion tombstone: while it exists, a stale in-memory
session snapshot cannot recreate the deleted row. A resume that loses the race
returns a retryable cleanup-pending error rather than starting a worker that
could reuse a remote session scheduled for deletion.

The considered alternative was an in-memory mutex plus bounded asynchronous
retries. It is smaller but cannot survive gateway restart or provide a
database-enforced barrier against stale `Upsert` writes, so it is rejected.

## Data model

Add matching SQLite and PostgreSQL migrations for `session_cleanup_tasks`.

| Column | Meaning |
| --- | --- |
| `id` | Generated task identifier. |
| `session_id` | Owning HotPlex session ID; unique while deletion is pending. |
| `worker_type` | Worker implementation to dispatch through the cleanup registry. |
| `worker_session_id` | Immutable remote-session ID captured at deletion time. |
| `attempts` | Number of claimed cleanup attempts. |
| `next_attempt_at` | Earliest time the task may be leased again. |
| `lease_until` | Lease expiry, so a task abandoned by process shutdown becomes runnable. |
| `last_error` | Most recent bounded diagnostic error. |
| `created_at` / `updated_at` | Audit and scheduling timestamps. |

Only sessions with a non-empty `worker_session_id` create a task. Cleanup for
workers without a registered cleaner is a successful no-op, preserving the
current behavior for non-persistent workers.

`session_id` is unique for an outstanding task. Successful completion deletes
the task rather than retaining a historical tombstone; a later intentional new
session with the same deterministic HotPlex ID can then be created normally.

## Atomic lifecycle rules

The store gains transactional operations instead of the current
delete-then-callback pattern:

- Logical delete changes the session to `DELETED` and enqueues its immutable
  cleanup task in one transaction.
- Physical delete removes the session and enqueues the task in one transaction.
- Retention GC deletes only still-`TERMINATED` rows and enqueues tasks in the
  same transaction.

Each transaction returns the affected session metadata only for logging and
audit; it does not run remote cleanup inline. The `Manager.OnDelete` callback
and its fire-and-forget use are removed.

The session store checks for a pending cleanup tombstone before every write
that could recreate or revive the same session ID. If present, it returns the
new sentinel `ErrSessionCleanupPending`. This check is enforced inside the
write transaction, not as a preceding read, so a stale `transitionState`
snapshot cannot win a check-then-write race.

`Get` also reports `ErrSessionCleanupPending` when the session row is gone but
its cleanup task remains. Gateway resume maps this error to a retryable
lifecycle failure and never creates or resumes an OCS worker. Once cleanup
finishes, the task is removed; a later request sees the normal not-found state
and may create a fresh session through the existing path.

## Cleanup execution

A `session` cleanup runner is started after worker registration and after
orphan-process cleanup. It:

1. Drains due tasks once during gateway startup, then polls on a bounded
   interval until the gateway context is cancelled.
2. Atomically leases a small batch. Leases prevent duplicate deletion when two
   gateway processes share PostgreSQL or a process is slow to exit.
3. Calls `worker.CleanupSession` with a per-attempt timeout.
4. Deletes the task on success, including the OCS cleaner's `404` success
   result. On failure it records the error and schedules exponential backoff
   with an upper bound; it never drops the task solely because the retry count
   is high.

Errors are logged with task/session/worker identifiers. Failure to schedule,
lease, or execute one task must not stop remaining tasks or gateway startup.
Shutdown cancels the runner; an unexpired lease is recovered automatically
after its expiry on the next startup.

## Concurrency and safety

The durable tombstone is the source of truth for deletion ownership:

- If resume transitions the row from `TERMINATED` before retention GC's
  transaction, GC affects no row and creates no task.
- If GC/delete commits first, the tombstone blocks stale `Upsert` and resume;
  the remote ID captured in the task can never be attached to a new worker.
- A newly created session cannot reuse the deterministic HotPlex ID until the
  tombstone is removed, avoiding ambiguity between the old and new remote
  session identity.
- The remote delete always targets `worker_session_id` stored in the task, not
  a later session row, so it cannot delete a newer remote session.

Existing lock order remains `Manager.mu` then `managedSession.mu`. The store
transaction is the cross-process synchronization boundary; no new long-lived
manager lock is held during remote I/O.

## Tests and acceptance criteria

Add SQLite unit/integration coverage for:

1. Logical delete, physical delete, and retention GC each atomically create a
   task with the original OCS session ID.
2. A retention-GC versus resume interleaving has exactly one winner: resume
   leaves no task, or cleanup wins and resume receives
   `ErrSessionCleanupPending`; no stale write recreates the row.
3. A remote cleanup failure records a retry, backs off, and succeeds on a later
   attempt; `404` completes immediately.
4. A leased-but-unfinished task is recovered and run after runner restart.
5. A task targets the old remote ID even if a new local session is later
   created with the same HotPlex session ID.

Run the existing session, gateway, worker, OCS, migration, race, and repository
quality gates. PR #873 is complete only when the new tests pass and review
findings no longer apply.

## Non-goals

- Changing OCS's session-preserving `release()` behavior from PR #873.
- Deleting event history as part of the outbox operation.
- Adding an operator UI for cleanup-task inspection or manual replay.
- Replacing the existing worker cleanup registry.
