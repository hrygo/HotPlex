# 单 Session 单 WebSocket 连接设计

**日期：** 2026-07-15  
**状态：** 已批准，待实施  
**范围：** `internal/gateway`、`pkg/events`、`webchat/lib`、`webchat/locales`、AEP 文档与客户端协议测试  
**关联：** WebChat 重复请求、响应错投与旧连接错误迁移 session 状态的问题

## 决策摘要

WebChat 的一个 session 在任意时刻只允许一个完成 AEP `init` 的 WebSocket
连接。已有存活连接时，Gateway 必须拒绝新的连接尝试；不采用“新连接接管旧
连接”、也不允许旧连接自然超时后继续收包。

本设计采用“首个成功绑定者保留，后来者拒绝”的模型：

1. Gateway 是唯一的正确性边界，不能依赖浏览器标签页协调。
2. 重复连接在完成认证和解析真实 session ID 后立即收到
   `SESSION_ALREADY_CONNECTED`，不会加入 Hub 路由、启动/恢复 Worker 或改变
   session 状态。
3. WebChat 将此错误呈现为明确的阻塞态：禁用发送与停止，但允许用户切换或
   新建会话，并提供人工“重试连接”。该状态不自动重连。
4. 平台消息订阅与 WebSocket owner 分离；“一个 WS”不能误删 Slack、飞书等
   同 session 的投递端点。

## 背景与根因

当前 `Hub.JoinSession` 仅把旧 `Conn` 从 `sessions[sessionID]` 的出站路由中
删除，并刻意不关闭旧连接。旧 `Conn` 仍留在 `h.conns`，其 `ReadPump` 继续把
input、control 和交互响应交给 `Handler`。因此可能出现：

```
旧 WS A 发 input → Worker 正常执行 → Hub 只向新 WS B 投递 message/done
```

用户在 A 所在页面看不到响应，但 event store 和 execution ledger 都显示该输入
已完成。旧连接稍后退出时还会无条件把 `RUNNING` session 迁为 `IDLE`，即使 B
仍持有并使用该 session。

另有两个结构性问题：

- HTTP Upgrade 与 `init` 期间使用同一 `sessions` 路由表，临时 URL session key
  可能残留。
- `sessions` 同时保存 `*Conn` 与 `pcEntry`。按 map 全量删除旧 subscriber 会影响
  平台消息投递，不能用它表达 WebSocket 的唯一所有权。

## 目标

1. 同一 session 同时最多一个已初始化的 WebChat WebSocket owner。
2. 被拒绝的连接不产生 Worker、副作用、session 状态变化或出站订阅。
3. 失去 owner 的旧连接不能再处理任何所有权敏感事件，也不能将 session 置为
   `IDLE`。
4. WebChat 对重复连接给出可理解、可恢复且无自动重试风暴的 UI。
5. 保留现有快速重连：客户端确认旧 owner 已释放后，可再次连接同一 session。
6. 不改变 Slack、飞书、Cron 等非 WebChat 投递语义。

## 非目标

- 不实现多个浏览器标签页共享同一实时会话。
- 不用 LocalStorage 或 BroadcastChannel 承担服务端互斥；它们最多作为后续体验
  优化，不能替代 Gateway 约束。
- 不让第二个连接强制接管或关闭第一个连接。
- 不新增排队、连接等待室或跨设备会话转移功能。
- 不改变 durable execution 的单活跃 execution 规则。

## 术语与不变量

### 术语

- **候选连接**：完成 WebSocket Upgrade 但尚未完成有效 AEP `init` 的连接；它不
  属于任何 session owner。
- **WS owner**：已通过认证、解析 session ID 并由 Gateway 成功登记的唯一
  WebChat `Conn`。
- **平台订阅者**：Slack/飞书等 `SessionWriter`，只接收投递事件，不参与 WebChat
  WS owner 互斥。

### 不变量

