# Issue #879 Eventstore 持久化断裂修复 实现计划

> **执行方式**：inline 执行（已建立完整代码级上下文，PR4 依赖 PR1 的 LatestSeq）。

**Goal:** 修复 issue #879 的 5 个独立问题（events 假性停写、seq 碰撞、webchat WS 横跳、updated_at 格式、debug 端点脱节），分 4 个 PR 交付。

**Architecture:** 后端（eventstore/session/gateway/admin，Go）+ 前端（webchat，TS/React）。根因主线：seq 计数器 WS 断连即删→从 1 重置，引发问题 1+2；前端 workspace-error 反馈循环放大问题 3。

**Tech Stack:** Go 1.23+（log/slog, database/sql, modernc sqlite / pgx）、TypeScript/React/Next.js、SQLite + PostgreSQL 双后端。

## Global Constraints
- Mutex 显式 `mu` 字段，不嵌入、不传指针；错误 `fmt.Errorf("...: %w", err)`
- 测试 `testify/require` + table-driven + `t.Parallel()`，单模块 ≤5s，禁 `time.Sleep` 等异步（用 `require.Eventually`/channel）
- SQL 文件经 `go:embed sql/queries/*.sql` 加载，key 去掉 `events.` 前缀；**SQLite + PG 双后端必须同步**
- migrations 在 `internal/session/sql/migrations/`（SQLite）与 `migrations-postgres/`（PG），goose 格式，下一个编号 027
- Fork-PR：push `myfork`(hotplex-ai) 优先，PR 到 `origin`(hrygo)；用 `gh auth token` 注入 credential.helper 避免 push 挂起
- Edit 工具改源码（tab 缩进），禁 sed/awk

## 核实修正（issue 路径偏差）
- eventstore SQL 实际在 `internal/eventstore/sql/queries/`（issue 漏 `queries/` 一层）
- PR2 前端 3/4 路径已变：`webchat/app/components/chat/ChatContainer.assistant-ui.tsx`、`webchat/lib/ai-sdk-transport/client/browser-client.ts`、`webchat/lib/hooks/useSessions.ts`；新增 `webchat/lib/session-select.ts`

---

## PR1: seq 计数器水合 + query_latest 排序（治本，最高优先级）

**根因**：`SeqGen` 纯内存（seq.go:10-12），`removeSession`(hub.go:263) 断连即删→重连从 1 重置→(a) 旧 seq 段碰撞入库；(b) `events.query_latest.sql` 用 `ORDER BY seq DESC`，新事件 seq 小被埋末尾被 LIMIT 截断（假性停写）。turns 用 `ORDER BY id DESC` 不受影响。

**Files:**
- Create: `internal/eventstore/sql/queries/events.latest_seq.sql`
- Modify: `internal/eventstore/store.go`（SQLiteStore.LatestSeq）+ `pg_store.go`（pgStore.LatestSeq）+ 接口
- Modify: `internal/gateway/seq.go`（SeqGen.Init）+ `hub.go`（seqHydrator + EnsureSeqHydrated）+ `conn.go`（resolveSession 水合点）+ `deps.go`/`gateway_run.go`（DI）
- Modify: `internal/eventstore/sql/queries/events.query_latest.sql`（ORDER BY id DESC）

**Interfaces:**
- 新增 `eventstore.LatestSeq(ctx, sessionID) (int64, error)` → `SELECT COALESCE(MAX(seq),0) FROM events WHERE session_id=?`
- 新增 `gateway.SeqHydrator` 接口（仅 LatestSeq），Hub 持有

### Tasks
- [ ] T1.1 eventstore LatestSeq（SQLite + PG + 测试）
  - 新建 `events.latest_seq.sql`；SQLiteStore/pgStore 仿 LatestGeneration 实现；加到 TurnQuerier 接口（或独立接口）。测试：空表返回 0；有数据返回 MAX。
- [ ] T1.2 SeqGen.Init（CAS 只增）+ 单测
  ```go
  func (g *SeqGen) Init(sessionID string, start int64) {
      val, _ := g.seq.LoadOrStore(sessionID, new(atomic.Int64))
      cur := val.(*atomic.Int64)
      for {
          old := cur.Load()
          if start <= old { return }
          if cur.CompareAndSwap(old, start) { return }
      }
  }
  ```
  测试：Init 后 Next 连续；Init 不回退已有值；并发 Init+Next 安全。
- [ ] T1.3 Hub.EnsureSeqHydrated + DI
  - Hub 加 `seqHydrator SeqHydrator` 字段 + `SetSeqHydrator()`；`EnsureSeqHydrated(sessionID)` 调 LatestSeq→seqGen.Init（3s timeout，错误仅 log.Warn 不阻断握手）
  - gateway_run.go 注入（eventstore store 同时实现 SeqHydrator）
- [ ] T1.4 水合点：conn.resolveSession JoinSession(490) 后、resolveSessionState(492) 前
  - `c.hub.EnsureSeqHydrated(sessionID)` —— 此时 worker 尚未启动，无并发 NextSeq；第一个 NextSeq 在 finalizeInit:734(init_ack)
- [ ] T1.5 events.query_latest.sql 改 `ORDER BY id DESC`（止血，恢复历史数据）
- [ ] T1.6 make check + push myfork + PR

**风险**：水合在握手路径加一次 DB 查询（<10ms，握手 30s 超时，可接受）；首次创建的 session LatestSeq=0，Init 无效，Next 从 1 开始（正确）。

---

## PR2: 前端 WS 横跳止血（workspace-error 冷却 + sessionId debounce）

**根因**：workspace 握手失败→onWorkspaceError(948)→handleWorkspaceError(311) 清 localStorage+loadWorkspaces→workspaceId 变→useSessions refetch→activeSessionId 变→ChatInterface remount(key=677)→重连→若新 workspace 也失败→循环。StrictMode 假设被反驳（reactStrictMode:false）。

