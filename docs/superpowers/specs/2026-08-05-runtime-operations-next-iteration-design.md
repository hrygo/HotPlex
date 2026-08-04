---
title: "HotPlex Runtime Operations 下一迭代详细设计"
type: design-spec
status: proposed
design_status: proposed
implementation_status: not_started
date: 2026-08-05
owners: [hotplex-runtime]
issue_refs: ["#877", "#946"]
references:
  - docs/v2/ROADMAP.md
  - docs/v2/ARCHITECTURE.md
  - docs/v2/IMPLEMENTATION-ROADMAP.md
  - docs/superpowers/specs/2026-08-04-runtime-operations-contract.md
  - docs/superpowers/plans/2026-08-04-runtime-operations-next-iteration.md
---

# HotPlex Runtime Operations 下一迭代详细设计

## 1. 文档状态与决策边界

本文是下一迭代的 proposed design spec，不代表实现已经完成，也不把本文中的推荐行为标记为已交付能力。实现前需要确认本 spec 的两个垂直切片边界、API 命名、状态迁移和验收标准。

本 spec 继承已批准的 [Runtime Operations Contract](2026-08-04-runtime-operations-contract.md)，只把其中当前迭代的 #877 与 #946 范围具体化：

1. #877：为无法自动收敛的 fenced execution 提供有授权、有审计、有条件并发保护的 operator action。
2. #946：把现有 `internal/agentspec.Resolver` 扩展为 `EffectiveRuntimePlan` 的只读/影子 first slice。

两条轨道共享 redaction、authorization、audit 和 verification 约束，但不共享新的持久化事实表；任一轨道可以单独合并和回滚。

## 2. 目标与非目标

### 2.1 目标

- 被 fence 的 session 可以被 operator 安全 inspect、resolve 或 abandon，不需要人工改库或整 Gateway 重启。
- 两个 Gateway 实例同时处理同一个 fence 时，只有持有最新 fence version 的 action 能成功。
- operator action 不会把未知执行伪装成成功，也不会自动重投旧 input。
- 所有 operator action 都具备 actor、target、reason、decision、evidence ref 和 timestamp 的审计事实。
- WS init、REST session creation、doctor 和 Admin diagnostics 使用同一个 `EffectiveRuntimePlan` resolver。
- 等价 WS/REST 输入产生相同 canonical plan hash。
- 计划输出只包含可安全展示的字段；未观测到 Worker/backend/artifact 时保持 `planned` 或 `unknown`。
- 首个切片可在不改变既有 Worker dispatch 和 AEP v1 client 行为的情况下上线。

### 2.2 非目标

- 不按固定时长自动清除 fence；时间经过不是旧 Worker 已终止的证据。
- 不自动重投原始 input；不新增 retry 或 scheduler 语义。
- 不把 `resolve` 或 `abandon` 解释为外部消息、Webhook、Cron 或 provider effect 已成功。
- 不实现 #867 的 strict environment allowlist、OS isolation、credential broker 或 host env 清理。
- 不实现 #947 `EffectLedger`、#851 `ExecutionQueue` 或 #868 Execution Cockpit。
- 不把 worker-private tool protocol 搬到 Gateway。
- 不创建第二套 session、execution、event bus、identity 或 configuration system。

## 3. 当前基线

当前代码已具备：

- `internal/execution` 的 payload hash、owner lease、single-active gate、`unknown`、fence、late convergence 和 fresh Worker 条件清理；
- `FenceBySession`、`ClearFenceAfterFreshStart` 和 `ErrExecutionFenced`；
- #847 first-cut `AgentSpec` 与纯 `Resolver`；
- WS/REST WebChat 输入的 `BuildWebChatInput` 和 AgentSpec shadow comparison；
- Admin Bearer+scope middleware、Admin audit、`/admin/health/workers`、`/admin/metrics`；
- SQLite/PostgreSQL 成对 migration 结构和 fault-injection / multi-instance 测试模式。

当前缺口是：

