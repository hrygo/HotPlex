# Durable Ingress 可靠性闭环设计

**日期：** 2026-07-14  
**状态：** 已批准，待实施  
**范围：** `internal/execution`、`internal/gateway`、`pkg/events`、`internal/observability`、SQLite/PostgreSQL migrations  
**关联：** issue #849、issue #851；后续解锁 issue #868

## 决策摘要

下一迭代不继续扩展 Agent Platform，而是先把已经交付的 durable input
ledger 做成可在生产多实例环境中成立的可靠性闭环：

1. 用 execution owner + lease 替换 Gateway 启动时对全库 `accepted`
   记录的无条件恢复。
2. 用有界终态修复器处理 `SetStatus` 暂时失败，避免记录永久停留在
   `accepted`。
3. 将 delivery status 与 runtime status 分离，并用数据库约束保证同一
   session 同时最多一个活跃 execution。
4. 只增加 `runtime.execution.started/completed/failed` 三个最小事件，让同一
   `execution_id` 能从输入 ACK 关联到最终 `Done/Error`。

该迭代优先保证“不重复产生副作用”和“结果最终可解释”。在不确定状态下，
系统宁可短暂拒绝同 session 的新输入，也不自动重放一个可能已经被 Worker
接受的输入。

## 背景与已证实问题

现有实现已经提供以下实际价值：

- 普通输入在调用 `Worker.Input` 前先写入 `execution_inputs`。
- 相同 `session_id + client_message_id + payload_hash` 的重试不会再次投递。
- `input.ack` 区分 `accepted`、`delivered`、`unknown`、`failed`。
- ledger 只保存 payload hash，不保存用户输入正文。

但当前实现还不能在声明支持的生产多实例场景中兑现这些保证。

### 多实例启动会误伤其他实例的活跃输入

`NewSQLStore` 在每个 Gateway 启动时执行：

```sql
UPDATE execution_inputs
SET status = 'unknown', error_code = 'GATEWAY_RESTART'
WHERE status = 'accepted';
```

在两个 Gateway 共用 PostgreSQL 时，实例 B 启动会把实例 A 正在投递的记录
改成 `unknown`。A 随后写入 `delivered` 时无法再满足
`WHERE status = 'accepted'`，最终产生“Worker 可能已经接受，但 durable state
为 unknown”的错误结果。滚动发布即可触发，不需要数据库故障。

### 终态写失败后会永久停留在 accepted

Gateway 在内存中先设置 intended status，再调用 `Store.SetStatus`。如果数据库
暂时不可写，当前客户端能收到 `unknown`，但数据库记录仍为 `accepted`。
请求随后被标记为 finalized，没有重试、修复队列或 reconciler。

同 ID 后续重试读取到旧的 `accepted` 后仍会被去重，因此不会重新投递，也不会
获得持久化终态。只有 Gateway 再次启动时的全表恢复才会把它改成 `unknown`，
而该恢复方式本身又不具备多实例安全性。

### execution_id 在 delivery ACK 后中断

`execution_id` 目前只进入 `input.ack` 和局部日志，没有进入 inbound event、
Bridge 的 Worker event 或最终 `Done/Error`。因此用户看到 delivery `unknown`
后，即使 Worker 稍后正常完成，系统也无法将结果与原 execution 对齐。

### 同 session 缺少活跃 execution 门

不同 `client_message_id` 可以为同一 session 创建多条输入记录。当前 Handler
仍直接调用 `Worker.Input`，没有持久化的单 session 活跃 execution 约束。
完成态关联若建立在无约束的 session→execution 内存映射上，会在并发输入时产生
歧义。

## 迭代目标

迭代完成后，一次普通输入必须满足：

1. **唯一：** 相同 client message ID 不会产生第二次 Worker 副作用。
2. **有主：** 每个活跃 execution 都属于一个 Gateway instance，并具有可过期
   的 lease。
3. **可修复：** 短暂数据库失败不会让终态永久停留在 `accepted`。
4. **可收敛：** delivery `unknown` 后收到晚到 `Done/Error` 时，runtime outcome
   可以收敛为 completed/failed。
