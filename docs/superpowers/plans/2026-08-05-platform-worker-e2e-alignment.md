# 飞书、Slack、WebChat × 四 Worker E2E 对齐总实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改变 AEP wire contract 的前提下，完成 3 个平台 × 4 个 Worker 的确定性 CI 契约、Slack 可靠性补强、Worker 生命周期对齐、WebChat 四 Worker 回归，以及真实 12 组合人工验收闭环。

**Architecture:** 使用“平台驱动 × Gateway harness × Worker protocol probe”的契约乘积。平台驱动保留真实 ingress/converter/dedup/PlatformConn，Gateway harness 保留真实 messaging Bridge、Gateway Handler/Bridge/Hub，Worker probe 使用各 Worker 的真实 parser/mapper 并用确定性协议 fake 替代外部进程；平台专属故障注入留在平台包内。

**Tech Stack:** Go 1.x、AEP v1、testify/require、SQLite 测试 store、Feishu Go SDK、Slack Go SDK、TypeScript、Vitest、Playwright、OpenTelemetry、GitHub Actions。

## Global Constraints

- 所有 shell 命令使用 `rtk` 前缀；源码编辑使用 Edit/apply_patch，不使用 `sed -i` 或 `awk` 修改源码。
- 严格 TDD：每个生产行为先写测试、观察预期失败、再写最小实现；测试意外通过或因语法错误失败时不得进入实现。
- Go 测试使用 `testify/require`、table-driven、`t.Parallel()`；修改进程级全局配置或 singleton 的测试不得并行。
- 异步测试禁止用 `time.Sleep` 等待结果；使用 channel 信号或 `require.Eventually`。单模块 `-count=1 -race` 目标不超过 5 秒。
- 普通 CI 必须运行 12/12 确定性组合；真实 12 组合只由人工验证，不使用外部凭证进入普通 CI。
- 证据严格分为 Source、Test、Live；mock/fixture 结果不得表述为真实 SaaS E2E。
- 能力语义固定为 `Native`、`GatewayFallback`、`Unsupported`、`NotApplicable`；skip 不计 pass。
- `control.stop` 只停止当前 turn，保留 session；至多一个 `done.reason="stopped_by_user"`，不得触发 crash fallback。
- 不修改 AEP Kind、Data、JSON tag；如必须修改，停止本计划并拆分协议 Issue/Spec。
- 不记录 prompt 原文、metadata 值、token/cookie、真实用户标识或原始 Worker 错误；仅记录受控分类和 SHA-256 短指纹。
- Worker 接口如发生变更，必须同步四个 adapter、`internal/worker/noop`、测试 mock 与 `registry_test`；本方案默认不修改 `worker.Worker`/`worker.Capabilities`。
- 指标通过 lazy `sync.Once` accessor 注册；label 只允许 `platform`、`worker_type`、`phase`、`result` 等低基数枚举。

---

## 1. 交付拆分与依赖

| 顺序 | 子计划 | 独立验收结果 | 依赖 |
| --- | --- | --- | --- |
| 1 | `2026-08-05-platform-worker-contract-matrix.md` | CI 可审计地运行 12/12，测试名和证据等级准确 | 无 |
| 2 | `2026-08-05-slack-reliability-alignment.md` | Slack media/dedup/terminal 与飞书可靠性基线对齐 | 子计划 1 的平台场景接口 |
| 3 | `2026-08-05-worker-lifecycle-alignment.md` | OCS 真实 abort；四 Worker stop/reset/resume/mid-turn 契约一致 | 子计划 1 的 Worker profile/probe |
| 4 | `2026-08-05-webchat-worker-matrix.md` | WebChat 四 Worker UI/transport 参数化回归 | 子计划 1 的组合 ID |
| 5 | `2026-08-05-platform-worker-live-validation.md` | 人工 runbook、模板和当前 commit 的 12/12 Live 记录 | 子计划 1–4 全部合并 |

每个子计划必须独立提交、独立审查。不得把五个子计划压成一个大提交；任何一个子计划未通过自己的门禁时，不开始下一个子计划。

## 2. 文件职责锁定

### 2.1 契约矩阵

