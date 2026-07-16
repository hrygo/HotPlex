# WebChat 连接交接设计

## 结论

在 WebChat 浏览器客户端引入按 session 串行化的连接建立与关闭交接机制，消除同一标签页因重连、React 组件重新挂载或连接调用重入而短暂创建两条 WebSocket 的竞态。Gateway 现有“每个 session 只允许一个直接 WebSocket owner”的约束保持不变，真实的多标签页冲突仍返回 `SESSION_ALREADY_CONNECTED`。

## 问题

`BrowserHotPlexClient._doConnect` 在发现已有 socket 时会发起关闭，然后立即创建新 socket。WebSocket 关闭握手与 Gateway `ReadPump` 释放 `webchatOwners` 都是异步过程，因此新连接可能在旧 owner 尚未释放时发送 init，被 Gateway 正确地判定为第二个连接。

React 交互事件只会更新消息状态，不会直接重新挂载 `ChatInterface`。重新挂载仅发生在 session key、workspace recovery epoch 或父级条件分支发生变化，以及开发模式 Strict Mode/Fast Refresh 等场景。不过这些场景与自动重连、手动重试或连接调用重入都可能触发同一个 close/open 竞态。Worker 的 permission、question 或 elicitation 请求往往是竞态后的第一个显著事件，因此用户会误以为交互请求触发了连接冲突。

Gateway 还会对非 owner 的 permission、question 和 elicitation 响应返回同一个 `SESSION_ALREADY_CONNECTED`。因此修复必须同时保证：

1. 新连接不会抢在本标签页旧连接释放前 init；
2. 交互响应始终从当前连接 owner 发出；
3. 真实的另一个标签页不能借本地重试机制绕过 owner 限制。

## 目标

- 同一浏览器 JavaScript realm 内，同一 session 同时最多进行一次连接建立。
- client cleanup 后，新 client 等待旧 socket 完成关闭交接再发送 init。
- 本地交接导致的短暂 `SESSION_ALREADY_CONNECTED` 最多内部重试一次，不展示冲突弹窗。
- 无本地交接证据的 `SESSION_ALREADY_CONNECTED` 继续作为 fatal error 展示。
- permission、question、elicitation 的请求、响应与 Worker 权威 ACK 语义不变。
- 不修改 AEP wire contract，不放宽 Gateway owner fence。

## 非目标

- 不允许新标签页自动接管另一个标签页的 session。
- 不引入 Gateway owner lease 超时或同用户自动接管。
- 不把 WebSocket client 提升为长期全局连接池。
- 不改变 session、workspace 或 Worker 生命周期。

## 方案比较

### 方案 A：前端 single-flight 与 close barrier（采用）

客户端在模块内维护按 session 隔离的关闭交接状态。同一实例的重复 `connect()` 共享连接 Promise；跨实例的相同 session 连接等待前一个实例登记的 close barrier。变更局限在浏览器连接生命周期，保留现有协议和服务端安全边界。

### 方案 B：全局 WebSocket 连接池

组件重新挂载时复用同一 `BrowserHotPlexClient`。它能避免大部分重建，但必须增加订阅迁移、引用计数、延迟销毁和挂载空窗事件缓存，生命周期复杂度明显更高。

### 方案 C：Gateway logical client ID 接管

在 init 中增加浏览器逻辑实例标识，允许同一逻辑实例的新连接替换旧 owner。该方案需要修改 AEP、SDK、安全模型及企业 WS 行为，超出本缺陷的最小修复范围。

## 详细设计

### 1. 按 session 的连接交接注册表

在浏览器 client 模块中增加进程内注册表，key 为已知 session ID，value 只保存连接交接所需的短生命周期状态：

- 当前进行中的 connect Promise；
- 最近一次由本 realm 发起的 close barrier；
- barrier 对应的 socket/连接代际；
- 本次连接是否具备一次本地交接重试资格。

注册表仅存在于当前标签页的 JavaScript realm。不同标签页不共享该状态，因此第二个标签页仍会被 Gateway 拒绝。

