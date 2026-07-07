# 用户行为审计系统设计 (User Behavior Audit Spec)

| 项 | 值 |
|---|---|
| 状态 | Implemented — P1/P2 complete; final follow-ups in `User-Behavior-Audit-Final-Followups-Spec.md` |
| 日期 | 2026-07-03 |
| 适用版本 | hotplex ≥ v1.31 |
| 关联调研 | events/sessions/turns 数据模型 · admin_audit · observability · 用户身份模型 |

---

## 1. 背景与目标

hotplex 需要审计每个用户的行为,支撑**内部调查**与**合规/取证**。当前不支持(见 §3)。

**目标**
- 按 user(平台原生 ID 维度)检索"某人某时段做了什么":认证、会话、消息、工具调用、管理操作
- 不可篡改、可追溯(合规取证)
- 3 年保留
- 提供告警扩展点(`AlertSink`,具体规则 / 通道另立 spec)

**非目标(v1)**
- 把 `ou_…`/`U…`/UUID 跨通道归一到同一真人(列 v2,Phase 3)
- 业务 BI / 用户画像聚合
- 替换 events/turns(它们仍是完整对话内容载体)

---

## 2. 关键决策(已确认)

| 决策 | 取值 |
|---|---|
| 驱动 | 内部调查 + 合规/取证 |
| 归因维度 | 平台原生 ID(`ou_…`/`U…`/UUID 各自独立审计) |
| 消息内容 | 独立审计表存摘要+sha256;全量靠 `event_ref` 引用 events/turns;敏感行为全量直存 |
| 保留期 | 3 年 |
| 不可变性 | append-only + hash chain(不需 WORM/外部存储) |
| 实时告警 | 需要(本 spec 只定义可扩展 `AlertSink` 接口,不实现具体规则/通道) |
| 冗余治理 | 接受为"程序简单/查询高效"的良性冗余;只清恶性(死视图/死字段/空文件/废弃配置) |

---

## 3. 现状调研结论(为什么必须新建)

三路调研合成:

- **events 表无 user_id**,靠 `session_id` 间接归因,**73% 孤立**(11,869 行 / 624 个 dead session_id)。session 删除不级联(`DeleteBySession` 是死代码,零调用方)。
- **turns.user_id 存在**(904/1214 填充)但 split-brain(平台 native id 与 UUID 混杂),**67% 孤立**;`TurnQuerier` 接口只暴露 by-session,无 by-user。
- **sessions.user_id split-brain**:messaging 存平台 native id、webchat 存 `users.id` UUID,**无 FK**;`owner_id` 全表为空(死字段);`v_turns*` 视图因此 user_id 全空,**不可用**。
- **chat_access_events** 仅 feishu 写入且数据来源不一致,不可作审计。
- **admin_audit** 仅 slog JSON 文本、仅覆盖 admin 写操作,**不入库、无 retention**。
- **背压静默丢弃**:`collector` channel 满时 events/turns 直接丢(仅 `dropped` 计数)——审计完整性缺口。
- **指标 / trace 零 user 维度**;**无 by-user 查询端点**(仅 `/admin/sessions?user_id=` 返回元数据)。
- 跨通道归一机制不存在(`user_identities` 是 OIDC 专用,空表)。

**结论**:无可复用的"用户行为审计载体",必须新建独立审计表;历史 73% 数据无法回填,**只向前治理**。

---

## 4. 方案选型

| 方案 | 思路 | 优 | 劣 |
|---|---|---|---|
| **A. 独立审计表 + 双写**(选) | 新建 append-only `user_activity` 表,关键行为点异步双写(独立通道),补 by-user 检索 | 审计与业务数据分离(合规);独立 retention/不可变;低侵入主路径;覆盖认证+admin(不只 AEP 流) | 双写一致性/性能成本;新表需建检索/UI |
| B. events 加 user_id 列 | 给 events/turns 加 user_id 冗余,激活 `DeleteBySession` | 复用现有表 | schema 改动大;历史无法回填;与 session GC 仍耦合;events 无 user_id 是设计非偶发 |
| C. 旁路订阅 AEP 流 | collector 加 audit sink 订阅事件流写审计表 | 解耦最好 | 只覆盖 AEP 流内行为,认证/admin 不在流内,覆盖不全 |

**选 A**。理由:① 合规最佳实践要求审计日志与业务数据分离、独立 retention、不可篡改;② events 73% 孤立已证明"用业务表做审计"不可靠;③ A 覆盖认证/授权/admin(不只对话流);④ 对 hotplex 主路径低侵入(双写异步)。

