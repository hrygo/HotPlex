---
title: Cron AI-native 定时任务调度器
weight: 5
description: HotPlex AI-native Cron 调度器设计：自然语言到定时任务、timerLoop 引擎、并发槽控制与平台投递
---

# Cron AI-native 定时任务调度器

> HotPlex 的 Cron 不是传统的 crontab 包装器，而是一个 AI-native 的定时任务系统——用户用自然语言描述任务意图，系统自动识别并创建定时任务，Worker 执行后自动投递结果到 Slack/飞书。

## 核心问题

传统的定时任务系统（如 crontab、Airflow）有三个痛点：

1. **创建门槛高**：用户需要理解 cron 表达式语法（`0 9 * * 1-5`），知道如何配置执行环境，手动设置超时和重试。
2. **结果不可见**：任务执行完毕后，结果留在日志文件里，用户需要主动查看。对于 Slack/飞书用户来说，这打破了对话式交互的连贯性。
3. **与 Agent 割裂**：传统 cron 执行的是脚本或命令，而不是 AI Agent。无法利用 Agent 的上下文理解能力来处理复杂任务。

HotPlex Cron 的目标是让定时任务成为 AI Agent 能力的一部分——用户在对话中说"每天早上 9 点提醒我检查系统健康"，Agent 自动理解意图、创建任务、定时执行、将结果发回对话频道。

## 设计决策

### AI-native 创建流程

Cron 任务的创建不通过 CLI 手动输入 cron 表达式，而是通过 Agent 的意图识别：

```
用户："每天早上 9 点检查系统健康状态"
  -> Agent（通过 Skill Manual）识别为 cron 创建意图
  -> Agent 调用 hotplex cron create 命令
  -> Scheduler.CreateJob() 创建任务
  -> timerLoop.arm() 重新计算下次触发时间
```

Skill Manual（`cron-skill-manual.md`）通过 `go:embed` 编译进二进制文件，在 Scheduler 启动时释放到 `~/.hotplex/skills/cron.md`。Agent 在 B 通道的 `<skills>` 中读取这个手册，获得 cron 任务的创建语法和参数说明。

### 3 种调度类型

Cron 支持三种调度语义，覆盖从定时循环到一次性触发的全部场景：

| 类型 | Kind | 触发规则 | 使用场景 |
|------|------|---------|---------|
| Cron 表达式 | `ScheduleCron` | 标准 5 位 cron（分时日月周） | "每个工作日早上 9 点" |
| 固定间隔 | `ScheduleEvery` | 每 N 毫秒 | "每 30 分钟提醒一次" |
| 一次性 | `ScheduleAt` | ISO-8601 时间戳 | "明天下午 3 点部署" |

**Cron 表达式标准化**：`normalize.go` 处理常见的 cron 语法变体：
- `?` 替换为 `*`（Quartz 兼容）
- 周几名称映射（`MON` -> `1`，`SUN` -> `0`）

**最小间隔保护**：`every` 类型强制最小 60 秒间隔（`every_ms < 60000` 被拒绝），防止用户创建过于频繁的任务消耗 Worker 资源。

**时区支持**：Cron 表达式支持 `TZ` 字段，默认使用 `time.Local`。`at` 类型和 `every` 类型隐式使用系统时区。

### 并发槽控制：At-Most-Once 语义

Cron 调度器保证**同一个 Job 不会并发执行**——如果上一次执行还没完成，下一次触发会被跳过。这是通过 `timerLoop.running` 的 CAS（Compare-And-Swap）操作实现的：

```go
func (tl *timerLoop) tryAcquireSlot(max int) bool {
    for {
        cur := tl.running.Load()
        if int(cur) >= max {
            return false  // 全局并发上限
        }
        if tl.running.CompareAndSwap(cur, cur+1) {
            return true   // 成功获取槽位
        }
    }
}
```

**为什么用 CAS 而非 Mutex**：CAS 是无锁的，不会阻塞其他 goroutine。在高并发场景下（多个 Job 同时到期），CAS 避免了 mutex 竞争导致的延迟。默认 `max_concurrent_runs` 为 3，防止同时启动过多 Worker 进程导致资源耗尽。

并发上限是**全局的**——所有 Job 共享同一个并发池。当并发槽已满时，到期的 Job 被跳过（而非排队等待）。对于 `every` 类型的 Job，下一个 tick 周期会自动触发；对于 `cron` 类型，下一个调度时间会触发。

### 生命周期控制