- fence 没有 operator-facing conditional action 和 operator-readable token/version；
- 没有统一的 redacted desired runtime plan；
- doctor/Admin 尚未消费同一份 plan resolver；
- 现有 AgentSpec 仍是 normalized view，不是完整的 desired/observed 分离模型。

## 4. 总体架构

```text
Authority / Scope / Capability
             │
             ▼
   EffectiveRuntimePlan  ───────────────┐
             │                          │
             ▼                          │
   Durable Input Execution              │
             │                          │
   unknown + fence                      │
             │                          │
             ▼                          │
   Operator inspect / resolve / abandon │
             │                          │
             └──── audit + runtime evidence

Worker start / WS init / REST create / doctor / Admin diagnostics
             └──────────► one Resolver ◄──────────┘
```

事实所有权保持不变：

| 事实 | 唯一写入者 | 本迭代消费者 |
|---|---|---|
| Input delivery/runtime/fence | `internal/execution` | Gateway、operator API、repairer |
| Operator actor/decision/reason/evidence | Admin handler + audit collector | Admin、CLI、审计查询 |
| Desired runtime plan | `internal/agentspec.Resolver` | WS、REST、doctor、Admin |
| Worker observed bootstrap | Worker/Gateway adapter evidence | Admin diagnostics、日志、trace |
| Wire event | AEP/eventstore | Client、RuntimeContext、audit/observability |

Operator API、CLI、doctor 和 WebChat 不得各自维护 execution 或 plan 的副本。CLI 只能调用 Admin API；doctor 只能做本地只读解析；UI 只消费 redacted projection。

## 5. #877 Fenced Execution Operator Action

### 5.1 数据模型

在现有 `execution_inputs` 增加两个字段，SQLite/PostgreSQL 必须成对迁移：

```sql
fence_version    INTEGER/BIGINT NOT NULL DEFAULT 0
fence_created_at INTEGER/BIGINT NULL
```

`Record` 增加：

```go
type Record struct {
    // existing fields...
    FenceReason    string
    FenceVersion   int64
    FenceCreatedAt *int64
}
```

`fence_version` 是并发 fencing token：每次 execution 从非 fenced 进入 fenced 时单调递增；operator action 必须携带读取时的 version。`fence_created_at` 只用于诊断、排序和 operator 判断，不触发自动清理。

### 5.2 Operator action contract

```go
type FenceDecision string

const (
    FenceDecisionResolve FenceDecision = "resolve"
    FenceDecisionAbandon FenceDecision = "abandon"
)

type FenceActionRequest struct {
    ExecutionID         string
    ExpectedFenceVersion int64
    Decision             FenceDecision
}

var ErrFenceConflict = errors.New("execution: fence version conflict")

type Store interface {
    // existing methods...
    ApplyFenceDecision(ctx context.Context, req FenceActionRequest) (*Record, error)
}
```

Actor、reason、evidence ref 不进入 `internal/execution.Store`，由 Admin 层校验、审计和关联；execution store 只负责原子状态迁移。

### 5.3 状态语义

#### Inspect

只读返回 execution ID、session ID、runtime status、delivery status、fence reason、fence version、fence created time、worker run ID 的脱敏引用和更新时间。不得返回 payload hash 以外的用户输入内容。

#### Resolve

- 条件：`execution_id` 匹配且 `fence_version = expected_fence_version`；
- 动作：清空 `fence_reason`，保留原有 `runtime_status=unknown`；
- 不动作：不修改 delivery 为 `delivered`，不生成新的 Worker run，不调用 Worker input；
- 后续：新的 client message 使用新 ID，按正常 accept 流程创建新 execution；
- 晚到事件：只有匹配原 `worker_run_id` 的晚到 completion 可以收敛原 execution，不能修改新 execution。

#### Abandon

- 条件：同 Resolve；
- 动作：清空 `fence_reason`，将 runtime status 置为 `failed`，错误枚举为 `OPERATOR_ABANDONED`；
- 不动作：不修改 delivery 为 `delivered`，不重投旧 input，不证明外部 effect 已失败或成功；
- 晚到事件：对已 terminal 的 abandoned execution 不得回写为 completed。

#### Keep fenced