5. **可关联：** `input.ack`、runtime events、eventstore、trace 和日志使用同一
   `execution_id`。
6. **可控：** 同一 session 同时最多一个 `pending/running` execution，不建立
   无界输入队列。
7. **不串线：** ambiguous execution 的旧 Worker 在产生晚到事件期间，不能让新
   execution 复用同一个 Worker/remote session。

## 非目标

- 不实现完整 `ExecutionQueue`、优先级、公平调度或分布式 scheduler。
- 不自动重投 delivery `unknown` 的输入。
- 不实现完整 runtime/security/context event taxonomy。
- 不实现 Execution Cockpit UI；本轮只提供后续查询所需的可信事实。
- 不引入 Temporal、LangGraph 或独立 workflow engine。
- 不持久化完整 prompt、tool arguments 或 transport payload。
- 不实现 AgentSpec、RuntimeContext、Coding Ops Recipes 或 worker env profile。

## 核心不变量

- `input.ack.status` 始终表示 **delivery status**；不得用 `completed` 等 runtime
  状态扩展或重解释现有字段。
- `runtime_status` 表示 Worker turn 的执行事实，与 delivery status 独立。
- duplicate 只允许读取和重放事实，绝不 resume session、取消 LLM retry、调用
  `Worker.Input` 或创建新的 execution。
- 只有 lease owner 可以推进活跃 execution；expired recovery 使用带 owner、
  status 和 lease 条件的原子更新。
- lease 比较使用数据库时间，不能依赖多个 Gateway 的本地时钟一致性。
- repair 只重试状态写入，绝不重试用户输入。
- 同一 session 最多一条 `runtime_status IN ('pending', 'running')` 的记录。
- ambiguous runtime terminal 必须留下 durable execution fence；fence 清除前不得
  在原 Worker/remote session 上投递新输入。
- `unknown -> completed/failed` 是允许的知识收敛；其他 terminal runtime 状态
  不回退。
- 任何日志、event、metric 都不得包含输入正文或 payload hash。

## 方案选择

### 采用：SQL owner lease + bounded repair + durable active gate

该方案复用现有 SQLite/PostgreSQL、execution ledger、Bridge 和 AEP 边界，不引入
新的控制平面。数据库负责跨进程所有权与单 session 活跃约束；Gateway 内存组件
只负责续租、终态修复和事件关联。

### 未采用：继续在启动时恢复所有 accepted

单实例下实现简单，但无法区分“上一个进程遗留”与“另一个存活实例正在处理”，
不适用于仓库已经声明支持的 PostgreSQL 多实例部署。

### 未采用：unknown 自动重投

`unknown` 的定义正是 Worker 可能已经接受输入。自动重投会恢复重复工具调用、
重复消息和重复外部副作用，违背 ledger 的核心目标。

### 未采用：直接建设完整 ExecutionQueue

完整队列会同时引入排队、取消、公平性、背压和调度策略，但不会自动解决现有
多实例恢复与终态落库缺陷。本轮只实现“不缓冲、单活跃”的 input gate。

## 数据模型

在 SQLite 和 PostgreSQL 的下一号 migration 中扩展 `execution_inputs`：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `owner_instance_id` | TEXT | 创建并拥有该活跃 execution 的 Gateway instance ID |
| `worker_run_id` | TEXT | Bridge 每次 attach fresh Worker 时生成的 opaque run ID |
| `lease_until` | INTEGER/BIGINT | 数据库时间语义下的租约到期时间 |
| `runtime_status` | TEXT | `pending/running/completed/failed/unknown` |
| `runtime_error_code` | TEXT | runtime 终态错误码；不保存错误正文 |
| `started_at` | INTEGER/BIGINT nullable | runtime started 时间 |
| `finished_at` | INTEGER/BIGINT nullable | runtime terminal 时间 |
| `fence_reason` | TEXT | 非空表示下一次输入必须先建立 fresh Worker session |

保留现有 `status` 字段和四个 delivery 值，不做重命名，避免破坏
`InputAckData` 和旧客户端。

新增索引：

