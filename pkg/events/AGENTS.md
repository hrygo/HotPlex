# pkg/events — AEP v1 数据结构

## OVERVIEW
AEP v1 (Agent Event Protocol v1) 类型定义。Gateway 和所有客户端 SDK 共享的唯一事件契约，是 wire format 的权威来源。

## STRUCTURE
```
events.go     # Kind 常量、Envelope/Event、所有 *Data 载荷结构、SessionState 状态机、Clone
helpers.go    # 类型提取辅助（DecodeAs 泛型、Map*Response 适配、数值/字符串提取）
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| 协议版本常量 | `events.go:9` | `Version = "aep/v1"`，所有 Envelope 必须 carry |
| Envelope 字段 | `events.go:94-110` | Version/ID/Seq/Priority/SessionID/Timestamp/Event/Metadata/OwnerID |
| Event wrapper | `events.go:172-175` | `Event{Type Kind, Data interface{}}`，Data 是开放联合 |
| 构造函数 | `events.go:561` | `NewEnvelope(id, sid, seq, kind, data)` 自动盖时间戳+版本 |
| 浅拷贝（独立 map） | `events.go:117` | `Clone` 递归深拷 Event.Data 和 Metadata |
| 全独立拷贝 | `events.go:159` | `CloneDeep` JSON 往返，用于 event store replay |
| 通用 Data 提取 | `helpers.go:120` | `DecodeAs[T](data)` 处理 typed struct 和 JSON round-trip 后的 map |
| SessionState 状态机 | `events.go:530-558` | created→running→idle→terminated→deleted；`IsValidTransition` |

## Kind 常量（events.go:15-52）

**协议核心**：`Init` `Error` `State` `Input` `InputAck` `Done` `Control` `Ping` `Pong`
**消息流（S→C）**：`Message` `MessageStart` `MessageDelta` `MessageEnd`
**Tool（S→C）**：`ToolCall` `ToolResult` `Reasoning` `Step` `Raw`
**用户交互**：`PermissionRequest/Response` `QuestionRequest/Response` `ElicitationRequest/Response`
**Worker 控制（S→C）**：`ContextUsage` `SkillsList` `MCPStatus` `WorkerCmd`（"worker_command"）
**ACP 扩展**：`ToolUpdate`（"tool_update"）`Plan` `ModeUpdate`
**内部（不转发客户端）**：`KindInternalReset`（"internal_reset"，仅 Worker→Gateway 协调）

## Data 载荷（按 Kind 配对）

| Kind | Data 类型 | 行号 |
|------|-----------|------|
| Error | `ErrorData{Code, Message}` | 178 |
| State | `StateData{State, Message}` | 184 |
| Input | `InputData{Content, Metadata}` | 190 |
| InputAck | `InputAckData{ClientMessageID, ExecutionID, Status, Duplicate, ErrorCode}` | 196 |
| Message(Delta|Start|End) | `MessageDeltaData`/`StartData`/`EndData` | 196/204/210 |
| Message | `MessageData`（非流式、向后兼容） | 265 |
| ToolCall/Result | 带 ACP 扩展字段（Title/Kind/Locations、Status/Diff） | 228/239 |
| Done | `DoneData{Success, Stats, Dropped}`（Dropped=背压静默丢弃标记） | 255 |
| Control | `ControlData{Action, Reason, DelayMs, ...}` | 368 |
| WorkerCmd | `WorkerCommandData{Command, Args}` | 410 |

完整列表见源码 events.go:177-506。

## KEY PATTERNS
**开放 Data 接口（events.go:174）** — `Data interface{}` 不强制具体类型，运行时由 Kind 决定。Decode 后可能是 typed struct 或 `map[string]any`（JSON 往返后），用 `DecodeAs[T]` 统一处理。

**Clone vs CloneDeep** — `Clone` 浅拷+递归 map（高性能、保留 typed struct）；`CloneDeep` JSON 往返（完全独立、用于持久化）。并发发送前用 Clone，避免 EncodeJSON in-place 改写互相干扰。

**ErrorCode 哨兵集（events.go:65-91）** — 26 个 `ErrCode*`（如 `SESSION_BUSY`、`VERSION_MISMATCH`、`WORKER_CRASH`），客户端按 code 分支处理而非字符串匹配。

**ControlAction vs WorkerStdioCommand** — Control 改会话状态（terminate/reset/gc/cd）；WorkerStdioCommand 是 Worker 进程内操作（context_usage/set_model/compact 等），`IsPassthrough()` 区分走 Input 还是 SendControlRequest。

**Priority 背压（events.go:55-60）** — `PriorityControl` 跳过背压直发，`PriorityData`（默认）受背压影响。message.delta 在通道满时静默丢弃（Done.Dropped 标记）。

## ANTI-PATTERNS
- ❌ 在 pkg/events 引入 internal/ 依赖（gateway/session 等）— 这是公共契约层
- ❌ 新增 Kind 不加对应 *Data 结构或 Documentation
- ❌ 客户端用字符串字面量匹配 Kind，应导入常量
- ❌ 跨 goroutine 共享 Envelope 不先 Clone（map 字段共享会导致数据竞争）
- ❌ 依赖 OwnerID 字段做 wire 逻辑（`json:"-"` 不序列化，仅 Gateway 内部用）

## PUBLIC API STABILITY
被 `client/`（Go SDK）、`examples/{typescript,python,java}-client/`、Gateway 三方共用。Kind 常量值（如 `"message.delta"`）和 Envelope JSON tag 是 wire contract，**禁止改名或改 tag**。新增 Kind/Data 字段必须 `omitempty` 或向后兼容。任何破坏性变更需 major 版本 bump。
