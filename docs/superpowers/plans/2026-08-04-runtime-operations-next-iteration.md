# HotPlex Runtime Operations Next Iteration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在一个 8–10 个工程日迭代内完成 #877 的 fenced execution operator recovery，并以只读/影子方式把现有 AgentSpec 收敛为 #946 定义的 `EffectiveRuntimePlan`。

**Architecture:** #877 只扩展现有 `internal/execution` 状态机和 Admin 授权边界，不创建第二套 execution store；operator action 通过 fence version 条件更新，旧输入永不自动重投。#946 复用 `internal/agentspec.Resolver` 作为唯一解析器，先产出 redacted canonical plan/hash 并接入 WS、REST、doctor 和 Admin diagnostics 的 shadow/read-only 路径，暂不实施 #867 的 strict env allowlist、#947 EffectLedger 或 #868 Cockpit。

**Tech Stack:** Go、Cobra、`net/http`、SQLite、PostgreSQL、`testify/require`、AEP v1、OpenTelemetry/Prometheus、现有 Admin Bearer+scope middleware。

## Global Constraints

- 所有 durable input 仍只保存 SHA-256 payload 指纹，不保存 prompt、metadata value、secret、credential、raw worker error 或完整 tool args。
- `accepted` 只能前进到 `delivered` / `unknown` / `failed`；`unknown` 不因 timeout、时间经过或 operator action 自动变成 `completed` / `delivered`。
- #877 不按固定时长自动清除 fence；operator action 必须带 actor、target、reason、decision、evidence ref、timestamp，并经过现有 workspace/session/admin authorization。
- 清除 fence 不重投旧 input；新 input 必须使用新 client message ID 创建新 execution；late completion 只能收敛同一 `worker_run_id` / execution。
- SQLite 与 PostgreSQL migration、条件更新、跨实例竞态和故障测试必须成对验证。
- AEP 新事件只做增量兼容；同步 Go SDK、TypeScript/Python/Java 示例 SDK、协议文档和双向测试。
- 低基数指标不得使用 plan hash、workspace、provider ref 或 evidence ref 作为 label；敏感值只进入脱敏 log/trace/audit correlation。
- 非 main 分支本地验证通过后直接 commit + push；本迭代不包含 merge、release 或生产配置改动。

---

## 迭代范围与退出条件

| 优先级 | 切片 | Issue | 本迭代终态 |
| --- | --- | --- | --- |
| P0 | Fenced execution operator action | #877 | 可 inspect、resolve、abandon；条件更新防竞态；Admin audit + runtime event；CLI 可执行；新输入行为可解释 |
| P1 | EffectiveRuntimePlan first vertical slice | #946 | `internal/agentspec` 产出 redacted canonical plan/hash；WS/REST 共用输入；doctor 与 Admin 提供只读/影子诊断；四 Worker 有 planned/unknown evidence |
| P2（不做） | Explicit env allowlist/isolation profiles | #867 | 仅保留依赖关系，不在本迭代改 `BuildEnv` 行为 |
| P2（不做） | Gateway-owned EffectLedger | #947 | 不新增表、不改 Cron/Webhook/message delivery |
| P2（不做） | Execution Cockpit / Queue | #851/#868 | 不新增 timeline、FIFO 或第二套查询事实 |

进入迭代的前置条件是当前 `main` 的 #950 审计修复、#878 durable ingress、#847 AgentSpec first cut 和 approved `Runtime Operations Contract` 均保持绿。退出条件是受影响模块 `-race -count=1`、`make check`、`make docs-build` 通过，且 SQLite/PostgreSQL、权限拒绝、redaction、late completion 和跨实例条件更新均有证据。

## 文件变更地图

### #877

