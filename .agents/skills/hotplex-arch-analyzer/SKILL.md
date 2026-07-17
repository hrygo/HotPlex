---
name: hotplex-arch-analyzer
description: HotPlex 架构深度审计。覆盖架构分析、SOLID/DRY 合规、并发安全、性能优化、安全扫描、存量 issue 审计与清理。自动创建 GitHub Issue，支持 `/loop` 循环执行。**核心流程**：选定模块 → 静态分析 → 产出结构化发现 → 自动建 issue → 闭环修复。
---

# 架构深度分析器
## 工作流概览
每次调用 = **一个分析周期**在**一个模块**上覆盖**2-3 个方面**。专为 `/loop` 设计：`/loop 10m /hotplex-arch-analyzer`。深度分析在独立 subagent 中执行，主会话只处理 JSON 结果（上下文隔离）。状态持久化在 `.claude/arch-analysis/progress.json`，跨会话存活。

每次调用遵循两种模式之一，由**步骤 1.5（模式检测）**决定：

**分析模式**（默认）：
```
加载进度 → 选择模块 → 选择方面 → [Subagent 深度分析+分诊] → 去重检查 → 创建/更新 Issue → 更新进度 → 报告
```

**审计模式**（open issues ≥ 阈值时激活）：
```
加载进度 → 选择 Issue → [Subagent 验证代码] → 分类处理 → 更新进度 → 报告
```

**为什么需要双模式**：分析模式持续发现新问题，但问题积累后会降低团队信任度和审查效率。审计模式定期清理存量 issue，关闭误报、更正偏差，保持 issue backlog 的健康。两个模式交替运行，既不停止分析，也不让 issue 失控。

### 步骤 1：加载进度

读取 `.claude/arch-analysis/progress.json`。

**如果未找到** — 运行初始化：
1. 发现模块（见下面的模块发现）
2. 创建进度文件，所有模块的所有 aspects 待处理
3. 输出发现的模块列表
4. 继续到步骤 2

### 步骤 1.5：模式检测

每次调用开始时，检查是否应该进入审计模式：

```bash
# 获取当前 open architecture issues 数量
OPEN_COUNT=$(gh issue list --label "architecture" --state open --json number --limit 200 | python3 -c "import json,sys; print(len(json.load(sys.stdin)))")
echo "Open architecture issues: $OPEN_COUNT"
```

**决策规则**（按优先级）：

1. **用户显式指定模式** → 遵循用户指令（如 "审计 issue"、"review issues"、"audit mode"）
2. **Open issues ≥ 30** → 进入**审计模式**，审计 3-5 个最旧的 issue
3. **Open issues < 30** → 继续**分析模式**（步骤 2-7）

**审计与分析的交替**：当 open issues 在阈值附近波动时，审计和分析交替执行。每审计一轮后检查：如果 open issues 仍 ≥ 30 → 继续审计；否则切回分析模式。

**为什么阈值是 30**：经验表明，超过 30 个 open issues 时：
- 团队审查负担过重，新 issue 被忽略的概率激增
- 存量 issue 中积累了足够多的误报和过时发现值得清理
- 清理 backlog 比创建新 issue 产生更大的实际价值

### 步骤 2：选择目标模块

优先级（第一个匹配胜出）：
1. `analysis_count == 0` — 从未分析，最高优先级
2. 最低 `analysis_count` — 最少覆盖
3. 在平局中：优先核心模块（gateway > session > messaging > worker > config > others）
4. 仍然平局：按字母顺序

### 步骤 3：选择方面

每个模块在 4 个阶段中有 12 个方面。从 `aspects_pending` 中选择 2-3 个：
- 如果阶段 1 方面仍存在 → 从阶段 1 中选择
- 如果阶段 1 完成，阶段 2 仍存在 → 从阶段 2 中选择
- 以此类推
- 如果所有方面都覆盖 → 重新分析最旧的阶段以获取更深入的见解（增量深度）

### 步骤 4：深度分析（Subagent 隔离执行）

