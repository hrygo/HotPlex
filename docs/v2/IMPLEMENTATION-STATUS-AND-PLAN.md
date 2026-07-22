# HotPlex 2.0 实施状态与整体方案（对账修订版）

> **状态**:  living document · **基线**: v1.37.2 · **首次对账**: 2026-07-21
> 本文把 `docs/v2/` 既有规划（ROADMAP / IMPLEMENTATION-ROADMAP / GITHUB-MILESTONES）与**当前代码实际状态**对账，修正"规划写于 v1.32.2、部分工作已由 #878 epic 交付"的偏差。规划原则与 Wave 划分仍以既有文档为准，本文只做状态修订与排序。

---

## 一、三个关键判断

1. **整体路线已存在**：Wave 0-4 / Milestone A-D 已由 `IMPLEMENTATION-ROADMAP.md` 与 `GITHUB-MILESTONES.md` 定义。本文不另起炉灶。

2. **#878 epic（durable ingress reliability）已验证并关闭（2026-07-21）**：6 个 slice 代码全部就位，且 Slice 6 验证矩阵已全绿——`make check`（SQLite + race + 4-worker + build + docs）+ real-PostgreSQL（docker pg16）多实例/故障注入/迁移/审计。残余的"PG 测试接入 CI"已决定不做（#923 not planned），改为本地 docker 按需验证 + PR checklist 补偿控制。

3. **epic 与 Wave 1/2 收敛，不平行**：epic Slice 5 交付了 **#849 的 first cut**（最小 3 个 runtime 事件 + `execution_id` metadata）；epic Slice 4 交付了 **#851 的 first cut**（单活跃门 `SESSION_BUSY`）。因此 #849/#851 应**收窄为剩余更广泛范围**，而非重做。

**真正的绿地工作集中在 Wave 1 链首 #847（AgentSpec）**——整个 v2 依赖链的根，完全未启动，ROI 最高。

---

## 二、Issue 状态对账表

| Issue | 规划归属 | 代码状态 | 真实剩余工作 |
|-------|----------|----------|--------------|
| **#878** epic | 可靠性 epic | ✅ **已验证并关闭**（2026-07-21） | Slice6 矩阵全绿：SQLite `make check` + real-PG（docker pg16）多实例/迁移/审计；PG-CI 接入已决定不做（#923 not planned），本地 docker 按需验证 |
| **#849** runtime events | Wave1 / Milestone A | 🟡 最小 3 事件已由 epic Slice5 交付 | 收窄：security/context/policy 事件分类 |
| **#851** execution queue | Wave2 / Milestone B | 🟡 单活跃门已由 epic Slice4 交付 | 收窄：完整 ExecutionQueue 抽象 |
| **#850** tracing/metrics | Wave1 / Milestone A | 🟡 部分 execution 指标已有 | runtime span attributes 标准化 + 低基数语义 key |
| **#847** AgentSpec | Wave1 / **链首** | ❌ 未启动 | 全量 first cut（归一化器 + resolver） |
| **#848** AgentIdentity | Wave1 | ❌ 未启动 | 全量（依赖 #847；需确认/补 session `context_json`） |
| **#852** RuntimeContext | Wave2 | ❌ 未启动 | 全量只读 Load facade（依赖 #848/#849） |
| **#866** 持久化快照 | Wave1 延伸 | ❌ 未启动 | 全量（依赖 #847/#848） |
| **#868** Execution Cockpit | Wave3 | ❌ 未启动 | 依赖多已由 epic 满足；epic 关闭后解锁 |
| **#877** fence escape hatch | epic 后续 | ❌ 未启动 | 被 #878 阻塞；epic 关闭后解锁（2-3 天） |
| **#867** worker env allowlist | 独立安全轨 | ❌ 未启动（BuildEnv 仍 blocklist） | 全量，跨三平台，可并行 |
| **#869** AEP canonical schema | 独立基建 | ❌ 未启动 | 全量；#849 命名稳定后 |
| **#870** Coding Ops Recipes | 独立 scheduler | ❌ 未启动 | 较大；#847/#849/#851 稳定后 |
| **#871** release SBOM/签名 | 独立 release 基建 | ❌ 未启动 | 全量；软阻塞于 #869 |

图例：✅ 已实现待关 · 🟡 部分实现需收窄 · ❌ 未启动

---

## 三、收敛关系图

```
              ┌────────────────────────────────────────┐
              │  #878 epic — durable ingress reliability │
              │  （可靠性硬基础，已基本实现）             │
              └──────────────┬─────────────────────────┘
        Slice 5 ─────────────┼──────────── Slice 4
        交付 #849 first cut   │             交付 #851 first cut
        (runtime.execution.*) │             (单活跃门 SESSION_BUSY)
                              ▼
      ┌─────────────── v2 Wave 1/2 路线 ───────────────┐
      │  #847 AgentSpec ─► #848 Identity ─► #849(剩余)  │
      │   (链首,未启动)        │            #850(剩余)   │
      │                       ├─────────► #852 Context  │
      └───────────────────────┴─────────► #866 快照     │
                              #851(剩余) ─► Wave3: #868 Cockpit
                                          #877 escape hatch (epic 后解锁)
      └────────────────────────────────────────────────┘
   独立平行轨：#867 worker env · #869 AEP schema · #871 release 门禁
   延后：#870 Coding Ops Recipes
```

