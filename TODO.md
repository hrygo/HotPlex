# TODO — Epic #954 平台×Worker E2E 对齐（交接文档）

> **交接时间**: 2026-08-05 · **分支**: `feat/954-platform-worker-e2e-alignment` · **worktree**: `.worktrees/954-platform-worker-e2e-alignment/`
> **交接人**: Claude Code（SDD 控制器） · **继续方式**: 见文末"如何继续"

---

## 1. 任务概况

Epic [#954](https://github.com/hrygo/hotplex/issues/954)：对齐飞书、Slack、WebChat × 四 Worker 的 E2E 可靠性与能力契约。
Spec: `docs/superpowers/specs/2026-08-05-platform-worker-e2e-alignment-design.md`
总计划: `docs/superpowers/plans/2026-08-05-platform-worker-e2e-alignment.md`
五份子计划: `docs/superpowers/plans/2026-08-05-{platform-worker-contract-matrix,slack-reliability-alignment,worker-lifecycle-alignment,webchat-worker-matrix,platform-worker-live-validation}.md`

**目标**（用户设定）: 交付高质量 PR，完成规格范围内全部工作，100% 通过验收。

**执行框架**: superpowers:subagent-driven-development（每任务独立实现者 + 任务评审 + 修复轮）。进度账本（权威记录）:
`.superpowers/sdd/2026-08-05-platform-worker-e2e-alignment/progress.md`
各子计划的 brief/report/review 包在 `.superpowers/sdd/<plan-name>/` 下。

---

## 2. 已完成（HEAD = `c2d44b44`，20 commits 全部评审通过）

| 子计划 | 任务 | Commit | 状态 |
|---|---|---|---|
| SP1 契约矩阵 | T1 e2econtract manifest | a236fa09 | ✅ 评审通过 |
| SP1 | T2 WorkerProbe 真实 parser/mapper | e8a88199 | ✅ |
| SP1 | T3 Gateway contract harness（execution store 接线 a85a8f1a、全 probe teardown 0a48f87f） | 8b6f9668 | ✅ |
| SP1 | T4 C01–C08 scenario runner | 81eafea5 + bbd6c472 | ✅（1 修复轮） |
| SP1 | T5 旧 interaction matrix 改名 | 442cd6c1 | ✅ |
| SP1 | T6 飞书四组合 | f8e510ab + 9245854b + e4426b2a | ✅（2 修复轮，含 C07/C08 fault arm 迁移） |
| SP1 | T7 Slack 四组合 | e351dc89 + cef48801 | ✅（1 修复轮） |
| SP1 | T8 WebChat 四组合（WS 真实入口） | 3f00ff8f + 9c92bc43 | ✅（1 修复轮） |
| SP1 | **T9 12 组合接入 CI（Makefile/脚本/ci.yml）** | — | ❌ **未开始** |
| SP2 Slack | T1 媒体边界（10MiB/0700/0600/原子落盘） | 2a737788 + 2ca6eb0a | ✅（1 修复轮） |
| SP2 | T2 dedup 条件提交/rollback | d03a704b | ✅ |
| SP2 | T3 terminal 错误传播 | c2d44b44 | ✅ |
| SP3 Worker | T1 OCS abort HTTP helper | 92a418ca | ✅ |
| SP3 | T2 OCS StopCurrentTurn 原地 abort | 360ec7a3 | ✅ |
| SP3 | T3 stopped marker 限定当前 turn（BeginTurn） | 588afd12 | ✅ |
| SP3 | T4 capability manifest 锁定 | 25c8eef4 | ✅ |
| SP3 | **T5 Gateway per-turn stop fence + C04/C05 合同** | — | 🔶 **进行中（见 §4）** |
| SP3 | T6 Reset/reconnect 四 Worker 合同 | — | ❌ 未开始 |
| SP3 | T7 Mid-turn Native/fallback 合同 | — | ❌ 未开始 |
| SP4 WebChat | T1 queue 可访问性基线 | 4d65aec3 + 8ce86357 + 6adb87c4 | ✅（2 修复轮，13/13 PASS） |
| SP4 | T2 mock gateway fixture 参数化 | 1ed426ec | ✅ |
| SP4 | **T3 四 Worker 页面级 matrix spec** | — | 🔶 **进行中（见 §4）** |
| SP4 | T4 BrowserHotPlexClient stop 合同 | 8de62bc0 | ✅（发现并修复真实缺陷：重复 Done 重发，256 上限去重） |
| SP4 | **T5 test:e2e:matrix 脚本 + CI job** | — | ❌ 未开始 |
| SP5 Live | T1 runbook | — | ❌ 未开始（文档，无代码依赖） |
| SP5 | T2 12 行验收模板 | — | ❌ 未开始（文档） |
| SP5 | T3–T8 人工执行 | — | ❌ **Human Gate：必须人工执行**，自动化不可替代 |
| 收尾 | 全分支 final review + PR | — | ❌ |

---

## 3. 当前关键状态

### 3.1 全部三个平台矩阵的 C04 仍为 expected-red

12 组合的 C01–C03 已全绿（通过真实平台入口 + 真实 parser/mapper）；C04（stop 单终态）在三个平台均报同一消息：
`scenario.go:206: C04: the stopped done reason must be stopped_by_user: got , want stopped_by_user`
C05–C08 因 runner 在 C04 失败后停止而尚未在已提交套件中运行（曾用 scratch 验证过逻辑正确）。

**C04 修复 = SP3-T5（stop fence）**，这是解锁 12×8 全绿与 SP1-T9 CI 门禁的关键路径。

### 3.2 未提交的工作树（两个被中断的实现者的现场）

```bash
git status --short   # 当前 HEAD=c2d44b44 之上:
 M internal/gateway/contracttest/worker_probe.go        # SP3-T5 扩展（channels/counters）
 M internal/gateway/contracttest/worker_probe_test.go   # SP3-T5 扩展
?? internal/gateway/stop_contract_test.go               # SP3-T5 新测试（C04/C05 合同）
?? internal/gateway/stop_fence.go                       # SP3-T5 fence 实现（已写，未接线）
?? internal/gateway/stop_fence_test.go                  # SP3-T5 fence 单测
?? webchat/e2e/platform-worker-matrix.spec.ts           # SP4-T3 matrix spec（未完成）
```

**继续 SP3-T5 时**: 从这些文件继续（fence API 已按计划写就：`Claim/Rollback/BeginTurn`，显式 `mu`，`claimed map[sessionID]workerRunID`）；接线点：`commands.go` handleControl（Claim 在 Worker 调用前）与 `handler.go` handleInput（BeginTurn 在 primary Input 前）。当前 stop_contract_test.go 引用 `probe.FailNextStop`（尚未在 probe 实现）。

### 3.3 SP3-T5 实现者遗留的关键分析（C04 确定性难题）

实现者已完成深入分析，核心结论（写入其报告前被中断，记录于此）：

1. **生产侧并不在 stop 后抑制 Worker 的 done**：四个 Worker adapter 内无 `IsStopped()` 抑制点；`IsStopped` 仅用于 `handler.go:603`（mid-turn 注入）与 `bridge_forward.go:908`（crash fallback 抑制）。
2. **probe 的 done 在 `Input` 内同步发射**（WorkerProbe.EmitBasicTurn），先于 stop 到达，因此成为 snapshot 锚点 → C04 red。fence 只能让第二次 stop 不产生第二个 done（stopCalls=1），**不能移除 probe-done**。
3. **到达顺序存在真实竞态**：forwarder 的 probe-done 经 broadcast 队列路由；handler 的 stop-done 亦经 broadcast；probe-done 的 enqueue 被 forwarder 的 `finishRuntimeOnDone`（SQLite OpenBySession，writeMu 串行化）延迟，stop-done 可能先到。已观察到的到达序：`runtime.failed(8), done(9), done(11), done(7)`。
4. **候选解法（实现者分析到、未定论）**：让 probe 的 done 发射**门控于 per-turn stopped 状态**（`!p.stopped.Load()` 在发射点检查），并把发射点延迟到 `StopCurrentTurn` / 下一轮 `Input` / conn close——但 C01 等正常轮需要 done 无需触发，此路不通；另一候选：**snapshot 语义**（driver 层）——但 runner 冻结。
5. **需要继续者裁决**：C04 的"单一 stopped_by_user"合同如何在 probe 同步发射 done 的现实下达成。选项：(a) 修改 probe：`EmitBasicTurn` 拆为 delta 同步 + done 由 `Input` 返回后经**异步 goroutine 稍后发射**（用 channel 信号而非 sleep），使 stop 有机会先到并置 stopped，done 发射前检查 stopped 则抑制——C01 正常轮无 stop 时 done 照发；(b) 与 plan 作者/评审确认 C04 断言语义是否可接受"首个 terminal 为 stopped done 且仅一个 done 总数"的变体；(c) 将 C04 的 probe 行为显式建模为"stop 后不再发射 turn done"。**不要修改 runner/harness 冻结接口**（plan 明确禁止），但 `contracttest/worker_probe.go` 是本任务在册文件，可扩展。

### 3.4 SP4-T3 实现者遗留

matrix spec 文件已写大部分（四 Worker 参数化 + 六步流程），被中断于 mutation 验证（`/api/sessions` URL 带 query string，其 glob 未命中）。继续时：修复 mutation 模式 → 完成验证 → `PLAYWRIGHT_PORT=3001` 运行 → 提交 `test(webchat): cover four worker browser combinations`。

---

## 4. 剩余任务执行顺序（建议）

```
SP3-T5 stop fence（关键路径，进行中） ──► 三个平台矩阵 C04 转绿、C05–C08 跑通
   ├──► SP1-T9 CI 门禁（make test-contract-matrix + ci.yml）
   ├──► SP3-T6 reset 合同 · SP3-T7 mid-turn 合同（可并行）
SP4-T3 matrix spec（进行中，收尾） ──► SP4-T5 CI job（可并行）
SP2 已全部完成
SP5-T1 runbook + SP5-T2 模板（文档，随时可做，可与代码并行）
SP5-T3–T8 人工 12 组合验收（Human Gate）
最终: 全分支 review（merge-base 起）→ 修 P0/P1 → push → PR（hrygo token，链接 #954）
```

**依赖**：SP1-T9 依赖 SP3-T5（0 failed 才可审计）；SP5-T3 依赖全部代码合并 + runbook/模板。

---

## 5. 验证命令（每任务门禁）

```bash
rtk go test ./internal/gateway/... ./internal/messaging/feishu ./internal/messaging/slack ./internal/worker/... -count=1 -race   # 子集按需
rtk make test-contract-matrix        # SP1-T9 落地后：12 combinations, 96 core scenarios, 0 skipped, 0 failed
rtk pnpm --dir webchat test          # Vitest
rtk pnpm --dir webchat exec tsc --noEmit
PLAYWRIGHT_PORT=3001 rtk pnpm --dir webchat exec playwright test e2e/platform-worker-matrix.spec.ts --project=chromium
rtk make docs-build && rtk make check
```

---

## 6. 关键发现与候选 Issue（最终 review 时处理）

1. **机器审计发现（生产 quirk）**: delivered-ack（PriorityControl 直写）可超越 broadcast 队列中的 terminal done 先到——seq 与到达序反转。三平台 driver 的 snapshot 均已按此过滤并注释。候选 issue：是否修 Gateway 的发射顺序。
2. **SP2-T2 候选 Issue（brief 第 4 条约束）**: control/worker 命令在 bridge=nil 时仍提交 dedup——平台重试同 ClientMsgID 会被静默去重（虽未消费）。须经 Issue 修复，不得扩接口。
3. **SP2-T3 决策项**: 终端错误传播已会触发 Hub `detachAndCloseSessionWriter`（含 ContentPresented=true 的装饰性失败）——feishu 对齐语义；候选后续决策：降级为 Warn+nil 或按事件类型门控 detach。
4. **SP3-T2 遗留**: gateway stop 路径收到真实 StopCurrentTurn 错误时仅 warn + "stop failed"，无 stopped done 也无 finishRuntimeOnStop（MarkStopped 已置位）。**SP3-T5 必须定义 failed-abort 收敛**（session 保留、可恢复）。
5. **C01 "nothing may follow the terminal" 断言因 snapshot 截断而平凡为真**——SP3-T5 不得依赖它。
6. **C04 红绿边界有竞态**：若 stopped done 先于 probe done 到达（broadcast 交错），C04 会静默转绿——SP3-T5 应确定性钉住顺序。
7. **SP4-T1 行为取舍**：失败项自动展开仅在"无打开 popover 且该项刚变失败"时触发（transition 语义）；期间其他 popover 打开时新失败不后补展开（有注释）。

## 7. 环境注意事项

- **PLAYWRIGHT_PORT=3001 必须设置**（主仓库陈旧 `next dev` 占 3000 端口，会伪造大量失败）。
- Git hooks 已装（scripts/git-hooks）；提交禁止 `--no-verify`。
- 并发多 agent 提交曾发生 staging 竞态（一个 commit 误吞另一 agent 的 staged 文件）——**提交前必须 `/usr/bin/git status --short` 确认只有自己的文件被 staged**；显式 path 限定 `git add`。
- 权限分类器（deepseek-v4-flash）间歇性不可用会阻塞 agent 的 Bash——重试即可，工作树状态安全。
- 敏感数据边界：测试 fixture 不得含真实凭证/路径；证据文档脱敏（SHA-256 短指纹）。

## 8. 如何继续（SDD 恢复）

1. 读取账本 `.superpowers/sdd/2026-08-05-platform-worker-e2e-alignment/progress.md`（权威记录）与各子计划 `task-N-brief.md`。
2. SP3-T5: 从工作树未提交文件继续（见 §3.2/§3.3），先裁决 C04 难题（§3.3.5），TDD 完成并跑通三平台矩阵；commit 信息 `test(gateway): enforce single stop terminal across workers`。
3. 每任务: `task-brief` 提取 → 实现者（sonnet）→ `review-package` + 任务评审 → 修复轮（≤5）。
4. SP4-T3: 收尾现有 spec 文件（§3.4），提交 `test(webchat): cover four worker browser combinations`。
5. SP1-T9: `scripts/test-contract-matrix.sh`（jq 审计 12×8、skip 即失败、trap 清理）+ Makefile target + ci.yml job；commit `ci(e2e): gate all platform worker combinations`。
6. 全部代码合并后: 最终全分支 review（最强调配模型）→ 修 P0/P1 → `git push`（非 main 分支直推，无需询问）→ PR（**hrygo token**，链接 #954 + spec，Source/Test/Live 分开陈述，Live 未执行必须明写）。
7. SP5: runbook + 模板先提交（NOT_RUN 态）；真实 12 组合人工验收由人执行（Human Gate），完成后才可关闭 Epic。

---

## 9. 完成定义（Epic 验收标准摘要）

- [ ] `make test-contract-matrix` 12/12、96 scenarios、0 skipped、0 failed
- [ ] Go race / make check / WebChat Vitest+tsc+Playwright / docs-build 全绿
- [ ] OCS abort 真实远端调用；四 Worker stop/reset/resume/interaction/mid-turn manifest 与协议一致
- [ ] Slack media/dedup/terminal 与飞书基线对齐且有 race-safe 回归测试
- [ ] WebChat 四 Worker 参数化场景覆盖 ACK/stream/stop/Done/next-turn
- [ ] runbook + 12 行模板落库；真实 12/12 人工 PASS 绑定同一 commit
- [ ] review 无 P0/P1；PR 合并；Epic 关闭
