# 数据库感知与统计分析手册

<role>
你需要对 HotPlex Gateway 的数据库进行感知、诊断或统计分析。本手册指导你精准识别当前数据库类型、获取正确的连接信息，并提供覆盖日常分析需求的 SQL 查询模板。
</role>

<database_detection>

## 1. 感知当前数据库类型

HotPlex 支持 SQLite（默认）和 PostgreSQL 两种数据库后端。执行任何数据库操作前，**必须先确定当前使用的是哪种数据库**。

### 第 1 步：定位配置文件

```bash
# 从运行中的 gateway 进程获取配置文件路径
# 注意：参数可能是 --config 或 -c
ps aux | grep hotplex | grep -v grep
# 提取 --config 或 -c 参数，例如：
# ./hotplex gateway start --config /home/user/.hotplex/config.yaml
# ./hotplex gateway start -c /home/user/.hotplex/config.yaml
# 配置文件路径 = 参数值
```

若 gateway 以系统服务运行，配置文件默认在 `~/.hotplex/config.yaml`。

### 第 2 步：检查环境变量覆盖（优先级最高）

环境变量 **优先于** 配置文件中的值。必须先检查环境变量，因为它可能覆盖文件配置。

HotPlex 环境变量前缀为 `HOTPLEX_`，配置键 `.` 替换为 `_`：

```bash
# 数据库驱动（决定使用 SQLite 还是 PostgreSQL）
echo $HOTPLEX_DB_DRIVER         # 空=SQLite, postgres/pg=PostgreSQL

# SQLite 路径
echo $HOTPLEX_DB_PATH

# PostgreSQL DSN（连接串）
echo $HOTPLEX_DB_POSTGRES_DSN

# PostgreSQL 连接池
echo $HOTPLEX_DB_POSTGRES_MAX_OPEN_CONNS
```

> **注意**：环境变量可能通过多种方式注入：
> 1. 系统环境变量（`export HOTPLEX_DB_DRIVER=postgres`）
> 2. `.env` 文件（配置文件同目录下，启动时自动加载）
> 3. Make 的 `MAKEFLAGS` 传递（`make dev` / `make dev-pg` 时，变量通过 make 参数注入进程）
>
> 检查所有来源：
> ```bash
> # 系统环境变量
> env | grep HOTPLEX_DB
>
> # .env 文件
> grep -E '^HOTPLEX_DB' <config-dir>/.env 2>/dev/null
>
> # Make 传递的变量（可能在 MAKEFLAGS 中）
> echo $MAKEFLAGS | tr ' ' '\n' | grep HOTPLEX_DB
> ```

### 第 3 步：读取配置文件（环境变量未覆盖时）

仅当第 2 步的环境变量为空时，才需要读取配置文件：

```bash
# 查看 db 段落
sed -n '/^db:/,/^[a-z]/p' <config-path>
```

### 配置优先级总结

```
系统环境变量 (HOTPLEX_DB_*) > .env 文件 > config.yaml
```

### 判定规则

| `HOTPLEX_DB_DRIVER` 或 `db.driver` | 数据库类型 | 连接信息来源 |
|---|---|---|
| 空 / 未设置 / `sqlite` | SQLite | `HOTPLEX_DB_PATH` > `.env` > `db.path` > 默认 `~/.hotplex/hotplex.db` |
| `postgres` / `pg` / `postgresql` | PostgreSQL | `HOTPLEX_DB_POSTGRES_DSN` > `.env` > `db.postgres.dsn` |

### 第 4 步：连接数据库

**SQLite**：
```bash
sqlite3 <db-path>

# 常用选项
sqlite3 -header -column <db-path>      # 带表头的列格式
sqlite3 -json <db-path> "SELECT ..."   # JSON 输出
```

**PostgreSQL**：
```bash
# 直接用 DSN 连接
psql "<dsn>"

# 或从环境变量获取
psql "$HOTPLEX_DB_POSTGRES_DSN"

# 或手动设置连接参数
export PGDATABASE=hotplex PGHOST=localhost PGPORT=5432 PGUSER=hotplex
psql
```

### 完整示例：从进程到连接

