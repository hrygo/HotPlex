# Slack Block Kit 升级 Spec

**Epic Issue**: #(待创建)
**分支**: `feat/slack-block-kit-upgrade`
**前置**: PR #562 (deps upgrade, slack-go v0.24.0 已合并)
**状态**: Draft

---

## 背景

slack-go v0.22.0 → v0.24.0 引入 4 种新 Block Kit 类型 + Assistant API 增强 + SocketMode dispatcher 暴露。当前 Slack 适配器存在以下痛点：

1. **TableBlock beta 兼容性**：3 处使用 `TableBlock`（beta API），每处都带 `invalid_blocks` fallback（双倍 API 调用）
2. **错误提示不醒目**：用 `ContextBlock` + emoji hack 显示错误/警告，视觉层级不够
3. **AI 结果无结构化**：skills 列表、多结果输出用多个 `SectionBlock` 拼接，超出 50-block 限制需分页
4. **Assistant 状态无品牌标识**：`SetAssistantThreadsStatusContext` 缺少 username/icon

---

## Phase 1: AlertBlock + Assistant 品牌化

### 1.1 AlertBlock 替换错误/状态提示

**目标**：将安全错误、Worker 超时、MCP 失败等提示从 SectionBlock/ContextBlock 迁移到 AlertBlock。

**改动文件**：
- `internal/messaging/slack/interaction.go` — 权限请求/Q&A/MCP 请求头部
- `internal/messaging/slack/adapter.go` — MCP 状态 fallback
- `internal/messaging/slack/validator.go` — 新增 `case *slack.AlertBlock`

**新增辅助函数**：
```go
// alertBlock 构建 AlertBlock，level 默认 info
func alertBlock(text string, level ...slack.AlertBlockLevel) *slack.AlertBlock {
    lvl := slack.AlertLevelInfo
    if len(level) > 0 {
        lvl = level[0]
    }
    return slack.NewAlertBlock(
        slack.AlertBlockOptionLevel(lvl),
    ).WithText(slack.NewTextBlockObject(slack.MarkdownType, text, false, false))
}
```

**具体映射**：

| 调用点 | 当前 | 改为 |
|--------|------|------|
| `interaction.go:188` 权限请求 | SectionBlock | `AlertBlock{level: warning}` |
| `interaction.go:290` Q&A | SectionBlock | `AlertBlock{level: info}` |
| `interaction.go:386` MCP Server 请求 | SectionBlock | `AlertBlock{level: info}` |
| `adapter.go:1288` context usage fallback | ContextBlock | `AlertBlock{level: info}` |
| `adapter.go:1338` MCP status fallback | plain text | `AlertBlock{level: warning}` |
| `conn_events.go:79` Worker 错误 | plain text prefix | `AlertBlock{level: error}` |

**Fallback 策略**：AlertBlock 不被支持时走 `invalid_blocks` → 现有 SectionBlock 路径（复用已有 fallback 机制）。

**测试**：
- `validator_test.go` — 新增 AlertBlock 校验测试
- `interaction_test.go` — 验证 block 构建输出包含 AlertBlock

### 1.2 Assistant Status 品牌

**改动文件**：`internal/messaging/slack/status.go`

**改动**：
```go
// status.go:305-311
params := slack.AssistantThreadsSetStatusParameters{
    ChannelID: channelID,
    ThreadTS:  threadTS,
    Status:    status,
    Username:  a.botName(),    // 从 AdapterConfig 获取
    IconEmoji: a.botIcon(),    // 从 AdapterConfig 获取
}
```

**配置扩展**：`AdapterConfig` 新增 `BotDisplayName` 和 `BotIconEmoji` 字段，通过 `extras` 注入。

**测试**：`status_test.go` 验证 params 包含 username/icon。

---

## Phase 2: DataTable 替换 TableBlock

### 2.1 目标

用 GA 级 `DataTableBlock` 替换 beta `TableBlock`，消除 3 处 `invalid_blocks` fallback。

### 2.2 改动文件

- `internal/messaging/slack/adapter.go` — 3 处 TableBlock → DataTableBlock
- `internal/messaging/slack/validator.go` — 新增 DataTableBlock 校验

### 2.3 具体改动

**turn summary** (`adapter.go:1189-1204`)：
```go
func (c *SlackConn) buildTurnSummaryTable(d messaging.TurnSummaryData) []slack.Block {
    table := slack.NewDataTableBlock("Turn Summary", slack.DataTableBlockOptionBlockID("turn_summary"))
    for _, f := range d.Fields() {
        val := f.Value
        if f.Label == "🔧 Tools" {
            val = formatToolNamesSlack(d.ToolNames, d.ToolCallCount)
        }
        table.AddRow(dataTableCell(f.Label), dataTableCell(val))
    }
    return []slack.Block{table}
}
```

**Fallback 保留**：DataTableBlock 为 GA API，但部分 workspace 可能不支持。
- `sendTurnSummary` / `sendContextUsage` / `sendMCPStatus` 均保留 `isInvalidBlocksError` fallback
- Fallback 使用 plain text 或 ContextBlock 替代

### 2.4 兼容性

DataTable 为 2026 GA API。保留渐进策略：
1. 先尝试 DataTable
2. `invalid_blocks` 错误 → fallback 到 AlertBlock（Phase 1 已实现）
3. 工作区兼容后移除 fallback

---

## Phase 3: CardBlock + CarouselBlock

### 3.1 目标

Skills 列表和多结果输出用 CardBlock/CarouselBlock 结构化展示。

### 3.2 改动文件

- `internal/messaging/slack/skills_list.go` — CardBlock 替代 SectionBlock
- `internal/messaging/slack/image_blocks.go` — CardBlock hero_image 模式
- `internal/messaging/slack/validator.go` — 新增 CardBlock/CarouselBlock 校验

### 3.3 Skills 列表改造

每个 skill group → 一张 CardBlock，所有 cards 包在 CarouselBlock 中。
解决 50-block 限制：CarouselBlock 只占 1 个 block 位。

---

## Phase 4: SocketMode Dispatcher（可选）

### 4.1 目标

暴露 `socketmode.handler.dispatcher`，注入中间件层（metrics、rate limit、panic recovery）。

### 4.2 改动文件

- `internal/messaging/slack/adapter.go` — 事件路由重构

### 4.3 评估

当前事件处理模式够用，此 Phase 仅在有明确性能瓶颈或可观测性需求时实施。

---

## 验收标准

- [ ] Phase 1: AlertBlock 在所有错误/状态提示场景替换完成
- [ ] Phase 1: Assistant status 显示 bot username 和 icon
- [ ] Phase 1: `make check` 全量通过
- [ ] Phase 2: DataTable 替换所有 TableBlock
- [ ] Phase 2: 3 处 `invalid_blocks` fallback 删除
- [ ] Phase 2: `make check` 全量通过
- [ ] Phase 3: Skills 列表用 CarouselBlock 展示
- [ ] Phase 3: 单条消息内无 50-block 限制溢出
- [ ] Phase 3: `make check` 全量通过

## 风险

| 风险 | 缓解 |
|------|------|
| AlertBlock/DataTable 不被部分 workspace 支持 | 保留 fallback 路径 |
| CarouselBlock 移动端渲染差异 | Block Kit Builder 测试 + fallback |
| slack-go v0.24.0 新 API 有 bug | 关注 upstream issues |