不调用 action endpoint；继续保持 fence。不存在隐式的“超时后自动 resolve”。

### 5.4 条件更新

SQLite 在 `sqlutil.WriteMu` 保护下执行；PostgreSQL 使用行版本条件更新。两者都必须满足：

```sql
UPDATE execution_inputs
SET fence_reason = ?, runtime_status = ?, runtime_error_code = ?, updated_at = ?
WHERE execution_id = ?
  AND fence_reason <> ''
  AND fence_version = ?;
```

影响行数为 `0` 时：重新读取当前记录；不存在返回 `ErrNotFound`，版本已变化或已经解 fence 返回 `ErrFenceConflict`。重复同一 action 不得产生第二条状态迁移或第二次 dispatch。

### 5.5 Admin HTTP API

新增 runtime scopes：

- `runtime:read`：读取 fence facts 和 redacted runtime plan；
- `runtime:write`：执行 resolve/abandon。

路由由 `cmd/hotplex/routes.go` 注册，并统一经过现有 `AdminAPI.Middleware`：

#### `GET /admin/executions/fences`

需要 `runtime:read`。支持 `session_id`、`limit`、`offset`，默认有界分页。返回：

```json
{
  "fences": [
    {
      "execution_id": "exec_...",
      "session_id": "sess_...",
      "runtime_status": "unknown",
      "delivery_status": "unknown",
      "fence_reason": "GATEWAY_LEASE_EXPIRED",
      "fence_version": 3,
      "fence_created_at": 1754300000000,
      "updated_at": 1754300000000
    }
  ],
  "limit": 100,
  "offset": 0
}
```

#### `POST /admin/executions/{id}/fence-action`

需要 `runtime:write`。请求：

```json
{
  "expected_fence_version": 3,
  "decision": "resolve",
  "reason": "verified worker process is no longer running",
  "evidence_ref": "audit:..."
}
```

约束：

- `decision` 只能是 `resolve` 或 `abandon`；
- `reason` 必填，长度限制为 1–512 个字符；
- `evidence_ref` 必填，长度限制为 1–256 个字符，不允许携带 prompt、secret 或完整 provider response；
- actor 从认证上下文读取，不允许客户端伪造；
- 成功返回新的 redacted execution fact；
- 版本冲突返回 `409 FENCE_CONFLICT`；参数错误返回 `400`；无 scope 返回 `403`；记录不存在返回 `404`。

### 5.6 Audit 与 runtime event

每次成功或拒绝的 operator write 都写入 Admin audit：

```json
{
  "actor": "user-or-admin-token",
  "action": "runtime.fence.resolve",
  "target": "exec_...",
  "decision": "resolve",
  "reason_code": "operator_reason_present",
  "evidence_ref": "audit:...",
  "result": "ok",
  "timestamp": "2026-08-05T...Z"
}
```

reason 内容只在受保护的 audit detail 中按现有 retention 规则保存；metrics 和 runtime event 不保存自由文本。

`abandon` 复用已有 additive `runtime.execution.failed` 语义，错误枚举为 `OPERATOR_ABANDONED`；`resolve` 不发出 completed/succeeded 事件。若当前 event contract 无法表达 operator decision，应先扩展 `pkg/events` 及 Go/TypeScript/Python/Java SDK 和双向测试，再实现 handler；不得临时增加未同步的 Kind。

### 5.7 CLI 与 doctor

CLI 只调用 Admin API，不直接打开数据库：

```text
hotplex runtime fences list
hotplex runtime fences resolve <execution-id> --fence-version 3 --reason "..." --evidence-ref "..."
hotplex runtime fences abandon <execution-id> --fence-version 3 --reason "..." --evidence-ref "..."
```

`resolve` 和 `abandon` 需要显式 `--confirm`；收到 `409` 时提示重新 inspect，不自动重试。

doctor 新增只读 `runtime.fenced_executions` checker：无 fence 为 Pass，有 fence 为 Warn，输出数量、最早 fence 时间和修复提示；没有 `FixFunc`。

## 6. #946 EffectiveRuntimePlan First Slice

### 6.1 Plan value object

