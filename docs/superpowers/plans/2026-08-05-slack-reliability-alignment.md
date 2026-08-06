# Slack 可靠性对齐 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Slack 的媒体摄取、消息去重和终态投递对齐到已验证的飞书可靠性基线，同时保持 Slack 的 SDK/展示语义，不重放已经执行的 Worker turn。

**Architecture:** 媒体层以“声明大小预检 + 写入过程硬上限 + 临时文件原子替换”形成双保险；入口层使用带 generation 的 dedup handle，仅在请求已被本地接受时提交；终态层同步关闭 streaming writer，在同一个有界 context 内尝试短静态兜底，并复用低基数 terminal 指标。

**Tech Stack:** Go、Slack Go SDK、testify/require、OpenTelemetry、`httptest.Server`。

## Frozen Semantics

- `mediaMaxSize` 固定为 `10 * 1024 * 1024` bytes；恰好 10 MiB 成功，10 MiB + 1 byte 失败。
- `MediaInfo.Size` 只做快速预检，不是安全边界；Slack 返回的实际字节数必须再次受限。
- 媒体目录权限为 `0700`，最终文件权限为 `0600`；Windows 只验证内容和清理，不断言 POSIX mode。
- 下载失败、超限、close、rename 任一步失败时不得留下最终路径或部分临时文件。
- dedup 在现有 ConvertMessage 与 access gate 之后建立；unsupported conversion/gate rejection 本来就不记录。ResolveMentions 后为空、命令未完成和 `HandleTextMessage` 返回错误时 rollback。正常消息投递成功、help/control/worker 命令已被消费、interaction response 已被消费时保留 dedup。
- 媒体下载失败当前会降级为受控占位文本并继续处理；只要后续消息成功投递，就保留 dedup，禁止因附件失败重放整个 turn。
- terminal 展示失败只上浮错误和尝试一次短静态兜底，不回滚输入、不重新调用 Worker。
- 不新增高基数指标；复用 `StreamingTerminalFailures()` 与 `PlatformTerminalFallback()`，新增属性只允许 `platform="slack"` 和受控 `fallback_result`。

---

### Task 1: 建立 Slack 媒体安全边界

**Files:**
- Modify: `internal/messaging/slack/converter.go`
- Create: `internal/messaging/slack/converter_test.go`

**Interfaces:**
- Produces: `ErrMediaTooLarge`、`boundedMediaWriter`、10 MiB 下载硬上限、原子落盘。
- Consumes: Slack client 的 `GetFileContext(ctx, url, io.Writer)`。

- [ ] **Step 1: 写大小边界红测**

在 `converter_test.go` 增加 table-driven 测试，expected 使用手工 literal：

```go
func TestDownloadMediaBytes_EnforcesActualTenMiBLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		declared  int
		actual    int
		wantError bool
	}{
		{name: "exact limit", declared: 10 * 1024 * 1024, actual: 10 * 1024 * 1024},
		{name: "one byte over", declared: 10*1024*1024 + 1, actual: 10*1024*1024 + 1, wantError: true},
		{name: "lying metadata", declared: 1, actual: 10*1024*1024 + 1, wantError: true},
	}
	// fake GetFileContext writes actual bytes in deterministic chunks.
}
```

另写文件路径测试：实际内容 10 MiB + 1 时返回 `errors.Is(err, ErrMediaTooLarge)`，最终 path 不存在，目录中不存在 `.hotplex-media-*`。

再写 `TestMediaFilePath_RejectsTraversal`：`FileID="../../escape"`、空 ID、未知 `Type` 都返回 error；最终 path 必须始终位于 `MediaPathPrefix` 下。测试会临时替换 package global `MediaPathPrefix=t.TempDir()`，因此该测试及所有落盘测试不得 `t.Parallel()`，cleanup 恢复原值。

- [ ] **Step 2: 验证 RED**

Run: `rtk go test ./internal/messaging/slack -run 'TestDownloadMedia(Bytes)?_EnforcesActualTenMiBLimit' -count=1`

Expected: FAIL；当前实现会接受 `declared=1` 的超限内容，或留下已创建文件。

- [ ] **Step 3: 实现受限 writer**

在 `converter.go` 定义：