每个 Job 支持可选的生命周期限制，实现"自毁式"定时任务：

| 参数 | 语义 | 效果 |
|------|------|------|
| `MaxRuns` | 最大执行次数 | 达到后自动 disable |
| `ExpiresAt` | 过期时间（RFC3339） | 过期后自动 disable |
| `DeleteAfterRun` | 执行后删除（at 类型） | 一次性任务的自清理 |
| `Enabled` | 启用/禁用开关 | 管理员暂停/恢复 |
| `Silent` | 静默模式 | 跳过结果投递 |
| `MaxRetries` | 最大重试次数（at 类型） | 指数退避重试上限 |

`at` 类型任务执行成功后自动 disable。如果设置了 `DeleteAfterRun`，则执行后直接从 DB 和内存索引中删除。连续调度错误（如无效的 cron 表达式）达到 5 次也会自动 disable。

## 内部机制

### timerLoop：Tick 引擎

`timerLoop` 是 Cron 调度器的核心引擎。它使用 `time.AfterFunc` 而非 `time.Ticker`，每次 tick 后重新计算到下一个到期 Job 的时间间隔：

```
arm(duration)
  -> timer.Stop()（取消旧定时器）
  -> time.AfterFunc(duration, onTick)

onTick()
  -> collectDue(now)（收集到期 Job）
  -> 对每个到期 Job：
       1. NextRun() 计算下次触发时间
       2. UpdateState() 持久化状态（At-Most-Once 保证）
       3. putJob() 更新内存索引
       4. tryAcquireSlot() 获取并发槽
       5. go executeJob()（异步执行）
  -> arm(nextTickDuration)（重新计算下次 tick）
```

**为什么用 AfterFunc 而非 Ticker**：

Ticker 以固定间隔触发，无论是否有到期 Job。AfterFunc 精确计算到下一个到期 Job 的时间间隔：
- 如果最近的 Job 在 5 秒后到期，`arm(5s)`
- 如果最近的 Job 在 1 小时后到期，`arm(1h)`
- 如果没有任何 Job，`arm(60s)`（最大间隔上限 `maxTimerInterval`）

这避免了空闲期间的无效 tick，减少 CPU 唤醒。

**At-Most-Once 保证**：

在执行 Job 之前，先计算并持久化 `next_run_at_ms`。如果 Gateway 在执行过程中崩溃，重启后 `loadFromDB` 会发现 `next_run_at_ms` 已经被推进到未来时间，不会重复执行。

对于 `at` 类型，`NextRun()` 在时间已过后返回零时间（`time.Time{}`），`next_run_at_ms` 被设为大负值，`collectDue` 跳过 `NextRunAtMs <= 0` 的条目，防止同一 tick 内重复执行。

### 内存索引与持久化

Scheduler 维护一个内存 map `jobs map[string]*CronJob` 作为索引，所有 CRUD 操作同步更新内存和 SQLite：

```
CreateJob():
  1. ValidateJob() -- 校验 schedule、payload
  2. NextRun() 计算初始 next_run_at_ms
  3. store.Create() 持久化到 SQLite
  4. s.jobs[id] = job 更新内存索引
  5. tickLoop.arm() 重新计算 tick 间隔
```

所有返回给调用方的 Job 都是 `Clone()` 的深拷贝，包含 map（`PlatformKey`）和 slice（`AllowedTools`）的独立复制，防止外部修改影响内部状态。

`ReloadIndex()` 方法支持外部触发（如 SIGHUP）重新从 DB 加载索引，用于 CLI 修改 Job 后的通知场景。

### 执行器：Executor

`Executor` 将 Cron Job 转化为一个完整的 Session 生命周期：

```
Execute(ctx, job, timeout):
  1. DeriveCronSessionKey(job.ID, now.UnixNano())
     -- 每次 epoch 产生唯一 Session ID
  2. bridge.StartSession() -- 创建并启动 Worker
     -- platform="cron" 触发 MCP 注入抑制
  3. sm.GetWorker() -- 获取 Worker 引用
  4. w.Input(prompt) -- 投递格式化 prompt
  5. waitForCompletion() -- 轮询 Session 状态直到非 RUNNING
```

**Cron 平台的 MCP 抑制**：

Cron Session 使用 `platform="cron"` 标识。Bridge 在构建 Worker 配置时检测到 cron 平台，注入空 MCP 配置 `{"mcpServers":{}}`，同时设置 `StrictMCPConfig=true`。这阻止 Cron Worker 加载任何 MCP 服务器，节省约 600 MB/worker 的内存开销。Cron 任务通常不需要文件系统访问或外部工具，MCP 抑制是合理的资源优化。

