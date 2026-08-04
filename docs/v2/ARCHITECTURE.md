# HOTPLEX 2.0 Architecture

## 文档定位

本文定义 HotPlex 2.0 的稳定组件、事实所有权、状态语义、数据流、兼容边界和反模式。产品阶段与优先级以 [ROADMAP](./ROADMAP.md) 为准，交付切片与闸门以 [Implementation Roadmap](./IMPLEMENTATION-ROADMAP.md) 为准。

## 架构定位

HotPlex 是 single-Gateway、self-hosted Agent Runtime Gateway。它在现有 Gateway、Session、Worker、AEP、Event Store、Execution、Audit、Observability 和 Cron 内核上形成受控运行时，不建立平行 control plane。

```text
Client / WebChat / Slack / Feishu / HTTP / Cron
                         │
                         v
              Gateway API + AEP Router
                         │
          ┌──────────────┼──────────────┐
          v              v              v
   Authority/Scope  Desired Plan   Session Manager
          │              │              │
          └──────────────┼──────────────┘
                         v
               Durable Input Execution
                         │
                         v
               Worker Adapter Registry
        ┌────────────┬────────────┬────────────┐
        v            v            v            v
   Claude Code  OpenCode Server  Codex CLI     ACP
        │            │            │            │
        └────────────┴────────────┴────────────┘
                         │
              Worker Events / Gateway Effects
                         │
          ┌──────────────┼──────────────┐
          v              v              v
     Event Store    Observed State   Effect Ledger
          │              │              │
          └──────────────┼──────────────┘
                         v
              Reconciliation / Fence
                         │
          ┌──────────────┼──────────────┐
          v              v              v
        Audit        Trace/Metrics   Admin/Cockpit
```

## 组件状态

| 组件 | 状态 | 说明 |
| --- | --- | --- |
| Gateway / Session / Worker / AEP | 已交付 | 当前运行时核心 |
| AgentSpec / AgentIdentity / RuntimeContext | 已交付 | 配置、身份和上下文契约 |
| Durable Input Execution | 已交付 | input ledger、owner lease、single-active gate、fence、repair |
| Runtime Observability | 已交付基线 | trace/metrics 可用，完整 AEP runtime event contract 仍在扩展 |
| EffectiveRuntimePlan / Observed Bootstrap | 已冻结 | 统一期望状态与运行时证据 |
| Gateway-owned EffectLedger | 已冻结 | 外部副作用事实与调和 |
| Bounded ExecutionQueue / Cockpit | 已冻结 | 排序元数据和只读运营投影 |
| Capability Inventory / Recipes | 已冻结 | 条件式平台能力 |

“已冻结”代表实现契约已批准，不代表当前代码已经提供该能力。

## Canonical Fact Ownership

| 事实 | Canonical owner | 写入边界 | 只读消费者 |
| --- | --- | --- | --- |
| Principal / workspace / bot ownership | identity、config、session owner chain | 认证、配置和 session 生命周期 | Gateway、adapter、audit、admin |
| Desired runtime state | `EffectiveRuntimePlan` resolver | WS/REST/doctor/worker/admin/recipe 共同解析路径 | worker start、diagnostics、Cockpit、audit |
| Input execution | `internal/execution` | accept、delivery、owner lease、runtime status、fence | Gateway、forwarder、repairer、runtime events |
| Session lifecycle | `internal/session` | session state machine 和 store | Gateway、Worker、RuntimeContext、admin |
| Runtime events | AEP + `internal/eventstore` | Gateway/Bridge 事件捕获 | client、RuntimeContext、Cockpit、observability |
| Gateway-owned external effect | `EffectLedger` | delivery、Webhook、Cron、control、recipe、connector | retry、reconcile、Cockpit、audit |
| Worker/provider/delivery observed state | worker/connector adapter evidence | actual worker/backend/artifact、provider receipt/query | plan status、reconciler、Cockpit、audit |
| Audit integrity | `internal/audit` | user/tool/admin/security action | CLI verify、admin、export |
| Trace/metrics | `internal/observability` | OTel/Prometheus lifecycle | operator、Cockpit summary、alerts |

Adapter、UI、admin handler、recipe 和 diagnostic checker 不拥有第二份 session、identity、execution 或 effect 真相。缓存必须携带 source version 或 plan hash，并定义失效和降级。

## 核心组件

### Gateway API 与 AEP Router

职责：

- WebSocket/HTTP 认证、workspace ownership、session init 和 AEP 分发；
- durable input accept、ACK、Worker delivery 和 runtime event emission；
- bridge lifecycle、history recovery、LLM retry、audit/trace correlation；
- admin/runtime API 的授权和 redacted response。

边界：

- 不保存 prompt、metadata value、credential 或 raw provider request；
- 不理解 Claude/OpenCode/Codex/ACP 的私有 tool protocol；
- 不以 connection success 或 Worker `done` 证明外部 effect 成功。

