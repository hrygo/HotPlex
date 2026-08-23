---
title: 定时任务 (Cron) 教程
weight: 4
description: 用自然语言调度 AI 任务 —— HotPlex AI-native 定时系统使用指南
---

# 定时任务 (Cron) 教程

HotPlex Cron 把自然语言 Prompt 交给 Worker 在计划时间执行。完整命令和
当前 flags 以 `hotplex cron --help`、`hotplex cron create --help` 为准；本页只
保留决策、安全和验证要点。

## 1. 选择 schedule

`--schedule` 使用 `kind:value` 形式：

| 形式 | 例子 | 适用场景 |
|------|------|----------|
| `cron:<expression>` | `cron:0 9 * * 1-5` | 周期性 cron 表达式 |
| `every:<duration>` | `every:30m` | 固定间隔（至少 1 分钟） |
| `at:<RFC3339>` | `at:2030-01-01T00:00:00Z` | 固定时间的一次性任务 |
| `at:+<duration>` | `at:+10m` | 从现在起的安全相对延迟 |

不要用 shell-specific 日期算术；直接使用可移植的 RFC3339 占位时间，或使用 parser
支持的 `at:+duration`。

## 2. 创建隔离的周期任务

周期任务应显式提供生命周期约束。以下示例也展示 `--timeout`、`--allowed-tools`
和 `--silent` 的位置；占位符不会被 HotPlex 当作真实凭据：

```bash
hotplex cron create \
  --name "health-check" \
  --schedule "every:30m" \
  --message "Read the current gateway health and report only actionable failures." \
  --bot-id "<BOT_ID>" \
  --owner-id "<OWNER_ID>" \
  --max-runs 10 \
  --expires-at "2030-01-01T00:00:00Z" \
  --timeout 120 \
  --allowed-tools "status,logs" \
  --platform cron \
  --silent
```

CLI 返回任务 ID 后，必须使用独立的 JSON 读取路径核对状态、schedule、platform
和 platform key；不能把 `cron list` 当作成功证据：

```bash
hotplex cron get <JOB_ID> --json
```

## 3. 创建一次性延迟任务

`at:+10m` 是 parser 接受的相对形式；配合 `--delete-after-run` 和
`--max-retries` 可表达一次性清理与有限重试：

```bash
hotplex cron create \
  --name "one-shot-health-check" \
  --schedule "at:+10m" \
  --message "Read the current gateway health and report the result." \
  --bot-id "<BOT_ID>" \
  --owner-id "<OWNER_ID>" \
  --platform cron \
  --delete-after-run \
  --max-retries 2
```

随后再次独立读取返回的 ID：

```bash
hotplex cron get <JOB_ID> --json
```

## 4. Attach 到已有 Session

只有请求明确要求复用已有 Session 时才使用 `--attach`。执行环境必须已经提供
`GATEWAY_SESSION_ID`；bot/owner 身份按当前 CLI 的 `GATEWAY_*` 解析规则提供，
不要把真实环境值写入 Prompt。attached one-shot 可使用 `at:+duration`：

```bash
hotplex cron create \
  --attach \
  --name "attached-health-check" \
  --message "Read the current gateway health." \
  --schedule "at:+10m"
```

同样使用返回 ID 做独立验证：

```bash
hotplex cron get <JOB_ID> --json
```

Attached recurring job 可以使用 `every:<duration>`，但必须在当前 help/validator
确认下同时提供 `--max-runs` 与 `--expires-at`。attached job 不接受 cron expression。

## 5. 生命周期与安全检查

- `--max-runs` 限制周期任务的成功执行次数，`--expires-at` 使用 RFC3339 绝对时间；
  两者可以同时指定。
- `--delete-after-run` 只用于明确要删除的一次性任务；`--silent` 抑制结果投递，
  不会跳过任务执行。
- `--timeout` 以秒为单位；`--allowed-tools` 是逗号分隔的工具名，按当前 Worker
  能力与授权选择最小集合。
- 创建前检查 `hotplex cron create --help`，创建后总是执行独立
  `hotplex cron get <id|name> --json`；`list`、`history`、`trigger` 不能替代这一步。
- `delete`、`trigger`、更新投递目标或扩大工具权限都属于有副作用的操作，必须得到
  明确授权。无法调用当前 CLI 时应报告 `unsupported`/degraded，不要声称任务已创建。
