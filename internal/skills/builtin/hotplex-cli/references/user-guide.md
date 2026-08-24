# 普通用户聊天与首次使用

本 reference 只用于解释已运行 HotPlex 的聊天体验，不执行主机、service、Admin、凭据或
白名单写操作。先查询当前会话的 `/help`、`/skills` 和 `/mcp`；能力以当前 Worker 的
`callable` 状态为准。

## 常用聊天命令

- `/help`：查看当前平台和 Worker 支持的命令。
- `/reset` 或 `/new`：清除当前上下文并开始新会话。
- `/gc` 或 `/park`：休眠会话，保留历史。
- `/context`：查看上下文使用情况。
- `/skills`：查看当前会话可调用的 Skill；`discoverable` 不等于 `callable`。
- `/mcp`：查看 MCP 连接状态。

如果平台支持中文短语，也可先输入 `$help` 或 `$技能`。不要猜测未在当前 `/help` 或
`/skills` 中出现的命令。

## 飞书消息不响应时

1. 私聊直接发送文本；群聊默认必须 `@HotPlex`。
2. 如果 Gateway 启用了 allowlist，只有管理员配置的 Feishu OpenID 才能触发；用户端通常
   不会收到被拒绝提示。
3. 先确认自己发的是 Bot 当前连接的应用，再使用 `/help` 或重新发送一条短消息。
4. 仍无响应时，把发送时间、聊天类型（私聊/群聊）和是否 @Bot 告知管理员；不要在聊天中
   发送 App Secret、Admin token 或其他凭据。

## 权限和会话

收到权限确认卡片时，在有效期内点击允许/拒绝；不要把凭据直接发到普通聊天消息中。配置
或 Skill 变更通常在新会话或 `/reset` 后生效。

需要修改白名单、重启 Gateway、安装服务或更新二进制时，应由管理员使用 operator 流程
处理；本 Skill 不把这些操作升级为普通用户权限。