```go
var ErrMediaTooLarge = errors.New("slack: media exceeds 10 MiB")

const mediaMaxSize = 10 * 1024 * 1024

type boundedMediaWriter struct {
	dst       io.Writer
	written   int64
	maxBytes  int64
}
```

`Write` 必须允许总量恰好等于 `maxBytes`；若本次 write 会越界，只把剩余许可字节写入 `dst`，然后返回 `ErrMediaTooLarge`。不得把超限尾部写入磁盘或 buffer。Slack SDK 接口只提供 `io.Writer`，因此此 writer 是 `io.LimitReader(max+1)` 的等价实现。

- [ ] **Step 4: 实现安全落盘**

`downloadMedia` 按以下固定顺序实现：

1. `m.Size > mediaMaxSize` 时快速返回 `ErrMediaTooLarge`；
2. `os.MkdirAll(dir, 0o700)`，并对既有目录执行 `os.Chmod(dir, 0o700)`；
3. `os.CreateTemp(dir, ".hotplex-media-*")`，立即 `Chmod(0o600)`；
4. 使用 `boundedMediaWriter` 调用 `GetFileContext`；
5. 成功后 `Close`，再 `os.Rename(tempName, path)`；
6. defer 中只要 `committed=false` 就关闭并删除 temp；
7. 返回前 `os.Chmod(path, 0o600)`。

`downloadMediaBytes` 使用同一个 `boundedMediaWriter` 写入 `bytes.Buffer`。`saveMediaBytes` 先检查 `len(data)`，再复用原子临时文件流程，不能直接 `os.WriteFile(path, ...)`。

把 `mediaFilePath` 改为返回 `(dir, path string, err error)`：Type 只允许 `image|audio|video|document|file`，FileID 必须非空且 `filepath.Base(id)==id`、不等于 `.`/`..`；未知 MIME extension 使用 `.bin`，不再把 FileID 拼成 extension。所有 caller 必须处理 error。

- [ ] **Step 5: 验证权限和失败清理**

新增 `TestSaveMediaBytes_PrivatePermissionsAndAtomicCleanup`。Unix 断言 `dirInfo.Mode().Perm()==0o700`、`fileInfo.Mode().Perm()==0o600`；Windows 用 `if runtime.GOOS == "windows" { return }` 只跳过 mode 断言，不跳过该组合测试。

Run: `rtk go test ./internal/messaging/slack -run 'Test(Download|Save)Media' -count=1 -race`

Mutation check: 删除实际字节限制时 “lying metadata” 必须失败；把 `0600` 改成 `0644` 时 Unix 权限测试必须失败；移除 FileID 校验时 traversal 行必须失败。

- [ ] **Step 6: Commit**

```bash
rtk git add internal/messaging/slack/converter.go internal/messaging/slack/converter_test.go
rtk git commit -m "fix(slack): bound media ingestion"
```

### Task 2: 把 Slack dedup 改为条件提交

**Files:**
- Modify: `internal/messaging/slack/adapter.go`
- Modify: `internal/messaging/slack/adapter_test.go`
- Modify: `internal/messaging/slack/e2e_test.go`

**Interfaces:**
- Consumes: `Dedup.TryRecordWithHandle`、`Dedup.Rollback`；保持当前 generation-safe rollback 语义。
- Produces: `handleMessageEvent` 的“失败可重试、成功不重复”合同。

- [ ] **Step 1: 写 Bridge 投递失败红测**

在 `e2e_test.go` 使用真实 `handleMessageEvent`，第一次让 Bridge/handler 返回受控错误，第二次发送相同 `ClientMsgID` 时恢复成功。断言第二次到达 `HandleTextMessage`/Bridge，Worker 输入总计恰好一次成功；不得直接调用 Dedup 代替 raw event。

Run: `rtk go test ./internal/messaging/slack -run '^TestHandleMessageEvent_RollsBackDedupAfterDeliveryFailure$' -count=1 -race`

Expected: FAIL；当前第一次 `TryRecord` 已永久占用 ID。

- [ ] **Step 2: 写空输入与并发 generation 红测**

- bot mention 解析后文本为空且无媒体：第一次结束后 `a.Dedup.TryRecord(id)` 必须为 true；
- 旧 handle rollback 与同 ID 的新 generation 交错时，旧 rollback 不得删除新记录。用两个 channel 精确控制顺序，禁止 `time.Sleep`。

