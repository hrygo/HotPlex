---
title: AEP v1 协议参考
weight: 1
description: Agent Event Protocol v1 完整参考：事件类型、Envelope 格式、背压机制与错误码
---

# AEP v1 协议参考

> Agent Event Protocol v1 完整参考文档——事件类型、Envelope 格式、背压机制、错误码

## 协议概述

AEP v1（Agent Event Protocol）是 HotPlex Gateway 的 WebSocket 通信协议：

- **传输层**：WebSocket，NDJSON（Newline-Delimited JSON）编码
- **通信模式**：全双工（Full-Duplex），Client 和 Server 可同时发送消息
- **设计理念**：Streaming-first、统一 Envelope、结构化用户交互
- **版本标识**：`aep/v1`

## Envelope 格式

所有消息共用统一的 Envelope 结构：

```json
{
  "version": "aep/v1",
  "id": "evt_<uuid>",
  "seq": 42,
  "priority": "data",
  "session_id": "sess_<uuid>",
  "timestamp": 1710000000123,
  "event": {
    "type": "message.delta",
    "data": {}
  }
}
```

| 字段 | 类型 | 必选 | 说明 |
|------|------|------|------|
| `version` | string | 是 | 固定 `aep/v1` |
| `id` | string | 是 | 事件唯一标识，UUID v4（`evt_` 前缀） |
| `seq` | int64 | 是 | 单调递增序列号，从 1 开始，per-session 独立空间 |
| `priority` | string | 否 | `"control"` 或 `"data"`（默认） |
| `session_id` | string | 是 | Session 标识（`sess_` 前缀） |
| `timestamp` | int64 | 是 | Unix 毫秒时间戳 |
| `event.type` | string | 是 | 事件类型 |
| `event.data` | object | 是 | 事件载荷 |

### Priority 语义

- **`control`**：跳过背压队列，优先发送（如 `control.reconnect`、`error`、`done`）
- **`data`**：正常排队，受背压控制（如 `message.delta`、`tool_call`）

### Seq 规则

- 仅实际发送的事件消耗 seq（被丢弃的 delta 不递增 seq）
- `ping`/`pong` 的 seq 为 0（不参与序号分配）
- Client 不应通过 seq gap 检测丢包——gap 不会出现

## Client → Server 事件

### init（连接握手）

WebSocket 连接建立后的**第一帧**必须是 `init`，30 秒超时。

```json
{
  "type": "init",
  "data": {
    "version": "aep/v1",
    "worker_type": "claude_code",
    "session_id": "sess_xxx",
    "title": "可选显示名称",
    "auth": { "token": "<api-key>" },
    "config": {
      "model": "claude-sonnet-4-6",
      "allowed_tools": ["read_file", "write_file"],
      "max_turns": 0,
      "work_dir": "/app"
    },
    "client_caps": {
      "supports_delta": true,
      "supports_tool_call": true,
      "supported_kinds": ["message.delta", "tool_call"]
    }
  }
}
```

| 字段 | 必需 | 说明 |
|------|------|------|
| `version` | 是 | 固定 `"aep/v1"` |
| `worker_type` | 是 | Worker 类型（`claude_code`、`codex_cli`、`acp` 等） |
| `session_id` | 否 | 空=创建新 Session，非空=Resume 模式。会被清洗和长度校验 |
| `title` | 否 | 会话显示名称，不参与 Session ID 派生。最大 256 字符，会被清洗 |

`session_id` 和 `title` 均经过 `SanitizeText()` 清洗（移除控制字符、null bytes、BOM、surrogates）和长度校验（最大 256 字符）。超长时返回 `INVALID_MESSAGE` 错误。

### input（用户输入）

```json
{
  "type": "input",
  "data": {
    "content": "请帮我重构 login 函数",
    "metadata": {}
  }
}
```