```sql
CREATE INDEX idx_execution_owner_runtime
ON execution_inputs(owner_instance_id, runtime_status, lease_until);

CREATE UNIQUE INDEX idx_execution_one_active_per_session
ON execution_inputs(session_id)
WHERE runtime_status IN ('pending', 'running') OR fence_reason <> '';
```

SQLite 和 PostgreSQL 必须表达相同约束。Migration 测试必须证明同 session
第二条活跃 execution 被数据库拒绝，而不是依赖进程内 mutex。

### 旧数据迁移

旧记录没有可信 runtime completion 事实，迁移不得把 `delivered` 推断成
`completed`：

| 原 delivery status | 迁移后的 runtime status |
| --- | --- |
| `accepted` | delivery=`unknown`，runtime=`unknown`，error=`MIGRATION_RECOVERY` |
| `delivered` | `unknown` |
| `failed` | `failed` |
| `unknown` | `unknown` |

迁移后所有新记录必须具有非空 `owner_instance_id` 和 `runtime_status`。旧记录可以
保留空 owner，但永远不能被重新 claim 或投递。

## Gateway instance 与 lease

Gateway 每次启动生成一个进程生命周期内稳定的 UUID，作为
`owner_instance_id`，通过依赖注入传给 `execution.SQLStore` 和 reconciler。
该 ID 不复用、不写配置文件，也不是用户身份。

首刀使用以下内部常量，不新增用户配置面：

- lease TTL：60 秒。
- renew interval：20 秒。
- expired recovery interval：20 秒。

续租以 owner 为单位批量更新本实例的 `pending/running` 记录，避免每个 execution
各自启动 goroutine 或 ticker。没有活跃 execution 时不执行续租写。

启动和周期恢复只能执行等价于以下条件更新：

```sql
UPDATE execution_inputs
SET status = CASE WHEN status = 'accepted' THEN 'unknown' ELSE status END,
    runtime_status = 'unknown',
    error_code = CASE
        WHEN status = 'accepted' THEN 'GATEWAY_LEASE_EXPIRED'
        ELSE error_code
    END,
    runtime_error_code = 'GATEWAY_LEASE_EXPIRED',
    fence_reason = 'GATEWAY_LEASE_EXPIRED',
    finished_at = db_now,
    updated_at = db_now
WHERE runtime_status IN ('pending', 'running')
  AND lease_until <= db_now;
```

禁止恢复未过期 lease，也禁止再使用无 owner 条件的
`WHERE status = 'accepted'` 全表更新。

优雅关闭时，Gateway 尝试把自己拥有的活跃 execution 标记为 `unknown`，并在
关闭预算内 drain repairer。非优雅崩溃由 lease expiry 恢复。

## Delivery 与 runtime 状态机

### 创建和 dispatch

新普通输入的原子创建结果为：

```text
delivery_status = accepted
runtime_status  = pending
owner           = current gateway instance
worker_run_id   = current bridge worker run
lease_until     = db_now + 60s
```

创建前先按 `session_id + client_message_id` 判定 duplicate。若不是 duplicate，
但 partial unique index 已存在同 session 活跃 execution，则返回
`ErrSessionBusy`，由 Gateway 映射为现有 `SESSION_BUSY`。本轮不缓存第二条输入；
客户端可以在当前 execution 终止后用新 ID 重试。

在调用 `Worker.Input` 前，runtime 从 `pending` 转为 `running` 并产生
`runtime.execution.started`。随后：

| Worker.Input 结果 | delivery | runtime |
| --- | --- | --- |
| 返回 nil | `delivered` | 保持 `running` |
| 明确拒绝/不可用 | `failed` | `failed` |
| 调用超时、Worker 可能仍处理 | `unknown` | 保持 `running` |

delivery `unknown` 不释放 active gate，因为 Worker 仍可能产生 `Done/Error`。

### Bridge 终态

Bridge 的单一 `forwardEvents` 路径负责所有 Worker 的终态关联：

- `Done{Success:true}` → runtime `completed`。
- `Done{Success:false}` → runtime `failed`，使用已分类的最后错误码。
- Worker crash、turn timeout、明确 terminate → runtime `failed` 或 `unknown`，
  取决于系统能否证明执行没有成功完成。
- 非终态 `Error` 只记录最后错误，不提前释放 gate；等待 `Done` 或 Bridge 退出。

