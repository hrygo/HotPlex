# 平台 × Worker 确定性契约矩阵 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让普通 CI 以可审计测试名运行飞书、Slack、WebChat × `claude_code`、`opencode_server`、`codex_cli`、`acp` 的 12/12 确定性组合，并纠正现有 mock matrix 的证据表述。

**Architecture:** 新建无 Gateway 反向依赖的 `internal/e2econtract` manifest，以及 `internal/gateway/contracttest` harness/scenario 工具包；harness 使用真实 Gateway Hub/Bridge/Handler 和各 Worker 真实 parser/mapper，外部进程由确定性 probe 替代。飞书/Slack 的四组合测试分别留在平台 package 以复用 package-private raw event/client fake；WebChat 使用 `gateway_test` package 从真实 WebSocket/AEP Conn 进入，避免 import cycle。

**Tech Stack:** Go、testify/require、gorilla/websocket、SQLite test store、Feishu/Slack SDK event structs、AEP v1。

## Global Constraints

- 继承总计划全部约束；普通 CI 12/12 core 场景不得 skip。
- `interaction_matrix_test.go` 只证明 metadata dispatch，不得继续命名为 E2E。
- Worker probe 必须调用对应真实 parser/mapper；禁止四行都返回同一手写 AEP 序列。
- 平台测试必须从 `handleMessage`、`handleMessageEvent` 或 WebSocket `ReadPump` 进入，不能直接调用 Gateway `handleInput` 冒充平台组合。
- 所有 probe 使用 channel 推进，不用 `time.Sleep`。
- `messaging.PlatformType` 没有也不应增加 WebChat；组合平台使用 `e2econtract.Platform`。

---

### Task 1: 固定组合 ID 和能力 profile

**Files:**
- Create: `internal/e2econtract/manifest.go`
- Create: `internal/e2econtract/manifest_test.go`

**Interfaces:**
- Consumes: `worker.WorkerType`；平台是本包的 string enum。
- Produces: `CapabilityMode`、`WorkerProfile`、`Combination`、`WorkerProfiles()`、`Combinations()`、`CombinationID()`。

- [ ] **Step 1: 写 12 行 literal 红测**

```go
func TestCombinations_ExactMatrix(t *testing.T) {
	t.Parallel()
	got := Combinations()
	require.Equal(t, []Combination{
		{ID: "F-C", Platform: PlatformFeishu, Worker: worker.TypeClaudeCode},
		{ID: "F-O", Platform: PlatformFeishu, Worker: worker.TypeOpenCodeSrv},
		{ID: "F-X", Platform: PlatformFeishu, Worker: worker.TypeCodexCLI},
		{ID: "F-A", Platform: PlatformFeishu, Worker: worker.TypeACP},
		{ID: "S-C", Platform: PlatformSlack, Worker: worker.TypeClaudeCode},
		{ID: "S-O", Platform: PlatformSlack, Worker: worker.TypeOpenCodeSrv},
		{ID: "S-X", Platform: PlatformSlack, Worker: worker.TypeCodexCLI},
		{ID: "S-A", Platform: PlatformSlack, Worker: worker.TypeACP},
		{ID: "W-C", Platform: PlatformWebChat, Worker: worker.TypeClaudeCode},
		{ID: "W-O", Platform: PlatformWebChat, Worker: worker.TypeOpenCodeSrv},
		{ID: "W-X", Platform: PlatformWebChat, Worker: worker.TypeCodexCLI},
		{ID: "W-A", Platform: PlatformWebChat, Worker: worker.TypeACP},
	}, got)
}
```

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/e2econtract -run '^TestCombinations_ExactMatrix$' -count=1`

Expected: FAIL because package/functions do not exist; compilation error must只指向这些缺失符号。

- [ ] **Step 3: 实现固定 profile**

```go
type CapabilityMode string

type Platform string

const (
	PlatformFeishu Platform = "feishu"
	PlatformSlack Platform = "slack"
	PlatformWebChat Platform = "webchat"
	Native CapabilityMode = "native"
	GatewayFallback CapabilityMode = "gateway_fallback"
	Unsupported CapabilityMode = "unsupported"
	NotApplicable CapabilityMode = "not_applicable"
)

