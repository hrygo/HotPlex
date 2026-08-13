# cron — Out-of-Process Client for `hotplex cron`

## OVERVIEW
Package `croncli` is the persistence + control layer used by the `hotplex cron` cobra subcommands (`create`/`update`/`delete`/`get`/`list`/`trigger`/`history`, wired in `cmd/hotplex/cron_*.go`). It opens the same store the running gateway uses, mutates jobs, then pokes the gateway via SIGHUP or the admin HTTP API so the in-process scheduler picks up changes. All job/schedule types come from parent package `internal/cron/`.

## STRUCTURE
```
cron/
  client.go          # 458 lines: OpenStore, ResolveJob, ParseSchedule, PrepareJobForCreate, TriggerViaAdmin, NotifyGateway, formatters
  client_test.go     # ParseSchedule + PrepareJobForCreate tests
  signal_unix.go     # sendReloadSignal → syscall.Kill(SIGHUP)   (build: darwin/linux)
  signal_windows.go  # sendReloadSignal → error (Windows has no SIGHUP) (build: windows)
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Open store (sqlite or postgres) | `client.go:37` | `OpenStore`: `dbutil.ParseDialect` → run session migrations → construct `cron.NewSQLiteStore` or `cron.NewPGStore`; returns cleanup func |
| Resolve id-or-name | `client.go:71` | `ResolveJob`: try `Get(id)` then `GetByName` |
| Parse `--schedule` | `client.go:84` | `ParseSchedule`: `cron:` / `every:` / `at:`; `at:+1h30m` relative form clamped to `[1m, 72h]` |
| Build a job from flags | `client.go:192` | `PrepareJobForCreate`: validates bot name, resolves platform, sets defaults (`MaxRuns=10`, `ExpiresAt=+24h` for recurring), assigns ID + initial `NextRunAtMs` |
| Platform routing resolution | `client.go:158` | `resolvePlatform`: CLI flags > `GATEWAY_PLATFORM`/`GATEWAY_CHANNEL_ID`/`GATEWAY_THREAD_ID` env > default `"cron"` (no delivery) |
| Enforce max jobs | `client.go:261` | `CheckMaxJobs`: reads `cfg.Cron.MaxJobs` (default 50), skips on config/store errors |
| Trigger via admin API | `client.go:281` | `TriggerViaAdmin`: POST `/admin/cron/jobs/{id}/run` with `Bearer` admin token; reads gateway's actual config path from PID file |
| History query | `client.go:330` | `QueryHistory`: `eventstore.TurnQuerier.QueryTurnStats` keyed by `job.SessionKey()` |
| Notify gateway of changes | `client.go:340` | `NotifyGateway`: read PID state → `sendReloadSignal(pid)`; no-op when gateway down |
| Formatting helpers | `client.go:419+` | `FormatSchedule`, `FormatTimeMs`, `FormatDurationMs`, `FormatCost` (USD, 4dp) |

## KEY PATTERNS

**Type re-export (client.go:31)**
```go
type (
    Store   = cron.Store
    CronJob = cron.CronJob
)
```
Callers import only `croncli`; the parent `internal/cron` stays the single source of job semantics.

**Schedule kinds (mirrors `internal/cron.ScheduleKind`)**
- `cron:<expr>` — 5-field cron expression
- `every:<duration>` — minimum 1 minute (`parseDurationMs` rejects `<60s`)
- `at:<RFC3339>` or `at:+<dur>` — one-shot; relative clamped to 72h

**Two ways to fire a job**
- `TriggerViaAdmin` — HTTP POST; works while gateway runs, returns `202`/`404`/`503`.
- `NotifyGateway` — SIGHUP reload after create/update/delete; CLI calls both as needed.

**Config path reconciliation**
- When `--config` is unset, `TriggerViaAdmin`/`gatewayConfigPath` consult the PID file to load the SAME `.env` the running gateway uses — avoids picking up a stale dev config. Callers translate "flag not passed" (`cmd.Flags().Changed("config") == false`) to an empty string; an explicitly passed path is never replaced.

**Safe env loading**
- `loadEnvFile` mirrors the slack package: skip protected keys, never overwrite existing env.

## ANTI-PATTERNS
- ❌ Re-implement schedule parsing or job validation — delegate to `internal/cron` (`cron.ValidateJob`, `cron.NextRun`, `cron.GenerateJobID`).
- ❌ Write to the store without calling `NotifyGateway` afterward — the running scheduler won't see the change until reload.
- ❌ Assume SIGHUP works on Windows — `signal_windows.go` returns an error; surface it and tell users to restart.
- ❌ Trust `--config` blindly when the gateway is running — prefer `gatewayConfigPath()` from the PID file.
- ❌ Construct platform keys ad hoc — use `resolvePlatform` so env-derived baseline + CLI override compose correctly.
