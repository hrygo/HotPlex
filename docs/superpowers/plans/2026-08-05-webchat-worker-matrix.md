# WebChat 四 Worker 矩阵 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把当前主要固定 `codex_cli` 的 WebChat 浏览器测试扩展为 W-C/W-O/W-X/W-A 四个可审计组合，覆盖 init、输入 ACK、stream、interaction、stop waiter、唯一 Done 和下一轮。

**Architecture:** Playwright 继续运行真实 WebChat 页面、runtime adapter 和 BrowserHotPlexClient，只替换 `/api/*` 与 `/ws` 外部对端；共享 mock gateway 接收明确的 worker 参数并记录完整 AEP outbound。Vitest 在 transport 层参数化四个 Worker，验证浏览器状态机不依赖某个 Worker 名称。

**Tech Stack:** TypeScript 6、Vitest、Playwright Chromium、Next.js、AEP v1。

## Frozen Matrix

```ts
export const WEBCHAT_WORKERS = [
  { id: "W-C", workerType: "claude_code" },
  { id: "W-O", workerType: "opencode_server" },
  { id: "W-X", workerType: "codex_cli" },
  { id: "W-A", workerType: "acp" },
] as const;
```

每个 Playwright test title 必须包含 `ID/webchat/worker/scenario`。四行都运行同一 core 流程，不按 Worker skip；协议差异已在 Go Worker probe 层验证，浏览器层验证 worker selection 和共享 AEP 行为。

---

### Task 1: 先修复现有 Playwright 语义基线

**Files:**
- Modify: `webchat/components/assistant-ui/FollowUpQueue.tsx`
- Modify: `webchat/e2e/chat.spec.ts`

**Verified baseline:** 2026-08-05 使用本机 Chrome 运行当前 `chat.spec.ts`：2/12 PASS、10/12 FAIL。截图证明 queue pills 实际存在，但组件重设计后丢失既有 `region/list/listitem` 与 action aria-label；测试仍按保留在 i18n 中的 `follow_up.aria.*` 查找，因此大部分流程在 selector 处提前终止。当前 CI 没有 Playwright job，未能阻止该回归。

- [ ] **Step 1: 保存 RED 证据**

Run: `rtk pnpm --dir webchat exec playwright test e2e/chat.spec.ts --project=chromium`

Expected before fix: queue 场景找不到 `region "后续消息队列"`、`listitem` 或 indexed edit/delete/send-now button；如果本机缺 Playwright browser，先按 Task 5 的 CI 安装命令安装 Chromium，不能把 browser missing 当测试 RED。

- [ ] **Step 2: 恢复可访问 DOM 合同**

组件已有完整中英文 i18n keys，不新增硬编码文案。固定结构：

- 根容器 `role="region" aria-label={t("follow_up.aria.panel")}`；
- pills row `role="list"`；
- 每个 item 用 `motion.div role="listitem"` 包裹；
- 展开 pill 是独立 `<button>`，aria-label 使用 `follow_up.aria.expand`/`collapse` + 1-based position；
- quick delete 是与展开按钮并列的真实 `<button>`，aria-label=`follow_up.aria.delete`；禁止 button 内嵌 button 或 `span role="button"`；
- popover textarea/action buttons分别使用 `edit_input`、`edit`、`delete`、`retry`、`send_now` 的 indexed aria-label；
- active item 被删除后同时关闭 popover，保持现有状态语义。

视觉 class 可原样迁到 wrapper/两个按钮；不得为通过测试退回旧面板 UI。

- [ ] **Step 3: 更新 selector，不使用 CSS class**

`chat.spec.ts` 继续使用 role/name：region → listitem by text → indexed action button。禁止 `.locator(".class")`、nth-only selector 或 `waitForTimeout` 规避。现有唯一 `waitForTimeout(100)` 改为对 outbound count/状态的 `expect.poll`。

- [ ] **Step 4: 验证 12/12 baseline**

Run: `rtk pnpm --dir webchat test`

Run: `rtk pnpm --dir webchat exec tsc --noEmit`

Run: `rtk pnpm --dir webchat exec playwright test e2e/chat.spec.ts --project=chromium --reporter=list`

Expected: 12/12 PASS、0 skip。若语义修复后仍有 dispatch/state assertion 失败，先按当前 queue store/runtime adapter 行为修复并单独 review；禁止进入四 Worker 参数化。

- [ ] **Step 5: Commit**

```bash
rtk git add webchat/components/assistant-ui/FollowUpQueue.tsx webchat/e2e/chat.spec.ts
rtk git commit -m "fix(webchat): restore queue accessibility contract"
```

### Task 2: 抽取可参数化 mock gateway

**Files:**
- Create: `webchat/e2e/fixtures/mock-gateway.ts`
- Modify: `webchat/e2e/chat.spec.ts`

**Interfaces:**
- Produces: `installMockGateway(page, workerType)`、`sentEvents(page)`、`sentInputs(page)`、`emitGatewayEvent(page, kind, data, id?)`、`emitDone(page, id, reason?)`。
- Consumes: 当前 `chat.spec.ts` 内联实现；必须保持其现有 queue/reconnect/failure 测试行为。

