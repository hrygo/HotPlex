# Thinking 显示优化实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 将 Slack answer 消息中的 thinking 内容提取为折叠块显示，提升用户体验

**Architecture:** 在 `builder_answer.go` 中增加 thinking 检测逻辑，提取thinking内容后分别构建 thinking 折叠块和最终回复块

**Tech Stack:** Go, Slack Block Kit, SDK

---

## Task 1: 添加 Thinking 提取函数到 formatting.go

**Files:**
- Modify: `chatapps/slack/formatting.go`

**Step 1: 添加测试用例**

```go
// 在 formatting_test.go 中添加
func TestMrkdwnFormatter_ExtractThinking(t *testing.T) {
    f := NewMrkdwnFormatter()

    // 有 thinking 的情况
    content := "<think>\n思考内容\n</think>\n\n最终回复"
    thinking, final, hasThinking := f.ExtractThinking(content)
    assert.True(t, hasThinking)
    assert.Equal(t, "思考内容", thinking)
    assert.Equal(t, "最终回复", final)

    // 无 thinking 的情况
    content2 := "普通回复"
    _, final2, hasThinking2 := f.ExtractThinking(content2)
    assert.False(t, hasThinking2)
    assert.Equal(t, "普通回复", final2)
}
```

**Step 2: 运行测试确认失败**

```bash
go test ./chatapps/slack/... -run TestMrkdwnFormatter_ExtractThinking -v
```
Expected: FAIL - undefined: ExtractThinking

**Step 3: 添加提取函数**

在 `formatting.go` 末尾添加:

```go
// ExtractThinking extracts thinking block from content
// Returns: thinking_content, final_content, has_thinking
func (f *MrkdwnFormatter) ExtractThinking(content string) (string, string, bool) {
    startTag := "<think>"
    endTag := "</think>"

    start := strings.Index(content, startTag)
    if start == -1 {
        return "", content, false
    }

    end := strings.Index(content[start+len(startTag):], endTag)
    if end == -1 {
        return "", content, false
    }
    end += start + len(startTag)

    thinking := content[start+len(startTag) : end-len(endTag)]
    thinking = strings.TrimSpace(thinking)

    // 移除 thinking 部分，保留最终回复
    final := content[:start] + content[end:]
    final = strings.TrimSpace(final)

    return thinking, final, true
}
```

**Step 4: 运行测试确认通过**

```bash
go test ./chatapps/slack/... -run TestMrkdwnFormatter_ExtractThinking -v
```
Expected: PASS

**Step 5: Commit**

```bash
git add chatapps/slack/formatting.go chatapps/slack/formatting_test.go
git commit -m "feat(slack): add ExtractThinking function"
```

---

## Task 2: 配置项添加

**Files:**
- Modify: `chatapps/slack/config.go:FeaturesConfig`
- Modify: `chatapps/config.go:PlatformConfig`

**Step 1: 添加配置结构**

在 `config.go` 的 `FeaturesConfig` 结构中添加:

```go
// ThinkingDisplayConfig controls thinking content display
type ThinkingDisplayConfig struct {
    Enabled       *bool `yaml:"enabled"`
    DefaultFolded *bool `yaml:"default_folded"`
}
```

在 `FeaturesConfig` 中添加字段:

```go
ThinkingDisplay *ThinkingDisplayConfig `yaml:"thinking_display"`
```

**Step 2: 更新默认值**

在 `config.go` 的 `defaultFeatures()` 中添加:

```go
defaultThinkingEnabled := true
defaultThinkingFolded := true
conf.ThinkingDisplay = &ThinkingDisplayConfig{
    Enabled:       &defaultThinkingEnabled,
    DefaultFolded: &defaultThinkingFolded,
}
```

**Step 3: 运行测试**

```bash
go build ./... && go test ./chatapps/slack/... -run TestConfig -v
```

**Step 4: Commit**

```bash
git add chatapps/slack/config.go chatapps/config.go
git commit -m "feat(config): add thinking display options"
```

---

## Task 3: 修改 Answer 构建器支持 thinking

**Files:**
- Modify: `chatapps/slack/builder_answer.go`

**Step 1: 添加测试用例**

在 `builder_subbuilders_test.go` 中添加:

```go
func TestAnswerMessageBuilder_WithThinking(t *testing.T) {
    builder := NewMessageBuilder(&Config{})

    msg := &base.ChatMessage{
        Type:    base.MessageTypeAnswer,
        Content: "<think>\n分析中...\n</think>\n最终回复",
    }

    blocks := builder.Build(msg)

    // 应该有 thinking section + divider + answer section
    assert.GreaterOrEqual(t, len(blocks), 2)

    // 检查第一个 block 是否包含 thinking 标记
    hasThinking := false
    for _, block := range blocks {
        if sb, ok := block.(*slack.SectionBlock); ok {
            if strings.Contains(sb.Text.Text, "Thinking") {
                hasThinking = true
            }
        }
    }
    assert.True(t, hasThinking)
}
```