扩展 `internal/agentspec`，不创建 `internal/runtimeplan` 平行包：

```go
type EffectiveRuntimePlan struct {
    Version       int
    Resolver      string
    PlanHash      string
    AgentSpec     AgentSpec
    EnvProfile    string
    EnvKeys       []string
    CapabilityIDs []string
    SkillHash     string
    ConfigHash    string
    SourceRefs    []PlanSourceRef
    Warnings      []PlanWarning
    Blocked       []PlanBlockReason
}

func (r Resolver) ResolvePlan(in Input) (EffectiveRuntimePlan, error)
func (p EffectiveRuntimePlan) Redacted() EffectiveRuntimePlanView
func CanonicalPlanHash(view EffectiveRuntimePlanView) string
```

本 first slice 的 `EffectiveRuntimePlan` 是 desired state，不是 observed success。`AgentSpec.Worker.Command` 等内部 contract 字段不得直接进入 Admin/doctor public view；public view 只保留 worker type、permission summary、sandbox summary、environment key names、hash、source refs、warnings 和 blocked codes。

### 6.2 Precedence 与 fail-closed

解析顺序固定为：

```text
compiled defaults
  -> base config
  -> platform/bot config
  -> workspace override
  -> session init metadata
  -> validated runtime capability
```

本迭代只复用当前 `AgentSpec` 已有的 config/init/workspace/platform 事实，不实现 #867 strict env allowlist。以下情况必须生成 bounded blocked reason 或 error，不能静默降级为可执行成功：

- 非空但未知 worker type；
- 非法 permission mode；
- workspace/owner/origin 事实缺失或冲突；
- plan 必需的 capability 无法验证；
- 发现 secret-shaped value 被尝试写入 public plan。

Compat 行为可以继续运行，但必须产生 warning；无法证明 enforcement 的能力只允许 `partial`、`unavailable` 或 `unknown`。

### 6.3 Canonical hash

hash 输入是 redacted canonical view，不包括：

- prompt、metadata value、credential、token、secret；
- 完整 command、host environment value、workspace absolute path；
- process ID、时间戳、provider/evidence ref；
- map 的随机遍历顺序、nil/empty 的非语义差异。

canonicalization 规则：

1. 固定字段集合和版本号；
2. nil slice 与空 slice 归一化；
3. 语义无序的 ID/key 列表排序；
4. 保留具有语义的 precedence/source 顺序；
5. UTF-8 JSON 编码后计算 SHA-256；
6. hash 只作为 desired plan identity，不证明 Worker/backend/sandbox 已应用。

### 6.4 WS/REST 共用解析路径

当前 `BuildWebChatInput` 保留为共同输入构造器：

```text
WS init ─────┐
             ├── BuildWebChatInput / agentspec.Input ── Resolver.ResolvePlan
REST create ─┘
```

实现阶段先 shadow：

- legacy `SessionStartParams` 继续作为 dispatch authoritative value；
- plan 解析错误记录为 redacted diagnostic，不改变现有行为，除非现有入口已明确 fail closed；
- plan hash 不写入高基数 metrics label；
- WS/REST 等价输入的 hash 不一致时记录 warning 并使测试失败。

### 6.5 Observed bootstrap

Worker 启动后只补充可验证事实：worker type、backend/artifact digest（如已有安全来源）、capability state 和 observed timestamp。状态映射：

| desired/observed 情况 | 输出 |
|---|---|
| 只有 plan 已生成 | `planned` |
| Worker/backend/artifact 未可验证 | `unknown` |
| 只声明但没有实际 enforcement 证据 | `declared` 或 `partial` |
| 具有真实 backend enforcement 证据 | `enforced` |

本切片不把配置 hash、HTTP 200、Worker done 或 audit row 单独提升为 `enforced`。

### 6.6 Admin 与 doctor read path

新增 `GET /admin/sessions/{id}/runtime-plan`，需要 `runtime:read` 和现有 session/workspace authorization。返回 redacted plan、plan hash、observed summary、warnings、blocked codes；不返回 prompt、secret、完整命令或 raw worker error。