```bash
# 1. 找到配置文件（兼容 --config 和 -c 两种参数形式）
CFG=$(ps aux | grep 'hotplex' | grep -oP '(?:--config|-c)\s+\S+' | awk '{print $2}' | head -1)
CFG_DIR=$(dirname "$CFG")

# 2. 确定驱动（环境变量优先，含 MAKEFLAGS 来源）
DRIVER=$(echo $MAKEFLAGS | tr ' ' '\n' | grep -oP '(?<=HOTPLEX_DB_DRIVER=)\S+' | head -1)
[ -z "$DRIVER" ] && DRIVER=$HOTPLEX_DB_DRIVER
[ -z "$DRIVER" ] && DRIVER=$(grep -oP '(?<=driver:\s).*' "$CFG" 2>/dev/null | tr -d ' "' | head -1)
echo "Driver: ${DRIVER:-sqlite}"

# 3. 获取连接信息
if [ "$DRIVER" = "postgres" ] || [ "$DRIVER" = "pg" ]; then
  DSN=$(echo $MAKEFLAGS | tr ' ' '\n' | grep -oP '(?<=HOTPLEX_DB_POSTGRES_DSN=)\S+' | head -1)
  [ -z "$DSN" ] && DSN=$HOTPLEX_DB_POSTGRES_DSN
  [ -z "$DSN" ] && DSN=$(grep '^HOTPLEX_DB_POSTGRES_DSN=' "$CFG_DIR/.env" 2>/dev/null | cut -d= -f2- | tr -d '"' | head -1)
  [ -z "$DSN" ] && DSN=$(grep -oP '(?<=dsn:\s).*' "$CFG" 2>/dev/null | tr -d '"' | head -1)
  echo "PostgreSQL DSN: $DSN"
  # 连接：docker exec <pg-container> psql -U hotplex -d hotplex
  # 或：psql "$DSN"
else
  DB_PATH=$HOTPLEX_DB_PATH
  [ -z "$DB_PATH" ] && DB_PATH=$(grep -oP '(?<=path:\s).*' "$CFG" 2>/dev/null | tr -d '"' | head -1)
  [ -z "$DB_PATH" ] && DB_PATH="$HOME/.hotplex/hotplex.db"
  echo "SQLite: $DB_PATH"
  # sqlite3 "$DB_PATH"
fi
```

</database_detection>

<schema_reference>

## 2. 完整数据结构

HotPlex 数据库包含 5 张核心表。SQLite 和 PostgreSQL 使用相同的表结构，但类型有差异。

### sessions — 会话生命周期

| 列名 | SQLite 类型 | PG 类型 | 说明 |
|---|---|---|---|
| id | TEXT PK | TEXT PK | Session UUID |
| user_id | TEXT NOT NULL | TEXT NOT NULL | 用户 ID |
| owner_id | TEXT | TEXT | 所有者 ID |
| bot_id | TEXT | TEXT | Bot ID |
| worker_session_id | TEXT | TEXT | Worker 内部 session ID |
| worker_type | TEXT NOT NULL | TEXT NOT NULL | `claude_code` / `opencode_server` |
| state | TEXT CHECK | TEXT CHECK | `created`/`running`/`idle`/`terminated`/`deleted` |
| platform | TEXT | TEXT | `feishu` / `slack` / 空 |
| platform_key_json | TEXT | TEXT | 平台路由键 JSON |
| created_at | DATETIME | TIMESTAMP | 创建时间 |
| updated_at | DATETIME | TIMESTAMP | 更新时间 |
| expires_at | DATETIME | TIMESTAMP | 过期时间 |
| idle_expires_at | DATETIME | TIMESTAMP | 空闲过期时间 |
| context_json | TEXT | TEXT | 上下文 JSON |
| work_dir | TEXT | TEXT | 工作目录 |
| title | TEXT | TEXT | 会话标题 |
| source | TEXT CHECK | TEXT CHECK | 空 / `cron` |

### events — 会话事件流

| 列名 | SQLite 类型 | PG 类型 | 说明 |
|---|---|---|---|
| id | INTEGER PK AUTO | BIGSERIAL PK | 自增 ID |
| session_id | TEXT NOT NULL | TEXT NOT NULL | 关联 session |
| seq | INTEGER NOT NULL | INTEGER NOT NULL | AEP 序列号 |
| type | TEXT NOT NULL | TEXT NOT NULL | 事件类型 |
| data | TEXT NOT NULL | TEXT NOT NULL | 事件数据 JSON |
| direction | TEXT DEFAULT 'outbound' | TEXT DEFAULT 'outbound' | `inbound`/`outbound` |
| source | TEXT CHECK | TEXT CHECK | `normal`/`crash`/`timeout`/`fresh_start` |
| created_at | INTEGER (Unix ms) | BIGINT (Unix ms) | 创建时间戳 |