- Modify: `internal/execution/store.go` — fence inspection/action 请求、状态和 Store 接口。
- Modify: `internal/execution/sql_store.go` — fence token/version、conditional resolve/abandon 和扫描字段。
- Create: `internal/session/sql/migrations/031_execution_fence_operator.sql`。
- Create: `internal/session/sql/migrations-postgres/031_execution_fence_operator.pg.sql`。
- Modify/Test: `internal/execution/store_test.go`, `internal/execution/fault_injection_test.go`, `internal/execution/multi_instance_test.go`, `internal/execution/multi_instance_pg_test.go`。
- Modify: `internal/admin/admin.go`, `internal/admin/handlers.go`, `internal/admin/models.go` — runtime scopes、provider、JSON response 和 handlers。
- Modify/Test: `internal/admin/handlers_test.go`, `internal/admin/middleware_test.go`。
- Modify: `cmd/hotplex/routes.go`, `cmd/hotplex/admin_adapters.go` — route and dependency wiring。
- Create/Modify/Test: `cmd/hotplex/runtime_cmd.go`, `cmd/hotplex/runtime_cmd_test.go`, `cmd/hotplex/main.go` — `hotplex runtime fences` CLI。
- Modify: `pkg/events/events.go`, `pkg/events/runtime_execution_test.go`, `internal/eventstore/collector.go` — only if the approved event vocabulary needs a new additive operator-action kind。

### #946

- Create/Modify/Test: `internal/agentspec/plan.go`, `internal/agentspec/plan_test.go` — `EffectiveRuntimePlan`, redaction, canonicalization and SHA-256 hash。
- Modify/Test: `internal/agentspec/resolve.go`, `internal/agentspec/resolver_test.go` — one resolver path and fail-closed validation。
- Modify/Test: `internal/gateway/agentspec.go`, `internal/gateway/api.go`, `internal/gateway/agentspec_test.go`, `internal/gateway/api_test.go` — WS/REST shared input and shadow comparison。
- Create/Test: `internal/cli/checkers/runtime_plan.go`, `internal/cli/checkers/runtime_plan_test.go` — `runtime.effective_plan` checker。
- Modify: `cmd/hotplex/doctor.go` only if category/help output requires explicit runtime-plan naming。
- Modify: `cmd/hotplex/routes.go`, `internal/admin/handlers.go`, `internal/admin/handlers_test.go` — redacted read-only session plan diagnostic。
- Modify: `docs/v2/ITERATION-NEXT.md`, `docs/v2/ROADMAP.md`, `docs/v2/IMPLEMENTATION-ROADMAP.md`, `docs/reference/admin-api.md`, `docs/reference/cli.md`, `docs/reference/metrics.md` — current iteration pointer, endpoint/CLI contract and validation evidence。

### Task 1: Freeze the #877 fence-action data contract

**Files:**
- Modify: `internal/execution/store.go`
- Modify: `internal/execution/sql_store.go`
- Create: `internal/session/sql/migrations/031_execution_fence_operator.sql`
- Create: `internal/session/sql/migrations-postgres/031_execution_fence_operator.pg.sql`
- Test: `internal/execution/store_test.go`, `internal/execution/fault_injection_test.go`

**Interfaces:**
- Consumes: existing `Record.FenceReason`, `RuntimeStatus`, `FenceBySession` and `ClearFenceAfterFreshStart`.
- Produces: `FenceVersion int64`, `FenceCreatedAt *int64`, `FenceDecision` (`resolve` or `abandon`), `FenceActionRequest{ExecutionID, ExpectedFenceVersion, Decision}` and `ApplyFenceDecision(ctx, request) (*Record, error)`.

- [ ] **Step 1: Write the failing store contract tests.** Add table-driven cases for `resolve`, `abandon`, empty/unknown decision, stale `ExpectedFenceVersion`, repeated same action, and a fenced record with no sensitive fields.

```go
func TestApplyFenceDecision_RejectsStaleFenceVersion(t *testing.T) {

	t.Parallel()
	store := newTestSQLStore(t)
	rec := acceptUnknownAndFence(t, store, "fence-v1")

	_, err := store.ApplyFenceDecision(t.Context(), execution.FenceActionRequest{
		ExecutionID:         rec.ExecutionID,
		ExpectedFenceVersion: rec.FenceVersion - 1,
		Decision:             execution.FenceDecisionResolve,
	})
	require.ErrorIs(t, err, execution.ErrFenceConflict)
}
```

- [ ] **Step 2: Run the focused tests and verify the new contract fails.**

