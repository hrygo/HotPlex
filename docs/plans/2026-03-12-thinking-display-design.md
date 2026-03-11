# Design: Thinking 内容优化 - Slack 折叠块显示

## 背景

Claude Code CLI 在 `final_response` 消息中返回 thinking 内容，格式为：
```
<think>
思考内容...
</think>

当前这些标签被直接发送到 Slack，用户看到的是原始标签而非结构化的 thinking 显示。

## 目标

将 answer 消息中的 thinking 内容提取出来，用 Slack 折叠块显示，提升 UX。

## 设计

### 1. 解析逻辑 (`chatapps/slack/formatting.go`)

```go
// ExtractThinking 从内容中提取 thinking 部分
// 返回: thinking_content, final_content, has_thinking
func (f *MrkdwnFormatter) ExtractThinking(content string) (string, string, bool) {
    start := strings.Index(content, "<think>")
    if start == -1 {
        return "", content, false
    }
    end := strings.Index(content[start+len("<think>"):], "</think>")
    if end == -1 {
        return "", content, false
    }
    end += start + len("</think>")

    thinking := content[start+len("<think>") : end-len("</think>")]
    final := content[:start] + content[end:]

    return strings.TrimSpace(thinking), strings.TrimSpace(final), true
}
```

### 2. 折叠块构建 (`chatapps/slack/builder_thinking.go`)

使用 Slack `section` block + `collapsible` 或 `accessory` 按钮实现：

```go
// BuildThinkingBlock 构建折叠的 thinking 块
func (b *AnswerMessageBuilder) BuildThinkingBlock(thinking string) []slack.Block {
    header := slack.NewTextBlockObject("mrkdwn", "💭 *Thinking...*", false, false)
    section := slack.NewSectionBlock(header, nil, nil)
    section.Collapsible = true  // Slack 折叠属性
    return []slack.Block{section}
}
```

### 3. 修改 Answer 构建器 (`chatapps/slack/builder_answer.go`)

```go
func (b *AnswerMessageBuilder) BuildAnswerMessage(msg *base.ChatMessage) []slack.Block {
    content := msg.Content
    if content == "" {
        return nil
    }

    // 1. 提取 thinking
    thinking, finalContent, hasThinking := b.formatter.ExtractThinking(content)

    // 2. 构建 blocks
    var blocks []slack.Block

    if hasThinking {
        // 添加 thinking 折叠块
        blocks = append(blocks, b.BuildThinkingBlock(thinking)...)
        // 分隔符
        blocks = append(blocks, slack.NewDividerBlock())
    }

    // 3. 处理最终回复（markdown + chunking）
    // ... 现有逻辑 ...

    return blocks
}
```

### 4. 配置项 (`chatapps/slack/config.go`)

```go
type FeaturesConfig struct {
    // ... existing fields ...
    ThinkingDisplay *ThinkingDisplayConfig `yaml:"thinking_display"`
}

type ThinkingDisplayConfig struct {
    Enabled       *bool `yaml:"enabled"`
    DefaultFolded *bool `yaml:"default_folded"`
}
```

### 5. Slack UI 效果

默认（折叠）:
```
💭 Thinking... ▼
─────────────────
最终回复内容...
```

展开后:
```
💭 Thinking... ▲
─────────────────
思考内容...
─────────────────
最终回复内容...
```

## 风险与回退

- 如果解析失败，退回到现有逻辑（直接显示原始内容）
- 配置项默认开启，用户可关闭恢复原行为