**为什么使用 Subagent**：直接在主会话中读取所有源文件会导致上下文快速膨胀 — 一个模块 ~7000 行源码消耗 ~20000 tokens。`/loop` 模式下分析 10+ 模块后主会话即因上下文耗尽被压缩。通过将文件读取和分析委托给 subagent，主会话只接收结构化的 JSON 结果（~500 tokens），将每模块上下文消耗降低 97%。

**上下文隔离原则**：
- **Subagent 内部**：读取源文件、分析清单、生成分诊结果 — 上下文膨胀在 subagent 内部消化
- **主会话**：只接收 JSON 结果 → 去重检查 → 创建 issue → 更新进度 → 报告
- **审计模式同理**：代码验证委托给 subagent，主会话只处理判定结果

#### Subagent 调用方式

使用 `Agent` 工具委托深度分析，subagent_type 使用 `general-purpose`：

```
Agent({
  description: "arch: <module_short> <aspects>",
  subagent_type: "general-purpose",
  prompt: <将下方模板中占位符替换为实际值>
})
```

#### Subagent Prompt 模板

将以下模板中的 `<PLACEHOLDER>` 替换为实际值后作为 prompt 传递。模板是自包含的 — subagent 无需主会话上下文。

```
你是 HotPlex 项目的 Go 架构分析代理。分析模块 `<MODULE_PATH>` 的以下方面：<ASPECTS_LIST>。

项目根目录：<PROJECT_ROOT>（当前工作目录）

## 分析步骤

1. 列出并读取 `<MODULE_PATH>/` 下所有 .go 源文件（含 _test.go）。用 Glob 列出文件，用 Read 读取内容。
2. 读取分析清单：`<PROJECT_ROOT>/.claude/skills/hotplex-arch-analyzer/references/analysis-checklist.md`，关注 <ASPECTS_LIST> 相关的部分。
3. 如果分析方面包含 performance(7) 或 scalability(8)，还需读取 `<PROJECT_ROOT>/.claude/skills/hotplex-arch-analyzer/references/performance-patterns.md`。
4. 对每个方面逐一分析，记录发现。

## 发现标准

每个发现必须包含四要素：
- **What**: 具体问题描述（1-2 句）
- **Where**: `file.go:line_range` 引用（2-3 个文件位置提供上下文）
- **Why**: 影响/风险量化（如"影响 N 个调用点"、"每个请求持有锁 ~200ms"）
- **How**: 重构方向 + 代码片段（5-15 行，显示 before/after 模式）

代码片段规则：
- 好的片段：5-15 行显示问题模式和提议的替代方案
- 不要粘贴整个函数，提取关键模式
- 不要写"重构使用接口"而不显示接口签名

严重性级别：Critical（数据丢失/安全/死锁）> High（可靠性/性能）> Medium（可维护性/DRY）> Low（风格/命名/次要）

## 分诊

分析完成后对每个发现进行分诊，**只保留通过两个过滤器的发现**：

**置信度过滤器**：
- High（多个代码引用确认，模式明确）→ 保留
- Medium（单实例但模式清晰，已知反模式）→ 保留
- Low（推测性，可能是有意设计，证据不足）→ **丢弃**

**ROI 过滤器**：
- High（小投入大影响，快速获胜）→ 保留
- Medium（中等投入明确影响，重构模式）→ 保留
- Low（大投入边际影响，过早抽象）→ **丢弃**（除非 Critical/High 严重性）

## 输出格式

在回复末尾，用以下格式返回结果（用 ```json 和 ``` 包裹）：

{
  "module": "<MODULE_PATH>",
  "aspects_analyzed": ["aspect1", "aspect2"],
  "findings": [
    {
      "name": "short-kebab-case-name",
      "severity": "Critical|High|Medium|Low",
      "confidence": "High|Medium",
      "roi": "High|Medium",
      "aspect": "aspect-name",
      "location": ["file.go:10-25", "file2.go:100-115"],
      "what": "具体问题描述",
      "why": "影响/风险量化",
      "current_pattern": "显示问题的 5-15 行代码片段",
      "proposed_fix": "显示解决方案的 5-15 行代码片段"
    }
  ],
  "dropped": [
    {"finding": "name-or-brief-description", "reason": "Low confidence: ... 或 Low ROI: ..."}
  ],
  "summary": "1-2 句模块健康评估"
}

重要：
- 只返回 High/Medium 置信度 AND High/Medium ROI 的发现
- 丢弃的发现记录在 dropped 数组中，附简要原因
- 如果没有可操作发现，返回空 findings 数组，summary 说明原因
- 不要编造发现。如果代码质量良好，明确说明
```