- 对任意 session ID，`webchatOwner[sessionID]` 至多包含一个 `*Conn`。
- 只有 owner 可把 input、control、worker command、permission/question/
  elicitation response 交给 `Handler`。
- 候选连接和被拒绝连接不得调用 `StartSession`、`ResumeSession`、
  `Worker.Input` 或 `SessionManager.Transition`。
- 只有“当前 owner 且释放成功”的连接关闭路径可以执行 WebChat 的
  `RUNNING → IDLE` 迁移。
- 一个 WebChat owner 的接入、释放和 owner 校验必须在同一 Hub 锁域内具备线性化
  语义；不得以“先检查、后登记”的两步逻辑实现。
- 平台订阅表与 WebChat owner 表相互独立。

## 方案选择

### 采用：首连接保留，后续连接拒绝

新连接在 `init` 解析出真实 session ID 后调用原子 `TryAcquireWebSocket`。若已有
owner，直接返回稳定的协议错误并关闭新连接。该方案严格满足单 session 单 WS，
没有接管窗口和重连抢占循环。

### 不采用：最新连接接管旧连接

此方案在连接交替时会短暂并存两个物理 WS，并且服务端关闭旧连接会触发浏览器
自动重连，导致两个标签页持续相互抢占。它与本规格的硬约束冲突。

### 不采用：只在 WebChat 做标签页协调

浏览器存储无法覆盖其他浏览器、设备或网络分区，也无法消除服务端并发竞态，
只能作为非正确性体验优化。

## 详细设计

### 1. Hub 的 WebChat owner 注册表

`Hub` 新增独立的 WebChat owner 表，概念模型如下：

```go
type webchatOwner struct {
    conn *Conn
    id   string // 每个 Conn 创建时生成的随机、不透明 ID
}

webchatOwners map[string]webchatOwner // session ID -> 唯一 WebChat owner
```

`sessions map[string]map[SessionWriter]bool` 继续作为**出站订阅表**，允许平台
订阅者存在；它不再承担 WebSocket 互斥语义。

新增内部方法：

```go
// TryAcquireWebChatOwner atomically registers conn if the session has no
// current WebChat owner. It returns false without mutating routes otherwise.
func (h *Hub) TryAcquireWebChatOwner(sessionID string, conn *Conn) bool

// IsWebChatOwner reports whether conn is still the unique owner.
func (h *Hub) IsWebChatOwner(sessionID string, conn *Conn) bool

// ReleaseWebChatOwner removes conn only when it is the current owner.
func (h *Hub) ReleaseWebChatOwner(sessionID string, conn *Conn) bool
```

这三个方法必须使用 `h.mu` 完成检查和更新。`Release` 返回 `true` 表示调用方仍是
owner；返回 `false` 时调用方不可改变 session 生命周期。

### 2. Init 与接入顺序

Upgrade 后只注册候选 `Conn` 以便清理资源，**不得**根据 URL 中的临时 session ID
调用 `JoinSession`。AEP `init` 的接入顺序固定为：

```
WebSocket Upgrade
  → 认证与 init 参数校验
  → 解析真实 session ID、workspace 与归属
  → TryAcquireWebChatOwner
  → JoinSession（仅此时加入 WebChat 出站路由）
  → sequence hydration
  → create/resume/fast reconnect Worker
  → init_ack
```

`TryAcquireWebChatOwner` 失败时，Gateway 直接向候选连接发送：

```json
{
  "event": {
    "type": "init_ack",
    "data": {
      "code": "SESSION_ALREADY_CONNECTED",
      "error": "session already has an active WebSocket connection"
    }
  }
}
```

随后关闭候选连接。失败路径不得进入 sequence hydration、worker 生命周期或普通
Hub 路由。

接入成功后，若后续 hydration 或 worker resume/start 失败，必须释放刚取得的
owner 和出站订阅，再向该候选连接回复原有 init 错误。`TryAcquire` 成功的前提是
当时没有先前 owner，因此失败时没有可恢复的旧 owner。

