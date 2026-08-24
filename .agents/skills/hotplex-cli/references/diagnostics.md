# 只读诊断

使用 HotPlex 只读命令检查运行态：

    hotplex status --help
    hotplex doctor --help
    hotplex security --help
    hotplex config --help
    hotplex service status

优先使用 status 查看紧凑健康摘要，使用 doctor 检查配置和依赖，使用 security 检查安全
姿态，使用 config 查看有效配置，使用 service status 查看进程状态。已安装命令提供时，
也可读取日志和 Cron history。安装、更新、重启、配置写入和 Admin 变更必须转交
`hotplex-operator`。

命令同时提供读写模式时，选择只读模式；检查不可用时如实报告，不要编造结果。绝不打印
token、cookie、凭据、任意环境变量或完整私人 payload。