Run: `rtk go test ./internal/execution -run 'TestApplyFenceDecision|TestFence' -count=1`

Expected: FAIL because the request type, migration columns and conditional method do not exist.

- [ ] **Step 3: Add the paired migration.** Add `fence_version INTEGER/BIGINT NOT NULL DEFAULT 0` and `fence_created_at INTEGER/BIGINT NULL`; update the fence-producing SQL to increment the version and set creation time only when entering a fence. Preserve old rows as version `0` with no timestamp.

- [ ] **Step 4: Implement the secret-free model and Store interface.** Add the decision enum, `ErrFenceConflict`, request type and record fields. Keep actor, reason and evidence outside `internal/execution`; the persistence layer receives only the conditional target and decision.

- [ ] **Step 5: Run SQLite and PostgreSQL store tests.**

Run: `rtk go test ./internal/execution ./internal/session/sql -count=1 -race`

Expected: PASS; stale requests do not mutate the record, `resolve` clears only the fence, and `abandon` marks runtime failed with a bounded enum error code.

- [ ] **Step 6: Commit the independently testable storage slice.**

Run: `rtk git add internal/execution internal/session/sql/migrations/031_execution_fence_operator.sql internal/session/sql/migrations-postgres/031_execution_fence_operator.pg.sql`

Run: `rtk git commit -m "feat(execution): add conditional fence decisions"`

### Task 2: Prove #877 cross-instance and late-completion semantics

**Files:**
- Modify/Test: `internal/execution/sql_store_test.go`, `internal/execution/fault_injection_test.go`, `internal/execution/multi_instance_test.go`, `internal/execution/multi_instance_pg_test.go`, `internal/gateway/input_execution_test.go`
- Modify: `internal/gateway/handler.go`, only if the new fence version must be carried through the existing fresh-worker path.

**Interfaces:**
- Consumes: `ApplyFenceDecision`, `FenceVersion`, existing `FinishRuntime(executionID, workerRunID, ...)`.
- Produces: verified invariants for operator action, old worker completion and new input acceptance.

- [ ] **Step 1: Add failing race tests** for two gateways applying the same expected fence version, a late `done` from the fenced worker after `resolve`, a late `done` after `abandon`, duplicate operator actions, and a restart between inspect and action.

- [ ] **Step 2: Run the race tests before implementation.**

Run: `rtk go test ./internal/execution ./internal/gateway -run 'Fence|Late|Operator' -count=1 -race`

Expected: FAIL for the new cases, while existing #878 fence tests remain green.

- [ ] **Step 3: Make the smallest conditional state transition.** `resolve` clears `fence_reason` and leaves the old runtime fact `unknown`; `abandon` clears the fence and sets `runtime_status=failed` with `OPERATOR_ABANDONED`; neither path changes delivery to `delivered` or invokes Worker input.

- [ ] **Step 4: Verify the fresh-worker path remains authoritative.** Existing `ClearFenceAfterFreshStart` must continue to require the current fence reason and fresh run ID; operator resolution must not bypass `StartFreshWorker` when the caller is using the normal automatic recovery path.

- [ ] **Step 5: Run focused and real-PostgreSQL tests.**

Run: `rtk go test ./internal/execution ./internal/gateway -count=1 -race`

Run: `rtk go test -tags pg -p 1 ./internal/execution ./internal/session/sql -count=1 -race`

Expected: exactly one operator action wins; late completion can refine only its original execution; a new client message is accepted only after the chosen fence decision releases the gate.

### Task 3: Expose authorized #877 Admin actions with audit and AEP evidence

**Files:**
- Modify: `internal/admin/admin.go`, `internal/admin/handlers.go`, `internal/admin/models.go`, `cmd/hotplex/routes.go`, `cmd/hotplex/admin_adapters.go`
- Test: `internal/admin/handlers_test.go`, `internal/admin/middleware_test.go`, `internal/gateway/api_test.go`
- Modify: `pkg/events/events.go`, `internal/eventstore/collector.go` only when an additive operator-action event is required by the existing runtime event contract.