### 3. 入站 fence 与关闭路径

`Conn.ReadPump` 在完成 init 后，针对以下事件，在分配 seq 或调用 `Handler`
之前调用 `IsWebChatOwner`：

- `input`
- `control`
- `worker.command`
- `permission.response`
- `question.response`
- `elicitation.response`

校验失败时，直接向该 `Conn` 定向写入 `SESSION_ALREADY_CONNECTED` 错误并终止
ReadPump；不得经 `Hub.SendToSession` 发送，以免错误投递给真正 owner。AEP ping
可以被忽略或定向回复，但不能影响 session 状态或触发业务 Handler。

ReadPump 的 defer 顺序改为：

```
停止 heartbeat
  → ReleaseWebChatOwner(sessionID, conn)
  → 若释放成功且 session 仍为 RUNNING，迁移至 IDLE
  → 从 WebChat 出站订阅与 h.conns 清理
  → 关闭底层 WebSocket
```

因此，候选连接、拒绝连接和任何非 owner 的延迟关闭都不能把当前 session 置为
`IDLE`。

### 4. AEP 错误契约

在 `pkg/events` 中新增稳定错误码：

```go
ErrCodeSessionAlreadyConnected ErrorCode = "SESSION_ALREADY_CONNECTED"
```

它是 init 级的、不可自动重试的业务拒绝，不是 `SESSION_BUSY`：

- `SESSION_BUSY`：同一个 owner 内已有 execution，连接仍可用。
- `SESSION_ALREADY_CONNECTED`：当前 WS 不是该 session 的唯一连接，连接不可用。

同步更新 Go SDK、TypeScript/Python/Java 示例 SDK、AEP 参考文档及双向协议测试。
客户端收到该错误后必须禁用 reconnect；只有用户点击“重试连接”才可以创建新的
连接尝试。

### 5. WebChat UI 与 UX

`BrowserHotPlexClient` 将 `SESSION_ALREADY_CONNECTED` 分类为 fatal init error：

- 关闭当前候选 socket。
- `shouldReconnect = false`，取消 heartbeat 和所有重连 timer。
- 触发专用 `sessionAlreadyConnected` 事件，或以可区分的错误码触发现有 error
  事件；不触发 `reconnect_failed`。

`useHotPlexRuntime` 增加连接状态 `already_connected`，只由该错误进入和由成功
init 离开。该 fatal error 后随之而来的通用 `disconnected` 事件不得覆盖此状态。
状态下：

- Thread 中央显示阻塞卡片：
  “该会话已在其他标签页或设备打开。关闭原页面后再重试连接。”
- Composer、发送按钮和停止按钮禁用；输入框显示相同含义的禁用提示。
- 会话列表、创建会话、切换会话、退出登录与设置保持可用。
- 卡片提供“重试连接”主操作。该操作只发起一次显式 `connect(sessionId)`；成功
  后恢复正常交互，失败则留在阻塞态。不得建立固定间隔或指数退避的后台重试。
- 若用户切换到其他 session，旧 runtime 必须彻底 disconnect 并清除阻塞状态；
  新 session 按自己的连接结果独立渲染。

所有新增 UI 文案必须进入：

- `webchat/locales/zh-CN/chat.json`
- `webchat/locales/en/chat.json`

两份文件使用完全相同的键，不得在组件中硬编码文本。

### 6. 可观测性

每个 `Conn` 生成 `conn_id`，但不得记录认证凭证、输入正文或 payload hash。新增
结构化日志字段：`conn_id`、`session_id`、`remote`、`owner_conn_id`、`reason`。

新增 metrics（通过现有 `sync.Once` 访问器注册）：

