# 四 Worker 生命周期对齐 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 OpenCode Server 只释放连接而不终止当前 turn 的缺口，并用统一但能力感知的测试合同锁定四个 Worker 的 stop、next-turn、reset、resume、interaction 和 mid-turn 行为。

**Architecture:** OCS 使用官方 `POST /session/:id/abort` 原地中止 turn，不释放 singleton、不删除远端 session、不关闭长期 SSE；Gateway 层继续负责 owner 校验、execution 收敛和唯一 `stopped_by_user` terminal。能力 manifest 只描述现有实现，不进入 AEP wire contract。

**Tech Stack:** Go、`httptest.Server`、testify/require、AEP v1、四个 Worker 协议 parser/mapper。

**Protocol source:** OpenCode 官方 Server API 当前定义 `POST /session/:id/abort`，无请求体，返回 boolean。实施当天再次核对 `https://opencode.ai/docs/server/`；若仓库锁定版本与官网不一致，暂停并附上版本/OpenAPI 证据，不猜测兼容行为。

## Frozen Capability Manifest

| Worker | stop | reset | resume | interaction | mid-turn input |
| --- | --- | --- | --- | --- | --- |
| `claude_code` | Native | Native, conn replaced | Native | Native | Native |
| `opencode_server` | Native | Native, in-place | Native | Native | GatewayFallback |
| `codex_cli` | Native | Native, conn replaced | Native | Native | Native |
| `acp` | Native | Native, in-place | Native | Native | GatewayFallback |

“Native”只表示 Worker adapter 直接调用该协议能力，不表示四个 Worker 的底层机制相同。`GatewayFallback` 的 mid-turn 输入必须由 `PendingBuffer` 在后续安全边界交付一次。

---

### Task 1: 为 OCS 增加独立、可测试的 abort HTTP helper

**Files:**
- Modify: `internal/worker/opencodeserver/worker.go`
- Modify: `internal/worker/opencodeserver/worker_test.go`

**Interfaces:**
- Produces: package-private `abortOCSSession(ctx, sessionID, httpAddr, projectDir string, client *http.Client) error`。
- Consumes: OCS 官方 `POST /session/:id/abort?directory=...`。

- [ ] **Step 1: 写 method/path/query/body 红测**

使用 `httptest.Server` 捕获请求并返回 `200` + `true`。手工断言：

```go
require.Equal(t, http.MethodPost, req.Method)
require.Equal(t, "/session/ses%2Fcontract/abort", req.URL.EscapedPath())
require.Equal(t, "/tmp/project with space", req.URL.Query().Get("directory"))
body, err := io.ReadAll(req.Body)
require.NoError(t, err)
require.Empty(t, body)
```

调用 session ID 使用 `ses/contract`，确保实现必须 `url.PathEscape`；project dir 含空格，确保使用 query encoder。

Run: `rtk go test ./internal/worker/opencodeserver -run '^TestAbortOCSSession_RequestContract$' -count=1`

Expected: FAIL because `abortOCSSession` does not exist。

- [ ] **Step 2: 写响应语义红测**

table rows：

- `200 true` → nil；
- `200 false` → nil（已经没有活动 turn，满足幂等目标）；
- `200 malformed` → stable decode error；
- `500` + 8 KiB body → error 只读取前 4096 bytes；
- server 等待 `<-req.Context().Done()` → caller 的 50ms 测试 deadline 返回 `context.DeadlineExceeded`/`context.Canceled`。

测试不使用 `time.Sleep`；server 通过 channel 通知请求已进入，再由 context 结束。

- [ ] **Step 3: 实现 helper**

固定实现边界：

1. 空 `sessionID`、`httpAddr` 或 nil client 返回带 `opencodeserver: abort session` 前缀的参数错误；
2. `http.NewRequestWithContext(ctx, POST, url, http.NoBody)`；
3. 仅当 `projectDir != ""` 时使用 `url.Values` 添加 `directory`；
4. `client.Do` 后 drain/close body；
5. 只接受官方文档的 `200 OK`，decode boolean；true/false 均为成功；
6. 非 200 使用 `io.LimitReader(resp.Body, 4096)`，不得把完整远端 body 记录到日志。

