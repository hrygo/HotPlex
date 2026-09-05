---
title: Worker 与平台恢复能力对齐实施计划
weight: 10
description: 使用 luna_worker 原子任务修复会话恢复链路中的功能差异。
---

# Worker 与平台恢复能力对齐实施计划

**Goal:** 统一三端会话恢复失败语义，接通 ACP 历史降级，保留 Worker 原生差异。

**Architecture:** 复用 Bridge 的 Seq hydration、SessionInfo.ConversationHistory 和 Worker 原有降级分支；不新增协议字段或数据库结构。

**Tech Stack:** Go、testify、现有 Worker 协议 fake、Vitest、Playwright。

**Spec:** `docs/superpowers/specs/2026-09-05-platform-worker-parity-design.md`。

## 约束

执行者为当前环境提供的 `luna_worker`。子代理仅修改明确授权文件，不提交或推送，由主 Agent 完成独立审查与集成。每任务先 RED 后 GREEN；禁止使用 sleep 等待事件。`bridge.go` 的任务必须串行执行。测试不得启动真实 Worker CLI 或访问真实平台。

## Task 1：Seq hydration 失败时中止恢复（R1）

Files：`internal/gateway/bridge.go`、新增 `internal/gateway/bridge_resume_hydration_test.go`。

Consumes：`Hub.EnsureSeqHydrated(sessionID string) error`、既有 `mockBridgeSM` 和 `mockSeqHydrator`。Produces：失败时会话、旧 Worker 和序号不变的 `ResumeSession`。

- [ ] 为四种 Worker 构造 `mockSeqHydrator{err: errors.New("event store unavailable")}`，调用 `ResumeSession`，断言 `require.ErrorContains(t, err, "hydrate")`、工厂未调用、Seq 未分配、旧 Worker未被终止、原会话状态不变。
- [ ] 执行 `go test ./internal/gateway -run 'TestBridge_Resume.*Hydrat' -count=1`，确认旧实现无法通过失败返回断言。
- [ ] 把 hydration 移至破坏性生命周期操作之前，用 Gateway 内独立哨兵标识无法安全恢复；`StartPlatformSession` 的两条恢复失败分支识别该哨兵并返回，不能转成 fresh start：

```go
if err := b.hub.EnsureSeqHydrated(id); err != nil {
    return fmt.Errorf("%w: %w", ErrResumeSequenceUnavailable, err)
}
```

- [ ] 覆盖 `StartPlatformSession` 的 IDLE / TERMINATED 恢复错误不创建替代会话，以及 hydration 修复后同会话可重试成功；运行 `go test ./internal/gateway -count=1 -race -shuffle=on`。
- [ ] 主 Agent 审查仅包含该行为后独立提交。

## Task 2：Gateway 提供 ACP 恢复历史（R2）

Files：`internal/gateway/bridge.go`、`internal/gateway/bridge_history_recovery_test.go`。依赖 Task 1 释放 `bridge.go` 所有权。

Consumes：`prepareWorkerInfo`、现有 turns 查询与压缩。Produces：ACP 的 `SessionInfo.ConversationHistory`，Codex 行为保持一致。

- [ ] 新增 ACP 表驱动测试，覆盖已有历史、新会话空历史、查询错误；用 literal turn 内容断言返回字段，不能直接赋值 pendingHistory 代替边界测试。
- [ ] 执行 `go test ./internal/gateway -run 'TestPrepareWorkerInfo' -count=1`，确认 ACP 历史断言在旧代码失败。
- [ ] 把现有历史查询条件改为：

```go
if (si.WorkerType == worker.TypeCodexCLI || si.WorkerType == worker.TypeACP) && b.turnsQuerier != nil {
    // 原有有界查询、缓存与压缩流程。
}
```

- [ ] 确认 Claude / OpenCode 原生路径不会因此额外查询历史；运行精准测试及 Gateway race。
- [ ] 主 Agent 审查并独立提交。

## Task 3：ACP 不支持 loadSession 时启用已有降级（R3）

Files：`internal/worker/acp/worker.go`、`internal/worker/acp/worker_history_test.go`，必要新增同包协议测试文件。

Consumes：ACP 测试客户端、Start/Resume 握手、`ConversationHistory`。Produces：恢复不可用时首轮历史注入与既有提示，不影响成功原生恢复。

- [ ] 使用现有协议 fake 构造 agent 未声明 loadSession、输入有 WorkerSessionID 和历史的场景；首次 Input 断言含 `CONVERSATION_HISTORY_RECOVERY_START` 和既有内容，下一次 Input 断言不含历史；检查已有恢复提示。
- [ ] 执行对应精准测试，确认旧实现遗漏历史。
- [ ] 在已知旧会话但没有 loadSession 的新建分支设置 `historyLost = true`，复用现有 seed 和提示逻辑；真正新会话和原生成功恢复保持不注入。
- [ ] 运行 `go test ./internal/worker/acp -count=1 -race -shuffle=on`；如果测试发现新建且已有恢复历史也被遗漏，以明确断言覆盖后修复同一降级语义。
- [ ] 主 Agent 复核不重投未知执行、不改变 wire contract 后独立提交。

## 后续任务形成条件

OpenCode 恢复提示、平台交互与 WebChat 光标问题必须先复核真实消费路径，再形成各自原子任务。不得把初步失败直接解释为已确定根因。最终统一复跑 `make test-contract-matrix`、涉及的 Go race 包、WebChat 单元测试与 Chromium 矩阵。