| 指标 | 类型 | 含义 |
| --- | --- | --- |
| `gateway_webchat_session_owner_connections` | ObservableGauge | 当前拥有 WS owner 的 session 数 |
| `gateway_webchat_duplicate_connection_rejected_total` | Counter | 因已有 owner 被拒绝的 init 数 |
| `gateway_webchat_non_owner_ingress_rejected_total` | Counter | 非 owner 入站 fence 拒绝数 |
| `gateway_webchat_owner_release_not_current_total` | Counter | 旧连接尝试释放但已非 owner 的次数 |

在 trace `conn.init` 和 `conn.recv` 中加入 `conn_id`；后续能够关联输入来源、owner
和关闭原因。

## 测试矩阵

### Gateway 单元与集成测试

1. A 已成功 init 后 B 以同一 session init：B 收到
   `SESSION_ALREADY_CONNECTED`，A 保持连通。
2. 重复 init 不调用 `StartSession`、`ResumeSession`、`Worker.Input` 或
   session transition。
3. A 为 owner、B 被拒绝后，B 的 input/control/interaction 不进入 Handler。
4. A 关闭后 B 重试成功；B 是唯一 owner 且能够正常收发。
5. A 关闭与 B 接入并发循环：`go test -race` 下最终至多一个 owner，连接计数与
   路由表一致。
6. 旧/被拒绝 Conn 关闭时不触发 `RUNNING → IDLE`；真正 owner 关闭时才触发一次。
7. Upgrade 临时 key 到 init 派生 session ID 后，不遗留临时订阅。
8. `JoinSession`/WebChat owner 操作不移除同 session 的 `pcEntry` 平台订阅。
9. `SESSION_BUSY` 与 `SESSION_ALREADY_CONNECTED` 的错误和重试语义独立。

### WebChat 单元测试

1. Browser client 收到 init ack 的 `SESSION_ALREADY_CONNECTED` 后，不调度自动
   reconnect，socket 被关闭。
2. Runtime 进入 `already_connected` 时 composer 与 stop 禁用，阻塞卡片和重试
   操作可见。
3. 点击重试只创建一次连接；失败后不产生 timer 重试，成功后恢复 composer。
4. 切换 session 或组件卸载时，清除 retry handler、heartbeat、pending connection
   waiter 和阻塞状态。
5. 中英文 locale key 完整且一致。

### 验证命令

至少执行：

```bash
go test -race -count=1 ./internal/gateway
cd webchat && npm test
cd webchat && npm run lint
```

合入前执行仓库质量门禁及 AEP 双向协议测试；若修改 `pkg/events` 的错误码或 wire
contract，必须同步验证 SDK、示例与文档。

## 验收标准

- 两个浏览器标签页打开同一 session 时，只有第一个完成 init 的标签可聊天。
- 第二个标签立即显示“已在其他位置打开”的阻塞态，不产生自动重连、重复 input
  或 Worker 调用。
- 关闭第一个标签后，在第二个标签点击“重试连接”即可恢复聊天。
- 任一旧连接的延迟断开、ping 或残余事件不会让仍被 owner 持有的 session 变为
  `IDLE`，也不会把结果投递到错误页面。
- 同 session 的 Slack/飞书投递功能不因 WebChat 的连接互斥而丢失。
- 日志与 metrics 可以定位重复连接被拒绝的 session、连接 ID 和原因，但不泄露
  用户输入内容。

## 实施顺序

1. 重构 Hub：分离 WebChat owner 与平台订阅，补 owner 获取/释放/fence 测试。
2. 调整 Conn init、ReadPump 和 defer 关闭顺序，删除 Upgrade 阶段的预绑定。
3. 增加 AEP 错误码、SDK/示例/文档与协议测试。
4. 实现 Browser client 的 fatal error 分类与 Runtime `already_connected` 状态。
5. 实现 Thread 阻塞卡片、禁用态、显式重试与中英文文案。
6. 跑 race、WebChat 单测、lint 和完整质量门禁，验证双标签端到端场景。