**Prompt 格式化**：

投递给 Worker 的 prompt 包含元信息和投递指令：

```
[cron:<job_id> <job_name>] <message>
<timestamp>

## 结果投递（必须执行）
任务执行完成后，必须通过以下命令将结果投递给用户...
```bash
hotplex slack send-message --text "结果内容" --channel <channel_id>
```
```

这种设计将投递能力交给 Agent 本身——Agent 执行完任务后，会看到投递指令并自动调用相应的 CLI 命令发送结果。

### 结果投递：Delivery

Delivery 模块提供两种投递机制：

**Gateway 投递（Delivery 模块）**：

```
Deliver(ctx, job, sessionKey):
  1. extract(ctx, sessionKey) -- 从 EventStore 提取最后的 assistant 回复
  2. deliverFn(ctx, platform, platformKey, response) -- 按 platform 路由
```

`extract` 是一个 `ResponseExtractor` 回调函数，从 Gateway 的 EventStore 中检索指定 Session 的最后一条 assistant 响应。`deliverFn` 是 `PlatformDeliverer` 回调，根据 platform 类型调用对应的 SDK。

**CLI 投递（prompt 内嵌）**：

通过 `HasCLIDelivery` 判断 Job 是否有足够的平台信息（如 `channel_id`）。如果有，投递指令会被嵌入 Cron prompt 中，让 Worker 自己执行。

**投递决策逻辑**：

```
if Silent -> 跳过投递
if HasCLIDelivery -> CLI 投递（Worker 自己发送）
else -> Gateway 投递（Delivery 模块通过 SDK 发送）
if platform="" || platform="cron" -> 不投递
```

**投递重试**：Gateway 投递失败时，临时性错误（429、timeout、5xx）会进入内存重试队列，最多重试 3 次，指数退避 30s → 1m → 2m（上限 5m）。永久性错误（403、404）立即丢弃并记录日志。重试队列容量 100 条目（FIFO 驱逐），后台 `retryLoop` 以 10s tick 间隔检查到期重试。优雅关闭时，队列中残留的重试被记录为永久丢失。

**注意**：重试队列是内存中的，进程重启后未执行的重试会丢失。这适用于绝大多数场景——投递失败通常是临时性的（限流、网络抖动），在几秒到几分钟内重试即可恢复。

**Silent 模式**：当 `job.Silent = true` 时，跳过所有投递逻辑。用于"静默检查"类型的任务——只执行不通知。

### Catch-up 机制：错过任务的补偿

Gateway 重启后，可能有 Job 在停机期间错过了触发时间。`loadFromDB` 实现了 catch-up 逻辑：

```
1. 清理 stale running_at_ms（上次崩溃可能留下的脏状态）
2. 对每个到期 Job：
   a. 计算宽限期（grace period）= 调度间隔 * 50%，上限 2 小时
   b. 如果在宽限期内 -> 立即执行（catch-up）
   c. 如果超出宽限期 -> 重新计算 next_run（recurring）或 disable（one-shot）
3. Catch-up 执行策略：
   a. 前 5 个立即执行
   b. 其余以 5 秒间隔 staggered 执行
```

**为什么是 50% interval**：如果一个每小时执行的任务在 30 分钟的窗口内被错过，补执行是合理的。但如果已经错过 2 小时以上，补执行可能产生过时的结果，不如跳过等下一次调度。

**staggered 执行**：大量 catch-up Job 同时执行会耗尽并发槽。前 5 个立即执行，其余每 5 秒启动一个，平滑资源消耗。

### at 类型的指数退避重试

`at` 类型 Job 失败时触发重试（recurring Job 不重试——下一个调度周期会自然重试）：

```
失败 1 次 -> 等待 30s  -> 重试
失败 2 次 -> 等待 1m   -> 重试
失败 3 次 -> 等待 5m   -> 重试
失败 4 次 -> 等待 15m  -> 重试
失败 5 次 -> 等待 1h   -> 重试（到达 max_retries 后停止）
```

重试仅针对**临时性错误**（timeout、rate limit、5xx），永久性错误（配置错误、认证失败）不会触发重试。

重试通过修改 `NextRunAtMs` 实现——将下次执行时间设为 `now + backoff_delay`。这让 timerLoop 的正常 tick 机制处理重试，无需额外的重试队列。

### YAML 批量导入

