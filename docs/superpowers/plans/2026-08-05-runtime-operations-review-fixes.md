# Runtime Operations Review Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 runtime operations review 中发现的 Admin API 地址、运行实例配置路径、终态结果完成和权限计划解析问题，并为每项行为保留回归测试。

**Architecture:** Runtime CLI 使用 Admin server 的独立地址，并在用户未显式指定配置时复用运行中 Gateway 的 PID state 配置路径。平台终态异步写入在 write loop 退出时也必须向调用方完成结果通知。EffectiveRuntimePlan 复用实际权限来源，确保诊断计划与 Worker 的权限边界一致。

**Tech Stack:** Go、Cobra、net/http、SQLite-backed session metadata、testify/require、Go race detector。

## Global Constraints

- 所有 shell 命令使用 `rtk` 前缀。
- 先写能复现问题的失败测试，再写最小生产修复。
- 终态 `done`/`error` 结果不得永久阻塞；Gateway 的终态事件不能丢失。
- 不保存 prompt、metadata value、凭证或原始 Worker 错误。
- 修改 Go 文件后运行 gofmt，并运行受影响包的 `go test -race`。

---

### Task 1: Runtime CLI 连接正确的 Admin server

**Files:**
- Modify: `cmd/hotplex/runtime_cmd.go:76-108`
- Test: `cmd/hotplex/runtime_cmd_test.go`

**Interfaces:**
- Consumes: `config.Config.Admin.Addr` and existing fence Admin API client.
- Produces: `newFenceAdminClient` targeting the dedicated Admin listener.

- [x] **Step 1: Write the failing test**

Add a test that loads a temporary config with distinct `gateway.addr` and `admin.addr`, calls the client constructor, and asserts `baseURL` uses the Admin address.

- [x] **Step 2: Run the focused test to verify it fails**

Run: `rtk go test ./cmd/hotplex -run TestNewFenceAdminClient_UsesAdminAddress -count=1`

Expected: FAIL because the current constructor uses `cfg.Gateway.Addr`.

- [x] **Step 3: Implement the minimal fix**

Resolve the host and port from `cfg.Admin.Addr`, preserving the existing default and token behavior.

- [x] **Step 4: Run the focused test to verify it passes**

Run: `rtk go test ./cmd/hotplex -run TestNewFenceAdminClient_UsesAdminAddress -count=1`

Expected: PASS.

### Task 2: Runtime CLI follows the running Gateway configuration

**Files:**
- Modify: `cmd/hotplex/runtime_cmd.go:76-85`
- Test: `cmd/hotplex/runtime_cmd_test.go`

**Interfaces:**
- Consumes: existing `gatewayConfigPath()`/PID state behavior used by CLI Admin clients.
- Produces: default-config invocations that read the running instance's config while explicit `--config` remains authoritative.

- [x] **Step 1: Write the failing test**

Add a test that writes a temporary Gateway state with a custom config path and asserts the constructor uses that path when the requested path is `config.DefaultConfigPath`, while an explicit path is not replaced.

- [x] **Step 2: Run the focused test to verify it fails**

Run: `rtk go test ./cmd/hotplex -run TestNewFenceAdminClient_UsesRunningConfigPath -count=1`

Expected: FAIL because the constructor always loads the passed default path.

- [x] **Step 3: Implement the minimal fix**

Before loading the config, resolve the running Gateway state only for the default/empty config path, matching the established Cron CLI behavior.

- [x] **Step 4: Run the focused test to verify it passes**

Run: `rtk go test ./cmd/hotplex -run TestNewFenceAdminClient_UsesRunningConfigPath -count=1`

Expected: PASS.

### Task 3: Complete async terminal results when the write loop exits

**Files:**
- Modify: `internal/gateway/platform_writer.go:188-198`
- Test: `internal/gateway/hub_test.go`

**Interfaces:**
- Consumes: `pcEntry.done` and per-terminal buffered result channels.
- Produces: exactly one terminal result for every successfully enqueued terminal write, including write-loop panic/exit paths.

- [x] **Step 1: Write the failing test**

Add a panic-producing `PlatformConn` test double and a Hub test that queues a panicking write before a terminal event; assert `SendToSession` returns an error within a bounded context instead of waiting indefinitely.

- [x] **Step 2: Run the focused test to verify it fails**

Run: `rtk go test ./internal/gateway -run TestHub_TerminalWriteCompletesWhenWriterExits -count=1`

Expected: FAIL or timeout because the `e.done` guard branch currently sends no result.

- [x] **Step 3: Implement the minimal fix**

Send a stable `platform conn closed` error to the buffered result channel when `e.done` wins, while retaining the one-shot/nonblocking behavior.

- [x] **Step 4: Run the focused test to verify it passes**

Run: `rtk go test ./internal/gateway -run TestHub_TerminalWriteCompletesWhenWriterExits -count=1`

Expected: PASS without leaked goroutines.

### Task 4: Include effective permission configuration in runtime plans

**Files:**
- Modify: `internal/agentspec/resolve.go`, `internal/agentspec/plan.go`
- Test: `internal/agentspec/plan_test.go`

**Interfaces:**
- Consumes: configured default permission mode and existing init/workspace precedence inputs.
- Produces: plan permission summary, source references, and hash that change when the effective configured ceiling changes.

- [x] **Step 1: Write the failing tests**

Add resolver tests for a workspace with no explicit override and a configured `worker.default_permission_mode`, plus a checker/admin projection assertion that the resolved permission is present and sourced from configuration.

- [x] **Step 2: Run the focused tests to verify they fail**

Run: `rtk go test ./internal/agentspec ./internal/admin ./internal/cli/checkers -run 'Test.*Permission|Test.*Plan' -count=1`

Expected: FAIL because permission currently remains empty unless passed as init metadata or workspace input.

- [x] **Step 3: Implement the minimal fix**

Extend the pure resolver input or its config-resolution helper to carry the same configured permission source used by the Bridge, without changing authoritative legacy dispatch. Pass the resolved value through the Admin and doctor plan callers.

- [x] **Step 4: Run the focused tests to verify they pass**

Run: `rtk go test ./internal/agentspec ./internal/admin ./internal/cli/checkers -run 'Test.*Permission|Test.*Plan' -count=1`

Expected: PASS.

### Task 5: Full verification and delivery

**Files:**
- Verify all changed files and generated status; no unrelated files.

- [x] **Step 1: Format changed Go files**

Run: `rtk gofmt -w cmd/hotplex/runtime_cmd.go cmd/hotplex/runtime_cmd_test.go internal/gateway/platform_writer.go internal/gateway/hub_test.go internal/agentspec/resolve.go internal/agentspec/plan.go internal/agentspec/plan_test.go internal/admin/runtime_plan.go internal/admin/runtime_plan_test.go internal/cli/checkers/effective_plan.go internal/cli/checkers/effective_plan_test.go`

- [x] **Step 2: Run affected package tests with race detection**

Run: `rtk go test -race ./internal/admin ./internal/execution ./internal/agentspec ./internal/gateway ./cmd/hotplex ./internal/cli/checkers`

- [x] **Step 3: Run repository checks relevant to the patch**

Run: `rtk git diff --check` and `rtk go test -race ./internal/messaging/feishu ./internal/messaging ./internal/observability ./internal/worker`

- [x] **Step 4: Inspect the final diff and status**

Run: `rtk git diff --stat`, `rtk git diff`, and `rtk git status --short --branch`; confirm only the intended fixes, tests, and plan are present.

- [x] **Step 5: Commit the verified fix**

Stage only the intended files and create a Conventional Commit with a `fix` type.