Run: `rtk go test ./internal/messaging/slack -run 'TestHandleMessageEvent_(RollsBackEmptyMessage|StaleRollbackCannotEraseNewGeneration)' -count=1 -race`

- [ ] **Step 3: 实现单一提交点**

将当前：

```go
if !a.Dedup.TryRecord(platformMsgID) { return }
```

替换为 `TryRecordWithHandle`。紧接其后建立：

```go
dedupHandle, recorded := a.Dedup.TryRecordWithHandle(platformMsgID)
if !recorded { return }
accepted := false
defer func() {
	if !accepted { a.Dedup.Rollback(dedupHandle) }
}()
```

所有正常消费分支在 `return` 前设置 `accepted=true`：help、control、worker command、pending interaction，以及普通 `HandleTextMessage` 返回 nil。ResolveMentions 后为空、adapter closing、命令处理无法完成和普通投递错误保持 `accepted=false`。

当前 `HandleTextMessage` 在 `a.Bridge()==nil` 时日志后返回 nil，会把“未投递”误判成成功。把该分支改为 `fmt.Errorf("slack: bridge not configured")`，由上层 rollback；不要新增公开 sentinel。测试必须覆盖 bridge=nil 后，相同 ID 在配置 bridge 后可重试。

命令 helper 当前不返回 error 的分支不得擅自改变公开签名；以“已进入并完成本地命令处理”为 accepted。若 reviewer 发现 helper 内部错误完全不可观察，另建 Issue，不在本切片扩大接口。

- [ ] **Step 4: 验证 GREEN 和成功去重**

新增 `TestHandleMessageEvent_KeepsDedupAfterSuccessfulDelivery`：相同 raw event 连续两次，Worker input 只一次。

Run: `rtk go test ./internal/messaging/slack -run 'TestHandleMessageEvent_' -count=1 -race`

Run: `rtk go test ./internal/messaging/slack -count=1 -race`

Mutation check: 删除错误路径 rollback 时 retry 测试失败；把成功分支也 rollback 时 duplicate 测试失败。

- [ ] **Step 5: Commit**

```bash
rtk git add internal/messaging/slack/adapter.go internal/messaging/slack/adapter_test.go internal/messaging/slack/e2e_test.go
rtk git commit -m "fix(slack): rollback rejected message dedup"
```

### Task 3: 传播 Slack terminal close/send 错误

**Files:**
- Modify: `internal/messaging/slack/adapter.go`
- Modify: `internal/messaging/slack/stream.go`
- Modify: `internal/messaging/slack/conn_events.go`
- Modify: `internal/messaging/slack/adapter_test.go`
- Modify: `internal/messaging/slack/stream_test.go`
- Create: `internal/messaging/slack/conn_events_test.go`
- Modify: `internal/observability/instruments.go`
- Modify: `docs/reference/metrics.md`

**Interfaces:**
- Produces: typed `StreamTerminalError`、`CloseContext(ctx) error`、`closeStreamWriter(ctx) error`、`terminalDeliveryContext`、`handleTerminalDeliveryError`。
- Consumes: `StreamingTerminalFailures()`、`PlatformTerminalFallback()`。

- [ ] **Step 1: 让 close 错误可测试**

为 `SlackConn` 增加 package-private callback 只作为测试 seam 会扩大生产状态，不采用该做法。改为把 `NativeStreamingWriter` 抽象成最小 package-private interface：

```go
type streamContentCloser interface {
	Content() string
	Write([]byte) (int, error)
	Close() error
	CloseContext(context.Context) error
}
```

`SlackConn.streamWriter` 改为该接口；测试 fake 可以稳定返回 sentinel close error。这个接口不得导出。`NativeStreamingWriter.Close()` 保留 `io.WriteCloser` 兼容，内部创建默认 10 秒 context 后调用 `CloseContext`；所有 Gateway terminal 路径调用 `CloseContext`，使 close 与外层 fallback 共享 deadline。

- [ ] **Step 2: 写 close/兜底红测**

在新 `conn_events_test.go` 覆盖：