---

## 5. 详细设计

### 5.1 数据模型 — `user_activity` 表

```sql
CREATE TABLE user_activity (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts            INTEGER NOT NULL,           -- UTC ms
  user_id       TEXT NOT NULL,              -- 平台原生 ID(ou_/U.../UUID)
  user_id_type  TEXT NOT NULL,              -- 'platform' | 'registered' | 'anonymous' | 'system'
  platform      TEXT NOT NULL,              -- webchat|feishu|slack|cron|admin|api
  session_id    TEXT,                       -- 可空(admin/api 无 session)
  action        TEXT NOT NULL,              -- enum,见 5.2
  resource_type TEXT,                       -- session|message|tool|credential|config|...
  resource_id   TEXT,
  outcome       TEXT NOT NULL,              -- success|failure|denied
  detail_json   TEXT NOT NULL,              -- 白名单字段(5.3/5.9)
  event_ref     TEXT,                       -- 指向 events.id 或 turns.id(全量下钻)
  ip            TEXT,
  user_agent    TEXT,
  prev_hash     TEXT NOT NULL,              -- hash chain
  self_hash     TEXT NOT NULL               -- sha256(prev_hash || canonical(其余列))
);
CREATE INDEX idx_ua_user_ts   ON user_activity(user_id, ts);
CREATE INDEX idx_ua_ts        ON user_activity(ts);
CREATE INDEX idx_ua_action_ts ON user_activity(action, ts);
```

- append-only:应用层 + DB trigger 禁 UPDATE/DELETE(见 5.5)
- PG 版本走 `dbutil`(Dialect / Rebind / BoolValue)抽象,迁移用现有 migration 框架

### 5.2 审计事件分类与记录点

| 类别 | action | 记录点(代码) | outcome |
|---|---|---|---|
| 认证 | `auth.login` / `auth.token_validated` / `auth.apikey_used` | `internal/security/auth.go` AuthenticateRequest;`gateway/conn.go` authenticateInit | success/failure |
| 会话 | `session.create` / `session.terminate` / `session.delete` | `session/manager.go` CreateWithBot/DeleteTerminated;`gateway/api.go` DeleteSession | success |
| 消息 | `message.inbound` | `gateway/bridge_forward.go` CaptureInbound | success |
| 工具调用 | `tool.call` | bridge 解析 tool_call(worker_cmds);敏感名(Bash/Write/Edit/网络) | success/failure |
| 管理 | `admin.<action>` | 复用 `internal/admin/audit.go` AdminAudit,迁移写入本表 | success/failure |
| 授权 | `auth.denied` | middleware(`admin/AdminAPI.Middleware`;`requireAdmin`) | denied |
| 配置变更 | `system.audit_config_changed` | audit 配置加载 / reload 点 | success |

### 5.3 内容策略(摘要 + 引用复用全量)

呼应决策 3(独立表则摘要+hash):

- 审计表 `detail_json` 存**摘要 + sha256**(永久可查,3 年)
- `event_ref` → `events.id` / `turns.id`,session 存活时下钻**复用** events/turns 完整内容(不在审计表重存全量)
- **敏感行为**(失败认证、敏感工具调用、denied、crash/timeout)在 `detail_json` 内**直接存全量上下文**(量小且重要,不依赖 events)
- 权衡:events/turns 现 retention 30 天,到期后普通消息只剩摘要。新增 `audit.full_content_retention` 把 events/turns 延长(默认 90 天)供下钻;敏感行为已全量入审计表,不受影响
- **wiring**:`effective events retention = max(events.retention, audit.full_content_retention)`(audit 启用时);注意此延长会同时影响**非审计用途**的 events(副作用已接受,换取下钻完整性)
- `event_ref` 预期会随 events/turns TTL(90 天)后**悬空**——届时审计行只剩摘要 + sha256(仍满足"是否做过某行为"的审计);需长期全量回溯的敏感行为已全量直存 `detail_json`,不依赖 `event_ref`。悬空是设计预期,非异常

### 5.4 归因

`user_id` = 平台原生 ID(NOT NULL,见 §5.1);`user_id_type` + `platform` + `session_id` 辅助。bot 不作主体(归人,不归 bot),用 `(user_id, bot_id)` 区分"谁通过哪个 bot"。

**各通道填充规则**:

| platform | user_id_type | user_id 来源 |
|---|---|---|
| webchat | registered | `users.id`(`auth.AuthenticateRequest`) |
| feishu | platform | open_id(`ou_…`) |
| slack | platform | user_id(`U…`) |
| cron | platform | `job.OwnerID` |
| api | registered | `api_key_users` → `users.id` |
| admin | registered | admin actor `users.id` |
| (认证失败/匿名) | anonymous | `"anonymous"`(占位)+ **必填** ip / user_agent |

> 认证失败(错密码 / 伪造 token / 匿名攻击)时无已知用户——`user_id` 用 `"anonymous"` 占位满足 NOT NULL,`user_id_type=anonymous`,**必带 ip + user_agent** 以支撑暴力破解检测(OWASP A09;先例 `internal/admin/audit.go:57` admin_audit 已用 `"anonymous"`)。
> - **`tool.call` 回溯**:worker 内执行工具时无原始 user_id——写入点在 bridge 解析 tool_call 处,沿 `session_id → sessions.user_id` 回溯取归因。
> - **cron 系统 / 无 owner 任务**:`job.OwnerID` 为空时用 sentinel `cronjob:{id}`(`user_id_type=system`),归因为"该定时任务"而非个人。

### 5.5 不可变性与完整性(hash chain)

- `self_hash = sha256(prev_hash || canonical_json(除 prev_hash/self_hash 外所有列))`;`canonical_json` 用 Go `encoding/json`(map key 默认字典序)+ 整数/NaN 处理规则固定(RFC 8785 JCS 思路),避免双实现歧义
- 首条 `prev_hash = ""`(genesis)
- 写入:mutex 临界区为"读 chain tail → 逐条算 hash → 提交"整段;**批量提交**时在单事务内按顺序计算(批内第 i 条 `prev_hash` = 第 i-1 条 `self_hash`,批首条 = 当前 tail),整批一条多值 INSERT——chain 串行化只在临界区,不阻塞整周期。PG 多实例用 advisory lock 串行化 tail。
- 验证:`audit_chain_verification` 周期任务从最新 checkpoint 起点向后顺序重算,断裂即告警 + `hotplex.audit.chain_breaks` 指标
- **篡改防护**:DB trigger **禁止 UPDATE**(SQLite `RAISE(ABORT)`;PG `BEFORE UPDATE`)。DELETE **不**用 trigger 强禁(避免与 GC 冲突),改为:① 应用层只有 audit GC loop 一处合法 DELETE;② chain 校验检测任何非法删除(中间行被删 → 链断 → 告警)
- **裁剪与重锚(rebase)**:GC 删最旧一段前,把该段最后一行的 `self_hash` 持久化为 checkpoint(新表 `audit_chain_checkpoints`:`pruned_at` / `last_self_hash` / `next_id`);该 checkpoint 作为**新 epoch 起点**——验证器不再要求"首行 prev_hash 为空",而是接受"最近 checkpoint 的 `last_self_hash` 为当前 genesis";剩余首行 `prev_hash` 必须等于该 checkpoint 的 `last_self_hash`(衔接校验),否则告警
- **诚实声明**:裁剪后 tamper-evidence 从"自原始 genesis 连续"**降级**为"自最近 checkpoint 连续"——checkpoint 之前的历史无法再密码学验证(接受)。checkpoint 行自身同样受 UPDATE trigger + 应用层保护

### 5.6 告警扩展接口(决策 6 — 可扩展,不内置具体能力)

本 spec **只定义告警的扩展点与数据契约,不实现具体规则 / 阈值 / 通道**。规则引擎、去噪、告警状态机、多通道投递等具体告警能力作为**独立子系统另立 spec**,不在本 spec 范围。

**接口契约**

```go
// AuditEvent 是投递给 AlertSink 的只读快照(数据契约)。
type AuditEvent struct {
    EventID      string         // 稳定唯一 ID(UUIDv7),供 sink 去重/幂等(后续规则引擎必需)
    Ts           time.Time
    UserID       string
    UserIDType   string
    Platform     string
    SessionID    string
    Action       string
    ResourceType string
    ResourceID   string
    Outcome      string
    Detail       map[string]any
    EventRef     string         // 指向 events.id/turns.id,sink 可下钻全量
    IP           string
    UserAgent    string
}

// AlertSink 是审计告警的扩展点。
// 实现方自行注入规则与通道;审计核心对 sink 无任何依赖。
type AlertSink interface {
    // OnAuditEvent 必须非阻塞(内部异步);其失败不得影响审计写入主路径。
    OnAuditEvent(ctx context.Context, e AuditEvent) error
}
```

**扩展机制**(仿 hotplex 既有惯例:`worker.Register` / `platform_registry`)

