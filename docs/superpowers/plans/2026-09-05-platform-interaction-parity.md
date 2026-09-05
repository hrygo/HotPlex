---
title: 平台交互与媒体边界对齐计划
weight: 10
description: 修复 Slack 文本审批目标匹配、OpenCode 问答位置和飞书媒体访问控制。
---

# 平台交互与媒体边界对齐计划

**Goal:** 让权限、问答和附件输入在平台边界保持用户意图与访问控制。

**Architecture:** 修改现有文本解析、位置数组编码和 Gate 顺序；保留 AEP 与 Worker 接口。

**Tech Stack:** Go、testify、httptest、现有平台 API fake。

**Spec:** `docs/superpowers/specs/2026-09-05-platform-worker-parity-design.md` R5–R11。

## 执行结果（2026-09-05）

Task 1–7 已完成。对应提交：Task 1 `7943eee`；Task 2 `e633863`；Task 3 `542d171`；Task 4 `d6dc1d8`；Task 5 `7abddc3`；Task 6 `da108c9` 与 `f961414`；Task 7 `4b3de98`。下方保留实施前清单，不将每条预定操作等同实际日志；最终证据见设计复核的“已集成记录”和“最终验证”。Task 6 浏览器测试独立落在 `webchat/e2e/terminal-interactions.spec.ts`，主 Agent 与核心矩阵合并运行 21 项全部通过。

## Task 1：Slack 文本响应路由（R5）

Files：`internal/messaging/slack/interaction.go`、`internal/messaging/slack/interaction_test.go`。

Consumes：`checkPendingInteraction` 与 InteractionManager。Produces：完整自由文本、精确审批目标、未知显式 ID 安全拒绝。

- [ ] 测试 `use PostgreSQL for storage` 作为 question 完整回传；`allow req-MixedCase` 精确匹配；`allow stale-id` 在有其他 pending 请求时不调用任何 SendResponse，并保留请求。
- [ ] 精准运行 `go test ./internal/messaging/slack -run TestCheckPendingInteraction -count=1`，确认 RED；替换原本要求错误 ID 回退成功的测试。
- [ ] 仅对已知 action 解析 ID，动作可忽略大小写，ID 保留原始大小写。显式 ID 未找到时提示过期/未知并消费该响应，禁止作为普通 prompt 或回退审批。
- [ ] 未带 ID 的动作只按明确的候选选择规则处理；普通 question 使用原文本；拒绝错误 owner，投递失败保持 pending 可重试。覆盖双 pending 和正常成功分支。
- [ ] 执行 Slack race/shuffle，主 Agent 审查后独立提交。

## Task 2：OpenCode 问答数组保留题号（R6）

Files：`internal/worker/opencodeserver/worker.go`、`internal/worker/opencodeserver/commands_test.go`。

Consumes：`answersToOrderedArrays`、`answerOptionsToOrderedArrays`。Produces：与 questionOrder 一一对应的回答位置。

- [ ] 对 `questionOrder = []string{"first", "second"}`、仅 second 有值的输入断言输出 `[][]string{{}, {"answer"}}`；HTTP JSON 必须是 `[[],["answer"]]`。
- [ ] 覆盖前、中、尾缺题，全空、非空多选和额外 key；两种函数均先 RED。
- [ ] 每个有序题目都 append 一个非 nil 数组，缺题用 `[]string{}`。extra key 仍排序追加；不修改协议类型。
- [ ] 运行 OpenCode 包 race/shuffle 和既有问题顺序测试，主 Agent 审查后独立提交。

## Task 3：飞书先鉴权后处理媒体（R7）

Files：`internal/messaging/feishu/handler.go`、新 `internal/messaging/feishu/handler_media_gate_test.go`。

Consumes：现有 Gate、下载 fake 与转写 fake。Produces：拒绝消息无媒体 I/O，允许消息沿原路径处理。

- [ ] 被 allowlist 拒绝的图片和语音事件中，下载/转写计数保持 0，dedup 可以再次 TryRecord；先观察旧实现失败。
- [ ] 提前解析 chatType/chatID/userID 和 mention gate；媒体处理移到 Gate 成功之后。保留其余命令、队列与消息语义。
- [ ] 补一个允许媒体消息的正向断言，运行飞书包 race/shuffle 与 F-C/F-O/F-X/F-A 矩阵。
- [ ] 主 Agent 审查后独立提交。

## 执行规则

每次只派发一个 Task 给 `luna_worker`，文件所有权明确；互不重叠的 Task 可并行。子代理不提交、不推送、不访问真实平台；主 Agent 统一集成。结果须包含失败断言与修复后验证，不以静态 helper 测试替代关键 HTTP/入口边界。

## Task 4：WebChat stop 后消息终态收敛