- `internal/gateway/interaction_matrix_test.go`：仅保留 Gateway metadata dispatch 证据，重命名测试，禁止继续称完整 E2E。
- `internal/gateway/contracttest/harness.go`：测试工具；创建真实 Hub/Bridge/Handler、SQLite session store、确定性 worker factory，并提供清理函数。
- `internal/e2econtract/manifest.go`：纯测试合同；定义三平台、12 组合 ID、四 Worker capability profile 和 Native/fallback 语义，不依赖 `internal/gateway`/`internal/messaging`。
- `internal/gateway/contracttest/scenario.go`：C01–C08/K01–K05 的共享 scenario runner 与 driver 接口。
- `internal/gateway/contracttest/worker_probe.go`：实现 `worker.Worker` 的确定性 probe，按 Worker 类型调用真实 parser/mapper。
- `internal/messaging/feishu/platform_worker_matrix_test.go`：飞书四 Worker；入口从 `handleMessage` 开始，终态通过真实 `FeishuConn` 观察。
- `internal/messaging/slack/platform_worker_matrix_test.go`：Slack 四 Worker；入口从 `handleMessageEvent` 开始，终态通过真实 `SlackConn` 观察。
- `internal/gateway/webchat_worker_matrix_test.go`：WebChat 四 Worker；入口从真实 WebSocket/AEP Conn 开始。
- `Makefile`：增加独立的 `test-contract-matrix`；普通 CI 显式调用并审计 12 个组合 ID，不重复塞入已经运行全量 Go test 的 `quality`。

### 2.2 Slack 对齐

- `internal/messaging/slack/converter.go`：媒体下载的有界 writer（Slack SDK 接收 `io.Writer`）、原子落盘与最小权限。
- `internal/messaging/slack/adapter.go`：dedup handle 生命周期、stream writer close 错误返回。
- `internal/messaging/slack/stream.go`、`conn_events.go`：stream close 的 typed error、done/error terminal 错误传播与有界 fallback。
- `internal/messaging/slack/converter_test.go`：10 MiB 边界、部分文件清理、Unix mode。
- `internal/messaging/slack/adapter_test.go`：dedup rollback、成功提交、close 幂等与错误。
- `internal/messaging/slack/e2e_test.go`：原始 Slack event → Bridge 的重试闭环。
- `internal/observability/instruments.go`、`docs/reference/metrics.md`：复用既有 terminal failure/fallback 指标并把描述扩为多平台。

### 2.3 Worker 生命周期

- `internal/worker/opencodeserver/worker.go`：`abortOCSSession` 与 `StopCurrentTurn`；不删除 session。
- `internal/worker/opencodeserver/worker_test.go`：HTTP method/path/query/status/timeout/idempotency。
- `internal/worker/base/worker.go` 与四个 Worker `Input`：下一主 turn 开始时清除 stopped marker；metadata/mid-turn 不清除。
- `internal/worker/{claudecode,codexcli,acp,opencodeserver}/worker_test.go`：stop 无活动 turn、重复 stop、next-turn、reset/resume。
- `internal/gateway/stop_contract_test.go`：四 Worker profile 的单 terminal、runtime 收敛、crash fallback 抑制。
- `internal/gateway/stop_fence.go`：同一 session/worker run 的 stop 只允许一次有效中止；失败可条件重试，下一主 turn 清除。
- `internal/gateway/reset_contract_test.go`：保留现有 replacement/in-place 断言并扩展四 Worker 表。
- `internal/gateway/pending_buffer_test.go`：Claude/Codex Native 与 OCS/ACP fallback 行为。

### 2.4 WebChat

- `webchat/components/assistant-ui/FollowUpQueue.tsx`：恢复 queue 的 region/list/listitem/action 可访问语义；当前 Playwright 红基线的前置修复。
- `webchat/e2e/chat.spec.ts`：抽取通用 mock gateway，不再固定 `codex_cli`。
- `webchat/e2e/platform-worker-matrix.spec.ts`：四 Worker 参数化 ACK、stream、stop、Done、next-turn、interaction。
- `webchat/lib/ai-sdk-transport/client/browser-client.test.ts`：stop waiter、重复 Done、超时和下一轮的低层回归。
- `webchat/package.json`：增加只运行矩阵的脚本 `test:e2e:matrix`。
- `.github/workflows/ci.yml`：在 WebChat job 中运行该矩阵，不使用真实凭证。

