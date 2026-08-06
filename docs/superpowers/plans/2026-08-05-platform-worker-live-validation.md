# 真实 12 组合人工验收 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to prepare the runbook and template. The 12 live runs themselves MUST be performed and attested by an authorized human. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为飞书、Slack、WebChat × 四 Worker 建立可重复、可脱敏、绑定精确 commit 的人工验收流程，并在 12 个真实组合全部 PASS 后形成 Live 证据闭环。

**Architecture:** runbook 先锁定候选 commit、隔离环境和配置摘要，再逐组合执行同一 basic → stop → next-turn → interaction 流程；运行时证据通过内置 doctor/status/Admin API 和平台截图交叉验证，仓库只保存短指纹、受控状态、时间窗和外部证据引用。

**Tech Stack:** HotPlex CLI/Admin API、真实飞书、真实 Slack、真实 WebChat、四个真实 Worker、Markdown evidence record。

## Human Gate

- 初级工程师可以准备环境、runbook、模板和失败分析，但不得替人工执行者把 `NOT_RUN`/`BLOCKED` 改成 `PASS`。
- 每行 PASS 必须有一名人工执行者、北京时间时间窗、平台侧结果、Gateway/Worker 侧结果和清理结果。
- 12 行必须绑定同一个候选 commit。代码变化后，旧记录保留历史价值但不能自动证明新 commit。
- 任一 `FAIL(issue/PR)` 或 `BLOCKED(reason)` 都不计通过，Epic #954 保持开放。
- 禁止把 token、cookie、tenant/user 真名、prompt 原文、metadata 值、完整 session/request ID、原始 Worker 错误或未脱敏本机路径提交到仓库/Issue。

---

### Task 1: 编写人工验收 runbook

**Files:**
- Create: `docs/guides/developer/platform-worker-e2e-validation.md`

**Interfaces:**
- Consumes: Issue #954、Approved Spec、`hotplex-diagnostics` 内置诊断顺序。
- Produces: 环境准备、逐组合操作、采证、失败分类、恢复和清理的唯一 runbook。

- [ ] **Step 1: 写文档结构**

固定章节：目的与证据等级、角色/HITL、环境隔离、候选 commit 锁定、凭证边界、preflight、平台配置、逐组合流程、证据字段、失败分类、恢复、清理、最终签字。

文档开头必须写：普通 CI 结果是 Test，不是 Live；此 runbook 的 12 行人工记录才是 Live。

- [ ] **Step 2: 固定 preflight 命令**

所有命令在候选 worktree/release 环境运行，并使用 `rtk`：

```bash
rtk git rev-parse HEAD
rtk git status --short
rtk hotplex doctor
rtk hotplex security
rtk hotplex status
rtk hotplex service status
rtk curl --fail --silent http://127.0.0.1:8888/health
rtk curl --fail --silent http://127.0.0.1:9999/admin/health/ready
```

Admin token 由执行者预先注入当前 shell 的专用变量 `HOTPLEX_E2E_ADMIN_TOKEN`；runbook 不提供从配置文件打印 token 的命令。健康检查：

```bash
rtk curl --fail --silent --header "Authorization: Bearer ${HOTPLEX_E2E_ADMIN_TOKEN}" http://127.0.0.1:9999/admin/health/workers
rtk curl --fail --silent --header "Authorization: Bearer ${HOTPLEX_E2E_ADMIN_TOKEN}" http://127.0.0.1:9999/admin/sessions
```

执行者只把受控字段写入记录：commit、HotPlex version、worker type、state、timestamp、短指纹；curl 原始输出不提交。

- [ ] **Step 3: 固定隔离和配置规则**

- 飞书和 Slack 分别使用专用 E2E bot、专用群/频道、专用测试用户；不得在生产群操作；
- WebChat 使用专用 E2E workspace，workdir 固定为不含真实用户名的临时测试项目；
- 每个组合创建新 session；stop 后的 next-turn 必须在该组合同一 session 内；下一组合不得复用上一组合 session；
- 飞书/Slack 用该 E2E bot 的 `bots[].worker_type` 选择 Worker；WebChat 用新建 session 的 worker selector；不得依赖共享默认 fallback；
- 配置变更后如需重启，只允许 `rtk hotplex service restart`，禁止 `stop && sleep && start`；
- 用 `/admin/debug/sessions/{id}` 的脱敏视图确认实际 `platform`、`worker_type` 和 session state，不能仅凭 UI 下拉框认定路由正确。

- [ ] **Step 4: 固定每个组合的操作脚本**

每行由人工按顺序执行，probe 内容现场发送但不复制到 evidence：

