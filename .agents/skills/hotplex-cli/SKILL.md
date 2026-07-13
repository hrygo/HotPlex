---
name: hotplex-cli
description: HotPlex Slack 与 Cron CLI 命令示例参考。当需要执行 hotplex slack 子命令（发消息、上传/下载文件、更新/定时消息、频道、书签、表情回复）或 hotplex cron 子命令（创建/列出/查看/更新/删除/触发/历史定时任务）时使用。
---

# HotPlex Slack / Cron CLI 示例

> 从 AGENTS.md 迁移出的命令参考。构建/测试/开发命令请用 `make help`。

## Slack 操作

```bash
hotplex slack send-message --text "Hello" --channel <id>
hotplex slack upload-file --file ./report.pdf --title "Report"
hotplex slack update-message --channel <id> --ts <ts> --text "Updated"
hotplex slack schedule-message --text "Reminder" --at "2026-05-04T09:00:00+08:00"
hotplex slack download-file --file-id <id> --output ./save.pdf
hotplex slack list-channels --types im,public_channel --json
hotplex slack bookmark add --channel <id> --title "Link" --url <url>
hotplex slack bookmark list --channel <id>
hotplex slack bookmark remove --channel <id> --bookmark-id <id>
hotplex slack react add --channel <id> --ts <ts> --emoji white_check_mark
```

## Cron 定时任务

```bash
# 创建周期任务（必填：name, schedule, message, bot-id, owner-id, max-runs, expires-at）
hotplex cron create \
  --name "daily-health" \
  --schedule "cron:0 9 * * 1-5" \
  -m "检查系统健康状态" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --max-runs 100 --expires-at "2027-01-01T00:00:00+08:00"

# 带短生命周期的周期任务
hotplex cron create \
  --name "remind" \
  --schedule "every:30m" \
  -m "提醒喝水" \
  --bot-id "$BOT_ID" --owner-id "$USER_ID" \
  --max-runs 6 --expires-at "2026-05-11T00:00:00+08:00"

# 列出 / 查看 / 更新 / 删除
hotplex cron list [--json] [--enabled]
hotplex cron get <id|name>
hotplex cron update <id|name> --enabled=false
hotplex cron delete <id|name>

# 手动触发（需 gateway 运行中）
hotplex cron trigger <id|name>

# 查看执行历史
hotplex cron history <id|name> [--json]
```