Files：`webchat/lib/adapters/hotplex-runtime-adapter.ts`、`webchat/e2e/platform-worker-matrix.spec.ts`，必要新增同目录纯逻辑测试文件。

Consumes：现有 done listener、增量批次、message IDs 和四Worker mock gateway。Produces：停止后所有属于该轮的 streaming 标记与增量批次结算，下一轮不受旧批次影响。

- [ ] 先检查失败 artifact 与事件/消息 ID，构造可确定性复现的 stop / pending delta 顺序；不得把 ACP 标签当作根因。
- [ ] 用现有测试入口证明停止后发送按钮可用而光标未清除，或证明原失败属于测试隔离并给出具体依据。
- [ ] 只修复复现根因；禁止增加 sleep、延长超时、隐藏光标或删除断言。真实 Chrome 与 fakeWS 保留旧文本且能完成下一轮。
- [ ] 运行新增精准测试、Vitest、TypeScript 和四Worker Chromium 矩阵，主 Agent 审查后独立提交。

## Task 5：ACP 已知问答扩展规范化（R8）

Files：`internal/worker/acp/worker.go`、新增 `internal/worker/acp/interaction_requests.go` 和同名测试，现有 `worker_phase2_test.go` 的 unknown fixture 改为真正未知方法；必要时 `client.go` 增加沿用 writeMu 与有界写入的私有协议错误响应方法。与恢复计划 Task3 串行。

Consumes：已有 `session/request_question`、`session/request_elicitation` request 与对应 response 方法。Produces：已有 AEP QuestionRequest/ElicitationRequest，不新增 wire 字段。

- [ ] 先写单 question 字符串、多题数组、elicitation schema 的协议测试；断言标准事件类型、原始 request ID、问题/表单内容，以及响应写回同一 JSON-RPC ID。
- [ ] 明确旧实现发 Raw 的 RED 断言；合法未知扩展仍保持 Raw，不被意外识别成标准请求。
- [ ] 用私有 mapper 将明确已知方法规范化，保留 pendingRequests 在成功写回前可重试；类型或必填项错误返回明确协议错误，不保留无法操作的 pending。
- [ ] 运行 ACP 包 race/shuffle、现有 Gateway interaction 契约；主 Agent 审查参数和响应类型后独立提交。

## Task 6：WebChat 终态结束未完成交互（R9）

Files：`webchat/lib/adapters/hotplex-runtime-adapter.ts`、新增纯逻辑 helper/test（仅在能减少重复时）、`webchat/e2e/platform-worker-matrix.spec.ts`，必须等待 Task4 释放同文件。

拆分为两步：先由独立 worker 仅实现 `terminal-interactions.ts` 与单测，提供不可变的指定 ID 过期函数；再由 WebChat worker 集成订阅、map/timer 清理和浏览器回归。两步不同时修改 runtime 或相同测试文件。

Consumes：已有 InteractionStatus 中的 expired 与 interaction map/timer。Produces：done / session terminated 后不可提交的未完成卡片。

- [ ] 三种交互分别注入 pending 和 submitting，然后发送 done 或 SESSION_TERMINATED，断言旧按钮不可操作、timer 不再回写、下一轮不继承旧 pending；先验证 RED。
- [ ] 只结束当前会话当前未完成请求，resolved/rejected 保持原状态；用现有 expired 状态，不新增协议枚举。迟到 ACK 不能复活已过期卡片。
- [ ] 临时断线保持与重连策略一致，不把可能继续的原生请求永久关闭；在连接恢复前提交应有明确失败反馈。
- [ ] 运行精准测试、Vitest、TypeScript 与浏览器回归，主 Agent 审查后独立提交。

## Task 7：命令目录与原生能力一致（R10）

Files：`internal/gateway/native_catalog.go`、`internal/gateway/native_catalog_test.go`；Codex 错误分类仅改 `internal/worker/codexcli/commands.go` 和对应测试。

Consumes：既有接口门控和具体 adapter 实现。Produces：不把明确未实现的命令列为可用，保留原有固定名称优先级。

- [ ] 先写矩阵断言：Codex 不展示 model/perm，ACP 不展示 compact/rewind/mcp，Claude 不展示 clear；已有真实支持项继续展示。
- [ ] 目录可见性与名称占用分开：保留原 requires 决定的固定名称集合，已占用但隐藏的名字不能从 Worker 或文件系统层重新出现；显式 /worker 也不能绕过固定命令处理。
- [ ] Codex set_model 返回现有 ErrNotImplemented，手工输入不支持命令仍获得明确能力错误；不添加新 Worker 接口或 AEP 字段。
- [ ] 精确目录测试与 Codex 命令测试 race/shuffle，审查后独立提交。