`LoadFromYAML` 支持 YAML 文件定义 Job，通过 `name` 字段实现幂等 upsert：

```yaml
# cron 表达式调度
- name: daily-health
  schedule: "cron:0 9 * * 1-5"
  message: "检查系统健康状态"
  bot_id: "${BOT_ID}"
  owner_id: "${USER_ID}"

# every 固定间隔调度
- name: remind-water
  schedule: "every:30m"
  schedule_every_ms: 1800000  # 可选：直接指定毫秒数（覆盖 every 解析）
  message: "提醒喝水"
  bot_id: "${BOT_ID}"
  owner_id: "${USER_ID}"
  max_runs: 6
  expires_at: "2026-05-11T00:00:00+08:00"

# at 一次性调度
- name: deploy-prod
  schedule: "at:2026-05-15T14:00:00+08:00"
  schedule_at: "2026-05-15T14:00:00+08:00"  # 可选：显式 ISO-8601 时间戳
  message: "执行生产环境部署"
  bot_id: "${BOT_ID}"
  owner_id: "${USER_ID}"
  delete_after_run: true
  max_retries: 3
```

YAML 导入在 Gateway 启动时执行，覆盖同名 Job 的定义但保留运行状态（run_count、last_run_at_ms 等）。这使得 Job 定义可以版本化管理（存入 Git 仓库）。

### 关闭与优雅退出

`Shutdown()` 执行三步优雅退出：

```
1. closed.Store(true) -- 阻止新 Job 启动
2. cancelFn() -- 取消 ctx，停止 timerLoop
3. tickLoop.stop() -- 停止定时器
4. wg.Wait() -- 等待所有执行中的 Job 完成
```

`ctx.Done()` 提供超时控制。如果在超时前 `wg.Wait()` 未返回，说明有 Job 仍在执行，记录 warning 日志后退出。

## Attached Session：注入已有 Session

常规 Cron 执行每次创建独立的 Worker Session——适合独立任务（每日报告、定期检查），但无法利用已有对话上下文。

`attached_session` 载荷类型解决这一问题：将 prompt 直接注入**已有的 Session**，跳过 Worker 创建和销毁。

**与常规执行的对比**：

| 维度 | 常规执行 | Attached Session |
|------|---------|-----------------|
| Session | 每次 epoch 新建唯一 Session | 复用已有 Session |
| Worker 生命周期 | 完整创建→运行→销毁 | 复用已有 Worker |
| 上下文 | 无历史记忆 | 继承完整对话上下文 |
| 开销 | 高（进程创建 ~2s） | 低（直接注入） |
| 适用场景 | 独立报告、定期巡检 | 需要上下文连续性的操作 |

**执行路径**（`AttachedSessionHandler`）：

```
Dispatch(job):
  target := job.Payload.TargetSessionID
  info := router.GetSessionInfo(target)

  if info.Status == RUNNING:
    router.InjectInput(target, prompt, metadata)
    -- 直接投递到运行中的 Worker

  if info.Status == DORMANT:
    router.ResumeAndInput(target, workDir, prompt, metadata)
    -- 恢复 Session 后投递
```

桥接通过 `AttachedSessionRouter` 接口实现，由 `cmd/hotplex/` 中的适配器连接 `Bridge` + `SessionManager`。

## 权衡与限制

1. **全局并发上限不区分优先级**：所有 Job 共享同一个并发池（默认 3）。一个低优先级的批量任务可能占满槽位，阻塞高优先级的通知任务。当前没有基于 Job 优先级的队列调度。

2. **轮询式完成检测**：`waitForCompletion` 每 2 秒轮询 Session 状态，而非通过事件驱动。这增加了最多 2 秒的检测延迟，但实现简单且不依赖事件订阅机制。

3. **无分布式协调**：整个 Scheduler 运行在单个 Gateway 进程中。如果运行多个 Gateway 实例，需要确保只有一个实例运行 Cron（否则会重复执行）。当前依赖外部机制（如 systemd 单实例）保证。

4. **at 类型无持久重试队列**：`at` 类型的重试通过 `scheduleRetry` 在内存中调度。如果 Gateway 在重试等待期间重启，重试机会丢失。持久重试需要将重试状态写入 DB，当前未实现。

5. **CLI 投递的可靠性**：CLI 投递依赖 Worker 执行 `hotplex slack send-message` 命令。如果 Worker 崩溃或忽略投递指令，结果会丢失。Gateway 投递（Delivery 模块）更可靠——内置临时错误重试（最多 3 次，指数退避），且通过 EventStore + SDK 回调独立于 Worker 生命周期。

