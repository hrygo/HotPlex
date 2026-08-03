# 飞书 × Claude Code 消息链加固实施计划

> **执行方式**：按 subagent-driven-development 流程逐任务执行；每个任务先红测、再最小实现、再绿测、提交、自审和独立任务审查。Epic：[#941](https://github.com/hrygo/hotplex/issues/941)。

**目标**：在保留 audit 原文明文的前提下，修复飞书 × Claude Code 端到端消息链的六类可靠性和安全风险。

**架构边界**：audit 是长期原文真相源，event store 是短期运行副本，INFO 日志仅留元数据/指纹。平台命令统一转换为 AEP control；队列承担单 chat 串行和安全生命周期；终态控制器返回真实投递结果，由连接层负责静态兜底。

**技术栈**：Go、AEP v1、Feishu Go SDK、OpenTelemetry、testify、GitHub Actions。

---

### Task 1：数据副本分层与正文日志消除

**文件**：
- 修改：`internal/gateway/handler.go`
- 修改：`internal/gateway/handler_test.go`
- 修改：`internal/config/config_types.go`
- 修改：`internal/config/effective_events_retention_test.go`
- 修改：`docs/reference/configuration.md`

**步骤**：
1. 新增红测：INFO handler 日志不包含输入原文，但包含 `data_size` 和 `data_sha256`；`emitAudit` 仍保留完整 `content`。
2. 新增红测：audit enabled 且 full-content retention 更长时，`EffectiveEventsRetention` 仍返回 `events.retention`。
3. 运行：`rtk go test ./internal/gateway ./internal/config -run 'Test.*(Audit|ReceivedEvent|EffectiveEventsRetention)' -count=1`，确认失败原因符合预期。
4. 实现稳定的 envelope 数据编码、大小和 SHA-256 短指纹日志，移除 INFO 的 `data` 字段。
5. 解耦 event retention 与 audit full-content retention；更新注释和配置文档。
6. 运行：`rtk go test ./internal/gateway ./internal/config -count=1 -race`。
7. 提交：`fix(gateway): separate plaintext audit from operational copies`

### Task 2：停止指令进入 AEP `control.stop`

**文件**：
- 修改：`internal/messaging/control_command.go`
- 修改：`internal/messaging/control_command_test.go`
- 修改：`internal/messaging/pipeline.go`
- 修改：`internal/messaging/feishu/handler.go`
- 修改：`internal/messaging/feishu/adapter_flow_test.go`
- 修改：`internal/messaging/slack/adapter.go`
- 修改：相关 Slack 测试

**步骤**：
1. 新增红测：`stop`、`停止`、`/stop` 被检测为 `CmdControl` 且 action 为 `ControlActionStop`。
2. 新增飞书/Slack 红测：abort 文本调用 control handler，而非仅本地返回或仅取消 queue。
3. 运行相关消息包定向测试，确认红测。
4. 将 abort trigger 统一映射为 control result；平台路由复用既有 control envelope 和 Gateway stop 实现。
5. 确保 stop 不新建错误 session、不触发 reset/terminate 语义。
6. 运行：`rtk go test ./internal/messaging/... ./internal/gateway -count=1 -race`。
7. 提交：`fix(messaging): route abort commands through control stop`

### Task 3：ChatQueue 生命周期与去重回滚

**文件**：
- 修改：`internal/messaging/feishu/chat_queue.go`
- 修改：`internal/messaging/feishu/chat_queue_test.go`
- 修改：`internal/messaging/dedup.go`
- 修改：`internal/messaging/dedup_test.go`
- 修改：`internal/messaging/feishu/handler.go`
- 修改：`internal/messaging/feishu/adapter_flow_test.go`

**步骤**：
1. 新增红测：Close 与 Enqueue 并发无 panic；Close 后返回 `ErrChatQueueClosed`；idle 退出边界不会接收孤儿任务。
2. 新增红测：queue full 时 handler 回滚 message ID，平台重试可再次进入；成功入队仍去重。
3. 新增 Dedup 条件删除红测。
4. 运行定向测试和 `-race`，确认红测或竞态。
5. 在队列锁内完成 worker 选择和 channel send，使用 closed 状态与实例条件删除。
6. handler 用提交/回滚语义管理 dedup 记录。
7. 运行：`rtk go test ./internal/messaging ./internal/messaging/feishu -count=1 -race`。
8. 提交：`fix(feishu): make queue admission and dedup retry-safe`

### Task 4：媒体有界读取与最小权限

**文件**：
- 修改：`internal/messaging/feishu/media.go`
- 修改：`internal/messaging/feishu/media_test.go`

**步骤**：
1. 新增红测：恰好 10 MiB 成功、10 MiB + 1 失败、超限 reader 不被完整读取。
2. 新增 Unix 权限红测：目录 `0700`、文件 `0600`；Windows 跳过 mode 位断言。
3. 运行定向测试确认红测。
4. 使用 `io.LimitReader(mediaMaxSize+1)` 有界读取，收紧 `MkdirAll`/`WriteFile` 权限。
5. 运行：`rtk go test ./internal/messaging/feishu -run 'Test.*Media' -count=1 -race`。
6. 提交：`fix(feishu): bound media reads and restrict temp files`

### Task 5：终态投递错误、指标与静态兜底

**文件**：
- 修改：`internal/messaging/feishu/streaming.go`
- 修改：`internal/messaging/feishu/streaming_test.go`
- 修改：`internal/messaging/feishu/conn.go`
- 修改：`internal/messaging/feishu/conn_test.go`
- 修改：`internal/observability/instruments.go`
- 修改：`internal/observability/instruments_test.go`
- 修改：`docs/reference/metrics.md`

**步骤**：
1. 新增红测：final CardKit 和 IM Patch 均失败时 `Close` 返回错误；header update 失败可见。
2. 新增红测：连接层收到 Close 错误后发送静态终态兜底；兜底失败可计量。
3. 新增 lazy `sync.Once` 指标 accessor 测试。
4. 运行定向测试确认红测。
5. 用 `errors.Join` 聚合终态错误，保持成功降级路径；增加 `hotplex.streaming.terminal_failures` 与兜底结果属性。
6. 连接层只发送简短静态说明，不重复完整正文。
7. 更新 metrics 文档并运行：`rtk go test ./internal/messaging/feishu ./internal/observability -count=1 -race`。
8. 提交：`fix(feishu): surface terminal delivery failures`

### Task 6：Claude Code bypass doctor 风险告警

**文件**：
- 修改：`internal/cli/checkers/permission_mode.go`
- 修改：`internal/cli/checkers/permission_mode_test.go`
- 修改：`docs/reference/configuration.md`
- 修改：`docs/guides/production.md`

**步骤**：
1. 新增红测：有效 permission mode 为 bypass 时 Warn，workspace 时 Pass，配置不可读时 Warn 且有 FixHint。
2. 覆盖 `HOTPLEX_WORKER_DEFAULT_PERMISSION_MODE` 环境覆盖和 YAML 默认值。
3. 运行定向测试确认红测。
4. 实现只读 checker，不提供 FixFunc，不修改配置。
5. 文档说明 bypass 的信任边界、allowlist 不能替代 Worker 权限和收紧方法。
6. 运行：`rtk go test ./internal/cli/checkers -count=1 -race`。
7. 提交：`feat(doctor): warn on claude bypass permission mode`

### Task 7：全局回归、最终审查与 PR

**文件**：
- 检查：本计划涉及的全部文件

**步骤**：
1. 对 `main...HEAD` 生成最终 review package，并由独立高能力 reviewer 审查设计一致性、并发、安全和跨平台风险。
2. 修复全部 Critical/Important，重复审查直至通过。
3. 运行：`rtk go test ./internal/messaging/... ./internal/gateway ./internal/config ./internal/cli/checkers ./internal/observability -count=1 -race`。
4. 运行：`rtk make check`。
5. 检查：`rtk git diff --check`、`rtk git status --short`、Issue/Spec/测试可追溯性。
6. 删除 SDD scratch workspace，推送 `agent/feishu-claude-e2e-hardening`。
7. 创建关联 `Closes #941` 的 Draft PR，写入测试证据和剩余现场验证边界。