#### Subagent Prompt 参数替换规则

| 占位符 | 替换为 |
|--------|--------|
| `<MODULE_PATH>` | 目标模块路径，如 `internal/gateway` |
| `<ASPECTS_LIST>` | 方面名称列表，如 `SOLID, DRY` |
| `<PROJECT_ROOT>` | 当前工作目录绝对路径 |

#### 处理 Subagent 返回结果

Subagent 返回后：

1. **提取 JSON**：从返回文本末尾找到 ` ```json ` 包裹的块
2. **验证结构**：确认 findings 数组中每个发现包含四要素 + severity/confidence/roi
3. **传递给后续步骤**：
   - `findings` → 步骤 4.8（去重检查）
   - `dropped` → 步骤 6（进度更新）
   - `summary` → 步骤 7（报告）

如果 subagent 未能返回有效 JSON，从返回文本中手动提取关键发现并继续。

### 步骤 4.8：Issue 去重与合并检查

**为什么需要去重**：长期运行的分析器容易对同一模块重复创建 issue，尤其是：
- 不同分析方面（如 SOLID 和 DRY）发现同一代码模式的不同表现
- 重新分析时再次发现已被 issue 跟踪的问题
- 大问题被拆分为多个子问题，导致碎片化

去重检查确保：每个真实问题只有一个 issue，避免审查疲劳和重复工作。

#### 去重检查流程

**第一步：按模块搜索现有 issue**

```bash
# 搜索当前模块相关的所有 open architecture issues
gh issue list --label "architecture" --state open \
  --json number,title,body,createdAt \
  --limit 200 | python3 -c "
