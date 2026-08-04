---
title: "Scope-aware Capability Inventory Contract"
type: spec
status: approved
date: 2026-08-04
owners: [hotplex-runtime]
references:
  - docs/v2/ROADMAP.md
  - docs/v2/ARCHITECTURE.md
  - docs/superpowers/specs/2026-08-04-runtime-operations-contract.md
  - docs/research/2026-08-04-qm-hotplex-deep-research-report.md
---

# Scope-aware Capability Inventory Contract

## 1. 定位

Capability Inventory 是 Agent Config、Skills、Worker 能力和 workspace 权限的 scope-aware、只读优先解释契约。它向 EffectiveRuntimePlan、doctor、admin diagnostics 和 Cockpit 提供稳定的 precedence、source、hash、materialization 和 enforcement facts。

Inventory 不是新的身份、授权、memory 或 marketplace 系统。

## 2. Scope 模型

Inventory 支持：

- global；
- platform；
- bot；
- workspace；
- session。

每个 entry 记录 scope kind、scope ID、source ref、version/hash 和 resolution state。当前 agent-config 文件 precedence 保持 `bot > platform > global`；workspace/session override 只能作用于显式允许的 runtime field。

## 3. Inventory 模型

```go
type CapabilityInventory struct {
    Version    int
    Hash       string
    PlanHash   string
    Scope      ScopeRef
    Entries    []CapabilityEntry
    Warnings   []InventoryWarning
    ObservedAt int64
}

type CapabilityEntry struct {
    ID              string
    Kind            string
    Scope           ScopeRef
    SourceRef       string
    ContentHash     string
    ResolutionState string
    Enforcement     string
    Materialization string
    RequiredBy      []string
}
```

`ResolutionState` 只允许 `effective`、`inherited`、`shadowed`、`explicitly_cleared`、`unavailable`。`Enforcement` 只允许 `declared`、`observed`、`enforced`、`partial`、`unavailable`、`unknown`。

## 4. Inventory 范围

- Worker type 和 provider/backend capability；
- permission mode；
- allowed/disallowed tools；
- workspace filesystem access；
- network capability；
- env profile 和允许注入的 key names；
- agent-config source files；
- skill names、source、content hash 和 required capabilities；
- sandbox/materialization status；
- policy and compatibility warnings。

Inventory 不包含 prompt、skill body、metadata value、secret、credential、完整文件内容、raw provider request、完整 tool args 或 raw worker error。

## 5. Resolution 与 Shadowing

- 同名 entry 按 scope precedence 解析；
- effective entry 记录被采用的 source 和 version/hash；
- shadowed entry 保留 redacted source ref 和 shadowed reason；
- explicit clear 与 absent/inherited 明确区分；
- 相同输入产生相同 canonical inventory hash；
- resolution 只读取 snapshot，不执行 promotion、publish 或 materialization mutation。

## 6. Safe Materialization

Materialization 在 Worker 注入前执行：

- relative path normalization 和 workspace containment；
- symlink、UNC、Windows separator、NUL 和 encoding bypass 检查；
- file type、size、bundle 和 count limits；
- reserved XML tag sanitization；
- Windows 临时文件注入；
- content hash marker；
- stale marker/content cleanup；
- keyed/advisory lock；
- materialized/failed/unknown result 和 redacted reason。

任何 unsafe path、oversized content、malformed reserved tag 或 stale ownership conflict 均 fail closed。

## 7. Authorization 与 Promotion

Inventory inspection 是 read-only。真正授权继续由 API key、workspace ownership、permission mode、allowed tools、worker policy 和 audit 执行。

Promotion/publish/move：

- 不属于 ordinary resolution；
- 需要显式 admin authorization；
- 校验 source/destination scope ownership；
- 重新计算 content hash 和 required capability；
- 写入 actor、action、source/destination、result 和 redacted reason audit；
- 不在该 contract 中提供 public registry 或 marketplace。

## 8. EffectiveRuntimePlan 集成

EffectiveRuntimePlan 消费：

- canonical inventory hash；
- effective capability IDs；
- skill/config hashes；
- source refs；
- isolation/materialization states；
- warnings 和 blocked reasons。

Inventory hash 只证明解析结果，不证明 capability 已应用。ObservedRuntimeState 提供实际 Worker/backend/materialization/enforcement evidence。

## 9. 兼容与持久化

- 当前 B/C agent-config channel、fallback 和 `inject_exclude` 保持兼容；
- `META-COGNITION.md` 强制注入边界不变；
- `worker.Register()` 和 Worker core interface 不被替换；
- 四类 Worker 使用相同 vocabulary，允许各自报告 unavailable；
- inventory 持久化时 SQLite/PostgreSQL schema 和查询行为一致；
- 旧客户端不需要理解 inventory 即可继续连接和收发 AEP v1。

## 10. 完成定义

- 等价 session input 产生相同 canonical inventory hash；
- 每个 entry 具有 source ref、resolution state 和 enforcement state；
- inherited、shadowed、explicitly-cleared 和 unavailable 可区分；
- path/size/XML/Windows/stale cleanup 具有正向和负向测试；
- read-only inspection 不产生 config/skill mutation；
- promotion 经过 authorization、hash validation 和 audit；
- SQLite/PostgreSQL、四 Worker 和现有 fallback/inject-exclude 行为一致；
- durable inventory 不包含 prompt、secret、credential 或 raw payload。

## 11. 非目标

- 独立 identity service；
- 新的 authorization system；
- replacement Worker registry/interface；
- memory product、vector store 或 workflow engine；
- public skill registry 或 marketplace；
- 以 inventory、token、hash 或 audit 证明 OS isolation。

## 12. 证据来源

- `qm/src/skills/skill-store.ts:114-195,221-278`
- `qm/src/skills/materialize.ts:210-379`
- `internal/agentconfig/loader.go`
- `internal/agentspec/resolve.go`
- `internal/worker/base/env.go`
- `docs/research/2026-08-04-qm-hotplex-deep-research-report.md`
