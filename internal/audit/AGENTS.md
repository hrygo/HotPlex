# Audit Event System

## OVERVIEW
Append-only user behavior audit log with SHA-256 hash chain integrity, zero-loss collector with WAL spill, pluggable AlertSink fan-out, SQLite/PostgreSQL dual-store, and background GC with verification. Independent module — emits from auth/credential boundaries, not per-request.

## STRUCTURE
```
audit/
  types.go      # Action constants (auth.login, session.create, message.inbound…), Event struct
  collector.go  # Collector: buffered channel → batch flush → fan-out to sinks + store
  spill.go      # WAL spill: disk-backed overflow when channel full (zero-loss guarantee)
  store.go      # Store: append-only insert, hash chain validation, query, dual SQLite/PG
  hash.go       # SHA-256 hash chain: Event.Hash = sha256(prevHash || eventJSON)
  gc.go         # Background GC: retention-based event pruning with verification
  verify.go     # ChainVerifier: integrity validation across entire hash chain
  sanitize.go   # sensitive field redaction for audit events
  sinks/
    sink.go     # AlertSink interface: fan-out to multiple sink backends
    log.go      # LogSink: writes to slog (structured JSON)
    webhook.go  # WebhookSink: HTTP POST to configured endpoints
    noop.go     # NoopSink: discard (testing/disabling)
    registry.go # SinkRegistry: register/instantiate named sinks
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Emit audit event | `collector.go` Emit() | Non-blocking send to buffered channel |
| Event action constants | `types.go` | ActionAuthLogin, ActionSessionCreate, ActionMessageInbound, etc. |
| Batch write + fan-out | `collector.go` batchLoop | batchSize events or batchInterval → sinks + store |
| WAL spill | `spill.go` SpillWriter | Disk-backed overflow when channel at capacity |
| Hash chain | `hash.go` ComputeHash | sha256(prevHash \|\| eventJSON) at insert time |
| Chain verification | `verify.go` ChainVerifier | Reads entire chain, validates hash continuity |
| Query audit log | `store.go` Query | By UserID, action, time range, with pagination |
| GC retention | `gc.go` RunGC | Pairs: Config.Audit.RetentionDays → delete expired |
| Add new sink | `sinks/` | Implement AlertSink, register in registry.go |
| PG advisory lock | `store.go` AuditAdvisoryLockKey | Serializes chain tail writes on PG (key 819207) |

## KEY PATTERNS

**Zero-loss collector (collector.go + spill.go)**
```
Emit(event) → buffered channel (cap: ChannelCap)
  ↓ if full (non-blocking send fails)
  spillWriter.Write(event) → WAL file (disk-backed overflow)
  ↓ batch goroutine
  batchLoop: collect BatchSize events or BatchInterval timer
    → flush batch: sinks.FanOut(events) + store.InsertBatch(events)
    → drain spill file (any overflow events written during flush)
```
Channel + WAL spill = zero event loss even under burst load.

**Hash chain integrity (hash.go + store.go)**
```
Event[N].Hash = sha256(Event[N-1].Hash || json.Marshal(Event[N]))
```
Computed at insert time in `store.go`. Verified end-to-end by `ChainVerifier`. Sentinel errors: `ErrCheckpointGap`, `ErrHashTailBroken`.

**Pluggable sinks (sinks/)**
- `AlertSink` interface: `Send(ctx, events)` + `HealthCheck()`
- `SinkRegistry`: name → constructor, instantiated at startup
- Built-in sinks: `log` (slog), `webhook` (HTTP POST), `noop` (discard)
- Sink timeout per batch: `SinkTimeout`

**Dual-store pattern (store.go)**
- SQLite: single-writer via `WriteMu.WithLock`, WAL mode, append-only
- PostgreSQL: advisory lock serialization (key 819207), same append-only schema
- Migration paired: any schema change must update both SQLite and PG migrations

**Background GC (gc.go)**
- Configurable retention via `Config.Audit.RetentionDays`
- Ticker-based scan, deletes expired events by creation time
- Chain verification runs after GC to confirm integrity

## ANTI-PATTERNS
- ❌ Emit audit events per-request — emit from auth/credential boundaries only
- ❌ Skip WAL spill config — zero-loss guarantee depends on disk-backed overflow
- ❌ Insert events without hash chain — append-only integrity requires prevHash linkage
- ❌ Query without pagination — audit tables grow unbounded; always use OFFSET/LIMIT
- ❌ Skip PG advisory lock — concurrent chain tail writes on PG require serialization
- ❌ Change Action constants without updating docs — actions are the public audit schema
- ❌ Block on Emit() — caller must use non-blocking send; backpressure handled by spill
