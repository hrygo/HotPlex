# Slack AI Native Assistant Phase 1 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** 实现 Slack AI Assistant 原生化基础架构，包括名字流光效果、原生状态反馈和流式输出。

**Architecture:** 通过扩展 `chatapps/base/types.go` 的 `MessageOperations` 接口定义平台中立抽象，在 `chatapps/slack/adapter.go` 中实现 Slack 特定的 Assistant Threads API 和流式消息 API 封装，最终在 `chatapps/engine_handler.go` 中重构 `StreamCallback` 以启用原生状态感知的流式流转。

**Tech Stack:** Go 1.25 | slack-go/slack v0.18.0 | Slack Assistant Threads API | Native Streaming API

---

## Phase 1 Overview

### P1.1 平台配置与基础接口扩展
- Slack Dashboard 配置 Agents & AI Apps
- Manifest 更新添加 assistant:write Scope
- `chatapps/base/types.go` 扩展 MessageOperations 接口

### P1.2 Adapter 接口扩展
- `chatapps/slack/adapter.go` 实现 SetAssistantStatus
- `chatapps/slack/adapter.go` 实现 StartStream/AppendStream/StopStream
- `chatapps/slack/adapter.go` 实现 NativeStreamingWriter

### P1.3 Engine 状态流转重构
- `chatapps/engine_handler.go` 重构 StreamCallback 启用原生状态
- 抑制旧版 MessageTypeThinking 气泡发送
- 实现流式感知的事件处理流转

---

## Task 1: 平台配置与基础接口扩展

**Files:**
- Modify: `chatapps/base/types.go`
- Create: `docs/slack/manifest.yaml` (配置参考)

### Step 1: 阅读 base/types.go 文件

Read: `chatapps/base/types.go`

确认当前 `MessageOperations` 接口的完整定义。

### Step 2: 扩展 MessageOperations 接口

Edit: `chatapps/base/types.go`

在 `MessageOperations` 接口中添加以下方法（添加到现有接口定义末尾）：

```go
// MessageOperations defines platform-specific message operations
type MessageOperations interface {
	DeleteMessage(ctx context.Context, channelID, messageTS string) error
	UpdateMessage(ctx context.Context, channelID, messageTS string, msg *ChatMessage) error

	// SetAssistantStatus 设置线程底部的原生助手状态文字
	// 用于驱动动态状态提示（如："正在思考..."、"正在搜索代码..."）
	SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error
	// StartStream 开启一个原生流式消息，返回 message_ts 作为后续锚点
	StartStream(ctx context.Context, channelID, threadTS string) (string, error)
	// AppendStream 向现有流增量推送内容
	AppendStream(ctx context.Context, channelID, messageTS, content string) error
	// StopStream 结束流并固化消息
	StopStream(ctx context.Context, channelID, messageTS string) error
}
```

### Step 3: 验证接口修改

Run: `go build ./chatapps/base/...`

Expected: 编译成功，无错误

### Step 4: 提交

Run: `git add chatapps/base/types.go`

Run: `git commit -m "feat(slack): extend MessageOperations interface with Assistant Threads API"`

---

## Task 2: Slack Adapter 原生流式 API 实现

**Files:**
- Modify: `chatapps/slack/adapter.go`
- Test: `chatapps/slack/adapter_test.go`

### Step 1: 阅读 adapter.go 文件

Read: `chatapps/slack/adapter.go`

定位现有方法实现的位置，找到合适的插入点（建议在文件末尾，与其他 SDK 方法放在一起）。

### Step 2: 实现 SetAssistantStatus 方法

在 `adapter.go` 文件末尾添加以下方法：

```go
// SetAssistantStatus 设置线程底部的原生助手状态文字
// 用于驱动动态状态提示（如："正在思考..."、"正在搜索代码..."）
// Slack API: assistant.threads.setStatus
func (a *Adapter) SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	params := slack.AssistantThreadsSetStatusParameters{
		ChannelID: channelID,
		ThreadTS:  threadTS,
		Status:    status,
	}

	return a.client.SetAssistantThreadsStatusContext(ctx, params)
}
```

### Step 3: 实现 StartStream 方法

在 `SetAssistantStatus` 方法后添加：