### 2.5 人工验收

- `docs/guides/developer/platform-worker-e2e-validation.md`：逐组合人工操作、证据采集、失败分类、清理。
- `docs/assets/e2e/platform-worker-matrix-template.md`：12 行固定模板。
- `docs/assets/e2e/platform-worker-matrix-<commit>.md`：执行时创建的当前 commit 记录；不得提前标 PASS。
- Issue #954：记录每个子计划/PR、验证命令和 Live 记录链接。

## 3. 稳定接口

子计划 1 必须先提供以下测试接口；后续计划只消费，不自行另造同义类型：

```go
package e2econtract

type Platform string

const (
	PlatformFeishu Platform = "feishu"
	PlatformSlack  Platform = "slack"
	PlatformWebChat Platform = "webchat"
)

type CapabilityMode string

const (
	Native          CapabilityMode = "native"
	GatewayFallback CapabilityMode = "gateway_fallback"
	Unsupported     CapabilityMode = "unsupported"
	NotApplicable   CapabilityMode = "not_applicable"
)

type WorkerProfile struct {
	Type         worker.WorkerType
	Stop         CapabilityMode
	Reset        CapabilityMode
	Resume       CapabilityMode
	Interaction  CapabilityMode
	MidTurnInput CapabilityMode
}

type Combination struct {
	ID       string
	Platform Platform
	Worker   worker.WorkerType
}

func WorkerProfiles() []WorkerProfile
func Combinations() []Combination
func CombinationID(platform Platform, wt worker.WorkerType) string
```

不得为本计划向 `messaging.PlatformType` 增加 WebChat：该类型表示消息平台 adapter，WebChat 直接进入 Gateway WebSocket。平台 adapter 需要映射时，飞书/Slack 测试分别显式使用现有 `messaging.PlatformFeishu`/`PlatformSlack`。

矩阵固定 ID：`F-C/F-O/F-X/F-A/S-C/S-O/S-X/S-A/W-C/W-O/W-X/W-A`。任何修改必须同时更新本计划、Spec、CI 汇总和人工模板。

Gateway harness 对外接口固定为：

```go
type Harness struct {
	Hub     *gateway.Hub
	Bridge  *gateway.Bridge
	Handler *gateway.Handler
}

func NewHarness(t testing.TB, platform e2econtract.Platform, profile e2econtract.WorkerProfile) *Harness
func (h *Harness) Worker() *WorkerProbe
func (h *Harness) WaitForKinds(t testing.TB, kinds ...events.Kind) []*events.Envelope
func (h *Harness) AssertSingleTerminal(t testing.TB, reason string)
```

`NewHarness` 必须用 `t.TempDir()` 的 SQLite store、`t.Cleanup` 关闭 Manager/Hub/store，不启动真实 CLI，不访问网络。

## 4. 初级工程师执行规则

### 4.1 每个任务开始前

- [ ] 阅读本总计划、当前子计划、Spec 和 Issue #954；把当前任务允许修改的文件列入工作笔记。
- [ ] 运行 `rtk git status --short`，确认没有他人改动；发现重叠改动立即暂停，不覆盖。
- [ ] 运行子计划给出的 baseline 命令；baseline 失败时保存输出并暂停，不在失败基线上开发。
- [ ] 写出“哪一个错误生产改动会让新测试失败”；无法回答时重写测试设计。

### 4.2 每个 RED/GREEN 周期

- [ ] 一次只增加一个行为测试，期望值使用手工 literal，不调用被测 helper 计算 expected。
- [ ] 运行精确 `-run '^TestName$'`，确认因缺失行为失败，而非编译、fixture 或竞态错误。
- [ ] 只实现让该测试通过的最小代码；不顺手重构邻近模块。
- [ ] 运行精确测试、包级 `-count=1 -race`、`rtk git diff --check`。
- [ ] mutation check：错误 method/path、漏 rollback、重复 terminal、错误 capability mode 中至少一个会被测试捕获。
- [ ] 按子计划给出的 Conventional Commit 提交，不使用 `--no-verify`，不 amend 失败提交。

