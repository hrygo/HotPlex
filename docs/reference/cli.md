---
title: HotPlex CLI 完整参考
weight: 4
description: HotPlex 命令行工具所有命令和标志的详尽参考文档
---

# HotPlex CLI 完整参考

HotPlex CLI（`hotplex`）是 HotPlex Worker Gateway 的统一管理工具，基于 [cobra](https://github.com/spf13/cobra) 构建。本文档说明决策、安全和生命周期语义；完整的 public command/flag surface 由 [generated CLI surface source](https://github.com/hrygo/hotplex/blob/main/internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md) 从当前 Cobra tree 生成，默认值、路径和可用性以已安装二进制的 `--help` 为准。

> **`--config` 默认值**：以下表格中的默认 `$HOTPLEX_HOME/config.yaml` 表示未显式传入 `--config` 时的解析结果——设置 `HOTPLEX_HOME` 环境变量后整个 workspace（配置文件、数据、日志、PID）随之迁移；未设置时回退 `$HOTPLEX_HOME/config.yaml`。

## 全局标志

以下标志在所有子命令中可用：

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

---

## 命令总览

```
hotplex
├── gateway          # Gateway 生命周期管理
│   ├── start        # 启动
│   ├── stop         # 停止
│   └── restart      # 重启
├── dev              # 开发模式快捷启动
├── status           # 检查 Gateway 运行状态
├── version          # 版本信息
├── doctor           # 环境诊断检查
├── security         # 安全审计
├── audit            # 审计链操作
│   └── verify       # 校验审计 Hash Chain 完整性（只读）
├── runtime          # Runtime 运维（fence 检查与决策）
│   └── fences
│       ├── list     # 列出阻塞新输入的 fenced executions
│       ├── resolve  # 解除 fence（runtime 保持 unknown）
│       └── abandon  # 放弃执行（runtime 置 failed）
├── config           # 配置管理
│   └── validate     # 验证配置文件
├── onboard          # 交互式配置向导
├── install          # 安装二进制到 PATH
├── update           # 自更新
├── service          # 系统服务管理
│   ├── install      # 安装服务
│   ├── uninstall    # 卸载服务
│   ├── start        # 启动服务
│   ├── stop         # 停止服务
│   ├── restart      # 重启服务
│   ├── status       # 服务状态
│   └── logs         # 查看日志
├── admin            # 用户与账号管理
│   └── create       # 创建账号（bootstrap admin 等）
├── slack            # Slack 消息操作
│   ├── send-message      # 发送消息
│   ├── update-message    # 更新消息
│   ├── schedule-message  # 定时消息
│   ├── upload-file       # 上传文件
│   ├── download-file     # 下载文件
│   ├── delete-file       # 删除文件
│   ├── list-channels     # 列出频道
│   ├── bookmark          # 书签管理
│   │   ├── add           # 添加书签
│   │   ├── list          # 列出书签
│   │   └── remove        # 删除书签
│   └── react             # 表情反应
│       ├── add           # 添加反应
│       └── remove        # 移除反应
├── skills           # embedded Skill inventory/projection 生命周期
│   ├── status       # 只读检查
│   ├── sync         # 显式同步
│   └── remove       # 删除有 receipt 证明归属的 projection
└── cron             # 定时任务管理
    ├── create       # 创建任务
    ├── list         # 列出任务
    ├── get          # 查看任务详情
    ├── update       # 更新任务
    ├── delete       # 删除任务
    ├── trigger      # 手动触发
    └── history      # 执行历史
```

---

## Gateway 命令

### `hotplex gateway start`

启动 Gateway 服务器。默认加载 `$HOTPLEX_HOME/config.yaml`，前台运行。

**示例**：

```bash
hotplex gateway start                          # 使用默认配置
hotplex gateway start -d                       # 后台守护进程模式
hotplex gateway start --dev                    # 开发模式（禁用认证）
hotplex gateway start -c /path/to/config.yaml  # 指定配置文件
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--dev` | | `bool` | `false` | 开发模式，禁用 API Key 认证和 Admin Token |
| `--daemon` | `-d` | `bool` | `false` | 后台守护进程模式运行，日志写入 `~/.hotplex/logs/gateway.log` |

### `hotplex gateway stop`

停止正在运行的 Gateway。自动检测 PID 文件和服务管理器两种模式。

```bash
hotplex gateway stop
```

无额外标志。

### `hotplex gateway restart`

重启 Gateway。停止当前实例后启动新实例，保留原配置和模式。

**示例**：

```bash
hotplex gateway restart       # 重启，保留原配置
hotplex gateway restart -d    # 重启为后台守护进程
hotplex gateway restart --detached  # Worker-initiated restart（独立进程安全隔离）
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--dev` | | `bool` | `false` | 开发模式 |
| `--daemon` | `-d` | `bool` | `false` | 后台守护进程模式 |
| `--detached` | | `bool` | `false` | 从 Worker 进程内部安全重启 Gateway。Fork 独立 PGID 的 helper 进程执行重启，与调用方 Worker 的生命周期完全隔离。内置 60s 冷却期防止循环重启 |

> `--detached` 适用于 AI Agent（Cron 任务或聊天指令）触发的 Gateway 重启。普通运维场景使用 `gateway restart` 即可。

---

## 开发模式

### `hotplex dev`

`hotplex gateway start --dev` 的快捷方式。开发模式下 API Key 认证和 Admin Token 被禁用，适合本地调试。

**示例**：

```bash
hotplex dev                        # 开发模式启动
hotplex dev -c /path/to/config.yaml
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

---

## 状态检查

### `hotplex status`

检查 Gateway 运行状态。通过 PID 文件和平台服务管理器检测进程，然后 ping 健康检查端点。

退出码：`0` = 运行中，`1` = 未运行。

**示例**：

```bash
hotplex status                # 文本输出
hotplex status --format json  # JSON 输出
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--format` | | `string` | `text` | 输出格式：`text`、`json` |

---

## 版本信息

### `hotplex version`

显示构建版本、编译时间、Go 运行时版本和平台信息。

```bash
hotplex version              # 文本输出
hotplex version --format json
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--format` | `string` | `text` | 输出格式：`text`、`json` |

---

## 诊断检查

### `hotplex doctor`

运行环境诊断检查，验证 HotPlex 配置是否正确。检查按类别组织：environment、config、dependencies、security、runtime、messaging、stt、tts、agent_config、worker。

**示例**：

```bash
hotplex doctor                     # 运行所有检查
hotplex doctor -v                  # 详细输出
hotplex doctor --fix               # 仅运行其他 checker 的受支持修复；built-in Skills checker 始终只读
hotplex doctor -C security         # 仅运行安全检查
hotplex doctor --json              # JSON 输出（用于脚本集成）
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--fix` | | `bool` | `false` | 请求其他 checker 修复；built-in Skills drift checker 没有 FixFunc，不会写入 |
| `--verbose` | `-v` | `bool` | `false` | 显示详细信息 |
| `--json` | | `bool` | `false` | JSON 格式输出 |
| `--category` | `-C` | `string` | | 仅检查指定类别：`environment`、`config`、`dependencies`、`security`、`runtime`、`messaging`、`stt`、`tts`、`agent_config`、`worker` |

**退出码**：

| 退出码 | 含义 |
|--------|------|
| `0` | 全部通过 |
| `1` | 存在失败项 |
| `3` | `--fix` 模式下部分修复失败 |

**runtime 类别新增检查项**（#877 / #946）：

| 检查项 | 说明 |
|--------|------|
| `runtime.fenced_executions` | 只读检查当前被 fence 阻塞的 executions（SQLite 经 `PRAGMA query_only` 只读打开；无 fence → Pass，有 fence → Warn 并给出 `hotplex runtime fences list` 提示；不提供 FixFunc —— operator 决策必须走审计过的 Admin API；PostgreSQL 后端指向 Admin API） |
| `runtime.effective_plan` | 用共享 agentspec resolver 对配置驱动的消息平台（slack/feishu/yuanxin）逐个解析 desired-state EffectiveRuntimePlan（只读，不触 DB/网络）；输出 plan hash / worker / permission / sandbox 摘要，blocked plan 以 Warn 呈现（blocked 永不视为静默成功）；webchat 为请求驱动，不在本地探测 |

---

## 安全审计

### `hotplex security`

对 HotPlex 配置运行安全审计。检查 TLS 设置、SSRF 防护和访问策略。

**示例**：

```bash
hotplex security                   # 运行安全审计
hotplex security -v                # 详细输出
hotplex security --fix             # 自动修复
hotplex security --json            # JSON 输出
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--fix` | | `bool` | `false` | 自动修复安全问题 |
| `--verbose` | `-v` | `bool` | `false` | 显示详细信息 |
| `--json` | | `bool` | `false` | JSON 格式输出 |

---

## 审计链操作

### `hotplex audit verify`

只读校验 `user_activity` Hash Chain 完整性（从最新 Checkpoint 或创世行起逐行校验；不应用任何 migration，可安全指向只读副本）。一次输出**全部**断裂点（非短路，上限 50 处），每条含 `id` / 时间戳 / `platform` / `action` / 按断裂类型的哈希诊断（链断裂为 `expected_prev_hash` / `actual_prev_hash`，篡改为 `expected_self_hash` / `actual_self_hash`）与处置建议。**链断裂时命令以非零退出码结束**（可用于 CI/cron 完整性门禁）。

自 migration 030 起未锚定 DELETE 已被数据库拒绝，因此新出现的 `prev_hash_mismatch` 通常意味着篡改或备份损坏。

**示例**：

```bash
hotplex audit verify               # 校验审计链（默认 $HOTPLEX_HOME/config.yaml）
hotplex audit verify --config configs/config-dev.yaml   # 指定配置
```

**输出**：

```
audit chain OK: 1503 rows checked, no breaks
audit chain BROKEN: 2 break(s) found across 1499 rows checked
  id=1253 ts=2026-07-21T13:48:21+08:00 platform=webchat action=skill.install outcome=failure reason=prev_hash_mismatch ...
advice: chain gap before this row: previous row missing/modified, ...
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

### `hotplex audit rebase --next-id <id> --confirm`

修复被历史手工 DELETE 打断的审计 Hash Chain：将链在指定行**重新锚定** —— 该行存储的 `prev_hash` 成为新的 Checkpoint 锚点，`verify` 从该行起重新验证；锚点之前的行保留但不再参与链校验（与 GC 的 Checkpoint 重锚定语义一致）。

这是断链**唯一合法的修复路径**：migration 023/030 拒绝行级 UPDATE 与未锚定 DELETE，断链行无法原地编辑或删除；从备份恢复是另一条路径（会丢失锚点前的记录）。**不可逆**，须 `--confirm` 确认；未确认时仅打印预览、不写任何数据。

**示例**：

```bash
hotplex audit rebase --next-id 1269 --confirm                 # 在 id=1269 处重新锚定
hotplex audit rebase --config configs/config-dev.yaml --next-id 1269 --confirm
```

**输出**：

```
rebase target: id=1269 ts=2026-07-21T14:15:26+08:00 platform=webchat action=session.create outcome=success
new checkpoint: next_id=1269 anchor_hash=1a7c83d5...
checkpoint written: id=7 next_id=1269 anchor_hash=1a7c83d5...
audit chain OK after rebase: 705 rows checked, no breaks
```

命令内部执行一次 `verify` 报告修复后状态；锚点后仍有断链时以非零退出码结束。

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--next-id` | | `int` | 必填 | 链上首个保留行的 id（其 `prev_hash` 成为新锚点） |
| `--confirm` | | `bool` | `false` | 确认执行不可逆的重锚定 |
| `--config` | | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

> **运维提示**：执行前先停止 Gateway（写操作与运行实例并发会破坏内存写回）；执行后用 `hotplex audit verify` 确认转绿并重启 Gateway。

---

## Runtime 运维（#877）

### `hotplex runtime fences`

检查并决策 fenced executions（Worker 运行结局不明、触发 fence 的投递）。**全部经由 Admin API over HTTP —— CLI 从不为 fence 读写打开数据库**；token 取自 `admin.tokens[0]`，可用 `HOTPLEX_ADMIN_TOKEN` 覆盖（分置 admin 部署）。

#### `hotplex runtime fences list`

列出当前阻塞新输入的 fenced executions（表格或 JSON）。

```bash
hotplex runtime fences list
hotplex runtime fences list --session-id sess-1 --json
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--session-id` | `string` | "" | 按 session 过滤 |
| `--json` | `bool` | `false` | JSON 输出 |
| `--limit` | `int` | 100 | 最大条数（上限 500） |
| `--config` / `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

输出列：`EXECUTION` / `SESSION` / `RUNTIME` / `REASON` / `FENCE VERSION` / `FENCED AT`。表格尾部提示决策命令模板。

#### `hotplex runtime fences resolve <execution-id>` / `abandon <execution-id>`

- **resolve**：清除 fence，runtime 保持 `unknown`，解除 session 阻塞（晚到终态事件仍可收敛）。
- **abandon**：清除 fence，runtime 置 `failed`（`OPERATOR_ABANDONED`），并向在线连接补发 `runtime.execution.failed` 事件。

两者均为**不可逆 operator 决策**，强制显式确认与条件版本：

```bash
hotplex runtime fences abandon exec-abc \
  --fence-version 3 \
  --reason "worker host lost, verified via run log" \
  --evidence-ref OPS-1234 \
  --confirm
```

| 标志 | 类型 | 必填 | 说明 |
|------|------|:---:|------|
| `--fence-version` | `int64` | ✅ | 取自 `fences list` 的预期 fence 版本；不匹配返回 `409 FENCE_CONFLICT` |
| `--reason` | `string` | ✅ | operator 理由（1–512 字符），仅进审计层 |
| `--evidence-ref` | `string` | - | 证据指针（工单/run ID，≤256 字符） |
| `--confirm` | `bool` | ✅ | 缺失时命令直接拒绝执行 |
| `--config` / `-c` | `string` | - | 配置文件路径 |

**冲突语义**：决策以 `fence_version` 为条件更新；并发 operator 或 inspect 与 action 之间的网关重启会产生 `409 FENCE_CONFLICT`，命令以非零码退出并提示重新 inspect —— **CLI 永不自动重试非幂等决策**。

---

## 配置管理

### `hotplex config validate`

验证配置文件。检查 YAML 语法、必填字段和值约束。

**示例**：

```bash
hotplex config validate                        # 验证默认配置
hotplex config validate -c /path/to/config.yaml
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

---

## 配置向导

### `hotplex onboard`

交互式配置向导，用于首次设置或重新配置。自动检测已有配置并引导创建 `config.yaml` 和 `.env`。

**示例**：

```bash
hotplex onboard                                    # 交互式向导
hotplex onboard --force                            # 覆盖已有配置
hotplex onboard --non-interactive                  # 自动生成，不提示
hotplex onboard --enable-slack --enable-feishu     # 启用所有平台
hotplex onboard --non-interactive --sync-skills     # 配置完成后显式同步 runtime Skills
hotplex onboard --non-interactive \
  --enable-slack \
  --slack-allow-from U12345,U67890 \
  --install-service
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--non-interactive` | `bool` | `false` | 非交互模式，使用默认值 |
| `--force` | `bool` | `false` | 覆盖已有配置 |
| `--enable-slack` | `bool` | `false` | 在非交互模式下启用 Slack（凭据通过 `.env` 配置） |
| `--enable-feishu` | `bool` | `false` | 在非交互模式下启用飞书（凭据通过 `.env` 配置） |
| `--slack-allow-from` | `stringSlice` | | Slack 允许的用户 ID 列表 |
| `--slack-dm-policy` | `string` | `allowlist` | Slack DM 策略：`open`、`allowlist`、`disabled` |
| `--slack-group-policy` | `string` | `allowlist` | Slack 群组策略：`open`、`allowlist`、`disabled` |
| `--feishu-allow-from` | `stringSlice` | | 飞书允许的用户 ID 列表 |
| `--feishu-dm-policy` | `string` | `allowlist` | 飞书 DM 策略：`open`、`allowlist`、`disabled` |
| `--feishu-group-policy` | `string` | `allowlist` | 飞书群组策略：`open`、`allowlist`、`disabled` |
| `--install-service` | `bool` | `false` | 在非交互模式下同时安装为系统服务 |
| `--service-level` | `string` | `user` | 服务级别：`user` 或 `system`（配合 `--install-service`） |
| `--sync-skills` | `bool` | `false` | 配置完成后显式同步 runtime built-in Skills；无此 flag 不同步 |

---

## 二进制安装

### `hotplex install`

将 hotplex 二进制安装到 PATH 中的目录。若已在 PATH 中且内容相同则为 no-op；若二进制不同则原地更新。

安装到新位置时，自动将目标目录添加到 PATH（通过 shell RC 文件 `~/.zshrc`/`~/.bashrc` 或 Windows 用户环境变量）。

**示例**：

```bash
hotplex install              # 安装到默认位置 (~/.local/bin)
hotplex install --path /usr/local/bin  # 安装到指定目录
hotplex install --force                # 强制重新安装
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--path` | `string` | `~/.local/bin` | 目标安装目录 |
| `--force` | `bool` | `false` | 即使已安装也重新安装 |

---

## 自更新

### `hotplex update`

检查并安装最新版本。从 GitHub Releases 下载归档文件（tar.gz / zip），验证 sha256 校验和后解压并原子替换。

支持平台：`linux/amd64`、`linux/arm64`、`darwin/amd64`、`darwin/arm64`、`windows/amd64`、`windows/arm64`。

> **注意**：Windows 下运行时二进制文件被锁定，请使用 `scripts/install.ps1` 替代。

`--check` 与 `--sync-skills` 互斥；只传 `--skills-profile`（即使是 `runtime`）而没有
`--sync-skills` 也会返回 usage error。用户取消真实更新时不会同步 Skills；替换成功后会先
报告新 binary，再执行一次同步，失败时不会声称同步成功。

**示例**：

```bash
hotplex update              # 交互式更新（带确认提示）
hotplex update --check      # 仅检查，不下载
hotplex update -y           # 跳过确认提示
hotplex update --restart    # 更新后自动重启 Gateway
hotplex update --sync-skills --skills-profile runtime  # 更新后显式同步 runtime Skills
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--check` | | `bool` | `false` | 仅检查更新可用性，不下载 |
| `--yes` | `-y` | `bool` | `false` | 跳过确认提示 |
| `--restart` | | `bool` | `false` | 更新成功后自动重启 Gateway |
| `--sync-skills` | | `bool` | `false` | 二进制替换成功后、重启前显式同步 Skills |
| `--skills-profile` | | `string` | `runtime` | 累积 profile：`runtime` 或 `operator`；仅与 `--sync-skills` 一起使用 |

---

## 系统服务管理

将 HotPlex Gateway 注册为系统服务，支持三种平台：

| 平台 | 用户级 | 系统级 |
|------|--------|--------|
| Linux | systemd `--user` | systemd system unit |
| macOS | `~/Library/LaunchAgents/` | `/Library/LaunchDaemons/` |
| Windows | 用户级 SCM | 系统 SCM |

### `hotplex service install`

安装为系统服务。

**示例**：

```bash
hotplex service install                     # 用户级安装（无需 root）
hotplex service install --level system      # 系统级安装（需 sudo）
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |
| `--level` | | `string` | `user` | 服务级别：`user`（无需 root）或 `system`（需 sudo） |

### `hotplex service uninstall`

卸载系统服务。

```bash
hotplex service uninstall                    # 卸载用户级服务
hotplex service uninstall --level system     # 卸载系统级服务
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--level` | `string` | `user` | 服务级别：`user` 或 `system` |

### `hotplex service start`

启动系统服务。

```bash
hotplex service start
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--level` | `string` | `user` | 服务级别：`user` 或 `system` |

### `hotplex service stop`

停止系统服务。如果服务管理器不可用，自动回退到 PID 文件检测。

```bash
hotplex service stop
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--level` | `string` | `user` | 服务级别：`user` 或 `system` |

### `hotplex service restart`

重启系统服务。

```bash
hotplex service restart
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--level` | `string` | `user` | 服务级别：`user` 或 `system` |

### `hotplex service status`

查看服务运行状态。

```bash
hotplex service status           # 文本输出
hotplex service status --json    # JSON 输出
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--level` | `string` | `user` | 服务级别：`user` 或 `system` |
| `--json` | `bool` | `false` | JSON 格式输出 |

### `hotplex service logs`

查看服务日志。

- **Linux**：通过 `journalctl`（用户级使用 `--user` 参数）
- **macOS**：`tail` launchd 日志文件
- **Windows**：PowerShell `Get-Content`

**示例**：

```bash
hotplex service logs             # 查看最近 100 行日志
hotplex service logs -f          # 实时跟踪日志
hotplex service logs -n 50       # 查看最近 50 行
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--level` | | `string` | `user` | 服务级别：`user` 或 `system` |
| `--follow` | `-f` | `bool` | `false` | 实时跟踪日志输出 |
| `--lines` | `-n` | `int` | `100` | 显示最近的行数（最小 1） |

---

## Slack 命令

Slack 操作命令使用与 Gateway 相同的配置（`~/.hotplex/.env`）。

### `hotplex slack send-message`

发送文本消息到 Slack 频道或 DM。支持 mrkdwn 格式。

**示例**：

```bash
hotplex slack send-message --text "Hello" --channel D0AQJ5CLZN0
hotplex slack send-message -t "Reply" --thread-ts 1777797319.120439 --channel C12345678
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--text` | `-t` | `string` | | 是 | 消息文本（支持 mrkdwn） |
| `--channel` | | `string` | | 否 | 目标频道或 DM ID |
| `--thread-ts` | | `string` | | 否 | 线程时间戳（回复线程消息） |
| `--json` | | `bool` | `false` | 否 | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack update-message`

更新已发送消息的内容。

```bash
hotplex slack update-message --channel C12345678 --ts 1777797319.120439 --text "Updated text"
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--text` | `-t` | `string` | | 是 | 新消息文本 |
| `--channel` | | `string` | | 是 | 频道 ID |
| `--ts` | | `string` | | 是 | 消息时间戳 |
| `--json` | | `bool` | `false` | 否 | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack schedule-message`

调度消息在指定时间发送。支持 ISO 8601 时间格式和 Unix 时间戳。

**示例**：

```bash
hotplex slack schedule-message --text "Reminder" --at "2026-05-04T09:00:00+08:00"
hotplex slack schedule-message -t "Report" --at 1746316800 --channel C12345678
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--text` | `-t` | `string` | | 是 | 消息文本 |
| `--channel` | | `string` | | 否 | 目标频道 |
| `--at` | | `string` | | 是 | 发送时间（ISO 8601 或 Unix 时间戳） |
| `--json` | | `bool` | `false` | 否 | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack upload-file`

上传文件到 Slack 频道或 DM。默认支持最大 50MB 文件。

**示例**：

```bash
hotplex slack upload-file --file ./podcast.mp3 --title "Podcast" --channel D0AQJ5CLZN0
hotplex slack upload-file -f report.pdf --comment "Q4 report"
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--file` | `-f` | `string` | | 是 | 本地文件路径 |
| `--title` | | `string` | | 否 | 文件标题（默认取文件名） |
| `--comment` | | `string` | | 否 | 文件描述 |
| `--channel` | | `string` | | 否 | 目标频道或 DM |
| `--thread-ts` | | `string` | | 否 | 线程时间戳（回复线程） |
| `--max-size` | | `int64` | `52428800`（50MB） | 否 | 文件大小上限（字节） |
| `--json` | | `bool` | `false` | 否 | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack download-file`

从 Slack 下载文件到本地。

**示例**：

```bash
hotplex slack download-file --file-id F0AQJ5CLZN0 --output ./report.pdf
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--file-id` | | `string` | | 是 | Slack 文件 ID |
| `--output` | `-o` | `string` | | 是 | 本地保存路径 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack delete-file`

从 Slack 删除文件。

**示例**：

```bash
hotplex slack delete-file --file-id F0AQJ5CLZN0
```

| 标志 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `--file-id` | `string` | | 是 | Slack 文件 ID |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack list-channels`

列出 Slack 频道、DM 和群组 DM。

**示例**：

```bash
hotplex slack list-channels                                    # 列出 DM
hotplex slack list-channels --types im,public_channel --json   # DM + 公开频道
hotplex slack list-channels --types im,public_channel,private_channel --limit 200
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--types` | | `string` | `im` | 频道类型（逗号分隔）：`im`、`public_channel`、`private_channel` |
| `--limit` | `-n` | `int` | `100` | 最大返回数量 |
| `--json` | | `bool` | `false` | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

### `hotplex slack bookmark`

频道书签管理。包含三个子命令。

#### `hotplex slack bookmark add`

添加频道书签。

```bash
hotplex slack bookmark add --channel C12345678 --title "Documentation" --url "https://docs.example.com"
hotplex slack bookmark add --channel C12345678 --title "Status" --emoji "white_check_mark"
```

| 标志 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `--channel` | `string` | | 是 | 频道 ID |
| `--title` | `string` | | 是 | 书签标题 |
| `--url` | `string` | | 否 | 书签 URL（与 `--emoji` 二选一） |
| `--emoji` | `string` | | 否 | 书签图标 emoji |
| `--json` | `bool` | `false` | 否 | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

#### `hotplex slack bookmark list`

列出频道书签。

```bash
hotplex slack bookmark list --channel C12345678
```

| 标志 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `--channel` | `string` | | 是 | 频道 ID |
| `--json` | `bool` | `false` | 否 | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

#### `hotplex slack bookmark remove`

删除频道书签。

```bash
hotplex slack bookmark remove --channel C12345678 --bookmark-id Bk12345678
```

| 标志 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `--channel` | `string` | | 是 | 频道 ID |
| `--bookmark-id` | `string` | | 是 | 书签 ID |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex slack react`

消息表情反应管理。包含两个子命令。

#### `hotplex slack react add`

为消息添加表情反应。

```bash
hotplex slack react add --channel D0AQJ5CLZN0 --ts 1777797319.120439 --emoji white_check_mark
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--channel` | | `string` | | 是 | 频道 ID |
| `--ts` | | `string` | | 是 | 消息时间戳 |
| `--emoji` | `-e` | `string` | | 是 | Emoji 名称（不含冒号） |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

#### `hotplex slack react remove`

移除消息表情反应。

```bash
hotplex slack react remove --channel D0AQJ5CLZN0 --ts 1777797319.120439 --emoji white_check_mark
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--channel` | | `string` | | 是 | 频道 ID |
| `--ts` | | `string` | | 是 | 消息时间戳 |
| `--emoji` | `-e` | `string` | | 是 | Emoji 名称（不含冒号） |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

---

## Cron 定时任务

定时任务命令直接操作本地 SQLite 数据库。CRUD 命令无需 Gateway 运行，但 `trigger` 需要通过 Admin API 通信。每个 create 示例都必须捕获返回 ID 并独立执行
`hotplex cron get <id|name> --json`；list/history 不能替代验证。更多安全路由见
[hotplex-cli Cron reference source](https://github.com/hrygo/hotplex/blob/main/internal/skills/builtin/hotplex-cli/references/cron.md)。

**调度表达式格式**：

| 格式 | 示例 | 说明 |
|------|------|------|
| `cron:<expression>` | `cron:*/5 * * * *` | 标准 cron 表达式 |
| `every:<duration>` | `every:30m` | 固定间隔（最小 1m） |
| `at:<timestamp>` | `at:2026-01-01T00:00:00Z` | 一次性执行（ISO-8601） |
| `at:+<duration>` | `at:+10m` | 从现在起的相对一次性执行（1m–72h） |

### `hotplex cron create`

创建定时任务。必填标志：`--name`、`--schedule`、`--message`（`-m`）、`--bot-id`、`--owner-id`。

**示例**：

```bash
# 标准周期任务
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "检查系统健康状态" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID"

hotplex cron get <JOB_ID> --json

# 带生命周期限制的周期任务
hotplex cron create \
  --name "remind" \
  --schedule "every:30m" \
  -m "提醒喝水" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --max-runs 6 --expires-at "2026-05-11T00:00:00+08:00"

hotplex cron get <JOB_ID> --json

# 静默一次性任务（不回传结果）
hotplex cron create \
  --name "cleanup" \
  --schedule "at:2026-05-15T02:00:00Z" \
  -m "清理过期数据" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --delete-after-run --silent

hotplex cron get <JOB_ID> --json
```

| 标志 | 短标志 | 类型 | 默认值 | 必填 | 说明 |
|------|--------|------|--------|------|------|
| `--name` | | `string` | | 是 | 任务名称（唯一标识） |
| `--schedule` | | `string` | | 是 | 调度表达式 |
| `--message` | `-m` | `string` | | 是 | Prompt 消息内容 |
| `--bot-id` | | `string` | | 是 | 关联的 Bot ID |
| `--owner-id` | | `string` | | 是 | 所有者 ID |
| `--description` | | `string` | | 否 | 任务描述 |
| `--work-dir` | | `string` | | 否 | 工作目录 |
| `--timeout` | | `int` | `0` | 否 | 执行超时（秒），`0` 表示不限 |
| `--allowed-tools` | | `string` | | 否 | 逗号分隔的允许工具列表 |
| `--delete-after-run` | | `bool` | `false` | 否 | 执行后自动删除（一次性任务） |
| `--silent` | | `bool` | `false` | 否 | 静默模式，不回传结果（自维护任务） |
| `--max-retries` | | `int` | `0` | 否 | 失败后最大重试次数（一次性任务） |
| `--max-runs` | | `int` | `0` | 否 | 最大执行次数后自动禁用（`0` = 无限） |
| `--expires-at` | | `string` | | 否 | 自动禁用时间（RFC3339 格式） |
| `--platform` | | `string` | | 否 | 目标投递平台：`slack`、`feishu`、`cron`（未设置时根据 `bot_id` 关联的 session 平台信息推断；若推断失败则默认为 `cron`，不投递结果） |
| `--platform-key` | | `string` | | 否 | 平台路由键（JSON 对象），如 `'{"channel_id":"C123"}'` |
| `--worker-type` | | `string` | | 否 | AI Agent 引擎类型：`claude_code`、`opencode_server`、`codex_cli`、`acp`。未设置时使用平台默认 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

### `hotplex cron list`

列出定时任务。

**示例**：

```bash
hotplex cron list                # 列出所有任务
hotplex cron list --enabled      # 仅列出已启用任务
hotplex cron list --json         # JSON 输出
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--enabled` | `bool` | `false` | 仅显示已启用的任务 |
| `--json` | `bool` | `false` | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

### `hotplex cron get`

查看定时任务详情。

```bash
hotplex cron get <id|name>       # 通过 ID 或名称查看
hotplex cron get daily-health    # 通过名称查看
hotplex cron get cron_abc123     # 通过 ID 查看
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--json` | `bool` | `false` | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

**位置参数**：

| 参数 | 必填 | 说明 |
|------|------|------|
| `<id\|name>` | 是 | 任务 ID 或名称 |

### `hotplex cron update`

更新定时任务。仅修改显式指定的标志。

```bash
hotplex cron update daily-health --enabled=false
hotplex cron update cron_abc123 --schedule "cron:0 */2 * * *" -m "新的消息内容"
```

| 标志 | 短标志 | 类型 | 默认值 | 说明 |
|------|--------|------|--------|------|
| `--schedule` | | `string` | | 调度表达式 |
| `--message` | `-m` | `string` | | Prompt 消息内容 |
| `--description` | | `string` | | 任务描述 |
| `--work-dir` | | `string` | | 工作目录 |
| `--bot-id` | | `string` | | Bot ID |
| `--owner-id` | | `string` | | 所有者 ID |
| `--timeout` | | `int` | `0` | 执行超时（秒） |
| `--allowed-tools` | | `string` | | 逗号分隔的允许工具列表 |
| `--enabled` | | `bool` | `true` | 启用或禁用任务 |
| `--delete-after-run` | | `bool` | `false` | 执行后自动删除 |
| `--silent` | | `bool` | `false` | 静默模式 |
| `--max-retries` | | `int` | `0` | 最大重试次数 |
| `--max-runs` | | `int` | `0` | 最大执行次数 |
| `--expires-at` | | `string` | | 自动禁用时间（RFC3339） |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

**位置参数**：

| 参数 | 必填 | 说明 |
|------|------|------|
| `<id\|name>` | 是 | 任务 ID 或名称 |

### `hotplex cron delete`

删除定时任务。

```bash
hotplex cron delete <id|name>
hotplex cron delete daily-health
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

**位置参数**：

| 参数 | 必填 | 说明 |
|------|------|------|
| `<id\|name>` | 是 | 任务 ID 或名称 |

### `hotplex cron trigger`

手动触发定时任务执行。需要 Gateway 正在运行（通过 Admin API 通信）。

```bash
hotplex cron trigger <id|name>
hotplex cron trigger daily-health
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

**位置参数**：

| 参数 | 必填 | 说明 |
|------|------|------|
| `<id\|name>` | 是 | 任务 ID 或名称 |

### `hotplex cron history`

查看定时任务执行历史，包含执行次数、成功/失败统计、持续时间和成本。

```bash
hotplex cron history <id|name>
hotplex cron history daily-health --json
```

| 标志 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `--json` | `bool` | `false` | JSON 格式输出 |
| `--config` | `-c` | `string` | `$HOTPLEX_HOME/config.yaml` | 配置文件路径 |

**位置参数**：

| 参数 | 必填 | 说明 |
|------|------|------|
| `<id\|name>` | 是 | 任务 ID 或名称 |

---

## Built-in Skill 生命周期

### `hotplex skills status|sync|remove`

三个命令共享 closed profile 和 worker 解析：`runtime` 只包含 `hotplex-cli`，`operator`
累积包含 `hotplex-cli` 与 `hotplex-operator`。`--worker` 可重复，支持 `--json`；`sync` 和
`remove` 另支持 `--dry-run`。完整 flags 见 [generated CLI surface source](https://github.com/hrygo/hotplex/blob/main/internal/skills/builtin/hotplex-cli/references/cli-surface.generated.md)。

```bash
hotplex skills status --profile runtime --worker claude_code --json
hotplex skills sync --profile runtime --worker claude_code --dry-run
hotplex skills remove --profile runtime --worker claude_code --json
```

不显式传 `--worker` 时，命令只解析已启用 Slack/Feishu/Yuanxin platform/bot 的 effective
worker targets；empty target 返回 bounded error，不回退 RegisteredTypes。Claude 的 native root
是 `<UserHome>/.claude/skills`，Codex/OpenCode 共享 `<UserHome>/.agents/skills`，ACP 没有可
推断 filesystem root。`$HOTPLEX_HOME/skills/builtin/<version>/<name>` 是 immutable inventory，
状态和 receipts 也位于 `$HOTPLEX_HOME`，与 UserHome projection 分离。

`status` 和 `--dry-run` 零写；sync 不覆盖未知 user/project Skill，collision、drift、failed
均以非零返回。remove 只删除 matching receipt 且 unchanged-tree 能证明归属的 projection，
不删除 inventory。Gateway startup 的 built-in reconciliation check 与 doctor 的 built-in Skills
checker 只读；Cron legacy compatibility helper 仍可能写入旧 `~/.hotplex/skills/cron.md`，它不
属于 AgentConfig B 通道、不是 canonical Agent Skill，也不由 `hotplex skills` 管理。onboard/update
只有显式 `--sync-skills` 才同步。内置 Skills 在 Admin/WebChat HTTP read surface 永久可发现；
Session `/skills` 仍按当前 Worker/filesystem evidence 决定是否出现，discoverable 不等于 callable。
真实同名 user/project Skill 优先，builtin-only update/delete 返回 `SKILL_BUILTIN_READONLY`。

## Admin 账号管理

用户与账号管理命令。用于 WebChat 多租户部署的 bootstrap admin 创建等场景（v1.29.1+）。

### `hotplex admin create`

创建账号（首个 admin，或后续普通用户）。直接读写 session 数据库，无需 Gateway 运行。

**示例**：

```bash
hotplex admin create --username bootstrap              # 交互式输入密码（不回显，推荐）
hotplex admin create --username alice --password '...' # 通过参数指定密码
hotplex admin create --username bob --admin=false      # 创建普通用户（非 admin 角色）
hotplex admin create --username ops --config /path/config.yaml
```

> `hotplex admin create` 属于 WebChat 多租户功能（v1.29.1 起的后端地基）；面向终端用户的登录与 workspace 管理 UI 将在后续版本上线，当前可先用此命令 bootstrap 首个 admin 账号。

| 标志 | 类型 | 默认值 | 必填 | 说明 |
|------|------|--------|------|------|
| `--username` | `string` | | 是 | 用户名（3-64 字符，仅 `[a-zA-Z0-9_.-]`，不可 `apikey:` 开头） |
| `--password` | `string` | | 否 | 密码（省略则交互式不回显；最少 8 字符）。⚠️ 作为进程参数对同机其他用户经 `ps`/`/proc` 可见，生产环境请使用交互式输入 |
| `--admin` | `bool` | `true` | 否 | 创建为 admin 角色 |
| `--config` | `string` | `$HOTPLEX_HOME/config.yaml` | 否 | 配置文件路径 |

密码使用 bcrypt（cost=12）存储。用户名保留命名空间 `apikey:` 检查用于防止与 API key 伪用户冲突导致的身份接管。

---

## 退出码汇总

| 退出码 | 命令 | 含义 |
|--------|------|------|
| `0` | 全部 | 成功 |
| `1` | `status` | Gateway 未运行 |
| `1` | `doctor` | 存在失败检查项 |
| `1` | 多数命令 | 执行错误 |
| `3` | `doctor --fix` | 部分修复失败 |

---

## 常见工作流

### 首次部署

```bash
# 1. 交互式配置
hotplex onboard

# 2. 验证环境
hotplex doctor

# 3. 启动开发模式验证
hotplex dev

# 4. 切换到生产模式
hotplex gateway start -d

# 5. 检查状态
hotplex status
```

### 生产环境部署

```bash
# 1. 非交互配置
hotplex onboard --non-interactive --enable-slack --install-service

# 2. 安装为系统服务
hotplex service install --level user

# 3. 验证
hotplex service status
hotplex security
```

### 版本升级

```bash
# 1. 检查更新
hotplex update --check

# 2. 执行更新并重启
hotplex update -y --restart
```

### 定时任务运维

```bash
# 创建
hotplex cron create --name "health" --schedule "cron:0 9 * * 1-5" \
  -m "检查系统健康" --bot-id "$BOT" --owner-id "$OWNER"

# 使用返回的 ID 独立核验（不要用 list 代替）
hotplex cron get <JOB_ID> --json

# 查看状态
hotplex cron list --enabled
hotplex cron get health --json

# 手动触发
hotplex cron trigger health

# 查看历史
hotplex cron history health --json

# 禁用
hotplex cron update health --enabled=false

# 删除
hotplex cron delete health
```