确定性的 runtime terminal transition 设置 `finished_at`、释放 active gate，并停止
该 execution 的 lease 续租。无法证明旧 Worker 已终止的 `unknown` transition
必须同时设置 `fence_reason`，不能直接放行新输入。

如果 lease expiry 已先把 runtime 标记为 `unknown`，同 execution、同
`worker_run_id` 的晚到 `Done` 可以执行 `unknown -> completed/failed`，用于收敛事实；
成功收敛同时清除 fence。任何晚到事件都不得触发第二次 input delivery。

### Ambiguity fence 与 fresh Worker

当 runtime 因 lease expiry、进程崩溃或无法证明 Worker 已停止而进入 `unknown`
时，该 execution 保持 `fence_reason`。新 client message ID 命中同 session fence
后返回内部 `ErrExecutionFenced`，Gateway 执行以下顺序：

1. 停止并隔离旧 Worker run；如果旧进程已不存在，该步骤幂等成功。
2. 启动 **fresh Worker session**，不得 resume 原 provider-private remote session。
3. 新 Worker readiness 成功后，使用带 execution ID 和 fence reason 条件的原子
   更新写入全新的 `worker_run_id` 并清除 fence。
4. 使用原 client message ID 重新执行 durable acceptance；acceptance 失败时不向
   fresh Worker 投递输入。

如果旧 execution 的晚到 `Done` 在步骤 2 之前到达，它可以先完成状态收敛并清除
fence，此时无需 fresh start。步骤 2 已开始后，旧 `worker_run_id` 的任何事件
只能更新旧 execution，不能进入新 Worker forward context。

fresh start 可能丢失 provider-private session context，这是为了避免晚到输出串入新
execution 的有意取舍。eventstore/turn history 保持不变，但本轮不承诺所有 Worker
都能把历史完整重注入 fresh session。

## ExecutionTracker

新增窄接口供 Handler 和 Bridge 共享，不让 Gateway 依赖 SQLStore 具体类型：

```go
type Tracker interface {
	Accept(ctx context.Context, req AcceptRequest) (*Record, bool, error)
	MarkRunning(ctx context.Context, executionID, ownerID, workerRunID string) error
	SetDelivery(ctx context.Context, executionID, ownerID string, status Status, code string) error
	FinishRuntime(ctx context.Context, executionID, ownerID, workerRunID string, status RuntimeStatus, code string) error
	ActiveBySession(ctx context.Context, sessionID string) (*Record, error)
	FenceBySession(ctx context.Context, sessionID string) (*Record, error)
	ClearFenceAfterFreshStart(ctx context.Context, executionID, reason, freshWorkerRunID string) error
}
```

具体方法名可在实现计划中按 Go 命名规范调整，但接口必须保持窄职责：持久化状态
推进和查询当前活跃 execution，不承担 Worker 调度或 UI 查询。

Handler 接受普通输入后，将 execution identity 注册到 Bridge 的 per-session
forward context。Bridge 只从该 context 或 durable `ActiveBySession` 获取 active
execution，不从事件正文猜测关联关系。

Bridge 每次 attach Worker 都生成新的 `worker_run_id`。fresh start 必须替换旧 run
关联；旧 `forwardEvents` 的晚到事件只有 `worker_run_id` 匹配时才能推进终态。

## 终态修复器

数据库终态写失败时，Gateway 将 immutable repair intent 送入有界内存队列：

```go
type RepairIntent struct {
	ExecutionID string
	OwnerID     string
	Kind        RepairKind
	Status      string
	ErrorCode   string
}
```

规则：

- repair queue 容量取 Gateway 全局 session pool 上限的两倍，最小 2，以容纳同一
  execution 的 delivery/runtime 两类 intent；不建立无界队列。
- 同一 execution + kind 的 intent 合并，terminal runtime intent 优先于早期状态。
- 重试使用指数退避：100ms 起步，最大 5 秒，单 intent 最长 30 秒。
- repair 期间继续续租，避免另一个实例提前执行 expired recovery。
- duplicate 命中本实例尚未修复的 execution 时，可以使用内存 intent 返回更准确
  的 ACK；不得重新调用 Worker。
