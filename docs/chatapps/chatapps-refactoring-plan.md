# chatapps 模块重构方案 (SDK First)

> 基于 DRY、SOLID 和整洁架构原则，**优先使用平台 SDK，不重复造轮子**

---

## 执行摘要

本方案基于 "SDK First" 原则重新审查。**重要发现**：
- `blocks.go` 和 `block_builder.go` 已经完全未被使用，可直接删除
- `builder.go` 已正确使用 SDK ✅
- 签名验证可使用 SDK 内置功能

| 维度 | 评分 | 主要问题 |
|------|------|----------|
| SDK 使用 | 8/10 | 签名验证可 SDK 化 |
| DRY | 8/10 | 发现的重复代码已废弃可删除 |
| SOLID | 6/10 | Adapter/StreamCallback 职责过多 |
| 整洁架构 | 6/10 | 跨层依赖问题 |

---

## 一、可直接删除的代码 🔴 [立即执行]

### 1.1 chatapps/slack/blocks.go

**状态**：完全未使用
```bash
# 验证：外部无任何导入
grep -r "blocks\.Build" chatapps/  # 无匹配
```

**操作**：删除此文件

### 1.2 chatapps/slack/block_builder.go

**状态**：完全未使用
```bash
# 验证：外部无任何导入
grep -r "block_builder\." chatapps/  # 无匹配
```

**操作**：删除此文件

---

## 二、已正确使用 SDK 的代码 ✅

### builder.go

`chatapps/slack/builder.go` 已正确使用 SDK：

```go
// ✅ 正确使用 SDK
func (b *MessageBuilder) BuildThinkingMessage(msg *base.ChatMessage) []slack.Block {
    text := slack.NewTextBlockObject(slack.MarkdownType, content, false, false)
    return []slack.Block{slack.NewSectionBlock(text, nil, nil)}
}

// ✅ 正确使用 SDK
func (b *MessageBuilder) BuildAnswerMessage(msg *base.ChatMessage) []slack.Block {
    blocks := []slack.Block{}
    blocks = append(blocks, slack.NewDividerBlock())
    blocks = append(blocks, slack.NewSectionBlock(mrkdwn, nil, nil))
    return blocks
}
```

---

## 三、待 SDK 化改造

### 3.1 签名验证 🟡

**现状**：`adapter.go` 手写 HMAC-SHA256 验证

```go
// ❌ 当前手写
func (a *Adapter) verifySignature(body []byte, timestamp, signature string) bool {
    // 手动实现 HMAC-SHA256 验证
}
```

**SDK 方案**：
```go
// ✅ 使用 SDK 内置验证
opts := []slack.Option{
    slack.OptionVerifySignature(signingSecret),
}
client := slack.New(botToken, opts...)
// SDK 自动验证所有请求
```

---

## 四、重构计划

### Phase 1: 删除废弃代码 [立即执行]

```bash
# 1. 删除 blocks.go
rm chatapps/slack/blocks.go

# 2. 删除 block_builder.go
rm chatapps/slack/block_builder.go

# 3. 验证编译
go build ./chatapps/slack/...
```

### Phase 2: SDK 化签名验证 [后续]

1. 在 `NewAdapter` 中添加签名验证选项
2. 移除手写的 `verifySignature` 方法
3. 测试 HTTP 模式下的签名验证

---

## 五、其他 DRY 问题

### 5.1 RateLimiter 重复

| 文件 | 描述 |
|------|------|
| `chatapps/ratelimit.go` | 通用限流 |
| `chatapps/slack/rate_limiter.go` | Slack 专用 |

**建议**：统一使用 `golang.org/x/time/rate`

### 5.2 整洁架构问题

| 问题 | 位置 | 建议 |
|------|------|------|
| 命令层→引擎层 | `command/*.go` | 定义 SessionManager 接口 |
| 集成层→具体平台 | `engine_handler.go:274` | 扩展 ChatAdapter 接口 |

---

## 六、结论

**好消息**：Slack 模块的大部分手写代码已经是废弃状态，可直接删除。

**需要做**：
1. 删除 `blocks.go` 和 `block_builder.go` (~2000 行废弃代码)
2. 将签名验证改为 SDK 内置
3. 统一 RateLimiter 实现

**不需要做**：
- `builder.go` 已经是最佳实践，无需修改
- 其他 Block 构建逻辑已正确使用 SDK
