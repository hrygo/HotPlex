# Shared Worker Base (base)

## OVERVIEW
所有 stdio worker 适配器（claudecode / opencodeserver）共享基座。`BaseWorker` 提供生命周期方法（Terminate/Kill/Wait/Health/LastIO）+ reset generation 计数；`Conn` 提供 NDJSON stdin SessionConn；`BuildEnv` 7 层安全构造子进程 env；`MetadataHandler` 统一三类控制响应分发。ACP 不复用 Conn（自建 acpConn）但仍 embed BaseWorker。

## STRUCTURE
```
base/
  worker.go             # BaseWorker: Terminate/Kill/Wait/Health/LastIO/Conn/resetGen
  conn.go               # Conn: stdin NDJSON SessionConn, WriteMu/StdinLocked, InputRecoverer
  env.go                # BuildEnv: 7 层 env 构造（blocklist + HOTPLEX_WORKER_ strip + session vars）
  metadata.go           # MetadataHandler 接口 + DispatchMetadata 三类控制响应路由
  pipeerr_unix.go       # IsDeadProcessError: EPIPE / ErrClosed
  pipeerr_windows.go    # IsDeadProcessError: ERROR_BROKEN_PIPE / ERROR_NO_DATA
  writeall_unix.go      # WriteAll: EAGAIN retry（macOS non-blocking pipe）
  writeall_windows.go   # WriteAll: 无 EAGAIN，纯 partial write 循环
  *_test.go             # Conn IO + env 构造 + worker lifecycle 测试
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Embed BaseWorker | `worker.go:30` | 适配器 embed `*base.BaseWorker`，`w.Mu` / `w.Proc` / `w.Log` 直接复用 |
| Lifecycle 入口 | `worker.go:100,110,120` | Terminate/Kill/Wait，委托 proc.Manager |
| Health 快照 | `worker.go:131` | PID/Running/Uptime/SessionID |
| LastIO 原子读写 | `worker.go:162,171` | `atomic.Int64` 存 unix nano |
| SetConn / SetConnLocked | `worker.go:176,184` | 后者调用方持锁 |
| Reset generation | `worker.go:198,203` | forwardEvents 用于区分 reset vs crash |
| Grace 常量 | `worker.go:26` | `GracefulShutdownTimeout = proc.DefaultGracePeriod`（5s） |
| Conn 创建 | `conn.go:29` | recvCh cap=256 |
| NDJSON Send | `conn.go:43` | 持 Mu 防 ControlHandler 写交错；EPIPE→WorkerError(Unavailable) |
| Stdin 原子访问 | `conn.go:128` StdinLocked() | 返回 stdin + 已锁 Mu，调用方解锁 |
| WriteMu 共享 | `conn.go:111` | ControlHandler 与 Send 共用同一 mutex |
| CloseInput EOF | `conn.go:151` | 关闭 stdin pipe，worker 处理完缓冲后退出 |
| InjectWithTimeout | `conn.go:87` | 2s 超时 + recover；Conn/acpConn/ocsConn 共享 |
| LastInput 崩溃恢复 | `conn.go:200` | 持 Mu 字符串字段（acpConn 用 atomic.Pointer） |
| Env 7 层构造 | `env.go:52` BuildEnv() | 见下 KEY PATTERNS |
| HOTPLEX_WORKER_ 剥离 | `env.go:20,79` | 仅此前缀剥离，其他 HOTPLEX_* 阻塞 |
| 动态 block 系统 secret | `env.go:108-112` | 剥离版存在时系统级同名变量自动屏蔽 |
| nested agent 剥离 | `env.go:137` | `security.StripNestedAgent` 移除 `CLAUDECODE=` |
| MetadataHandler 接口 | `metadata.go:8` | permission/question/elicitation 三类 |
| DispatchMetadata 路由 | `metadata.go:17` | Input metadata type-switch，命中即终止 |
| Pipe 错误检测 | `pipeerr_*.go` | Unix: EPIPE/ErrClosed；Win: ERROR_BROKEN_PIPE/NO_DATA |
| WriteAll EAGAIN retry | `writeall_unix.go` | macOS 非阻塞 pipe，`runtime.Gosched()` 让出 |

## KEY PATTERNS

**BaseWorker embedding（高耦合影响）**
- 每个 stdio 适配器 embed `*base.BaseWorker`，共享 `Mu` / `Proc` / `Log` / `Cfg` / `lastIO` / `resetGen`
- ACP 也 embed 但 **不复用 Conn**（acpConn 仅 up 方向，user input 走 client.Prompt）：`SetConnLocked(nil)` + 重写 `Conn()` 方法
- 修改 BaseWorker 字段 = 同步影响所有 4 个适配器

**withProc 模式（worker.go:62-97）**：三泛型 helper（`withProc`/`withProcCode`/`withProcResult`），锁内 snapshot Proc，锁外调用 fn，成功后锁内置 nil。消除重复 lock-snapshot-nil-clear，保证 Terminate/Kill/Wait 只执行一次。

**resetGen 解决 reset vs crash 竞态（worker.go:41-48）**：`IncResetGeneration()` 在 Terminate+Start 前递增；forwardEvents goroutine 启动时捕获 gen，recv channel 关闭后比较当前 gen；不一致 = 期间发生 reset，旧 forwardEvents 干净退出（不触发崩溃处理）。替代旧版 boolean flag 的竞态漏洞。

**Conn 并发契约**
- `Send` 持 `c.mu`，防 ControlHandler 同 fd 写交错；`StdinLocked()` 返回 Mu 已锁状态供原子 read-write-SetLastInput 序列；`WriteMu()` 暴露 Mu 给 ControlHandler 共享
- recvCh cap=256；`TrySend` 非阻塞；`Inject` 2s 超时（`InjectWithTimeout` 由 Conn/acpConn/ocsConn 共享）

**BuildEnv 7 层（env.go:52）优先级 low→high**
1. `os.Environ()` 经 blocklist 过滤
2. `HOTPLEX_WORKER_*` 前缀剥离注入（`HOTPLEX_WORKER_GITHUB_TOKEN`→`GITHUB_TOKEN`）
3. `HOTPLEX_SESSION_ID` / `HOTPLEX_WORKER_TYPE` 注入
4. 剥离变量覆盖系统同名（动态 block 系统 secret）
5. `session.Env` per-session 覆盖
6. `security.StripNestedAgent`（移除 `CLAUDECODE=` 防嵌套）
7. `session.ConfigEnv`（`worker.environment`）最高优先级

blocklist 规则：精确 key 进 `blockSet`；`_` 结尾进 `prefixKeys` 前缀匹配。

**MetadataHandler 统一分发**：Input 进入时先 `DispatchMetadata`，命中 `permission_response`/`question_response`/`elicitation_response` 即路由到 handler 并跳过正常 Input。ClaudeCode（stdin control）和 OCS（HTTP POST）共享此模式。

**跨平台 pipe 处理**：Unix `EPIPE`/`os.ErrClosed`，Windows `ERROR_BROKEN_PIPE`/`ERROR_NO_DATA`（避免 locale 字符串匹配）。`WriteAll` 在 macOS 非阻塞 pipe 上必须用裸 `syscall.Write` + `runtime.Gosched()` 重试 EAGAIN（Go stdlib `File.Write` 不重试）；Windows 无 EAGAIN，简化循环。

## ANTI-PATTERNS
- ❌ 修改 BaseWorker 字段不同步所有适配器 — 4 个适配器全受影响
- ❌ 直接读 `Conn.stdin` 不持 Mu — 与 ControlHandler 写交错
- ❌ 用 `Stdin()`（deprecated）— 返回的 `*os.File` 释放锁后无保护，改用 `StdinLocked()`
- ❌ 用 `os.Environ()` 直接传 worker — 必须经 `BuildEnv` 7 层过滤
- ❌ 在 BuildEnv 跳过 `StripNestedAgent` — `CLAUDECODE=` 会泄漏给子进程
- ❌ 假设 `HOTPLEX_*` 都该传给 worker — 仅 `HOTPLEX_WORKER_*` 前缀剥离，其他全是 gateway 内部
- ❌ 用 `os.ErrClosed` 字符串匹配判 pipe 错 — Windows 必须用 errno（locale-dependent）
- ❌ ACP 适配器强制用 base.Conn — acpConn 仅 up 方向，user input 走 client.Prompt
