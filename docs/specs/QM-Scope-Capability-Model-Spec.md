---
title: "QM Scope Capability Model"
type: spec
status: approved
date: 2026-08-04
---

# QM Scope Capability Model

## 定位

为 HotPlex 的 Agent Config、Skills、Worker 能力和 workspace 权限建立一个 scope-aware、只读优先的 capability inventory，吸收 qm 的 scope precedence、review/publish、shadowing、content hash 和 safe materialization 经验。

## 终态契约

- 输出每个 session 的 effective capability inventory：worker、permission mode、allowed/disallowed tools、workspace access、agent config sources、skill names、skill/config hash；
- 明确 precedence：bot > platform > global，以及 workspace/session override 的边界；
- 只输出 capability 名称、来源、版本/hash 和 enforcement 状态，不输出 prompt、secret 或完整文件内容；
- worker 注入前执行 path/size/XML/Windows file-injection 安全检查，并记录 materialization result；
- 同名 skill/config 发生 shadowing 时在 plan 与 audit 中可解释；
- 仅允许 admin-gated promotion，暂不实现 marketplace 或 remote registry。

## 设计边界

Capability inventory 是解释与校验契约，不是新的授权系统。真正授权仍由现有 API key、workspace ownership、permission mode、allowed tools、audit 和 worker-specific policy 决定。inventory 无法证明的能力标为 `partial`/`unknown`，不得升级为“已隔离”。

## 完成定义

- equivalent session inputs produce identical canonical inventory hash；
- every listed capability has a source ref and enforcement state；
- shadowed/inherited/explicitly-cleared entries are distinguishable；
- worker injection and skill materialization have bounded size/path tests；
- SQLite/PostgreSQL do not diverge where inventory is persisted；
- old clients and current B/C agent-config channels remain compatible。

## Non-goals

- 不新增独立 identity service；
- 不替换 `worker.Register()` 或 Worker interface；
- 不将 skill body、prompt、credential 或 raw worker error 写入 execution；
- 不建设 memory product、vector store、workflow engine 或 skill marketplace。

## 参考

- `qm/src/skills/skill-store.ts:114-195,221-278`
- `qm/src/skills/materialize.ts:210-379`
- `internal/agentconfig/loader.go`
- `internal/agentspec/resolve.go`
- `internal/worker/base/env.go`
- `docs/research/2026-08-04-qm-hotplex-deep-research-report.md`
