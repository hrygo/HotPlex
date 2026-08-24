---
name: hotplex-operator
description: "运维或初始化 HotPlex 主机，覆盖首次 onboard、服务安装/启动、二进制更新、主机配置、审计检查和 Admin 变更。仅在明确授权的 operator 上下文中使用。"
compatibility: "需要本机主机访问、hotplex CLI，以及明确的 operator 或 Admin 权限。"
---

# HotPlex operator

仅在用户明确授权主机或 Admin 操作时使用本 Skill。任何变更前先检查已安装命令的
`--help`，确认目标、影响和用户授权覆盖该具体副作用。

- 服务生命周期：[references/service-lifecycle.md](references/service-lifecycle.md)
- 首次初始化和重新配置：[references/initialization.md](references/initialization.md)
- 安装和更新：[references/install-update.md](references/install-update.md)
- 主机配置和内置 Skill 对账：[references/configuration.md](references/configuration.md)
- Admin 和审计操作：[references/admin-audit.md](references/admin-audit.md)

不要从诊断请求推断变更权限，不要从配置请求推断安装意图，也不要从更新检查推断重启
权限。报告结果和剩余风险时不得暴露凭据或私人元数据。