type WorkerProfile struct {
	Type worker.WorkerType
	Stop, Reset, Resume, Interaction, MidTurnInput CapabilityMode
}
```

`WorkerProfiles()` 的 mid-turn literal：Claude/Codex=`Native`，OCS/ACP=`GatewayFallback`；四个 stop/reset/interaction 均为 `Native`，resume 以当前四个 `SupportsResume()` 实现为准并写 literal 测试。

- [ ] **Step 4: 验证 GREEN 与唯一性**

Run: `rtk go test ./internal/e2econtract -count=1 -race`

Expected: PASS；另一个测试用 map 断言 12 个 ID、12 个 platform/worker pair 均唯一。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/e2econtract/manifest.go internal/e2econtract/manifest_test.go
rtk git commit -m "test(e2e): define platform worker contract profiles"
```

### Task 2: 建立真实 parser/mapper WorkerProbe

**Files:**
- Create: `internal/gateway/contracttest/worker_probe.go`
- Create: `internal/gateway/contracttest/worker_probe_test.go`

**Interfaces:**
- Consumes: `e2econtract.WorkerProfile`；Claude `Parser.ParseLine`/`Mapper.Map`，OCS `Converter.Convert`，Codex `Parser.ParseNotification`/`Mapper.MapNotification`，ACP `ACPMapper.MapNotification`。
- Produces: `NewWorkerProbe(profile e2econtract.WorkerProfile, sessionID string) *WorkerProbe`；实现 `worker.Worker`，并提供 `EmitBasicTurn(context.Context) error`、`StopCalls() int`、`InputCalls() int`。

- [ ] **Step 1: 写四种映射红测**

测试表使用四份手工协议 fixture；每行调用 `EmitBasicTurn` 后从 `probe.Conn().Events()` 读取，断言至少包含 `message.start`、`message.delta` 或 `message`、一个 `done`，且 envelope `SessionID` 等于 literal `session-contract`。

Claude fixture 通过 `Parser.ParseLine`；Codex fixture先通过 `ParseNotification`；OCS 调用 `Convert`；ACP 构造完整 `JSONRPCNotification`。测试断言 mapper 产物，不断言 mock 调用次数。

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/gateway/contracttest -run '^TestWorkerProbe_UsesRealParserAndMapper$' -count=1`

Expected: FAIL because `NewWorkerProbe` is missing。

- [ ] **Step 3: 实现 probe 状态机**

```go
type WorkerProbe struct {
	*noop.Worker
	profile e2econtract.WorkerProfile
	conn *probeConn
	inputCalls atomic.Int32
	stopCalls atomic.Int32
	stopped atomic.Bool
}

func (p *WorkerProbe) Type() worker.WorkerType { return p.profile.Type }
func (p *WorkerProbe) Input(context.Context, string, map[string]any) error {
	p.inputCalls.Add(1)
	return p.EmitBasicTurn(context.Background())
}
func (p *WorkerProbe) StopCurrentTurn(context.Context) error {
	if p.stopped.CompareAndSwap(false, true) { p.stopCalls.Add(1) }
	return nil
}
```

嵌入 `internal/worker/noop.Worker` 复用完整 `worker.Worker` no-op surface，再显式覆盖 `Type`、capabilities、`Start`、`Input`、`Resume`、`Conn`、`StopCurrentTurn`、`ResetContext`、`IsStopped`。保留 `var _ worker.Worker = (*WorkerProbe)(nil)`，这样未来 Worker 接口变化会在测试工具处编译失败。`EmitBasicTurn` 必须按类型走四个独立分支，且所有 mapper 输出原样写入 `probeConn`。

- [ ] **Step 4: 验证 GREEN、mutation 和 race**

Run: `rtk go test ./internal/gateway/contracttest -run 'TestWorkerProbe' -count=1 -race`

Mutation check: 把 Codex method 改成未知 method 时 Codex 行必须失败；把 OCS event type 改成未知值时 OCS 行必须失败。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/gateway/contracttest/worker_probe.go internal/gateway/contracttest/worker_probe_test.go
rtk git commit -m "test(e2e): add real worker mapper probes"
```

### Task 3: 建立 Gateway Harness

**Files:**
- Create: `internal/gateway/contracttest/harness.go`
- Create: `internal/gateway/contracttest/harness_test.go`

**Interfaces:**
- Consumes: `gateway.NewHub`、`gateway.NewBridge`、`gateway.NewHandler`、`gateway.SetWorkerFactory`、`session.NewSQLiteStore`、`session.NewManager`、`WorkerProbe`。
- Produces: `NewHarness(t, e2econtract.Platform, e2econtract.WorkerProfile)`、`WaitForKinds`、`AssertSingleTerminal`。

- [ ] **Step 1: 写 clean lifecycle 红测**