### turns — 对话轮次（物化视图）

| 列名 | SQLite 类型 | PG 类型 | 说明 |
|---|---|---|---|
| id | INTEGER PK AUTO | BIGSERIAL PK | 自增 ID |
| session_id | TEXT NOT NULL | TEXT NOT NULL | 关联 session |
| generation | INTEGER DEFAULT 1 | INTEGER DEFAULT 1 | 世代号 |
| turn_num | INTEGER NOT NULL | INTEGER NOT NULL | 世代内轮次号 |
| seq | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | AEP seq |
| role | TEXT NOT NULL | TEXT NOT NULL | `user` / `assistant` |
| content | TEXT DEFAULT '' | TEXT DEFAULT '' | 内容摘要 |
| platform | TEXT DEFAULT '' | TEXT DEFAULT '' | 平台 |
| user_id | TEXT DEFAULT '' | TEXT DEFAULT '' | 用户 ID |
| model | TEXT DEFAULT '' | TEXT DEFAULT '' | 模型名称 |
| success | INTEGER | BOOLEAN | 是否成功（user 轮为 NULL） |
| source | TEXT DEFAULT 'normal' | TEXT DEFAULT 'normal' | 来源 |
| tools_json | TEXT | TEXT | 工具使用统计 JSON |
| tool_count | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 工具调用次数 |
| tokens_input | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 输入 token |
| tokens_cache_write | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 缓存写入 token |
| tokens_cache_read | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 缓存读取 token |
| tokens_out | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 输出 token |
| duration_ms | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 耗时毫秒 |
| cost_usd | REAL DEFAULT 0.0 | NUMERIC(18,8) DEFAULT 0 | 费用 USD |
| created_at | INTEGER (Unix ms) | BIGINT (Unix ms) | 创建时间戳 |

### cron_jobs — 定时任务

| 列名 | SQLite 类型 | PG 类型 | 说明 |
|---|---|---|---|
| id | TEXT PK | TEXT PK | `cron_<uuid>` |
| name | TEXT UNIQUE | TEXT UNIQUE | 任务名称 |
| description | TEXT DEFAULT '' | TEXT DEFAULT '' | 描述 |
| enabled | INTEGER CHECK | BOOLEAN DEFAULT TRUE | 是否启用 |
| schedule_kind | TEXT CHECK | TEXT CHECK | `at`/`every`/`cron` |
| schedule_data | TEXT NOT NULL | TEXT NOT NULL | 调度数据 |
| payload_kind | TEXT CHECK | TEXT CHECK | `isolated_session`/`system_event`/`attached_session` |
| payload_data | TEXT NOT NULL | TEXT NOT NULL | Prompt 内容 |
| work_dir | TEXT DEFAULT '' | TEXT DEFAULT '' | 工作目录 |
| bot_id | TEXT DEFAULT '' | TEXT DEFAULT '' | Bot ID |
| owner_id | TEXT DEFAULT '' | TEXT DEFAULT '' | 所有者 ID |
| platform | TEXT DEFAULT '' | TEXT DEFAULT '' | 平台 |
| platform_key | TEXT DEFAULT '{}' | TEXT DEFAULT '{}' | 平台键 JSON |
| timeout_sec | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 超时秒数 |
| delete_after_run | INTEGER CHECK | BOOLEAN DEFAULT FALSE | 执行后删除 |
| silent | INTEGER CHECK | BOOLEAN DEFAULT FALSE | 静默模式 |
| max_retries | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 最大重试 |
| max_runs | INTEGER DEFAULT 0 | INTEGER DEFAULT 0 | 最大执行次数 |
| expires_at | TEXT DEFAULT '' | TEXT DEFAULT '' | 过期时间 |
| state | TEXT DEFAULT '{}' | TEXT DEFAULT '{}' | 运行状态 JSON（含 next_run_at_ms、run_count 等） |
| created_at | INTEGER (Unix ms) | BIGINT (Unix ms) | 创建时间戳 |
| updated_at | INTEGER (Unix ms) | BIGINT (Unix ms) | 更新时间戳 |

