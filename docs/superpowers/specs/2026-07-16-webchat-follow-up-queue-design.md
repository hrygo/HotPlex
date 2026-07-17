# WebChat 可编辑后续消息队列设计

## 背景与目标

WebChat 目前将 composer 提交直接交给 `BrowserHotPlexClient.sendInput()`。当一个 turn 尚未结束时，assistant-ui external-store runtime 会禁用发送，同时 Browser client 只允许一个 `pendingInput`，Gateway 也用 active gate 保证同一 session 只有一个 active execution。

本设计在不修改 AEP wire contract、不并发调用 `Worker.Input`、不持久化未发送 prompt 的前提下，为每个 WebChat session 增加页面生命周期内的可编辑 FIFO 队列，并提供“立即发送”的 stop-and-send 语义。

## 方案选择

### 采用：页面级队列存储 + runtime 单航班消费

- `ChatContainer` 创建一个页面级 `FollowUpQueueStore`，其生命周期覆盖 session 切换，但不跨页面刷新。
- `useHotPlexRuntime` 订阅当前 session 的队列快照，负责 dispatch、ACK、terminal、重连和 stop 编排。
- `Thread` 只渲染队列并调用明确的 enqueue/edit/delete/retry/send-now 操作。
- Browser client 提供可等待 terminal 的 single-flight stop 操作，不再在发送 stop frame 时提前清除 `pendingInput`。

这一方案保持产品状态、传输状态和展示层各自独立，且所有竞态均可由纯队列状态测试和 Browser client 时序测试覆盖。

### 未采用：assistant-ui 内置 queue

当前使用的 external-store runtime 将 `capabilities.queue` 固定为 `false`。启用它需要升级或 fork 上游，并且仍不能表达 HotPlex 的 `delivered`、`unknown`、stop terminal 与 durable execution 语义。

### 未采用：Gateway durable queue

服务端队列会扩大到协议、调度、隐私与持久化工程，并可能保存尚未发送的 prompt，与本需求“页面生命周期内存队列”的边界冲突。

## 组件边界

### `FollowUpQueueStore`

新建纯 TypeScript 队列存储，按 `session_id` 隔离数据并提供订阅接口。`ChatContainer` 持有其实例，session 删除成功或乐观删除时显式清理相应队列。

队列项包含：

- `id`：页面本地唯一 ID。
- `text`：最终待 dispatch 的文本。
- `createdAt`：创建时间。
- `status`：`queued | sending | failed`。
- `errorKind` / `errorMessage`：失败原因；`unknown` 使用独立类型。
- `clientMessageId`：仅在真正 dispatch 后设置。

队列最大 20 条。达到上限时 enqueue 返回结构化错误，调用方不清空 composer。存储不访问 `localStorage`、网络、日志或指标。

`failed` 项保留原文并阻塞 FIFO。只有用户点击“重试”后才转回 `queued`，因此 `unknown`、断线或 ambiguous delivery 不会自动重投。

### Runtime drain 状态机

runtime 为当前 session 维护一个 single-flight drain 和一个 active queue item 关联：

1. 仅在 client 已连接、session 非 running/stopping、没有 active queue item、队首为 `queued` 时开始 dispatch。
2. dispatch 前原子地把队首改为 `sending`，随后才创建 `client_message_id` 和 optimistic user/assistant 消息。
3. `input.ack=delivered` 后从可见队列移除该项，但 active 关联保留到 turn terminal，防止下一项过早发送。
4. `done` 清除 active 关联，并仅触发一次下一轮 drain。
5. `failed`、`unknown`、发送异常或断线会把尚未确认 delivered 的 active 项改为 `failed`，保留原文；不会自动 drain 越过它。
6. 重复 ACK、重复 terminal、重连事件和 React effect 重建均通过 queue item ID、client message ID 与 single-flight guard 幂等处理。

普通 composer 提交在 session 空闲时沿用直接 dispatch；running 或 stopping 时只 enqueue，不创建 optimistic message。只有真正 dispatch 时消息才进入 thread。

### “立即发送”

点击任意未发送项时：

1. 把该项移到队首，其余项保持相对顺序。
2. 若当前 turn 正在运行，调用 Browser client 的 `stopCurrentTurn()`。
3. `stopCurrentTurn()` 对连续调用复用同一 Promise，并等待 `done`；自然完成与 `stopped_by_user` 都是合法 terminal。
4. terminal 到达后由统一 drain 发送队首，不在按钮回调中直接发送，避免双 dispatch。
5. stop 超时、断线或失败时不发送队首，项目保留并显示可重试错误。

普通停止按钮也使用同一 stop waiter，但不改变队列顺序。发送 stop frame 不再提前清除 Browser client 的 `pendingInput`；它由 `done`、明确 error 或 disconnect 统一结算。

## UI 与可访问性

队列面板位于消息列表与 composer 之间，保持 HotPlex 现有工业化、紧凑的视觉语言：编号体现 FIFO，金色表示排队，珊瑚色强调“立即发送”，失败态使用可辨识的边框与文字而不只依赖颜色。

每项支持展开/收起、编辑、保存、取消、删除、立即发送；失败项额外显示“重试”。`sending` 项禁用所有破坏性操作。编辑器支持 Escape 取消、明确的 label、focus ring 与屏幕阅读器状态。所有新增文案和 aria-label 同步加入中英文 `chat.json`，并保持 key 完全一致。

running/stopping 时 Enter 或发送按钮调用 enqueue；达到 20 条时 composer 草稿保持不变并显示双语错误。Shift+Enter 仍插入换行。

## Handler 防御性变更

同一 PR 按用户要求纳入 `internal/gateway/handler.go` 的空白 input 防御：在命令分发与 Worker 投递前忽略纯空白内容，并添加表驱动回归测试。该变更作为独立原子 commit，避免与 #902 的前端实现混在同一提交中。

## 测试策略

- 队列存储 Vitest：FIFO、编辑/取消、删除、session 隔离、20 条上限、move-to-front、状态 CAS、failed 明确重试、session 清理。
- Runtime Vitest：正常 done 后逐条 drain、delivered 前失败回滚、unknown 不重投、重复 ACK/terminal、重连与 stop/done 竞态。
- Browser client Vitest：stop single-flight、自然 done、`stopped_by_user`、超时/断线、stop 不提前释放 pending input、terminal 后可发送下一条。
- 组件/E2E：运行中连续提交、队列可视化、编辑后发送、立即发送、键盘与 aria、溢出保留草稿。
- Gateway Go 测试：空白 input 不触发命令或 Worker 投递，非空输入行为不变，并运行相关 `-race`。
- 最终门禁：WebChat `pnpm test`、`pnpm build`、相关 Playwright、Go 相关测试与仓库 `make check`。

## 非目标与不变量

- 不跨刷新、设备或 Gateway 实例保存队列。
- 不向 execution store、eventstore、日志或指标写入排队 prompt。
- 不允许同一 session 同时存在两个 active execution。
- 不引入 Worker 原生 mid-turn steering。
- 不修改 Slack、飞书、Cron 入站行为。
- 不实现拖拽排序、优先级调度或批量编辑。