外层 Envelope 的 `id` 同时作为默认 `client_message_id`。客户端在连接中断、
ACK 丢失等投递结果不明的情况下重发时，必须复用原 Envelope（包括相同 `id`）；
同一 Session 内，相同 ID + 相同 payload 不会再次调用 Worker，相同 ID + 不同
payload 会返回 `INVALID_MESSAGE`。消息平台适配器使用平台原生 message ID。

`failed` 或 `SESSION_BUSY` 表示本次投递已明确失败，后续逻辑重试应使用新 ID。
`unknown` 表示可能已产生副作用：复用原 ID 只会查询现状；若用户明确接受重复风险
并决定再次执行，则必须创建新 ID。

只有实际投递给 Worker 的普通输入进入持久化账本；Gateway 自身处理的帮助、
控制和 Worker 命令不产生 `input.ack`。

### permission_response（权限响应）

```json
{
  "type": "permission_response",
  "data": {
    "id": "perm_123",
    "allowed": true,
    "reason": "approved by user"
  }
}
```

### question_response（用户回答）

```json
{
  "type": "question_response",
  "data": {
    "id": "q_<uuid>",
    "answers": { "问题文本": "选项标签" }
  }
}
```

### elicitation_response（MCP 输入响应）

```json
{
  "type": "elicitation_response",
  "data": {
    "id": "el_<uuid>",
    "action": "accept",
    "content": { "token": "ghp_xxx" }
  }
}
```

`action` 取值：`"accept"` | `"decline"` | `"cancel"`。

### worker_command（Worker 命令）

通过 Worker stdio 发送的控制命令，用于在 Agent Turn 过程中查询状态或修改运行时行为。

```json
{
  "type": "worker_command",
  "data": {
    "command": "context_usage",
    "args": "",
    "extra": {}
  }
}
```

`WorkerStdioCommand` 支持以下命令：

| Command | 说明 |
|---------|------|
| `context_usage` | 查询当前 context window 使用情况 |
| `mcp_status` | 查询 MCP 服务器连接状态 |
| `set_model` | 切换 LLM 模型 |
| `set_permission` | 修改工具权限策略 |
| `skills` | 列出可用 Skills |
| `compact` | 压缩上下文历史 |
| `clear` | 清空上下文 |
| `model` | 查询当前模型信息 |
| `effort` | 调整推理努力级别 |
| `rewind` | 回退到指定对话快照 |
| `commit` | 触发代码提交 |

### ping（心跳）

```json
{ "type": "ping", "data": {} }
```

## Server → Client 事件

### init_ack（握手确认）

```json
{
  "type": "init_ack",
  "data": {
    "session_id": "sess_xxx",
    "state": "created",
    "server_caps": {
      "protocol_version": "aep/v1",
      "supports_resume": true,
      "supports_delta": true,
      "max_frame_size": 32768
    }
  }
}
```

错误时 `state` 为 `"deleted"`，附带 `error` + `code` 字段。

### input.ack（输入持久化与投递确认）

```json
{
  "type": "input.ack",
  "data": {
    "client_message_id": "evt_<uuid>",
    "execution_id": "exec_<uuid>",
    "status": "accepted",
    "duplicate": false
  }
}
```

Gateway 在输入写入持久化账本后先发送 `accepted`，在 `Worker.Input` 返回后再发送
最终投递状态：

| Status | 说明 |
|--------|------|
| `accepted` | 已持久化，尚未确认 Worker 是否接受 |
| `delivered` | Worker 输入端点已接受 |
| `unknown` | 超时或重启导致结果不确定；为避免重复副作用，Gateway 不自动重投 |
| `failed` | Worker 明确拒绝或投递失败；`error_code` 提供分类 |

`duplicate: true` 表示 Gateway 返回已有记录且未再次调用 Worker。`input.ack`
使用 control priority，绕过普通 broadcast 背压队列。

### message.start / message.delta / message.end（流式输出）

三段式流式消息生命周期：

```
message.start → message.delta* → message.end
```

- `message.start`：消息元数据（ID、role、content_type）
- `message.delta`：增量文本（可被背压丢弃，`dropped` 标记）
- `message.end`：消息结束标记