---

## 四、分阶段方案

### Phase 0 — 收尾对账（低成本清场）
- **#878**：运行 Slice 6 验证矩阵（`make check` + real-PG round-trip + 4-worker 契约 + bench P95 + `make docs-build`）→ 关闭；real-PG 缺口见 §五。
- **#849 / #851 / #850**：改写 body，注明 first cut 已由 epic Slice 4/5 交付，收窄为剩余范围，加 epic cross-ref。

### Phase 1 — Wave 1 契约基础（关键路径）
1. **#847 AgentSpec**（~3-5 天，链首）：只读归一化器 + resolver，WS init 与 REST create-session 共用，映射到现有 `worker.SessionInfo`，不改 Worker 接口、不加持久化；表驱动测试覆盖 4 种 worker。
2. **#848 AgentIdentity**（~2-3 天，依赖 #847）：先落 session `context_json`，贯穿 AEP/audit/trace 同一套 key，保留 anonymous 兼容。

### Phase 2 — 可观测收敛 + 协议锁定
3. **#849 剩余**（~2-3 天）：在 epic 已交付的 3 个 runtime.* 之上扩展 security/context/policy 事件，保持 AEP v1 加性兼容。
4. **#850 剩余**（~2-3 天，依赖 #849）：runtime span attributes + 低基数语义 key。
5. **#869 AEP canonical schema + 跨 SDK 一致性**（~3-4 天）：golden envelope 语料 + Go/TS/Python/Java 同语料 CI + schema-diff 加性/破坏分类。

### Phase 3 — Wave 2 控制平面
6. **#851 剩余**（~3-4 天）：epic 单活跃门之上做完整 ExecutionQueue（FIFO/attempt/retry/queue state）。
7. **#852 RuntimeContext**（~3-4 天，依赖 #848/#849）：只读 Load facade，数据源限 eventstore/turns/worker_session_id/workspace。
8. **#866 持久化有效 AgentSpec 快照**（~2-3 天，依赖 #847/#848）：secret-free 快照落 context_json，restart 不漂移。

### Phase 4 — Wave 3 运营（epic 关闭后解锁）
9. **#868 Execution Cockpit**（~4-5 天）：按 execution_id join 持久化事实的只读 admin API + 时间线 UI（中英双语 i18n）。
10. **#877 fence escape hatch**（~2-3 天）：时间界强制清 fence + admin 手动覆盖（审计）。

### 平行轨 / 延后
- **#867 worker env allowlist + 隔离 profile**（~2-3 天，跨三平台，独立可并行）。
- **#871 release 门禁 + SBOM + 签名**（~3-5 天，建议 #869 后）。
- **#870 Coding Ops Recipes**（延后，需 #847/#849/#851 稳定）。

---

## 五、风险与缺口

| 风险/缺口 | 说明 | 缓解 |
|------|------|------|
| **real-PG 未接入 CI（已接受）** | epic 已用本地 docker（pg16）验证 real-PG 全绿并关闭，但 `//go:build pg` 测试**仍未接入 CI**（ci.yml 无 postgres service） | 已决定不做（#923 not planned）：PG 变更须本地 docker 跑 `-tags pg -p 1` 验证并写入 PR checklist；若重估，pg 测试共享库须 `-p 1` 防 goose 踩踏 |
| #849/#851 范围漂移 | 收窄不当会漏掉 epic 未覆盖部分 | 逐条对照 epic body 的 "Relationships/Converges" 声明 |
| #848/#866 依赖 context_json | session store 可能尚无该列 | 实施前核实/补列（SQLite+PG 成对迁移） |
| 文档基线过时 | ROADMAP 写于 v1.32.2 | 本次已把"当前基线"更新到 v1.37.x |
| AEP wire 兼容 | 新增 runtime 事件须加性兼容 | 遵守 AEP wire contract 规范，同步 Go SDK + 3 示例 SDK + 协议文档 |

---

## 六、关键路径

```
关键路径：#878 ✅已关闭 → **#847** → #848 → #849(剩余) → #850 → #852 → #866
                                       └→ #869 → (锁定协议)
解锁后：  #868 / #877（epic 关闭即解锁）
平行：    #867 / #871（任意时间插入）
延后：    #870
```

**#847 必须是下一个**：它是 #848/#849/#851/#852/#866/#870 的共同祖先。IMPLEMENTATION-ROADMAP 已论证"先定义标准化配置模型，避免后续 issue 各自发明字段"。