不要复用现有 `httpPost`：它会 JSON marshal payload、只校验状态码且不解析 boolean，不符合该无 body endpoint 的精确合同。

- [ ] **Step 4: 验证 GREEN 与 mutation**

Run: `rtk go test ./internal/worker/opencodeserver -run '^TestAbortOCSSession_' -count=1 -race`

Mutation check: 将 POST 改 GET、漏 PathEscape、漏 directory、把 false 当错误，必须分别让对应行失败。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/worker/opencodeserver/worker.go internal/worker/opencodeserver/worker_test.go
rtk git commit -m "test(opencodeserver): define abort request contract"
```

### Task 2: 让 OCS StopCurrentTurn 原地 abort

**Files:**
- Modify: `internal/worker/opencodeserver/worker.go`
- Modify: `internal/worker/opencodeserver/worker_test.go`

**Interfaces:**
- Consumes: `abortOCSSession`、`BaseWorker.MarkStopped`。
- Produces: OCS 当前 turn 中止；保留 `httpConn`、SSE subscription、singleton ref 和 Worker session ID。

- [ ] **Step 1: 写活跃 turn 红测**

构造 Worker 的 package-private 状态：`httpConn` 的 session ID=`ses-live`、projectDir=`/tmp/live`，`httpAddr` 指向 `httptest.Server`，`client` 为 server client。调用 `StopCurrentTurn` 后断言：

- server 恰好收到一次 `/session/ses-live/abort`；
- `w.IsStopped()` 为 true；
- `w.Conn()` 仍是调用前同一个 conn；
- `w.GetWorkerSessionID()` 仍为 `ses-live`；
- singleton `Release` 计数未增加；SSE cancel 未调用。

测试用 fake singleton 或现有 test singleton seam 观察 ref count；不得通过检查日志字符串判断行为。

Run: `rtk go test ./internal/worker/opencodeserver -run '^TestWorker_StopCurrentTurn_AbortsWithoutReleasingSession$' -count=1 -race`

Expected: FAIL；当前实现调用 `release()`，清空 conn。

- [ ] **Step 2: 写无活动连接和超时红测**

- `httpConn=nil`：返回 nil、标记 stopped、不访问网络；
- server 直到 context cancel：`StopCurrentTurn` 使用调用方更短 deadline；调用方无 deadline 时内部上限 2s；
- abort 返回 500：错误使用 `%w` 上浮，conn/session/singleton 仍保留。

- [ ] **Step 3: 实现 StopCurrentTurn**

在 `w.Mu` 下只快照 conn、addr、client、projectDir、sessionID，然后立即解锁；绝不在网络请求期间持锁。固定顺序：

```go
w.MarkStopped()
if conn == nil || sessionID == "" || client == nil { return nil }
abortCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
defer cancel()
return abortOCSSession(abortCtx, sessionID, addr, projectDir, client)
```

若 `ctx` 已有更短 deadline，`WithTimeout` 会保留更早截止。不要调用 `release()`、`sseCancel()`、`singleton.Release()`、`deleteOCSSession()` 或 `conn.Close()`。

- [ ] **Step 4: 验证 GREEN**

Run: `rtk go test ./internal/worker/opencodeserver -run '^TestWorker_StopCurrentTurn_' -count=1 -race`

Run: `rtk go test ./internal/worker/opencodeserver -count=1 -race`

- [ ] **Step 5: Commit**

```bash
rtk git add internal/worker/opencodeserver/worker.go internal/worker/opencodeserver/worker_test.go
rtk git commit -m "fix(opencodeserver): abort active session turn"
```

### Task 3: 将 stopped marker 限定为当前 turn

**Files:**
- Modify: `internal/worker/base/worker.go`
- Modify: `internal/worker/base/worker_test.go`
- Modify: `internal/worker/claudecode/worker.go`
- Modify: `internal/worker/claudecode/worker_test.go`
- Modify: `internal/worker/opencodeserver/worker.go`
- Modify: `internal/worker/opencodeserver/worker_test.go`
- Modify: `internal/worker/codexcli/worker.go`
- Modify: `internal/worker/codexcli/worker_test.go`
- Modify: `internal/worker/acp/worker.go`
- Create: `internal/worker/acp/worker_stop_test.go`

**Interfaces:**
- Produces: `BaseWorker.BeginTurn()`；不改变 `worker.Worker` interface。
- Consumes: 四个 `Input` 中现有 `base.DispatchMetadata` 分界。

- [ ] **Step 1: 写 base 红测**

`TestBaseWorker_BeginTurnClearsStopped`：初始 false → `MarkStopped()` true → `BeginTurn()` false。并行运行，直接断言 atomic state。

Run: `rtk go test ./internal/worker/base -run '^TestBaseWorker_BeginTurnClearsStopped$' -count=1 -race`

Expected: FAIL because `BeginTurn` does not exist。

- [ ] **Step 2: 写四 adapter 顺序测试**

每个 Worker 测试两个路径：

1. stopped=true 时发送只含 interaction response metadata 的 `Input`，`DispatchMetadata` 返回 handled=true，stopped 必须仍为 true；
2. stopped=true 时发送下一轮 primary content，协议 fake 观察到发送后 stopped 必须为 false。

使用各包现有 fake conn/client/manager，不启动真实进程。测试名统一 `TestWorker_Input_ClearsStoppedOnlyForPrimaryTurn`。

- [ ] **Step 3: 实现最小状态边界**

在 BaseWorker 增加：

```go
// BeginTurn clears the user-stop marker immediately before a new primary turn.
func (w *BaseWorker) BeginTurn() { w.stopped.Store(false) }
```

四个 `Input` 都在 `DispatchMetadata` 返回 `handled=false` 之后、实际 primary protocol send 之前调用 `BeginTurn()`。连接不存在、metadata dispatch error 或 handled metadata 不得清零。不要把方法加入 `worker.Worker`，也不要在 `InjectMidTurn` 中调用。

- [ ] **Step 4: 验证 GREEN 与 crash recovery mutation**

Run: `rtk go test ./internal/worker/base ./internal/worker/{claudecode,opencodeserver,codexcli,acp} -run 'TestBaseWorker_BeginTurn|TestWorker_Input_ClearsStoppedOnlyForPrimaryTurn' -count=1 -race`

Gateway C05 增加断言：next-turn 开始后模拟 Worker 非零退出，必须进入正常 crash recovery，而不是继续按 stopped 抑制。删除任一 adapter 的 `BeginTurn` 时其行必须失败。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/worker/base/worker.go internal/worker/base/worker_test.go internal/worker/claudecode/worker.go internal/worker/claudecode/worker_test.go internal/worker/opencodeserver/worker.go internal/worker/opencodeserver/worker_test.go internal/worker/codexcli/worker.go internal/worker/codexcli/worker_test.go internal/worker/acp/worker.go internal/worker/acp/worker_stop_test.go
rtk git commit -m "fix(worker): scope stopped marker to current turn"
```

