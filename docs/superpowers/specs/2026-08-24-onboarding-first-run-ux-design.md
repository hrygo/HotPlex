# HotPlex 初始化与首次用户体验优化设计

## 状态

Accepted — 2026-08-24

## 背景

首次使用 HotPlex 时，`onboard`、开发环境、用户级 service 和 Worker Skill projection
存在多个有效配置来源。当前 Feishu 默认白名单策略还要求用户预先知道 OpenID；同时非交互
`onboard` 在某些路径下可能重写已有平台凭据，导致“初始化成功但无法对话”。

## 决策

1. `onboard` 继续生成同目录 `config.yaml` 与 `.env`，并在重写 `.env` 时保留已有平台凭据；
   交互式和非交互式 allowlist 通过平台环境变量落盘，保持 service/dev 的来源一致。
2. 不降低默认安全策略：DM/group 仍默认为 `allowlist`，群聊仍默认要求 `@HotPlex`。首次
   Feishu 指引明确说明通过接收日志取得 OpenID，并在重启后验证。
3. 保留 Skill 同步的安全边界，扩展 runtime `hotplex-cli` 的普通用户指引，而不把
   `hotplex-operator` 的服务、Admin 或主机写权限暴露给普通会话。

## 验收标准

- 非交互 `onboard --enable-feishu` 不删除同目录已有 `APP_ID`/`APP_SECRET`。
- `--feishu-allow-from` 能写入 effective `.env`，重跑 onboard 后仍保留。
- Feishu 引导覆盖 OpenID、白名单、群聊 mention 和重启验证。
- runtime Skill 能解释 `/help`、`/reset`、`/skills`、群聊 mention 与白名单拒绝；不执行
  service/Admin/凭据变更。
- 相关 Go 测试、文档构建和链接校验通过。

## 不在本次范围

- 不改变默认 allowlist 安全策略。
- 不实现自动从飞书 API 获取用户 OpenID。
- 不新增数据库表、AEP 字段、service manager 行为或外部依赖。