import json, sys
issues = json.load(sys.stdin)
module = '$MODULE'  # 当前模块，如 internal/gateway
module_short = module.split('/')[-1]  # 如 gateway
for i in issues:
    title = i['title'].lower()
    body = (i.get('body') or '').lower()
    if module in title or module in body or module_short in title:
        print(f\"{i['number']}: {i['title'][:80]}\")
"
```

**同时检查进度文件**：读取 `progress.json` 中当前模块的 `issues_created` 数组，获取该模块历史上创建的所有 issue 编号（含已关闭的），用于语义匹配。

**第二步：语义重叠检测**

对第一步返回的每个相关 issue，读取其 body，与本周期发现逐一比较：

| 比较维度 | 检查方法 |
|----------|----------|
| **文件重叠** | 本发现引用的文件是否在已有 issue 的 Key files 中出现 |
| **位置重叠** | 行号范围是否有交集（±50 行容差，考虑代码变动） |
| **方面重叠** | 是否覆盖同一分析方面（如都是 error-handling） |
| **问题模式** | 描述的问题模式是否本质相同（如"静默吞掉错误"vs"错误未传播"= 同一问题） |

**重叠判定规则**：
- 文件重叠 + 位置重叠 → **高概率重复**
- 文件重叠 + 方面重叠 + 相似问题模式 → **中概率重复**，需读取已有 issue body 确认
- 仅方面重叠 → **低概率重复**，通常可创建新 issue

**第三步：判定与处理**

对每个发现，给出以下判定之一：

| 判定 | 条件 | 处理方式 |
|------|------|----------|
| **new** | 无任何重叠，全新发现 | 进入步骤 5 正常创建 |
| **duplicate** | 与已有 issue 描述完全相同的问题 | 跳过创建，进度中记录 |
| **subsumed** | 本发现是已有 issue 的子集或特殊情况 | 更新已有 issue，添加此发现为补充 |
| **supersedes** | 本发现更全面，已有 issue 是其子集 | 扩展已有 issue 的标题和内容 |
| **related** | 相关但独立，共享部分代码但根本原因不同 | 创建新 issue，引用已有 issue |

**第四步：执行处理**

- **new** → 进入步骤 5 正常创建
- **duplicate** → 在进度 `findings_dropped` 中记录：`{"finding": "name", "reason": "Duplicate of issue N — <overlap reason>"}`
- **subsumed** → 更新已有 issue（添加补充发现）：
  ```bash
  gh issue comment <EXISTING_NUMBER> --body "$(cat <<'EOF'
  arch-analysis (cycle N, <aspect>): 补充发现。

  #### <finding-name>

  **Location**: \`file.go:line_range\`
  **问题**: <简述 — 2-3 句>

  **代码片段**:
  \`\`\`go
  <5-10 行代码>
  \`\`\`

  **补充说明**: <此发现如何与原 issue 关联>
  EOF
  )"
  ```
- **supersedes** → 更新已有 issue 标题和 Finding Summary 表，扩展范围
- **related** → 创建新 issue，在 Background 中引用：`Related: issue N`

#### 去重优先级

当发现可能与多个已有 issue 相关时，按以下优先级匹配：
1. 同一模块 + 同一文件 + 同一方面 → **最可能是重复**
2. 同一模块 + 同一文件 + 不同方面 → **可能是子集**（如 SOLID 和 DRY 发现同一代码段）
3. 同一模块 + 不同文件 + 同一方面 → **可能相关**
4. 不同模块 → **大概率独立**

### 步骤 5：创建 GitHub Issue

将此周期中经过步骤 4.8 判定为 **new** 或 **related** 的发现合并到**一个** GitHub issue 中。被判定为 duplicate/subsumed/supersedes 的发现已在步骤 4.8 中处理，不进入此步骤。

**为什么合并而不是拆分**：相关问题放在一个 issue 中提供上下文 — 审查者可以看到模式的完整图景，实施者可以在一次 PR 中解决相关问题，减少上下文切换。

**重要的格式规则**：
- **避免 `#` 数字**：GitHub 将 `#1` 解释为 issue 引用。使用描述性标题或基于 bullet 的编号
- **使用 `#### finding-name` 或 `- **Finding Name**:`** 作为子标题，永远不要用 `#### 1. Title`
- **写"cycle N"或"cycle number N"** 而不是"cycle #N"

**格式规则**：避免 `#` 数字（GitHub 会解析为 issue 引用），使用 `#### finding-name` 作为子标题，写"cycle N"而不是"cycle #N"（`#N` 会被 GitHub 解析为 issue 引用）。

**Issue 结构**：Background → Finding Summary → Findings（含 Severity/Confidence/ROI/Location/Current Pattern/Proposed Fix/AC）→ Implementation Priority → Out of Scope → Verification。完整模板和 AC 撰写指南见 `references/issue-template.md`。

**标题格式**：`<type>(<module>): <scope>` — 类型：`refactor` / `fix` / `perf` / `security` / `chore`

**标签映射**：Critical → `P1`，High → `P2`，Medium/Low → `P3`

如果没有值得 issue 的发现，跳过创建并在进度中注明。

### 步骤 6：更新进度

更新 `.claude/arch-analysis/progress.json`：
- 增加模块的 `analysis_count`
- 将分析的方面移至 `aspects_covered`
- 将 issue 号添加到 `issues_created`
- 将丢弃的发现添加到 `findings_dropped`（带原因）
- 更新 `last_analyzed` 时间戳
- 增加 `total_cycles`
- 添加到 `recent_activity` 日志（保留最后 20 个）

### 步骤 7：报告

输出包含进度矩阵的简洁摘要：

```
## Analysis Cycle N Complete

**Module**: `internal/<module>` (分析通过 M)
**Aspects**: <aspect1>, <aspect2>
**Findings**: X Critical, Y High, Z Medium, W Low
**Issue**: <issue URL 或"跳过 — 无可操作发现">

### Coverage Matrix

| Module | Ph1 | Ph2 | Ph3 | Ph4 | Issues | Status |
|--------|-----|-----|-----|-----|--------|--------|
| gateway | 3/3 | 2/3 | — | — | 2 | 进行中 |
| session | 3/3 | 3/3 | 2/2 | — | 3 | Ph3 完成 |
| messaging | 1/3 | — | — | — | 1 | Ph1 已开始 |
| ... | | | | | | |

### Stats
- 总周期：N
- 总 issues：M
- 完全覆盖的模块：A/总计
- **下一个目标**：`internal/<next-module>` (analysis_count=N, Phase P 待处理)
```

覆盖矩阵给出了每个模块在 4 个阶段中位置的快速可视化。使用 `n/3`（或 Ph3/4 的 `n/2`）格式进行中的阶段，`—` 表示未开始，✓ 表示完成。

---

## Issue 审计模式

当步骤 1.5 决定进入审计模式时，执行 5 步审计流程替代分析模式（步骤 2-7）。

**审计流程概览**：选择审计目标 → [Subagent 验证代码] → 判定处理（fixed/invalid/outdated/valid/duplicate）→ 更新进度 → 审计报告。

**每次审计 3-5 个 issue**，优先从未审计过的最旧 issue 开始，同模块 issue 放在一起审计。

完整审计步骤（含 bash 命令、判定表、处理操作、进度更新、报告格式）见 `references/audit-mode.md`。

### 审计模式 Subagent 委托

代码验证步骤委托给 subagent 执行，避免在主会话中读取大量源文件。

#### 审计 Subagent Prompt 模板

```
你是 HotPlex 项目的架构审计代理。验证以下 GitHub Issue 是否仍然有效。

项目根目录：<PROJECT_ROOT>（当前工作目录）

## 待审计 Issue

Issue 编号: <ISSUE_NUMBER>
标题: <ISSUE_TITLE>
内容摘要: <ISSUE_BODY_SUMMARY>

## 验证步骤

1. 读取 Issue 中引用的所有代码文件和行号
2. 检查每个发现点的当前代码状态：
   - 文件是否存在？
   - 引用的行号处的代码是否仍匹配 Issue 描述的问题模式？
   - 问题是否已被后续提交修复？
3. 如果 Issue 引用了多个发现，逐一验证每个发现

## 判定标准

- **valid**: 代码仍存在描述的问题，Issue 完全有效
- **partial**: 部分发现已修复，但核心问题仍存在
- **fixed**: 所有发现已被修复（指出修复的 commit 或 PR 如果能从代码看出）
- **invalid**: 问题从未存在或 Issue 描述不准确
- **outdated**: 代码已重构，相关文件/函数已不存在或完全改写
- **duplicate**: 与另一个 Issue 描述相同问题

## 输出格式

在回复末尾，用以下格式返回结果（用 ```json 和 ``` 包裹）：

{
  "issue_number": <ISSUE_NUMBER>,
  "verdict": "valid|partial|fixed|invalid|outdated|duplicate",
  "findings_status": [
    {
      "finding_name": "name",
      "status": "valid|fixed|invalid|outdated",
      "file": "path/to/file.go",
      "lines": "10-25",
      "evidence": "当前代码中观察到的证据，1-2 句"
    }
  ],
  "recommendation": "建议操作：close / keep-open / update-body / add-comment",
  "comment_body": "如果建议 add-comment 或 update-body，提供建议内容（可选）"
}
```

---

## 模块发现

**主要来源**：`AGENTS.md` STRUCTURE 部分 — 如果存在，使用记录的模块边界。

**后备**：扫描 `internal/`、`pkg/`、`cmd/` 目录。每个带有 `.go` 文件的子目录 = 一个模块。

### 模块分组

分组紧密耦合的子包：
- `messaging/slack/` + `messaging/feishu/` + `messaging/yuanxin/` + `messaging/stt/` + `messaging/tts/` + `messaging/toolfmt/` → 作为子模块在 `messaging` 下分析
- `worker/claudecode/` + `worker/codexcli/` + `worker/opencodeserver/` + `worker/acp/` + `worker/base/` + `worker/proc/` → 在 `worker` 下的子模块
- `cli/checkers/` + `cli/onboard/` + `cli/cron/` + `cli/slack/` → 在 `cli` 下的子模块

父模块（`messaging`、`worker`、`cli`）获得自己的分析通过，涵盖共享代码（bridge、接口、基本类型）。

---

## 分析方面

12 个方面跨 4 个阶段。每个阶段更深入 — 阶段 1 是结构性的，阶段 4 是细粒度的。

### 阶段 1：架构与设计
| # | 方面 | 重点 |
|---|--------|-------|
| 1 | **SOLID** | SRP 违规、接口隔离、依赖倒置、开/闭 |
| 2 | **DRY** | 模块内的重复逻辑、值得提取的跨模块模式 |
| 3 | **耦合** | 导入图、循环依赖、稳定依赖原则 |

### 阶段 2：可靠性
| # | 方面 | 重点 |
|---|--------|-------|
| 4 | **错误处理** | 静默失败、错误吞咽、哨兵错误、包装一致性 |
| 5 | **并发** | 竞争条件、互斥锁排序、goroutine 生命周期、通道泄漏 |
| 6 | **资源管理** | 连接/文件/goroutine 泄漏、defer 清理、关闭路径 |

### 阶段 3：性能与规模
| # | 方面 | 重点 | 参考 |
|---|--------|-------|------|
| 7 | **性能** | 热路径、不必要的分配、N+1 模式、缓冲区重用 | `references/performance-patterns.md` |
| 8 | **可扩展性** | 在 10x/100x 负载下的单点故障、瓶颈 | `references/performance-patterns.md` |

### 阶段 4：安全与质量
| # | 方面 | 重点 |
|---|--------|-------|
| 9 | **安全** | 输入验证缺口、注入风险、认证/授权绕过 |
| 10 | **可观测性** | 结构化日志缺口、指标覆盖、跟踪跨度 |
| 11 | **可测试性** | DI 覆盖、可模拟性、错误路径的测试缺口 |
| 12 | **代码质量** | 圈复杂度、上帝对象、死代码、命名一致性 |

---

## 参考文件索引

以下详细内容已提取到 `references/` 目录，按需读取：

| 文件 | 内容 | 何时读取 |
|------|------|----------|
| `references/issue-template.md` | 完整 issue bash 模板 + 标题/标签格式 + AC 撰写指南 | 创建 issue 前（步骤 5） |
| `references/audit-mode.md` | 审计模式完整流程（5 步 + bash 命令 + 判定表） | 进入审计模式时（步骤 1.5） |
| `references/analysis-checklist.md` | 12 个方面的详细分析清单（含 HotPlex 性能专项） | 深度分析时（步骤 4） |
| `references/performance-patterns.md` | HotPlex 热路径性能模式 + 通用 Go 反模式 + 检测方法论 + 验证手段 | 分析 aspect 7-8（Performance/Scalability）时 |
| `references/progress-and-metrics.md` | 进度文件 schema + 边缘情况 + 成功指标 + 持续改进 | 需要查阅 schema 或调优时 |
| `references/troubleshooting.md` | 9 个常见陷阱及解决方案 | 遇到问题或结果不理想时 |

**进度文件关键字段**：`findings_dropped` 跟踪分诊丢弃和去重判为 duplicate 的发现。`audited_issues` 跟踪已审计 issue 避免重复审计。完整 schema 见 `references/progress-and-metrics.md`。

**健康指标速查**：Issue 创建率 60-80%，有效率 ≥70%，审计回收率 10-30%，去重拦截率 10-25%。详细指标和调整建议见 `references/progress-and-metrics.md`。