测试创建 `t.TempDir()` SQLite、一个 F-C harness，发送一个 input envelope，断言 WorkerProbe 收到一次、Hub 观察到 mapper 事件；测试结束后 store 文件可关闭/删除，证明无泄漏 goroutine/FD。

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/gateway/contracttest -run '^TestHarness_BasicTurnAndCleanup$' -count=1 -race`

Expected: FAIL because `NewHarness` is missing。

- [ ] **Step 3: 实现 harness**

使用 `config.Default()`，同时设置 `cfg.DB.Path` 与 `cfg.DB.SQLite.Path` 为 `filepath.Join(t.TempDir(), "sessions.db")`；`writeMu := sqlutil.NewWriteMu(sqlutil.DialectSQLite)`；依次创建 `session.NewSQLiteStore`、`session.NewManager`、`config.NewConfigStore(cfg,nil)`、`gateway.NewHub`、`gateway.NewBridge`、`gateway.NewHandler`。WorkerFactory 对请求 type 与 profile type 不一致时返回错误，避免一行误用其他 Worker。

所有清理统一放入一个 `t.Cleanup`，固定顺序为 `Bridge.Shutdown(cleanupCtx)` → `Hub.Shutdown(cleanupCtx)` → `Manager.Close()` → `store.Close()`；cleanupCtx 最多 2 秒。不得新增生产 cleanup 方法。

- [ ] **Step 4: 验证 GREEN**

Run: `rtk go test ./internal/gateway/contracttest -count=1 -race`

Expected: PASS，测试无 `time.Sleep`，无 goroutine leak warning。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/gateway/contracttest/harness.go internal/gateway/contracttest/harness_test.go
rtk git commit -m "test(e2e): add gateway contract harness"
```

### Task 4: 建立 C01–C08 共享 scenario runner

**Files:**
- Create: `internal/gateway/contracttest/scenario.go`
- Create: `internal/gateway/contracttest/scenario_test.go`

**Interfaces:**
- Produces: `PlatformDriver` 和 `RunCoreScenarios(t, combo, driver)`。
- Consumes: `Harness`；平台包提供 raw ingress/terminal fault 的 driver。

- [ ] **Step 1: 固定 driver surface**

```go
type PlatformDriver interface {
	BeginScenario(t testing.TB, scenarioID string)
	EndScenario(t testing.TB)
	SendRawInput(ctx context.Context, id, content string) error
	SendRawDuplicate(ctx context.Context, id, content string) error
	FailNextDelivery(err error)
	SendStopTwice(ctx context.Context) error
	Reset(ctx context.Context) error
	FailNextTerminal(stage TerminalFailureStage, err error)
	SaturateDeltaQueue(ctx context.Context) error
	WaitForTerminal(t testing.TB) []*events.Envelope
	DeliveredInputs() int
	VisibleTerminals() int
}

type TerminalFailureStage string
const (
	TerminalIncrement TerminalFailureStage = "increment"
	TerminalClose TerminalFailureStage = "close"
	TerminalFallback TerminalFailureStage = "fallback"
)
```

driver 可以有 package-private扩展，但不得删减这些方法或用空实现让场景通过。一个组合只创建一个 SQLite/Gateway harness；每个场景用 `BeginScenario` 创建唯一 session/client IDs、清空计数器和 fault state，`EndScenario` 等待 goroutine/conn 收敛。这样既隔离场景，也避免 96 次 migration/Hub callback 注册导致单包超过 5 秒。

- [ ] **Step 2: 写 runner 自测 RED**

用 recording driver 跑一个 F-C，断言执行顺序恰好 `C01...C08`；故意让 C04 返回错误时，subtest 名包含 `F-C/feishu/claude_code/C04-stop` 且只该场景失败。expected 场景列表为八行 literal。

Run: `rtk go test ./internal/gateway/contracttest -run '^TestRunCoreScenarios_ExactEight$' -count=1`

- [ ] **Step 3: 实现八个场景的共享断言**

- C01：ACK 一次、Seq 严格递增、内容可见、done 一次；
- C02：同 ID/同 payload 重放不增加 input；同 ID/不同 payload 得稳定冲突且不执行；
- C03：delivery fault 后只在安全阶段允许同 ID retry，最终 input 一次；
- C04：活跃 turn 双 stop，effective stop 一次、一个 stopped done；
- C05：同 session next input 正常 done；
- C06：reset/reconnect 后旧 forwarder 事件不可见；
- C07：increment/close/fallback fault 可观察，Worker input 不重放；
- C08：delta 可丢，state/done/error 均保留。

