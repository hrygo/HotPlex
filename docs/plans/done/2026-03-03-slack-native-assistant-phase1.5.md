# Slack AI Native Assistant Phase 1.5: 架构优化方案

> **目标**: 基于 DRY、SOLID 和整洁架构原则，优化当前实现，为 Phase 2 (Brain 集成) 打下坚实基础

---

## 1. 现状问题分析

### 1.1 违反 DRY 原则 (Don't Repeat Yourself)

#### 问题 1: 状态更新存在双重路径

```
当前实现:
┌─────────────────────────────────────────────────────────────────┐
│                     handleThinking()                            │
│                              │                                  │
│              ┌───────────────┴───────────────┐                  │
│              ▼                               ▼                  │
│  updateStatusMessage()          (未调用) SetAssistantStatus()  │
│  发送 MessageTypeThinking 气泡     Slack 原生状态文字           │
│              │                               │                  │
│              └───────────────┬───────────────┘                  │
│                              ▼                                  │
│                    两套机制做同一件事                           │
└─────────────────────────────────────────────────────────────────┘
```

**具体表现**:
- `handleThinking()` → `updateStatusMessage(MessageTypeThinking)` → 发送气泡
- `handleToolUse()` → `updateStatusMessage(MessageTypeToolUse)` → 发送气泡
- **缺少**: 任何地方都**没有调用** `SetAssistantStatus()`

**重复点**:
- 两者都在告诉用户"AI 正在工作"
- 两者都需要 channelID + threadTS
- 两者都有状态文本内容

#### 问题 2: ChatMessage 构建逻辑重复

```go
// handleToolUse (line 751-761)
msg := &base.ChatMessage{
    Type:    base.MessageTypeToolUse,
    Content: toolName,
    Metadata: map[string]any{
        "input":         input,
        "input_summary": inputSummary,
        "truncated":     truncated,
        "event_type":    string(provider.EventTypeToolUse),
        "stream":        true,
    },
}

// handleAnswer (line 931-941)
msg := &base.ChatMessage{
    Type:    base.MessageTypeAnswer,
    Content: content,
    Metadata: map[string]any{
        "event_type": string(provider.EventTypeAnswer),
    },
}
```

**重复模式**: 创建 → 设置 Metadata → mergeMetadata → convertToChatMessage

---

### 1.2 违反 SOLID 原则

#### 问题 1: 单一职责原则 (SRP) - StreamCallback 过于臃肿

`StreamCallback` 当前承担了 **7 种职责**:

| 职责 | 方法/字段 |
|------|-----------|
| 事件路由 | `OnEvent()`, `handleXxx()` 系列 |
| 消息构建 | `convertToChatMessage()`, `sendMessageAndGetTS()` |
| 状态管理 | `updateStatusMessage()`, `currentStatus` |
| 流式输出 | `streamWriter`, `streamWriterActive` |
| 气泡管理 | `thinkingSent`, `thinkingMessageTS` |
| 会话生命周期 | `handleSessionStart()`, `handleSessionStats()` |
| 平台抽象 | `messageOps`, `sessionOps` |

**问题**: 任何职责的变更都会影响这个类，难以测试和维护

#### 问题 2: 依赖倒置原则 (DIP) - 缺少状态抽象接口

```go
// 当前: 直接依赖具体类型
func (c *StreamCallback) updateStatusMessage(statusType base.MessageType, displayText string) error {
    // statusType 是 base.MessageType 枚举
    // 具体实现硬编码在方法内部
}
```

**应该**: 依赖抽象接口，让平台适配器决定如何实现

---

### 1.3 整洁架构问题

#### 问题 1: 层级边界模糊

```
当前架构 (混乱):
┌─────────────────────────────────────────────────────────────────┐
│  应用层 (StreamCallback)                                        │
│  - 直接调用 messageOps.SetAssistantStatus()                     │
│  - 直接构造 base.ChatMessage                                     │
│  - 直接操作 Slack 特定的 channelID, threadTS                    │
├─────────────────────────────────────────────────────────────────┤
│  领域层 (base/types.go)                                         │
│  - MessageType 枚举 (thinking, tool_use, answer...)            │
│  - MessageOperations 接口                                      │
├─────────────────────────────────────────────────────────────────┤
│  基础设施层 (slack/adapter.go)                                  │
│  - Slack 特定的 API 调用                                        │
└─────────────────────────────────────────────────────────────────┘

问题: StreamCallback 跨越了多个层级
```

#### 问题 2: 依赖方向错误

```
错误: 应用层 → 基础设施层 (直接依赖)
StreamCallback → slack.Adapter (Slack 特定)

正确: 应用层 → 领域层 → 基础设施层
StreamCallback → MessageOperations → slack.Adapter
```