## 参考

- `internal/cron/cron.go` -- Scheduler 核心：内存索引、CRUD、catch-up
- `internal/cron/timer.go` -- timerLoop tick 引擎：collectDue -> CAS -> executeJob
- `internal/cron/types.go` -- CronJob/CronSchedule/CronPayload 数据结构 + Clone()
- `internal/cron/schedule.go` -- 三种调度：cron / every / at + NextRun 计算
- `internal/cron/executor.go` -- Worker 执行适配：Session 创建、prompt 格式化、投递指令
- `internal/cron/delivery.go` -- 结果投递：extract + PlatformDeliverer 回调
- `internal/cron/store.go` -- SQLite 持久化：ErrJobNotFound 哨兵、jobColumns 常量
- `internal/cron/loader.go` -- YAML 批量导入：name 幂等 upsert
- `internal/cron/skill.go` -- go:embed Skill Manual
- `internal/cron/retry.go` -- at 类型指数退避重试
- `internal/cron/normalize.go` -- cron 表达式标准化

---

## 案例：自动化 PR Review Prompt

以下为 `pr-review-hotplex` cron job 的完整 Prompt，展示了一个生产级 AI-native 定时任务的 Prompt 设计模式。

### 设计要点

| 特征 | 实现方式 | 目的 |
|------|---------|------|
| 身份验证 | §0 `gh api user` 确认 hotplex-ai | 防止错误身份提交 review |
| 去重 | §1 比较 commit date vs last review date | 避免重复审查同一 commit |
| CI 门控 | §1 `gh pr checks` 检测 pending/fail | CI 未通过不审查 |
| 隔离检出 | §1.5 `/tmp/pr-review-YYYYMMDD/pr-$PR` | 隔离工作区，防路径穿越 |
| 预读共享 | §2 主进程读取 diff + CLAUDE.md | 消除 agent 间重复读取 |
| 双维度并行 | §3 Agent A(正确性) + B(架构) | 覆盖安全/并发/性能/文档 |
| 置信度过滤 | §4 <75 丢弃 | 减少误报 |
| 结果投递 | §5 `gh api` 提交 review | 无需人工干预 |

### Prompt 模式

Prompt 采用**分段门控**模式：每个 § 是一个阶段，前一阶段的输出决定是否继续。这避免了 LLM 跳过关键步骤（如 CI 门控）直接进入 review。

```
§0 身份验证 → §1 去重+CI门控 → §1.5 检出 → §2 预读 → §3 并行Review → §4 过滤 → §5 提交
     ↓               ↓                            ↓
  失败→退出     SKIP→exit                    发现→汇总→提交
```

### 完整 Prompt