runner 只编排和断言，不直接调用平台/Gateway 私有函数。每个组合共享 harness，但场景必须有独立 session；`EndScenario` 失败时立即停止该组合后续场景，禁止在污染状态上继续产生误导结果。

- [ ] **Step 4: 验证 GREEN 与 Commit**

Run: `rtk go test ./internal/gateway/contracttest -count=1 -race`

```bash
rtk git add internal/gateway/contracttest/scenario.go internal/gateway/contracttest/scenario_test.go
rtk git commit -m "test(e2e): define shared core scenario runner"
```

### Task 5: 纠正旧 interaction matrix 名称

**Files:**
- Modify: `internal/gateway/interaction_matrix_test.go`

**Interfaces:**
- Consumes: 现有 36 个 metadata dispatch subtests。
- Produces: `TestInteractionMetadataDispatch_AllPlatformsWorkersAndKinds`；测试注释明确 Test 证据边界。

- [ ] **Step 1: 先运行旧测试记录 baseline**

Run: `rtk go test ./internal/gateway -run '^TestInteractionE2EMatrix_AllPlatformsWorkersAndKinds$' -count=1 -v`

Expected: PASS，36 个 subtest。

- [ ] **Step 2: 只改名和注释**

注释写明：只验证 Gateway response metadata dispatch，不穿过平台 adapter、Worker parser/mapper 或外部系统。

- [ ] **Step 3: 验证新旧名称**

Run: `rtk go test ./internal/gateway -run '^TestInteractionMetadataDispatch_AllPlatformsWorkersAndKinds$' -count=1 -v`

Expected: PASS，36 个 subtest；旧名称运行应显示 “no tests to run”。

- [ ] **Step 4: Commit**

```bash
rtk git add internal/gateway/interaction_matrix_test.go
rtk git commit -m "test(gateway): name interaction matrix by evidence level"
```

### Task 6: 飞书四 Worker raw ingress 组合

**Files:**
- Create: `internal/messaging/feishu/platform_worker_matrix_test.go`

**Interfaces:**
- Consumes: package-private `newTestAdapter`、`handleMessage`，`contracttest.NewHarness`、F-C/F-O/F-X/F-A。
- Produces: `TestPlatformWorkerContractMatrix_Feishu` 四个 core subtests。

- [ ] **Step 1: 写 F-C 红测并确认真实入口**

构造完整 `larkim.P2MessageReceiveV1`，包含 message ID、create time、chat/user/type/content；配置 adapter 的 messaging Bridge 指向 harness。断言 input 一次、mapper stream 到真实 FeishuConn、terminal 一次。

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/messaging/feishu -run '^TestPlatformWorkerContractMatrix_Feishu/F-C$' -count=1 -race`

Expected: FAIL until harness/adapter wiring完整；不得通过直接调用 `Handler.Handle` 修复。

- [ ] **Step 3: 扩为四行 literal profile**

每行名称用 `combo.ID`，但 expected 列表为手工 `[]string{"F-C","F-O","F-X","F-A"}` 断言，防止循环漏行仍绿。每行把 Feishu driver 交给 `RunCoreScenarios`，测试名必须形成 `combo/platform/worker/C01...C08`。

- [ ] **Step 4: 运行四行与重复输入/stop/next-turn core 场景**

Run: `rtk go test ./internal/messaging/feishu -run '^TestPlatformWorkerContractMatrix_Feishu' -count=1 -race -v`

Expected: 4 profiles × core scenarios 全部 PASS，0 skip。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/messaging/feishu/platform_worker_matrix_test.go
rtk git commit -m "test(feishu): cover four worker contract combinations"
```

### Task 7: Slack 四 Worker raw ingress 组合

**Files:**
- Create: `internal/messaging/slack/platform_worker_matrix_test.go`

**Interfaces:**
- Consumes: package-private `newTestAdapter`、`handleMessageEvent`、完整 Slack API fake，`contracttest.NewHarness`、S-C/S-O/S-X/S-A。
- Produces: `TestPlatformWorkerContractMatrix_Slack`。

- [ ] **Step 1: 写 S-C 红测**