---

### 1.4 其他架构问题

#### 问题 3: 缺少 StatusProvider 抽象

当前状态管理只能通过一种方式（发送气泡消息），无法优雅地切换到原生 API

#### 问题 4: metadata 滥用

```go
// 到处传递的 map[string]any，缺乏类型安全
msg.Metadata["stream"] = true
msg.Metadata["event_type"] = string(provider.EventTypeToolUse)
msg.Metadata["input_summary"] = inputSummary
```

---

## 2. 整洁架构优化方案

### 2.1 引入 StatusProvider 接口

**目标**: 抽象状态通知机制，支持多种实现

```go
// chatapps/base/types.go

// StatusType 定义 AI 工作状态
type StatusType string

const (
    StatusThinking   StatusType = "thinking"
    StatusToolUse    StatusType = "tool_use"
    StatusToolResult StatusType = "tool_result"
    StatusAnswering  StatusType = "answering"
    StatusIdle       StatusType = "idle"
)

// StatusProvider 定义状态通知的抽象接口
// 遵循依赖倒置原则，让适配器决定具体实现
type StatusProvider interface {
    // SetStatus 设置当前状态，平台适配器负责转换为原生 API 或消息气泡
    SetStatus(ctx context.Context, status StatusType, text string) error

    // ClearStatus 清除状态指示
    ClearStatus(ctx context.Context) error
}
```

### 2.2 重构 AdapterManager

**目标**: 统一获取平台特定接口

```go
// chatapps/manager.go

type AdapterManager interface {
    // ... 现有方法

    // GetStatusProvider 获取状态提供者
    GetStatusProvider(platform string) (StatusProvider, bool)
}
```

### 2.3 重构 Slack Adapter

```go
// chatapps/slack/adapter.go

// 确保实现 StatusProvider 接口
var _ base.StatusProvider = (*Adapter)(nil)

func (a *Adapter) SetStatus(ctx context.Context, status base.StatusType, text string) error {
    // 方案 A: 调用原生 SetAssistantStatus (如果可用)
    if a.client != nil {
        threadTS := "" // 从 context 或参数获取
        return a.SetAssistantStatus(ctx, a.channelID, threadTS, text)
    }

    // 方案 B: 回退到发送气泡消息
    return a.fallbackSendBubble(ctx, status, text)
}
```

### 2.4 抽取 StatusManager

**目标**: 单一职责管理状态

```go
// chatapps/internal/status_manager.go

// StatusManager 统一管理 AI 状态通知
type StatusManager struct {
    provider  base.StatusProvider  // 依赖抽象
    logger    *slog.Logger
    mu        sync.Mutex
    current   base.StatusType
}

func NewStatusManager(provider base.StatusProvider, logger *slog.Logger) *StatusManager {
    return &StatusManager{
        provider: provider,
        logger:   logger,
    }
}

func (m *StatusManager) Notify(status base.StatusType, text string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.current == status {
        return nil // 避免重复
    }
    m.current = status

    return m.provider.SetStatus(context.Background(), status, text)
}

func (m *StatusManager) Clear() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.current = base.StatusIdle
    return m.provider.ClearStatus(context.Background())
}
```

---

## 3. 实施路线图

### Task 1: 抽取 StatusProvider 接口

**文件**:
- `chatapps/base/types.go` - 新增 StatusType, StatusProvider

**步骤**:
1. 定义 `StatusType` 枚举
2. 定义 `StatusProvider` 接口
3. 更新 Slack Adapter 实现接口

### Task 2: 抽取 StatusManager

**文件**:
- `chatapps/internal/status_manager.go` (新建)

**步骤**:
1. 创建 StatusManager 结构
2. 实现状态去重、节流逻辑
3. 单元测试

### Task 3: 重构 StreamCallback

**文件**:
- `chatapps/engine_handler.go`

**步骤**:
1. 移除 `updateStatusMessage()` 中的直接气泡发送逻辑
2. 注入 `StatusManager` 依赖
3. 将状态更新委托给 StatusManager

### Task 4: 统一消息构建

**文件**:
- `chatapps/engine_handler.go`

**步骤**:
1. 抽取 `buildChatMessage()` 辅助函数
2. 消除重复的 Metadata 设置逻辑

### Task 5: 添加单元测试

**文件**:
- `chatapps/internal/status_manager_test.go`

---

## 4. 预期收益

### 4.1 DRY 原则

| 改进前 | 改进后 |
|--------|--------|
| 状态更新两套逻辑 | 统一通过 StatusProvider |
| ChatMessage 构建重复 | 抽取 buildChatMessage() |

### 4.2 SOLID 原则

