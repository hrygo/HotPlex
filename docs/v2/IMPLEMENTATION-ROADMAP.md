# HOTPLEX 2.0 Implementation Roadmap

## 文档定位

本文展开 [HotPlex 2.0 Roadmap](./ROADMAP.md) 的交付切片、依赖、阶段进入条件、退出条件、验证和回滚要求。产品定位、目标状态和非目标以 ROADMAP 为准；组件职责和事实所有权以 [Architecture](./ARCHITECTURE.md) 为准。

## 执行原则

1. 扩展 Gateway、Session、Worker、AEP、Execution 和 config，不建立平行 runtime。
2. 已交付能力作为稳定基线，不在后续 Issue 中重新实现。
3. 未交付能力遵循 approved spec，先完成垂直切片，再提取通用抽象。
4. 每个切片具有独立用户价值、独立测试和独立回滚边界。
5. 新事实层先 read-only 或 shadow，不改变现有 dispatch 和 AEP v1 语义。
6. SQLite/PostgreSQL、四类 Worker、Linux/macOS/Windows 和 AEP/SDK 同步验证。
7. completion、audit、HTTP response 和 config hash 不替代 external evidence。
8. prompt、metadata value、secret、credential、raw provider request、完整 tool args 和 raw worker error 不进入 durable facts。

## 已完成基线

