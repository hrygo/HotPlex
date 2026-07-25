# IM 渠道 SESSION_BUSY mid-turn 透传 + 兜底 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** IM 渠道用户在 turn 执行中追问撞 SESSION_BUSY 时，由 gateway 按 worker 能力分流——支持 mid-turn 的 worker（CC/codex）直接注入当前 turn，不支持的（acp/ocs）gateway 暂存并在 done 后合并重投，消除黑洞。

**Architecture:** gateway `deliverToWorker` 的 `ErrSessionBusy` 分支探测 worker 是否实现 `MidTurnInjector`：是则透传（CC 写 stdin / codex `turn/steer`），否则存入 bridge 的 `PendingBuffer`。文案经 hub 广播 `message`+`Metadata["supplement_mode"]`，三 conn 识别后发各自 i18n 文案。done 时 bridge 异步重投兜底 pending。

**Tech Stack:** Go（gofmt/tab 缩进）、`go.opentelemetry.io/otel/metric`、testify/require、`pkg/events`+`pkg/aep`。

**Spec:** [`docs/superpowers/specs/2026-07-25-session-busy-midturn-design.md`](../specs/2026-07-25-session-busy-midturn-design.md)

## Global Constraints

- **Mutex**：显式 `mu` 字段，不嵌入 struct，不传 `*sync.Mutex` 指针跨函数。
- **错误**：哨兵 `Err` 前缀；`fmt.Errorf("...: %w", err)` 包装。
- **测试**：testify/require、table-driven、`t.Parallel()`、单模块 ≤5s（`-count=1 -race`）；禁止 `time.Sleep` 等待异步（用 `require.Eventually` 或 channel）。
- **改源码用 Edit 工具**，禁止 `sed -i`。
- **CC `InjectMidTurn` 不得调 `SetLastInput`**（崩溃恢复 `bridge_worker.go` 重投 lastInput，mid-turn 内容污染会导致误重投）——只复用 `writeStreamInputLocked`。
- **不改 active gate**（partial unique index `idx_execution_one_active_per_session` 不动）。
- **不改 AEP wire contract**（不新增 event Kind；文案复用 `message` + metadata key）。
- **mid-turn input 不进 execution ledger**（不占 active gate 槽）。
- 文案托管在各 messaging 渠道（i18n），gateway 不硬编码渠道文案。

---

## File Structure

| 文件 | 责任 |
|---|---|
| `internal/observability/instruments.go` | 新增 `MidTurnInjected`/`SupplementBuffered` counter（仿 `ExecutionSessionBusy`） |
| `internal/worker/worker.go` | 新增 `MidTurnInjector` 可选接口 |
| `internal/worker/claudecode/worker.go` | CC 实现 `InjectMidTurn`（复用 `writeStreamInputLocked`，不 `SetLastInput`） |
| `internal/worker/codexcli/worker.go` | codex 实现 `InjectMidTurn`（调 `manager.SteerTurn`） |
| `internal/gateway/pending_buffer.go`（新） | `PendingBuffer`（Append/DrainAndMerge/Clear/ClearAll）+ 私有 `cloneForReplay` |
| `internal/gateway/bridge.go` | `pending *PendingBuffer` 字段 + 代理方法；`PendingReplayer` 接口 + `SetPendingReplayer` late setter |
| `internal/gateway/bridge_forward.go` | `finishRuntimeOnDone` 成功后异步重投 |
| `internal/gateway/handler.go` | busy 分支 `handleSupplementOnBusy` + `notifySupplement` + `DeliverReplay` |
| `cmd/hotplex/gateway_run.go` | 注入 `bridge.SetPendingReplayer(handler)` |
| `internal/messaging/{feishu,slack,yuanxin}/conn*.go` | message 处理识别 `supplement_mode` 发文案 |
| `docs/reference/metrics.md` | 记录两个新 counter |

---

## Task 1: 新增 observability metrics

**Files:**
- Modify: `internal/observability/instruments.go`（Durable Ingress 块，`ExecutionSessionBusy` 附近）
- Modify: `docs/reference/metrics.md`（Execution 指标表）
- Test: `internal/observability/instruments_test.go`（若已有 counter 测试模式，跟随）

**Interfaces:**
- Produces: `observability.MidTurnInjected() metric.Int64Counter`、`observability.SupplementBuffered() metric.Int64Counter`

- [ ] **Step 1: 在 `instruments.go` 的 Durable Ingress `var` 块（`ExecutionSessionBusy` 同块）加两个变量**

```go
	midTurnInjected       metric.Int64Counter
	midTurnInjectedInit   sync.Once
	supplementBuffered    metric.Int64Counter
	supplementBufferedInit sync.Once
```

- [ ] **Step 2: 在 `ExecutionSessionBusy()` 访问器（`instruments.go:1122-1134`）后加两个访问器**

```go
// MidTurnInjected counts user supplements injected into a running turn
// (worker implements MidTurnInjector; CC headless stream-json / codex turn-steer).
func MidTurnInjected() metric.Int64Counter {
	midTurnInjectedInit.Do(func() {
		var err error
		midTurnInjected, err = Meter().Int64Counter(
			"hotplex.execution.mid_turn_injected",
			metric.WithDescription("User supplements injected into a running turn (mid-turn passthrough)"),
		)
		if err != nil {
			warnInstrument("hotplex.execution.mid_turn_injected", err)
		}
	})
	return midTurnInjected
}

// SupplementBuffered counts user supplements buffered for replay when the
// worker does not support mid-turn injection (acp/ocs fallback).
func SupplementBuffered() metric.Int64Counter {
	supplementBufferedInit.Do(func() {
		var err error
		supplementBuffered, err = Meter().Int64Counter(
			"hotplex.execution.supplement_buffered",
			metric.WithDescription("User supplements buffered for replay (worker lacks mid-turn support)"),
		)
		if err != nil {
			warnInstrument("hotplex.execution.supplement_buffered", err)
		}
	})
	return supplementBuffered
}
```

