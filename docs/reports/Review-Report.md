# 架构实施覆盖率审查报告（HotPlex Gateway v1.0）

基于 `docs/Worker-Gateway-Design.md`、`docs/AEP-v1-Protocol.md` 等核心设计文档与当前 `internal/` 及 `pkg/` 目录下的源代码的交叉比对，系统的大部分主体架构（AEP Envelope、Hub 广播路由、基于 SQLite 的 Session 管理、认证中间件等）均已按照设计落地。

然而，在 **协议严谨性、资源清理的生命周期闭环、以及边缘态防护** 上，目前的实现存在几处显著的盲区和设计脱节，未能做到对设计文档的“完整覆盖”。

## 审查发现：架构与实现的主要偏差（Gaps）

### 1. 致命缺陷：Worker 进程内存与僵尸进程泄漏 (Process Leaks)
- **文档约定**：`docs/Worker-Gateway-Design.md` 的 `5.1` 和 `6.3 GC 策略` 明确指出，在 Session `IDLE` 超时转入 `TERMINATED` 或被 Admin API 置为 `DELETED` 时，应当**“清理 runtime/同时 SIGKILL runtime”**。
- **代码现状**：在 `internal/session/manager.go` 中的 `Delete()` 或 `gc()` routine 中，仅对数据库的状态进行了变更和 map 移除。它**并没有**调用 `pool.Release()`，也**没有**调用 `worker.Kill()` 结束底层的进程实体。
- **后果**：每次 Session 超时或被销毁后，其持有的 CLI 进程（如 `claude` 或 `opencode`）都会沦为孤儿在后台永远挂起，导致严重的内存和句柄泄漏。当前仅在网关停机的 `pool.Close()` 中才会清理进程。

### 2. AEP 协议违背：事件序列（Seq）丢失机制瘫痪
- **文档约定**：`AEP-v1-Protocol.md` 的 `4. Event Ordering` 表示，同一 session 内 event `seq` 字段**严格递增**（丢弃的 delta 不自增），终端客户端依据 Seq 来保证时序。
- **代码现状**：在 `internal/gateway/bridge.go` 中的 `forwardEvents` 内，Worker 产生的回复事件被强制赋值写死为 `env.Seq = 0`，随后抛入 `hub.SendToSession` 广播流；而 `hub.go` 在 `routeMessage` 向外分发时并未分配真实的 Seq（只有主动下发的系统消息使用 `SeqGen`）。
- **后果**：返回给 Client 的业务流数据全部都会是 `Seq: 0`，完全破坏了 AEP 协议的时序性和幂等重试校验。

### 3. UI 对账死结：Backpressure 静默丢包未弥补
- **文档约定**：`AEP-v1-Protocol.md` §9 The "UI 对账强制约束" 声明：若本轮出现过 `message.delta` 丢弃（通道积压），Gateway **必须**在下发 `done` 之前发送完整的 `message` 对象以兜底渲染。
- **代码现状**：在 `internal/gateway/hub.go` 的 `SendToSession` 中，对于 Droppable 事件确实触发了正常的 `select default` 缓冲区剔除机制并静默丢弃，但代码没有任何标志位追踪“当前 Turn 是否发生过 Drop”。
- **后果**：高频输出打满 WebSocket 缓冲区时，前端会永久缺失部分代码文本。没有全量 `message` 的补充下发，前端页面会发生渲染断层（字集残缺）。

### 4. 幽灵进程防护：Zombie IO Polling 完全未实现
- **文档约定**：`Worker-Gateway-Design.md` §5.3 生命周期规则要求防范“Zombie IO Polling”：必须检查 `RUNNING` 态进的 Worker 是否在超过指定时长内没有 `HealthChecker.LastIO()`，如果触发需强制 `TERMINATED` 防堵塞。
- **代码现状**：`session/manager.go` 中的 GC 仅依据 DB 中的 `expires_at` 进行粗粒度的 TTL 清理。未见任何轮询或检测 Worker 实时 IO 频次的守护协程，`LastIO()` 虽然在接口设计中有占位，但也未实际接引。
- **后果**：如果底层模型 API Hang 死但进程未崩溃，该 Worker 槽位将永久处于 `RUNNING` 锁死状态且无法接收新指令（SESSION_BUSY），直到整个 Session 触及 `max_lifetime`，这极大拉低了高并发情况下的 Session 槽周转率。

### 5. 协议模型闭环：控制事件 (Control Events) 类型缺失
- **文档约定**：AEP 协议将 `control.reconnect`、`control.session_invalid`、`control.throttle` 等 Server 端主动分发的控制面事件列为系统的核心控制原语。
- **代码现状**：`pkg/events/events.go` 常量集合中仅仅实现了 `PriorityControl` 作为优先级概念，但缺失了用于此功能的基建数据结构及对应的事件分类 `"control"` (Kind)。
- **后果**：目前 Gateway 是通过 Error 替代控制命令，或者在需要服务器主动拉断要求 Client 重连（例如网关滚动更新时）时无可用原语，将导致服务平滑切换退化为强制断连。

---

## 修复建议

为保证完全覆盖设计文档要求，接下来应当：
1. **注入 GC 钩子**：打通 `session.Manager` 与 `pool.Manager` 的联动。在 Session 转为 `TERMINATED` 时，显式调用 Hook 或抛出 Event 使 Pool 同步调用 `worker.Kill()` 并释放配额。
2. **Seq 下放**：修改 `hub.go` 的 `routeMessage` 或修改 `Bridge` 通道接收处，务必在最终调用 `json.Encode` 发向网络时将 `h.seqGen.Next()` 赋予每一条广播包。
3. **Drop 标志位**：在 `managedSession` 状态机或 `PoolEntry` 里面加入单个 Turn 的 `HasDropped` 原子开关；在 `Bridge` 转发遇到 `done` 且 `HasDropped == true` 时，阻塞性调用 Worker 的提取完整上下文的方法并推送一次 `events.Message`。
4. **补充 Zombie GC**：在现有的 Session 后台 GC 定时器内，补充对 active sessions 的 `LastIO()` 扫描。若超过 5 分钟，执行 Kill 与强行转态。
5. **完善 `pkg/events` 定义**：增加 `Control` 类型及其相关结构体封装。
