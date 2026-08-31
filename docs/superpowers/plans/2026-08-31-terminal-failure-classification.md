# Terminal Failure Classification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prevent failed and stopped turns from being shown as an empty successful Agent reply while preserving the true error and legitimate empty-success fallback.

**Architecture:** Resolve a worker terminal outcome before emitting user-visible fallbacks. The Bridge keeps a separate real-content signal for retry decisions and only synthesizes success fallbacks for successful Done events. Feishu uses Done metadata to avoid converting a user stop into an empty-success warning.

**Tech Stack:** Go, testify/require, existing Gateway Hub and Feishu streaming-card tests.

**Spec:** User-approved terminal failure analysis from 2026-08-31.

## Global Constraints

- Preserve the AEP `DoneData` wire format and existing event ordering guarantees.
- Keep `fc.turnText` history backfill, but never use synthesized text as real worker output.
- Use table-driven/testify tests with `t.Parallel()` where safe; no sleeps for asynchronous assertions.
- Do not change service configuration, credentials, or runtime state.

---

### Task 1: Bridge terminal-outcome regression coverage

**Files:**

- Modify: `internal/gateway/bridge_forward_fallback_test.go`
- Modify: `internal/gateway/bridge_forward.go`

**Interfaces:**

- Consumes: `processForwardedEvent(env, worker, forwardOpts, forwardContext)`.
- Produces: failed `Error → Done{Success:false}` turns display the real error once and never call the empty-success fallback; successful empty turns still synthesize the retryable warning.

- [x] **Step 1: Write failing tests**

```go
// Error followed by Done(false), with retry disabled, must emit Error and no
// events.Message containing messaging.FormatEmptySuccess().
// Done(false) without Error must not emit the empty-success warning.
```

- [x] **Step 2: Run the focused Gateway test to verify failure**

Run: `go test ./internal/gateway -run TestBridge_FailedDone -count=1`

Expected: FAIL because `maybeSendDoneFallback` currently emits the empty-success message and the terminal fence drops the buffered Error.

- [x] **Step 3: Implement terminal classification before side effects**

```go
done, isDone := asDoneData(env.Event.Data)
failed := isDone && !done.Success
// Do not synthesize success text for failed Done events.
// Decide retry before terminal emission; when no retry is selected, send the
// buffered Error as the single user-visible failure terminal.
```

- [x] **Step 4: Re-run focused Gateway tests**

Run: `go test ./internal/gateway -run 'TestBridge_FailedDone|TestMaybeSendDoneFallback' -count=1 -race -shuffle=on`

Expected: PASS.

### Task 2: Preserve real content semantics

**Files:**

- Modify: `internal/gateway/bridge_forward.go`
- Modify: `internal/gateway/bridge_forward_fallback_test.go`

**Interfaces:**

- Consumes: `extractMessageContent(env)` and `forwardContext.turnText`.
- Produces: typed `MessageData` and pointer payloads count as real assistant content; synthesized fallback text cannot change retry eligibility.

- [x] **Step 1: Write failing tests**

```go
// A typed events.MessageData{Content: "answer"} followed by Done(true)
// emits only the answer, not FormatEmptySuccess().
```

- [x] **Step 2: Run the focused test to verify failure**

Run: `go test ./internal/gateway -run TestExtractMessageContent_MessageData -count=1`

Expected: FAIL because `extractMessageContent` only accepts `MessageDeltaData` and maps.

- [x] **Step 3: Implement type-complete extraction and independent real-content state**

```go
case events.MessageData:
    return d.Content
case *events.MessageData:
    if d != nil { return d.Content }
case *events.MessageDeltaData:
    if d != nil { return d.Content }
```

- [x] **Step 4: Re-run focused Gateway tests**

Run: `go test ./internal/gateway -run 'TestBridge_FailedDone|TestExtractMessageContent|TestMaybeSendDoneFallback' -count=1 -race -shuffle=on`

Expected: PASS.

### Task 3: Feishu terminal semantics

**Files:**

- Modify: `internal/messaging/feishu/conn.go`
- Modify: `internal/messaging/feishu/streaming_empty_success_test.go`

**Interfaces:**

- Consumes: `events.DoneData.Reason` and an active `StreamingCardController` placeholder.
- Produces: `stopped_by_user` closes the stream without the empty-success warning; genuine successful empty turns retain the retryable warning.

- [x] **Step 1: Write a failing stopped-turn test**

```go
// Feed DoneData{Reason: "stopped_by_user"} to a connection with an empty
// active placeholder and assert the final content does not contain
// "未收到可展示".
```

- [x] **Step 2: Run the focused Feishu test to verify failure**

Run: `go test ./internal/messaging/feishu -run Test.*Stopped -count=1`

Expected: FAIL because `handleDone` closes the active placeholder through the generic empty-success backstop.

- [x] **Step 3: Pass a non-success terminal result to the close path**

```go
// On stopped_by_user, clear the placeholder before Close so Close does not
// synthesize a successful-empty response.
```

- [x] **Step 4: Re-run focused Feishu tests**

Run: `go test ./internal/messaging/feishu -run 'Test.*Stopped|TestStreamingCardController_Close_PlaceholderEmptySuccess' -count=1 -race -shuffle=on`

Expected: PASS.

### Task 4: Final verification and review

**Files:**

- Verify: `internal/gateway/bridge_forward.go`
- Verify: `internal/messaging/feishu/conn.go`

- [x] **Step 1: Format changed Go files**

Run: `gofmt -w internal/gateway/bridge_forward.go internal/gateway/bridge_forward_fallback_test.go internal/messaging/feishu/conn.go internal/messaging/feishu/streaming_empty_success_test.go`

- [x] **Step 2: Run package suites**

Run: `go test ./internal/gateway ./internal/messaging/feishu -count=1 -race -shuffle=on`

Expected: PASS.

- [x] **Step 3: Run repository quality gate**

Run: `make quality`

Expected: PASS.

- [x] **Step 4: Inspect the final diff and commit one focused bug-fix change**

Run: `git diff --check && git diff --staged && git status --short`

Expected: only the four implementation/test files and this plan; no credentials or generated artifacts.
