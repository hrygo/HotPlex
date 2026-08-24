# HotPlex CLI 公共命令面

由公开 Cobra 命令树生成。语法、默认值和可用性以已安装命令的帮助输出为最终准则；未映射的命令说明保留实际 CLI help 的原文。

## hotplex
HotPlex Worker 网关

## hotplex admin
用户与账号管理（bootstrap admin 等）

选项：--config <string>

## hotplex admin create
创建账号（首个 admin，或后续用户）

选项：--admin --config <string> --password <string> --username <string>

## hotplex audit
审计日志链操作

## hotplex audit rebase
在仍存的记录上重新锚定审计哈希链（修复）

选项：--config <string> --confirm --next-id <int64>

## hotplex audit verify
验证审计哈希链完整性（只读）

选项：--config <string>

## hotplex config
管理配置

## hotplex config validate
验证配置文件

选项：--config <string>

## hotplex cron
Cron 任务管理

## hotplex cron create
创建 Cron 任务

选项：--allowed-tools <string> --attach --bot-id <string> --bot-name <string> --config <string> --delete-after-run --description <string> --expires-at <string> --max-retries <int> --max-runs <int> --message <string> --name <string> --owner-id <string> --platform <string> --platform-key <string> --schedule <string> --silent --timeout <int> --work-dir <string> --worker-type <string>

## hotplex cron delete
删除 Cron 任务

选项：--config <string>

## hotplex cron get
获取 Cron 任务详情

选项：--config <string> --json

## hotplex cron history
显示 Cron 任务执行历史

选项：--config <string> --json

## hotplex cron list
列出 Cron 任务

选项：--config <string> --enabled --json

## hotplex cron trigger
触发 Cron 任务执行

选项：--config <string>

## hotplex cron update
更新 Cron 任务

选项：--allowed-tools <string> --bot-id <string> --bot-name <string> --config <string> --delete-after-run --description <string> --enabled --expires-at <string> --max-retries <int> --max-runs <int> --message <string> --owner-id <string> --schedule <string> --silent --timeout <int> --work-dir <string> --worker-type <string>

## hotplex dev
快速启动开发模式

选项：--config <string>

## hotplex doctor
运行诊断检查

选项：--category <string> --config <string> --fix --json --verbose

## hotplex gateway
管理 Gateway 服务

## hotplex gateway restart
重启 Gateway 服务

选项：--config <string> --daemon --detached --dev

## hotplex gateway start
启动 Gateway 服务

选项：--config <string> --daemon --dev

## hotplex gateway stop
停止运行中的 Gateway 服务

## hotplex install
将 hotplex 二进制安装到 PATH

选项：--force --path <string>

## hotplex onboard
交互式配置向导

选项：--config <string> --enable-feishu --enable-slack --feishu-allow-from <stringSlice> --feishu-dm-policy <string> --feishu-group-policy <string> --force --install-service --non-interactive --service-level <string> --slack-allow-from <stringSlice> --slack-dm-policy <string> --slack-group-policy <string> --sync-skills

## hotplex runtime
运行时操作：检查并处理 fenced execution

## hotplex runtime fences
列出并处理 fenced execution（通过 Admin API）

## hotplex runtime fences abandon
放弃 fenced execution：以 OPERATOR_ABANDONED 标记失败并解除 session 阻塞

选项：--config <string> --confirm --evidence-ref <string> --fence-version <int64> --reason <string>

## hotplex runtime fences list
列出阻塞新输入的 fenced execution

选项：--config <string> --json --limit <int> --session-id <string>

## hotplex runtime fences resolve
处理 fence：清除它、保留 runtime=unknown 并解除 session 阻塞

选项：--config <string> --confirm --evidence-ref <string> --fence-version <int64> --reason <string>

## hotplex security
运行安全审计

选项：--config <string> --fix --json --verbose

## hotplex service
管理系统服务

## hotplex service install
安装为系统服务

选项：--config <string> --level <string>

## hotplex service logs
查看服务日志

选项：--follow --level <string> --lines <int>

## hotplex service restart
重启系统服务

选项：--level <string>

## hotplex service start
启动系统服务

选项：--level <string>

## hotplex service status
检查服务状态

选项：--json --level <string>

## hotplex service stop
停止系统服务

选项：--level <string>

## hotplex service uninstall
卸载系统服务

选项：--level <string>

## hotplex skills
管理内置 Agent Skill

## hotplex skills remove
删除受管理的内置 Skill projection

选项：--config <string> --dry-run --json --profile <string> --worker <stringArray>

## hotplex skills status
检查内置 Skill inventory 和 projection

选项：--config <string> --json --profile <string> --worker <stringArray>

## hotplex skills sync
将内置 Skill 同步到 Worker 根目录

选项：--config <string> --dry-run --json --profile <string> --worker <stringArray>

## hotplex slack
Slack 消息操作

## hotplex slack bookmark
管理频道书签

## hotplex slack bookmark add
添加书签

选项：--channel <string> --config <string> --emoji <string> --json --title <string> --url <string>

## hotplex slack bookmark list
列出书签

选项：--channel <string> --config <string> --json

## hotplex slack bookmark remove
移除书签

选项：--bookmark-id <string> --channel <string> --config <string>

## hotplex slack delete-file
从 Slack 删除文件

选项：--config <string> --file-id <string>

## hotplex slack download-file
从 Slack 下载文件

选项：--config <string> --file-id <string> --output <string>

## hotplex slack list-channels
列出频道和私聊

选项：--config <string> --json --limit <int> --types <string>

## hotplex slack react
添加或移除表情回复

## hotplex slack react add
添加表情回复

选项：--channel <string> --config <string> --emoji <string> --ts <string>

## hotplex slack react remove
移除表情回复

选项：--channel <string> --config <string> --emoji <string> --ts <string>

## hotplex slack schedule-message
安排消息稍后投递

选项：--at <string> --channel <string> --config <string> --json --text <string>

## hotplex slack send-message
发送文本消息

选项：--channel <string> --config <string> --json --text <string> --thread-ts <string>

## hotplex slack update-message
更新现有消息

选项：--channel <string> --config <string> --json --text <string> --ts <string>

## hotplex slack upload-file
上传文件到 Slack

选项：--channel <string> --comment <string> --config <string> --file <string> --json --max-size <int64> --thread-ts <string> --title <string>

## hotplex status
检查 Gateway 状态

选项：--config <string> --format <string>

## hotplex update
将 hotplex 更新到最新版本

选项：--check --restart --skills-profile <string> --sync-skills --yes

## hotplex version
打印版本信息

选项：--format <string>