### Session Manager

职责：

- 管理 `CREATED/RUNNING/IDLE/TERMINATED/DELETED` 状态机；
- 绑定 user、workspace、AgentIdentity 和 worker session reference；
- 提供 SQLite/PostgreSQL 持久化、quota 和 lifecycle transaction。

边界：

- session 状态不承载 external effect outcome；
- adapter 不绕过 session owner 校验或直接维护独立 session 生命周期。

### Worker Adapter Registry

职责：

- 通过 `worker.Register()` 管理 `claude_code`、`opencode_server`、`codex_cli`、`acp`；
- 将 AgentSpec/EffectiveRuntimePlan 映射为现有 Worker start/input contract；
- 提供 worker/backend/artifact 和 isolation capability evidence。

边界：

- Worker interface 变化同步四 adapter、mocks、`internal/worker/noop` 和 registry tests；
- CLI、singleton 和 RPC Worker 的新能力语义必须明确；
- host env inheritance、filesystem 和 network restriction 只按真实 enforcement 报告。

### AgentSpec 与 EffectiveRuntimePlan

AgentSpec 是当前已交付的 normalized runtime view。EffectiveRuntimePlan 是冻结契约，它将 config、init metadata、workspace、worker registry、policy、sandbox、env profile、capability 和 source refs 解析为一份 canonical redacted desired state。

```text
compiled defaults
  -> base config
  -> platform/bot config
  -> workspace override
  -> session init metadata
  -> validated runtime capability
  -> canonical redacted plan
```

plan 输出包含 plan hash、resolver version、worker、permission、budget/timeout、env key names、sandbox desired state、capability/skill/config hash、source refs、warnings 和 blocked reasons。它不包含值级 secret、prompt、完整命令或 raw error。

### Durable Input Execution

`internal/execution` 是 input lifecycle 的事实源：

- 只保存 payload SHA-256 指纹；
- 区分 accepted/delivered 与 pending/running/completed/failed/unknown；
- 使用 owner instance、lease、worker run ID 和 conditional update；
- single-active gate 返回 `SESSION_BUSY`；
- ambiguity fence 阻止不安全重投；
- late completion 可收敛同一 worker run；
- repairer 处理 durable write failure 和 expired lease。

该 ledger 不承担 Gateway-owned external effect 的 provider evidence。

### Gateway-owned EffectLedger

EffectLedger 是冻结契约，覆盖 delivery、Webhook、Cron、admin/control、recipe 和 connector operation。

```text
planned -> started -> succeeded
                  -> failed
                  -> unknown -> reconciled_succeeded
                             -> reconciled_failed
                             -> fenced
```

每个 effect 具有 execution/effect identity、business idempotency key、claim owner/lease、attempt、provider reference、evidence/confidence、redacted reason 和 timestamps。worker-private tool/model-provider 内部调用不进入该 ledger。

### Observed State 与 Reconciliation

Observed State 记录实际 worker type、backend、artifact/layer digest、env/isolation evidence、provider receipt、delivery acknowledgement、external query result 和 observed timestamp。

Reconciliation 比较 desired 与 observed：

- 外部状态可查询：query 后写入 reconciled result；
- 可安全重试：在 lease、attempt cap、backoff 和 idempotency contract 下重试；
- 结果不可证明：保持 `unknown` 并 fence 冲突操作；
- 需要人工接管：通过授权 operator action 写入 audit 和 final evidence。

### RuntimeContext

RuntimeContext 从 session、eventstore、turns、worker internal session ID 和 workspace metadata 读取恢复事实，为 resume/fork/history reconstruction 提供统一接口。它不创建独立 memory database，也不把 prompt/context 扩展为通用 workflow history。

### Audit 与 Observability

Audit 记录 actor、action、target、decision 和 redacted references；Observability 使用一致的 agent/session/execution/effect correlation keys。

- `execution_id`、`effect_type`、`worker_type`、`outcome` 等有限枚举可用于 metrics；
- plan hash、workspace、provider/evidence ref 只用于脱敏 trace/log/audit correlation；
- tracing disabled 保持 noop，不阻止 Gateway 启动；
- Cockpit 只消费现有 canonical facts，并具备分页、时间窗、payload 和数据库连接预算。

## 状态语义

| 状态 | 证明的事实 | 不证明的事实 |
| --- | --- | --- |
| planned | Gateway 计算出期望计划 | worker/backend/sandbox 已应用 |
| accepted | input 已持久化接收 | Worker 已处理 |
| delivered | input 已投递到指定 Worker run | Worker turn 已成功 |
| running | Worker run 已进入运行态 | 外部 provider/effect 已成立 |
| completed | Worker turn 返回完成终态 | 消息、Webhook 或 connector effect 已成功 |
| effect succeeded | provider/connector contract 确认 effect 成功 | 所有下游最终用户状态均已验证 |
| unknown | 现有 evidence 无法证明成功或失败 | 动作未发生 |
| reconciled | query/callback/operator evidence 已收敛 unknown | 未来不会出现新的外部变化 |
| fenced | 冲突动作被阻止 | unknown 已解决 |

