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
    end

    subgraph Storage Layer
        Interface[ChatAppMessageStore]
        Memory[MemoryStore]
        SQLite[SQLiteStore]
        PostgreSQL[PostgreSQLStore]
    end

    Adapter --> Plugin
    Plugin --> Interface
    Interface --> Memory
    Interface --> SQLite
    Interface --> PostgreSQL
```

## Features

- **Multi-Backend Support**: SQLite (L1), PostgreSQL (L2), In-Memory
- **Streaming Buffer**: Memory-efficient buffering for LLM token streams
- **ISP-Compliant Interfaces**: `ReadOnlyStore`, `WriteOnlyStore`, `SessionStore`
- **Soft Delete**: Messages marked as deleted, not physically removed
- **Session Metadata**: Track last message, message count per session

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
    storage_policy: complete_only  # complete_only | all_chunks
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

// Write-only operations
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
    Close() error
}
```

## Streaming Support

The streaming buffer prevents database I/O thrashing by accumulating chunks in memory and persisting only the final merged content.

```go
// Stream buffer accumulates chunks
streamStore := storage.NewStreamMessageStore(5 * time.Minute)

// Add chunks
streamStore.AddChunk(sessionID, "Hello")
streamStore.AddChunk(sessionID, ", ")
streamStore.AddChunk(sessionID, "World!")

// Get merged content (final message)
merged := streamStore.GetMergedContent(sessionID)
// merged == "Hello, World!"
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
    end

    subgraph 存储层
        Interface[ChatAppMessageStore]
        Memory[MemoryStore]
        SQLite[SQLiteStore]
        PostgreSQL[PostgreSQLStore]
    end

    Adapter --> Plugin
    Plugin --> Interface
    Interface --> Memory
    Interface --> SQLite
    Interface --> PostgreSQL
```

## 特性

- **多后端支持**: SQLite (L1)、PostgreSQL (L2)、内存
- **流式缓冲**: LLM token 流的内存高效缓冲
- **ISP 合规接口**: `ReadOnlyStore`、`WriteOnlyStore`、`SessionStore`
- **软删除**: 消息标记删除，非物理删除
- **会话元数据**: 追踪最近消息、每会话消息计数

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
    storage_policy: complete_only  # complete_only | all_chunks
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

// 只写操作
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
    Close() error
}
```

## 流式支持

流式缓冲通过在内存中累积块并仅持久化最终合并内容，防止数据库 I/O 抖动。

```go
// 流缓冲累积块
streamStore := storage.NewStreamMessageStore(5 * time.Minute)

// 添加块
streamStore.AddChunk(sessionID, "你好")
streamStore.AddChunk(sessionID, "，")
streamStore.AddChunk(sessionID, "世界！")

// 获取合并内容 (最终消息)
merged := streamStore.GetMergedContent(sessionID)
// merged == "你好，世界！"
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
```

---

**状态**: 生产就绪
**维护者**: HotPlex Core Team
