# cmd/hotplex — Gateway CLI Commands

## OVERVIEW
Cobra CLI entry points for the HotPlex gateway binary. Root command in main.go, subcommands in dedicated files. Gateway startup/DI in gateway_run.go, CLI lifecycle in gateway_cmd.go.

## STRUCTURE
```
cmd/hotplex/
  main.go            cobra root: register gateway, doctor, security, onboard, version subcommands
  gateway_run.go     gateway run: DI pipeline decomposed into initLogging, initOrphanCleanup, initStores, shutdownGateway
  gateway_cmd.go     gateway subcommand: start/stop/restart + daemon launcher; preserves config path across restarts
  routes.go          HTTP route registration: /ws (gateway), /admin/*, /health, /metrics
  admin_adapters.go  admin provider adapters: bridge concrete types to admin.Provider interfaces; botListerAdapter for multi-bot registry
  messaging_init.go  messaging adapter lifecycle: multi-bot init (platforms × bots loop), fillSlackExtras/fillFeishuExtras, startup validation, STT engine setup
  doctor.go          doctor subcommand: run DefaultRegistry checkers, render structured report
  security.go        security subcommand: path validation, env isolation checks
  onboard.go         onboard subcommand: launch interactive wizard, generate config
  config_cmd.go      config subcommand: validate, dump, show current config
  status.go          status subcommand: check gateway process status via PID file
  banner.go          startup banner: ASCII art + config summary + endpoint URLs
  dev.go             dev subcommand: start gateway + webchat concurrently
  pid.go             gateway state management: JSON PID file with config path + dev mode, discovery, stop, waitForProcessExit
  version.go         version subcommand: print version string
  banner_art.txt                  ASCII art banner content
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add new CLI subcommand | `main.go` | Create `<name>.go`, register via `rootCmd.AddCommand()` |
| Gateway DI pipeline | `gateway_run.go` | GatewayDeps struct: Hub, SessionManager, Bridge, LLMRetryController creation |
| Signal handler | `gateway_run.go` | SIGINT/SIGTERM → cancel ctx → ordered shutdown |
| Shutdown order | `gateway_run.go` | signal → cancel → tracing → hub → configWatcher → sessionMgr → HTTP server |
| HTTP route registration | `routes.go` | `/ws`, `/admin/*`, `/health`, `/metrics` — add new routes here |
| Messaging adapter wiring | `messaging_init.go` | `startMessagingAdapters()`: multi-bot loop (platforms × bots) → validate → Register → Configure → Start; `resolveSlackBot`/`resolveFeishuBot` per-bot config lookup |
| STT engine setup | `messaging_init.go` | STT engine creation per platform: local/persistent/fallback |
| Gateway flags | `serve.go` | `-config`, `-addr`, `-log-level` etc. → Viper binding |
| GatewayDeps struct | `serve.go` | Holds all runtime deps: Hub, SM, Bridge, ConfigStore, LLMRetryController |
| Startup banner | `banner.go` | ASCII art + resolved config summary (addr, admin, platforms) |
| PID management | `pid.go` | Write PID on start, remove on stop, detect stale PIDs |

## KEY PATTERNS

**GatewayDeps (DI container)**
```go
// serve.go
type GatewayDeps struct {
    Hub         *gateway.Hub
    SM          *session.Manager
    Bridge      *gateway.Bridge
    ConfigStore *config.ConfigStore
    LLMRetry    *gateway.LLMRetryController
    // ... admin, metrics, tracing
}
```

**Messaging init sequence**
1. Iterate `config.Messaging.Slack/Feishu` → skip if `!Enabled`
2. `messaging.New(platformType)` → adapter instance
3. `adapter.ConfigureWith(AdapterConfig)` → inject Hub/Handler/Bridge/Gate/Extras
4. `adapter.Start(ctx)` → connect to platform (Socket Mode / WS)
5. `msgBridge.SetAdapter(adapter)` → register adapter for botID resolution

**Route registration (routes.go)**
- `/ws` → Hub.HandleHTTP (WebSocket upgrade)
- `/admin/*` → AdminAPI.Mux() (scoped auth)
- `/health` → liveness probe
- `/metrics` → Prometheus handler

**Signal handling (gateway_run.go)**
- `notifyContext(ctx, SIGINT, SIGTERM)` → context cancellation
- Ordered shutdown via defer chain (reverse of init order)

## ANTI-PATTERNS
- ❌ Add DI wiring outside `gateway_run.go` — all startup logic centralized there
- ❌ Import adapter packages directly — use `messaging.New()` registry pattern
- ❌ Skip PID file management — needed for `status` and `stop` subcommands
- ❌ Register routes outside `routes.go` — single file for all HTTP routing