- [ ] **Step 3: 在 `docs/reference/metrics.md` Execution 指标表加两行**

```markdown
| `hotplex.execution.mid_turn_injected` | Counter | 用户追问在 busy 时被透传注入当前 turn（worker 支持 mid-turn） |
| `hotplex.execution.supplement_buffered` | Counter | 用户追问在 busy 时被暂存待 done 后重投（worker 不支持 mid-turn 的兜底） |
```

- [ ] **Step 4: 编译验证**

Run: `go build ./internal/observability/...`
Expected: 无错误。

- [ ] **Step 5: Commit**

```bash
git add internal/observability/instruments.go docs/reference/metrics.md
git commit -m "feat(observability): add mid_turn_injected + supplement_buffered counters"
```

---

## Task 2: 新增 `MidTurnInjector` 可选接口

**Files:**
- Modify: `internal/worker/worker.go`（`PermissionCeilingReporter` 接口 `:166` 附近）

**Interfaces:**
- Produces: `worker.MidTurnInjector`（`InjectMidTurn(ctx, content, metadata) error`）

- [ ] **Step 1: 在 `PermissionCeilingReporter` 接口后加新接口**

```go
// MidTurnInjector is implemented by workers that can accept a user message
// mid-turn — injecting it into the currently running turn rather than starting
// a new one. Gateway probes this via type assertion at the SESSION_BUSY branch;
// workers that don't implement it fall back to pending-buffer replay.
//
// Implementations MUST NOT update crash-recovery lastInput: a mid-turn inject
// is supplemental, not the turn's primary input.
type MidTurnInjector interface {
	// InjectMidTurn delivers a user message into the active turn of the worker.
	// It must return an error if the turn is no longer active (caller falls back
	// to pending-buffer replay).
	InjectMidTurn(ctx context.Context, content string, metadata map[string]any) error
}
```

- [ ] **Step 2: 编译验证**

Run: `go build ./internal/worker/...`
Expected: 无错误。

- [ ] **Step 3: Commit**

```bash
git add internal/worker/worker.go
git commit -m "feat(worker): add MidTurnInjector optional interface"
```

---

## Task 3: CC 实现 `InjectMidTurn`

**Files:**
- Modify: `internal/worker/claudecode/worker.go`（`Input` 方法 `:521` 后）
- Test: `internal/worker/claudecode/input_test.go`（或 `worker_test.go`，跟随现有 mid-turn/stdin 测试位置）

**Interfaces:**
- Consumes: `worker.MidTurnInjector`（Task 2）
- Produces: `(*Worker).InjectMidTurn` —— 复用 `writeStreamInputLocked`（`input.go:43`），**不调 `SetLastInput`**

- [ ] **Step 1: 写失败测试**（验证 InjectMidTurn 写 stdin 且不调 SetLastInput）

```go
func TestInjectMidTurn_WritesStdinWithoutSetLastInput(t *testing.T) {
	t.Parallel()
	w, baseConn := newTestWorkerWithPipeConn(t) // 现有测试 helper：构造 *Worker + *base.Conn（pipe stdin）
	content := "BONUS: tell me 7+8"

	require.NoError(t, w.InjectMidTurn(t.Context(), content, nil))

	// 断言 stdin 收到 stream-json user 消息
	frame, err := baseConn.ReadStringFromStdin(t.Context())
	require.NoError(t, err)
	require.Contains(t, frame, content)
	require.Contains(t, frame, `"type":"user"`)

	// 关键约束：InjectMidTurn 不得更新 lastInput（崩溃恢复重投 lastInput）
	require.Empty(t, baseConn.LastInput(), "InjectMidTurn must not SetLastInput")
}
```

