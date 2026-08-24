# Cron 操作

使用 `hotplex cron` 命令族管理 Agent 定时任务。创建任务前先检查已安装版本的校验器：

    hotplex cron --help
    hotplex cron create --help

支持的 schedule 形式为 `cron:<expression>`、`every:<duration>`、`at:<RFC3339>` 和
`at:+<duration>`。使用可移植的 RFC3339 时间戳，不要依赖 shell 专用的日期计算。
重复任务必须设置明确的生命周期限制，例如 `--max-runs` 和 `--expires-at`。

默认使用隔离执行。prompt 必须包含后续运行所需的全部上下文。隔离的重复任务应使用
自包含 prompt 和明确的生命周期限制：

    hotplex cron create \
      --name "health-check" \
      --schedule "every:30m" \
      --message "Read the current gateway health and report only actionable failures." \
      --bot-id "<BOT_ID>" \
      --owner-id "<OWNER_ID>" \
      --max-runs 10 \
      --expires-at "2030-01-01T00:00:00Z" \
      --platform cron \
      --silent

记录返回的任务标识，然后通过独立的 JSON 读取路径验证，不触发执行：

    hotplex cron get <JOB_ID> --json

安全的隔离一次性相对调度应使用解析器接受的 `at:+duration` 形式，并设置明确的清理策略：

    hotplex cron create \
      --name "one-shot-health-check" \
      --schedule "at:+10m" \
      --message "Read the current gateway health and report the result." \
      --bot-id "<BOT_ID>" \
      --owner-id "<OWNER_ID>" \
      --platform cron \
      --delete-after-run

随后独立执行：

    hotplex cron get <JOB_ID> --json

只有请求明确需要复用已有 session，且当前命令帮助确认 attached-session 要求时，才使用
`--attach`。附加任务需要 `GATEWAY_SESSION_ID`；一次性任务使用 `at:+duration`，重复任务
使用 `every:<duration>` 并设置明确限制：

    hotplex cron create \
      --attach \
      --name "attached-health-check" \
      --message "Read the current gateway health." \
      --schedule "at:+10m"

独立验证返回的任务 ID：

    hotplex cron get <JOB_ID> --json

当已安装命令要求时，使用当前的 `GATEWAY_*` 身份和投递路由键；绝不打印其值，也不要把
无关环境变量复制到 prompt。使用前根据当前帮助和校验器确认 `--silent`、
`--delete-after-run`、`--max-retries`、`--timeout` 和 `--allowed-tools` 的语义。

使用 `hotplex cron list` 查找任务，使用 `hotplex cron update` 修改任务，使用
`hotplex cron trigger` 立即运行任务，使用 `hotplex cron history` 查看执行记录。
只有获得明确删除授权后才能使用 `hotplex cron delete`。list 或 history 结果不能替代
创建后的 `cron get --json` 验证。