### Task 4: 固化四 Worker capability manifest

**Files:**
- Modify: `internal/e2econtract/manifest.go`
- Modify: `internal/e2econtract/manifest_test.go`
- Modify: `internal/gateway/contracttest/worker_probe_test.go`

**Interfaces:**
- Consumes: `worker.MidTurnInjector` type assertion、四 Worker `SupportsResume()`、`ResetContext()` 结果与 interaction handler interfaces。
- Produces: 总计划中的固定 manifest，并证明声明与当前 adapter 类型一致。

- [ ] **Step 1: 写 exact literal 测试**

`TestWorkerProfiles_ExactCapabilities` 直接比较四行 literal；不调用 `WorkerProfiles()` 生成 expected。四个 concrete Worker 的测试逐个断言：

- `SupportsResume()==true`；
- Claude/Codex 满足 `worker.MidTurnInjector`，OCS/ACP 不满足；
- 对应 permission/question/elicitation handler type assertions 与 manifest `Native` 一致。

Run: `rtk go test ./internal/e2econtract ./internal/gateway/contracttest -run 'TestWorkerProfiles|TestWorkerProbe_CapabilitiesMatchAdapters' -count=1`

Expected: manifest 尚未完成时 FAIL；若当前源码事实与表不同，停止并升级，不为通过测试改 Worker 能力。

- [ ] **Step 2: 实现/修正 manifest**

只改测试 manifest，不改 `worker.Worker` 或 AEP。每个 capability 字段必须是 `Native`、`GatewayFallback`、`Unsupported`、`NotApplicable` 之一。