- 30 秒内仍失败时停止主动修复，保留指标和错误日志，由 lease expiry 将事实收敛
  为 `unknown`。
- shutdown 最多 drain 5 秒；未完成 intent 依赖 lease recovery。
- enqueue 已满时，执行一次短超时同步重试；仍失败则记录 dropped 指标并依赖
  lease recovery，不能阻塞 Gateway 主消息循环。

repairer 解决短暂数据库故障，不承诺在进程和数据库同时永久失效时保存 intended
terminal outcome。该场景的正确 durable 结果是 `unknown`，而不是猜测
`delivered/completed`。

## Runtime events 与关联

新增最小 AEP event kinds：

```text
runtime.execution.started
runtime.execution.completed
runtime.execution.failed
```

事件 Data 至少包含：

```go
type RuntimeExecutionData struct {
	ExecutionID string         `json:"execution_id"`
	Status      string         `json:"status"`
	ErrorCode   ErrorCode      `json:"error_code,omitempty"`
	StartedAt   int64          `json:"started_at,omitempty"`
	FinishedAt  int64          `json:"finished_at,omitempty"`
}
```

所有 runtime event 和对应的现有 `Done/Error` Envelope metadata 使用统一 key：

- `execution_id`
- `worker_type`
- `workspace_id`（存在时）
- `trace_id` / `span_id`（observability 启用时）

`CaptureInbound` 必须保留 `execution_id`，eventstore 通过现有 AEP 事实流持久化
runtime events，不增加第二套事件总线。旧客户端必须继续安全忽略未知 event kind。

本轮不增加按 execution 查询的 Admin API；但持久化结构必须使 issue #868 后续
可以构建 timeline，而无需重新解释或修复历史状态。

## 可观测性

新增低基数指标：

- execution accept、duplicate、conflict、session-busy 计数。
- delivery outcome 和 runtime outcome 计数。
- delivery latency、runtime duration histogram。
- lease renew failure、expired recovery 计数。
- repair backlog gauge、attempt、success、timeout、drop 计数。

允许的 label 限于 `worker_type`、`delivery_status`、`runtime_status`、`reason`。
严禁将 `session_id`、`execution_id`、`user_id`、payload hash 作为 metric label。

日志应包含 `execution_id`、`session_id`、`owner_instance_id` 和操作名，但不包含
content、metadata 原文或 payload hash。

## 错误处理

- acceptance 写失败：继续 fail-closed，不调用 Worker。
- active gate 冲突：返回现有 `SESSION_BUSY`，不创建第二条 execution。
- delivery/runtime terminal 写失败：发送符合当前知识的 ACK/event，进入 repairer。
- lease renew 失败：记录指标和 warning，不立即杀死 Worker；恢复后续租，或由 lease
  expiry 收敛为 `unknown` 并设置 fence。
- expired recovery：只推进未过期保护条件已失效的记录，不触发 input redelivery。
- late Done：只允许同 execution、同 `worker_run_id` 的 knowledge refinement。
- execution fence：只有晚到终态收敛或 fresh Worker readiness 后的条件更新可以
  清除。
- repair queue 满：不阻塞主循环，不自动重放，最终依赖 lease expiry。

## 安全与隐私

- 新字段只包含 opaque ID、状态、时间和枚举错误码。
- repair intent 不包含输入正文、tool arguments、platform payload 或凭据。
- runtime events 的 metadata 经过现有审计/日志脱敏边界。
- `payload_hash` 不进入日志和 metrics。
- Admin 查询能力不在本轮开放，因此不新增授权面。

## 兼容性与发布

### 协议兼容

- 现有 `input.ack` shape 和四个 delivery status 不变。
- 新 runtime event 为 AEP v1 additive event；旧客户端忽略即可。
- Go、WebChat/TypeScript、Python、Java 客户端都必须通过 unknown-event
  compatibility corpus。

### 数据库兼容

Migration 只增加字段和索引，不删除现有列。SQLite 和 PostgreSQL 必须同步维护。

旧二进制仍包含无 owner 条件的启动恢复 SQL，因此多实例环境不能在存在新 lease
记录时自动回滚到旧版本。首次生产发布采用受控升级：

