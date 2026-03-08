# Storage Plugin / 存储插件

<!-- LANGUAGES: English | [简体中文](#简体中文) -->

A production-grade message storage plugin for HotPlex ChatApps, supporting SQLite, PostgreSQL, and in-memory backends with streaming message buffering.

---

## Architecture

```mermaid
graph TD
    subgraph ChatApps Layer
        Adapter[ChatAdapter]
        Plugin[MessageStorePlugin]
        Transformer[MessageTransformer]
    end

    subgraph Storage Layer
        Interface[ChatAppMessageStore]
        WriteOnly[WriteOnlyStore]
        ReadOnly[ReadOnlyStore]
        Session[SessionStore]
        Memory[MemoryStore]
        SQLite[SQLiteStore]
        PostgreSQL[PostgreSQLStore]
    end

    Adapter --> Plugin
    Plugin --> Transformer
    Transformer --> Interface
    Interface --> WriteOnly
    Interface --> ReadOnly
    Interface --> Session
    WriteOnly --> Memory
    WriteOnly --> SQLite
    WriteOnly --> PostgreSQL
```

## Features

- **Multi-Backend Support**: SQLite (L1), PostgreSQL (L2), In-Memory
- **Streaming Buffer**: Memory-efficient buffering for LLM token streams
- **Fallback Strategy**: Automatic direct storage when buffer overflows
- **Retry Mechanism**: Exponential backoff for transient failures
- **ISP-Compliant Interfaces**: `ReadOnlyStore`, `WriteOnlyStore`, `SessionStore`
- **Soft Delete**: Messages marked as deleted, not physically removed
- **Session Metadata**: Track last message, message count per session

## New: Reliability Features

### Fallback Storage

When stream buffer is full and no expired buffers can be evicted, the system automatically falls back to direct storage:

```go
// When buffer overflows, log warning and store directly
logger.Warn("stream buffer full, falling back to direct storage",
    "max_buffers", 2,
    "session_id", sessionID,
    "fallback", "direct_store")
return s.store.StoreBotResponse(ctx, &ChatAppMessage{...})
```

### Retry Mechanism

Built-in retry with exponential backoff for storage failures:

```go
// Configuration
RetryConfig{
    MaxAttempts:  3,
    InitialDelay: 100ms,
    MaxDelay:     2s,
    Multiplier:   2.0,
}

// Automatic retry on failure
withRetry(ctx, logger, "StoreUserMessage", func() error {
    return store.StoreUserMessage(ctx, msg)
})
```

## Quick Start

### 1. Create Storage Backend

```go
import "github.com/hrygo/hotplex/plugins/storage"

// SQLite (recommended for edge deployments)
cfg := storage.SQLiteConfig{
    Path:      "~/.hotplex/messages.db",
    MaxSizeMB: 512,
}
store, err := storage.NewSQLiteStore(cfg)

// PostgreSQL (recommended for production)
cfg := storage.PostgresConfig{
    DSN:            "postgres://user:pass@localhost:5432/hotplex",
    MaxConnections: 10,
    Level:          1, // 1=million, 2=hundred million
}
store, err := storage.NewPostgreSQLStore(cfg)

// In-Memory (for testing)
store := storage.NewMemoryStore()
```

### 2. Store Messages

```go
// Store user message
msg := &storage.ChatAppMessage{
    ChatSessionID: "slack:U123:U456:C789:TS123",
    ChatPlatform:  "slack",
    ChatUserID:    "U123",
    MessageType:   types.MessageTypeAnswer,
    Content:       "Hello, bot!",
    CreatedAt:     time.Now(),
}
err := store.StoreUserMessage(ctx, msg)

// Store bot response
botMsg := &storage.ChatAppMessage{
    ChatSessionID: "slack:U123:U456:C789:TS123",
    MessageType:   types.MessageTypeAnswer,
    Content:       "Hello! How can I help?",
    CreatedAt:     time.Now(),
}
err := store.StoreBotResponse(ctx, botMsg)
```

### 3. Query Messages

```go
query := &storage.MessageQuery{
    ChatSessionID: "slack:U123:U456:C789:TS123",
    Limit:         50,
    Ascending:     true,
}
messages, err := store.List(ctx, query)
```

## Configuration

### YAML Configuration

```yaml
message_store:
  enabled: true
  type: sqlite          # sqlite | postgres | memory
  sqlite:
    path: ~/.hotplex/chatapp_messages.db
    max_size_mb: 512
  postgres:
    dsn: postgres://user:pass@localhost:5432/hotplex
    max_connections: 10
    level: 1
  strategy: default     # default | verbose | minimal
  streaming:
    enabled: true
    timeout: 5m
    max_buffers: 1000
    storage_policy: complete_only  # complete_only | all_chunks
  retry:
    max_attempts: 3
    initial_delay: 100ms
    max_delay: 2s
    multiplier: 2.0
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `HOTPLEX_MESSAGE_STORE_TYPE` | Storage backend type | `memory` |
| `HOTPLEX_MESSAGE_STORE_SQLITE_PATH` | SQLite database path | `~/.hotplex/messages.db` |
| `HOTPLEX_MESSAGE_STORE_POSTGRES_DSN` | PostgreSQL connection string | - |

## Interfaces

### ISP-Compliant Design

```go
// Read-only operations
type ReadOnlyStore interface {
    Get(ctx context.Context, messageID string) (*ChatAppMessage, error)
    List(ctx context.Context, query *MessageQuery) ([]*ChatAppMessage, error)
    Count(ctx context.Context, query *MessageQuery) (int64, error)
}

// Write-only operations (minimal interface for StreamMessageStore)
type WriteOnlyStore interface {
    StoreUserMessage(ctx context.Context, msg *ChatAppMessage) error
    StoreBotResponse(ctx context.Context, msg *ChatAppMessage) error
}

// Session metadata operations
type SessionStore interface {
    GetSessionMeta(ctx context.Context, chatSessionID string) (*SessionMeta, error)
    ListUserSessions(ctx context.Context, platform, userID string) ([]string, error)
    DeleteSession(ctx context.Context, chatSessionID string) error
}

// Combined interface
type ChatAppMessageStore interface {
    ReadOnlyStore
    WriteOnlyStore
    SessionStore
    Initialize(ctx context.Context) error
    Close() error
}
```

## Streaming Support

The streaming buffer prevents database I/O thrashing by accumulating chunks in memory and persisting only the final merged content.

### Buffer Overflow Handling

```go
// When buffer is full (> max_buffers):
// 1. Try to evict expired buffers
// 2. If no expired buffers, fall back to direct storage
// 3. Log warning for observability

func (s *StreamMessageStore) OnStreamChunk(ctx context.Context, sessionID, chunk string) error {
    if len(s.buffers) >= s.maxBuffers {
        // Try evict expired buffers
        for id, buf := range s.buffers {
            if buf.IsExpired(s.timeout) {
                delete(s.buffers, id)
                evicted = true
                break
            }
        }
        // Fall back to direct storage if no eviction
        if !evicted {
            return s.store.StoreBotResponse(ctx, &ChatAppMessage{
                ChatSessionID: sessionID,
                Content:       chunk,
            })
        }
    }
    // Normal buffering
    buf.Append(chunk)
    return nil
}
```

### Stream Completion

```go
// When stream completes:
// 1. Merge all chunks into single message
// 2. Store merged content
// 3. Clean up buffer

func (s *StreamMessageStore) OnStreamComplete(ctx context.Context, sessionID string, msg *ChatAppMessage) error {
    mergedContent := buf.Merge()
    msg.Content = mergedContent
    
    err := s.store.StoreBotResponse(ctx, msg)
    
    // Clean up buffer after successful storage
    delete(s.buffers, sessionID)
    
    return err
}
```

## Data Model

```go
type ChatAppMessage struct {
    ID                string
    ChatSessionID     string
    ChatPlatform      string
    ChatUserID        string
    ChatBotUserID     string
    ChatChannelID     string
    ChatThreadID      string
    EngineSessionID   uuid.UUID
    EngineNamespace   string
    ProviderSessionID string
    ProviderType      string
    MessageType       types.MessageType
    FromUserID        string
    FromUserName      string
    ToUserID          string
    Content           string
    Metadata          map[string]any
    CreatedAt         time.Time
    UpdatedAt         time.Time
    Deleted           bool
    DeletedAt         *time.Time
}
```

## Testing

```bash
# Run all storage tests
go test -v ./plugins/storage/...

# Run with race detection
go test -race ./plugins/storage/...

# Run specific backend tests
go test -v ./plugins/storage/... -run SQLite
go test -v ./plugins/storage/... -run PostgreSQL

# Run ChatApp storage integration tests
go test -v ./chatapps/base/... -run E2E
go test -v ./chatapps/slack/... -run Storage
```

---

<a name="简体中文"></a>

# 存储插件

[English](#storage-plugin--存储插件) | 简体中文

HotPlex ChatApps 的生产级消息存储插件，支持 SQLite、PostgreSQL 和内存后端，具备流式消息缓冲。

---

## 架构

```mermaid
graph TD
    subgraph ChatApps 层
        Adapter[ChatAdapter]
        Plugin[MessageStorePlugin]
        Transformer[MessageTransformer]
    end

    subgraph 存储层
        Interface[ChatAppMessageStore]
        WriteOnly[WriteOnlyStore]
        ReadOnly[ReadOnlyStore]
        Session[SessionStore]
        Memory[MemoryStore]
        SQLite[SQLiteStore]
        PostgreSQL[PostgreSQLStore]
    end

    Adapter --> Plugin
    Plugin --> Transformer
    Transformer --> Interface
    Interface --> WriteOnly
    Interface --> ReadOnly
    Interface --> Session
    WriteOnly --> Memory
    WriteOnly --> SQLite
    WriteOnly --> PostgreSQL
```

## 特性

- **多后端支持**: SQLite (L1)、PostgreSQL (L2)、内存
- **流式缓冲**: LLM token 流的内存高效缓冲
- **降级策略**: 缓冲区溢出时自动降级为直接存储
- **重试机制**: 指数退避的存储失败重试
- **ISP 合规接口**: `ReadOnlyStore`、`WriteOnlyStore`、`SessionStore`
- **软删除**: 消息标记删除，非物理删除
- **会话元数据**: 追踪最近消息、每会话消息计数

## 新增：可靠性特性

### 降级存储

当流缓冲区满且无过期缓冲可清理时，系统自动降级为直接存储：

```go
// 缓冲区溢出时，记录警告并直接存储
logger.Warn("stream buffer full, falling back to direct storage",
    "max_buffers", 2,
    "session_id", sessionID,
    "fallback", "direct_store")
return s.store.StoreBotResponse(ctx, &ChatAppMessage{...})
```

### 重试机制

内置存储失败的指数退避重试：

```go
// 配置
RetryConfig{
    MaxAttempts:  3,
    InitialDelay: 100ms,
    MaxDelay:     2s,
    Multiplier:   2.0,
}

// 失败时自动重试
withRetry(ctx, logger, "StoreUserMessage", func() error {
    return store.StoreUserMessage(ctx, msg)
})
```

## 快速开始

### 1. 创建存储后端

```go
import "github.com/hrygo/hotplex/plugins/storage"

// SQLite (推荐边缘部署)
cfg := storage.SQLiteConfig{
    Path:      "~/.hotplex/messages.db",
    MaxSizeMB: 512,
}
store, err := storage.NewSQLiteStore(cfg)

// PostgreSQL (推荐生产环境)
cfg := storage.PostgresConfig{
    DSN:            "postgres://user:pass@localhost:5432/hotplex",
    MaxConnections: 10,
    Level:          1, // 1=百万级, 2=亿级
}
store, err := storage.NewPostgreSQLStore(cfg)

// 内存 (用于测试)
store := storage.NewMemoryStore()
```

### 2. 存储消息

```go
// 存储用户消息
msg := &storage.ChatAppMessage{
    ChatSessionID: "slack:U123:U456:C789:TS123",
    ChatPlatform:  "slack",
    ChatUserID:    "U123",
    MessageType:   types.MessageTypeAnswer,
    Content:       "你好，机器人！",
    CreatedAt:     time.Now(),
}
err := store.StoreUserMessage(ctx, msg)

// 存储 Bot 响应
botMsg := &storage.ChatAppMessage{
    ChatSessionID: "slack:U123:U456:C789:TS123",
    MessageType:   types.MessageTypeAnswer,
    Content:       "你好！有什么可以帮你的？",
    CreatedAt:     time.Now(),
}
err := store.StoreBotResponse(ctx, botMsg)
```

### 3. 查询消息

```go
query := &storage.MessageQuery{
    ChatSessionID: "slack:U123:U456:C789:TS123",
    Limit:         50,
    Ascending:     true,
}
messages, err := store.List(ctx, query)
```

## 配置

### YAML 配置

```yaml
message_store:
  enabled: true
  type: sqlite          # sqlite | postgres | memory
  sqlite:
    path: ~/.hotplex/chatapp_messages.db
    max_size_mb: 512
  postgres:
    dsn: postgres://user:pass@localhost:5432/hotplex
    max_connections: 10
    level: 1
  strategy: default     # default | verbose | minimal
  streaming:
    enabled: true
    timeout: 5m
    max_buffers: 1000
    storage_policy: complete_only  # complete_only | all_chunks
  retry:
    max_attempts: 3
    initial_delay: 100ms
    max_delay: 2s
    multiplier: 2.0
```

### 环境变量

| 变量 | 描述 | 默认值 |
|------|------|--------|
| `HOTPLEX_MESSAGE_STORE_TYPE` | 存储后端类型 | `memory` |
| `HOTPLEX_MESSAGE_STORE_SQLITE_PATH` | SQLite 数据库路径 | `~/.hotplex/messages.db` |
| `HOTPLEX_MESSAGE_STORE_POSTGRES_DSN` | PostgreSQL 连接字符串 | - |

## 接口

### ISP 合规设计

```go
// 只读操作
type ReadOnlyStore interface {
    Get(ctx context.Context, messageID string) (*ChatAppMessage, error)
    List(ctx context.Context, query *MessageQuery) ([]*ChatAppMessage, error)
    Count(ctx context.Context, query *MessageQuery) (int64, error)
}

// 只写操作 (StreamMessageStore 使用的最小接口)
type WriteOnlyStore interface {
    StoreUserMessage(ctx context.Context, msg *ChatAppMessage) error
    StoreBotResponse(ctx context.Context, msg *ChatAppMessage) error
}

// 会话元数据操作
type SessionStore interface {
    GetSessionMeta(ctx context.Context, chatSessionID string) (*SessionMeta, error)
    ListUserSessions(ctx context.Context, platform, userID string) ([]string, error)
    DeleteSession(ctx context.Context, chatSessionID string) error
}

// 组合接口
type ChatAppMessageStore interface {
    ReadOnlyStore
    WriteOnlyStore
    SessionStore
    Initialize(ctx context.Context) error
    Close() error
}
```

## 流式支持

流式缓冲通过在内存中累积块并仅持久化最终合并内容，防止数据库 I/O 抖动。

### 缓冲区溢出处理

```go
// 当缓冲区满 (> max_buffers):
// 1. 尝试驱逐过期缓冲
// 2. 无过期缓冲则降级为直接存储
// 3. 记录警告日志

func (s *StreamMessageStore) OnStreamChunk(ctx context.Context, sessionID, chunk string) error {
    if len(s.buffers) >= s.maxBuffers {
        // 尝试驱逐过期缓冲
        for id, buf := range s.buffers {
            if buf.IsExpired(s.timeout) {
                delete(s.buffers, id)
                evicted = true
                break
            }
        }
        // 无法驱逐则降级为直接存储
        if !evicted {
            return s.store.StoreBotResponse(ctx, &ChatAppMessage{
                ChatSessionID: sessionID,
                Content:       chunk,
            })
        }
    }
    // 正常缓冲
    buf.Append(chunk)
    return nil
}
```

### 流完成处理

```go
// 流完成时:
// 1. 合并所有块为单条消息
// 2. 存储合并内容
// 3. 清理缓冲

func (s *StreamMessageStore) OnStreamComplete(ctx context.Context, sessionID string, msg *ChatAppMessage) error {
    mergedContent := buf.Merge()
    msg.Content = mergedContent
    
    err := s.store.StoreBotResponse(ctx, msg)
    
    // 成功存储后清理缓冲
    delete(s.buffers, sessionID)
    
    return err
}
```

## 数据模型

```go
type ChatAppMessage struct {
    ID                string
    ChatSessionID     string    // 会话 ID
    ChatPlatform      string    // 平台 (slack/feishu 等)
    ChatUserID        string    // 用户 ID
    ChatBotUserID     string    // Bot 用户 ID
    ChatChannelID     string    // 频道 ID
    ChatThreadID      string    // 线程 ID
    EngineSessionID   uuid.UUID // 引擎会话 ID
    EngineNamespace   string    // 引擎命名空间
    ProviderSessionID string    // 提供商会话 ID
    ProviderType      string    // 提供商类型
    MessageType       types.MessageType
    FromUserID        string
    FromUserName      string
    ToUserID          string
    Content           string
    Metadata          map[string]any
    CreatedAt         time.Time
    UpdatedAt         time.Time
    Deleted           bool
    DeletedAt         *time.Time
}
```

## 测试

```bash
# 运行所有存储测试
go test -v ./plugins/storage/...

# 带竞态检测
go test -race ./plugins/storage/...

# 运行特定后端测试
go test -v ./plugins/storage/... -run SQLite
go test -v ./plugins/storage/... -run PostgreSQL

# 运行 ChatApp 存储集成测试
go test -v ./chatapps/base/... -run E2E
go test -v ./chatapps/slack/... -run Storage
```

---

**状态**: 生产就绪
**维护者**: HotPlex Core Team
**版本**: v1.1 (2026-03-08)
