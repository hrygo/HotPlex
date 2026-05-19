# 迁移手册：Cache Token Accounting & 物化 Turns 表

**适用版本**: v1.16.0（分支 `fix/cache-token-accounting`）
**迁移类型**: 数据库自动迁移 + API Breaking Change

---

## 1. 概述

### 变更动机

| 问题 | 旧方案 | 新方案 |
|------|--------|--------|
| 性能瓶颈 | `v_turns` 视图每行 13+ `json_extract` 调用 | 物化 `turns` 表，0 次解析 |
| Token 计费不准 | `tokens_in` 仅统计 `input_tokens` | 拆分为 `tokens_input` + `tokens_cache_write` + `tokens_cache_read` |
| 历史分页低效 | 基于 AEP `seq` 的游标 | 基于 `turns.id`（AUTOINCREMENT PK）的游标 |

### 影响范围

- 数据库：新增 `turns` 表，删除 3 个视图（`v_turns`、`v_turns_assistant`、`v_turns_user`）
- API：History 端点参数变更
- JSON Schema：`TurnRecord` 字段变更
- 配置：新增 `events.retention`
- Go 接口：`EventTx`、`TurnQuerier`、`BridgeDeps` 扩展

---

## 2. 数据库迁移

### 自动执行

Goose 迁移在 Gateway 启动时自动运行：

- **删除**：`003_create_turns_view.sql`（旧视图定义，已从代码库移除）
- **新增**：`009_create_turns_table.sql`（物化 turns 表）

```sql
-- 009 自动创建的表结构
CREATE TABLE turns (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id          TEXT    NOT NULL,
    generation          INTEGER NOT NULL DEFAULT 1,
    turn_num            INTEGER NOT NULL,
    seq                 INTEGER NOT NULL DEFAULT 0,
    role                TEXT    NOT NULL,            -- 'user' | 'assistant'
    content             TEXT    NOT NULL DEFAULT '',
    platform            TEXT    NOT NULL DEFAULT '',
    user_id             TEXT    NOT NULL DEFAULT '',
    model               TEXT    NOT NULL DEFAULT '',
    success             INTEGER,                    -- NULL for user turns
    source              TEXT    NOT NULL DEFAULT 'normal',
    tools_json          TEXT,
    tool_count          INTEGER NOT NULL DEFAULT 0,
    tokens_input        INTEGER NOT NULL DEFAULT 0,
    tokens_cache_write  INTEGER NOT NULL DEFAULT 0,
    tokens_cache_read   INTEGER NOT NULL DEFAULT 0,
    tokens_out          INTEGER NOT NULL DEFAULT 0,
    duration_ms         INTEGER NOT NULL DEFAULT 0,
    cost_usd            REAL    NOT NULL DEFAULT 0.0,
    created_at          INTEGER NOT NULL            -- Unix ms
);
```

### 历史数据说明

**重要**：旧视图数据在部署后丢失。`turns` 表从空开始，仅记录新 session 的对话轮次。

- 旧的 `events` 表数据**不受影响**，仍可通过 Events API 查询
- 旧的 `v_turns`、`v_turns_assistant`、`v_turns_user` 视图定义被删除（migration 003 removed）
- 视图在 DB 中残留为孤儿对象，不影响功能，可手动清理：

```sql
-- 可选：手动清理残留视图
DROP VIEW IF EXISTS v_turns;
DROP VIEW IF EXISTS v_turns_assistant;
DROP VIEW IF EXISTS v_turns_user;
```

### Generation 机制

`generation` 是单调递增计数器，用于 session reset 场景：

- 新 session：`generation = 1`
- `/reset` 后：`generation` 递增
- `turn_num` 在每个 generation 内从 1 重新开始
- 历史查询默认返回最新 generation 的数据

---

## 3. API Breaking Changes

### 3.1 History 端点参数变更

**端点**：`GET /api/sessions/{id}/history`

| 参数 | 旧版本 | 新版本 |
|------|--------|--------|
| 游标参数 | `before_seq` (AEP seq) | `before_id` (turns 表 PK) |
| 游标类型 | `int64` | `int64` |

```
# 旧版
GET /api/sessions/{id}/history?limit=20&before_seq=150

# 新版
GET /api/sessions/{id}/history?limit=20&before_id=42
```

### 3.2 TurnRecord JSON Schema 变更

| 字段 | 旧版本 | 新版本 | 说明 |
|------|--------|--------|------|
| `id` | `string` | `int64` | turns 表 AUTOINCREMENT PK |
| — | — | `generation` 新增 | session reset 计数器 |
| — | — | `turn_num` 新增 | generation 内轮次编号 |
| — | — | `tokens_input` 新增 | 纯输入 token |
| — | — | `tokens_cache_write` 新增 | 缓存写入 token |
| — | — | `tokens_cache_read` 新增 | 缓存读取 token |

### 3.3 tokens_in 语义变更

**旧版本**：`tokens_in` = `input_tokens`（仅原始输入 token）

**新版本**：`tokens_in` = `tokens_input + tokens_cache_write + tokens_cache_read`