doctor 新增 `runtime.effective_plan` checker：使用本地 config 和现有 resolver，输出 plan hash、worker type、permission/sandbox summary、warnings/blocked codes；只读，不自动改配置。

首个切片不新增 plan 专用表或列。已有 secret-free `SpecSnapshot` 继续承担向后兼容的 session snapshot；完整 computed plan 作为诊断 projection 计算，不建立第二套持久化真相。

## 7. 数据流与生命周期

### 7.1 Fence operator flow

```text
operator/CLI
    │ GET fences
    ▼
Admin scope + session authorization
    │
    ▼
redacted FenceFact + fence_version
    │ POST action(expected version, decision, reason, evidence)
    ▼
Admin validation + actor extraction
    │
    ├── Admin audit intent/result
    ├── execution.Store conditional update
    ├── runtime.execution.failed for abandon only
    └── redacted response
```

旧 Worker late completion 与 operator action 的竞态由 `execution_id + worker_run_id + runtime status` 条件更新收敛；operator action 不调用 Worker、不触发 input replay。

### 7.2 Plan flow

```text
config + platform/bot + workspace + init metadata
                     │
                     ▼
             agentspec.Input
                     │
                     ▼
           Resolver.ResolvePlan
                     │
        ┌────────────┴────────────┐
        ▼                         ▼
 legacy dispatch shadow      redacted diagnostics
        │                         │
        ▼                         ▼
 Worker observed bootstrap    Admin / doctor / trace
```

任何单一来源都不能替代其他事实层：plan hash 不替代 execution，Worker done 不替代 effect evidence，audit 不替代 provider receipt。

## 8. 错误与兼容

| 场景 | HTTP/CLI/内部语义 | 是否重试 |
|---|---|---|
| fence 不存在 | `404 FENCE_NOT_FOUND` / CLI non-zero | 否 |
| fence version 过期 | `409 FENCE_CONFLICT` / 重新 inspect | 否，禁止自动重试 write |
| 无 `runtime:read` | `403` | 否 |
| 无 `runtime:write` | `403` | 否 |
| decision/reason/evidence 非法 | `400` | 否 |
| DB conditional update timeout | `503` | operator 可重新 inspect；不自动重复 action |
| unknown worker type | plan blocked/error | 不启动新 Worker |
| capability 无观测证据 | `unknown`/`partial` | 不宣称 enforced |
| legacy session 无 plan snapshot | computed plan unavailable/unknown | 保持旧读取兼容，不回填敏感字段 |

现有 AEP v1 客户端必须继续工作。`runtime.execution.failed` 使用现有事件字段；如必须新增 Kind、Data 或 JSON tag，必须同时更新 `pkg/events`、Go SDK、TypeScript/Python/Java 示例 SDK、`docs/reference/{aep-protocol,events}.md` 和双向协议测试。

## 9. 隐私、安全与审计

- execution store 只保存 payload SHA-256，不保存输入正文。
- Plan public view 只保留 IDs、hash、枚举、key names、source refs 和 bounded diagnostics。
- reason/evidence 由 Admin API 做长度、字符集和敏感字段边界检查；自由文本不进入 metrics label。
- actor 从认证上下文提取，客户端不能伪造；`admin-token` 与用户 actor 按现有审计规范记录。
- `runtime:read` 与 `runtime:write` 分离；写操作默认经过现有 Bearer+scope middleware 和 admin audit。
- resolve/abandon 不授予新的 workspace、filesystem、network 或 credential 能力。
- capability status 只表达证据级别，不把 allowlist、token、audit 或 plan hash 解释为形式化隔离证明。
- 所有 response body、event data、logs 和 tests 禁止出现 prompt、secret、token、完整 command、raw provider request 和 raw worker error。

## 10. 数据库迁移与回滚

新增：

- `internal/session/sql/migrations/031_execution_fence_operator.sql`；
- `internal/session/sql/migrations-postgres/031_execution_fence_operator.pg.sql`。

迁移要求：

