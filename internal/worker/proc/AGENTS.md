# Process Lifecycle Package (proc)

## OVERVIEW
跨平台 worker 子进程生命周期管理：PGID 隔离（Unix）+ Job Object（Windows）双轨制，3 层分层终止（SIGTERM → 5s grace → SIGKILL），tree-kill 兜底逃逸后代，PID 文件追踪孤儿进程。

## STRUCTURE
```
proc/
  manager.go                  # Manager: Start/Terminate/Kill/Wait/ReadLine, stdio pipe, scanner
  pidfile.go                  # Tracker: PID 文件原子写（tmp+rename），orphan 扫描+清理
  pid_helpers.go              # TrackPID/UntrackPID 包级 helper（全局 tracker 透明代理）
  stderr_handler.go           # StderrHandler 接口 + DefaultStderrHandler（[LEVEL] 前缀解析）
  ansi.go                     # StripANSI() — stderr 行入库前剥离 ANSI 转义序列（drainStderr 统一调用）
  signal_unix.go              # POSIX: Setpgid/SIGTERM/SIGKILL/-pgid 信号原语
  signal_windows.go           # Win32: CREATE_NEW_PROCESS_GROUP/GenerateConsoleCtrlEvent
  tree_kill_unix.go           # ForceKillTree + findDescendants（递归扫逃逸 PGID 的孤儿）
  tree_kill_children_linux.go # /proc/<pid>/task/<pid>/children 快路径，/proc/*/stat 回退
  tree_kill_children_darwin.go# sysctl KERN_PROC_PID 枚举（maxScanPID=4096）
  tree_kill_windows.go        # no-op（Job Object 负责）
  manager_job_unix.go         # createJobAndAssign/closeJobHandle no-op
  manager_job_windows.go      # Job Object 句柄绑定到 Manager
  jobobject_windows.go        # CreateJobObject/AssignProcessToJob（KILL_ON_JOB_CLOSE）
  jobobject_other.go          # 非 Windows no-op stub
  memlimit_linux.go           # RLIMIT_AS 已禁用（modern JIT 需要大 VA 空间）
  memlimit_other.go           # 非 Linux no-op
  *_test.go                   # Manager/PIDfile/tree-kill 测试
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Start subprocess | `manager.go:97` Start() | `exec.CommandContext` + `SetSysProcAttr` + 3 个 os.Pipe |
| Graceful termination | `manager.go:209` Terminate() | `GracefulTerminate(pgid)` → 等 gracePeriod → `Kill()` |
| Force kill | `manager.go:250` Kill() | `closeJobHandle()` + `ForceKill(pgid)` + `ForceKillTree(pgid)`，然后后台 goroutine reap + `killWaitTimeout`(5s) 兜底（#838） |
| Read stdout line | `manager.go:366` ReadLine() | 单 goroutine 串行，**不持 mu**；panic-recover 抓 `bufio.ErrTooLong` |
| Drain stderr | `manager.go:408` drainStderr() | 后台 goroutine，stderr 句柄所有权转移 |
| Pipe FD cleanup | `manager.go:447` closeLocked() | 幂等，跳过 `os.ErrClosed` |
| PGID 隔离设置 | `signal_unix.go:13` | `SysProcAttr{Setpgid:true}` |
| Windows 进程组 | `signal_windows.go:15` | `CREATE_NEW_PROCESS_GROUP` |
| 3-layer 终止信号 | `signal_unix.go:18,29` | SIGTERM → SIGKILL，都用 `-pgid` 组播 |
| Grace 常量 | `signal_unix.go:41` | `DefaultGracePeriod = 5 * time.Second`（Win/Mac 等价副本） |
| Tree-kill 主流程 | `tree_kill_unix.go:17` | findDescendants → ForceKill(pgid) → 逐个 SIGKILL 孤儿 |
| Linux 子进程枚举 | `tree_kill_children_linux.go:15` | 快路径 `/proc/<pid>/task/<pid>/children`，回退 `/proc/*/stat` |
| Darwin 子进程枚举 | `tree_kill_children_darwin.go:14` | sysctl KERN_PROC_PID，capped 4096 |
| Windows Job Object | `jobobject_windows.go:15` | `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` |
| PID 文件原子写 | `pidfile.go:95` Write() | tmp 文件 + `os.Rename` |
| Orphan 清理 | `pidfile.go:177` CleanupOrphans() | 并发 cap（默认 3），minAge 过滤防误杀新进程 |
| PID 复用检测 | `pidfile.go:290` | `IsProcessGroupAlive(pgid)` 验证仍是 group leader |
| stderr 级别解析 | `stderr_handler.go:66` ParseLevelPrefix() | `[INFO]` 降级为 Debug（agent INFO 噪声大） |
| Scanner buffer | `manager.go:22` | 初始 64KB，硬上限 10MB |
| Scanner buffer 上限常量 | `manager.go:22-24` | `scannerInitSize` / `scannerMaxSize` |

## KEY PATTERNS

**双轨制平台隔离（build tags）**
- Unix：`Setpgid:true` 创建独立进程组，`kill(-pgid, sig)` 组播
- Windows：`CREATE_NEW_PROCESS_GROUP` + Job Object（`KILL_ON_JOB_CLOSE` 关闭句柄即杀全树）
- 每 platform-specific API 都有 `_unix`/`_windows` 或 `_linux`/`_darwin`/`_other` 对偶

**3-layer 终止（强制顺序）**
1. `GracefulTerminate(pgid)` → SIGTERM 整组（Unix）/ CTRL_BREAK（Win，best-effort）
2. 等 `gracePeriod`（默认 5s，`DefaultGracePeriod` 常量跨文件共享避免魔术数字）
3. `Kill()` → `closeJobHandle()` + `ForceKill(pgid)` + `ForceKillTree(pgid)`

**tree-kill 兜底**
- 标准 `kill(-pgid)` 抓不到逃逸 PGID 的后代（Codex MCP server 自建进程组）
- `findDescendants` BFS 递归扫子进程，过滤同 PGID 的，返回孤儿列表
- Linux 用 `/proc/<pid>/task/<pid>/children` 快路径，macOS 用 sysctl 枚举

**Manager 并发契约**
- `mu sync.Mutex` 显式字段，保护 cmd/scanner/pgid/started/exited/exitCode
- `waitOnce sync.Once` 保证 `cmd.Wait()` 只调一次（Terminate/Wait/Kill 三入口竞争）
- `waitErr` 由 `waitOnce.Do` 的 happens-before 边界同步，**不**由 `mu` 保护（#838 后 Kill 把 cmd.Wait 移到后台 goroutine）
- `ReadLine` 故意不持 mu（避免阻塞 Terminate/Kill），调用方必须单 goroutine 串行
- `Kill()` 异步：发 SIGKILL（持 mu）→ 后台 goroutine reap + `killWaitTimeout`(5s) 兜底（不持 mu）；timeout 分支标记 `exited=true` 保证 `IsRunning()` 正确

**PID 文件追踪**
- `InitTracker(dir, log)` 全局 atomic.Pointer，`Start` 时 `trackPID`，`Terminate/Kill/Wait` 时 `untrackPID`
- `CleanupOrphans` 启动时扫 stale 文件，minAge 过滤防误杀新 worker
- 双重校验：`IsProcessAlive` + `IsProcessGroupAlive`（防 PID 复用）

**stderr 处理可插拔**
- `StderrHandler` 接口：`Handle(line) (level, msg)`，空 msg = 抑制整行
- `StderrHandlerFactory` 每 session 新建实例（支持多行折叠状态）
- `DefaultStderrHandler` 解析 `[ERROR]/[WARN]/[DEBUG]/[INFO]` 前缀，`[INFO]` 降级 Debug
- **ANSI 剥离前置**：`drainStderr` 在调用 handler 前统一执行 `StripANSI(line)`，避免彩色输出（如 codex rmcp 的 `\x1b[31m`）污染日志；剥离后再做 `[LEVEL]` 解析，ANSI 包裹的 marker 仍能识别

## ANTI-PATTERNS
- ❌ 跳过 `Setpgid:true` / `CREATE_NEW_PROCESS_GROUP` — 子进程清理全靠组隔离
- ❌ 直接 `kill(pid)` 单进程 — 必须用 `-pgid` 组播，否则孤儿泄漏
- ❌ 跳过 `ForceKillTree` — Codex MCP server 会逃逸 PGID
- ❌ 并发调用 `ReadLine()` — 单 goroutine 所有权契约，扫描器不持锁
- ❌ Windows 上跳过 Job Object — `GenerateConsoleCtrlEvent` 对独立 console 不可靠
- ❌ 在 Linux 启用 `RLIMIT_AS` — modern JIT（Bun/Node）需要 ~70GB VA 空间，会立即崩
- ❌ 跳过 PID 文件 minAge 过滤 — 会误杀刚启动、PID 被复用的进程
