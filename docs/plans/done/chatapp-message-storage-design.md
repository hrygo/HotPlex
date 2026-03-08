# ChatApp 消息存储表结构设计

_版本：v3.0 (参考文档) | 最后更新：2026-03-03 | 作者：探云_

> **注意**: 本文档为存储表结构参考设计，完整插件化集成方案请参考 [`hotplex-storage-plugin-design.md`](./hotplex-storage-plugin-design.md)

---

## 📋 目录

- [概述](#概述)
- [核心设计原则](#核心设计原则)
- [SessionID 架构](#sessionid-架构)
- [Level 1: 百万级表结构](#level-1-百万级表结构)
- [Level 2: 亿级表结构](#level-2-亿级表结构)
- [索引策略](#索引策略)

---

## 概述

本文档定义 ChatApp 消息存储的 **SQL 表结构设计**，支持从百万级到亿级的平滑扩展。

### 核心目标

| 目标 | 说明 |
|------|------|
| **SessionID 服务于消息** | SessionID 字段直接嵌入消息表 |
| **减少 JOIN** | 所有查询单表完成 |
| **UUIDv5 确定性** | ChatApp 层和 Provider 层支持重复定位 |
| **平滑扩展** | 单表 (L1) → 分区表 (L2) |

---

## 核心设计原则

1. **SessionID 嵌入消息表** - 无需单独映射表，减少 JOIN
2. **三层 SessionID 架构** - ChatApp/Engine/Provider 各层标识完整存储
3. **辅助表优化** - `session_metadata` 缓存会话状态，避免 COUNT 聚合

---

## SessionID 架构

### 三层 SessionID

| 层级 | 字段名 | 格式 | 生成方式 |
|------|--------|------|---------|
| **ChatApp 层** | `chat_session_id` | TEXT (UUIDv5) | 确定性，相同输入=相同 ID |
| **Engine 层** | `engine_session_id` | UUID (UUIDv4) | 随机 |
| **Provider 层** | `provider_session_id` | TEXT (UUIDv5) | 确定性，相同输入=相同 ID |

### UUIDv5 生成规则

```go
// Chat SessionID
chat_session_id = UUID5(
    namespace="hotplex",
    input="hotplex:session:{platform}:{user_id}:{bot_user_id}:{channel_id}:{thread_id}"
)

// Provider SessionID
provider_session_id = UUID5(
    namespace="hotplex",
    input="hotplex:provider:{provider_type}:{engine_session_id}"
)
```

---

## Level 1: 百万级表结构

### messages 消息主表

```sql
-- messages 消息主表 (单表，目标 <1000 万行)
CREATE TABLE messages (
    -- ========== 消息主键 ==========
    id TEXT PRIMARY KEY,                    -- 消息唯一 ID (雪花 ID/UUIDv4)
    
    -- ========== ChatApp 层 SessionID (嵌入) ==========
    chat_session_id TEXT NOT NULL,          -- ChatApp 层会话 ID (UUIDv5 确定性)
    chat_platform TEXT NOT NULL,            -- 平台：slack/telegram/dingtalk/feishu/discord/whatsapp
    chat_user_id TEXT NOT NULL,             -- 平台用户 ID
    chat_bot_user_id TEXT,                  -- 机器人用户 ID (多机器人场景)
    chat_channel_id TEXT,                   -- 频道/房间 ID (DM 为空)
    chat_thread_id TEXT,                    -- 线程/话题 ID (可选)
    
    -- ========== Engine 层 SessionID (嵌入) ==========
    engine_session_id UUID NOT NULL,        -- HotPlex Engine 内部 session ID (UUIDv4)
    engine_namespace TEXT NOT NULL DEFAULT 'hotplex',
    
    -- ========== Provider 层 SessionID (嵌入) ==========
    provider_session_id TEXT NOT NULL,      -- Provider 持久化会话 ID (UUIDv5)
    provider_type TEXT NOT NULL,            -- Provider 类型：claude-code/opencode/codex
    
    -- ========== 消息内容 ==========
    message_type TEXT NOT NULL DEFAULT 'text',  -- 消息类型：text/image/file/system
    from_user_id TEXT NOT NULL,                 -- 发送者 ID
    from_user_name TEXT,                        -- 发送者名称 (冗余，减少 JOIN)
    to_user_id TEXT,                            -- 接收者 ID (群聊为 NULL)
    content TEXT NOT NULL,                      -- 消息正文
    metadata JSONB,                             -- 扩展元数据 (结构化存储)
    
    -- ========== 时间戳 ==========
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    
    -- ========== 软删除标记 ==========
    deleted BOOLEAN DEFAULT FALSE,
    deleted_at TIMESTAMPTZ
);
```

### session_metadata 会话元数据表

```sql
-- session_metadata 会话元数据表 (缓存会话状态，避免 COUNT 查询)
CREATE TABLE session_metadata (
    chat_session_id TEXT PRIMARY KEY,
    chat_platform TEXT NOT NULL,
    chat_user_id TEXT NOT NULL,
    last_message_id TEXT,
    last_message_at TIMESTAMPTZ,
    message_count INTEGER DEFAULT 0,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

### 触发器：自动更新会话元数据

```sql
CREATE OR REPLACE FUNCTION update_session_metadata()
RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' AND NEW.deleted = FALSE THEN
        INSERT INTO session_metadata (
            chat_session_id, chat_platform, chat_user_id,
            last_message_id, last_message_at, message_count
        )
        VALUES (
            NEW.chat_session_id, NEW.chat_platform, NEW.chat_user_id,
            NEW.id, NEW.created_at, 1
        )
        ON CONFLICT (chat_session_id) DO UPDATE SET
            last_message_id = EXCLUDED.last_message_id,
            last_message_at = EXCLUDED.last_message_at,
            message_count = session_metadata.message_count + 1,
            updated_at = NOW();
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_session_metadata
    AFTER INSERT ON messages
    FOR EACH ROW
    EXECUTE FUNCTION update_session_metadata();
```

---

## Level 2: 亿级表结构

### 分区表设计

```sql
-- messages_partitioned 分区表父表 (按月分区)
CREATE TABLE messages_partitioned (
    id TEXT NOT NULL,
    chat_session_id TEXT NOT NULL,
    chat_platform TEXT NOT NULL,
    chat_user_id TEXT NOT NULL,
    chat_bot_user_id TEXT,
    chat_channel_id TEXT,
    chat_thread_id TEXT,
    engine_session_id UUID NOT NULL,
    engine_namespace TEXT NOT NULL DEFAULT 'hotplex',
    provider_session_id TEXT NOT NULL,
    provider_type TEXT NOT NULL,
    message_type TEXT NOT NULL DEFAULT 'text',
    from_user_id TEXT NOT NULL,
    from_user_name TEXT,
    to_user_id TEXT,
    content TEXT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted BOOLEAN DEFAULT FALSE,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 创建分区 (自动化脚本每月执行)
CREATE TABLE messages_2026_03 
    PARTITION OF messages_partitioned
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');

CREATE TABLE messages_2026_04 
    PARTITION OF messages_partitioned
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');

-- 默认分区 (捕获未来数据)
CREATE TABLE messages_default 
    PARTITION OF messages_partitioned DEFAULT;
```

---

## 索引策略

### Level 1 索引

```sql
-- 主查询索引 (会话 + 时间，最常用)
CREATE INDEX idx_messages_chat_session_created 
    ON messages(chat_session_id, created_at DESC);

-- Engine 层查询
CREATE INDEX idx_messages_engine_session 
    ON messages(engine_session_id, created_at DESC);

-- Provider 层查询
CREATE INDEX idx_messages_provider_session 
    ON messages(provider_type, provider_session_id, created_at DESC);

-- JSONB 元数据索引
CREATE INDEX idx_messages_metadata 
    ON messages USING GIN(metadata);
```

### Level 2 索引 (分区表)

```sql
-- 分区表索引 (在每个分区上自动创建)
CREATE INDEX idx_part_chat_session_created 
    ON messages_partitioned(chat_session_id, created_at DESC);

CREATE INDEX idx_part_engine_session 
    ON messages_partitioned(engine_session_id, created_at DESC);

CREATE INDEX idx_part_provider_session 
    ON messages_partitioned(provider_type, provider_session_id, created_at DESC);
```

---

_本文档为存储表结构参考设计，完整插件化集成方案请参考 [`hotplex-storage-plugin-design.md`](./hotplex-storage-plugin-design.md)_

_版本：v3.0 (参考文档)，最后更新：2026-03-03_