- [ ] **Step 3: 验证 GREEN**

Run: `rtk go test ./internal/e2econtract ./internal/gateway/contracttest -count=1 -race`

- [ ] **Step 4: Commit**

```bash
rtk git add internal/e2econtract/manifest.go internal/e2econtract/manifest_test.go internal/gateway/contracttest/worker_probe_test.go
rtk git commit -m "test(worker): lock lifecycle capability manifest"
```

### Task 5: Gateway stop/next-turn 单终态合同

**Files:**
- Create: `internal/gateway/stop_contract_test.go`
- Create: `internal/gateway/stop_fence.go`
- Create: `internal/gateway/stop_fence_test.go`
- Modify: `internal/gateway/commands.go`
- Modify: `internal/gateway/handler.go`
- Modify: `internal/gateway/contracttest/worker_probe.go`
- Modify: `internal/gateway/contracttest/worker_probe_test.go`

**Interfaces:**
- Consumes: real `Handler.handleControl` path、`Bridge` forwarder、execution runtime、四个 `WorkerProfile`。
- Produces: C04/C05 的四 Worker Test 证据。

- [ ] **Step 1: 写四 Worker C04 红测**

每个 subtest 名固定为 `<worker>/C04-double-stop`。probe 收到 input 后先发 start/delta 并阻塞 terminal；测试连续发送两个相同 owner 的 `control.stop`，然后释放 probe。断言：

- 有效 `StopCurrentTurn` 调用一次；
- client 只收到一个 `Done`，reason=`stopped_by_user`；
- execution runtime 终态一次；
- 没有 crash fallback error/done；
- Seq 严格递增。

用 `enteredTurn`、`allowTerminal` channel 控制；禁止 `time.Sleep`。

Run: `rtk go test ./internal/gateway -run '^TestWorkerLifecycleContract/C04-double-stop' -count=1 -race -v`

- [ ] **Step 2: 写 C05 next-turn 红测**

在同一 session/owner 上发送新 input ID。probe 必须将 per-turn `stopped` 状态清零并产生正常 done；断言 session ID 不变、Worker input 总计 2、第二轮 reason 不是 `stopped_by_user`。

注意：生产 `BaseWorker.IsStopped()` 是 session worker 的历史标记，用于抑制 crash fallback；probe 需要独立的 per-turn CAS，不能假设 production `MarkStopped` 会自动清零。

- [ ] **Step 3: 写并实现 per-turn stop fence**

当前 `handleControl` 会让两个 stop 都调用 Worker 并各发一个 Done，因此必须在 Gateway control admission 修复，不能只改 probe 或平台 adapter。

新增 package-private：

```go
type turnStopFence struct {
	mu sync.Mutex
	claimed map[string]string // session ID -> worker run ID
}

func (f *turnStopFence) Claim(sessionID, workerRunID string) bool
func (f *turnStopFence) Rollback(sessionID, workerRunID string)
func (f *turnStopFence) BeginTurn(sessionID, workerRunID string)
```

显式 `mu` 字段，不嵌入、不传 mutex 指针。`Handler` 初始化一个 fence。stop 路径在 Worker 调用前 Claim：同 session/run 已 claim 时直接返回 nil，不调用 Worker、不发第二个 Done；`StopCurrentTurn` 失败时条件 Rollback 允许人工重试；成功时保留 claim。`handleInput` 完成 Worker binding/MarkRunning 后、调用新的 primary `w.Input` 前执行 `BeginTurn`，即使同一个 Worker run 跨 turn 复用也能清除上一 turn fence。metadata response 和 mid-turn injection 不调用 BeginTurn。

`stop_fence_test.go` 用 table/channel 验证 concurrent Claim 只有一个成功、旧 run Rollback 不清除新 run、BeginTurn 只清同 session。禁止 `time.Sleep`。

- [ ] **Step 4: 实现最小 probe/test wiring**

只扩展 test probe 的 channel 和计数器，不为测试导出 Gateway 私有方法。

- [ ] **Step 5: 验证 GREEN**

Run: `rtk go test ./internal/gateway -run '^TestWorkerLifecycleContract' -count=1 -race -v`

