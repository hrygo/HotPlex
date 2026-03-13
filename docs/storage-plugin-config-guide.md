# HotPlex ChatApp 消息存储插件配置指南

## 概述

消息存储插件支持三种存储后端：
- **Memory**: 开发/测试环境
- **SQLite**: 小规模生产环境 (<1000万消息)
- **PostgreSQL**: 大规模生产环境 (亿级消息，分区表)

## 快速开始

### 1. 启用消息存储

在 `config.yaml` 中添加：

```yaml
message_store:
  enabled: true
  type: sqlite  # 或 postgres, memory
```

### 2. SQLite 配置 (Level 1: <10M 行)

```yaml
message_store:
  enabled: true
  type: sqlite
  sqlite:
    path: ~/.config/hotplex/chatapp_messages.db
    max_size_mb: 512
```

### 3. PostgreSQL 配置 (Level 2: 亿级)

```yaml
message_store:
  enabled: true
  type: postgres
  postgres:
    host: localhost
    port: 5432
    user: hotplex
    password: ${POSTGRES_PASSWORD}
    database: hotplex
    ssl_mode: disable
    max_open_conns: 25
    max_idle_conns: 5
    max_lifetime: 300
```

#### PostgreSQL 表结构 (自动创建)

插件会自动创建以下表：

```sql
-- 消息表 (支持分区)
CREATE TABLE messages (
  id VARCHAR(64) NOT NULL,
  chat_session_id VARCHAR(128) NOT NULL,
  chat_platform VARCHAR(32) NOT NULL,
  chat_user_id VARCHAR(128) NOT NULL,
  chat_bot_user_id VARCHAR(128),
  chat_channel_id VARCHAR(128),
  chat_thread_id VARCHAR(128),
  engine_session_id UUID NOT NULL,
  engine_namespace VARCHAR(128),
  provider_session_id VARCHAR(128),
  provider_type VARCHAR(32),
  message_type VARCHAR(32) NOT NULL,
  from_user_id VARCHAR(128),
  from_user_name VARCHAR(256),
  to_user_id VARCHAR(128),
  content TEXT,
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  deleted BOOLEAN DEFAULT FALSE,
  deleted_at TIMESTAMPTZ,
  PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 会话元数据表
CREATE TABLE session_meta (
  chat_session_id VARCHAR(128) PRIMARY KEY,
  chat_platform VARCHAR(32) NOT NULL,
  chat_user_id VARCHAR(128) NOT NULL,
  last_message_id VARCHAR(64),
  last_message_at TIMESTAMPTZ,
  message_count BIGINT DEFAULT 0,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

## 存储策略

### 默认策略 (default)

只存储白名单消息类型：
- `MessageTypeUserInput`: 用户输入
- `MessageTypeFinalResponse`: 最终响应

### 调试策略 (debug)

存储所有消息类型，用于调试。

```yaml
message_store:
  strategy: debug
```

## 流式消息配置

对于 AI 流式输出，配置缓冲策略：

```yaml
message_store:
  streaming:
    enabled: true
    buffer_size: 100      # 最大缓冲消息数
    timeout_seconds: 300 # 缓冲超时时间
    storage_policy: complete_only  # 只存储最终合并结果
```

### 策略说明

- `complete_only`: 只存储流式消息的最终合并结果（推荐）
- `all_chunks`: 存储所有流式 chunk

## 性能优化

### PostgreSQL 分区表

对于亿级消息，建议按月分区：

```sql
-- 创建分区表
CREATE TABLE messages_y2025m01 PARTITION OF messages
    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');

CREATE TABLE messages_y2025m02 PARTITION OF messages
    FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
```

### 索引优化

插件自动创建以下索引：
- `idx_messages_session`: (chat_session_id, created_at DESC)
- `idx_messages_user`: (chat_platform, chat_user_id)
- `idx_messages_engine`: (engine_session_id)
- `idx_messages_provider`: (provider_session_id)
- `idx_messages_type`: (message_type)
- `idx_messages_metadata`: GIN (metadata)

## 监控

### 会话元数据查询

```sql
-- 查看用户会话列表
SELECT * FROM session_meta 
WHERE chat_platform = 'slack' AND chat_user_id = 'U123456';

-- 查看会话消息统计
SELECT chat_session_id, message_count, last_message_at 
FROM session_meta 
ORDER BY last_message_at DESC 
LIMIT 10;
```

### 连接池监控

监控 PostgreSQL 连接池状态：
- `max_open_conns`: 最大打开连接数
- `max_idle_conns`: 最大空闲连接数
- `max_lifetime`: 连接最大生命周期

## 故障排除

### 连接失败

检查网络和凭据：
```bash
psql -h localhost -U hotplex -d hotplex
```

### 性能问题

1. 检查索引是否创建
2. 调整连接池参数
3. 考虑使用分区表

### 数据清理

软删除会话：
```sql
-- 软删除
UPDATE messages SET deleted = TRUE, deleted_at = NOW() 
WHERE chat_session_id = 'session-123';

-- 物理删除 (谨慎)
DELETE FROM messages WHERE deleted = TRUE AND deleted_at < NOW() - INTERVAL '30 days';
```