### 2. Single-flight connect

`connect()` 与自动重连统一进入一个串行入口：

1. 若当前实例已经连接到目标 session，直接返回最近一次成功 init 结果，不创建 socket；
2. 若相同目标已有进行中的 connect，返回同一个 Promise；
3. 清理尚未执行的自动重连 timer，避免手动连接与 timer 同时进入；
4. 等待目标 session 的前置 close barrier；
5. 创建唯一的新 socket 并发送 init；
6. 成功或失败后只清理属于当前连接代际的注册状态。

所有异步回调使用连接代际检查，旧 socket 的 message、close 或 error 不能修改新 socket 状态。

### 3. Close barrier

`disconnect()` 仍可由 React cleanup 同步调用，但会在关闭 socket 前为当前 session 登记 barrier。barrier 在以下任一条件满足时完成：

- 原生 socket 发出 `close`；
- socket 在登记时已经处于 `CLOSED`；
- 关闭握手超过有界超时。

超时不能直接把普通连接冲突伪装成成功。超时后的新连接可以继续尝试，但只有带本地 barrier 证据的连接才拥有一次内部冲突重试资格。

### 4. 本地交接冲突重试

若 init 收到 `SESSION_ALREADY_CONNECTED`：

- 没有本地 close barrier/交接代际：保持现有 fatal 行为，进入 `already_connected`；
- 有本地交接证据且尚未重试：等待一个有界退避后重新建立一次连接，不向 Runtime 发出 `sessionAlreadyConnected`；
- 重试仍冲突：按真实冲突处理，停止自动重连并展示弹窗。

该规则避免无限重连，也不会让第二个标签页获得接管能力。

### 5. React Runtime 生命周期

`useHotPlexRuntime` 继续按 `sessionId`、`workspaceId` 和 `sessionWorkerType` 管理 client。cleanup 无需等待异步 Promise；后续 client 的 `connect()` 会通过模块级 close barrier 完成交接。

Runtime 不因 permission、question 或 elicitation 消息更新而重建连接。交互响应仍调用当前 `clientRef`，Gateway 的权威 ACK 仍只在 Worker 接受响应后回显。

## 错误处理

- close barrier 超时记录结构化 warning，包含 session 和连接代际，不记录凭证。
- 本地交接首次冲突记录 info；第二次冲突记录现有 duplicate-session 状态。
- 非交接冲突不重试。
- 组件卸载后，旧连接回调不得触发 React state 更新或清除新连接状态。

## 测试

### Browser client 单元测试

- 同一实例并发两次 `connect()` 只创建一个 WebSocket，两个调用共享结果。
- 已连接的同 session 再次 `connect()` 不创建新 socket。
- 自动重连 timer 与手动 `connect()` 竞争时只建立一次连接。
- 旧实例 `disconnect()` 后立即创建新实例，新实例在旧 socket close 前不发送 init。
- 旧 socket 的迟到 message/close 不能改变新连接状态。
- 带本地交接证据的首次 `SESSION_ALREADY_CONNECTED` 静默重试一次。
- 无本地交接证据或第二次冲突继续触发 `sessionAlreadyConnected`。

### Runtime/交互回归测试

- cleanup/remount 交接后，permission response 从新 owner 成功发送并收到 ACK。
- question 与 elicitation 使用相同交接路径。
- 交互 request 的纯消息状态更新不会创建新 WebSocket。

### Gateway 回归测试

- 真实的第二连接仍被拒绝。
- 当前 owner 的三类交互响应通过，非 owner 响应被拒绝。

## 验收标准

- 单标签页在重连、组件重新挂载和交互请求期间不再出现错误的 session 冲突弹窗。
- 同一 session 的两个真实标签页仍只有第一个连接成功。
- 连接交接不会重复发送用户 input 或交互响应。
- WebChat、Gateway 相关 race tests、TypeScript 检查与现有质量门禁通过。