> 若 `newTestWorkerWithPipeConn`/`ReadStringFromStdin` helper 不存在，复用现有 `worker_test.go` 里测 `Input` 写 stdin 的 helper（同模式）；`base.Conn.LastInput()` 取值器若未导出，用 `SetLastInput` 后 `LastInput()` 的现有访问路径（确认 `base/conn.go` 暴露）。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/worker/claudecode/ -run TestInjectMidTurn -count=1`
Expected: FAIL（`InjectMidTurn` 未定义）。

- [ ] **Step 3: 实现 `InjectMidTurn`**（在 `Input` 方法后插入；复制 `Input` 的 `:539-574` stdin 写段，**去掉 `:575 SetLastInput`**，加 `SetLastIO`）

```go
// InjectMidTurn delivers a user message into the active turn by writing the
// same stream-json user frame to stdin. Claude Code (headless --print
// --input-format stream-json) absorbs it into the running turn rather than
// queuing a new one (verified by mid-turn injection probe, 2026-07-25).
//
// Unlike Input, it MUST NOT call SetLastInput: this is a supplemental inject,
// not the turn's primary input — crash recovery (bridge_worker.go) replays
// lastInput and must not replay a mid-turn supplement.
func (w *Worker) InjectMidTurn(ctx context.Context, content string, metadata map[string]any) error {
	conn := w.Conn()
	if conn == nil {
		return fmt.Errorf("claudecode: not started")
	}
	baseConn, ok := conn.(*base.Conn)
	if !ok {
		return fmt.Errorf("claudecode: inject requires base conn")
	}
	stdin, mu := baseConn.StdinUnlocked()
	if stdin == nil {
		return &worker.WorkerError{Kind: worker.ErrKindUnavailable, Message: "claudecode: stdin closed"}
	}
	// Mutex acquired inside the closure for the same orphan-write reason as Input
	// (see Input comment, worker.go:544-554).
	if err := base.WriteWithCtxBounded(ctx, func() error {
		mu.Lock()
		defer mu.Unlock()
		return writeStreamInputLocked(stdin, content)
	}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &worker.WorkerError{
				Kind:    worker.ErrKindUnavailable,
				Message: "claudecode: stdin write stalled (worker not reading input)",
				Cause:   err,
			}
		}
		return fmt.Errorf("claudecode: inject mid-turn: %w", err)
	}
	w.SetLastIO(time.Now())
	return nil
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./internal/worker/claudecode/ -run TestInjectMidTurn -count=1 -race`
Expected: PASS。

- [ ] **Step 5: 确认 `*Worker` 现在满足 `worker.MidTurnInjector`**（编译期断言）

在 `worker.go` 末尾加（或测试文件加）：
```go
var _ worker.MidTurnInjector = (*Worker)(nil)
```
Run: `go build ./internal/worker/claudecode/`
Expected: 无错误（若报错说明签名不匹配，修正）。

- [ ] **Step 6: Commit**

```bash
git add internal/worker/claudecode/worker.go internal/worker/claudecode/*_test.go
git commit -m "feat(claudecode): implement InjectMidTurn (reuses stdin write, no SetLastInput)"
```

---

## Task 4: codex 实现 `InjectMidTurn`

**Files:**
- Modify: `internal/worker/codexcli/worker.go`（`StopCurrentTurn` `:529` 后）
- Test: `internal/worker/codexcli/worker_test.go`（跟随现有测试位置）

**Interfaces:**
- Consumes: `worker.MidTurnInjector`（Task 2）、`(*CodexAppServerManager).SteerTurn(threadID, expectedTurnID, text)`（`manager.go:1093`，已存在）
- Produces: `(*AppServerWorker).InjectMidTurn`

- [ ] **Step 1: 写失败测试**（mock manager，验证 SteerTurn 被正确参数调用；无 active turn 时返回 error）

```go
func TestInjectMidTurn_SteersActiveTurn(t *testing.T) {
	t.Parallel()
	w := newTestAppServerWorker(t) // 现有 helper
	w.threadID = "thr_1"
	w.turnID = "turn_1"

	require.NoError(t, w.InjectMidTurn(t.Context(), "more info", nil))

	calls := w.manager.SteerTurnCalls() // mock 记录
	require.Len(t, calls, 1)
	require.Equal(t, "thr_1", calls[0].ThreadID)
	require.Equal(t, "turn_1", calls[0].ExpectedTurnID)
	require.Equal(t, "more info", calls[0].Text)
}

func TestInjectMidTurn_NoActiveTurn(t *testing.T) {
	t.Parallel()
	w := newTestAppServerWorker(t)
	// threadID/turnID 空
	require.Error(t, w.InjectMidTurn(t.Context(), "x", nil))
}
```

> 复用现有 codex 测试里 mock `*CodexAppServerManager` 的模式（`StopCurrentTurn`/`InterruptTurn` 已有测试，照抄 mock）。若 `SteerTurnCalls` 记录器不存在，在 mock manager 上加（同 InterruptTurn mock 模式）。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/worker/codexcli/ -run TestInjectMidTurn -count=1`
Expected: FAIL。

- [ ] **Step 3: 实现 `InjectMidTurn`**（照抄 `StopCurrentTurn:529-539` 锁模式）

```go
// InjectMidTurn steers the active turn with supplemental user input via the
// app-server turn/steer RPC. Unlike Input (turn/start), it does not start a
// new turn and does not update lastInput. Returns an error if no turn is
// active so the gateway can fall back to pending-buffer replay.
func (w *AppServerWorker) InjectMidTurn(ctx context.Context, content string, metadata map[string]any) error {
	w.mu.Lock()
	tid := w.threadID
	turnID := w.turnID
	w.mu.Unlock()
	if tid == "" || turnID == "" {
		return fmt.Errorf("codexcli: no active turn to steer")
	}
	_, err := w.manager.SteerTurn(tid, turnID, content)
	return err
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./internal/worker/codexcli/ -run TestInjectMidTurn -count=1 -race`
Expected: PASS。

- [ ] **Step 5: 编译期断言**

在 `worker.go` 末尾加：`var _ worker.MidTurnInjector = (*AppServerWorker)(nil)`
Run: `go build ./internal/worker/codexcli/`
Expected: 无错误。

- [ ] **Step 6: Commit**

```bash
git add internal/worker/codexcli/worker.go internal/worker/codexcli/*_test.go
git commit -m "feat(codexcli): implement InjectMidTurn via turn/steer (first caller)"
```

---

## Task 5: `PendingBuffer` + `cloneForReplay`

**Files:**
- Create: `internal/gateway/pending_buffer.go`
- Test: `internal/gateway/pending_buffer_test.go`

**Interfaces:**
- Consumes: `events.Clone`（`pkg/events/events.go:126`）、`aep.NewID()`（`pkg/aep/codec.go:189`）
- Produces: `PendingBuffer`（Append/DrainAndMerge/Clear/ClearAll）、`cloneForReplay`

- [ ] **Step 1: 写失败测试**（table-driven：单条原样/多条合并编号/去重/上限20/空/并发）

```go
func TestPendingBuffer_DrainAndMerge(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		inputs  []string
		wantSub []string // merged 中应包含的子串
		single  bool
	}{
		{"single passthrough", []string{"继续"}, []string{"继续"}, true},
		{"multi merged numbered", []string{"继续", "补充", "换角度"},
			[]string{"3 条消息", "1. 继续", "2. 补充", "3. 换角度"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pb := NewPendingBuffer()
			env := newInputEnvelope(t, "sess_1", "orig")
			for _, c := range tc.inputs {
				pb.Append("sess_1", c, env)
			}
			merged, repr, ok := pb.DrainAndMerge("sess_1")
			require.True(t, ok)
			require.NotNil(t, repr)
			if tc.single {
				require.Equal(t, tc.inputs[0], merged)
			} else {
				for _, s := range tc.wantSub {
					require.Contains(t, merged, s)
				}
			}
			// drain 后清空
			_, _, ok2 := pb.DrainAndMerge("sess_1")
			require.False(t, ok2)
		})
	}
}

func TestPendingBuffer_DedupAdjacentAndCap(t *testing.T) {
	t.Parallel()
	pb := NewPendingBuffer()
	env := newInputEnvelope(t, "s", "x")
	// 相邻完全相同去重
	pb.Append("s", "同", env); pb.Append("s", "同", env)
	merged, _, _ := pb.DrainAndMerge("s")
	require.Equal(t, "同", merged) // 只剩一条

	// 上限 20：塞 25 条，只保留最新 20，合并后编号 1..20
	for i := 0; i < 25; i++ {
		pb.Append("s", fmt.Sprintf("m%d", i), env)
	}
	merged, _, _ = pb.DrainAndMerge("s")
	require.Contains(t, merged, "20 条消息")
	require.Contains(t, merged, "1. m5")  // 最旧保留的是 m5（丢 m0..m4）
	require.Contains(t, merged, "20. m24")
}

func TestPendingBuffer_Concurrent(t *testing.T) {
	t.Parallel()
	pb := NewPendingBuffer()
	env := newInputEnvelope(t, "s", "x")
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() { pb.Append("s", "c", env); done <- struct{}{} }()
		go func() { pb.DrainAndMerge("s"); done <- struct{}{} }()
	}
	for i := 0; i < 100; i++ { <-done }
}

func TestCloneForReplay_NewIDs(t *testing.T) {
	t.Parallel()
	orig := newInputEnvelope(t, "s", "old")
	orig.Event.Data.(map[string]any)["client_message_id"] = "cmid_orig"
	c := cloneForReplay(orig, "new content")
	require.NotEqual(t, orig.ID, c.ID)
	require.NotEqual(t, "cmid_orig", c.Event.Data.(map[string]any)["client_message_id"])
	require.Equal(t, "new content", c.Event.Data.(map[string]any)["content"])
	require.Equal(t, "s", c.SessionID)            // 复用 session
	require.Zero(t, c.Seq)                          // 由 hub 重分配
}
```

`newInputEnvelope` helper（测试文件内）：
```go
func newInputEnvelope(t *testing.T, sid, content string) *events.Envelope {
	t.Helper()
	return events.NewEnvelope("evt_orig", sid, 0, events.Input, map[string]any{
		"content":            content,
		"client_message_id":  "cmid_orig",
	})
}
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/gateway/ -run TestPendingBuffer -count=1`
Expected: FAIL（`PendingBuffer` 未定义）。

- [ ] **Step 3: 创建 `internal/gateway/pending_buffer.go`**

```go
package gateway

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// maxPendingPerSession caps buffered supplements per session to bound memory.
const maxPendingPerSession = 20

type pendingEntry struct {
	content    string
	envelope   *events.Envelope
	receivedAt int64
}

// PendingBuffer holds user supplements that arrived while a session was busy,
// keyed by sessionID. Only used for workers that do NOT implement
// MidTurnInjector (acp/ocs fallback); mid-turn-capable workers inject directly.
type PendingBuffer struct {
	mu    sync.Mutex
	items map[string][]pendingEntry
}

func NewPendingBuffer() *PendingBuffer {
	return &PendingBuffer{items: make(map[string][]pendingEntry)}
}

// Append adds a supplement. Adjacent identical content is deduped; over the
// per-session cap the oldest are dropped to keep the newest.
func (p *PendingBuffer) Append(sessionID, content string, env *events.Envelope) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.items[sessionID]
	if n := len(items); n > 0 && items[n-1].content == content {
		return
	}
	items = append(items, pendingEntry{content: content, envelope: events.Clone(env), receivedAt: time.Now().UnixMilli()})
	if len(items) > maxPendingPerSession {
		items = items[len(items)-maxPendingPerSession:]
	}
	p.items[sessionID] = items
}

// DrainAndMerge atomically removes and returns the merged supplement for a
// session. A single entry is returned verbatim; multiple are joined with a
// header and 1-based numbering. repr is the last entry's envelope (template
// for replay). ok is false if nothing was buffered.
func (p *PendingBuffer) DrainAndMerge(sessionID string) (string, *events.Envelope, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := p.items[sessionID]
	if len(items) == 0 {
		delete(p.items, sessionID)
		return "", nil, false
	}
	delete(p.items, sessionID)
	repr := items[len(items)-1].envelope
	if len(items) == 1 {
		return items[0].content, repr, true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "（以下是上一轮执行期间追加的 %d 条消息，请一并处理）\n", len(items))
	for i, it := range items {
		fmt.Fprintf(&b, "%d. %s\n", i+1, strings.TrimSpace(it.content))
	}
	return b.String(), repr, true
}

func (p *PendingBuffer) Clear(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.items, sessionID)
}

func (p *PendingBuffer) ClearAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.items = make(map[string][]pendingEntry)
}

// cloneForReplay builds a fresh Input envelope for replay: new id + new
// client_message_id (avoids UNIQUE(session_id, client_message_id) dedup),
// replaced content, reused session/owner/metadata, seq=0 (re-assigned by hub).
// Lives in gateway (not pkg/events) because it needs aep.NewID and pkg/events
// cannot import pkg/aep (cycle).
func cloneForReplay(env *events.Envelope, content string) *events.Envelope {
	c := events.Clone(env) // deep-copies Event.Data map + Metadata
	c.ID = aep.NewID()
	c.Seq = 0
	if data, ok := c.Event.Data.(map[string]any); ok {
		data["content"] = content
		data["client_message_id"] = aep.NewID()
	}
	return c
}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./internal/gateway/ -run "TestPendingBuffer|TestCloneForReplay" -count=1 -race`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/pending_buffer.go internal/gateway/pending_buffer_test.go
git commit -m "feat(gateway): add PendingBuffer + cloneForReplay for busy supplements"
```

---

## Task 6: Bridge 持有 `PendingBuffer` + `PendingReplayer` late setter

**Files:**
- Modify: `internal/gateway/bridge.go`（Bridge struct `:43-100`、`SetAuditCollector` `:225` 附近、`NewBridge` 初始化处）

**Interfaces:**
- Consumes: `PendingBuffer`（Task 5）
- Produces: `(*Bridge).BufferPending`、`ClearPending`、`ClearAllPending`、`PendingReplayer` 接口、`SetPendingReplayer`、内部 `b.pending`（同包 `bridge_forward` 用）

- [ ] **Step 1: 在 `Bridge` struct 加字段**（`accum`/`accumMu` `:74-82` 附近，遵循 granular mutex 模式）

```go
	pending   *PendingBuffer   // busy-supplement buffer (acp/ocs fallback)
	replayer  PendingReplayer  // late-injected (Bridge built before Handler)
```

- [ ] **Step 2: 在 `NewBridge`（构造函数）初始化 `pending`**

```go
	pending: NewPendingBuffer(),
```
（在 `BridgeDeps` 构造的 struct literal 里加这一行。）

- [ ] **Step 3: 加 `PendingReplayer` 接口 + setter + 代理方法**（`SetAuditCollector:225` 后）

```go
// PendingReplayer replays a buffered supplement as a fresh input turn.
// Implemented by Handler (which owns deliverToWorker); injected after Bridge
// construction via SetPendingReplayer because Bridge is built before Handler.
type PendingReplayer interface {
	DeliverReplay(ctx context.Context, env *events.Envelope) error
}

// SetPendingReplayer late-injects the replay target (Handler). Optional: nil
// leaves done-time replay disabled (supplements buffered but not replayed).
func (b *Bridge) SetPendingReplayer(r PendingReplayer) { b.replayer = r }

// BufferPending appends a busy-supplement for the fallback path (worker lacks
// mid-turn support). Called from Handler's SESSION_BUSY branch.
func (b *Bridge) BufferPending(sessionID string, env *events.Envelope, content string) {
	if b.pending != nil {
		b.pending.Append(sessionID, content, env)
	}
}

func (b *Bridge) ClearPending(sessionID string) {
	if b.pending != nil {
		b.pending.Clear(sessionID)
	}
}

func (b *Bridge) ClearAllPending() {
	if b.pending != nil {
		b.pending.ClearAll()
	}
}
```

- [ ] **Step 4: 编译验证**

Run: `go build ./internal/gateway/...`
Expected: 无错误。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/bridge.go
git commit -m "feat(gateway): Bridge holds PendingBuffer + PendingReplayer late setter"
```

---

## Task 7: Handler busy 分支透传/兜底 + `notifySupplement` + `DeliverReplay`

**Files:**
- Modify: `internal/gateway/handler.go`（busy 分支 `:536-540`、新增三个方法）
- Test: `internal/gateway/handler_test.go`（跟随现有 busy/SESSION_BUSY 测试位置）

**Interfaces:**
- Consumes: `worker.MidTurnInjector`、`observability.MidTurnInjected/SupplementBuffered`（Task 1）、`h.bridge.BufferPending`（Task 6）、`h.sm.GetWorker`、`h.hub.SendToSession/NextSeq`
- Produces: `(*Handler).handleSupplementOnBusy`、`(*Handler).notifySupplement`、`(*Handler).DeliverReplay`

- [ ] **Step 1: 写失败测试**（mock worker：实现 MidTurnInjector → 透传；不实现 → buffer；透传失败 → 回退 buffer）

```go
type mockMidTurnWorker struct {
	mockWorker             // 现有 mock，满足 worker.Worker
	injectErr   error
	injected    string
	injectCount int
}

func (m *mockMidTurnWorker) InjectMidTurn(ctx context.Context, content string, md map[string]any) error {
	m.injectCount++
	m.injected = content
	return m.injectErr
}

func TestHandleSupplementOnBusy_Passthrough(t *testing.T) {
	t.Parallel()
	h, sm := newTestHandler(t) // 现有 helper
	mw := &mockMidTurnWorker{}
	sm.worker = mw
	env := newInputEnvelope(t, "s", "追问")

	err := h.handleSupplementOnBusy(t.Context(), env, "追问")
	require.NoError(t, err)            // 透传成功不报错
	require.Equal(t, "追问", mw.injected)
}

func TestHandleSupplementOnBusy_FallbackBuffer(t *testing.T) {
	t.Parallel()
	h, sm := newTestHandler(t)
	sm.worker = &mockWorkerNoMidTurn{} // 不实现 MidTurnInjector
	env := newInputEnvelope(t, "s", "追问")

	require.NoError(t, h.handleSupplementOnBusy(t.Context(), env, "追问"))
	// bridge buffer 收到
	merged, _, ok := h.bridge.pending.DrainAndMerge("s")
	require.True(t, ok)
	require.Equal(t, "追问", merged)
}

func TestHandleSupplementOnBusy_InjectFailureFallsBack(t *testing.T) {
	t.Parallel()
	h, sm := newTestHandler(t)
	mw := &mockMidTurnWorker{injectErr: errors.New("turn ended")}
	sm.worker = mw
	env := newInputEnvelope(t, "s", "追问")

	require.NoError(t, h.handleSupplementOnBusy(t.Context(), env, "追问"))
	require.Equal(t, 1, mw.injectCount)            // 试了透传
	merged, _, ok := h.bridge.pending.DrainAndMerge("s")
	require.True(t, ok)                            // 失败 → 回退 buffer
	require.Equal(t, "追问", merged)
}
```

> 复用现有 `handler_test.go` 的 `newTestHandler`/`mockWorker` 模式（参考现有 SESSION_BUSY 测试）。`newInputEnvelope` 同 Task 5。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/gateway/ -run TestHandleSupplementOnBusy -count=1`
Expected: FAIL。

- [ ] **Step 3: 改造 busy 分支**（`handler.go:536-540`）

将：
```go
		if errors.Is(err, execution.ErrSessionBusy) {
			observability.ExecutionSessionBusy().Add(ctx, 1)
			h.cancelRetryIfNeeded(env.SessionID)
			return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session has an active execution")
		}
```
改为：
```go
		if errors.Is(err, execution.ErrSessionBusy) {
			observability.ExecutionSessionBusy().Add(ctx, 1)
			h.cancelRetryIfNeeded(env.SessionID)
			return h.handleSupplementOnBusy(ctx, env, content)
		}
```

- [ ] **Step 4: 实现三个方法**（`deliverToWorker` 后或 `tryInteractionResponse` 后）

```go
// handleSupplementOnBusy routes a SESSION_BUSY input to mid-turn passthrough
// (worker implements MidTurnInjector) or the fallback pending buffer.
func (h *Handler) handleSupplementOnBusy(ctx context.Context, env *events.Envelope, content string) error {
	if w := h.sm.GetWorker(env.SessionID); w != nil {
		if inj, ok := w.(worker.MidTurnInjector); ok && !w.IsStopped() {
			if err := inj.InjectMidTurn(ctx, content, nil); err == nil {
				observability.MidTurnInjected().Add(ctx, 1)
				if h.bridge != nil {
					h.bridge.CaptureInboundEvent(env.SessionID, env.Seq, events.Input, env.Event.Data)
				}
				h.notifySupplement(ctx, env.SessionID, "injected")
				return nil
			} else {
				h.log.Warn("gateway: mid-turn inject failed, falling back to buffer",
					"session_id", env.SessionID, "err", err)
			}
		}
	}
	if h.bridge != nil {
		h.bridge.BufferPending(env.SessionID, env, content)
		observability.SupplementBuffered().Add(ctx, 1)
		h.notifySupplement(ctx, env.SessionID, "buffered")
		return nil
	}
	return h.sendErrorf(ctx, env, events.ErrCodeSessionBusy, "session has an active execution")
}

// notifySupplement broadcasts a marker message so platform conns can render
// their own i18n "supplement accepted" text. Metadata carries the mode
// ("injected"|"buffered"); Content is empty (conns substitute their text).
func (h *Handler) notifySupplement(ctx context.Context, sessionID, mode string) {
	env := events.NewEnvelope(aep.NewID(), sessionID, h.hub.NextSeq(sessionID),
		events.Message, events.MessageData{Content: ""})
	env.Metadata = map[string]any{"supplement_mode": mode}
	_ = h.hub.SendToSession(ctx, env)
}

// DeliverReplay replays a buffered supplement as a fresh input. Implements
// bridge.PendingReplayer; active gate is already released by the prior Done.
func (h *Handler) DeliverReplay(ctx context.Context, env *events.Envelope) error {
	data, _ := env.Event.Data.(map[string]any)
	content, _ := data["content"].(string)
	return h.deliverToWorker(ctx, env, content)
}
```

- [ ] **Step 5: 运行测试，确认通过**

Run: `go test ./internal/gateway/ -run TestHandleSupplementOnBusy -count=1 -race`
Expected: PASS。

- [ ] **Step 6: 确认 `*Handler` 满足 `PendingReplayer`**

在 `handler.go` 末尾加：`var _ PendingReplayer = (*Handler)(nil)`
Run: `go build ./internal/gateway/`
Expected: 无错误。

- [ ] **Step 7: Commit**

```bash
git add internal/gateway/handler.go internal/gateway/handler_test.go
git commit -m "feat(gateway): SESSION_BUSY branch — mid-turn passthrough or buffer fallback"
```

---

## Task 8: `finishRuntimeOnDone` 异步重投

**Files:**
- Modify: `internal/gateway/bridge_forward.go`（`finishRuntimeOnDone:474`，`FinishRuntime` 成功后 `:527` 附近）

**Interfaces:**
- Consumes: `b.pending.DrainAndMerge`、`b.replayer`（Task 6）、`cloneForReplay`（Task 5）

- [ ] **Step 1: 写失败测试**（mock replayer，drain 后调用 DeliverReplay 一次；无 pending 不调用）

```go
func TestFinishRuntimeOnDone_ReplaysPending(t *testing.T) {
	t.Parallel()
	b, fc := newTestBridgeWithDoneEnv(t) // 现有 helper：构造 Bridge + forwardContext + Done envelope
	b.executionStore = newFakeExecStoreWithOpen(t, "s", "exec_1") // 返回可 FinishRuntime 的 open 记录
	rp := &fakeReplayer{}
	b.SetPendingReplayer(rp)
	b.pending.Append("s", "追问", newInputEnvelope(t, "s", "x"))

	b.finishRuntimeOnDone("s", fc, fc.doneEnv)

	require.Eventually(t, func() bool { return rp.calls.Load() == 1 },
		time.Second, 10*time.Millisecond)
	require.Equal(t, "追问", rp.lastContent.Load())
}

func TestFinishRuntimeOnDone_NoPendingNoReplay(t *testing.T) {
	t.Parallel()
	b, fc := newTestBridgeWithDoneEnv(t)
	b.executionStore = newFakeExecStoreWithOpen(t, "s", "exec_1")
	rp := &fakeReplayer{}
	b.SetPendingReplayer(rp)
	b.finishRuntimeOnDone("s", fc, fc.doneEnv)
	time.Sleep(50 * time.Millisecond) // 给 goroutine 机会（仅此处可接受短 sleep：断言非触发）
	require.Zero(t, rp.calls.Load())
}
```

> `fakeReplayer` 记录 calls/lastContent（atomic）。`newInputEnvelope` 同 Task 5。`newTestBridgeWithDoneEnv`/`newFakeExecStoreWithOpen` 复用现有 bridge_forward 测试模式。

- [ ] **Step 2: 运行测试，确认失败**

Run: `go test ./internal/gateway/ -run TestFinishRuntimeOnDone_Replays -count=1`
Expected: FAIL。

- [ ] **Step 3: 在 `finishRuntimeOnDone` 的 success 路径插入重投**

在 `FinishRuntime` err-block 之后、`observability.ExecutionRuntimeOutcome().Add(...)`（`:527`）**之前**插入：

```go
	// Replay buffered supplements now that the active gate is released. This
	// method runs under processForwardedEvent's held seq lease, so replay
	// MUST run async (it re-enters deliverToWorker → acceptInputExecution →
	// NextSeq); do not call BeginSeqOperation here.
	if b.pending != nil && b.replayer != nil {
		if merged, repr, ok := b.pending.DrainAndMerge(sessionID); ok {
			replayEnv := cloneForReplay(repr, merged)
			go func() {
				if err := b.replayer.DeliverReplay(context.Background(), replayEnv); err != nil {
					b.log.Warn("bridge: supplement replay failed", "session_id", sessionID, "err", err)
					// replay 又 busy（done 后瞬间被占）：递归存回，等下一个 done
					b.pending.Append(sessionID, merged, replayEnv)
				}
			}()
		}
	}
```

- [ ] **Step 4: 运行测试，确认通过**

Run: `go test ./internal/gateway/ -run TestFinishRuntimeOnDone -count=1 -race`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/bridge_forward.go internal/gateway/bridge_forward_test.go
git commit -m "feat(gateway): replay buffered supplements after Done (async, lease-safe)"
```

---

## Task 9: 注入 `SetPendingReplayer`

**Files:**
- Modify: `cmd/hotplex/gateway_run.go`（`:557-559` `SetAuditCollector` 注入链）

- [ ] **Step 1: 在 `bridge.SetAuditCollector(auditCollector)` 后加一行**

```go
	bridge.SetPendingReplayer(handler)
```

- [ ] **Step 2: 编译验证**

Run: `go build ./cmd/hotplex/...`
Expected: 无错误。

- [ ] **Step 3: Commit**

```bash
git add cmd/hotplex/gateway_run.go
git commit -m "feat(gateway): wire bridge.SetPendingReplayer(handler)"
```

---

## Task 10: 三渠道 conn 识别 `supplement_mode` 发文案

**Files:**
- Modify: `internal/messaging/feishu/conn.go`（message 处理，`WriteCtx:238` 附近）
- Modify: `internal/messaging/slack/conn_events.go`（`handleDefaultText:137` 或 message 分发处）
- Modify: `internal/messaging/yuanxin/adapter.go`（`WriteCtx:585` message case）
- Test: 各渠道 `*_test.go`

**Interfaces:**
- Consumes: `env.Metadata["supplement_mode"]`、各渠道文案方法（feishu `sendTextMessage:257` / slack `writeWithPostMessage:956` / yuanxin `SendResponse:507`）

- [ ] **Step 1: 飞书 — 在 `WriteCtx` 的 message 分支前检查 mode**

在 feishu `conn.go` message 事件处理入口（展示 text 之前）加：
```go
if mode, _ := env.Metadata["supplement_mode"].(string); mode != "" {
	text := feishuSupplementText(mode) // injected/buffered 文案
	_ = c.adapter.sendTextMessage(ctx, c.chatID, text)
	return nil
}
```
文案常量（feishu 包内）：
```go
func feishuSupplementText(mode string) string {
	if mode == "injected" {
		return "⏳ 已收到，正在当前任务中一并处理"
	}
	return "⏳ 已收到，当前任务完成后会自动处理"
}
```

- [ ] **Step 2: Slack — 在 `handleDefaultText`（或 message 展示入口）前检查 mode**

```go
if mode, _ := env.Metadata["supplement_mode"].(string); mode != "" {
	_ = c.writeWithPostMessage(ctx, slackSupplementText(mode), false)
	return nil
}
```
```go
func slackSupplementText(mode string) string {
	if mode == "injected" {
		return "⏳ Got it — processing within the current task."
	}
	return "⏳ Got it — will process automatically once the current task finishes."
}
```

- [ ] **Step 3: yuanxin — 在 `WriteCtx` 的 message case 前检查 mode**

```go
if mode, _ := env.Metadata["supplement_mode"].(string); mode != "" {
	return c.adapter.SendResponse(ctx, c, yuanxinSupplementText(mode))
}
```
```go
func yuanxinSupplementText(mode string) string {
	if mode == "injected" {
		return "⏳ 已收到，正在当前任务中一并处理"
	}
	return "⏳ 已收到，当前任务完成后会自动处理"
}
```

- [ ] **Step 4: 各渠道写测试**（mock envelope 带 `supplement_mode=injected|buffered`，断言文案方法被调用、展示路径不被触发）

示例（feishu）：
```go
func TestWriteCtx_SupplementMode_RendersI18nText(t *testing.T) {
	t.Parallel()
	c, adapter := newTestFeishuConn(t)
	for _, mode := range []string{"injected", "buffered"} {
		env := &events.Envelope{SessionID: "s", Event: events.Event{Type: events.Message},
			Metadata: map[string]any{"supplement_mode": mode}}
		require.NoError(t, c.WriteCtx(t.Context(), env))
		require.Contains(t, adapter.lastSentText, "⏳ 已收到")
	}
}
```
（slack/yuanxin 同模式，跟随各自现有 `WriteCtx`/`handleDefaultText` 测试 helper。）

- [ ] **Step 5: 运行测试**

Run: `go test ./internal/messaging/feishu/ ./internal/messaging/slack/ ./internal/messaging/yuanxin/ -run Supplement -count=1 -race`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/messaging/feishu/conn.go internal/messaging/slack/conn_events.go internal/messaging/yuanxin/adapter.go internal/messaging/*/conn*_test.go
git commit -m "feat(messaging): recognize supplement_mode metadata, render i18n busy text"
```

---

## Task 11: 清理路径（conn.Close / CloseSharedState）

**Files:**
- Modify: `internal/messaging/feishu/conn.go`（`Close`）
- Modify: `internal/messaging/slack/conn_events.go` 或 `adapter.go`（`Close`）
- Modify: `internal/messaging/yuanxin/adapter.go`（`Close`）
- Modify: `internal/messaging/platform_adapter.go`（`CloseSharedState:72`）
- Test: 相关 `*_test.go`

**Interfaces:**
- Consumes: `bridge.ClearPending(sessionID)`、`bridge.ClearAllPending()`（Task 6）—— messaging 层调 gateway Bridge 需通过其 HubInterface/现有 bridge 句柄；若 messaging PlatformAdapter 不持有 gateway Bridge，则清理由 gateway 侧 session 生命周期触发（见 Step 1 注）。

- [ ] **Step 1: 确认清理触发点**

> **先勘察**：messaging `PlatformAdapter` 持有的是 messaging `*Bridge`（`platform_adapter.go:14`），不是 gateway Bridge。gateway Bridge 的 `ClearPending`/`ClearAllPending` 无法从 messaging 层直接调。
>
> 因此清理挂在 **gateway 侧 session 生命周期**：
> - `sessionID` 级清理（conn.Close 对应）：gateway 在 session `terminated/deleted` 时调 `bridge.ClearPending(sessionID)`。
> - 全局清理：gateway 关闭时 `bridge.ClearAllPending()`。
>
> 在 `internal/gateway` 找 session 终止/删除路径（session manager delete、bridge shutdown），插入 `ClearPending`/`ClearAllPending`。若现有 session 删除已有 cleanup outbox/钩子，跟随该模式。

在 gateway session 删除/终止路径加：
```go
	h.bridge.ClearPending(sessionID)  // 或 b.ClearPending 在 bridge 内部钩子