### chat_access_events — 聊天访问记录

| 列名 | SQLite 类型 | PG 类型 | 说明 |
|---|---|---|---|
| id | INTEGER PK AUTO | BIGSERIAL PK | 自增 ID |
| event_id | TEXT UNIQUE | TEXT UNIQUE | 事件 ID |
| platform | TEXT CHECK | TEXT CHECK | `feishu` / `slack` |
| chat_id | TEXT NOT NULL | TEXT NOT NULL | 聊天 ID |
| user_id | TEXT NOT NULL | TEXT NOT NULL | 用户 ID |
| bot_id | TEXT DEFAULT '' | TEXT DEFAULT '' | Bot ID |
| last_message_at | INTEGER DEFAULT 0 | BIGINT DEFAULT 0 | 最后消息时间 (Unix ms) |
| welcome_sent | INTEGER CHECK | BOOLEAN DEFAULT FALSE | 是否已发送欢迎语 |
| created_at | INTEGER (Unix ms) | BIGINT (Unix ms) | 创建时间戳 |

### api_key_users — API Key 用户映射

| 列名 | SQLite 类型 | PG 类型 | 说明 |
|---|---|---|---|
| id | INTEGER PK AUTO | BIGSERIAL PK | 自增 ID |
| api_key | TEXT UNIQUE | TEXT UNIQUE | API Key |
| user_id | TEXT NOT NULL | TEXT NOT NULL | 用户 ID |
| description | TEXT DEFAULT '' | TEXT DEFAULT '' | 描述 |
| created_at | DATETIME | TIMESTAMP | 创建时间 |
| updated_at | DATETIME | TIMESTAMP | 更新时间 |

</schema_reference>

<statistics_queries>

## 3. 统计分析查询模板

以下查询中的时间戳处理：
- **SQLite**: `datetime(ts/1000, 'unixepoch')` 将 Unix ms 转为可读时间
- **PostgreSQL**: `to_timestamp(ts/1000)` 将 Unix ms 转为可读时间
- `sessions` 表使用原生 DATETIME/TIMESTAMP，无需转换

### 3.1 会话概览

```sql
-- 当前各状态会话数
SELECT state, COUNT(*) AS cnt FROM sessions GROUP BY state ORDER BY cnt DESC;
```

```sql
-- 各平台会话分布
SELECT platform, COUNT(*) AS cnt, 
       SUM(CASE WHEN state='running' THEN 1 ELSE 0 END) AS active
FROM sessions GROUP BY platform;
```

```sql
-- 各 Bot 会话分布
SELECT bot_id, COUNT(*) AS cnt,
       SUM(CASE WHEN state='running' THEN 1 ELSE 0 END) AS active
FROM sessions WHERE bot_id != '' GROUP BY bot_id ORDER BY cnt DESC;
```

```sql
-- 最近 24 小时新建会话数
-- SQLite:
SELECT COUNT(*) FROM sessions 
WHERE created_at >= datetime('now', '-24 hours');
-- PostgreSQL:
SELECT COUNT(*) FROM sessions 
WHERE created_at >= NOW() - INTERVAL '24 hours';
```

### 3.2 Worker 与模型分析

```sql
-- 各 worker_type 使用分布
SELECT worker_type, COUNT(*) AS cnt FROM sessions GROUP BY worker_type;
```

```sql
-- 各模型使用统计（来自 turns 表）
-- SQLite:
SELECT model, COUNT(*) AS turns,
       SUM(tokens_input) AS total_input_tokens,
       SUM(tokens_out) AS total_output_tokens,
       ROUND(SUM(cost_usd), 4) AS total_cost_usd
FROM turns WHERE model != '' AND role='assistant'
GROUP BY model ORDER BY turns DESC;
-- PostgreSQL:
SELECT model, COUNT(*) AS turns,
       SUM(tokens_input) AS total_input_tokens,
       SUM(tokens_out) AS total_output_tokens,
       ROUND(SUM(cost_usd)::numeric, 4) AS total_cost_usd
FROM turns WHERE model != '' AND role='assistant'
GROUP BY model ORDER BY turns DESC;
```