### 4.3 偏离保护

出现以下任一情况立即停止并升级给资深工程师：

- 需要修改 `pkg/events`、AEP JSON tag 或客户端 SDK；
- 需要在 `worker.Worker`/`Capabilities` 增加方法；
- 需要把 `unknown` 自动重投；
- 需要将 prompt/metadata/原始错误写入日志或证据；
- 需要真实飞书/Slack token 才能让普通 CI 通过；
- 需要 `time.Sleep` 才能稳定测试；
- 需要 skip 一个 12 组合 core 场景；
- 单包 race 测试持续超过 5 秒或连续两次 flaky；
- 新指标 label 包含 session/request/error/file 名称；
- OCS stop 实现删除 session 或释放整个 singleton，而不是 abort 当前 turn。

## 5. 集成顺序

### Task 1: 完成契约矩阵子计划

**Files:** `2026-08-05-platform-worker-contract-matrix.md` 中列出的文件。

**Interfaces:**
- Produces: `e2econtract.WorkerProfile`、`Combination`、`contracttest.Harness`、12 项可审计 CI 结果。
- Consumes: 当前 Gateway/messaging/Worker mapper 公共接口。

- [ ] **Step 1:** 按子计划逐个 RED/GREEN 提交，直至 `rtk make test-contract-matrix` 输出 12 个固定 ID，且 CI 审计步骤确认 12 passed、0 skipped、0 failed。
- [ ] **Step 2:** 运行 `rtk go test ./internal/gateway ./internal/messaging/feishu ./internal/messaging/slack -count=1 -race`。
- [ ] **Step 3:** 由 reviewer 核对每个组合确实调用平台入口和对应 Worker parser/mapper；只循环字符串不予通过。
- [ ] **Step 4:** 提交并 push，Issue #954 记录 Test 证据，不写 Live 已通过。

### Task 2: 完成 Slack 对齐子计划

**Files:** `2026-08-05-slack-reliability-alignment.md` 中列出的文件。

**Interfaces:**
- Consumes: `contracttest.Harness` 的 Slack 四组合场景。
- Produces: bounded media、dedup rollback、terminal error/fallback 指标。

- [ ] **Step 1:** 严格按 media → dedup → terminal 顺序完成三个独立提交。
- [ ] **Step 2:** 运行 Slack 包 race 和 S-C/S-O/S-X/S-A 四项矩阵。
- [ ] **Step 3:** reviewer 核对所有失败返回点是否 rollback，所有成功 admission 是否保留 dedup。

### Task 3: 完成 Worker 生命周期子计划

**Files:** `2026-08-05-worker-lifecycle-alignment.md` 中列出的文件。

**Interfaces:**
- Consumes: `WorkerProfile`、Gateway stop/reset/pending buffer。
- Produces: OCS abort 和四 Worker 生命周期合同。

- [ ] **Step 1:** 先完成 OCS HTTP abort 单元测试与实现，再运行 Gateway 四 Worker 合同。
- [ ] **Step 2:** 对每个 Worker 验证重复 stop、单 terminal、next-turn；不从一个 Worker 的结论外推其他 Worker。
- [ ] **Step 3:** reviewer 核对 stop 不删除 session、不触发 crash fallback、不污染下一轮。

### Task 4: 完成 WebChat 子计划

**Files:** `2026-08-05-webchat-worker-matrix.md` 中列出的文件。

**Interfaces:**
- Consumes: 组合 ID 与 Worker profile。
- Produces: W-C/W-O/W-X/W-A 的 transport/UI Test 证据。

- [ ] **Step 1:** 参数化 mock API 的 workspace/session/workers 数据，四行使用完整真实响应结构。
- [ ] **Step 2:** 四 Worker 分别运行 ACK → stream → stop → Done → next-turn → interaction。
- [ ] **Step 3:** 运行 Vitest、TypeScript、目标 Playwright 和 CI job；禁止只运行 Chromium 页面加载。