### message（完整消息）

非流式场景的完整消息，向后兼容。流式场景推荐使用三段式。

### tool_call / tool_result（工具调用通知）

```json
{ "type": "tool_call", "data": { "id": "call_123", "name": "read_file", "input": {"path": "/app/main.py"} } }
{ "type": "tool_result", "data": { "id": "call_123", "output": "file content...", "error": "" } }
```

Autonomous 模式下为**通知性质**，Worker 内部执行，Client 无需回传结果。

### tool_update（工具调用中间状态）

```json
{ "type": "tool_update", "data": { "id": "call_123", "name": "read_file", "status": "in_progress" } }
```

ACP 专用：映射 `tool_call_update`，报告工具调用的中间状态（`pending` / `in_progress`）。

### plan（计划更新）

```json
{ "type": "plan", "data": { "entries": [{"id": "1", "text": "Read config file", "status": "completed"}] } }
```

ACP 专用：映射 `AgentPlanUpdate`，Agent 的计划/任务列表变更通知。

### mode_update（模式切换）

```json
{ "type": "mode_update", "data": { "mode_id": "code", "name": "Code Mode" } }
```

ACP 专用：映射 `CurrentModeUpdate`，Agent 执行模式切换通知。

### state（状态变更）

```json
{ "type": "state", "data": { "state": "running", "message": "context_reset" } }
```

状态集合：`created` | `running` | `idle` | `terminated`

### done（执行完成）

```json
{
  "type": "done",
  "data": {
    "success": true,
    "stats": {
      "duration_ms": 5200,
      "tool_calls": 3,
      "input_tokens": 1000,
      "output_tokens": 500,
      "total_tokens": 1700,
      "model": "claude-sonnet-4-6",
      "context_used_percent": 45.2
    },
    "dropped": false
  }
}
```

`dropped: true` 表示本轮有 `message.delta` 被丢弃，Client 应以最终完整载荷覆盖渲染。

> **注意**：`stats` 字段类型为 `map[string]any`，无固定 schema。上表列出的是常见字段，实际返回的字段取决于 Worker 类型和执行结果，可能包含 `cost_usd`、`cache_read_tokens` 等额外信息。

### permission_request / question_request / elicitation_request（用户交互）

Worker 请求人类介入的结构化交互事件。默认 5 分钟超时自动拒绝（auto-deny）。

### reasoning / step / raw（辅助事件）

- `reasoning`：Agent 思维过程（thinking）
- `step`：执行阶段标记（plan/execute/verify）
- `raw`：Worker 原始事件透传

### context_usage（Context 用量报告）

```json
{
  "type": "context_usage",
  "data": {
    "total_tokens": 62000,
    "max_tokens": 200000,
    "percentage": 31,
    "categories": [{"name": "system_prompt", "tokens": 4500}]
  }
}
```

### mcp_status（MCP 服务状态）

```json
{
  "type": "mcp_status",
  "data": {
    "servers": [{"name": "github-mcp", "status": "connected"}]
  }
}
```

### pong（心跳响应）

```json
{ "type": "pong", "data": { "state": "idle" } }
```

## 双向事件

### control（控制命令）

**Client → Server**：

| Action | 说明 |
|--------|------|
| `terminate` | 终止 Worker，Session → `TERMINATED` |
| `delete` | 删除 Session + 清理 runtime |
| `reset` | 清空上下文，Session 保持 `RUNNING` |
| `gc` | 归档会话：终止 Worker，保留历史，可 Resume |
| `cd` | 切换工作目录（创建新 Session 继承原 Session 上下文） |

**Server → Client**（`priority: "control"`）：

| Action | 说明 |
|--------|------|
| `reconnect` | 强制重连（服务器维护、版本升级） |
| `session_invalid` | Session 失效通知 |
| `throttle` | 降级通知（速率限制建议） |

### error（错误通知）

```json
{ "type": "error", "data": { "code": "WORKER_CRASH", "message": "exit code 139" } }
```