## 数据流

### Session 初始化

```text
client init
  -> authenticate principal and workspace ownership
  -> AgentSpec / EffectiveRuntimePlan resolution
  -> validate Worker registry and capabilities
  -> bind AgentIdentity
  -> create or resume Session
  -> start Worker with plan reference
  -> capture observed worker/backend evidence
  -> AEP init_ack + runtime metadata/events
```

plan resolution 失败发生在 Worker 或外部副作用启动之前。等价 WS/REST 输入必须得到相同 canonical plan hash。

### Input 执行

```text
AEP input
  -> validate owner/session/payload fingerprint
  -> durable accept + ACK
  -> single-active gate / execution lease
  -> Worker.Input
  -> bridge.forwardEvents
  -> eventstore + trace + audit
  -> durable runtime terminal or unknown/fence
```

旧 forwarder 绑定 immutable worker run；`/reset` 或跨平台重连不能使旧连接事件写入新 run。

### Gateway-owned External Effect

```text
execution intent
  -> effect planned + business idempotency key
  -> claim lease
  -> provider attempt
  -> accepted/receipt/query evidence?
       -> yes: succeeded
       -> no: unknown
  -> reconcile query / bounded retry / fence / operator action
  -> audit + eventstore + diagnostics reference
```

provider response 丢失不自动触发第二次外部动作；只有 provider contract 和当前 evidence 证明安全时才允许重试。

### Context 恢复

```text
resume request
  -> session owner/state lookup
  -> RuntimeContext reads eventstore/turns/worker session ref
  -> execution fence/open-state check
  -> worker-specific history reconstruction
  -> observed state refresh
  -> session RUNNING or IDLE
```

## 兼容边界

| 兼容面 | 契约 |
| --- | --- |
| AEP v1 | 新 Kind/Data/Metadata 增量兼容；旧客户端可忽略未知事件和字段 |
| SDK | AEP 变化同步 Go SDK 和 TypeScript/Python/Java 示例 SDK |
| Config | 现有 `worker_type` 和 worker config 保留；AgentSpec/plan 是 normalized view |
| Session | 新数据使用兼容字段或 versioned JSON；旧记录可读 |
| Worker | 保持核心 start/input/event contract；扩展同步四 adapter 和 mocks |
| Database | SQLite/PostgreSQL migration、conditional update 和跨实例测试成对 |
| Cross-platform | process、env、path、signal 和 injection 在 Linux/macOS/Windows 分离验证 |
| Observability | disabled 模式 noop；新增 metrics 使用低基数属性 |

## 安全与隐私边界

- authentication、authorization、capability declaration、filesystem/network enforcement、credential injection 和 audit 分开建模；
- workspace/path/origin 使用类型化解析、canonicalization、allowlist 和负向测试；
- strict profile fail closed；compat profile 明确 host env inheritance 和迁移告警；
- durable facts 不保存 prompt、metadata value、secret、credential、raw provider request、完整 tool args 或 raw worker error；
- audit 证明操作被记录，不证明模型无法越权；
- capability token 或 plan hash 不证明 OS isolation；
- XML sanitizer 和 Windows 临时文件注入保持强制安全边界。

## 关闭与恢复

Gateway 关闭顺序保持：

```text
signal -> cancel ctx -> tracing -> hub -> bridge -> sessionMgr -> HTTP
```

Repairer、reaper 和 reconciler 必须有 stop condition；关闭时未确认的 effect 保持 durable pending/unknown，不记录为已成功，也不因进程退出盲目重投。

## 反模式

- 新建独立 runtime/session/worker 生命周期；
- 为 runtime events 引入第二 event bus；
- 让 WebChat、Slack、Feishu、doctor 或 recipe 各自解析 runtime plan；
- 把 Worker `done`、HTTP 2xx、timeout 或 audit 记录映射为 external effect success；
- 把单进程 mutex/Set 当成跨重启幂等；
- 在 ExecutionQueue 中理解 Worker 私有协议；
- 建立独立 identity、memory 或 capability authorization 事实源；
- 以 high-cardinality metrics、N+1 查询或无界 snapshot 实现 Cockpit；
- 只实现 SQLite 或 PostgreSQL 一种方言；
- 在没有 evidence、rollback 和 operator action 时报告 `enforced` 或 `completed`。

## 关联文档

- [HotPlex 2.0 Roadmap](./ROADMAP.md)
- [HotPlex 2.0 Implementation Roadmap](./IMPLEMENTATION-ROADMAP.md)
- [Runtime Operations Contract](../superpowers/specs/2026-08-04-runtime-operations-contract.md)
- [Scope-aware Capability Inventory Contract](../specs/Scope-Aware-Capability-Inventory-Spec.md)