- old rows 默认 `fence_version=0`、`fence_created_at=NULL`；
- migration 可重复启动且不会重建 `execution_inputs`；
- SQLite/PostgreSQL 列类型和条件更新语义一致；
- rollback 只允许在没有新 operator action 依赖时执行，且由版本化 migration 管理，禁止人工改生产库；
- 如果迁移失败，保留旧 fence behavior，不启动新的 operator write path。

#946 first slice 不新增 plan 表或列，回滚只需关闭 shadow diagnostic route/checker，旧 AgentSpec 和 session snapshot 继续可读。

## 11. 可观测性与容量预算

允许的低基数 metrics：

- `hotplex_runtime_fence_actions_total{decision,result}`；
- `hotplex_runtime_fence_conflicts_total`；
- `hotplex_runtime_plan_resolutions_total{result}`；
- `hotplex_runtime_plan_blocked_total{reason_code}`；
- `hotplex_runtime_plan_observed_total{state}`。

禁止 label：execution ID、session ID、workspace ID、plan hash、provider ref、evidence ref、actor ID。

容量约束：

- fence list 默认 `limit <= 100`，最大 `limit <= 500`；
- reason 最大 512 字符，evidence ref 最大 256 字符；
- plan public view 只返回 bounded arrays，单响应不超过现有 Admin API payload budget；
- CLI 不缓存 action intent，不在 409 后自动 replay；
- audit 使用现有 retention/GC，不创建平行 action log。

## 12. 测试与验证矩阵

### 12.1 #877

| 类别 | 必测场景 |
|---|---|
| Store | resolve、abandon、非法 decision、missing record、stale version、重复 action |
| State | unknown 保留、abandon 变 failed、delivery 不变、旧 input 不重投 |
| Concurrency | SQLite WriteMu、PostgreSQL MVCC、两个 Gateway 竞争同一 fence version |
| Late event | resolve/abandon 后旧 worker done、不同 worker run ID、terminal 不回退 |
| Security | runtime scopes、session/workspace authorization、CSRF/Origin 规则、actor 不可伪造 |
| Audit | 成功、拒绝、409、reason/evidence redaction、链式 audit 可验证 |
| API/CLI | 400/403/404/409/503、分页、显式确认、CLI 不直接访问 DB |
| Restart | inspect 后 Gateway restart，再 action；旧记录仍可读、条件 token 仍有效或明确冲突 |

### 12.2 #946

| 类别 | 必测场景 |
|---|---|
| Resolver | 四 Worker、config fallback、init override、workspace permission、unknown worker、invalid permission |
| Hash | 等价输入相同 hash、无序 list 归一化、nil/empty 等价、source 顺序保持 |
| Redaction | secret/token/env value/command/path/raw error 不进入 public JSON 或 hash |
| Equivalence | WS init 与 REST create semantic input 产生相同 plan hash |
| Observed | planned、unknown、declared、partial、enforced 的边界不能混淆 |
| Admin | session/workspace authorization、runtime:read、bounded response、旧 snapshot 兼容 |
| Doctor | `runtime.effective_plan` 注册、JSON 输出、blocked/warn、无 FixFunc |
| Compatibility | 旧 AEP v1 client、旧 AgentSpec/session snapshot、四平台路径/env 行为不回归 |

### 12.3 验证命令

```text
rtk go test ./internal/execution ./internal/admin ./internal/agentspec ./internal/gateway ./internal/cli/... ./cmd/hotplex -count=1 -race
rtk go test -tags pg -p 1 ./internal/execution ./internal/session/sql -count=1 -race
rtk make check
rtk make docs-build
rtk git diff --check
```

真实 PostgreSQL 测试依赖 `HOTPLEX_TEST_PG_DSN`；未配置时只能报告环境未执行，不能报告 PostgreSQL 验证完成。

## 13. 发布、暂停与回滚

### 发布顺序

1. 先部署 migration 和只读 fence list / plan diagnostics；
2. 验证 redaction、权限、payload budget 和 metrics cardinality；
3. 开启 operator write endpoint；
4. 开启 CLI action facade；
5. 保持 #946 shadow comparison，观察 WS/REST plan divergence；
6. 在下一迭代评审后再决定是否将 plan 解析从 shadow 提升为 authoritative。