使用完整 `slackevents.MessageEvent`，设置 `ClientMsgID`、`TimeStamp`、`Channel`、`User`、`Message.Text`；fake API 返回完整 Slack 响应结构。断言从 raw handler 进入、真实 SlackConn 收到 mapper 事件、一个 terminal。

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/messaging/slack -run '^TestPlatformWorkerContractMatrix_Slack/S-C$' -count=1 -race`

Expected: FAIL until real Bridge/Hub wiring exists。

- [ ] **Step 3: 扩为四 Worker并把 Slack driver 交给 `RunCoreScenarios`**

Run: `rtk go test ./internal/messaging/slack -run '^TestPlatformWorkerContractMatrix_Slack' -count=1 -race -v`

Expected: S-C/S-O/S-X/S-A 全部执行，0 skip。

- [ ] **Step 4: Commit**

```bash
rtk git add internal/messaging/slack/platform_worker_matrix_test.go
rtk git commit -m "test(slack): cover four worker contract combinations"
```

### Task 8: WebChat 四 Worker WebSocket 组合

**Files:**
- Create: `internal/gateway/webchat_worker_matrix_test.go`（`package gateway_test`）
- Reuse: `internal/gateway/testutil/ws_mock.go`

**Interfaces:**
- Consumes: real Conn/ReadPump、AEP init/input/control envelopes、`contracttest.NewHarness`、W-C/W-O/W-X/W-A。
- Produces: `TestPlatformWorkerContractMatrix_WebChat`。

- [ ] **Step 1: 写 W-C RED**

通过 WebSocket 发送 init(worker_type=`claude_code`) 和 input，读取 `input.ack`、mapper stream 和 done；发送 stop 后断言一个 `stopped_by_user`，再发送 next-turn input。

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/gateway -run '^TestPlatformWorkerContractMatrix_WebChat/W-C$' -count=1 -race`

Expected: FAIL until harness接入真实 Conn。

- [ ] **Step 3: 扩为四行并把 WebSocket driver 交给 `RunCoreScenarios`**

Run: `rtk go test ./internal/gateway -run '^TestPlatformWorkerContractMatrix_WebChat' -count=1 -race -v`

Expected: W-C/W-O/W-X/W-A 全部执行，0 skip。

- [ ] **Step 4: Commit**

```bash
rtk git add internal/gateway/webchat_worker_matrix_test.go
rtk git commit -m "test(webchat): cover four worker gateway combinations"
```

### Task 9: 将 12 组合接入普通 CI

**Files:**
- Modify: `Makefile`
- Modify: `.github/workflows/ci.yml`
- Create: `scripts/test-contract-matrix.sh`

**Interfaces:**
- Consumes: 三个 `TestPlatformWorkerContractMatrix_*` parent tests。
- Produces: `make test-contract-matrix` 和机器校验的 12 × 8 CI summary。

- [ ] **Step 1: 写失败的 Make target smoke 检查**

Run: `rtk make test-contract-matrix`

Expected: FAIL with “No rule to make target”。

- [ ] **Step 2: 增加验证脚本和 target**

`scripts/test-contract-matrix.sh` 使用 Bash `set -euo pipefail`，先确认 `jq` 可用，再以 `go test -count=1 -race -json` 覆盖三个 package并保存 `mktemp` JSONL。脚本必须：

1. `go test` 非零时原样非零退出；
2. 任一匹配 matrix 的 `Action=skip` 时失败；
3. 从 Test path 解析组合 ID，和 12 行 literal expected set 做双向 diff；
4. 每个 ID 的 C01–C08 pass set 必须恰好八项；
5. 打印 `contract matrix: 12 combinations, 96 core scenarios, 0 skipped, 0 failed`；
6. trap 删除临时 JSONL，不把测试 payload 写入 artifact。

不得仅 grep 12 个字符串后输出 pass。`Makefile` target 只调用该脚本，并加入 `.PHONY`；不加入 `quality`，避免全量 Go test 内重复执行 race matrix。

- [ ] **Step 3: 更新 CI**

增加独立 job/step `Deterministic platform-worker matrix`，运行 `make test-contract-matrix`；不配置飞书/Slack/Worker secrets。把脚本的单行 summary 写入 GitHub step summary，失败仍保留完整 `go test -json` 控制台输出。

- [ ] **Step 4: 验证本地汇总**

Run: `rtk make test-contract-matrix`

Expected: 输出精确包含 12 个 ID 和 `12 combinations, 96 core scenarios, 0 skipped, 0 failed`。

- [ ] **Step 5: 全局回归与 Commit**

Run: `rtk go test ./internal/gateway/... ./internal/messaging/feishu ./internal/messaging/slack -count=1 -race`

Run: `rtk make check`

```bash
rtk git add Makefile .github/workflows/ci.yml scripts/test-contract-matrix.sh
rtk git commit -m "ci(e2e): gate all platform worker combinations"
```