**Files:**
- Modify: `webchat/app/components/chat/ChatContainer.assistant-ui.tsx`
- Modify: `webchat/lib/adapters/hotplex-runtime-adapter.ts`（可选 debounce 备选）

### Tasks
- [ ] T2.1 handleWorkspaceError 加 5s 冷却（切断循环）
  ```tsx
  const lastWsErrorRef = useRef(0);  // 插入 218 行后 workspace state 区
  const handleWorkspaceError = useCallback(() => {
      const now = Date.now();
      if (now - lastWsErrorRef.current < 5000) return;  // 5s 冷却
      lastWsErrorRef.current = now;
      localStorage.removeItem("hotplex_active_workspace_id");
      loadWorkspaces();
  }, [loadWorkspaces]);
  ```
- [ ] T2.2 ChatInterface debounce sessionId 150ms（通用减振，合并快速 remount）
  ```tsx
  // ChatInterface(55-112) 内，useHotPlexRuntime 前加：
  const [stableSessionId, setStableSessionId] = useState(sessionId);
  useEffect(() => {
      const t = setTimeout(() => setStableSessionId(sessionId), 150);
      return () => clearTimeout(t);
  }, [sessionId]);
  // adapter = useHotPlexRuntime({ sessionId: stableSessionId ?? undefined, ... })
  ```
- [ ] T2.3 tsc --noEmit + make check + push + PR

**风险**：debounce 延迟所有 session 切换 150ms（可接受）；不移除 key=677（防跨 session 状态泄漏）。

---

## PR3: 防御层（UNIQUE 索引 + runWriter panic recovery）

**Files:**
- Create: `internal/session/sql/migrations/027_events_unique_session_seq.sql` + `migrations-postgres/027_events_unique_session_seq.pg.sql`
- Modify: `internal/eventstore/collector.go`（runWriter panic recovery）

### Tasks
- [ ] T3.1 UNIQUE 索引 migration（027，SQLite + PG）
  - **必须先去重**：现有重复 `(session_id,seq)` 会让 CREATE UNIQUE INDEX 失败。migration 先 `DELETE` 保留每组 (session_id,seq) 最大 id 的行，再建索引。
  - SQLite: `DELETE FROM events WHERE id NOT IN (SELECT MAX(id) FROM events GROUP BY session_id, seq);` + `CREATE UNIQUE INDEX IF NOT EXISTS idx_events_session_seq_uq ON events(session_id, seq);`
  - PG 同构（大写引号）
  - 测试：重复数据 migration 后保留最新；新插入碰撞报错
- [ ] T3.2 runWriter panic recovery（collector.go:362）
  ```go
  func (c *Collector) runWriter() {
      defer c.closeWg.Done()
      defer func() {
          if r := recover(); r != nil {
              c.log.Error("eventstore: runWriter panic, restarting", "panic", r, "stack", string(debug.Stack()))
              c.closeWg.Add(1)
              go c.runWriter()  // 重启而非永久死亡
          }
      }()
      ...
  }
  ```
  测试：注入 panic 的 store，验证 runWriter 重启后仍能写入。
- [ ] T3.3 make check + push + PR

**风险**：UNIQUE 索引让 seq 碰撞从静默腐败变为报错——Append 失败会导致该 event 丢失（batch 内 break）。需配合 PR1 水合（seq 不再重置）避免碰撞。去重 DELETE 在大表上慢（一次性 migration，可接受）。

---

## PR4: 清理项（updated_at 格式 + debug 端点 DB 字段）

**Files:**
- Modify: `internal/session/cleanup_outbox.go`（upsertSessionArgs:140 时间格式）
- Modify: `internal/admin/handlers.go`（HandleDebugSession:357 补 DB 字段）+ `admin.go`（注入依赖）

### Tasks
- [ ] T4.1 upsertSessionArgs 时间转 RFC3339Nano UTC
  ```go
  func upsertSessionArgs(info *SessionInfo, ctxJSON, pkJSON []byte) []any {
      return []any{..., info.CreatedAt.UTC().Format(time.RFC3339Nano),
          info.UpdatedAt.UTC().Format(time.RFC3339Nano),
          formatNullTime(info.ExpiresAt), formatNullTime(info.IdleExpiresAt), ...}
  }
  // formatNullTime: nil→nil, else UTC RFC3339Nano
  ```
  - **PG 验证**：PG TIMESTAMP 列收 RFC3339 字符串（pgx 能解析）；scanSession time.Time/NullTime 反向解析 RFC3339 兼容
  - 测试：SQLite/PG 写入+读回 round-trip 时间一致
- [ ] T4.2 HandleDebugSession 补 DB 字段（复用 PR1 LatestSeq + turnStore）
  ```go
  "db_turn_count": dbTurnCount,   // turnStore 查 turns 表
  "db_last_seq":   dbLastSeq,     // LatestSeq（PR1）
  "runtime_only":  true,          // 标注 turn_count/last_seq_sent 是 ephemeral
  ```
  - AdminAPI 注入 eventstore 依赖（或扩展 turnStore 接口加 LatestSeq）
  - DB 查询失败时字段设 null，不阻断端点
- [ ] T4.3 make check + push + PR

**依赖**：T4.2 的 db_last_seq 复用 PR1 的 LatestSeq → PR4 在 PR1 之后。

---

## 执行顺序
PR1（治本，最高优先级）→ PR2（前端，独立）→ PR3（防御层，依赖 PR1 减少碰撞）→ PR4（清理，依赖 PR1 LatestSeq）

每个 PR：切分支 `fix/issue-879-prN-xxx` → TDD → make check → push myfork → PR origin → review loop。