1. **BASIC**：发送含组合 ID 和随机 nonce 的无敏感 probe；确认平台收到、Gateway 接受、至少一个 stream 更新、一个正常 terminal；
2. **STOP**：发起可产生持续增量的受控任务；看到首个增量后人工点 Stop/发送 stop command 两次；确认只出现一个 stopped terminal，UI 不再继续增量；
3. **NEXT**：同 session 发送新 nonce；确认正常完成，session 未被删除、无 crash fallback；
4. **INTERACTION**：要求真实 Worker 在回答前使用其原生 permission/question/elicitation 机制询问一次；人工回复后确认 turn 继续且 response 只交付一次；
5. **OBSERVE**：查看 worker health、session debug 和 metrics 时间窗；确认 worker_type 正确，`gateway_platform_dropped_total`/`gateway_deltas_dropped_total` 没有因本次异常增长；
6. **CLEANUP**：通过 UI/受支持 Admin endpoint 删除测试 session，确认本地终态和远端 cleanup 最终收敛；不得手动写 SQLite。

重复 stop 的成功标准是一个 terminal，不要求平台展示两个“已停止”提示。若 Worker 在人工动作前已自然完成，STOP 判为 `NOT_RUN` 并重新执行该组合，不把自然完成计为 stop PASS。

- [ ] **Step 5: 固定故障分类**

只允许以下 `failure_class`：`PROCESS_DOWN`、`ROUTING_MISMATCH`、`INGRESS_REJECTED`、`WORKER_START_FAILED`、`NO_STREAM`、`STOP_FAILED`、`DUPLICATE_TERMINAL`、`NEXT_TURN_FAILED`、`INTERACTION_FAILED`、`FEEDBACK_STALL`、`CLEANUP_FAILED`、`EVIDENCE_INCOMPLETE`。

反馈中断再按诊断技能细分：`PIPELINE_STALL`、`BACKPRESSURE_DROP`、`ADAPTER_FAILURE`、`CLIENT_DISCONNECT`。不得把原始远端错误当 failure class。

- [ ] **Step 6: 校验文档**

Run: `rtk make docs-build`

Run: `rtk rg -n 'token=|cookie=|session_id.:|request_id.:|/Users/|/home/' docs/guides/developer/platform-worker-e2e-validation.md`

Expected: docs build PASS；敏感模式无命中。示例中的环境变量名可出现，变量值不可出现。

- [ ] **Step 7: Commit**

```bash
rtk git add docs/guides/developer/platform-worker-e2e-validation.md
rtk git commit -m "docs(e2e): add live platform worker runbook"
```

### Task 2: 创建固定 12 行验收模板

**Files:**
- Create: `docs/assets/e2e/platform-worker-matrix-template.md`

**Interfaces:**
- Produces: 只含 `NOT_RUN` 的 12 行模板；执行时复制为 commit-specific record。

- [ ] **Step 1: 写不可省略的元数据**

模板顶部字段：

```markdown
- Candidate commit: NOT_SET
- HotPlex version: NOT_SET
- Environment class: isolated-live-e2e
- Gateway started at: NOT_SET
- Executed at (Asia/Shanghai): NOT_SET
- Human executor: NOT_SET
- Reviewer: NOT_SET
- CI matrix run URL: NOT_SET
- Evidence retention location: NOT_SET
```

不记录 bot 名、tenant、user、channel、workspace 真实标识。

- [ ] **Step 2: 写 exact 12 行表**

列固定为：`ID | Platform | Worker | Basic | Stop | Next | Interaction | Runtime | Cleanup | Evidence refs | Executor | Timestamp | Overall`。行顺序固定 F-C/F-O/F-X/F-A/S-C/S-O/S-X/S-A/W-C/W-O/W-X/W-A；所有 scenario 与 Overall 初始值必须是 `NOT_RUN`。

`Runtime` 只记录短指纹和受控摘要，例如 `sid_fp=12hex; worker=codex_cli; terminal=stopped_by_user`。`Evidence refs` 只放受访问控制的截图/日志工单 URI，不粘贴图片、日志或原始 ID。

- [ ] **Step 3: 增加状态校验脚本命令**

runbook 给出只读检查：

```bash
rtk rg -n '\| (NOT_RUN|FAIL\(|BLOCKED\()' docs/assets/e2e/platform-worker-matrix-<commit>.md
```

命令有任何输出就不能声明 12/12 PASS。`<commit>` 是文档中的说明性文件名占位符，执行时替换为 `git rev-parse --short=12 HEAD` 的实际值；模板本身不创建虚假 record。

- [ ] **Step 4: 校验 exact row set**

人工与 reviewer 分别核对 12 个 ID 各一次。再运行：

```bash
rtk rg -n '^\| (F-C|F-O|F-X|F-A|S-C|S-O|S-X|S-A|W-C|W-O|W-X|W-A) \|' docs/assets/e2e/platform-worker-matrix-template.md
```

