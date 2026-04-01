# AGENTS.md — internal/session

## OVERVIEW
Session lifecycle manager with SQLite persistence, state machine, single-writer write path, and background GC.

## STRUCTURE
| File | Purpose |
|------|---------|
| `manager.go` | Manager, managedSession, SessionInfo, state transitions (688 lines) |
| `store.go` | Store/MessageStore interfaces, SQLiteStore, SQLiteMessageStore (525 lines) |
| `pool.go` | PoolManager: global + per-user quota enforcement |

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Session CRUD + state transitions | `manager.go:34` Manager, `manager.go:52` managedSession | Lock ordering: m.mu → ms.mu |
| SessionInfo struct definition | `manager.go:61` | ID, UserID, OwnerID, BotID, WorkerType, State, timestamps, Context, AllowedTools |
| Atomic state + input recording | `manager.go:309` TransitionWithInput | Check → transition → input all under ms.mu.Lock() |
| SESSION_BUSY hard reject | `manager.go:285` | RUNNING state rejects new input, no queuing |
| SQLite persistence | `store.go:31` SQLiteStore | WAL mode, busy_timeout 5000ms |
| Message event log | `store.go:327` SQLiteMessageStore | Single-writer goroutine, batch flush 50 items / 100ms |
| Pool quota management | `pool.go` PoolManager | MaxPoolSize global, MaxIdlePerUser per-user |
| GC goroutine lifecycle | `manager.go:34` gcStop/gcDone channels | Ticker-based expired scan |

## KEY PATTERNS

### State Machine
```
CREATED → RUNNING → IDLE → TERMINATED → DELETED
   ↑                    ↓
   └─── RESUME ←────────┘
```
Valid transitions defined in `pkg/events/events.go:261`.

### Concurrency
- `Manager.mu sync.RWMutex` protects sessions map
- `managedSession.mu sync.Mutex` protects per-session fields
- **Lock ordering**: Always `Manager.mu` → `managedSession.mu` to prevent deadlock
- `TransitionWithInput` holds `ms.mu.Lock()` for entire check → transition → input sequence

### SQLite Single-Writer
- `MaxOpenConns=1` enforces serialized writes
- `SQLiteMessageStore` uses channel-based single-writer goroutine
- Batch flush: 50 items or 100ms interval via `writeC chan *writeReq` (cap 1024)
- Graceful shutdown: `closeC` signal → `closeWg` wait

### GC Rules
- Idle timeout → TERMINATED
- Max lifetime → TERMINATED
- Zombie (LastIO timeout) → TERMINATED
- Retention expired → DELETE
- Parallel expired queries via errgroup

## ANTI-PATTERNS
- ❌ `Lock()` on Manager then `RLock()` on managedSession — deadlock risk
- ❌ Non-atomic state + input: check outside mutex, transition inside — race condition
- ❌ SESSION_BUSY with queuing — hard reject only, no pending queue
- ❌ `t.Fatal` in tests — use `testify/require`
- ❌ Missing WAL mode on SQLiteStore
- ❌ MaxOpenConns > 1 for SQLiteStore — breaks single-writer guarantee
- ❌ Forgetting `closeWg.Add(1)` before goroutine start