- [ ] **Step 1: 再确认绿色 browser baseline**

Run: `rtk pnpm --dir webchat exec playwright test e2e/chat.spec.ts --project=chromium`

Expected: 12/12 PASS。baseline 失败时暂停，不在失败基线上抽 fixture。

- [ ] **Step 2: 新建 fixture 并先写类型红测**

`MockGatewayWindow` 明确暴露：

```ts
type MockGatewayWindow = Window & {
  __aepEvents: Envelope[];
  __mockAEP: {
    emit(type: string, data: Record<string, unknown>, id?: string): void;
    disconnect(): void;
    pauseNextConnect(): void;
    setNextInitState(state: "idle" | "running"): void;
    setNextInputOutcome(outcome: "delivered" | "unknown" | "failed"): void;
  };
};
```

先让 `chat.spec.ts` import 尚不存在的 exports。

Run: `rtk pnpm --dir webchat exec tsc --noEmit`

Expected: FAIL only because fixture/exports are missing。

- [ ] **Step 3: 搬迁，不改变现有语义**

把 `addInitScript`、API route、event helpers逐字迁入 fixture。`installMockGateway` 必须把 `workerType` 传给浏览器 init script/route closure，不读取进程环境或全局可变变量。

API fake 返回完整一致数据：

- workspace `worker_preference=workerType`；
- session `worker_type=workerType`；
- `/api/workers` 返回四行 `{type, installed:true}`，不是只返回当前 Worker；
- `/ws` 收到的 init envelope `event.data.worker_type` 必须由测试断言等于 workerType；
- 所有其余字段保持当前 fixture 结构，避免页面走隐式 fallback。

- [ ] **Step 4: 验证无行为回归**

Run: `rtk pnpm --dir webchat exec tsc --noEmit`

Run: `rtk pnpm --dir webchat exec playwright test e2e/chat.spec.ts --project=chromium`

Expected: 与 baseline 相同 test count、全部 PASS。

- [ ] **Step 5: Commit**

```bash
rtk git add webchat/e2e/fixtures/mock-gateway.ts webchat/e2e/chat.spec.ts
rtk git commit -m "test(webchat): extract parameterized gateway fixture"
```

### Task 3: 增加 W-C/W-O/W-X/W-A 页面级 core flow

**Files:**
- Create: `webchat/e2e/platform-worker-matrix.spec.ts`

**Interfaces:**
- Consumes: `WEBCHAT_WORKERS` 和 mock gateway fixture。
- Produces: 四个组合的 C01/C04/C05/K01 页面/transport Test 证据。

- [ ] **Step 1: 写 W-C init/input RED**

测试进入当前 chat route，等待 composer，可见状态下发送 literal `matrix-basic-W-C`。从 `__aepEvents` 断言：

- init `worker_type="claude_code"`；
- input content 和 client ID 非空；
- fake 返回两阶段 `input.ack` 后 pending input 状态正确。

再 emit `message.start`、两条 `message.delta`、`done(reason="completed")`，断言页面只出现一份拼接结果和完成状态。

Run: `rtk pnpm --dir webchat exec playwright test 'e2e/platform-worker-matrix.spec.ts' --grep 'W-C/webchat/claude_code/C01'`

Expected: FAIL because file/flow does not exist。

- [ ] **Step 2: 扩为四行 exact loop**

使用上方 literal `WEBCHAT_WORKERS`。另写一项独立 test 断言 IDs 恰好为 `['W-C','W-O','W-X','W-A']`，避免循环数组漏行时整套仍绿。

每个组合的单一 Playwright flow 固定执行：

1. C01 init → input → accepted/delivered ACK → delta → completed done；
2. K01 emit `permission_request`，页面显示 permission card，点击 Allow；outbound 恰好一个 `permission_response`，ID/allowed 保真；
3. 第二个长 turn 先 emit delta；用户触发现有 stop UI；outbound `control` action=`stop` 恰好一个；
4. 连续第二次 stop 不产生第二个 control；
5. emit 一个 `done(reason="stopped_by_user")`，页面停止 pending；重复相同 done ID 不产生第二条 terminal UI；
6. 同 session 发送 `matrix-next-<ID>`，收到正常 completed done，输入列表增加一次。

交互 fixture 使用固定、无敏感数据：permission ID=`permission-<ID>`、tool=`Read`、受控路径=`/tmp/hotplex-e2e/sample.txt`。不得使用本机用户路径。

- [ ] **Step 3: 验证四组合都真实执行**

Run: `rtk pnpm --dir webchat exec playwright test e2e/platform-worker-matrix.spec.ts --project=chromium --reporter=list`

Expected: 输出含 W-C/W-O/W-X/W-A，0 skipped。若某行因为 UI state 共享而失败，修 fixture 隔离，禁止 serial 或 retry 掩盖。

