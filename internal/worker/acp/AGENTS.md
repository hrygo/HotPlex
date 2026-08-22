# ACP Worker Adapter (Agent Client Protocol)

## OVERVIEW
通用 ACP v1 适配器，stdio 传输 JSON-RPC 2.0 over NDJSON。支持任何 ACP 兼容 agent（默认 `hermes acp`）。完整生命周期：Initialize 握手 → NewSession/LoadSession/ForkSession → session/prompt → session/cancel。B/C 通道 system prompt 作为首条 user input 前缀注入（ACP v1 无原生 system prompt）。

## STRUCTURE
```
acp/
  worker.go           # Worker: Start/Input/Resume/Terminate/ResetContext, lifecycle 编排, server request 路由
  client.go           # ACPClient: JSON-RPC 调用 + readLoop, Initialize/NewSession/LoadSession/ForkSession/Prompt
  codec.go            # NDJSON 编解码: JSONRPCRequest/Response/Notification/Error, WriteMessage/ReadMessage
  mapper.go           # ACPMapper: notification → AEP Envelope, tool_call/plan/usage/permission 映射
  conn.go             # acpConn: SessionConn 实现（仅 up 方向），TrySend 背压感知, InputRecoverer
  trace.go            # TraceWriter: JSONL 协议级调试 trace（acp.debug:true 启用，50MB rotation）
  stderr_handler.go   # acpStderrHandler: system prompt/traceback/XML config 折叠（per-session 状态）
  *_test.go           # 单元测试 + bench + phase2/phase3 集成测试
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Initialize 握手 | `client.go:60` Initialize() | 30s timeout，协商 protocolVersion + agentInfo |
| NewSession / LoadSession / ForkSession | `client.go:80,86,180` | LoadSession 需 capability，Fork 用于 AC-FR08-01 |
| Prompt / Cancel | `client.go:99,121` | Prompt 30min 阻塞；Cancel 2-3s 短超时 |
| JSON-RPC call + ID 兜底 | `client.go:272,340` | nextID 原子；`alternateIDKey` 兼容 `"1"` vs `1` |
| 消息分发 readLoop | `client.go:214` | Response→pending / Notification→NotificationCh / Request→RequestCh |
| NDJSON 编解码 | `codec.go:70,89` | WriteMessage 独立分配 line slice；ReadMessage discriminator 分类 |
| Scanner buffer | `codec.go:56` | 初始 64KB，硬上限 10MB |
| Start 编排 | `worker.go:236` Start() | 字段→proc→client→readLoop→Initialize→session→mapper→conn |
| Session 类型决策 | `worker.go:372` | Fork→fork; loadSession cap→Load; 否则 NewSession |
| 首输入注入（system prompt + JSON Schema） | `worker.go:478,482` | `[SYSTEM INSTRUCTIONS]` 前缀，CAS 保证只注入一次 |
| Prompt 错误分类 | `worker.go:509` | `isFatalRPCError` → WorkerError(Unavailable) 触发崩溃恢复 |
| Terminate 流程 | `worker.go:555` | trace→cancel→conn.Close→proc.Close（解阻塞 Scan）→client.Cancel→base.Terminate |
| ResetContext | `worker.go:683` | 同进程新建 session（PID 不变，acpSessionID 变） |
| Server request 路由 | `worker.go:998` handleServerRequest() | `session/request_permission` + 其他 → Raw 转发 |
| 自动批准 | `worker.go:1008` | `autoApprove` atomic.Bool，默认 true（sandbox 无人工审批） |
| 权限响应 | `worker.go:839` HandlePermissionResponse() | pendingPerm map → RespondRequest |
| Notification → AEP | `mapper.go:81` MapNotification() | session/update 分发到 text/thought/tool/plan/mode/usage |
| Permission request 映射 | `mapper.go:404` MapPermissionRequest() | PermissionMapResult 含 allowed/denied outcome |
| TrySend 背压 | `conn.go:59` | droppable（message.delta/raw）丢弃；critical 阻塞 5s |
| InputRecoverer | `conn.go:130` LastInput() | atomic.Pointer[string]，崩溃恢复重投递 |
| Trace 启用 / rotation | `worker.go:344` / `trace.go:86` | `acp.debug:true` → NewTraceWriter，50MB rotation |

## KEY PATTERNS

**JSON-RPC 2.0 三类消息（codec.go）**
- `JSONRPCRequest`: id + method（client 发起或 server 发起如 request_permission）
- `JSONRPCResponse`: id + result/error
- `JSONRPCNotification`: 无 id，有 method（session/update 主力）
- 单次 unmarshal 用 discriminator 字段（`ID`/`Method` 是否为 nil）分类

**ACP 会话状态机（worker.go:372）**
```
首次启动：             → NewSession
WorkerSessionID 存在：ForkSession→Fork；loadSession cap→Load；否则 NewSession
失败回退：             → NewSession + historyLost=true（发 HISTORY_LOST error 给客户端）
```

**双阶段超时**：Initialize 30s handshake；NewSession/LoadSession 独立 30s（不与 handshake 共享预算）

**Prompt 错误双层分类（worker.go:509）**
- `JSONRPCError` + `isFatalRPCError`（session not found/expired/invalid）→ `WorkerError{Kind: Unavailable}` 触发 Bridge 崩溃恢复
- 业务错误（rate limit, permission denied）→ nil（worker 继续）

**通知 drain 协议（worker.go:526）**
- Prompt 返回前必须 drain 缓冲通知，否则 Input 与 readLoop 竞争 NotificationCh
- 双 channel 信号：`drainCh` 触发 readLoop 排空，`drainDoneCh` 回执；readLoop 是 NotificationCh 唯一消费者

**Terminate 关键顺序**
- `proc.Close()` 必须早于 base.Terminate：关闭 stdout pipe 解除 `Scan()` 阻塞（ctx cancel 不中断 in-progress Scan）
- `client.Cancel` 2s 短超时，不侵蚀 SIGTERM grace period

**SystemPrompt/JSONSchema 注入**
- ACP v1 无原生 system prompt；完整 AgentConfig 不得拼入 user input
- 首条普通输入只附加固定、非私密的兼容规则；显式 Skill invocation 保持原始 slash command
- `CompareAndSwap(false, true)` 保证兼容规则只注入一次；ResetContext 后重置

**Capability 协商（worker.go:649）**：缺省 true（未声明视为支持），显式 false 才否定；当前仅 `loadSession` 受控

**acpConn 仅 up 方向**：`Send()` 返回 `ErrNotImplemented`（用户输入走 client.Prompt）；`Recv()` 返回 readLoop → TrySend 的 recvCh；实现 `InputRecoverer`

**Stderr 折叠（per-session 状态）**：12KB+ system prompt echo → 单行 Debug；Python traceback → 末行 Error；XML config → Debug；安全阀 256 行/32KB 强制 flush

## ANTI-PATTERNS
- ❌ 直接读 `client.NotificationCh` — readLoop 是唯一消费者，Input 必须走 drain 协议
- ❌ 跳过 `proc.Close()` 直接 base.Terminate — Scan() 不会因 ctx cancel 解阻塞
- ❌ 假设 agent ID 格式 — 用 `alternateIDKey` 兜底 `"1"` vs `1` 差异
- ❌ 跳过 capability 检查直接 LoadSession — 不支持的 agent 会报错
- ❌ Propagate nested agent env — `acpEnvBlocklist` 必须通过 `base.BuildEnv` 强制剥离
- ❌ 在 acpConn.Send 实现真实写入 — 用户输入只能走 `client.Prompt`，acpConn 只负责 up 方向
