# PR Review Webhook 优化：CI-Only 触发 + 动态 WorkDir

**版本**: v2.0 · **日期**: 2026-06-05 · **状态**: Draft  
**Issue**: #662

---

## 1. 目标

两项优化：

1. **CI-Only 触发**：webhook 仅在 CI 通过后触发 review，消除无效 SKIP session
2. **动态 WorkDir**：webhook 触发时 executor 计算 PR 专属工作目录，Worker 直接在目标目录启动

## 2. 当前行为与问题

### 2.1 事件订阅

GitHub Webhook 订阅 3 类事件：

| 事件 | Action | 触发条件 |
|------|--------|---------|
| `pull_request` | opened, synchronize, reopened, ready_for_review | 非 draft、state=open |
| `check_suite` | completed | conclusion=success |
| `check_run` | completed | conclusion=success |

### 2.2 问题 A：`pull_request` 事件触发过早

PR opened/pushed 时 CI 未完成 → worker 启动 → §1 CI 门控检测 pending → SKIP。

每次 SKIP 消耗 ~$0.20（冷启动 + system prompt 加载），典型 PR 浪费 1-2 次。实证：PR #659、#661 均出现 CI pending SKIP。

### 2.3 问题 B：WorkDir 静态 + Prompt 层 cd

当前数据流：

```
executor.go:70    job.WorkDir = "/tmp/hotplex" (静态)
    ↓
bridge.go:701     SessionInfo{ProjectDir: workDir}
bridge.go:775     env["GATEWAY_WORK_DIR"] = workDir
    ↓
worker.go:200     Proc.Start(..., session.ProjectDir)
    ↓
manager.go:115    cmd.Dir = dir
manager.go:124    os.MkdirAll(dir, 0o755)  ← 自动创建目录
    ↓
Worker 启动 cwd=/tmp/hotplex
    ↓
prompt §1.5: cd /tmp/pr-review-YYYYMMDD/pr-$PR  ← LLM 执行 shell cd
```

Worker 启动在 `/tmp/hotplex`，prompt §1.5 通过 `gh repo clone + cd` 切换到实际工作目录。这依赖 LLM 正确执行 cd（实测未失败，但冗余）。

### 2.4 关键发现

**`manager.go:124` 已有 `os.MkdirAll(dir, 0o755)`**。executor 只需设置 `workDir` 路径，目录创建由 proc manager 自动完成。executor 零额外代码。

---

## 3. 方案

### 3.1 CI-Only 触发

#### GitHub Webhook 订阅调整

| 事件 | 保留 | 理由 |
|------|:---:|------|
| `pull_request` | ❌ 移除 | CI 未完成时触发无意义 |
| `check_suite` | ✅ 保留 | CI 通过主信号 |
| `check_run` | ✅ 保留 | 细粒度 check 完成信号 |

#### 三层过滤

| 层级 | 位置 | 过滤逻辑 | 作用 |
|------|------|---------|------|
| L1: 订阅 | GitHub Webhook Settings | 仅勾选 `check_suite` + `check_run` | 消除 `pull_request` 事件 |
| L2: Handler | `webhook.go extractPRs` | `completed` + `conclusion=success` | 过滤 CI 失败 |
| L3: Worker | prompt §1 CI 门控 | 保留作为防御性校验 | 防止极端情况（CI 后又 push） |

#### `extractPRs` 简化

```go
// 移除 pull_request 分支
func (h *WebhookHandler) extractPRs(eventType string, e *GitHubEvent) []int {
    switch eventType {
    case "check_suite":
        if e.CheckSuite == nil || e.CheckSuite.Conclusion != "success" {
            return nil
        }
        return extractPRNumbers(e.CheckSuite.PullRequests)
    case "check_run":
        if e.CheckRun == nil || e.CheckRun.Conclusion != "success" || e.CheckRun.CheckSuite == nil {
            return nil
        }
        return extractPRNumbers(e.CheckRun.CheckSuite.PullRequests)
    }
    return nil
}
```

#### Prompt §1 简化

CI 门控降级为防御性校验。移除 `pending|queued|in progress` 检查（不应再出现）：

```bash
CI=$(gh pr checks $PR --repo hrygo/hotplex 2>&1 || true)
echo "$CI" | grep -qiE "fail|neutral|timed.out" && { echo "SKIP PR#$PR (CI failed)"; continue; }
```

### 3.2 动态 WorkDir

#### 核心思路

Webhook 触发时 `PlatformKey["pr_number"]` 已注入（`webhook.go` → `TriggerByName` → `executor.go`）。Executor 在 `StartSession` 前计算动态路径，作为 `workDir` 参数传入。

**无需创建目录**：`manager.go:124` 的 `os.MkdirAll` 自动处理。

#### Executor 改动