| 改进前 | 改进后 |
|--------|--------|
| StreamCallback 7 种职责 | 拆分后各司其职 |
| 直接依赖 MessageType 枚举 | 依赖 StatusProvider 抽象 |

### 4.3 整洁架构

| 改进前 | 改进后 |
|--------|--------|
| 层级边界模糊 | 清晰的三层架构 |
| 依赖方向错误 | 依赖接口而非实现 |

### 4.4 可测试性

| 改进前 | 改进后 |
|--------|--------|
| 难以 mock 状态逻辑 | StatusManager 可独立测试 |
| 难以替换平台实现 | StatusProvider 接口隔离 |

---

## 5. 注意事项

### 5.1 向后兼容

- 当前 `messageOps` 接口保持不变
- 新增 `StatusProvider` 作为可选依赖
- 渐进式迁移，不破坏现有功能

### 5.2 与 Phase 2 的关系

Phase 1.5 的优化为 Phase 2 (Brain 集成) 打下基础：

```
Phase 1.5 (当前)          Phase 2 (未来)
─────────────────         ─────────────────
StatusManager      →     Brain.EventHandler
                              │
                              ▼
                     StatusManager.Notify("reasoning", "思考中...")
```

当 Brain 模块抛出 `Reasoning` 事件时，通过 StatusManager 统一处理，无需修改现有代码。

---

## 6. 验证标准

完成实施后，代码应满足：

- [ ] `go build ./chatapps/...` 编译通过
- [ ] `go test ./chatapps/... -race` 测试通过
- [ ] StreamCallback 职责明显减少
- [ ] StatusProvider 接口可被 mock
- [ ] 无明显的代码重复

---

## 7. 待排查问题

### 问题: 流式写入失败 `invalid_arguments`

**发现时间**: 2026-03-03

**现象**: 日志中反复出现以下警告：
```
level=WARN source=chatapps/engine_handler.go:923 msg="Failed to write to stream" error="start stream: start stream: invalid_arguments"
```

**影响**:
- 消息无法实时流式显示到 Slack
- 降级为聚合器批量发送（延迟约 500ms）

**问题定位**:
| 位置 | 代码 |
|------|------|
| `streaming_writer.go:59` | `w.adapter.StartStream(w.ctx, w.channelID, w.threadTS)` |
| `adapter.go:1821` | `a.client.StartStreamContext(ctx, channelID, ...)` |

**可能原因**:
1. `channelID` 或 `threadTS` 参数格式不正确
2. Slack API `StartStreamContext` 缺少必填参数（如 `subtype`）
3. Slack App 权限不足（需要 `chat:write` 等）
4. Slack SDK 版本不兼容

**排查步骤**:
1. 打印 `channelID` 和 `threadTS` 确认值正确
2. 检查 Slack API 文档确认 `StartStreamContext` 完整参数
3. 验证 Slack App Token 权限
4. 对比 Slack SDK 示例代码

**相关文件**:
- `chatapps/slack/streaming_writer.go`
- `chatapps/slack/adapter.go`
- `chatapps/engine_handler.go`

---

## 8. 待办事项

### 8.1 已完成 (Phase 1.5.1)

- [x] Task 1: 抽取 StatusProvider 接口 (`chatapps/base/types.go`)
- [x] Task 2: 抽取 StatusManager (`chatapps/internal/status_manager.go`)
- [x] Task 3: Slack Adapter 实现 StatusProvider (`chatapps/slack/adapter.go`)
- [x] Task 4: 统一消息构建 - 添加 `buildChatMessage()` 辅助函数
- [x] Task 5: 单元测试 (`chatapps/internal/status_manager_test.go`)

### 8.2 已完成 (Phase 1.5.2)

- [x] StreamCallback 集成 StatusManager
  - 注入 StatusManager 依赖到 StreamCallback
  - 将 `updateStatusMessage()` 改为调用 StatusManager
- [x] 迁移剩余 handler 到 `buildChatMessage()`
  - handleToolUse, handleToolResult, handleError, handleSessionStats
  - handleCommandProgress, handleCommandComplete, handleStepStart
  - handleStepFinish, handleDangerBlock
- [x] 排查流式写入失败 `invalid_arguments` 问题
  - 修复: StartStream 使用非空文本 `" "` 满足 Slack API 要求
  - 添加调试日志

### 8.3 待处理 (Phase 2)

- [ ] 移除重复的 currentStatus 追踪 (StatusManager vs StreamCallback)
- [ ] 拆分 StreamCallback 为更小的组件 (SRP)
- [ ] Brain 集成 (见 `native-brain-architecture_zh.md`)

---

**文档状态**: Completed
**创建时间**: 2026-03-03
**更新时间**: 2026-03-04
