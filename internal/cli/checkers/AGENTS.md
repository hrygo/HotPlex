# checkers — Diagnostic Checks for `hotplex doctor`

## OVERVIEW
25 self-registering diagnostic checks across 9 categories. Each check implements `cli.Checker`, returns a single `cli.Diagnostic`, and registers itself in `init()` via `cli.DefaultRegistry.Register`. Powers `hotplex doctor [--fix]`.

## STRUCTURE
```
checkers/
  config.go          # 5 config.* checks: exists, syntax, required, values, env_vars
  agentconfig.go     # 3 agent_config checks: suffix_deprecated, directory_structure, global_files
  dependencies.go    # 2 checks: worker_binary, sqlite_path
  environment.go     # 3 checks: go_version, os_arch, build_tools
  messaging.go       # 3 checks: slack_creds, feishu_creds, multi_bot_config
  runtime.go         # 4 checks: disk_space, port_available, orphan_pids, data_dir_writable
  security.go        # 3 checks: admin_token, file_permissions, env_in_git
  stt.go             # 1 check: stt.runtime (Python deps, ONNX model)
  tts.go             # 1 check: tts.runtime (Edge-TTS, ffmpeg)
  disk_unix.go       # syscall.Statfs for disk_space (build: darwin/linux)
  disk_windows.go    # GetDiskFreeSpaceEx for disk_space (build: windows)
  *_test.go          # table-driven, one per checker file
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Add a check | new `<name>.go` | Define type, implement `Name/Category/Check`, register in `init()` |
| Checker contract | `../checker.go:29` | `Check(ctx) Diagnostic` (singular, not a slice) |
| Set config path | `config.go:20` | `SetConfigPath()` gates all `config.*` checks; `loadConfig()` returns `(nil,nil)` when unset |
| Multi-bot credential check | `config.go:177` | `config.required` walks `bots[]` arrays (single-bot fallback at line 194) |
| Auto-fix callback | `config.go:70` | `FixFunc` field; `fixConfigExists`, `fixConfigValues`, `fixEnvVars` write back to disk |
| Agent-config layout | `agentconfig.go` | Detects deprecated `SOUL.slack.md` suffix; validates dir tree against `validConfigFiles` map |
| Disk space platform split | `disk_unix.go` / `disk_windows.go` | Build-tagged; both export the symbol `runtime.diskSpaceChecker` calls into |
| STT/TTS install fix | `stt.go` / `tts.go` | `FixFunc` shells out to `pip install` (uses `os/exec`, the one allowed binary path) |

## KEY PATTERNS

**Checker shape (config.go:36)**
```go
type configExistsChecker struct{}
func (c configExistsChecker) Name() string     { return "config.exists" }
func (c configExistsChecker) Category() string { return "config" }
func (c configExistsChecker) Check(ctx context.Context) cli.Diagnostic { ... }
func init() { cli.DefaultRegistry.Register(configExistsChecker{}) }
```

**One init, many registers (agentconfig.go:87)**
- A single `init()` may register multiple related checkers; keep co-located checkers in one `init`.

**Name / Category convention**
- Name: `<category>.<check>` dotted lowercase (e.g. `runtime.port_available`).
- Category: one of `config`, `agent_config`, `dependencies`, `environment`, `messaging`, `runtime`, `security`, `stt`, `tts`.

**FixFunc discipline**
- Set `FixHint` (human text) on every Warn/Fail; set `FixFunc` only when an idempotent auto-fix exists. `doctor --fix` invokes FixFunc and ignores the returned error path on success.

**Platform-specific helpers**
- Keep syscall code in `*_unix.go` / `*_windows.go`; never call `syscall.Statfs` directly in a checker body.

## ANTI-PATTERNS
- ❌ Return `[]Diagnostic` — the interface returns ONE `Diagnostic`. Emit multiple aspects by widening `Detail`/`Message`, or split into separate checkers.
- ❌ Register at runtime or in `TestMain` — `init()` only; tests inject state via unexported struct fields (e.g. `agentConfigSuffixChecker{dir}`).
- ❌ Read config without guarding `configPath == ""` — `loadConfig()` already returns `(nil,nil)`, prefer it over `config.Load` directly.
- ❌ Skip `FixHint` on Warn/Fail — every negative result needs a remediation pointer.
- ❌ Hardcode platform paths — use `config.HotplexHome()` and `filepath.Join`.