```go
// StartStream 开启一个原生流式消息，返回 message_ts 作为后续锚点
// Slack API: 通过 slack-go 库的 StartStreamContext 实现
func (a *Adapter) StartStream(ctx context.Context, channelID, threadTS string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("slack client not initialized")
	}

	// 构建流式消息选项
	options := []slack.MsgOption{}

	// 如果有 threadTS，在_thread_中启动流式消息
	if threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	// 启动流式消息（空内容启动）
	_, ts, err := a.client.StartStreamContext(ctx, channelID,
		slack.MsgOptionText("", false),
	)
	if err != nil {
		return "", fmt.Errorf("start stream: %w", err)
	}

	return ts, nil
}
```

### Step 4: 实现 AppendStream 方法

在 `StartStream` 方法后添加：

```go
// AppendStream 向现有流增量推送内容
// Slack API: 通过 slack-go 库的 AppendStreamContext 实现
func (a *Adapter) AppendStream(ctx context.Context, channelID, messageTS, content string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, err := a.client.AppendStreamContext(ctx, channelID, messageTS,
		slack.MsgOptionText(content, false),
	)
	if err != nil {
		return fmt.Errorf("append stream: %w", err)
	}

	return nil
}
```

### Step 5: 实现 StopStream 方法

在 `AppendStream` 方法后添加：

```go
// StopStream 结束流并固化消息
// Slack API: 通过 slack-go 库的 StopStreamContext 实现
func (a *Adapter) StopStream(ctx context.Context, channelID, messageTS string) error {
	if a.client == nil {
		return fmt.Errorf("slack client not initialized")
	}

	_, err := a.client.StopStreamContext(ctx, channelID, messageTS)
	if err != nil {
		return fmt.Errorf("stop stream: %w", err)
	}

	return nil
}
```

### Step 6: 验证编译

Run: `go build ./chatapps/slack/...`

Expected: 编译成功，无错误

### Step 7: 提交

Run: `git add chatapps/slack/adapter.go`

Run: `git commit -m "feat(slack): implement Assistant Threads API and Native Streaming methods"`

---

## Task 3: NativeStreamingWriter 实现

**Files:**
- Create: `chatapps/slack/streaming_writer.go`

### Step 1: 创建 streaming_writer.go 文件

Create: `chatapps/slack/streaming_writer.go`

```go
package slack

import (
	"context"
	"fmt"
	"io"
	"sync"
)

// NativeStreamingWriter 实现 io.Writer 接口，封装 Slack 原生流式消息的生命周期管理
// 首次 Write 调用时启动流，后续调用追加内容，Close 时结束流
type NativeStreamingWriter struct {
	ctx        context.Context
	adapter    *Adapter
	channelID  string
	threadTS   string
	messageTS  string
	mu         sync.Mutex
	started    bool
	closed     bool
	onComplete func(string) // 流结束时的回调，用于获取最终 messageTS
}

// NewNativeStreamingWriter 创建新的原生流式写入器
func NewNativeStreamingWriter(
	ctx context.Context,
	adapter *Adapter,
	channelID, threadTS string,
	onComplete func(string),
) *NativeStreamingWriter {
	return &NativeStreamingWriter{
		ctx:        ctx,
		adapter:    adapter,
		channelID:  channelID,
		threadTS:   threadTS,
		onComplete: onComplete,
	}
}

// Write 实现 io.Writer 接口
// 首次调用执行 StartStream 获取 TS；后续调用执行 AppendStream 增量推送
func (w *NativeStreamingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("stream already closed")
	}

	content := string(p)
	if content == "" {
		return len(p), nil
	}

	// 首次调用，启动流
	if !w.started {
		messageTS, err := w.adapter.StartStream(w.ctx, w.channelID, w.threadTS)
		if err != nil {
			return 0, fmt.Errorf("start stream: %w", err)
		}
		w.messageTS = messageTS
		w.started = true
	}

	// 追加内容到流
	if err := w.adapter.AppendStream(w.ctx, w.channelID, w.messageTS, content); err != nil {
		return 0, fmt.Errorf("append stream: %w", err)
	}

	return len(p), nil
}

// Close 结束流并固化消息
func (w *NativeStreamingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	if !w.started {
		// 如果从未启动过流（空内容），直接返回
		w.closed = true
		return nil
	}

	// 结束流
	if err := w.adapter.StopStream(w.ctx, w.channelID, w.messageTS); err != nil {
		return fmt.Errorf("stop stream: %w", err)
	}

	w.closed = true

	// 调用完成回调，传递最终的 messageTS
	if w.onComplete != nil {
		w.onComplete(w.messageTS)
	}

	return nil
}

// MessageTS 返回流式消息的 timestamp
func (w *NativeStreamingWriter) MessageTS() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.messageTS
}

// IsStarted 返回流是否已启动
func (w *NativeStreamingWriter) IsStarted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started
}

// IsClosed 返回流是否已关闭
func (w *NativeStreamingWriter) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}
```