**Interfaces:**
- Consumes: `execution.Store.ApplyFenceDecision`, `FenceBySession`, existing `AdminAPI.Middleware` actor/scope context and `AdminAudit`.
- Produces: `GET /admin/executions/fences`, `POST /admin/executions/{id}/fence-action`, scopes `runtime:read` and `runtime:write`.

- [ ] **Step 1: Write handler tests for authorization and redaction.** Cover missing scopes (`403`), missing execution (`404`), stale version (`409`), invalid decision (`400`), successful resolve/abandon, actor extraction, reason/evidence validation, and responses containing no payload/prompt/secret/raw error.

- [ ] **Step 2: Run the handler tests to verify they fail.**

Run: `rtk go test ./internal/admin ./cmd/hotplex -run 'Fence|Runtime' -count=1`

Expected: FAIL because routes, scopes and provider methods are absent.

- [ ] **Step 3: Add provider boundaries and scoped routes.** Keep `internal/admin` independent from `internal/gateway` and `internal/session`; inject a narrow `ExecutionProvider` from `cmd/hotplex`. Register both routes only in `cmd/hotplex/routes.go` under the existing Admin middleware.

- [ ] **Step 4: Implement audit and runtime evidence.** Record `actor`, action (`runtime.fence.resolve` or `runtime.fence.abandon`), target execution ID, decision, bounded reason, evidence ref and result. Emit an additive `runtime.execution.failed` only for `abandon` if the existing event contract supports it; never put reason detail, prompt, secret or raw error in the event payload.

- [ ] **Step 5: Run focused tests and inspect JSON contracts.**

Run: `rtk go test ./internal/admin ./cmd/hotplex ./pkg/events ./internal/eventstore -count=1 -race`

Expected: PASS; the endpoint returns a stable redacted response and all write paths are auditable.

### Task 4: Add the #877 CLI and diagnostics read path

**Files:**
- Create/Modify: `cmd/hotplex/runtime_cmd.go`, `cmd/hotplex/runtime_cmd_test.go`, `cmd/hotplex/main.go`
- Create/Modify: `internal/cli/checkers/runtime_fence.go`, `internal/cli/checkers/runtime_fence_test.go`
- Modify: `docs/reference/cli.md`, `docs/reference/admin-api.md`

**Interfaces:**
- Consumes: the Admin endpoint and existing `hotplex doctor` checker registry.
- Produces: `hotplex runtime fences list`, `hotplex runtime fences resolve <execution-id>`, `hotplex runtime fences abandon <execution-id>`; no direct SQL mutation from CLI.

- [ ] **Step 1: Write command tests** using an `httptest.Server`; assert list renders only redacted fields, resolve/abandon send the expected fence version and reason/evidence, `409` is reported as stale state, and no command retries a non-idempotent operator action.

- [ ] **Step 2: Implement the HTTP client and Cobra wiring.** Read endpoint, address, token and config through the existing CLI config path; register `newRuntimeCmd()` from `main.go`; use explicit confirmation for `resolve` and `abandon` flags rather than an implicit destructive action.

- [ ] **Step 3: Add a read-only doctor checker.** `runtime.fenced_executions` reports the count and oldest fence timestamp as Warn with a remediation hint; it has no `FixFunc` and never clears a fence.

- [ ] **Step 4: Run CLI and doctor tests.**

Run: `rtk go test ./cmd/hotplex ./internal/cli/... -run 'Runtime|Fence|Doctor' -count=1 -race`

Expected: PASS; the CLI is a query/action facade over Admin and the doctor path is read-only.

### Task 5: Add `EffectiveRuntimePlan` to the existing AgentSpec resolver

**Files:**
- Create/Modify/Test: `internal/agentspec/plan.go`, `internal/agentspec/plan_test.go`, `internal/agentspec/resolve.go`, `internal/agentspec/resolver_test.go`

**Interfaces:**
- Consumes: `agentspec.Input`, `AgentSpec`, existing config precedence and worker validation.
- Produces: `Resolver.ResolvePlan(in Input) (EffectiveRuntimePlan, error)`, `EffectiveRuntimePlan.Redacted() EffectiveRuntimePlanView`, and `CanonicalPlanHash(view EffectiveRuntimePlanView) string`.