这意味着 `tokens_in` 的值会比旧版本**更大**，因为现在包含了 cache token。这是正确的计费口径，与 Anthropic/Claude 的计费模型一致。

---

## 4. 配置变更

### 新增 events.retention

```yaml
# config.yaml
events:
  retention: 168h  # 默认 7 天，events 和 turns 共享此 TTL
```

Gateway 启动后会在后台运行 GC goroutine，每小时清理超过保留期的 events 和 turns 记录。

---

## 5. WebChat 前端适配

### 已适配文件

| 文件 | 变更 |
|------|------|
| `webchat/lib/api/sessions.ts` | `beforeSeq` → `beforeId`，`before_seq` → `before_id`；`ConversationRecord.id` string→number，新增字段 |
| `webchat/lib/adapters/hotplex-runtime-adapter.ts` | `minSeqRef` → `minIdRef`；游标提取修复（`turn:{id}:{role}` 中间段）；字段映射更新 |
| `webchat/lib/utils/turn-replay.ts` | `ConversationTurn.id` string→number，新增字段，`created_at` string→number |

---

## 6. Go 接口变更

面向二次开发或扩展 HotPlex 的开发者。

### EventTx 扩展

```go
type EventTx interface {
    Append(ctx context.Context, event *StoredEvent) error
    AppendTurn(ctx context.Context, turn *TurnWriteRequest) error  // 新增
    Commit() error
}
```

### TurnQuerier 接口

```go
type TurnQuerier interface {
    QueryTurns(ctx context.Context, sessionID string, limit, offset int) ([]*TurnRecord, error)
    QueryTurnsBefore(ctx context.Context, sessionID string, beforeID int64, limit int) ([]*TurnRecord, error)  // 新增
    QueryTurnStats(ctx context.Context, sessionID string) (*TurnStats, error)
    LatestGeneration(ctx context.Context, sessionID string) (int64, error)  // 新增
    DeleteExpiredTurns(ctx context.Context, cutoff time.Time) (int64, error)  // 新增
}
```

### BridgeDeps 扩展

```go
type BridgeDeps struct {
    // ...existing fields...
    TurnsQuerier eventstore.TurnQuerier  // 新增
}
```

---

## 7. 升级步骤

### 前置条件

- [ ] 确认所有活跃 session 已结束或可中断
- [ ] 备份数据库文件：`cp hotplex.db hotplex.db.bak.$(date +%Y%m%d)`

### 升级流程

```bash
# 1. 构建新版本
make build

# 2. 停止 Gateway
hotplex gateway stop

# 3. 部署新二进制
cp hotplex /path/to/installation/hotplex

# 4. 启动 Gateway（Goose 自动执行迁移）
hotplex gateway start
```

### 验证 Checklist

- [ ] Gateway 启动无错误，日志中无 migration 失败
- [ ] `turns` 表已创建：`sqlite3 <db> ".tables" | grep turns`
- [ ] 新 session 对话后 turns 有数据：`sqlite3 <db> "SELECT count(*) FROM turns"`
- [ ] History API 返回正确数据：`curl localhost:8080/api/sessions/{id}/history -H "Authorization: Bearer <token>"`
- [ ] WebChat 前端历史分页正常（需先完成前端适配）

### 可选：清理孤儿视图

```bash
sqlite3 <db> "DROP VIEW IF EXISTS v_turns; DROP VIEW IF EXISTS v_turns_assistant; DROP VIEW IF EXISTS v_turns_user;"
```

---

## 8. 回滚方案

```bash
# 1. 停止新版本 Gateway
hotplex gateway stop

# 2. 恢复旧版本二进制
cp /path/to/backup/hotplex.old /path/to/installation/hotplex

# 3. Goose down 会自动删除 turns 表
# 旧版本启动时会重建 v_turns 视图（如果 003 migration 仍在）
```

> **注意**：回滚后新版本期间产生的 turns 数据将丢失，但 events 数据不受影响。

---

## 附录：文件变更清单

| 文件 | 变更类型 |
|------|----------|
| `internal/session/sql/migrations/009_create_turns_table.sql` | 新增 |
| `internal/session/sql/migrations/003_create_turns_view.sql` | 删除 |
| `internal/eventstore/turn_write.go` | 新增 |
| `internal/eventstore/collector.go` | 修改（双通道：event + turn） |
| `internal/eventstore/store.go` | 修改（TurnRecord 字段扩展） |
| `internal/eventstore/sql/queries/turns.*.sql` | 修改（新表查询） |
| `internal/gateway/bridge_forward.go` | 修改（turn 写入逻辑） |
| `internal/gateway/session_stats.go` | 修改（cache token 累加） |
| `internal/gateway/api.go` | 修改（`before_seq` → `before_id`） |
| `internal/gateway/deps.go` | 修改（`TurnsQuerier` 注入） |
| `internal/config/config.go` | 修改（`EventsConfig`） |
| `cmd/hotplex/gateway_run.go` | 修改（turns GC goroutine） |