**Step 2: 运行测试确认失败**

```bash
go test ./chatapps/slack/... -run TestAnswerMessageBuilder_WithThinking -v
```
Expected: FAIL - 现有逻辑不处理 thinking

**Step 3: 修改 BuildAnswerMessage**

修改 `BuildAnswerMessage` 函数:

```go
func (b *AnswerMessageBuilder) BuildAnswerMessage(msg *base.ChatMessage) []slack.Block {
    content := msg.Content
    if content == "" {
        return nil
    }

    var blocks []slack.Block

    // 1. 检查并提取 thinking
    thinking, finalContent, hasThinking := b.formatter.ExtractThinking(content)

    if hasThinking {
        // 添加 thinking section (使用 collapsible section)
        thinkingText := "💭 *Thinking...*"
        thinkingObj := slack.NewTextBlockObject("mrkdwn", thinkingText, false, false)
        thinkingSection := slack.NewSectionBlock(thinkingObj, nil, nil)
        // 设置折叠属性 (collapsible 需要使用 accessory 实现)
        thinkingSection.Collapsible = true
        blocks = append(blocks, thinkingSection)

        // 添加分隔符
        blocks = append(blocks, slack.NewDividerBlock())

        // 使用提取后的最终内容
        content = finalContent
    }

    // 2. 处理 Markdown
    formattedContent := content
    markdownEnabled := BoolValue(b.config.Features.Markdown.Enabled, true)
    if markdownEnabled {
        formattedContent = b.formatter.Format(content)
    }

    // 3. 处理 chunking
    chunkingEnabled := BoolValue(b.config.Features.Chunking.Enabled, true)
    maxChars := b.config.Features.Chunking.MaxChars
    if maxChars <= 0 {
        maxChars = 3500
    }

    if chunkingEnabled && len(formattedContent) > maxChars {
        chunks := b.chunkText(formattedContent, maxChars)
        for i, chunk := range chunks {
            if i > 0 {
                blocks = append(blocks, slack.NewDividerBlock())
            }
            mrkdwn := slack.NewTextBlockObject("mrkdwn", chunk, false, false)
            blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))
        }
    } else {
        mrkdwn := slack.NewTextBlockObject("mrkdwn", formattedContent, false, false)
        blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))
    }

    return blocks
}
```

**Step 4: 运行测试确认通过**

```bash
go test ./chatapps/slack/... -run TestAnswerMessageBuilder_WithThinking -v
```

**Step 5: Commit**

```bash
git add chatapps/slack/builder_answer.go chatapps/slack/builder_subbuilders_test.go
git commit -m "feat(slack): extract thinking to collapsible block in answer messages"
```

---

## Task 4: 端到端测试

**Step 1: 启动服务**

```bash
make run
```

**Step 2: 通过 Slack 发送复杂问题触发 thinking**

在 Slack 中发送需要 AI 思考的问题。

**Step 3: 检查日志**

```bash
grep -i thinking .logs/daemon.log | tail -20
```

**Step 4: 验证 UI**

- 观察 Slack 消息是否显示折叠的 "💭 Thinking..."
- 点击展开查看思考内容

---

## Task 5: 更新配置文档

**Files:**
- Modify: `docs/chatapps/chatapps-slack-manual-zh.md`

添加 thinking display 配置说明:

```markdown
### thinking_display

控制 thinking 内容的显示方式。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| enabled | bool | true | 是否启用 thinking 折叠显示 |
| default_folded | bool | true | 默认是否折叠 |

示例:
```yaml
features:
  thinking_display:
    enabled: true
    default_folded: true
```
```

---

## 总结

| Task | 文件 | 描述 |
|------|------|------|
| 1 | formatting.go | 添加 ExtractThinking 函数 |
| 2 | config.go | 添加 thinking_display 配置项 |
| 3 | builder_answer.go | 修改 BuildAnswerMessage 支持 thinking |
| 4 | E2E 测试 | 验证功能正常工作 |
| 5 | 文档 | 更新配置说明 |

---

**Plan complete and saved to `docs/plans/2026-03-12-thinking-display-design.md`**

Two execution options:

1. **Subagent-Driven (this session)** - I dispatch fresh subagent per task, review between tasks, fast iteration

2. **Parallel Session (separate)** - Open new session with executing-plans, batch execution with checkpoints

Which approach?