- [ ] **Step 1: Write failing canonicalization tests.** Cover equivalent input ordering, WS/REST semantic equivalence, unknown worker, invalid permission, secret-free output, stable source refs, warnings/blocked reasons and planned/unknown observed state.

```go
func TestResolvePlan_EquivalentInputsHaveSameHash(t *testing.T) {

	t.Parallel()
	r := testResolver()
	a, err := r.ResolvePlan(Input{Platform: "webchat", InitMeta: InitMetadata{WorkerType: "acp"}})
	require.NoError(t, err)
	b, err := r.ResolvePlan(Input{Platform: "webchat", InitMeta: InitMetadata{WorkerType: "acp"}})
	require.NoError(t, err)
	require.Equal(t, a.PlanHash, b.PlanHash)
}
```

- [ ] **Step 2: Run the new tests before implementation.**

Run: `rtk go test ./internal/agentspec -run 'ResolvePlan|CanonicalPlan' -count=1`

Expected: FAIL because the plan value object and method do not exist.

- [ ] **Step 3: Implement the redacted plan value object.** Include version, resolver version, embedded secret-free AgentSpec fields, worker type, permission/sandbox summaries, env key names only, capability IDs, config/skill hashes, source refs, bounded warnings and blocked reasons. Do not expose resolved command values, host environment values, tokens or raw errors in the public view.

- [ ] **Step 4: Implement deterministic hashing.** Normalize nil/empty slices, sort only semantically unordered collections, marshal a fixed field-order canonical view, and compute SHA-256. Hashing must never include timestamps, process IDs, workspace paths or evidence references that change between equivalent requests.

- [ ] **Step 5: Run the resolver tests.**

Run: `rtk go test ./internal/agentspec -count=1 -race`

Expected: PASS; first-cut behavior remains compatible with existing AgentSpec tests and no secret sentinel appears in JSON or hash input.

### Task 6: Wire the #946 plan through WS/REST and read-only diagnostics

**Files:**
- Modify/Test: `internal/gateway/agentspec.go`, `internal/gateway/api.go`, `internal/gateway/agentspec_test.go`, `internal/gateway/api_test.go`
- Create/Test: `internal/cli/checkers/runtime_plan.go`, `internal/cli/checkers/runtime_plan_test.go`
- Modify/Test: `internal/admin/handlers.go`, `internal/admin/handlers_test.go`, `cmd/hotplex/routes.go`

**Interfaces:**
- Consumes: `Resolver.ResolvePlan`, existing `BuildWebChatInput`, `SessionInfo.SpecSnapshot` and Admin provider boundaries.
- Produces: shared WS/REST plan input, `GET /admin/sessions/{id}/runtime-plan`, and `runtime.effective_plan` doctor output.

- [ ] **Step 1: Add failing equivalence and endpoint tests.** Verify WS and REST with the same semantic worker/policy inputs produce the same plan hash; verify the Admin endpoint is read-only, ownership/scope protected, paginated only where needed, and returns `planned`/`unknown` when worker evidence is absent.

- [ ] **Step 2: Replace duplicate input assembly with one helper.** Keep `BuildWebChatInput` as the shared constructor; make both WS init and REST session creation call the same plan path in shadow mode. The legacy `SessionStartParams` remains authoritative until drift evidence is reviewed.

- [ ] **Step 3: Add observed bootstrap without overclaiming.** At worker start, attach worker type/backend/artifact evidence to the in-memory diagnostic projection. If evidence is unavailable, return `unknown`; do not label a configured capability `enforced`.

- [ ] **Step 4: Add the Admin read path and doctor checker.** Admin returns the redacted plan/hash and bounded warnings; doctor computes the same plan from the local config and reports blocked reasons with no auto-fix. No new database column is required in this first slice; the existing spec snapshot remains the backward-compatible persisted view.

- [ ] **Step 5: Run focused tests.**

Run: `rtk go test ./internal/agentspec ./internal/gateway ./internal/admin ./internal/cli/... -run 'AgentSpec|RuntimePlan|EffectivePlan' -count=1 -race`