```go
// executor.go — Execute 方法内，StartSession 之前
workDir := job.WorkDir // 静态值: /tmp/hotplex

// Webhook 触发：计算 PR 专属工作目录
if prNum := job.PlatformKey["pr_number"]; prNum != "" && job.PlatformKey["trigger"] == "webhook" {
    workDir = filepath.Join(os.TempDir(),
        fmt.Sprintf("pr-review-%s/pr-%s", time.Now().Format("20060102"), prNum))
}

if err := e.bridge.StartSession(ctx, sessionKey, job.OwnerID, job.BotID,
    wt, job.Payload.AllowedTools, workDir, // 使用动态 workDir
    job.Platform, platformKey, title, "",
); err != nil {
```

#### 数据流（Webhook 触发）

```
executor.go         workDir = /tmp/pr-review-20260605/pr-662
    ↓
bridge.go:701       SessionInfo{ProjectDir: "/tmp/pr-review-20260605/pr-662"}
bridge.go:775       env["GATEWAY_WORK_DIR"] = "/tmp/pr-review-20260605/pr-662"
    ↓
manager.go:115      cmd.Dir = "/tmp/pr-review-20260605/pr-662"
manager.go:124      os.MkdirAll("/tmp/pr-review-20260605/pr-662", 0o755)  ← 自动创建
    ↓
Worker 启动 cwd=/tmp/pr-review-20260605/pr-662  ← 已在目标目录
    ↓
prompt §0: export GH_TOKEN=$(cat ~/.hotplex/secrets/github-hotplex-ai-token)
prompt §1.5: git init && git remote add origin ... && git fetch + checkout
              （不再需要 gh repo clone 和 cd）
```

#### 数据流（Cron Fallback）

Cron fallback 无 `PlatformKey["pr_number"]`，`workDir` 保持静态值 `/tmp/hotplex`，prompt §1.5 维持原逻辑（`gh repo clone + cd`）。

#### Prompt §1.5 修改

需区分 Worker 当前目录是否为目标目录：

```bash
## §1.5 检出 PR 分支(强制)
RB="/tmp/pr-review-$(date +%Y%m%d)" && mkdir -p "$RB"
PD="$RB/pr-$PR"

if [ "$(pwd)" = "$PD" ]; then
  # Webhook 触发：executor 已设置 cwd 为目标目录
  git init
  git remote add origin https://github.com/hrygo/hotplex.git
  git fetch origin "+pull/$PR/head:review"
  git checkout review
elif [ -d "$PD/.git" ]; then
  cd "$PD" && git fetch origin "+pull/$PR/head:review" && git reset --hard review
else
  gh repo clone hrygo/hotplex "$PD" -- --no-checkout
  cd "$PD" && git fetch origin "pull/$PR/head:review" && git checkout review
fi
```

---

## 4. Cron Fallback 不变

Cron fallback（`every:30m`）无 `PlatformKey["pr_number"]`，所有行为保持现状：

- `workDir` = `/tmp/hotplex`（静态）
- prompt §0-§1.5 完整执行
- §1.5 走 `gh repo clone + cd` 路径

---

## 5. 涉及文件

| 文件 | 类型 | 说明 |
|------|------|------|
| GitHub Webhook Settings | 配置 | 取消 `pull_request` 事件订阅 |
| `internal/gateway/webhook.go` | 修改 | `extractPRs` 移除 `pull_request` 分支 |
| `internal/gateway/webhook_test.go` | 修改 | 更新 pull_request 相关测试 |
| `internal/cron/executor.go` | 修改 | 动态 WorkDir 计算（~6 行） |
| cron prompt | 修改 | §1 简化 + §1.5 新增 webhook 路径 |

---

## 6. 验收标准

### CI-Only 触发

| ID | 条件 | 验证方法 |
|----|------|---------|
| AC-1 | PR opened 不触发 review | 创建 PR → gateway 日志无 trigger |
| AC-2 | PR push 不触发 review | push commit → gateway 日志无 trigger |
| AC-3 | CI 通过触发 review | CI 绿勾 → 日志 `trigger pr-review-hotplex pr=N` |
| AC-4 | CI 失败不触发 review | CI 红叉 → 日志无 trigger |
| AC-5 | 24h 内 webhook SKIP = 0 | webhook 触发 session 无 CI pending SKIP |

### 动态 WorkDir

| ID | 条件 | 验证方法 |
|----|------|---------|
| AC-6 | Webhook 触发：Worker cwd = `/tmp/pr-review-YYYYMMDD/pr-N` | §1.5 首条输出 `pwd` |
| AC-7 | Webhook 触发：§1.5 走 `git init` 路径 | session 日志无 `gh repo clone` |
| AC-8 | Cron 触发：行为不变 | `hotplex cron trigger` → 走 `gh repo clone` 路径 |
| AC-9 | 重复 webhook：同一 PR 目录复用 | 第二次触发走 `git fetch + reset` 路径 |

---

## 7. 回滚

```bash
# 1. GitHub Webhook Settings 重新勾选 pull_request 事件
# 2. redeploy webhook.go + executor.go（恢复 pull_request 分支，移除动态 WorkDir）
# 3. 恢复 prompt §1 完整 CI 门控 + §1.5 原 gh repo clone 逻辑
```

---

## 8. R2-R4 备注

R2-R4 为用户已同意的改进项，原始提案待确认。在 issue #662 中标记为 checklist。