Expected: 恰好 12 行。

- [ ] **Step 5: Commit**

```bash
rtk git add docs/assets/e2e/platform-worker-matrix-template.md
rtk git commit -m "docs(e2e): add twelve-combination live template"
```

### Task 3: 锁定候选 commit 与执行窗口

**Files:**
- Create during execution: `docs/assets/e2e/platform-worker-matrix-<12-char-commit>.md`
- Update externally: Issue #954 comment

- [ ] **Step 1: 代码门禁先通过**

必须先有：`make test-contract-matrix` 12/12、Go race、WebChat Vitest/Playwright、docs-build、make check、远端 CI green、review 无 P0/P1。任一未通过时不开始 Live。

- [ ] **Step 2: 复制模板并绑定 commit**

使用当前 HEAD 的 12 字符短 SHA 作为文件名；复制后立刻填 candidate commit 的完整 40 字符 SHA。文件创建/编辑使用 Edit/apply_patch，不用 shell 重定向或 `cp` 后批量替换。

- [ ] **Step 3: 在 Issue #954 发布执行窗口**

Issue comment 只包含候选 commit、版本、执行时间窗、12 行均 `NOT_RUN` 的 record 链接和负责人；不包含凭证/真实环境标识。这个 comment 是外部写操作，由负责人确认后发布。

### Task 4: 人工执行飞书四组合

**Files:**
- Modify: commit-specific live record

- [ ] F-C：按 BASIC/STOP/NEXT/INTERACTION/OBSERVE/CLEANUP 完整执行并签名。
- [ ] F-O：同上；确认 OCS stop 后远端 session 保留，下一轮复用成功。
- [ ] F-X：同上。
- [ ] F-A：同上。
- [ ] 每行结束立即更新记录，不在四行完成后凭记忆补录。
- [ ] 由第二人核对四行证据时间窗与 worker_type，不从 F-C 外推其余三行。

### Task 5: 人工执行 Slack 四组合

**Files:**
- Modify: commit-specific live record

- [ ] S-C：完整执行；另确认 10 MiB 边界测试使用独立无敏感测试文件，超限文件不落盘。
- [ ] S-O：同上；确认 OCS abort + next-turn。
- [ ] S-X：同上。
- [ ] S-A：同上。
- [ ] 每行检查 terminal 展示失败指标没有异常增长，且没有重复完整回复。
- [ ] 第二人核对四行。

### Task 6: 人工执行 WebChat 四组合

**Files:**
- Modify: commit-specific live record

- [ ] W-C：完整执行，确认实际 init/session worker_type。
- [ ] W-O：同上；确认 OCS abort + next-turn。
- [ ] W-X：同上。
- [ ] W-A：同上。
- [ ] 每行在浏览器刷新/重连后确认历史不重复 terminal。
- [ ] 第二人核对四行。

### Task 7: 失败处理与重验

- [ ] 失败行立即写 `FAIL(#issue)`；先保存脱敏时间窗/短指纹，再恢复服务。
- [ ] 使用 `hotplex doctor/status` → `/admin/health/workers` → session debug → metrics → logs 的顺序缩小根因；进程不在时停止后续反馈链路分析。
- [ ] 不手工修改 DB；异常 session 优先用受支持 terminate/delete endpoint。
- [ ] 修复进入新 commit 后，新建该 commit 的 record；不得擦掉旧失败或把旧 PASS 复制为新 PASS。
- [ ] 仅重验失败组合仍不足以证明新 commit 12/12；若修复触及共享 Gateway/Worker/platform terminal，重新执行受影响面，最低为该平台四行或该 Worker 三行，由 reviewer 记录理由。

### Task 8: Live 收口、提交和 Issue 更新

- [ ] `rtk rg -n '\| (NOT_RUN|FAIL\(|BLOCKED\()' <commit-record>` 无输出。
- [ ] reviewer 手工计数 12 行 Overall=`PASS`，并核对每行 Executor/Timestamp/Evidence refs/Cleanup 非空。
- [ ] `rtk make docs-build` PASS；敏感模式扫描无命中。
- [ ] commit record：`rtk git commit -m "docs(e2e): record live twelve-combination validation"`。
- [ ] push 后由负责人确认并更新 Issue #954：分别列 Source/Test/Live，链接 candidate commit、CI run、record 和失败修复 Issue；不得把“本地测试绿”写成 Live。
- [ ] 只有 12/12 PASS、review 通过且 record 已在目标分支时，才能关闭 Epic。

Expected final statement: `Live: 12/12 PASS for <full commit>; independently reviewed; no credentials or raw identifiers stored.`