```
在 bridge shutdown（`Close`/`shutdown`）加：
```go
	b.ClearAllPending()
```

- [ ] **Step 2: 写测试**（session 删除后 pending 被清空；bridge shutdown 清空全部）

```go
func TestPendingClearedOnSessionEnd(t *testing.T) {
	t.Parallel()
	b := newTestBridge(t)
	b.pending.Append("s1", "x", newInputEnvelope(t, "s1", "x"))
	b.ClearPending("s1")
	_, _, ok := b.pending.DrainAndMerge("s1")
	require.False(t, ok)
}
```

- [ ] **Step 3: 运行测试 + 编译**

Run: `go test ./internal/gateway/ -run TestPendingCleared -count=1 -race && go build ./...`
Expected: PASS + 无错误。

- [ ] **Step 4: Commit**

```bash
git add internal/gateway/*.go
git commit -m "feat(gateway): clear pending buffer on session end and bridge shutdown"
```

---

## Task 12: 全量验证 + 文档

- [ ] **Step 1: 全量测试（含 race）**

Run: `go test ./... -count=1 -race -timeout 300s`
Expected: 全绿。

- [ ] **Step 2: 质量门禁**

Run: `make check`（或 `make lint && make test-short`，跟随仓库 `make help`）
Expected: 通过。

- [ ] **Step 3: 确认设计文档/计划与实现一致**（无残留 `SupplementNotifier` 引用）

Run: `grep -rn "SupplementNotifier\|NotifySupplementAccepted" internal/ cmd/ docs/`
Expected: 仅 docs/specs 历史段（若有），代码无引用。

- [ ] **Step 4: Commit 收尾**（若有文档微调）

```bash
git add docs/
git commit -m "docs(mid-turn): finalize spec/plan after implementation"
```

---

## Self-Review 结果

**Spec coverage**：设计文档各节均有任务覆盖——MidTurnInjector（T2）、CC/codex 实现（T3/T4）、PendingBuffer（T5）、busy 决策+文案+重投（T6/T7/T8/T9）、三渠道文案（T10）、清理（T11）、metrics（T1）。typed-error 不做（设计已定，无任务，正确）。acp/ocs 不实现 MidTurnInjector → 自动兜底（T7 的 else 分支）。

**Placeholder 风险**：T3/T4/T10 的测试 helper（`newTestWorkerWithPipeConn`/`newTestAppServerWorker`/`newTestFeishuConn` 等）标注"复用现有模式"——这些 helper 在各自 `*_test.go` 已存在（`Input`/`StopCurrentTurn`/`WriteCtx` 已有测试），实施时按现有 mock 跟随；若不存在则按现有同模式创建。T11 Step 1 标"先勘察"——因为 messaging 层不持有 gateway Bridge，清理点的精确位置需实施时确认（设计已锁定挂 gateway session 生命周期）。

**类型一致性**：`MidTurnInjector.InjectMidTurn(ctx, content, metadata)` 签名（T2）== CC/codex 实现（T3/T4）；`PendingReplayer.DeliverReplay(ctx, env)`（T6）== Handler 实现（T7）== bridge_forward 调用（T8）；`PendingBuffer.Append/DrainAndMerge/Clear/ClearAll`（T5）== bridge 代理（T6）== bridge_forward 调用（T8）；`cloneForReplay(env, content)`（T5）== bridge_forward 调用（T8）。一致。