### Task 5: 完成人工 12 组合子计划

**Files:** `2026-08-05-platform-worker-live-validation.md` 中列出的文件。

**Interfaces:**
- Consumes: 已合并候选 commit 和 12 项人工模板。
- Produces: 绑定该 commit 的 12/12 Live 证据。

- [ ] **Step 1:** 先提交 runbook/template；模板初始状态全部为 `NOT_RUN`。
- [ ] **Step 2:** 人工逐项执行，状态只允许 `PASS`、`FAIL(issue/PR)`、`BLOCKED(reason)`。
- [ ] **Step 3:** 12 项全部 `PASS` 后才更新 Issue #954 为 Live 完成；任何 BLOCKED 不关闭 Epic。

## 6. 最终合并门禁

- [ ] `rtk make test-contract-matrix`：12 passed、0 skipped、0 failed。
- [ ] `rtk go test ./internal/messaging/... ./internal/gateway/... ./internal/worker/... -count=1 -race`：0 fail、0 race。
- [ ] `rtk pnpm --dir webchat test`：0 fail。
- [ ] `rtk pnpm --dir webchat exec tsc --noEmit`：exit 0。
- [ ] `rtk pnpm --dir webchat exec playwright test e2e/platform-worker-matrix.spec.ts`：四 Worker 场景全部执行。
- [ ] `rtk make docs-build`、`rtk make check`、pre-push 6/6。
- [ ] `rtk git diff --check`、工作区只含本 Epic 文件，无生成物和凭证。
- [ ] review 对当前 HEAD 无 P0/P1，值得修复的 P2 已一次性处理。
- [ ] CI 远端检查通过后才能合并；本地 green 不等于合并证据。
- [ ] 真实 12 组合与自动门禁分开报告；未执行 Live 时明确写“Live 未验证”。

## 7. Spec 可追踪性

| Spec 项 | 唯一实施落点 | 必须出现的验证证据 |
| --- | --- | --- |
| G1 Slack media | Slack 子计划 Task 1 | exact 10 MiB、10 MiB+1、lying size、cleanup、0700/0600 |
| G2 Slack dedup | Slack 子计划 Task 2 | delivery retry、empty rollback、stale generation、success duplicate |
| G3 Slack terminal | Slack 子计划 Task 3 | typed close error、shared deadline、fallback sent/failed/skipped、no replay |
| G4 OCS abort | Worker 子计划 Task 1–2 | POST path/query/body/boolean/timeout；conn/session/singleton 保留 |
| G5 misleading matrix | Contract 子计划 Task 4–9 | 旧测试改名；12 × C01–C08 机器审计 |
| G6/G8 WebChat matrix/baseline | WebChat 子计划 Task 1–5 | 先恢复 12/12 browser baseline，再验证 W-C/W-O/W-X/W-A init/ACK/stream/interaction/stop/next |
| G7/G9 lifecycle/stop scope | Worker 子计划 Task 3–7 | stopped marker turn scope；per-turn stop fence；manifest 与 concrete adapter 一致；单 terminal；reset/mid-turn |
| Live 12/12 | Live 子计划 Task 1–8 | 同一 commit 的 12 行人工 PASS、双人核对、cleanup |

Reviewer 必须逐行勾选此表。任何 Spec 项没有对应测试名和命令输出时，不得用“由其他测试间接覆盖”关闭。

## 8. 提交与 PR 策略

建议按以下提交序列，子计划内部可再拆 RED/GREEN 逻辑提交，但不得跨域混合：

1. `test(e2e): define platform worker contract matrix`
2. `fix(slack): bound media ingestion and rollback dedup`
3. `fix(slack): surface terminal delivery failures`
4. `fix(opencodeserver): abort active session turn`
5. `test(worker): enforce lifecycle capability contracts`
6. `test(webchat): cover all worker transport combinations`
7. `docs(e2e): add manual twelve-combination validation runbook`

PR 描述必须链接 #954 和本 Spec，分开列出 Source/Test/Live。若 Live 尚未完成，PR 可以满足代码合并条件，但 Epic 保持开放直至人工 12/12 完成。