Expected: PASS; no legacy WS/REST behavior changes outside the shadow projection.

### Task 7: Add the iteration documentation and evidence matrix

**Files:**
- Modify: `docs/v2/ITERATION-NEXT.md`, `docs/v2/ROADMAP.md`, `docs/v2/IMPLEMENTATION-ROADMAP.md`
- Modify: `docs/reference/admin-api.md`, `docs/reference/cli.md`, `docs/reference/metrics.md`
- Test/Verify: repository docs links, redaction examples and command snippets

- [ ] **Step 1: Replace the stale July iteration pointer.** Set the baseline to current `main`/v1.38.1, identify #877 and #946 as this iteration, and explicitly mark #867/#947/#851/#868 as deferred dependencies.

- [ ] **Step 2: Document the exact operator contract.** Include endpoint paths, scopes, request fields, `409` stale-token behavior, `resolve` versus `abandon`, no-auto-retry semantics, audit fields and redaction rules.

- [ ] **Step 3: Document the plan contract.** Include canonical hash inputs, `planned`/`unknown` evidence semantics, WS/REST equivalence, doctor/Admin read-only behavior and the fact that #867 strict environment enforcement is not implemented by this iteration.

- [ ] **Step 4: Run documentation validation.**

Run: `rtk make docs-build`

Run: `rtk git diff --check`

Expected: PASS with no broken links, stale endpoint descriptions or unbounded sensitive examples.

### Task 8: Final verification and release gate

- [ ] **Step 1: Run focused package tests.**

Run: `rtk go test ./internal/execution ./internal/admin ./internal/agentspec ./internal/gateway ./internal/cli/... ./cmd/hotplex -count=1 -race`

- [ ] **Step 2: Run PostgreSQL-specific tests serially.**

Run: `rtk go test -tags pg -p 1 ./internal/execution ./internal/session/sql -count=1 -race`

Expected: PASS when `HOTPLEX_TEST_PG_DSN` is configured; otherwise record the environment limitation and do not claim real-PG completion.

- [ ] **Step 3: Run repository quality gates.**

Run: `rtk make check`

Run: `rtk make docs-build`

Run: `rtk git diff --check`

- [ ] **Step 4: Verify outcome evidence.** Confirm the Admin route is actually registered, `hotplex runtime fences --help` exposes the commands, stale operator requests return `409`, audit rows contain the action, runtime events remain redacted, old input is not dispatched again, and WS/REST plan hashes match.

- [ ] **Step 5: Commit in reviewable slices.** Keep the storage, Admin/CLI, plan resolver, integration, and docs changes separately reviewable; do not squash until the reviewer has verified the cross-slice invariants.

## Risks and explicit stop conditions

| Risk | Stop condition | Response |
| --- | --- | --- |
| Operator action accidentally proves old execution success | Any code path writes `completed`/`delivered` from `resolve` or emits a success claim without provider evidence | Stop; revert the transition and keep the record `unknown` |
| Conditional update is weaker than the read token | Two instances can both mutate one fence version | Stop; add version predicate and repeat real-PG test before continuing |
| Plan resolver becomes a second config system | WS, REST, doctor or Admin recompute precedence independently | Stop; move precedence into `internal/agentspec.Resolver` |
| #946 expands into strict env isolation | Any change to `internal/worker/base/BuildEnv` or host env inheritance is required for the first slice | Defer to #867 and ship only declared/observed/unknown evidence |
| AEP event scope expands unexpectedly | New Kind/Data/JSON tag requires SDK/docs updates beyond the approved additive event contract | Stop and split protocol work into #849/#869 before merging |
| Verification depends on unavailable external services | Tests require live Slack/Feishu/provider credentials | Keep the slice hermetic; use fakes, SQL fault injection and explicit evidence labels |

## Handoff

Plan complete and saved to `docs/superpowers/plans/2026-08-04-runtime-operations-next-iteration.md`.

Recommended execution path: use `superpowers:subagent-driven-development` for Task 1–4 and Task 5–8 as separate reviewable batches. Inline execution with `superpowers:executing-plans` is also valid when one engineer owns the entire runtime contract.