| 能力 | Issue | 已交付事实 | 后续约束 |
| --- | --- | --- | --- |
| AgentSpec | [#847](https://github.com/hrygo/hotplex/issues/847) | config/init/workspace/worker 归一化视图 | EffectiveRuntimePlan 复用现有 resolver |
| AgentIdentity | [#848](https://github.com/hrygo/hotplex/issues/848) | identity 贯穿 session metadata、audit 和 trace | 不新建 identity service |
| Runtime Observability | [#850](https://github.com/hrygo/hotplex/issues/850) | runtime spans/metrics 基线 | 新属性保持低基数和统一命名 |
| RuntimeContext | [#852](https://github.com/hrygo/hotplex/issues/852) | eventstore/turns/worker session/workspace context 接口 | 不新建 memory product |
| Durable Ingress | [#878](https://github.com/hrygo/hotplex/issues/878) | input ledger、single-active gate、owner lease、runtime/delivery 分离、ambiguity fence、late convergence、有界 repairer | #851/#947 复用 execution identity、lease 和 fence |
| Fence Escape Hatch | [#877](https://github.com/hrygo/hotplex/issues/877) | fence 持久化（`fence_created_at`/`fence_version`）、条件 resolve/abandon（跨实例 409 冲突）、审计、Admin API、CLI、doctor | operator 决策不自动重试、不伪装成功、不重投旧 input；`fence_version` 是唯一并发条件 |
| EffectiveRuntimePlan (shadow) | [#946](https://github.com/hrygo/hotplex/issues/946) | redacted plan 投影 + canonical hash、fail-closed blocked codes、WS/REST 影子解析、Admin/doctor 只读路径 | dispatch 仍以 legacy SessionStartParams 为准；plan hash 永不进入 metric label；worker start/recipe dry-run 的 authoritative 切换属后续切片 |

完整 AEP runtime event contract 仍由 [#849](https://github.com/hrygo/hotplex/issues/849) 跟踪；#878 交付的最小 `runtime.execution.*` 事件不代表 #849 全部完成。

## 依赖图

```text
已交付基线
  #847 AgentSpec ─────────────┐
  #848 AgentIdentity          │
  #850 Runtime Observability  │
  #852 RuntimeContext         │
  #878 Durable Ingress ───────┼──────────────────────┐
                              │                      │
                              v                      v
                    #946 EffectiveRuntimePlan   #877 Fence Escape Hatch
                              │                      │
                     ┌────────┴────────┐             │
                     v                 v             │
               #849 AEP Events   #867 Isolation     │
                     │           Profiles            │
                     └────────┬────────┘             │
                              v                      v
                         #947 EffectLedger <─────────┘
                              │
                     ┌────────┴────────┐
                     v                 v
               #851 ExecutionQueue  #868 Cockpit
                     │                 │
                     └────────┬────────┘
                              v
                    #870 Recipes / #948 Capability Inventory
```

## 当前交付切片

| 顺序 | 切片 | 直接产出 | 验证重点 | 状态 |
| ---: | --- | --- | --- | --- |
| 1 | #877 fenced execution operator action | 有审计、可授权、可恢复的 session 解锁 | 三重失败、late completion、权限和审计 | ✅ 已交付（PR #953） |
| 2 | #946 EffectiveRuntimePlan + observed bootstrap | WS/REST/doctor/worker/admin/recipe 统一 plan | canonical hash、redaction、四 Worker、drift/unknown | 🟡 shadow first slice 已交付（PR #953）；worker start/recipe authoritative 切换未开始 |
| 3 | #867 env allowlist + isolation report | compat/strict profile 和真实 enforcement 状态 | 三平台 env、filesystem/network evidence | 未开始 |
| 4 | #947 Cron/Webhook/message durable effect | 第一条 `desired → effect → observed → reconcile` 闭环 | 重启、多实例、timeout、5xx、响应丢失、晚到确认 | 未开始 |
| 5 | #851 bounded ExecutionQueue | FIFO metadata、attempt、timeout/retry reason、queue state | race、现有 turn/LLM/crash 语义兼容 | 未开始 |
| 6 | #868 Execution Cockpit | canonical runtime facts 的只读 timeline | 查询预算、授权、redaction、低基数 | 未开始 |
| 7 | #870 Coding Ops Recipes | versioned manifest、dry-run plan、effect-backed delivery | 幂等、权限、双数据库、非 DAG 边界 | 未开始 |
| 8 | #948 Capability Inventory | scope precedence、hash、safe materialization | safe path/size/XML/Windows、admin-gated promotion | 未开始 |

## Stage 1: Runtime Contract

### 进入条件

- #847、#848、#850、#852 已交付；
- 四类 Worker adapter 和现有 AEP v1 行为有回归测试；
- runtime metadata、audit 和 trace 已具备 agent/session/execution correlation 基线。

### 交付物

- #849 完整 runtime event contract；
- #946 `EffectiveRuntimePlan`、preflight、plan hash、source refs、warnings 和 observed bootstrap；
- #867 explicit env allowlist、compat/strict profile 和 isolation capability report；
- [Runtime Operations Contract](../superpowers/specs/2026-08-04-runtime-operations-contract.md) 定义的 redaction、fact separation 和 compatibility。

### 退出条件

- 等价 WS/REST 输入产生相同 canonical plan hash；
- doctor、worker start、admin diagnostics 和 recipe dry-run 使用同一 resolver；
- 四类 Worker 均能输出 declared/observed/enforced/partial/unavailable；
- 未观察到 worker/backend/artifact 时输出 `planned` 或 `unknown`，不输出 `enforced`；
- 旧客户端可忽略新 AEP event/metadata 并继续工作；
- 受影响 AEP/SDK、配置、session lifecycle 和 security 文档同步。

## Stage 2: Runtime Reliability

### 进入条件

- Stage 1 的 plan、observed 和 isolation vocabulary 已冻结；
- #878 的 execution identity、owner lease、fence 和 repairer 作为唯一 input execution 基线；
- connector/provider 对 acknowledgement、queryability 和 idempotency 的能力有明确报告。

### 交付物

- #877 operator escape hatch；
- #947 Gateway-owned `EffectLedger` 状态机和双数据库 schema；
- Cron/Webhook/message delivery durable vertical slice；
- claim、attempt、effect fact、business idempotency key、provider reference、evidence/confidence 和 unknown reason；
- lease/attempt cap/backoff/stop condition/operator action。

### 退出条件

- provider success 之后才写 effect ack；response 丢失保留 `unknown` 或可查询 pending；
- late completion 只能收敛同一 effect，不能产生第二个 effect；
- 重启、多实例竞争、DB 短暂失败、lease expiry、provider timeout/5xx、成功但响应丢失和人工确认有 fault-injection 测试；
- SQLite/PostgreSQL migration、conditional update 和 real-PG 多实例路径一致；
- effect runtime event、audit 和 diagnostics 默认脱敏；
- repair/reconcile 可暂停、可 fence、可限流并有 operator escape hatch。

## Stage 3: Runtime Operations

### 进入条件

- 至少一条 Gateway-owned external effect 在重启和多实例下完成 observed/reconciled 闭环；
- plan、execution、effect、observed 和 audit 具有稳定 correlation keys；
- Cockpit 数据查询具有 snapshot、retention 和数据库连接预算。

### 交付物

- #851 bounded `ExecutionQueue`；
- #868 Execution Cockpit 和 read-only admin runtime diagnostics；
- session execution list、per-execution timeline、policy summary、queue/effect/unknown/fence/reconcile refs；
- operator action authorization、audit 和 redacted response。

### 退出条件

- 同一 session 的输入顺序可解释、可审计、可恢复；
- queue 与现有 turn timeout、LLM retry、crash synthetic turn 和 eventstore capture 语义兼容；
- Cockpit 区分 input accepted、worker done、provider accepted、externally verified、unknown 和 reconciled；
- 查询具备分页、时间窗、payload 上限、workspace/session authorization 和 N+1 防护；
- metrics 不使用 plan hash、workspace、provider ref 等无界标签；
- API、WebChat、authorization、中文和英文 UI 文案测试通过。

## Stage 4: Conditional Platform Expansion

### 进入条件

- Stage 1–3 的 plan divergence、unknown age、reconcile result、delivery loss/duplicate、DB pool saturation 和定位能力具有持续运行证据；
- capability/recipe 场景复用现有 Gateway、Session、Worker、AEP、Execution、Effect、Audit 和 Observability；
- 单 Agent runtime + tools/context 无法满足的业务需求已有真实样本。

### 交付物

- #870 versioned Coding Ops Recipes；
- #948 scope-aware capability inventory 和 safe materialization；
- [Scope-aware Capability Inventory Contract](../specs/Scope-Aware-Capability-Inventory-Spec.md) 定义的 precedence、redaction、enforcement state 和 admin-gated promotion。

### 退出条件

- recipe dry-run 输出完整 EffectiveRuntimePlan 和 delivery contract；
- recipe external effect 使用 #947 的 business idempotency 和 unknown/reconcile 语义；
- capability inventory 对 inherited、shadowed、explicitly-cleared、materialized 状态可解释；
- unsafe path、oversized content、reserved XML、Windows injection 和 stale marker fail closed；
- SQLite/PostgreSQL 和四 Worker 行为一致；
- Recipes 不扩展为 workflow/DAG engine，Capability Inventory 不扩展为 marketplace 或 identity system。

## 暂缓能力与启动条件

| 能力 | 状态 | 启动条件 |
| --- | --- | --- |
| 跨 session 分布式 scheduler | 暂缓 | 单机 queue SLO 稳定，存在多个真实跨 session 调度场景 |
| Multi-agent workflow/DAG | 暂缓 | 单 Agent + tools/context 无法满足的业务证据成立 |
| 外部 memory product | 暂缓 | RuntimeContext/eventstore/worker history 无法满足明确恢复需求 |
| Agent/skill marketplace | 暂缓 | Capability Inventory、签名、promotion 和 supply-chain policy 已稳定 |
| SPIFFE/SPIRE workload identity | 暂缓 | 跨主机/跨集群信任与轮换成为硬需求 |
| Kubernetes operator / SaaS control plane | 暂缓 | 单 Gateway runtime、迁移、容量和租户边界具备生产证据 |

## 迁移与兼容契约

### AEP

- Kind、Data、JSON tag 和 metadata 只做增量兼容；
- 同步 Go SDK、TypeScript/Python/Java 示例 SDK、`docs/reference/{aep-protocol,events}.md` 和双向协议测试；
- 未知 event/field 不破坏旧客户端。

### Worker

- Worker interface 变化同步四 adapter、test mocks、`internal/worker/noop` 和 registry tests；
- 新能力明确 CLI、singleton 和 RPC Worker 的语义；
- Linux/macOS/Windows 的 env、path、process 和 injection 行为分别验证。

### Database

- SQLite/PostgreSQL migration 成对新增；
- schema 变化支持旧版本读取和失败恢复；
- condition update 包含 owner/run/fence/version 约束；
- migration interruption、multi-instance startup、connection jitter 和 pool recovery 有测试；
- 禁止 startup 无界 DDL 和人工直接修改生产库作为正常恢复路径。

### Privacy / Security

- durable facts 只保留 ID、hash、枚举、redacted reason 和 external reference；
- authentication、authorization、capability、isolation、credential injection 和 audit 分开验证；
- compat profile 带迁移告警，strict profile 才能 fail closed；
- 无 runtime evidence 的能力保持 `partial` 或 `unavailable`。

## 验证与发布闸门

每个交付切片同时满足：

- targeted tests 和受影响模块 `-race -count=1`；
- `make check`；
- `make docs-build`；
- Linux、macOS、Windows 受影响路径；
- SQLite/PostgreSQL 受影响 schema 和状态迁移；
- AEP/SDK/Worker contract 同步；
- redaction、negative boundary 和 fault-injection 测试；
- query/storage/retry/connection budget；
- read-only/shadow rollout、暂停、回退和 operator action；
- GitHub Issue、ROADMAP、Architecture、approved spec 和用户文档双向追踪。

## 交付索引

| 能力 | Issue | Spec | Stage |
| --- | --- | --- | --- |
| Runtime AEP Events | [#849](https://github.com/hrygo/hotplex/issues/849) | AEP reference docs | 1 |
| EffectiveRuntimePlan | [#946](https://github.com/hrygo/hotplex/issues/946) | [Runtime Operations Contract](../superpowers/specs/2026-08-04-runtime-operations-contract.md) | 1 |
| Isolation Profiles | [#867](https://github.com/hrygo/hotplex/issues/867) | Runtime Operations Contract | 1 |
| Fence Escape Hatch | [#877](https://github.com/hrygo/hotplex/issues/877) | execution lifecycle docs | 2 |
| EffectLedger | [#947](https://github.com/hrygo/hotplex/issues/947) | Runtime Operations Contract | 2 |
| ExecutionQueue | [#851](https://github.com/hrygo/hotplex/issues/851) | Runtime Operations Contract | 3 |
| Execution Cockpit | [#868](https://github.com/hrygo/hotplex/issues/868) | Runtime Operations Contract | 3 |
| Coding Ops Recipes | [#870](https://github.com/hrygo/hotplex/issues/870) | Runtime Operations Contract | 4 |
| Capability Inventory | [#948](https://github.com/hrygo/hotplex/issues/948) | [Scope-aware Capability Inventory Contract](../specs/Scope-Aware-Capability-Inventory-Spec.md) | 4 |