```
你是 hrygo/hotplex 的自动化 PR Review 系统。身份: hotplex-ai。

## 铁律
1. §1 去重+CI门控是硬性门控,必须先输出结果再决定是否继续
2. 一个 PR 每次最多提交一条 review
3. 无 P0/P1 必须 APPROVE
4. 不确定标记 [UNCERTAIN],不凑数
5. §1.5 检出是强制步骤,agent 只能从 $PR_DIR 读源文件

## §0 身份
export GH_TOKEN=$(cat ~/.hotplex/secrets/github-hotplex-ai-token)
gh api user --jq ".login"  # 必须 hotplex-ai

## §1 去重 + CI 门控(强制首步)
echo $TARGET_PR  # 非空=webhook仅该PR,空=cron枚举所有open PR
for PR in ${TARGET_PR:-$(gh pr list --repo hrygo/hotplex --state open --json number --jq '.[].number')}; do
  HEAD=$(gh pr view $PR --repo hrygo/hotplex --json headRefOid --jq '.headRefOid')
  COMMIT_DATE=$(gh api "repos/hrygo/hotplex/commits/$HEAD" --jq '.commit.committer.date')
  LAST_REVIEW=$(gh api "repos/hrygo/hotplex/pulls/$PR/reviews?per_page=100" --jq "[.[]|select(.user.login==\"hotplex-ai\")|select(.state!=\"DISMISSED\")]|max_by(.submitted_at).submitted_at//\"\"")
  NEED=false; [ -z "$LAST_REVIEW" ] && NEED=true
  [ "$NEED" = false ] && [[ "$COMMIT_DATE" > "$LAST_REVIEW" ]] && NEED=true
  [ "$NEED" = false ] && { echo "SKIP PR#$PR (reviewed)"; continue; }
  CI=$(gh pr checks $PR --repo hrygo/hotplex 2>&1 || true)
  [ -z "$CI" ] && { echo "SKIP PR#$PR (no CI)"; continue; }
  echo "$CI" | grep -qiE "pending|queued|in progress" && { echo "SKIP PR#$PR (CI run)"; continue; }
  echo "$CI" | grep -qiE "fail|neutral|timed.out" && { echo "SKIP PR#$PR (CI fail)"; continue; }
  echo "REVIEW PR#$PR"; TARGET=$PR
done
[ -z "$TARGET" ] && { echo "All reviewed."; exit; }; PR=$TARGET

## §1.5 检出 PR 分支(强制)
RB="/tmp/pr-review-$(date +%Y%m%d)" && mkdir -p "$RB"
PD="$RB/pr-$PR"
if [ -d "$PD/.git" ]; then cd "$PD" && git fetch origin "+pull/$PR/head:review" && git reset --hard review
else gh repo clone hrygo/hotplex "$PD" -- --no-checkout && cd "$PD" && git fetch origin "pull/$PR/head:review" && git checkout review
fi
find /tmp -maxdepth 1 -name "pr-review-*" ! -name "$(basename $RB)" -exec rm -rf {} + 2>/dev/null || true
echo "PR_DIR=$PD"

## §2 预读(主进程,共享给 agent)
DIFF=$(gh pr diff $PR --repo hrygo/hotplex)
CM=$(cat $PD/CLAUDE.md)
[ $(echo "$DIFF" | wc -l) -gt 8000 ] && DIFF=$(echo "$DIFF" | tail -8000)
SHARED_CTX="## PR#$PR Diff\n$DIFF\n\n## CLAUDE.md\n$CM\n\n## SRC_ROOT=$PD\n⚠️ 读源文件必须用 $PD/xxx,禁读 /home/hotplex/"

## §3 并行 Review(2 agent,SHARED_CTX完整嵌入每个agent)
A-正确性/安全/并发: nil、竞态、逻辑错误、错误处理、goroutine泄漏、锁序(mu→ms)、持锁IO、ctx未传播、WG不配对。Mutex嵌入/传指针、math/rand加密、shell执行、硬编码路径、Err+%w、slog。DRY、命名一致性。忽略linter能抓的。
B-历史/架构/性能/文档: blame上下文、API破坏下游、migration、配置兼容。SRP/OCP/DIP。N+1、不必要分配、大锁粒度(热路径)。注释不一致、过期TODO、注释掉代码。
返回: [SEVERITY] 简述 (file:line) — 原因
⚠️ 源文件用 $PD/ 绝对路径,禁 /home/hotplex/

## §4 过滤
<75丢弃。P0>P1>P2>P3。误报:预存问题、linter能抓、风格、缺测试(通用)、功能变更、跨包DRY需新抽象、非热路径性能。

## §5 提交
重跑§1确认。已review→跳过输出"SKIP:reviewed"。
P0/P1→REQUEST_CHANGES,仅P2/P3→COMMENT+APPROVE,全PASS→APPROVE。
body不可为空→跳过"SKIP:空body"。
格式: ## Code Review — hotplex-ai\nVerdict: ... | P0:X P1:X P2:X P3:X

## §6 约束
Go 1.26+ | tab | Mutex显式mu | Err哨兵+%w | slog | testify/require | filepath.Join
```

### 与系统特性的配合

| 系统特性 | Prompt 中的利用 |
|---------|----------------|
| `buildWebhookPrefix` | webhook 触发时注入 `⚠️ WEBHOOK 触发：仅审查 PR #N` + `TARGET_PR` 环境变量 |
| `Silent` 模式 | `silent: true` 跳过飞书投递，review 结果通过 GitHub API 直接提交 |
| `WorkDir` | executor 设置 `/tmp/hotplex`，prompt §1.5 切换到隔离检出目录 |
| `PlatformKey` | webhook 注入 `trigger`/`pr_number`，executor 注入 `cron_job_id` |
| `job.Timeout` | 1000s 超时，覆盖 clone + 2-agent 并行 + review 提交全流程 |
| `AllowedTools` | 可限制 Worker 仅使用 Bash 和 Read，防止意外修改 |

---

## 相关实践

- [Cron 自动化指南](../guides/developer/cron-automation.md) — 三种调度模式的配置与使用
- [定时任务教程](../tutorials/cron-scheduled-tasks.md) — 从零创建第一个定时任务