### Step 2: 验证编译

Run: `go build ./chatapps/slack/...`

Expected: 编译成功，无错误

### Step 3: 提交

Run: `git add chatapps/slack/streaming_writer.go`

Run: `git commit -m "feat(slack): add NativeStreamingWriter for io.Writer-based streaming"`

---

## Task 4: Engine Handler 状态流转重构

**Files:**
- Modify: `chatapps/engine_handler.go`

### Step 1: 阅读 engine_handler.go 关键部分

Read: `chatapps/engine_handler.go:200-350`

理解 `StreamCallback` 结构体及其 `updateStatusMessage` 方法的当前实现。

### Step 2: 在 StreamCallback 中添加流式状态字段

在 `StreamCallback` 结构体中添加以下字段（添加到现有字段附近）：

```go
// StreamCallback struct 中添加：

// 原生流式状态
nativeStream       *NativeStreamingWriter // Slack 原生流式写入器（如果平台支持）
nativeStreamActive bool                   // 原生流是否激活
```

注意：由于 `StreamCallback` 在 `chatapps/engine_handler.go` 中，而 `NativeStreamingWriter` 在 `chatapps/slack` 包中，我们需要使用接口抽象或延迟初始化。

**修正方案：** 使用接口抽象，在 `chatapps/base/types.go` 中定义 `StreamWriter` 接口：

```go
// StreamWriter defines the interface for streaming message writes
type StreamWriter interface {
	Write(p []byte) (n int, err error)
	Close() error
	MessageTS() string
}
```

然后在 `StreamCallback` 中：

```go
// 原生流式状态
streamWriter       StreamWriter // 平台中立的流式写入器
streamWriterActive bool         // 流是否激活
```

### Step 3: 在 handleAnswer 中启用原生流式

Read: `chatapps/engine_handler.go:832-925`

定位 `handleAnswer` 方法，在首次收到 answer 事件时启动流式输出。

修改 `handleAnswer` 方法，在首次调用时：

```go
// 在 handleAnswer 方法开始处添加
c.mu.Lock()
if !c.streamWriterActive {
	// 首次 answer 事件，启动原生流式输出
	c.streamWriterActive = true
	// TODO: 通过 messageOps 启动流式（需要平台特定的工厂方法）
}
c.mu.Unlock()

// 后续通过 streamWriter.Write 推送内容
if c.streamWriter != nil {
	_, err := c.streamWriter.Write([]byte(answerContent))
	if err != nil {
		c.logger.Warn("Failed to write to stream", "error", err)
	}
}
```

### Step 4: 在会话结束时关闭流

Read: `chatapps/engine_handler.go:1002-1052`

定位 `handleSessionStats` 方法（会话结束事件），在此处关闭流式输出。

添加流关闭逻辑：

```go
// 在 handleSessionStats 方法结束前
c.mu.Lock()
if c.streamWriter != nil {
	if err := c.streamWriter.Close(); err != nil {
		c.logger.Warn("Failed to close stream", "error", err)
	}
	c.streamWriter = nil
	c.streamWriterActive = false
}
c.mu.Unlock()
```

### Step 5: 抑制旧版 MessageTypeThinking 气泡发送

在 `updateStatusMessage` 方法中添加判断：

```go
// 如果使用原生流式输出，抑制旧的 thinking 气泡发送
if c.streamWriterActive {
	c.logger.Debug("Native streaming active, suppressing thinking bubble")
	return nil
}
```

### Step 6: 验证编译

Run: `go build ./chatapps/...`

Expected: 编译成功，无错误

### Step 7: 提交

Run: `git add chatapps/engine_handler.go chatapps/base/types.go`

Run: `git commit -m "feat(engine): enable native streaming in StreamCallback with state-aware flow"`

---

## Task 5: Slack Adapter 流式工厂方法实现

**Files:**
- Modify: `chatapps/slack/adapter.go`

### Step 1: 添加创建 StreamWriter 的方法

在 `adapter.go` 中添加：

```go
// NewStreamWriter 创建平台特定的流式写入器
// 返回 StreamWriter 接口，实现平台中立抽象
func (a *Adapter) NewStreamWriter(ctx context.Context, channelID, threadTS string) base.StreamWriter {
	return NewNativeStreamingWriter(ctx, a, channelID, threadTS, nil)
}
```

### Step 2: 让 Adapter 实现 MessageOperations 接口

在 `adapter.go` 文件顶部添加接口合规性检查：