```sql
-- 各平台 token 消耗
-- SQLite:
SELECT t.platform, 
       COUNT(*) AS assistant_turns,
       SUM(t.tokens_input) AS input_tokens,
       SUM(t.tokens_cache_write) AS cache_write_tokens,
       SUM(t.tokens_cache_read) AS cache_read_tokens,
       SUM(t.tokens_out) AS output_tokens,
       ROUND(SUM(t.cost_usd), 4) AS cost_usd
FROM turns t WHERE t.role='assistant'
GROUP BY t.platform;
-- PostgreSQL:
SELECT t.platform,
       COUNT(*) AS assistant_turns,
       SUM(t.tokens_input) AS input_tokens,
       SUM(t.tokens_cache_write) AS cache_write_tokens,
       SUM(t.tokens_cache_read) AS cache_read_tokens,
       SUM(t.tokens_out) AS output_tokens,
       ROUND(SUM(t.cost_usd)::numeric, 4) AS cost_usd
FROM turns t WHERE t.role='assistant'
GROUP BY t.platform;
```

### 3.3 成本分析

```sql
-- 每日成本趋势（最近 30 天）
-- SQLite:
SELECT DATE(datetime(created_at/1000, 'unixepoch')) AS day,
       COUNT(*) AS turns,
       ROUND(SUM(cost_usd), 4) AS cost_usd
FROM turns WHERE role='assistant'
  AND created_at >= CAST(strftime('%s','now','-30 days') AS INTEGER)*1000
GROUP BY day ORDER BY day;
-- PostgreSQL:
SELECT DATE(to_timestamp(created_at/1000)) AS day,
       COUNT(*) AS turns,
       ROUND(SUM(cost_usd)::numeric, 4) AS cost_usd
FROM turns WHERE role='assistant'
  AND created_at >= (EXTRACT(EPOCH FROM NOW() - INTERVAL '30 days')::bigint)*1000
GROUP BY day ORDER BY day;
```

> **性能说明**：WHERE 条件直接比较 `created_at`（原始 BIGINT），使 `idx_turns_created` 索引生效。避免 `created_at/1000 >= ...` 形式——对列做运算会阻止索引使用导致全表扫描。

```sql
-- 每用户成本排行（Top 10）
-- SQLite:
SELECT user_id, COUNT(*) AS turns,
       ROUND(SUM(cost_usd), 4) AS cost_usd
FROM turns WHERE role='assistant' AND user_id != ''
GROUP BY user_id ORDER BY cost_usd DESC LIMIT 10;
-- PostgreSQL:
SELECT user_id, COUNT(*) AS turns,
       ROUND(SUM(cost_usd)::numeric, 4) AS cost_usd
FROM turns WHERE role='assistant' AND user_id != ''
GROUP BY user_id ORDER BY cost_usd DESC LIMIT 10;
```

```sql
-- 缓存命中率
-- SQLite:
SELECT 
  ROUND(SUM(tokens_cache_read)*100.0/NULLIF(SUM(tokens_input),0), 1) AS cache_hit_pct,
  SUM(tokens_input) AS total_input,
  SUM(tokens_cache_write) AS cache_write,
  SUM(tokens_cache_read) AS cache_read
FROM turns WHERE role='assistant' AND tokens_input > 0;
-- PostgreSQL:
SELECT 
  ROUND((SUM(tokens_cache_read)*100.0/NULLIF(SUM(tokens_input),0))::numeric, 1) AS cache_hit_pct,
  SUM(tokens_input) AS total_input,
  SUM(tokens_cache_write) AS cache_write,
  SUM(tokens_cache_read) AS cache_read
FROM turns WHERE role='assistant' AND tokens_input > 0;
```

### 3.4 响应时间分析

```sql
-- 平均响应时间（按平台）
-- SQLite / PostgreSQL:
SELECT platform,
       COUNT(*) AS turns,
       ROUND(AVG(duration_ms), 0) AS avg_ms,
       ROUND(MIN(duration_ms), 0) AS min_ms,
       ROUND(MAX(duration_ms), 0) AS max_ms
FROM turns WHERE role='assistant' AND duration_ms > 0
GROUP BY platform;
```