1. close 成功：无静态 fallback，返回 nil；
2. close 失败且 `ContentPresented=false`：发送一次固定短文本 `⚠️ Reply delivery failed. Please try again.`，返回原 close error；
3. close 失败且 `ContentPresented=true`：不重复完整文本、不发送 fallback，返回 close error；
4. close 与 fallback 都失败：返回 `errors.Join(closeErr, fallbackErr)`，两个 `errors.Is` 都为 true；
5. error event 的 PostMessage 失败同步返回，不再由 goroutine 吞掉；
6. caller context 无 deadline 时 close 与 fallback 共享一个 5s deadline；已有 deadline 时不得延长。

测试 client 记录调用和 context deadline，使用 channel/直接调用，不等待后台 goroutine。

另在 `stream_test.go` 写：StopStream error 必须由 `CloseContext` 返回；final flush 有 failed chunk 时返回 `ContentPresented=false`；只发生 stop decoration error 且所有 chunks 已展示时返回 `ContentPresented=true`；旧的完整内容 PostMessage fallback 不再由 writer 私自发送。

Run: `rtk go test ./internal/messaging/slack -run 'TestNativeStreamingWriter_CloseContext|^TestSlackConn_Handle(Done|Error)_' -count=1`

Expected: FAIL；当前 `closeStreamWriter` 丢弃错误，`handleError` 异步返回 nil。

- [ ] **Step 3: 实现终态错误路径**

- 新建 `StreamTerminalError{Cause error; ContentPresented bool}`，实现 `Error()`/`Unwrap()`；`NativeStreamingWriter.CloseContext` 汇总 final flush/StopStream 错误并返回 typed error；删除其内部“重发完整内容但吞掉发送错误”的逻辑；
- `closeStreamWriter(ctx) error` 在锁外调用 `CloseContext`，无 writer 返回 nil，仍保持幂等；
- `handleDone` 先读取 `Content()`，同步 close；close error 交给 `handleTerminalDeliveryError`；
- `handleError` 同步 close，然后同步发送受控错误文本；使用 `errors.Join` 保留两类失败；
- `terminalDeliveryContext` 复制飞书的边界：已有 deadline 只 `WithCancel`，否则 `WithTimeout(..., 5*time.Second)`；
- terminal fallback 只发固定短文本，绝不包含完整回答或原始 Worker error；
- `SlackConn.Close`、`handleInteraction`、`handleTextControlCommand` 等调用点显式处理返回值：已处于不可返回接口时结构化 Warn + counter；可返回接口时上浮。不得用 `_ = c.closeStreamWriter(...)`。

- [ ] **Step 4: 复用指标并收窄标签**

`StreamingTerminalFailures().Add` 使用 `platform="slack"` 与 `fallback_result="sent|failed|skipped_body_presented"`；成功发送一次 fallback 时同时增加 `PlatformTerminalFallback()`。更新 `instruments.go` 注释和 `metrics.md`，把指标描述从飞书 card 专属扩为平台 streaming terminal；不改 metric name。

- [ ] **Step 5: 验证 GREEN、race 和组合矩阵**

Run: `rtk go test ./internal/messaging/slack -run 'TestSlackConn_(closeStreamWriter|HandleDone|HandleError)' -count=1 -race`

Run: `rtk go test ./internal/observability ./internal/messaging/slack -count=1 -race`

Run: `rtk make test-contract-matrix`

Expected: Slack 四组合 S-C/S-O/S-X/S-A 均 PASS；terminal fault 不增加 Worker input 次数。

- [ ] **Step 6: Commit**

```bash
rtk git add internal/messaging/slack/adapter.go internal/messaging/slack/stream.go internal/messaging/slack/conn_events.go internal/messaging/slack/adapter_test.go internal/messaging/slack/stream_test.go internal/messaging/slack/conn_events_test.go internal/observability/instruments.go docs/reference/metrics.md
rtk git commit -m "fix(slack): surface terminal delivery failures"
```

### Task 4: Slack 子计划回归门禁

- [ ] `rtk go test ./internal/messaging/slack -count=1 -race`
- [ ] `rtk go test ./internal/observability -count=1 -race`
- [ ] `rtk make test-contract-matrix`
- [ ] `rtk git diff --check`
- [ ] reviewer 逐条核对：声明大小不能绕过实际限制；失败 rollback、成功提交；terminal failure 不重放 Worker turn；无 `time.Sleep`。

Expected: 0 fail、0 race；四个 Slack 组合全部执行，无 skip。