Mutation check: `/api/sessions` 强制返回 `codex_cli` 时 W-C/W-O/W-A 的 init assertion 必须失败；删掉 stop coalescing 时每行 control count 失败。

- [ ] **Step 4: Commit**

```bash
rtk git add webchat/e2e/platform-worker-matrix.spec.ts
rtk git commit -m "test(webchat): cover four worker browser combinations"
```

### Task 4: 参数化 BrowserHotPlexClient stop 状态机

**Files:**
- Modify: `webchat/lib/ai-sdk-transport/client/browser-client.test.ts`

**Interfaces:**
- Consumes: `WorkerType.ClaudeCode/OpenCodeServer/CodexCLI/ACP`。
- Produces: 四 Worker 的 stop waiter、重复 Done、timeout、disconnect、next-turn unit contract。

- [ ] **Step 1: 提取 literal worker table**

把当前只用 `WorkerType.CodexCLI` 的 stop tests 包在：

```ts
describe.each([
  ["W-C", WorkerType.ClaudeCode],
  ["W-O", WorkerType.OpenCodeServer],
  ["W-X", WorkerType.CodexCLI],
  ["W-A", WorkerType.ACP],
] as const)("%s browser stop contract", (_id, workerType) => { ... });
```

至少参数化以下现有行为：重复 stop 共用同一个 Promise 且只发送一次；unrelated error 不结束 waiter；timeout 不释放 active input；disconnect rejects；Done 后可发送下一 input。

- [ ] **Step 2: 写重复 Done/next-turn 红测**

第一次 stopped Done resolve stop 与 input；路由同 ID Done 第二次不重复 emit；随后 `sendInputAsync("next")` 可建立新 pending，并由 completed Done 正常 resolve。

Run: `rtk pnpm --dir webchat exec vitest run lib/ai-sdk-transport/client/browser-client.test.ts`

若新增测试在当前实现已 PASS，执行 mutation：临时删除 done 去重或 pending 清理断言验证测试会失败，再恢复源码；只提交测试。

- [ ] **Step 3: 验证 timer/cleanup**

所有 timeout 测试使用 `vi.useFakeTimers()` + `advanceTimersByTimeAsync`，afterEach 恢复 real timers；不得真实等待 500ms/30s。每个 Worker 行结束断言没有 pending stop timer/listener。

- [ ] **Step 4: Commit**

```bash
rtk git add webchat/lib/ai-sdk-transport/client/browser-client.test.ts
rtk git commit -m "test(webchat): enforce stop contract for every worker"
```

### Task 5: 增加本地脚本和独立 CI job

**Files:**
- Modify: `webchat/package.json`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Produces: `pnpm --dir webchat test:e2e:matrix`；CI job `WebChat platform-worker matrix`。

- [ ] **Step 1: 验证脚本 RED**

Run: `rtk pnpm --dir webchat test:e2e:matrix`

Expected: FAIL with missing script。

- [ ] **Step 2: 增加 script**

```json
"test:e2e:matrix": "playwright test e2e/platform-worker-matrix.spec.ts --project=chromium"
```

- [ ] **Step 3: 增加 CI job**

独立 job 不依赖 setup action 的 cache-hit 分支，固定步骤：checkout → `pnpm/action-setup@v6` version 11.4.0 → `actions/setup-node@v6` Node 22 + pnpm cache → `pnpm --dir webchat install --frozen-lockfile` → `pnpm --dir webchat exec playwright install --with-deps chromium` → `pnpm --dir webchat test` → `tsc --noEmit` → 现有 `chat.spec.ts` 12 项 → `test:e2e:matrix`。

timeout 10 分钟；失败时上传 `webchat/playwright-report` 与 `webchat/test-results`，retention 7 天。job 不配置 Gateway、Slack、飞书或 Worker credentials。

- [ ] **Step 4: 本地验证**

Run: `rtk pnpm --dir webchat test`

Run: `rtk pnpm --dir webchat exec tsc --noEmit`

Run: `rtk pnpm --dir webchat test:e2e:matrix`

Expected: 0 fail、W-C/W-O/W-X/W-A 全部出现、0 skip。

- [ ] **Step 5: Commit**

```bash
rtk git add webchat/package.json .github/workflows/ci.yml
rtk git commit -m "ci(webchat): gate four worker browser matrix"
```

### Task 6: WebChat 子计划回归门禁

- [ ] `rtk pnpm --dir webchat test`
- [ ] `rtk pnpm --dir webchat exec tsc --noEmit`
- [ ] `rtk pnpm --dir webchat exec playwright test e2e/chat.spec.ts e2e/platform-worker-matrix.spec.ts --project=chromium`
- [ ] `rtk make test-contract-matrix`
- [ ] `rtk git diff --check`
- [ ] reviewer 核对四行 API session/workspace/init worker 一致；测试穿过真实页面/runtime/client；没有真实凭证；Playwright retry 不用于掩盖状态泄漏。

Expected: 0 fail、0 skip；普通 CI 中 WebChat 四组合可按 ID 独立定位。
