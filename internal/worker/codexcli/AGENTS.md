# Codex CLI Worker Adapter (app-server singleton)

## OVERVIEW
`codex app-server` 单例进程适配器。所有 Codex CLI session 共享一个跨进程 stdio JSON-RPC 连接。`CodexAppServerManager` 管理引用计数 + 30min 空闲排空 + 崩溃检测；`AppServerWorker` 是轻量线程级适配器（thread/start, turn/start, thread/unsubscribe）。B/C 通道通过 `baseInstructions` 注入 thread/start。

## STRUCTURE
```
codexcli/
  config.go     # 全局 atomic.Pointer[CodexCLIConfig] + Singleton 生命周期（Init/Shutdown/Get）
  manager.go    # CodexAppServerManager: 单例进程 + JSON-RPC + 引用计数 + idle drain + 崩溃恢复
  worker.go     # AppServerWorker: thread 生命周期（Start/Resume/Input/Terminate/ResetContext）+ appConn
  commands.go   # ServerCommander: control request 路由（context_usage/mcp_*/compact）
  mapper.go     # Mapper: codex notification → AEP Envelope（25+ 通知类型）
  parser.go     # Parser: JSONRPC frame 解析（method + params 提取）
  types.go      # CodexEvent/Item/TokenUsage + JSON-RPC wire types + ThreadStart/TurnStart params
  worker_test.go# 集成测试
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| 注册 Worker | `worker.go:22` init() | 依赖 `GetSingleton()` 已初始化 |
| AppServerWorker 字段 | `worker.go:97` | embed `*base.BaseWorker`，manager/threadID/turnID/crashSub/doneCh |
| Start / Resume | `worker.go:180,430` | Acquire→startNewThread；Resume=Start 回退 `ErrFellBackToFreshStart` |
| Input 流程 | `worker.go:216` | DispatchMetadata→injectHistoryPrefix→turn/start |
| 历史前缀注入 | `worker.go:379` | 首输入注入 ConversationHistory（boundary 包裹） |
| Terminate/Kill | `worker.go:472,480` | 都调 `shutdown()`→`release()`，**不杀进程** |
| release() 回收 | `worker.go:517` | thread/unsubscribe + Unsubscribe + conn.Close + manager.Release |
| ResetContext | `worker.go:553` | IncResetGeneration→cleanupOldThread→startNewThread（同进程换 thread） |
| Wait | `worker.go:497` | select crashSub/doneCh；崩溃返回 `CrashExitCode()` |
| appState 状态机 | `worker.go:88` | New→Starting→Ready→Terminated |
| Singleton API | `config.go:35-49` | InitSingleton/ShutdownSingleton/GetSingleton（globalSingleton atomic.Pointer） |
| Manager 字段 | `manager.go:53` | refs/state/crashCh/crashExitCode/pending/subscribers/idleTimer/pgid |
| managerState | `manager.go:25` | Idle→Starting→Running→Stopped（crash 回 Idle） |
| Acquire / Release | `manager.go:119,150` | refs++ 懒启动；refs==0→startIdleDrainLocked |
| 进程启动 | `manager.go:315` startProcessLocked() | `codex app-server` + handshake + readNotifications + monitorProcess |
| initialize 握手 | `manager.go:372` | clientInfo + experimentalApi capability |
| JSON-RPC Call | `manager.go:199` | nextReqID + pending map + 30s defaultCallTimeout |
| Frame 分发 | `manager.go:471` dispatchFrame() | 单次 unmarshal：server request/response/notification 三分 |
| Server request | `manager.go:530` dispatchServerRequest() | approval 等 server 主动请求 |
| monitorProcess | `manager.go:940` | pm.Wait() 阻塞；exit→stateIdle + close(crashCh) + 重建 |
| Idle drain | `manager.go:993` startIdleDrainLocked() | 30min AfterFunc；到期 ForceKill+ForceKillTree |
| KillIfIdle | `manager.go:1048` | refs==0 跳过 SIGTERM 直接 ForceKill（避免 proc.mu 死锁） |
| Subscribe 路由 | `manager.go:168` | per-thread chan cap=256 |
| sendEnvelope 背压 | `manager.go:914` | delta 丢弃；critical 5s 超时；recover 防 send-on-closed |
| buildEnv | `manager.go:1066` | 委托 `base.BuildEnv` + `EnvBlocklist`（HOTPLEX_/CODEX_/CODEXCLI） |
| thread/start 参数 | `types.go:148` ThreadStartParams | model/cwd/sandbox/approvalPolicy + baseInstructions（B/C 通道） |
| 事件常量 | `types.go:78-101` | thread.started/turn.completed/item.* + 11 item types |
| Control request | `commands.go:17` | get_context_usage/mcp_status/mcp_refresh/mcp_oauth/compact |
| Notification → AEP | `mapper.go:32` MapNotification() | 25+ 方法：item.started/updated/completed, turn.*, delta, approval |

## KEY PATTERNS

**Singleton + 引用计数（manager.go）**：单一 `codex app-server` 进程跨所有 session 共享。`Acquire` refs++（首次触发 `startProcessLocked` 懒启动），`Release` refs--（==0 时启动 30min `idleTimer`，到期 `ForceKill(pgid)` + `ForceKillTree`，绕过 proc.mu 防死锁）。

**崩溃检测 + crashCh 隔离**：`monitorProcess` goroutine 阻塞 `pm.Wait()`；进程退出时 state→Idle + `close(crashCh)` + 重建新 crashCh。仅 `wasRunning && refs > 0` 视为崩溃（refs==0 是预期退出）。Worker 通过 `Wait()` 的 `<-crashSub` 分支感知，返回 `CrashExitCode()`。每个 Acquire→Release 生命周期返回当时的 crashCh，旧 Worker 不收过期信号。

**Worker ≠ Manager 职责边界**
| 操作 | AppServerWorker | CodexAppServerManager |
|------|-----------------|----------------------|
| fork/kill 进程 | ❌ | ✅ |
| thread 创建/销毁 | ✅（JSON-RPC thread/start） | ❌ |
| 引用计数 | ❌ | ✅ |
| 崩溃检测 | ❌ | ✅ |

`shutdown()`→`release()` 只 unsubscribe + Release ref，**绝不杀进程**。

**Idle drain / KillIfIdle 死锁规避（manager.go:993,1048）**：不用 `proc.Manager.Kill()`（会 acq proc.mu + cmd.Wait()），与 `monitorProcess` 的 `pm.Wait()`（已持 proc.mu via waitOnce）死锁。改用裸 `proc.ForceKill(pgid)` + `proc.ForceKillTree(pgid)`，monitorProcess 观察到退出后自然清理。

**Thread 生命周期**：`thread/start`（ThreadStartParams 含 baseInstructions）→ threadID；`turn/start`（每次 Input）；`thread/unsubscribe`（release 时）。同进程可创建多 thread（ResetContext 换新 thread 不重启进程）。

**Resume = Start 回退（worker.go:430）**：ephemeral thread 无持久化状态。新建/已终止 → resetLifecycleState + Start + `ErrFellBackToFreshStart`（Bridge 调账）；已 Ready → cleanupOldThread + startNewThread。

**Frame 单次解析（manager.go:471）**：单次 unmarshal 到 `JSONRPCFrame`，按 ID/Method/Error discriminator 三分：server request（ID+Method）/ response（ID only）/ notification（Method only）/ error-only（丢弃）。

**B/C 通道 + 历史注入**：`baseInstructions` 字段（types.go:161）映射 codex `base_instructions`，bridge `injectAgentConfig` 合并 B/C 通道。`pendingHistory` 首输入注入（boundary 包裹防混淆）。

**EnvBlocklist（types.go:104）**：`HOTPLEX_` / `CODEX_` / `CODEXCLI` 全剥离，通过 `base.BuildEnv` 7 层过滤。

## ANTI-PATTERNS
- ❌ 在 Worker.Terminate/Kill 调 `proc.Kill()` — 共享进程会被无辜 session 拖死
- ❌ 用 `proc.Manager.Kill()` 做 idle drain — 与 monitorProcess 的 Wait 死锁
- ❌ 跳过 `thread/unsubscribe` 直接 Unsubscribe — server 端订阅泄漏
- ❌ 假设 Resume 恢复历史 — ephemeral thread，靠 `pendingHistory` 首输入注入
- ❌ 并发写 stdin — 必须经 `writeMu`（writeFrame 内部已加锁）
- ❌ 修改 baseInstructions 不重置 thread — 已运行 thread 不感知；需 ResetContext 换新 thread
- ❌ 直接 close subscriber chan — `subsClosed atomic.Bool` 防 monitorProcess 双关闭