```sql
-- 响应时间分布
-- SQLite / PostgreSQL:
SELECT
  CASE
    WHEN duration_ms < 5000 THEN '<5s'
    WHEN duration_ms < 15000 THEN '5-15s'
    WHEN duration_ms < 30000 THEN '15-30s'
    WHEN duration_ms < 60000 THEN '30-60s'
    ELSE '>60s'
  END AS bucket,
  COUNT(*) AS cnt
FROM turns WHERE role='assistant' AND duration_ms > 0
GROUP BY bucket ORDER BY MIN(duration_ms);
```

> **排序说明**：使用 `ORDER BY MIN(duration_ms)` 按实际耗时排序，而非字符串排序（`'5-15s'` 字符串 > `'>60s'`，导致顺序错误）。

### 3.5 工具使用分析

```sql
-- 工具调用频次排行（Top 15）
-- SQLite:
SELECT tool_name, SUM(cnt) AS total_calls
FROM (
  SELECT key AS tool_name, CAST(value AS INTEGER) AS cnt
  FROM turns, json_each(tools_json)
  WHERE role='assistant' AND tools_json IS NOT NULL
)
GROUP BY tool_name ORDER BY total_calls DESC LIMIT 15;
-- PostgreSQL:
SELECT tool_name, SUM(cnt) AS total_calls
FROM (
  SELECT key AS tool_name, (value::text)::int AS cnt
  FROM turns, jsonb_each_text(tools_json::jsonb)
  WHERE role='assistant' AND tools_json IS NOT NULL
) sub
GROUP BY tool_name ORDER BY total_calls DESC LIMIT 15;
```

```sql
-- 平均每轮工具调用数
-- SQLite / PostgreSQL:
SELECT ROUND(AVG(tool_count), 2) AS avg_tools_per_turn
FROM turns WHERE role='assistant' AND tool_count > 0;
```

### 3.6 会话质量分析

```sql
-- 成功率（按模型）
-- SQLite:
SELECT model,
       COUNT(*) AS total,
       SUM(CASE WHEN success=1 THEN 1 ELSE 0 END) AS succeeded,
       ROUND(SUM(CASE WHEN success=1 THEN 1 ELSE 0 END)*100.0/COUNT(*), 1) AS success_pct
FROM turns WHERE role='assistant' AND model != ''
GROUP BY model ORDER BY total DESC;
-- PostgreSQL:
SELECT model,
       COUNT(*) AS total,
       COUNT(*) FILTER (WHERE success IS TRUE) AS succeeded,
       ROUND(COUNT(*) FILTER (WHERE success IS TRUE)*100.0/COUNT(*), 1) AS success_pct
FROM turns WHERE role='assistant' AND model != ''
GROUP BY model ORDER BY total DESC;
```

> **类型差异**：SQLite `success` 为 INTEGER (0/1)，PG 为 BOOLEAN。PG 版使用 `IS TRUE` 和 `FILTER` 语法避免类型错误。

```sql
-- 每会话轮次排行（Top 20）
-- SQLite / PostgreSQL:
SELECT t.session_id, s.platform, s.user_id, s.state,
       COUNT(*) AS turn_count
FROM turns t
JOIN sessions s ON s.id = t.session_id
WHERE t.role = 'assistant'
GROUP BY t.session_id
ORDER BY turn_count DESC
LIMIT 20;
```

> **性能说明**：从 turns 表出发 INNER JOIN sessions，先过滤 `role='assistant'`（大幅减少行数），再 JOIN 获取会话信息。比 LEFT JOIN sessions + GROUP BY 全表更高效。

### 3.7 Cron 任务统计

```sql
-- 任务概览
-- SQLite / PostgreSQL:
SELECT enabled, schedule_kind, COUNT(*) AS cnt
FROM cron_jobs GROUP BY enabled, schedule_kind;
```

```sql
-- 运行统计（从 state JSON 提取）
-- SQLite:
SELECT name, schedule_kind,
       json_extract(state, '$.run_count') AS run_count,
       CASE WHEN json_extract(state, '$.last_run_at_ms') > 0
            THEN datetime(json_extract(state, '$.last_run_at_ms')/1000, 'unixepoch')
            ELSE 'never' END AS last_run
FROM cron_jobs WHERE enabled=1;
-- PostgreSQL:
SELECT name, schedule_kind,
       (state::jsonb->>'run_count')::int AS run_count,
       CASE WHEN (state::jsonb->>'last_run_at_ms') IS NOT NULL
                 AND (state::jsonb->>'last_run_at_ms')::bigint > 0
            THEN to_timestamp((state::jsonb->>'last_run_at_ms')::bigint/1000)::text
            ELSE 'never' END AS last_run
FROM cron_jobs WHERE enabled=true;
```

