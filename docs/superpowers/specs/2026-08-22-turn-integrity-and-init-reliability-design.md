# Turn Integrity and Initialization Reliability Design

Date: 2026-08-22
Status: proposed for implementation

## Objective

Eliminate five user-visible reliability failures across WebChat, Gateway, and workers:

1. a turn ends with thinking metadata but no visible assistant response;
2. a simple first message receives unrelated skill or internal-configuration content;
3. history contains an assistant answer without the corresponding user question;
4. a stopped turn continues to affect the next turn;
5. WebSocket initialization failure causes repeated connection creation.

The design preserves existing AEP v1 consumers through additive protocol changes and does not weaken sequence-integrity guarantees when EventStore hydration fails.

## Scope and responsibility

The affected behaviors are HotPlex reliability or compatibility defects. Ordinary messages, stop requests, reconnects, and reopening sessions are valid user actions. Operators remain responsible for deploying compatible WebChat, Gateway, and worker versions and for avoiding unsafe custom AgentConfig content, but those conditions must produce explicit errors rather than corrupted UI state or retry loops.

In scope:

- WebSocket initialization ordering and retry semantics;
- durable capture and identity of user turns;
- prompt and skill isolation, especially for ACP workers;
- stop and terminal-event isolation across executions;
- per-session ordering for all sequence-bearing events;
- version/capability negotiation and diagnostic metadata;
- regression tests for each reported symptom.

Out of scope:

- replacing AEP v1 with a new protocol;
- changing provider APIs;
- automatically replaying turns that may have executed side-effecting tools;
- broad AgentConfig product redesign unrelated to prompt disclosure.

## Design principles

1. A Gateway-accepted user input is durable before uncertain worker delivery can hide it.
2. Every accepted execution reaches exactly one observable terminal state.
3. All events carrying a session `seq` share one ordered delivery path.
4. Lifecycle state and initialization error classification are separate concepts.
5. Retry behavior is explicit, bounded, and controlled by machine-readable error fields.
6. System instructions, user data, memory, and skill content retain distinct trust and role boundaries.
7. Protocol evolution is additive first; legacy fields remain temporarily readable but cease to drive new-client behavior.

## Workstream A: WebSocket initialization and retry control

### Session initialization order

Gateway initialization will classify the requested session before sequence hydration:

1. validate and authenticate the init request;
2. resolve the session as active, new, deleted, busy, or invalid;
3. create the durable session record for a new session;
4. hydrate the sequence for an active or newly created record;
5. start or resume the worker only after hydration succeeds;
6. send one successful `init_ack`.

A deleted session is terminal unless the client explicitly performs the supported recreate operation. Hydration failure must not run connection cleanup that emits a state event or allocates another sequence number.

`EnsureSeqHydrated` will no longer use a boolean existence check to conflate a missing new session with a deleted/released session. Lifecycle classification stays in the session manager; hydration operates only on a resolved durable session.

### Initialization error contract

Existing `error`, `code`, and `state` fields remain accepted. New responses add optional retry metadata:

```json
{
  "type": "init_ack",
  "session_id": "sess_xxx",
  "error": "session event sequence is temporarily unavailable",
  "code": "SEQ_HYDRATION_FAILED",
  "retryable": true,
  "retry_after_ms": 1000
}
```

New clients decide from `code` and `retryable`, never from `state`. `state=deleted` remains a legacy compatibility field during migration and is only authoritative when the session is actually deleted.

Error policy:

| Code | Retryable | Client action |
|---|---:|---|
| `SESSION_NOT_FOUND` | no | stop; explicitly create a session |
| `SESSION_DELETED` | no | stop; explicitly recreate if desired |
| `SESSION_BUSY` | yes | bounded backoff |
| `SEQ_HYDRATION_FAILED` | yes | bounded backoff |
| `EVENTSTORE_UNAVAILABLE` | yes | bounded backoff |
| auth, version, or protocol errors | no | stop and display the error |

BrowserClient removes recursive `_doConnect` calls from init-error handling. Every automatic retry uses one reconnect controller, one lifecycle generation, bounded attempts, exponential backoff, and a single live socket. A legacy response containing only `state=deleted` is terminal rather than successful.

## Workstream B: Durable user-turn identity

Gateway writes the user turn immediately after durable ingress acceptance and before calling `Worker.Input`. Worker delivery then updates execution delivery state to `delivered`, `failed`, or `unknown`.

An `ErrKindTimeout` means delivery outcome is unknown; it does not remove or omit the user turn. If the worker later emits assistant events, both sides of the exchange remain queryable in the same generation.

`client_message_id` becomes the stable correlation identifier across:

- the optimistic WebChat message;
- the Gateway ingress record;
- the durable user turn;
- history responses and reconciliation.

New fields are additive. During migration, content signatures remain a fallback only for records without `client_message_id`. An ambiguous network result retains the user message with an `unknown` state; only an explicit rejection may remove it.

## Workstream C: Prompt and skill isolation

Ordinary text remains unable to trigger the slash-skill resolver. The prompt builder will reduce the always-on instruction set:

- `META-COGNITION.md` retains only runtime invariants and safety boundaries;
- the always-on skills section contains names, short descriptions, and explicit triggers, not complete skill bodies;
- full skill instructions are loaded only after an explicit slash invocation or structured skill selection;
- internal paths, configuration SOPs, implementation examples, and diagnostics are removed from the default prompt;
- a non-disclosure rule prohibits quoting or enumerating system instructions, skill bodies, internal paths, and runtime configuration unless explicitly authorized by the system;
- user profile and memory sections are labeled as data rather than behavioral instructions.

