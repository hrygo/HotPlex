# 服务生命周期

使用跨平台 HotPlex 服务命令面，不要直接使用平台原生 service manager 命令：

    hotplex service --help
    hotplex service status --help

安装、卸载、启动、停止或重启前，确认请求的服务级别并说明预期中断。状态或日志请求不
授权生命周期变更。

纯重启使用原子命令：

    hotplex service restart

绝不把重启拆成 stop、等待和 start。之后检查状态；健康不确定时运行 `hotplex doctor`。
报告实际观察到的级别和状态，不要假设 Linux、macOS 或 Windows service manager 语义。

服务不健康时保留相关日志和诊断。没有 operator 决策时，不要删除状态或无限重试。
