## OVERVIEW
WebSocket broadcast hub with AEP v1 protocol dispatch, per-connection read/write pumps, session-worker lifecycle orchestration, LLM auto-retry, and HTTP session API.

## STRUCTURE
```
hub.go          # Hub struct: broadcast loop, conn/session registry, backpressure, seq gen
conn.go         # Conn struct: read/write pumps, Handler struct for event dispatch
bridge.go       # Bridge struct: session ↔ worker lifecycle, event forwarding, LLM retry integration
llm_retry.go    # LLMRetryController: retryable error detection, exponential backoff
api.go          # GatewayAPI: HTTP session endpoints (list/get/terminate)
init.go         # Init handshake: InitData, InitAckData, caps, ValidateInit, BuildInitAck
heartbeat.go    # Missed ping counter with stop channel
testutil/       # WebSocket mock helpers for tests
*_test.go       # 6+ test files (conn_test, hub_test, ctrl_test, bridge_test, init_test, llm_retry_test)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Broadcast hub | `hub.go:68` | Hub struct, Run() goroutine, seq gen |
| Connection pumps | `conn.go:35` | Conn struct, ReadPump/WritePump goroutines |
| Event dispatch | `handler.go` | Handler: handleInput, handlePing, handleControl, handleWorkerCommand, handleSkillsList |
| Input idempotency | `handler.go` + `internal/execution` | Durable accept → `input.ack` → Worker delivery outcome; duplicate IDs never redeliver |
| Passthrough feedback | `handler.go` | `handlePassthroughCommand`: sends message AEP after WorkerCommander ops; rejects /effort, /commit |
| Fast reconnect | `conn.go:377` | Skips Transition when session already running with live worker |
| writeCh buffering | `conn.go:123` | Buffer 256; do not reduce without safe background-tab client batching |
| Seq hydration | `conn.go:566` | Never fall back to 1 on DB error — duplicate would roll back an entire collector batch |
| Terminal event ordering | `bridge_forward.go:544` | No terminal runtime event when durable write failed |
| Session lifecycle | `bridge.go` | Bridge: StartSession, ResumeSession, forwardEvents, InputRecoverer |
| LLM auto-retry | `llm_retry.go` | LLMRetryController: retryable patterns, per-session backoff |
| HTTP session API | `api.go` | GatewayAPI: ListSessions, GetSession, TerminateSession, CreateSession |
| Skills listing | `handler.go:720` | `handleSkillsList`: discovers skills via skills.Locator |
| Init handshake | `init.go` | 30s timeout, first frame must be "init" |
| Heartbeat | `heartbeat.go:12` | Missed ping tracking |

## KEY PATTERNS

**Hub goroutine (hub.go)**
- Run() loop: select on broadcast channel + ctx.Done()
- broadcast chan *EnvelopeWithConn (buffered, size from config)
- seqGen *SeqGen: per-session monotonic seq allocation
- droppable events use trySendBroadcast (non-blocking); guaranteed events use sendBroadcast (blocks on ctx.Done)

**Conn pumps (conn.go)**
- ReadPump: reads WS frames, dispatches via Handler, defers cleanup
- WritePump: exits on done/heartbeat stopped
- mu sync.Mutex protects closed flag

**Bridge lifecycle (bridge.go)**
- StartSession: create session → start worker → attach → forward events
- ResumeSession: resume existing → re-attach worker
- forwardEvents: goroutine reads worker.Recv() and broadcasts via hub
- Fresh start fallback: resume fails → create fresh worker → re-deliver last input via InputRecoverer
- Panic recovery in forwardEvents: recover → log → return "bridge panic"

**LLM auto-retry (llm_retry.go)**
- Default retryable patterns: 429, 529, 5xx, network errors, API rejections
- Per-session attempt tracking in `attempts map[string]int`
- Exponential backoff: initial 2s, max 60s
- Configurable via `config.AutoRetryConfig`
- Integrated into bridge forwardEvents loop

**GatewayAPI (api.go)**
- HTTP REST endpoints for session management alongside WebSocket
- Auth via `security.Authenticator` (API key + Bot ID header)
- ListSessions, GetSession, TerminateSession handlers

**Backpressure**
- message.delta/raw: non-blocking select, drop if broadcast full
- state/done/error: blocking send, never dropped

**Init handshake (init.go)**
- 30s timeout from first connection
- First frame must be type="init"
- InitError returned on validation failure

**Passthrough command dispatch (handler.go)**
- `WorkerCommander` interface: Compact/Clear/Rewind → HTTP REST to OCS
- Non-WorkerCommander: `/model`, `/effort`, `/commit` → fall through to `w.Input()`
- `/effort` and `/commit` return `NOT_SUPPORTED` for WorkerCommander path
- `sendCommandFeedback()` sends visible `message` AEP after success

**Fast reconnect guard (conn.go)**
- When WebSocket reconnects with live worker: skip `Transition(running)` if already `running`
- Avoids invalid `running→running` state machine error

## ANTI-PATTERNS
- ❌ Skip heartbeat stop on connection close
- ❌ Send on closed broadcast channel
- ❌ Handle input after session terminated without mutex
- ❌ Allow init after 30s timeout
- ❌ Skip panic recovery in bridge forwardEvents
- ❌ Use `sendBroadcast` for droppable events (`hub.go:411`) — full channel blocks Hub.Run and cascades backpressure; use `trySendBroadcast`
- ❌ Reduce writeCh buffer below 256 (`conn.go:123`) without background-tab-safe client batching
- ❌ Fall back to seq 1 on DB hydration error (`conn.go:566`) — duplicate seqs roll back collector batches
- ❌ Call `Close()` while holding `c.mu` (`conn.go:1070`) — `sync.Mutex` is not reentrant, self-deadlock
- ❌ Emit a terminal runtime event when the durable write failed (`bridge_forward.go:544`) — client must not see completion while the record is non-terminal
