# HotPlex 下一迭代规划（Iteration N+1）

> **主题**: Wave 1 契约启动 —— 推进 v2 依赖链链首
> **周期**: ~1 周（5-6 工程日）· **基线**: v1.37.2 · **规划日期**: 2026-07-22（#923 关闭后修订）
> **范围决策**: 核心（#847 + #848）
> **依据**: `docs/v2/IMPLEMENTATION-STATUS-AND-PLAN.md` 关键路径 `#878✅ → #847 → #848 → #849剩余 → ...`

---

## 一、迭代目标

1. **推进关键路径**：实施 #847 AgentSpec（链首），其合并后衔接 #848 AgentIdentity。
2. **PG 验证策略**：PG 测试接入 CI 已决定**不做**（#923 not planned）；涉及 PG 的变更（如 #848 的 `context_json` 迁移）须本地 docker 跑 `-tags pg -p 1` 验证，并写入 PR checklist 作为补偿控制。
3. **保持向后兼容**：旧配置仅设 `worker_type` 仍能启动；AEP wire / Worker 接口不变。

---

## 二、迭代内容

### 必做（核心，关键路径）
| # | Issue | 工作量 | 依赖 | 说明 |
|---|-------|--------|------|------|
| 1 | **#847 AgentSpec 实施** | 3-5d | 设计已就绪（待评审） | 按 `docs/superpowers/specs/2026-07-21-agentspec-runtime-model-design.md` §5 验收；TDD；表驱动覆盖 4 worker；新增 `internal/agentspec` 包。**实施前先修订独立审查发现 F1-F7**（尤其 F1 移除 AllowedModels 行为变更、F2 permission 映射 pre-work） |

### 衔接（#847 合并后启动）
| # | Issue | 工作量 | 依赖 | 说明 |
|---|-------|--------|------|------|
| 2 | **#848 AgentIdentity** | 2-3d | #847 | AgentIdentity 值对象落 session `context_json`，贯穿 AEP/audit/trace；需先核实/补 `context_json` 列（SQLite+PG 成对迁移，**含本地 docker PG 验证**） |

---

## 三、排序与日程

```
Day 1   评审 #847 设计 spec → 定稿（含独立审查 F1-F7 修订）
Day 1-4 #847 实施（TDD：internal/agentspec 包 + resolver + mapper + 4-worker 表驱动 + WS≡REST 等价性 + secret-free 断言）
Day 4-6 #848 实施（#847 合并后；context_json 迁移 + identity 贯穿；本地 docker 跑 -tags pg -p 1 验证 PG 迁移）
```

**为什么这个顺序**：#847 是依赖链根，必须在其下游之前；#848 紧跟 #847 复用其 AgentSpec 字段。#848 含 PG 迁移，CI 不跑 PG（#923 not planned），须本地 docker 验证。若 #847 延期，#848 顺延至下迭代，不强求同迭代完成。

---

## 四、验收门禁（Definition of Done）

- [ ] #847 验收标准全过（spec §5）：4-worker 表驱动、旧配置兼容、未知 type 边界拒绝、secret-free、WS≡REST 等价、`docs/reference/configuration.md` + `docs/v2/API-DESIGN.md` 更新。
- [ ] #848 验收：workspace owner 校验不破、旧 session 可读、anonymous 有确定性 identity、AEP/audit/trace 可按 session+identity 关联、`context_json` 向后兼容且 secret-free。
- [ ] #848 的 PG 迁移经本地 docker `-tags pg -p 1` 验证通过（CI 不跑 PG，见 #923 not planned）。
- [ ] 每个 PR：`make check` + `make docs-build` 通过。
- [ ] 迭代末：#847/#848 关闭，CI 绿。

---

## 五、风险

| 风险 | 缓解 |
|------|------|
| #847 设计评审发现结构需调整 | Day 1 先评审定稿再实施；spec §7 + 独立审查 F1-F7 已列待决项 |
| #847 "reuse 现有映射"假设不成立（F2/F3） | permission tier→worker 映射、worker_type 5 级 fallback 可能无单一可复用入口；实施前落到具体函数，查无实据者列为 pre-work |
| #848 依赖 session `context_json` 列可能不存在 | 实施前先核实；需 SQLite+PG 成对迁移（CLAUDE.md 迁移规范） |
| #848 PG 迁移无 CI 兜底（#923 not planned） | 本地 docker 跑 `-tags pg -p 1` 验证；写入 PR checklist |
| 单迭代 #847+#848 偏紧 | #848 为"衔接"项，#847 延期则顺延，不强求 |

---

## 六、不在本迭代（后续候选 / 已决定不做）

- **#923 PG 测试接入 CI**（已关闭 **not planned**）—— 决定不做；PG 变更靠本地 docker 按需验证 + PR checklist 补偿控制。若重估：CI 跑 pg-tagged 测试须 `-p 1`（共享库防 goose 踩踏）。
- **#867 worker env allowlist**（安全平行轨，跨三平台，~2-3d）—— 与 Wave 1 契约主题连贯性低，独立排期。
- **pre-push hook test-short flaky 治理**（dev-ex，~0.5-1d）。
- **#849 剩余 / #850 / #852 / #866**（Wave 1/2 后续，#848 之后）。
- **#869 AEP schema**（独立基建，#849 命名稳定后）。
- **#870 Coding Ops Recipes**（延后，需 #847/#849/#851 稳定）。
- **#871 release 门禁**（软阻塞于 #869）。