### 3.8 数据库健康

```sql
-- 数据量统计
-- SQLite:
SELECT 'sessions' AS tbl, COUNT(*) AS rows FROM sessions
UNION ALL SELECT 'events', COUNT(*) FROM events
UNION ALL SELECT 'turns', COUNT(*) FROM turns
UNION ALL SELECT 'cron_jobs', COUNT(*) FROM cron_jobs
UNION ALL SELECT 'chat_access_events', COUNT(*) FROM chat_access_events
UNION ALL SELECT 'api_key_users', COUNT(*) FROM api_key_users;
-- PostgreSQL:
SELECT 'sessions' AS tbl, COUNT(*) AS rows FROM sessions
UNION ALL SELECT 'events', COUNT(*) FROM events
UNION ALL SELECT 'turns', COUNT(*) FROM turns
UNION ALL SELECT 'cron_jobs', COUNT(*) FROM cron_jobs
UNION ALL SELECT 'chat_access_events', COUNT(*) FROM chat_access_events
UNION ALL SELECT 'api_key_users', COUNT(*) FROM api_key_users;
```

```sql
-- SQLite 特有：数据库文件大小与碎片
-- SQLite only:
PRAGMA page_count;
PRAGMA page_size;
PRAGMA freelist_count;
-- 数据库大小 = page_count * page_size，碎片率 = freelist_count / page_count

-- PostgreSQL 特有：表大小
-- PostgreSQL only:
SELECT relname AS table_name,
       pg_size_pretty(pg_total_relation_size(c.oid)) AS total_size,
       pg_size_pretty(pg_relation_size(c.oid)) AS table_size,
       pg_size_pretty(pg_total_relation_size(c.oid) - pg_relation_size(c.oid)) AS index_size
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'r'
ORDER BY pg_total_relation_size(c.oid) DESC;
```

```sql
-- 事件表膨胀检测（events 表应定期 GC）
-- SQLite:
SELECT COUNT(*) AS event_count,
       COUNT(DISTINCT session_id) AS sessions_with_events,
       ROUND(COUNT(*)*1.0/COUNT(DISTINCT session_id), 1) AS avg_events_per_session
FROM events;
-- PostgreSQL:
SELECT COUNT(*) AS event_count,
       COUNT(DISTINCT session_id) AS sessions_with_events,
       ROUND((COUNT(*)*1.0/COUNT(DISTINCT session_id))::numeric, 1) AS avg_events_per_session
FROM events;
```

### 3.9 时间趋势

```sql
-- 每小时会话创建趋势（最近 7 天）
-- SQLite:
SELECT strftime('%Y-%m-%d %H:00', created_at) AS hour, COUNT(*) AS cnt
FROM sessions
WHERE created_at >= datetime('now', '-7 days')
GROUP BY hour ORDER BY hour;
-- PostgreSQL:
SELECT date_trunc('hour', created_at) AS hour, COUNT(*) AS cnt
FROM sessions
WHERE created_at >= NOW() - INTERVAL '7 days'
GROUP BY hour ORDER BY hour;
```

```sql
-- 每日活跃用户数（DAU，最近 30 天）
-- SQLite:
SELECT DATE(datetime(created_at/1000, 'unixepoch')) AS day,
       COUNT(DISTINCT user_id) AS dau
FROM turns
WHERE created_at >= CAST(strftime('%s','now','-30 days') AS INTEGER)*1000
GROUP BY day ORDER BY day;
-- PostgreSQL:
SELECT DATE(to_timestamp(created_at/1000)) AS day,
       COUNT(DISTINCT user_id) AS dau
FROM turns
WHERE created_at >= (EXTRACT(EPOCH FROM NOW() - INTERVAL '30 days')::bigint)*1000
GROUP BY day ORDER BY day;
```

> **性能说明**：WHERE 直接比较原始 `created_at`（BIGINT Unix ms），使 `idx_turns_created` 索引生效。

</statistics_queries>