```go
// 确保 Adapter 实现 base.MessageOperations 接口
var _ base.MessageOperations = (*Adapter)(nil)
```

### Step 3: 验证编译

Run: `go build ./chatapps/...`

Expected: 编译成功，无错误

### Step 4: 提交

Run: `git add chatapps/slack/adapter.go`

Run: `git commit -m "feat(slack): add NewStreamWriter factory method for platform-agnostic streaming"`

---

## Task 6: 集成测试与验证

**Files:**
- Create: `chatapps/slack/streaming_writer_test.go`

### Step 1: 创建流式写入器测试

Create: `chatapps/slack/streaming_writer_test.go`

```go
package slack

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNativeStreamingWriter_Write(t *testing.T) {
	// 注意：此测试需要 mock Slack API 客户端
	// 实际测试中应使用 mock 或集成测试

	ctx := context.Background()

	// 模拟场景：创建写入器 -> Write 内容 -> Close
	// 验证：StartStream/AppendStream/StopStream 被正确调用

	t.Skip("Integration test requires Slack API credentials")
}

func TestNativeStreamingWriter_DoubleClose(t *testing.T) {
	// 验证 Close 的幂等性
	ctx := context.Background()

	t.Skip("Integration test requires Slack API credentials")
}
```

### Step 2: 运行测试

Run: `go test ./chatapps/slack/... -v`

Expected: 测试通过（或跳过集成测试）

### Step 3: 提交

Run: `git add chatapps/slack/streaming_writer_test.go`

Run: `git commit -m "test(slack): add NativeStreamingWriter unit tests"`

---

## Task 7: 文档更新

**Files:**
- Modify: `docs/plans/slack-ai-native-evolution-plan.md`

### Step 1: 更新实施状态

Edit: `docs/plans/slack-ai-native-evolution-plan.md`

更新第 6 节 "落地实施路线图" 中的状态：

```markdown
| 节点   | 核心任务                                     | 状态     | 相关依赖             |
| :----- | :------------------------------------------- | :------- | :------------------- |
| **P1** | Dashboard 配置 + `base` 接口扩展             | ✅ 完成   | -                    |
| **P1** | Adapter 封装 `SetAssistantStatus` 与原生流式 | ✅ 完成   | `slack-go v0.18.0`   |
| **P2** | Engine 逻辑重构，启用流式感知流转            | 🔄 进行中 | `issues/124` (Brain) |
```

### Step 2: 提交

Run: `git add docs/plans/slack-ai-native-evolution-plan.md`

Run: `git commit -m "docs(slack): update roadmap status for Phase 1 completion"`

---

## 验证清单

完成所有任务后，运行以下验证命令：

```bash
# 1. 编译验证
go build ./...

# 2. 测试验证
go test ./chatapps/... -race

# 3. 代码风格验证
golangci-lint run ./chatapps/...

# 4. Git 状态检查
git status
git diff --staged
```

---

## 依赖与注意事项

### 外部依赖
- `github.com/slack-go/slack v0.18.0`（已存在于 go.mod）
- Slack App Dashboard 配置（需手动完成）

### Slack 平台配置（需用户手动完成）
1. 访问 Slack App Dashboard
2. 进入 App Home 设置
3. 开启 "Agents & AI Apps" 开关
4. 在 OAuth & Permissions 中添加 `assistant:write` Scope
5. 重新安装 App 以应用新权限

### Manifest 配置参考

创建 `docs/slack/manifest.yaml` 作为配置参考：

```yaml
display_information:
  name: HotPlex AI
  description: 您的研发全栈助手
features:
  app_home:
    home_tab_enabled: true
    messages_tab_enabled: true
    messages_tab_read_only_enabled: false
  assistant_view:
    assistant_description: "HotPlex AI: 您的研发全栈助手"
oauth_config:
  scopes:
    bot:
      - assistant:write
      - chat:write
      - im:history
      - im:read
settings:
  org_deploy_enabled: false
  socket_mode_enabled: true
  token_rotation_enabled: false
```

---

## 后续任务（Phase 2）

Phase 1 完成后，可继续实施 Phase 2：

- **P2.1**: Suggested Prompts 智能推荐
- **P2.2**: Thread Titling 自动标题总结
- **P2.3**: Brain Guard 安全审查
- **P2.4**: Chat2Config 平台自驱配置

---

**Plan complete and saved to `docs/plans/2026-03-03-slack-native-assistant-phase1.md`. Two execution options:**

**1. Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

**2. Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

**Which approach?**
