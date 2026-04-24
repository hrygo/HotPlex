# AGENTS.md — internal/session

## OVERVIEW
Session lifecycle manager with SQLite persistence, deterministic session IDs (UUIDv5), state machine, single-writer write path, per-user quota+memory tracking, and background GC.

## STRUCTURE
| File | Purpose |
|------|---------|
| `manager.go` | Manager, managedSession, SessionInfo, state transitions (825 lines) |
| `store.go` | Store interface, SQLiteStore (371 lines) |
| `message_store.go` | MessageStore interface, SQLiteMessageStore (301 lines) |
| `key.go` | DeriveSessionKey (UUIDv5), PlatformContext, DerivePlatformSessionKey (100 lines) |
| `pool.go` | PoolManager: global + per-user quota + per-user memory tracking (154 lines) |
| `pgstore.go` | Postgres stub (ErrNotImplemented) |
| `queries.go` | embed.FS loader + stripComments for sql/ files |
| `stores.go` | Multi-store registry (SQLite/Postgres) |

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Session CRUD + state transitions | `manager.go:34` Manager, `manager.go:54` managedSession | Lock ordering: m.mu → ms.mu |
| SessionInfo struct definition | `manager.go:64` | ID, UserID, OwnerID, BotID, WorkerType, State, timestamps, Context, AllowedTools, WorkerSessionID, Platform, PlatformKey |
| Atomic state + input recording | `manager.go:309` TransitionWithInput | Check → transition → input all under ms.mu.Lock() |
| SESSION_BUSY hard reject | `manager.go:285` | RUNNING state rejects new input, no queuing |
| Deterministic session ID | `key.go:18` DeriveSessionKey | UUIDv5 from (ownerID, workerType, clientSessionID, workDir) |
| Platform session key | `key.go` PlatformContext | Platform-specific fields: Slack channel/thread, Feishu chat |
| SQLite persistence | `store.go:31` SQLiteStore | WAL mode, busy_timeout 5000ms |
| Message event log | `message_store.go:40` MessageStore | Single-writer goroutine, batch flush 50 items / 100ms |
| Pool quota + memory | `pool.go` PoolManager | MaxPoolSize global, MaxIdlePerUser per-user, 512MB/worker memory estimate |
| Postgres stub | `pgstore.go` | ErrNotImplemented — not production-ready |
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

### Deterministic Session IDs (key.go)
- `DeriveSessionKey(ownerID, workerType, clientSessionID, workDir)` → UUIDv5 (SHA-1)
- Same inputs always produce same session ID — cross-environment consistency
- `PlatformContext` adds platform fields (Slack channel/thread, Feishu chat) for `DerivePlatformSessionKey`
- Namespace UUID: `urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8`

### SQLite Single-Writer
- `MaxOpenConns=1` enforces serialized writes
- `SQLiteMessageStore` uses channel-based single-writer goroutine
- Batch flush: 50 items or 100ms interval via `writeC chan *writeReq` (cap 1024)
- Graceful shutdown: `closeC` signal → `closeWg` wait

### PoolManager Quotas
- `MaxPoolSize`: global concurrent session limit
- `MaxIdlePerUser`: per-user idle session limit
- `maxMemoryPerUser`: per-user memory budget (512MB per worker estimate)
- `userMemory map[string]int64`: tracks total estimated memory per user
- Acquire/Release: atomic counters with metrics

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