Mutation check: 删除 stop CAS 会导致 stopCalls=2；允许 probe stop 后再发 crash terminal 会导致 terminal count >1。

- [ ] **Step 6: Commit**

```bash
rtk git add internal/gateway/stop_contract_test.go internal/gateway/stop_fence.go internal/gateway/stop_fence_test.go internal/gateway/commands.go internal/gateway/handler.go internal/gateway/contracttest/worker_probe.go internal/gateway/contracttest/worker_probe_test.go
rtk git commit -m "test(gateway): enforce single stop terminal across workers"
```

### Task 6: Reset/reconnect 与 next forwarder 合同

**Files:**
- Modify: `internal/gateway/reset_contract_test.go`
- Modify: `internal/gateway/bridge_test.go`

**Interfaces:**
- Consumes: 现有 `connReplacingWorker`、reset generation、`ResetResult.ConnReplaced`。
- Produces: Claude/Codex replaced 与 OCS/ACP in-place 的四行表。

- [ ] **Step 1: 扩展现有 reset contract**

固定 expected：

```go
[]struct{
	worker worker.WorkerType
	connReplaced bool
}{
	{worker.TypeClaudeCode, true},
	{worker.TypeOpenCodeSrv, false},
	{worker.TypeCodexCLI, true},
	{worker.TypeACP, false},
}
```

每行断言旧 forwarder 关闭后不触发 crash recovery；replacement 行只有新 conn 事件到达，in-place 行仍由原 forwarder继续。

- [ ] **Step 2: RED/GREEN 验证**

Run: `rtk go test ./internal/gateway -run 'TestResetContract|TestBridge_Reset' -count=1 -race -v`

如果当前实现已满足合同，先通过 mutation（颠倒一行 `connReplaced`）证明测试有效，再只提交测试增强；不得制造生产改动来获取 RED。

- [ ] **Step 3: Commit**

```bash
rtk git add internal/gateway/reset_contract_test.go internal/gateway/bridge_test.go
rtk git commit -m "test(gateway): cover reset modes for all workers"
```

### Task 7: Mid-turn Native/fallback 合同

**Files:**
- Modify: `internal/gateway/pending_buffer_test.go`
- Modify: `internal/gateway/handler_test.go`

**Interfaces:**
- Consumes: `worker.MidTurnInjector`、`PendingBuffer`。
- Produces: Claude/Codex 注入；OCS/ACP 缓存并在下一边界只交付一次。

- [ ] **Step 1: 写四行表**

对 active session 发送 supplemental input：

- Claude/Codex fake 实现 `MidTurnInjector`：调用一次，pending size=0，平台收到 `supplement_mode=injected`；
- OCS/ACP fake 不实现该接口：pending size=1，当前 Worker `Input` 不增加，平台收到 `supplement_mode=buffered`；
- turn done 后 drain，OCS/ACP 的补充输入恰好一次成为下一轮，重复 done 不重复 drain。

Run: `rtk go test ./internal/gateway -run 'Test.*MidTurn|TestPendingBuffer' -count=1 -race`

- [ ] **Step 2: 仅在测试暴露真实缺陷时修改生产代码**

生产改动只允许落在 `handler.go`/`pending_buffer.go` 的现有 admission/drain 边界。若需要新 AEP 字段或 Worker 接口，停止并拆 Issue。

- [ ] **Step 3: Commit**

```bash
rtk git add internal/gateway/pending_buffer_test.go internal/gateway/handler_test.go
rtk git commit -m "test(gateway): verify worker mid-turn fallback modes"
```

### Task 8: Worker 生命周期回归门禁

- [ ] `rtk go test ./internal/worker/{claudecode,opencodeserver,codexcli,acp} -count=1 -race`
- [ ] `rtk go test ./internal/gateway -run 'TestWorkerLifecycleContract|TestResetContract|Test.*MidTurn|TestPendingBuffer' -count=1 -race`
- [ ] `rtk make test-contract-matrix`
- [ ] `rtk git diff --check`
- [ ] reviewer 核对 OCS stop 路径中不存在 `release`/delete/SSE cancel；四个 manifest 行都由 concrete type assertion 支撑；重复 stop 只有一个 terminal；next-turn 可用。

Expected: 0 fail、0 race、0 skip。
