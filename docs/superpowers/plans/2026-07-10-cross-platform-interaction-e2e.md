# 跨渠道交互式消息端到端实施计划

设计依据：`docs/superpowers/specs/2026-07-10-cross-platform-interaction-e2e-design.md`

## 任务 1：修复 Codex server request 路由与当前协议覆盖

涉及文件：

- `internal/worker/codexcli/types.go`
- `internal/worker/codexcli/manager.go`
- `internal/worker/codexcli/mapper.go`
- `internal/worker/codexcli/worker.go`
- `internal/worker/codexcli/worker_test.go`

步骤：

1. 先增加失败测试：JSON-RPC ID 为 `0`、非零整数和字符串时都进入 server-request 路径并原样响应。
2. 用可区分“缺失”和 `0` 的原始 ID 类型解析 frame；client response 仍只接受内部整数 ID。
3. 增加 `item/tool/requestUserInput` 和 `item/permissions/requestApproval` 的映射测试。
4. 保存 server request method、raw params 和 raw ID，按方法构造 question、permission 和 elicitation 响应。
5. 验证写失败保留映射，成功或 `serverRequest/resolved` 后清理。
6. 运行 `go test -count=1 -race ./internal/worker/codexcli`。

## 任务 2：飞书错误消息原位终结

涉及文件：

- `internal/messaging/feishu/streaming.go`
- `internal/messaging/feishu/conn.go`
- `internal/messaging/feishu/streaming*_test.go`
- `internal/messaging/feishu/adapter_flow_test.go`

步骤：

1. 增加失败测试：只有 placeholder 时收到 Error，断言同一 msg/card 被更新且不调用额外 reply。
2. 为 streaming controller 增加终态正文写入能力，替换 placeholder 后执行 Close。
3. 仅在卡片不存在或原位更新失败时使用静态错误回退。
4. 保持 Done/Turn Summary 的独立行为与幂等 Close。
5. 运行 `go test -count=1 -race ./internal/messaging/feishu`。

## 任务 3：Slack 采用可靠交付状态机

涉及文件：

- `internal/messaging/slack/interaction.go`
- `internal/messaging/slack/interaction_test.go`
- `internal/messaging/slack/adapter_test.go`

步骤：

1. 增加按钮与文字路径的失败测试：投递失败保留 pending、显示 retry，随后重试成功只投递一次。
2. `registerInteraction` 同时注册同步 sender。
3. Socket Mode 立即 ACK 后执行 claim/deliver/complete-or-release。
4. 失败更新保留 actions/input；成功才移除交互组件。
5. 文字 fallback 使用相同共享函数。
6. 多问题表单按问题 ID 及顺序构造 metadata；覆盖单选、多选、自定义答案。
7. 运行 `go test -count=1 -race ./internal/messaging/slack`。

## 任务 4：WebChat 使用 Gateway 权威 ACK

涉及文件：

- `internal/gateway/handler.go`
- `internal/gateway/handler_test.go`
- `webchat/lib/ai-sdk-transport/client/browser-client.ts`
- `webchat/lib/ai-sdk-transport/client/types.ts`
- `webchat/lib/adapters/hotplex-runtime-adapter.ts`
- WebChat 对应测试

步骤：

1. 增加 Gateway 失败测试：显式响应成功时回送同类型 ACK，失败只发关联 Error。
2. Gateway 在 Worker 成功接受显式响应后向 session 回送带 request ID 的响应事件。
3. BrowserClient 路由三种响应事件。
4. Runtime Adapter 发送后保持 submitting；精确 request ID ACK 后 resolved/rejected，关联 Error 后 failed。
5. 增加客户端确认超时和断连失败恢复；移除“任意 submitting 项”错误兜底。
6. 运行 WebChat 单测、类型检查和 Gateway race 测试。

## 任务 5：共享 Worker 契约与 36 条组合测试

涉及文件：

- `internal/worker/base/metadata_test.go`
- 四个 Worker 的 response 测试
- 新增跨组合 E2E 测试文件（放在能使用真实 Adapter/Handler/Worker 接口的内部测试包）

步骤：

1. 为四个 Worker 增加 `base.MetadataHandler` 编译期断言。
2. 为每个 Worker 的三类原生出口验证成功、失败保留 pending 和重试成功。
3. 建立显式枚举 platform × worker × interaction 的 36 条 table-driven 主链。
4. 每条主链断言：渠道 response → Gateway metadata → Worker native fake → 平台 resolved 状态 → 后续 Worker event 返回渠道。
5. 增加适用的 deny/decline、超时、重复、非 owner、Worker 退出、reset/terminate 分支。

## 任务 6：质量门禁与运行时验证

1. 运行目标 Go 包组合 `-count=1 -race`。
2. 运行 WebChat 测试和类型检查。
3. 运行 `make check`。
4. 使用当前 dev gateway 在安全飞书会话重放截图中的 Codex 授权，并确认：没有空消息卡、按钮成功、Agent 后续完成。
5. 使用本地 WebChat 对四个 Worker 各验证至少一种交互；其余组合由确定性 E2E 覆盖。
6. Slack 仅在已有安全测试频道时执行外部 smoke，否则记录 mock E2E 证据。
7. 请求独立代码审查，修复 Critical/Important 发现后重新运行受影响验证。
8. 审计 36 条矩阵及所有分支证据；只有全部有权威证据时才完成目标。
