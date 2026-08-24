# 安装和二进制更新

## 安装或初始化

从已安装 CLI 开始，不要照抄包管理器指令：

    hotplex onboard --help
    hotplex install --help

首次配置或重新配置使用 `hotplex onboard`。只运行 operator 请求的模式；覆盖配置、安装
服务和同步 Skill 的参数都是独立变更，需要对应权限。如果用户请求安装本地构建二进制，
先检查 `hotplex install --help`，确认写入目标。

如果没有可信的 `hotplex` 二进制，停止并询问 operator 计划使用哪个官方 release artifact
或源码构建路径。不要发明远程脚本安装链路。安装或初始化后运行：

    hotplex doctor

报告失败和警告检查，不要自动应用用户未请求的修复。

## 更新

变更前检查本地更新契约和当前版本：

    hotplex update --help
    hotplex update --check

获得授权的二进制更新使用 `hotplex update`。只有请求包含服务重启时才添加 `--restart`，
只有请求包含内置 Skill 同步时才添加 `--sync-skills`。同步时显式选择目标 profile，并遵循
[configuration.md](configuration.md) 的对账检查。

记录更新前版本和预期恢复决策，然后验证已安装版本并运行 `hotplex doctor`。失败时保留
updater 和服务诊断，停止并交由 operator 决策；不要用临时复制、固定等待或 Git checkout
替换二进制。
