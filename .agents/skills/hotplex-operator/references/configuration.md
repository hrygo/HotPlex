# 主机配置和内置 Skills

## 配置主机

修改配置前先检查已安装命令面：

    hotplex config --help
    hotplex onboard --help

只在用户请求的设置或重新配置模式下使用 `hotplex onboard`。将 `--force`、服务安装和
Skill 同步视为不同副作用。确认有效配置来源，区分仓库本地开发环境与运行时 HotPlex home，
并保留无关设置。

不要通过修改全局 AgentConfig 影响单个 Bot。验证 AgentConfig 变更时遵守文档规定的新
session 或 reset 生效边界。绝不打印凭据或完整的含密文件。变更后运行：

    hotplex doctor

报告实际验证的具体检查和有效行为。

## 对账内置 Skills

每次决定 projection 前都先读取 inventory 和 drift 证据：

    hotplex skills status
    hotplex skills status --profile operator --worker <worker>

选择 profile 或 worker 前先检查已安装二进制的 `hotplex skills status --help`。执行 `sync`
或 `remove` 时显式传入选定的 `--profile` 和每个 `--worker` 目标；意图尚不清晰时使用
`--dry-run`。报告 collision、drift、跳过目标和最终状态。

Skill 同步和删除会变更 Worker 可见的 projection，但不会编辑 canonical 内置包；状态查询
请求也不授权这两种变更。
