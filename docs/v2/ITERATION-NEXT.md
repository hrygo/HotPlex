# HotPlex 下一迭代规划（Iteration N+2）

> **主题**: 协议锁定 + 可靠性加固
> **周期**: ~1.5 周（8-9 工程日）· **基线**: v1.38.0 (main@2c6240b4) · **规划日期**: 2026-07-24
> **前置完成**: Wave 1 链首 #847/#848/#850/#852/#866 已合并（PR #924）· #878 epic 已关闭
> **范围决策**: 核心（#869 + #877）· 快速收尾（#927）
> **依据**: `docs/v2/IMPLEMENTATION-STATUS-AND-PLAN.md` 四收窄与刻意延后决策

---

## 一、迭代目标

1. **锁定协议面**：实施 #869 AEP canonical schema，为 #849 剩余范围（事件分类扩展）和 #871 release 门禁解除命名层面的阻塞。
2. **加固可靠性**：实施 #877 fenced execution escape hatch，消除 #878 epic 留下的"永久 fenced session"病态路径。
3. **保持加性兼容**：AEP v1 wire contract 不变；新事件/schema 对旧客户端透明。

---

## 二、迭代内容

### 快速收尾（Day 0）
| # | Issue | 工作量 | 依赖 | 说明 |
|---|-------|--------|------|------|
| 0 | **#927 docs(patrol)** | 0.5d | 无 | 补 `reference/admin-api.md` 两个端点：`POST /api/admin/users/{id}/password` + `GET /admin/sessions/pool`；`make docs-build` 绿 |

### 核心轨 A - 协议锁定（#869）
| # | Issue | 工作量 | 依赖 | 说明 |
|---|-------|--------|------|------|
| 1 | **#869 AEP canonical schema + 跨 SDK 一致性** | 3-4d | 无（#849 命名协调） | canonical schema 源自 Go 契约 -> golden envelope 语料（required/optional/unknown/compatibility）-> Go/TS/Python/Java 同语料 CI -> schema-diff 加性/破坏分类 -> AEP v1 加性兼容保持。**本 issue 是 #849 剩余 + #871 的前置门** |

### 核心轨 B - 可靠性加固（#877）
| # | Issue | 工作量 | 依赖 | 说明 |
|---|-------|--------|------|------|
| 2 | **#877 fence escape hatch** | 2-3d | 无（#878 已关闭解锁） | 代码已确认 fence 机制就位（`fence_reason` / `ClearFenceAfterFreshStart` / `ErrExecutionFenced`），但**无 `fence_created_at`、无超时强制清除、无 admin 覆盖**。实施 Option A（时间界自动清除）+ Option B（admin 手动覆盖 + 审计） |

---

## 三、排序与日程

```
Day 0   #927 doc patrol（补 2 个 admin API 端点到 reference/admin-api.md）
Day 1-4 #869 AEP canonical schema（设计 spec -> schema 定义 -> golden corpus -> 4-SDK CI -> schema-diff）
Day 1-3 #877 fence escape hatch（与 #869 可并行：fence_created_at + 时间界清除 + admin 端点 + 审计 + 测试）
```

**为什么这两条轨**：#869 是当前关键路径瓶颈--#849 剩余明确"事件命名须先与 #869 canonical schema 对齐"才扩展，#871 release 门禁软阻塞于 #869。#877 自 #878 关闭后已解锁，独立于协议轨，两条线无共享文件可并行。总计 6-8 工程日 + 0.5d 收尾。

---

## 四、验收门禁（Definition of Done）

- [ ] #927: `reference/admin-api.md` 补全 2 个端点；`make docs-build` 绿（62 篇无断链）。
- [ ] #869: 一个 corpus 被 Go/TS/Python/Java 测试消费；runtime 事件覆盖；未知加性 kind 安全可忽略；field/tag drift 失败 CI 且 diff 可读；生成确定性强；协议 + SDK 文档更新。
- [ ] #877: fenced session 不超过配置最大存活时间（默认 30min 可调）；force-clear 设 `unknown` + 独立 reason（`FENCE_FORCE_CLEARED` / `FENCE_ADMIN_CLEARED`）；不重投原始输入；admin 覆盖走现有 Bearer+scope 鉴权 + 审计日志；force-clear 发射 `runtime.execution.failed` 事件；测试覆盖自动清除、手动清除、审计、事件发射、清除后接受新输入。
- [ ] 每个 PR：`make check` + `make docs-build` 通过。
- [ ] 迭代末：#869/#877/#927 关闭，CI 绿。

---

## 五、风险

| 风险 | 缓解 |
|------|------|
| #869 schema-diff 误报加性/破坏 | 先在已知 breaking 改动上验证分类器；golden corpus 含显式 compatibility case |
| #869 跨 SDK CI 矩阵复杂 | 先只做 schema + Go 消费；TS/Python/Java 分阶段接入，first cut 只验 Go |
| #877 force-clear 语义与 #878 fence 保证冲突 | 严格遵守 AC：force-clear 只设 `unknown`，永不设 `completed`/`delivered`；不触发重投；非默认关闭需显式 opt-in |
| #877 最大存活时间默认值不当 | 30min 是保守起点；operator 可调；disabled 仅显式 opt-in |
| #869 + #877 并行 PR 审查负担 | 两条轨文件不交叉（#869: `pkg/events` + `examples/` + CI；#877: `internal/execution/` + `internal/gateway/commands.go` + admin handler） |

---

## 六、不在本迭代（后续候选 / 已决定不做）

| Issue / 项 | 延后理由 | 承接 |
|------------|----------|------|
| **#849 剩余**（security/context/policy 事件分类） | 事件命名须先与 #869 canonical schema 对齐--本迭代 #869 落地后即可衔接 | 下迭代紧接 |
| **#851 剩余**（完整 ExecutionQueue: FIFO/attempt/retry/timeout/queue state） | 超时/重试/queue state 语义需与现有 turn timeout x LLM retry x crash synthetic turn 协调，属设计决策非机械实现 | 需专项设计 |
| **#868 Execution Cockpit** | 依赖 #849 剩余 + #851 剩余的 queue state 暴露 | 两轨完成后 |
| **#867 worker env allowlist + 隔离 profile** | allowlist 完整性风险：漏一个合法 env var 会静默打断 worker；须跨三平台逐项验证 PATH/HOME/locale/temp 且有兼容模式迁移路径 | 独立安全轨，下迭代可插入 |
| **#871 release 门禁 + SBOM + 签名** | 建议在 #869 落地后，确保 SDK conformance 检查可作为 release 门禁 | #869 后 |
| **#870 Coding Ops Recipes** | 需 #847✅/#849/#851 稳定 | 多轨完成后 |

---

## 七、依赖全景图（当前状态）

```
已完成:  #878✅ -> #847✅ -> #848✅ -> #850✅ -> #852✅ -> #866✅
                   #878✅ 交付 #849 first-cut + #851 first-cut

本迭代:  #869 -------------------------- 协议锁定（解锁 #849剩余 + #871）
         #877 -------------------------- 可靠性加固（独立，#878 解锁）
         #927 -------------------------- doc 收尾

下迭代:  #849剩余 <-(需#869) -> #850剩余（字面量迁移）
         #851剩余 <-(需调度设计) -> #868 Cockpit
平行轨:  #867（独立安全）· #871（#869后）· #870（延后）
```
