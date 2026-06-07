# 升级指南：v1.25.0 → v1.26.0

---

## 本次升级带来了什么

### 1. Agent Config 路径解析：从 botID 迁移到 botName

本次升级将 Agent 配置文件（SOUL.md、AGENTS.md 等）的路径解析从 Bot 平台运行时 ID（如 `U12345`）迁移为 YAML 配置名（如 `my-bot`）。

**迁移前**：`~/.hotplex/agent-configs/feishu/U12345/SOUL.md`
**迁移后**：`~/.hotplex/agent-configs/feishu/my-bot/SOUL.md`

优势：
- 配置文件路径可读性更强，与 YAML 配置中 `bots[].name` 一致
- 重命名 Bot 后无需迁移配置目录（见下方说明）
- 多 Bot 管理更直观

### 2. SessionStartParams 结构体

`StartSession` 和 `StartPlatformSession` 接口从 11-13 个位置参数重构为结构体传参，消除参数错位风险。

### 3. GATEWAY_BOT_NAME 环境变量

Worker 进程新增 `GATEWAY_BOT_NAME` 环境变量注入，Cron 任务可通过 `--bot-name "$GATEWAY_BOT_NAME"` 使用正确的 Bot 级 Agent 配置。

---

## 升级前须知

### Session 不受影响（不可变设计）

**已有 Session 的 `bot_name` 字段在创建时写入，之后不可变。** 重命名 Bot 不会影响正在运行或已存在的 Session。具体行为：

| 场景 | 行为 |
|------|------|
| 重命名 Bot | 已有 Session 保持原 `bot_name`，继续使用对应目录的 Agent 配置 |
| 删除 Bot 配置目录 | 已有 Session 的 Agent 配置回退到平台级（不会报错） |
| 新建 Session | 使用当前 Bot 的最新 `name` 解析配置 |

### 数据库自动迁移

升级时 Gateway 自动执行数据库迁移，为 `sessions` 和 `cron_jobs` 表添加 `bot_name` 列。迁移过程：

1. **SQLite**：`ALTER TABLE ADD COLUMN bot_name TEXT NOT NULL DEFAULT ''`
2. **PostgreSQL**：同理，添加带默认值的列

迁移是增量的，不删除任何现有数据，回滚安全。

### 旧版配置目录兼容

本次升级将单 Bot 归一化从 `Name:"default"` 改为 `Name:""`（空字符串）。这意味着单 Bot 模式下 Agent 配置直接使用平台级目录（如 `feishu/SOUL.md`），不再创建 `default/` 子目录。

**受影响用户**：在 v1.25.0 之前的版本中已手动创建 `~/.hotplex/agent-configs/{platform}/default/` 目录并放入配置文件的用户。

**Legacy fallback 行为**：
- 系统在解析 Agent 配置时，若 `botName` 为空且平台级目录未找到目标文件，会自动回退检查 `{platform}/default/` 目录
- 触发 fallback 时会输出 `slog.Warn` 日志提示迁移

**建议迁移路径**（二选一）：
1. **推荐**：将配置文件从 `default/` 移至平台级目录：`mv ~/.hotplex/agent-configs/feishu/default/* ~/.hotplex/agent-configs/feishu/`
2. **替代**：在 YAML 中显式配置 `name: "default"` 使其成为正式的 bot-level 配置（此时路径保持 `feishu/default/SOUL.md` 不变）

单 Bot 模式未手动创建 `default/` 目录的用户不受影响。

---

## 升级步骤

### 1. 停止 Gateway

```bash
hotplex gateway stop
# 或使用 make
make dev-stop
```

### 2. 更新二进制

```bash
hotplex update -y
```

### 3. 迁移 Agent 配置目录（可选）

如果你的 Agent 配置目录使用了 Bot 运行时 ID 命名：

```bash
# 查看当前目录结构
ls ~/.hotplex/agent-configs/feishu/

# 如果看到类似 U12345 的目录，重命名为 Bot 配置名
mv ~/.hotplex/agent-configs/feishu/U12345 ~/.hotplex/agent-configs/feishu/my-bot
```

> **不迁移也能正常运行**：系统会自动回退到 `default/` 目录。但建议迁移以获得更好的可读性。

### 4. 更新 Cron 任务（可选）

如果使用了多 Bot 模式，建议为 Cron 任务补充 `--bot-name`：

```bash
# 查看现有任务
hotplex cron list

# 为任务添加 bot-name
hotplex cron update <job-name> --bot-name "my-bot"
```

### 5. 启动 Gateway

```bash
hotplex gateway start
# 或使用 make
make dev
```

---

## 验证

```bash
# 1. 检查数据库迁移成功
hotplex gateway status

# 2. 确认 Session 包含 bot_name
# 在 Gateway 日志中搜索 "bot_name" 确认新 Session 正确写入

# 3. 确认环境变量注入
# 新 Session 的 Worker 进程应包含 GATEWAY_BOT_NAME 环境变量
```

---

## 回滚

如果需要回滚到 v1.25.0：

1. 停止 Gateway
2. 恢复旧版二进制
3. 数据库新增的 `bot_name` 列不影响旧版运行（旧版 binary 的 SELECT 查询使用显式列名，不包含 `bot_name`，因此忽略该列）
4. 如果已重命名配置目录，需要改回 Bot 运行时 ID 名称
