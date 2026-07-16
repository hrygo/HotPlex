# Issue #902 实施计划

设计依据：`docs/superpowers/specs/2026-07-16-webchat-follow-up-queue-design.md`

## 交付原则

- 先写失败测试，再实现最小正确行为。
- 保持一个 active execution；队列项只在 `delivered` ACK 后移除。
- `unknown`、断线和 ambiguous outcome 不自动重投。
- 未发送 prompt 仅存在页面内存，不进入存储、日志或指标。
- #902 与 `handler.go` 防空输入分别形成原子 commit。

## Task 1：页面级队列存储

文件：

- 新建 `webchat/lib/adapters/follow-up-queue.ts`
- 新建 `webchat/lib/adapters/follow-up-queue.test.ts`

步骤：

1. 为 enqueue/FIFO、编辑、删除、move-to-front、状态转换、session 隔离、20 条上限和 clearSession 编写失败测试。
2. 实现 `FollowUpQueueStore`、不可变快照和 subscribe/getSnapshot。
3. 增加 `queued | sending | failed`、结构化失败原因与显式 retry。
4. 验证该模块不读取 browser persistence API、不记录 prompt。

验证：

```bash
cd webchat && pnpm vitest run lib/adapters/follow-up-queue.test.ts
```

## Task 2：Browser client stop terminal waiter

文件：

- 修改 `webchat/lib/ai-sdk-transport/client/browser-client.ts`
- 修改 `webchat/lib/ai-sdk-transport/client/browser-client.test.ts`

步骤：

1. 将旧的“send stop 立即清 pending”测试改为“stop 不提前释放 pending”。
2. 增加 stop single-flight、自然 done/`stopped_by_user`、重复 done、timeout、disconnect 测试。
3. 实现 `stopCurrentTurn()`；连续调用复用 Promise，只有 terminal 才 resolve。
4. 将 done/error/disconnect 的 stop waiter 结算集中到单一方法，避免竞态双结算。

验证：

```bash
cd webchat && pnpm vitest run lib/ai-sdk-transport/client/browser-client.test.ts
```

## Task 3：Runtime queue 与 single-flight drain

文件：

- 修改 `webchat/lib/adapters/hotplex-runtime-adapter.ts`
- 扩展 `webchat/lib/adapters/runtime-adapter.test.ts`，必要时新建纯状态机测试文件
- 修改 `webchat/app/components/chat/ChatContainer.assistant-ui.tsx`

步骤：

1. `ChatContainer` 创建稳定的页面级 store，并传给 keyed `ChatInterface`。
2. 提取公共 `dispatchInput(text, queueItemId?)`，让普通发送和队列发送共享 optimistic/rollback 逻辑。
3. 维护 running/stopping/connection refs、active queue item 与 drain single-flight。
4. 将 delivered ACK、failed/unknown ACK、done、fatal error、disconnect、reconnect、state idle 连接到幂等队列转换。
5. 实现 enqueue/edit/delete/retry/send-now/clear 操作并通过 adapter `extras` 暴露。
6. session 删除时在 `removeSession` wrapper 中清理对应队列。

验证重点：

- done 后一次只发送队首。
- delivered 移除可见项，但下一项等 terminal。
- unknown/断线保留原文并转 failed。
- 重复 ACK、terminal 与 send-now 点击不会重复发送。

## Task 4：队列面板与 composer 交互

文件：

- 新建 `webchat/components/assistant-ui/FollowUpQueue.tsx`
- 修改 `webchat/components/assistant-ui/thread.tsx`
- 修改 `webchat/locales/zh-CN/chat.json`
- 修改 `webchat/locales/en/chat.json`

步骤：

1. 在消息与 composer 之间渲染编号 FIFO 面板。
2. 实现展开/收起、编辑、保存、取消、删除、立即发送和失败重试。
3. `sending` 禁止重复操作；失败提示不只依赖颜色。
4. running/stopping 时由 composer 直接 enqueue；成功才清草稿，溢出保留草稿。
5. 支持 Enter 提交、Shift+Enter 换行、Escape 取消编辑、aria-label 与 focus ring。
6. 校验中英文 JSON key 完全一致。

验证：

```bash
cd webchat && pnpm lint && pnpm test && pnpm build
```

## Task 5：Gateway 空白 input 防御

文件：

- 将当前主工作区 `internal/gateway/handler.go` 的 4 行修改复制到 feature worktree
- 修改最合适的 `internal/gateway/*_test.go`

步骤：

1. 增加表驱动测试，覆盖空串、空格、tab、换行和非空输入。
2. 证明纯空白输入不调用命令路由、session transition 或 Worker input。
3. 保持非空 input 原语义不变。
4. 独立提交并在 commit body 说明防御目的。

验证：

```bash
go test -race -count=1 ./internal/gateway
```

## Task 6：E2E 与完整门禁

文件：

- 修改 `webchat/e2e/chat.spec.ts` 或新增专用 queue spec

步骤：

1. 使用可控 WebSocket stub 覆盖连续发送、可见队列、编辑后 dispatch、立即发送 stop→done→input 和 aria/键盘。
2. 运行 WebChat unit、production build 与 Chromium Playwright。
3. 运行 Gateway race 测试和仓库 `make check`。
4. 检查 `git diff --check`、翻译 key 对齐、无持久化/日志泄漏路径。

验证：

```bash
cd webchat && pnpm test && pnpm build && pnpm test:e2e
go test -race -count=1 ./internal/gateway
make check
```

## Task 7：提交、审查与 PR

提交顺序：

1. `docs(webchat): design follow-up queue for #902`
2. `feat(webchat): add editable follow-up queue`
3. `fix(gateway): ignore blank input payloads`
4. 后续测试或审查修复仅在逻辑独立时拆分，否则归入对应实现提交前完成。

交付：

1. 自审全部 diff，重点检查竞态、隐私、session 隔离与 a11y。
2. 推送 feature 分支并创建中文 PR，正文逐项映射 #902 验收标准，使用 `Closes #902`。
3. 检查 PR checks 与当前 HEAD review；一次性修复 P0/P1 和有价值的 P2。
4. 仅在 checks 通过、无未处理高优先级 review、PR mergeable 且验收审计有直接证据时完成交付。