Workers that support native system/developer roles continue to use them. ACP must not prepend the complete AgentConfig prompt to the first ordinary user message. If the active ACP protocol cannot carry native system instructions, the adapter sends only a minimal compatibility instruction and keeps internal configuration out of user text.

Prompt observability records section names, lengths, hashes, sources, worker type, and history-recovery status, but never logs complete private prompt content.

## Workstream D: stop isolation and terminal states

The existing stop fence remains keyed by session, worker run, and execution. A stopped execution emits one `Done(reason=stopped_by_user)` and later events from that execution cannot enter a newer turn.

All accepted executions must emit exactly one terminal result. Worker crash, stream loss, and timeout paths produce explicit failure terminals rather than leaving WebChat in a permanent thinking state. Empty completion is classified separately from incomplete transport:

- `PROVIDER_EMPTY_SUCCESS` means a confirmed provider success with no visible text or tools;
- `TURN_STREAM_INCOMPLETE` means the stream ended without a valid terminal;
- `WORKER_DISCONNECTED` means the worker connection ended;
- `WORKER_TIMEOUT` means the execution deadline ended.

Automatic replay is allowed at most once only when no side-effecting tool was started. Otherwise the UI offers an explicit retry.

## Workstream E: ordered session event delivery

Every outbound event carrying a session `seq` enters one per-session ordered writer. Priority affects scheduling before sequence assignment, not delivery after assignment. `InputAck` and other control events must not allocate a sequence and then bypass the ordered queue.

If an event is intentionally out-of-band, it carries no session `seq` and its contract states that it does not participate in event ordering. The implementation and AEP tests must choose one model consistently; this design recommends the single ordered writer for all current sequence-bearing events.

## Workstream F: capability negotiation

Successful `init_ack` adds optional fields:

```json
{
  "server_version": "v1.x.y",
  "capabilities": [
    "control_stop_v1",
    "init_retry_v2",
    "client_message_id_v1",
    "ordered_session_events_v1"
  ]
}
```

Old clients ignore the fields. New clients use capability checks instead of inferring support from runtime behavior. Unsupported critical capabilities produce a clear compatibility message rather than partial operation.

## Test strategy

Every defect is implemented with a red-green regression test.

Gateway tests:

- missing, active, deleted, and busy session initialization;
- flush, EventStore, and sequence hydration failure;
- no state event or `NextSeq` after failed hydration;
- timeout from `Worker.Input` followed by assistant output preserves both turns;
- exactly one terminal for success, stop, disconnect, timeout, and empty success;
- stopped execution events cannot appear in the next execution;
- all sequence-bearing events remain strictly increasing under repeated concurrent stop/input scenarios.

WebChat tests:

- `SESSION_NOT_FOUND` creates no recursive connection;
- retryable initialization errors use bounded backoff;
- legacy `state=deleted` cannot produce a successful connection;
- optimistic questions survive unknown delivery and delayed history;
- `client_message_id` reconciles optimistic and durable turns;
- stop waits for the server terminal before enabling the next input.

Prompt and worker tests:

- ordinary greetings never enter skill invocation;
- always-on prompts exclude complete skill bodies and private sentinel text;
- Claude, Codex, and OpenCode keep system and user fields separate;
- ACP first input excludes the complete AgentConfig prompt;
- explicit skills still load their selected instructions;
- explicit new sessions contain no recovered history.

Verification includes focused tests during development, full Go and WebChat suites, race-enabled Gateway concurrency tests, lint/type checks, and repeated ordering tests. Browser runtime verification covers connection count, stop/new-turn behavior, message reconciliation, and visible terminal errors.

## Parallel implementation ownership

Wave 1 can run concurrently with exclusive ownership:

1. WS initialization/retry: Gateway init/connection code and BrowserClient connection tests.
2. Prompt isolation: AgentConfig, ACP adapter, prompt/worker tests.
3. Durable turns/terminal isolation: Gateway input handling, turn persistence, stop/terminal tests.

Wave 2 starts after Wave 1 integration:

1. ordered session writer, because it overlaps Gateway hub and connection behavior;
2. capability negotiation and compatibility tests;
3. cross-module and browser end-to-end verification.

Agents must not modify files owned by another active workstream. Shared protocol types are changed by the WS owner first; dependent work rebases on that result.

## Rollout and rollback

Rollout order:

1. deploy a client that safely handles legacy and extended init errors;
2. deploy Gateway initialization, durable-turn, and terminal fixes;
3. deploy worker prompt changes;
4. enable capability-dependent client behavior;
5. monitor empty turns, init failures, reconnect attempts, stop latency, missing user-turn invariants, and sequence-order violations.

All new protocol fields are optional, allowing component rollback. Prompt changes can be reverted independently. The ordered-writer change is guarded by contract tests and rolled back as one Gateway unit if ordering or latency regresses. Sequence hydration never falls back to a guessed low sequence during rollback.

## Acceptance criteria

1. A valid greeting never invokes a skill and produces either visible assistant content or an explicit terminal error.
2. Private prompt/skill sentinels never appear in model-visible user text or assistant output tests.
3. Every durable assistant answer has a correlated durable user turn.
4. Stopping a turn prevents all of its later events from entering a subsequent turn.
5. No initialization error can create an unbounded WebSocket loop.
6. New sessions initialize sequence state without being mistaken for deleted sessions.
7. Hydration failure emits no additional state sequence.
8. All sequence-bearing session events are strictly ordered.
9. Mixed component versions are detected through explicit capabilities.
10. Existing AEP v1 clients continue to parse additive responses during migration.