1. 暂停新 ingress 或 drain 所有旧 Gateway。
2. 停止旧实例。
3. 执行 migration。
4. 启动全部新实例并验证 owner lease。
5. 恢复 ingress。

这是本迭代接受的显式运维代价。后续若要求零停机升级，应单独设计兼容桥接版本，
不能在本 spec 中隐式宣称支持旧、新二进制混跑。

### 回滚

代码回滚只能在确认没有 `pending/running` v2 execution 后进行。数据库 down
migration 不作为常规回滚手段；优先前滚修复。需要紧急回滚时先 drain ingress，
将活跃记录收敛为 `unknown`，再停止所有新实例并回滚二进制。

## 测试与验收

### Store 与 migration

- SQLite 和真实 PostgreSQL round-trip 覆盖新增字段和所有状态推进。
- 两个 SQLStore 共用 PostgreSQL：实例 B 启动不得改变实例 A 的未过期记录。
- expired lease 被一个且仅一个 reconciler 收敛为 `unknown` 并设置 fence。
- owner 不匹配、lease 未过期、runtime 已 terminal 时条件更新均不生效。
- partial unique index 拒绝同 session 第二条活跃 execution。
- ambiguous terminal 的 fence 同样阻止第二条 execution，直到晚到 Done 或 fresh
  Worker 流程清除。
- migration 不把历史 `delivered` 伪造为 `completed`。

### Gateway 与 repair

- `SetDelivery`、`FinishRuntime` 分别注入连续失败后恢复，30 秒内成功修复。
- repair 期间 duplicate 不调用 Worker，且不会永久只看到旧 `accepted`。
- repair queue 满、shutdown drain、lease expiry fallback 均有确定性测试。
- 禁止使用 `time.Sleep` 等待异步结果；使用 channel 或 `require.Eventually`。

### Runtime correlation

- 同一 `execution_id` 贯穿 input、started、Done/Error、runtime terminal event。
- Claude Code、Codex CLI、OpenCode Server、ACP 共用 Bridge contract tests。
- delivery timeout 后晚到 Done 能执行 `unknown -> completed` 收敛。
- reset 后旧 `worker_run_id` 的晚到 Done 不能完成新 execution。
- fenced session 的下一条新输入必须 fresh start，不能 resume 原 remote session。
- 同 session 并发输入最多一个 `Worker.Input` 被调用。

### 质量门禁

必须通过：

```bash
go test -count=1 -race ./internal/execution ./internal/gateway ./internal/session ./internal/worker/...
make check
make docs-build
```

另需运行真实 PostgreSQL 多实例集成测试，以及 Go/TypeScript/Python/Java AEP
unknown-event compatibility corpus。

### 性能验收

- 无活跃 execution 时，lease runner 不产生周期数据库写。
- 有活跃 execution 时，每个 Gateway 每个 renew interval 最多一次批量续租事务。
- 相比当前 durable ingress 基线，普通输入 acceptance + dispatch 的 P95 额外延迟
  不超过 10%；测试报告必须同时给出绝对毫秒值，避免低基数误导。
- repair queue 和 active execution 内存占用受 session pool 上限约束。

## 迭代代价

### 工程容量

预计完整范围总投入 **18–23 工程人日**：

| 工作项 | 预计投入 |
| --- | ---: |
| 双数据库 migration、owner lease、条件状态推进 | 4–5 人日 |
| repairer、续租/恢复生命周期、shutdown | 3–4 人日 |
| 单 session gate、ambiguity fence、fresh Worker | 4–5 人日 |
| Bridge correlation、runtime events | 3–4 人日 |
| PostgreSQL 多实例、故障注入、SDK/文档/门禁 | 4–5 人日 |

两名熟悉 Go runtime/store 的工程师在一个两周迭代内完成属于紧排期，几乎没有
review 与返工余量；更安全的配置是三名工程师，或将 P0 owner lease + repairer
作为第一交付切片、gate/correlation 作为紧随其后的第二切片。单人实施更接近
4 个自然周。并发实施必须先冻结 migration 和状态机契约，否则 store 与 Gateway
两条线会互相返工。