### 暂停条件

- 任何 operator action 导致重复 Worker input；
- 任何 action 把 unknown 写成 completed/delivered；
- 任何 response/event/audit 出现 prompt、secret 或 raw worker error；
- 两个实例可成功应用同一个 fence version；
- WS/REST divergence 影响实际 dispatch；
- plan diagnostics 产生 high-cardinality metrics 或无界读取。

### 回滚

- 禁用 `runtime:write` scope 或 route，保留 inspect；
- 停止 CLI write command，不删除 audit facts；
- 将 #946 shadow route/checker 置为 read-only/disabled，保留旧 `AgentSpec` 行为；
- 不回滚已产生的 operator audit；
- 不通过删除 migration 或直接修改数据库恢复状态。

## 14. 成本与收益边界

### 成本

- 一次成对数据库迁移和跨实例条件更新测试；
- Admin scope/API/CLI/doctor 的新增维护面；
- 四 Worker 与 WS/REST plan equivalence 的回归验证；
- AEP/SDK 同步成本仅在确需新增 event Kind 时发生；
- 本迭代不承担 EffectLedger、strict isolation、queue 或 Cockpit 的实现成本。

### 可验证收益

- fenced session 从“只能人工介入”变成“可审计的受控恢复”；
- 重复 dispatch、错误恢复和 session lockout 的风险边界可测试、可监控；
- 平台/运维能以 plan hash 和 blocked reason 解释实际运行意图；
- 后续 #867、#947、#868 复用同一 plan、execution、audit 和 evidence vocabulary，减少平行实现和返工。

本 spec 不给出节省金额或 payback 数字；应在上线后通过 fence action 数、恢复耗时、重复 dispatch、plan divergence 和 blocked preflight 数据计算实际 ROI。

## 15. 完成定义

### #877 完成

- [ ] `GET /admin/executions/fences` 可读取 redacted fence facts，并有 runtime:read + session/workspace authorization。
- [ ] `POST /admin/executions/{id}/fence-action` 支持 resolve/abandon、runtime:write、reason/evidence 校验和 409 fencing conflict。
- [ ] SQLite/PostgreSQL 条件更新一致，两个实例竞争只能成功一个 action。
- [ ] resolve 保留 unknown，不重投旧 input；abandon 写入 bounded `OPERATOR_ABANDONED` failed fact。
- [ ] late completion、restart、重复 action 和旧 worker run 语义有测试。
- [ ] Admin audit、runtime event 和 CLI/doctor 证据脱敏并可查询。

### #946 first slice 完成

- [ ] `Resolver.ResolvePlan`、redacted public view 和 canonical SHA-256 hash 已存在并有确定性测试。
- [ ] WS/REST semantic equivalent input 产生相同 plan hash。
- [ ] doctor、Admin diagnostics 和 shadow comparison 消费同一 resolver。
- [ ] 四类 Worker 的 observed state 不把 planned/unknown 误报为 enforced。
- [ ] 不新增 plan parallel store，不改变 legacy dispatch，不泄露敏感字段。

### 全局门禁

- [ ] 受影响模块 `go test -race -count=1` 通过。
- [ ] PostgreSQL 受影响路径在配置 DSN 时通过；未配置时明确标注未执行。
- [ ] `make check`、`make docs-build`、`git diff --check` 通过。
- [ ] 代码、测试、API 文档、CLI 文档、Roadmap 和 Issue 状态相互可追踪。

## 16. 开工闸门

实现前需确认：

1. 是否接受 `resolve` 只清 fence、保留 `unknown` 的语义；
2. 是否接受 `abandon` 仅写 `failed/OPERATOR_ABANDONED`、不改变 delivery 的语义；
3. 是否接受 `runtime:read` / `runtime:write` 两个新 scope 和本文 API 路径；
4. 是否接受 #946 first slice 保持 shadow/read-only，不在本迭代切换 authoritative dispatch；
5. 是否接受本迭代不包含 #867、#947、#851、#868。

以上确认完成后，按现有实施计划执行 Task 1–8；任一确认未通过，先修改本 spec，不直接进入代码实现。