- `audit.RegisterSink(name, factory)` 注册 sink 工厂;启动时按配置实例化
- 通过 `GatewayDeps` 依赖注入,符合既有 DI 模式
- v1 内置仅两个 sink:
  - `NoopSink`(默认,不输出)
  - `LogSink`(写 slog,供调试)
- 后续 webhook / feishu / slack / SIEM / 规则引擎均作为 `AlertSink` 实现接入,**不改审计核心、不改本 spec**

**调用路径**:`audit_collector` 在持久化审计行后,以 fan-out + 非阻塞方式调用已注册 sink;任一 sink 阻塞或出错不影响其他 sink 与主路径,错误仅计入 `hotplex.audit.sink_failures` 指标。

### 5.7 保留与 GC

- `audit.retention` 默认 **3 年**(可配)
- 独立 GC loop 删 `ts < now-retention`;裁剪按 §5.5 checkpoint 机制保证剩余链可验证
- 可选冷归档(导出 JSON 到外部存储)—— v1 可选

### 5.8 检索端点与 UI

- `GET /admin/users/{id}/activity?from=&to=&action=&outcome=&limit=&offset=`
- `GET /admin/activity?user_id=&...`(admin 全局时间线)
- 导出:`?format=json|csv`
- admin UI 用户活动时间线(复用 `/admin/workspaces` 控制台模式,issue #807)

**鉴权与治理**:所有 `/admin/*` 检索与导出端点走 `AdminAPI.Middleware`(Bearer + scope,cookie fallback,issue #788 模式),仅 admin 角色可访问;**每次导出**记 `user_activity`(`system.audit_export`,含导出者 / 目标 user / 范围);CSV/JSON 导出对高敏 PII 列默认掩码,需显式 `?include_pii=true` 才全量;端点加 rate-limit 防批量拖库。

### 5.9 隐私与数据最小化

- `detail_json` 字段白名单(结构化)
- **禁录**:凭证、password、API key 明文、session token、完整 PII(身份证/手机号)
- API key 记前缀 + 掩码(`hpk_a4c1…`)
- 消息正文:摘要(前 N 字 + PII 脱敏)+ sha256;全量靠 `event_ref` 或敏感行为路径
- 符合 OWASP Logging Cheat Sheet "What Not to Log"

### 5.10 性能与背压(补 events 静默丢弃缺口)

- 独立 `audit_collector`(仿 `eventstore/collector.go` 批写模式)
- channel 满时 **spill 到磁盘队列(WAL + fsync)**,不丢(审计不能丢)
- 批量异步写(1s / 100 批)
- 写入失败 → 告警 + 重试(宁可短暂阻塞审计点,也不静默丢)
- hash chain 写入用独立 mutex 序列化(避免 self_hash 并发竞争)

---

## 6. 与现有系统关系(复用)

- `admin.AdminAudit(actor, action, target, result)` 模式 + 动作枚举 → 扩展,迁移写入本表
- `eventstore/collector.go` 批写模式 → 仿建 `audit_collector`
- `session.Manager.List` by-user → admin adapter 透传
- 告警扩展点:`AlertSink` 接口 + `RegisterSink` 注册式(仿 `worker.Register` / `platform_registry`);具体通道(webhook/feishu/slack/SIEM)与规则引擎后续作为接口实现接入,不在本 spec
- `observability` 指标 → 新增 `hotplex.audit.events`(by action/outcome)、`hotplex.audit.chain_breaks`、`hotplex.audit.spill`、`hotplex.audit.write_failures`、`hotplex.audit.sink_failures`

---

## 7. 迁移与历史数据

- 历史 73% 孤立 events/turns **不回填**(session 已删,无 user 归因)
- 新表从上线起完整
- `admin_audit` slog 逻辑迁移为双写 `user_activity`(兼容期保留 slog),之后下线 slog-only 路径
- `DeleteBySession` 死代码:**保持现状**(events/turns 仍独立 TTL);审计不依赖它——审计表自带 user_id,session 删除**不影响**审计归因(这正是新表核心价值,根除"session 没了归因就断")

---

## 8. 配置项

```yaml
audit:
  enabled: true
  retention: 26280h              # 3 年
  full_content_retention: 2160h  # events/turns 延长(90 天)供下钻
  chain_verify_interval: 1h
  collector:
    channel_cap: 4096
    spill_dir: ~/.hotplex/data/audit-spill
    batch_interval: 1s
    batch_size: 100
  sinks:                          # 可扩展告警 sink 列表(schema 由 sink 实现定义)
    - name: noop
      type: noop                  # v1 内置:noop | log;后续可扩展 webhook|feishu|slack|siem|rule-engine
      config: {}
```

**meta-audit**:`audit.enabled` / `retention` / sink 列表的变更纳入审计(写 `user_activity` `action=system.audit_config_changed`);`enabled` **不可热关**(需重启 + sign-off),防管理员在审计盲区操作。

---

## 9. 测试策略

遵循项目规范(table-driven + `testify/require` + `t.Parallel()`,单模块 ≤5s,`-count=1 -race`,禁 `time.Sleep` 用 `require.Eventually`/channel):

- hash chain:批量写入顺序化、并发安全、UPDATE 篡改检测、**裁剪后剩余首行 prev_hash 与 checkpoint 衔接、checkpoint 之后链可验证**
- collector spill:channel 满 → 落盘 → 恢复重放,**零丢失**
- 写入点:各 action 覆盖 success/failure/denied
- retention GC + 裁剪事件
- sink 收到事件(LogSink 落 slog);规则引擎另立 spec,不在本测
- by-user 检索分页/过滤
- 端到端:发消息 → 审计记录 → 检索 → 告警

---

## 10. 分期交付

- **Phase 1(核心)**:表 + migration + hash chain(checkpoint)+ 独立 collector(spill)+ 写入点(认证/会话/消息)+ retention(3 年)+ by-user 端点 + chain verify + **`AlertSink` 接口 + `NoopSink`(collector 预留 sink fan-out 钩子)**
- **Phase 2(覆盖 + 告警 + UI)**:工具调用审计 + admin 操作迁移 + **`LogSink` + `RegisterSink` 注册机制 + 告警配置** + admin UI 时间线 + 导出
- **Final follow-ups(PR #854)**:admin 活动时间线 UI + `audit_identity_links` 显式跨通道身份链接 + `principal_user_id` 查询展开 + identity-link admin API + 文档化 SIEM/冷归档边界

> 具体告警能力(规则引擎 / 多通道投递 / 去噪 / 告警状态机)**另立 spec**,不在本 spec 范围。本 spec 仅提供 `AlertSink` 扩展点与数据契约,保证审计核心稳定、告警子系统可独立演进。

---

## 11. 实施状态(Implementation Status)

> **当前状态**:Phase 1 PR [#844](https://github.com/hrygo/hotplex/pull/844) 已合并,Phase 2 PR [#845](https://github.com/hrygo/hotplex/pull/845) 已合并,final follow-ups PR [#854](https://github.com/hrygo/hotplex/pull/854) 已创建并通过质量门禁。

### P1 已完成 ✅

| 组件 | 状态 | 落地位置 |
|------|------|----------|
| Migration 023 (SQLite + PG) | ✅ | `internal/session/sql/migrations/023_user_activity.{sql,pg.sql}` |
| `user_activity` + `audit_chain_checkpoints` 表 | ✅ | spec §5.1 |
| `BEFORE UPDATE` 触发器 (SQLite + PG) | ✅ | spec §5.5 |
| `DROP VIEW IF EXISTS v_turns*` | ✅ | spec §11.2 |
| `AuditConfig` + hot-reload + env binding | ✅ | spec §8 |
| `EventsPath` 配置清理(归 deprecated) | ✅ | spec §11.2 |
| 5 个 Prometheus 指标 | ✅ | `internal/observability/instruments.go` |
| 公开类型:`UserActivity` / `Checkpoint` / `AuditEvent` / `AlertSink` | ✅ | `internal/audit/types.go` |
| sha256 hash chain + checkpoint 锚定 | ✅ | `internal/audit/hash.go` |
| `CollectorConfig` 默认值 | ✅ | `internal/audit/collector.go` |
| 双重 DB store (SQLite `WriteMu` + PG `pg_advisory_xact_lock(819207)`) | ✅ | `internal/audit/store.go` |
| WAL spill + O_SYNC | ✅ | `internal/audit/spill.go` |
| 零丢失 collector (3 级 Enqueue: 通道→spill→阻塞) | ✅ | `internal/audit/collector.go` |
| 原子 `SpillFile.Drain()` (修竞态) | ✅ | `internal/audit/spill.go:Drain` |
| Sink fan-out(per-sink goroutine,5s 超时) | ✅ | `internal/audit/collector.go:fanOutSinks` |
| `AlertSink` + `NoopSink` + `LogSink` + 内部 registry | ✅ | `internal/audit/sinks/` |
| 6 个写入点:`auth.*` (6 paths) + `message.inbound` (7 paths) + `session.create/delete` (5 paths) | ✅ | spec §5.2 |
| Retention GC (3y) + checkpoint 锚定 + 全裁剪 genesis 边缘 | ✅ | `internal/audit/gc.go` |
| 链路验证 (后台 ticker + `chain_breaks` 指标) | ✅ | `internal/audit/verify.go` |
| `GET /admin/users/{id}/activity` + 导出 (JSON/CSV) | ✅ | `internal/admin/activity_handlers.go` |
| `GET /admin/activity` + 导出 (JSON/CSV) | ✅ | 同上 |
| `?include_pii=true` 需要 `admin:write` | ✅ | spec §5.9 |
| PII 掩码:IPv4 末段→0、IPv6 末 4 段→0、UA 截断 50 字 | ✅ | `internal/admin/audit_service.go:maskPII` |
| `system.audit_export` meta-audit (每次导出) | ✅ | spec §5.8 |
| `GatewayDeps.AuditCollector` 集成 + 关闭顺序 (在 EventCollector 之后、SessionMgr 之前) | ✅ | `cmd/hotplex/gateway_run.go` |
| Hot-reload 接线 (retention 立即生效,`batch_interval` 需重启) | ✅ | spec §8 |

### P2 已完成 ✅

- `tool.call` 工具调用审计(spec §5.2)
- 现有 `internal/admin/audit.go` AdminAudit slog 路径迁移为双写 `user_activity`
- 外部 `RegisterSink` 机制 + `WebhookSink`
- `full_content_retention` 延长 events/turns TTL(默认 90 天)
- `system.audit_config_changed` meta-audit
- sinks↔audit 单向导入清理

### Final follow-ups 已完成 / PR #854 ✅

- `docs/specs/User-Behavior-Audit-Final-Followups-Spec.md` 收尾 spec
- `audit_identity_links` 显式身份链接表(SQLite + PostgreSQL migration 024)
- `GET /admin/activity?principal_user_id=...` 展开 principal + linked subjects
- `GET/POST/DELETE /admin/audit/identity-links`
- `/admin/activity` webchat admin 时间线(过滤 + JSON/CSV 导出)
- `docs/reference/admin-api.md` 更新活动查询、身份链接、`admin_audit` 兼容期说明

### P3 / 独立子系统仍未开始 ⏳

- 自动身份匹配 / 跨通道归一 v2(当前只做 admin 显式链接,不做 email/name fuzzy matching)
- 告警规则引擎 / 多通道投递 / 去噪 / 告警状态机
- 冷归档到外部存储(当前通过 JSON/CSV 导出作为人工归档面)
- PG 按月分区(需要 staged table replacement + staging smoke,不做未知生产表的 unsafe in-place conversion)
- `admin_audit` slog 完全退役(当前是兼容流,权威审计载体为 `user_activity`)

### 已发现 + 已修复的 Bug (P1 review 期间)

| Bug | 根因 | 修复 | 提交 |
|-----|------|------|------|
| `TestCollector_SpillOnFull` 偶发 1/5 失败 | `SpillFile.ReadAll` 与 `Truncate` 分两次获取 `s.mu`,并发 Enqueue 在两者之间写入新记录会被 Truncate 抹掉,违反 §5.10 零丢失保证 | 新增 `SpillFile.Drain()` 原子读+截断,collector 改用 Drain | `dc7b269f` |
| `DeleteSession` 鉴权后丢弃 `*SessionInfo` | 旧代码 `id, _, ok := g.authorizeSession(...)` 把用户 ID 丢了,无法审计操作者 | 改为 `id, si, ok` 并 emit `session.delete` | `fa01af5a` |
| `auth` 不上报匿名失败 | spec §5.4 要求 `user_id="anonymous"` + IP/UA 必填;原代码仅返回 error 不上报 | 加 `OnAuthEvent` 回调 + 6 path instrumentation | `1ae66e69` |
| `internal/config` hot-reload 不支持 `audit.*` | 新增字段未登记到 `hotReloadableFields`/`BindEnv` | 加入 watcher + loader + 默认值 | `9cdb76c7` |
| `events_path` 配置仍被 `normalizePaths` 处理 | spec §11.2 要求废弃但保留字段 | 从 normalizePaths 移除 + 加 deprecation 注释 | `9cdb76c7` + `690d49ad` |

### P1 review 第二轮 (code-reviewer subagent 发现,全部修复)

| Issue | 严重度 | 根因 | 修复 |
|-------|--------|------|------|
| `EventID` 非 UUIDv7,同毫秒碰撞 | Critical (§5.6 数据契约) | `fmt.Sprintf("ev_%d_%x", ts, hash(ts))` 是纯时间函数,同毫秒多事件 ID 完全相同 | 按 RFC 9562 原生实现 UUIDv7(48bit ms + 12bit 进程计数器 + 62bit crypto/rand),无需升 google/uuid |
| `decodeDetail` 永远返回 nil | Important (§5.6 sink 契约) | stub 未实现,sink 永远收 `Detail==nil` | 实现 `json.Unmarshal`,空串/坏 JSON 返 nil 不 panic |
| CSV 导出无公式注入防护 | Important (OWASP CWE-1236) | `encodeCSV` 直接写值,`user_agent`/`user_id` 可被攻击者控制成 `=cmd|...` | 新增 `sanitizeCSVCell` 前缀 `'`,覆盖 `=+-@` 与 Tab/CR |
| `audit.retention` hot-reload 不生效 | Important (§8 静默合规 bug) | watcher 接受新值但 GC 实例从未收到(`auditGC` 局部作用域) | 提升变量作用域 + 新增 `GC.UpdateRetention`(mutex 保护),reload callback 调用 |
| Verifier 全表载入内存 | Important (生产 OOM 风险) | `collectAllRows` 翻页后把所有行追加到单个 slice 再验证,3 年保留下 ~10B 行会 OOM | 重写为流式:新增 `Store.QueryAsc`,`VerifyOnce` 每批只载 1000 行,批间用滚动 `cursor` 串联 |
| spill drain 时 DB flush 失败丢记录 | Critical (§5.10 零丢失) | `Drain()` 先截断后 `flushBatch`,DB 写失败则已读出的记录永久丢失 | 新增 `reSpillLocked`:flush 失败时把记录写回 spill 文件 |
| IPv6 掩码脆弱(Minor,顺带修) | Minor (§5.9) | 字符串 split 漏掉简写(`::1`)/v4-mapped 形式 | 改用 `net.ParseIP` + `Mask(CIDRMask)`,全形式/简写/v4-mapped 统一处理 |

**测试新增**:UUIDv7 形状/同毫秒唯一性(5000 次)、decodeDetail 真解码/坏 JSON 不 panic、CSV 公式注入中和、`sanitizeCSVCell` 全 introducer 覆盖、`GC.UpdateRetention` hot-reload 生效 + 非正值忽略、spill flush 失败 re-spill 零丢失、流式 verifier 2500 行分页 + 第二批断链检测。

### 评审反馈 / 待办(给 reviewer)

- [ ] PG migration 在 PR 环境未跑(syntax 匹配 022+009);建议 reviewer 在 staging PG 实例验证 `023_user_activity.pg.sql`
- [ ] `p2 spec` (告警规则引擎 + 多通道) 尚未立项,见 issue 评论区跟进
- [ ] admin UI 用户活动时间线(issue #807)后续 issue
- **Phase 3(归一 + 合规)**:跨通道归一 v2 + SIEM/OTLP 导出 + 冷归档

---

## 11. 冗余治理原则与清单

实施审计表时一并清理 hotplex 主库的**恶性冗余**;同时明确**良性冗余予以保留**(本 spec 的设计哲学)。

### 11.1 原则

**接受为"程序逻辑更简单、查询更高效"而存在的良性冗余**——不为一味消除冗余引入 JOIN 放大或逻辑复杂度。只清理无引用、无语义、纯残留的恶性冗余。

### 11.2 恶性冗余(清理,并入 Phase 1)

| 项 | 类型 | 证据 | 处理 |
|---|---|---|---|
| `v_turns` / `v_turns_user` / `v_turns_assistant` | 死视图 | migration 009 注释替代;迁移目录无 `CREATE VIEW`(可能仅 dev DB 残留),`DROP VIEW IF EXISTS` 无害 | Phase 1 migration `DROP VIEW IF EXISTS` |
| 根 `hotplex.db` / 根 `cron.db` / `data/cron.db`(均 0B) | 空遗留文件 | 配置指向 `data/hotplex.db`,cron 在主库 `cron_jobs` | 启动时清理 / 文档标注可删 |
| `events_path` 配置 | 废弃项 | config 自标 `Deprecated` | 移除配置项 + 兼容读取期 |

### 11.3 良性冗余(保留,标注理由)

| 项 | 保留理由 |
|---|---|
| `events`(完整事件流)↔ `turns`(轮次聚合)内容双存 | 两层模型:events 供完整回放(reasoning/delta/tool 明细),turns 供高效检索/成本统计;合并会牺牲其一,保留换取查询路径简单 |
| `turns.user_id` 反范式拷贝 | 使 turns 检索 / 未来 by-user 查询端点(§5.8)不依赖 JOIN sessions,session 删后仍可归因——审计表虽独立,turns 仍是按 user 下钻全量的来源(§5.3 `event_ref`) |
| `user_activity.user_id` + `session_id` + `event_ref`(本表) | 审计表自带 user_id,独立于 session 生命周期;为 by-user O(1) 检索接受反范式 |

### 11.4 数据异常(治理,非阻塞审计)

- `chat_access_events` 32 行全 `platform=slack` 但写入只有 feishu adapter → 修写入路径(独立小任务,不阻塞本 spec)
- 孤立 events(73%)/turns(67%):**审计表独立后对审计零影响**(审计自带 user_id);仅作 db 卫生,可选激活 `DeleteBySession` 或维持 30 天 TTL,**不在本 spec 强制治理**
- `sessions.owner_id`:实证 92/92 空,**但读路径在用**(`store.list_sessions.sql` / `get_session.sql` / `message_store.get_session_owner.sql` 均用 `COALESCE(owner_id, user_id)` 作权威 owner,`upsert_session.sql:14` 显式 `CASE` 保留)——**不删**,删了破坏 owner 解析。属"运行时未填充"非"死字段"

---

## 12. 风险与权衡

| 风险 | 缓解 |
|---|---|
| 双写性能开销 | 异步批量 + spill;审计点轻量(摘要) |
| hash chain 写入序列化瓶颈 | SQLite 单进程 mutex + 批量足够;**PG 多 pod 共享 advisory lock 是全局串行化点**——高吞吐场景按 `user_id` hash 分片多链(见 §14 分区) |
| events 到期后普通消息只剩摘要 | `full_content_retention` 延长;敏感行为全量入审计表 |
| 平台原生 ID 不归一 → 同人跨通道分散 | v1 接受;v2 建映射(§13) |
| 审计表 3 年膨胀 | TTL GC + 索引 + 可选冷归档 |
| spill 磁盘故障丢审计 | WAL + fsync;失败告警 |

---

## 13. 最佳实践对标

- **OWASP Top 10:2025 A09 + Logging Cheat Sheet**:记录认证/授权/特权/数据访问/输入校验失败;禁录凭证/PII → §5.2 / §5.9
- **NIST SP 800-92 r1**:日志生命周期管理(产生→保留→处置)→ §5.7 GC + 链验证
- **SOC 2 CC7.2 / ISO 27001 A.8.15-16**:用户活动监控、UTC 时钟同步、保留 → §5 全覆盖
- **不可变性**:hash chain(合规常用强度,per 决策 5 足够)
- **标准化**:结构化 JSON + correlation(`session_id`/`event_ref`)+ UTC
- **告警解耦**:本 spec 暴露 `AlertSink` 扩展点;OWASP A09:2025 的 alerting 要求由后续告警子系统实现,审计核心不耦合

---

## 14. Open Questions(后续 Phase 决策)

- 跨通道归一 v2 的映射载体:扩展 `user_identities` 还是新建 `platform_user_map`?(Phase 3)
- 告警子系统(规则引擎 + 多通道 sink + 去噪 + 状态机)独立 spec 何时启动?
- `AlertSink` 背压策略(sink 阻塞时丢弃 vs 有界队列)取舍?
- PG 场景审计表是否按月分区控 3 年查询性能?

---

## 附录:关键代码位置(实施时参考)

- 表 migration(pressly/goose,双 embed 目录):SQLite `internal/session/sql/migrations/0NN_user_activity.sql` + PG `internal/session/sql/migrations-postgres/0NN_user_activity.pg.sql`,均含 `-- +goose Up/Down` 标记(漏 PG 双胞胎会导致 PG 部署静默不应用)
- 审计写入点:`internal/security/auth.go`、`internal/gateway/conn.go` / `bridge_forward.go` / `api.go`、`internal/session/manager.go`、`internal/admin/audit.go`
- collector 范本:`internal/eventstore/collector.go`
- by-user 检索范本:`internal/admin/sessions.go:67-84`、`internal/session/sql/queries/store.list_sessions.sql`
- 未来告警 sink 实现可参考:`internal/cron/delivery.go` 投递模式
- 指标注册:`internal/observability/instruments.go`