### 架构复杂度

系统新增 owner、lease、delivery/runtime 双状态机、repair queue 和 late-event
收敛规则。它们提高了故障正确性，也增加了：

- Store 接口与测试矩阵。
- shutdown/restart 的生命周期依赖。
- SQLite/PostgreSQL 语义一致性维护成本。
- 未来修改 `Done/Error`、reset、retry 时需要维护 execution invariant 的成本。

因此本轮必须限制 event taxonomy 和 queue 能力，避免同时引入更多控制面概念。

### 运行时成本

- 每个活跃 Gateway 每 20 秒至多一次批量 lease 续租事务。
- 每次 execution 增加 runtime 状态更新和 1–2 条最小 runtime event。
- `execution_inputs` 每行增加约百字节量级的 ID、状态和时间字段。
- PostgreSQL 多实例增加 expired recovery 条件扫描，因此需要 owner/runtime/lease
  组合索引。

这些开销必须通过性能验收验证；若 P95 增幅超过 10%，不能以“可靠性功能”名义
直接接受，需要先优化批量续租或事件写入。

### 可用性取舍

这是本迭代最重要的用户可见代价：

- 当 execution outcome 不确定时，同 session 新输入先被 `SESSION_BUSY` 阻塞；
  repair 通常在 30 秒内收敛，owner 失联后至少等待 60 秒 lease expiry。
- lease expiry 后如果旧 Worker 是否仍执行无法证明，下一条输入会触发 fresh start；
  provider-private 上下文可能丢失，直到未来 RuntimeContext 能力补齐。fresh start
  失败时 fence 会继续保留，因此极端情况下阻塞时间没有固定上限。
- 系统选择短暂降低可用性，而不是冒险接受可能造成重复副作用的新输入。
- 首次多实例生产发布需要受控 drain，不能承诺旧、新二进制零停机混跑。

该取舍是有意的：对会执行 shell、写文件、调用外部 API 的 Agent Gateway，
“可能重复执行”比“短暂不能发送下一条消息”代价更高。

### 机会成本

本迭代会主动延期：

- issue #847 AgentSpec resolver。
- issue #867 worker environment allowlist/profile。
- issue #868 Execution Cockpit。
- issue #870 Coding Ops Recipes。
- 完整 issue #851 ExecutionQueue 和 issue #849 event taxonomy。

其中 #867 的安全价值最高，应成为下一候选；若存在完全独立的安全实施通道，可以
并行，但不得占用本迭代 PostgreSQL 多实例与故障注入验证容量。

## 交付顺序

1. **Store contract：** migration、owner/lease、runtime status、原子状态推进。
2. **Recovery contract：** 批量续租、expired recovery、graceful shutdown。
3. **Repair contract：** 有界修复、退避、指标、duplicate overlay。
4. **Gateway contract：** active gate、ExecutionTracker、Bridge `worker_run_id` 关联。
5. **Protocol contract：** 三个 runtime events、metadata、兼容测试。
6. **验证：** SQLite/PostgreSQL、双实例、四 Worker、故障注入、性能与文档门禁。

前一阶段的 contract tests 未通过时，不进入后一阶段。不得先做 UI 或完整 queue
来掩盖底层状态不可靠。

## 完成定义

- 滚动启动模拟中，新实例不会修改其他存活实例的未过期 execution。
- 短暂数据库不可写后，delivery/runtime terminal 状态在 30 秒内修复。
- 相同 `session_id + client_message_id` 在所有故障注入场景下最多调用一次
  `Worker.Input`。
- 同 session 同时最多一个 active execution。
- ambiguous execution 不会与下一条输入复用同一 Worker/remote session；fence 只能
  通过晚到终态或 fresh Worker readiness 清除。
- 100% 普通输入在测试矩阵中能通过同一 `execution_id` 关联到 runtime terminal。
- delivery `unknown` 后的晚到 Done 可以收敛，但不会触发自动重投。
- SQLite、真实 PostgreSQL、四 Worker contract、race、`make check`、
  `make docs-build` 和多 SDK compatibility 全部通过。
- spec 中的非目标没有被实现范围偷偷扩大。