`error` 后必须跟随 `done`。

## 错误码参考

### Worker 类

| Code | 说明 |
|------|------|
| `WORKER_START_FAILED` | Worker 启动失败 |
| `WORKER_CRASH` | 进程崩溃（SIGSEGV 等） |
| `WORKER_TIMEOUT` | 执行超时 |
| `WORKER_OOM` | 内存溢出（exit code 137） |
| `PROCESS_SIGKILL` | 被强制终止 |
| `WORKER_OUTPUT_LIMIT` | 单行输出超限（10MB） |

### Session 类

| Code | 说明 |
|------|------|
| `SESSION_NOT_FOUND` | Session 不存在 |
| `SESSION_EXPIRED` | Session 已过期 |
| `SESSION_BUSY` | 正在执行，拒绝新 input |
| `SESSION_TERMINATED` | Session 已终止 |
| `SESSION_INVALIDATED` | Session 被失效 |

### Protocol 类

| Code | 说明 |
|------|------|
| `INVALID_MESSAGE` | 消息格式无效 |
| `PROTOCOL_VIOLATION` | 协议违规 |
| `VERSION_MISMATCH` | 版本不兼容 |
| `CONFIG_INVALID` | 配置校验失败 |

### Auth / Gateway 类

| Code | 说明 |
|------|------|
| `UNAUTHORIZED` | 认证失败 |
| `AUTH_REQUIRED` | 认证缺失 |
| `INTERNAL_ERROR` | 内部错误 |
| `GATEWAY_OVERLOAD` | 过载 |
| `RATE_LIMITED` | 速率限制 |
| `EXECUTION_TIMEOUT` | Worker 僵死超时 |
| `RECONNECT_REQUIRED` | 服务端要求客户端重连 |
| `RESUME_RETRY` | Session resume 失败，建议重试 |
| `NOT_SUPPORTED` | 操作不支持 |
| `TURN_TIMEOUT` | Turn 执行超时 |

## 背压机制

Worker 产出过快时，Gateway 使用 bounded channel（默认容量 256）缓冲消息：

| 事件类型 | 背压行为 |
|---------|---------|
| `message.delta` / `raw` | **可丢弃**（非阻塞 select） |
| `message` / `done` / `error` / `control` | **不可丢弃**（阻塞发送） |
| `input.ack` | **直接投递**（control priority） |
| `priority: "control"` | 跳过背压队列，直接发送 |

丢弃的 delta 不消耗 seq，通过 `done.dropped` 标记通知 Client。

## Init 握手流程

```
Client                          Server
  |                               |
  |--- init(version, caps) ------>|
  |                               |--- 创建/恢复 Session
  |<-- init_ack(session_id) ------|
  |                               |
  |--- input(content) ----------->|--- state(running)
  |<-- input.ack(accepted) -------|--- 持久化入口账本
  |<-- input.ack(delivered) ------|--- Worker 接受输入
  |                               |--- message.start
  |<-- message.delta * ------------|--- message.delta
  |<-- message.end ---------------|
  |<-- done(success) -------------|--- state(idle)
  |                               |
```

## 全双工通信流

```
Client ←→ Server

Input Flow:     input → input.ack(accepted) → input.ack(delivered|unknown|failed) → [tool_call → tool_result]* → [message.delta*] → done
Control Flow:   control(action) → state(new_state) / error
Interactive:    permission_request ←→ permission_response
                question_request ←→ question_response
                elicitation_request ←→ elicitation_response
Heartbeat:      ping ←→ pong
```

## 最小合规要求

**必须支持**：`init`、`input`、`control`、`ping`、`init_ack`、`message.delta`、`state`、`error`、`done`、`pong`

**可选扩展**：`input.ack`、`message.start/end`、`message`、`tool_call/result`、`tool_update`、`plan`、`mode_update`、`reasoning`、`step`、`raw`、`permission_*`、`question_*`、`elicitation_*`、`context_usage`、`mcp_status`、`worker_command`
