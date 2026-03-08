# PRD: 自我诊断能力 (Self-Diagnostics)

| 字段 | 值 |
|------|-----|
| **Issue** | [#219](https://github.com/hrygo/hotplex/issues/219) |
| **状态** | Draft |
| **版本** | v1.0 |
| **日期** | 2026-03-08 |
| **作者** | AI Agent |

---

## 1. 执行摘要

为 HotPlex 引入自我诊断能力，当错误发生时自动派生诊断 Session 分析问题，通过固定通道返回诊断结果，支持用户确认后自动创建 GitHub Issue。

### 核心价值

| 维度 | 现状 | 目标 |
|------|------|------|
| 问题发现 | 用户报告 | 自动诊断 |
| 上下文收集 | 手动收集 | 自动收集 |
| Issue 质量 | 不一致 | 标准化模板 |
| 响应速度 | 小时/天级 | 分钟级 |

---

## 2. 用户故事

### 2.1 自动诊断 (Auto-Diagnosis)

```
AS A HotPlex 运维人员
I WANT 系统自动捕获并诊断运行时错误
SO THAT 问题能在用户报告前被发现和记录
```

**验收标准**:
- CLI 异常退出时自动触发诊断
- 超时事件触发诊断
- WAF 拦截触发诊断
- 诊断结果发送到固定通道
- 包含 [创建 Issue] [忽略] 操作按钮

### 2.2 手动诊断 (Slash Command)

```
AS A HotPlex 用户
I WANT 在固定通道使用 /diagnose 命令
SO THAT 主动触发对当前会话的诊断
```

**验收标准**:
- 仅在配置的固定通道中可用
- 直接创建 GitHub Issue（无需确认）
- 返回 Issue 链接

### 2.3 自动创建 Issue

```
AS A 开发者
I WANT 诊断结果超时后自动创建 GitHub Issue
SO THAT 不会遗漏任何潜在问题
```

**验收标准**:
- 5 分钟无操作自动创建
- 通道不可用时直接创建
- Issue 包含完整诊断信息

---

## 3. 功能需求

### 3.1 触发机制

| 触发类型 | 触发源 | 诊断类型 |
|---------|--------|---------|
| **错误钩子** | CLI 异常退出 | `auto` |
| **超时事件** | Session 超时 | `auto` |
| **WAF 拦截** | 安全违规 | `auto` |
| **Slash Command** | `/diagnose` | `command` |

### 3.2 上下文收集器

```
┌─────────────────────────────────────────┐
│           DiagContext                    │
├─────────────────────────────────────────┤
│ OriginalSessionID   string              │
│ Platform            string              │
│ UserID              string              │
│ ChannelID           string              │
│ ThreadID            string              │
│ Trigger             DiagTrigger         │
│ Error               *ErrorInfo          │
│ Conversation        *ConversationData   │
│ Logs                []byte              │
│ Environment         *EnvInfo            │
└─────────────────────────────────────────┘
```

#### 3.2.1 错误信息 (ErrorInfo)

```go
type ErrorInfo struct {
    Type       string // "exit", "timeout", "waf_violation"
    Message    string
    ExitCode   int
    StackTrace string
    Timestamp  time.Time
}
```

#### 3.2.2 会话记录 (ConversationData)

```go
type ConversationData struct {
    RawSize      int    // 原始大小 (bytes)
    Processed    string // 处理后内容
    IsSummarized bool   // 是否经过摘要
    MessageCount int    // 消息数量
}
```

**处理策略**:
- `size ≤ 20KB` → 直接附加原文
- `size > 20KB` → 调用 `Brain.Chat()` 摘要压缩至 ≤20KB

#### 3.2.3 环境信息 (EnvInfo)

```go
type EnvInfo struct {
    HotPlexVersion string
    GoVersion      string
    OS             string
    Arch           string
    CLIVersion     string // Claude Code / OpenCode 版本
    ConfigHash     string // 配置文件哈希（脱敏）
}
```

### 3.3 敏感信息脱敏

**必须脱敏的字段**:
- API Keys (正则: `(?i)(api[_-]?key|token|secret)[\s:=]+[\w-]+`)
- Tokens (正则: `xox[bap]-[\w-]+`)
- 密码字段
- 内网 IP（可选）

**脱敏实现** (`redact.go`):
```go
func Redact(input string) string {
    // 1. API Keys → [REDACTED_KEY]
    // 2. Tokens → [REDACTED_TOKEN]
    // 3. Passwords → [REDACTED_PWD]
}
```

### 3.4 诊断结果分发

#### 3.4.1 固定通道模式

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  触发事件    │ ──→ │  诊断引擎    │ ──→ │  固定通道    │
└──────────────┘     └──────────────┘     └──────────────┘
                                                 │
                           ┌─────────────────────┴─────────────────┐
                           │                                       │
                    ┌──────▼──────┐                        ┌──────▼──────┐
                    │ 用户确认    │                        │ 超时自动创建│
                    │ [创建][忽略]│                        │ (5min)      │
                    └──────┬──────┘                        └──────┬──────┘
                           │                                       │
                    ┌──────▼──────────────────────────────────────▼──────┐
                    │              GitHub Issue API                      │
                    └────────────────────────────────────────────────────┘
```

#### 3.4.2 通知优先级

1. 固定通道可用 → 发送带按钮消息
2. 用户点击 [创建 Issue] → 创建 Issue
3. 用户点击 [忽略] → 记录日志，不创建
4. 5 分钟无响应 → 自动创建 Issue
5. 通道不可用 → 直接创建 Issue

### 3.5 Issue 预览结构

```go
type IssuePreview struct {
    Title         string   // 自动生成标题
    Labels        []string // 自动分类标签
    Priority      string   // high/medium/low
    Summary       string   // 问题摘要
    Reproduction  string   // 复现步骤
    Expected      string   // 预期行为
    Actual        string   // 实际行为
    SuggestedFix  string   // 建议修复（可选）
}
```

---

## 4. 技术架构

### 4.1 模块依赖

```
internal/diag/
    ├── diagnostician.go   # 诊断核心逻辑
    ├── collector.go       # 上下文收集器
    ├── prompt.go          # LLM Prompt 模板
    ├── issue.go           # GitHub Issue 创建
    ├── notifier.go        # 通道通知
    ├── redact.go          # 敏感信息脱敏
    └── types.go           # 数据结构定义

internal/persistence/
    └── history.go         # [新增] 消息历史查询
```

### 4.2 依赖关系

```
internal/diag/
    │
    ├──→ brain/            # Chat 摘要, Analyze 结构化诊断
    │
    └──→ persistence/      # MessageHistoryStore (新增)
    └──→ plugins/storage/  # ChatAppMessageStore.ReadOnlyStore
```

### 4.3 接口设计

#### 4.3.1 Diagnostician 接口

```go
package diag

type Diagnostician interface {
    // Diagnose 执行诊断并返回预览
    Diagnose(ctx context.Context, trigger Trigger, sessionID string) (*DiagResult, error)

    // CreateIssue 从诊断结果创建 GitHub Issue
    CreateIssue(ctx context.Context, result *DiagResult) (string, error)
}

type Trigger interface {
    Type() DiagTrigger
    SessionID() string
    Error() *ErrorInfo
}
```

#### 4.3.2 MessageHistoryStore 接口 (新增)

```go
package persistence

// MessageHistoryStore 消息历史存储接口
type MessageHistoryStore interface {
    // GetRecentMessages 获取会话最近 N 条消息
    GetRecentMessages(ctx context.Context, sessionID string, limit int) ([]*storage.ChatAppMessage, error)

    // GetMessagesByTimeRange 获取时间范围内的消息
    GetMessagesByTimeRange(ctx context.Context, sessionID string, start, end time.Time) ([]*storage.ChatAppMessage, error)
}
```

### 4.4 配置项

```yaml
diagnostics:
  enabled: true
  notify_channel: "C12345678"        # 固定通知通道
  log_size_limit: 20KB               # 日志大小限制
  conversation_size_limit: 20KB      # 会话大小限制
  confirm_timeout: 5m                # 确认超时
  github:
    repo: "hrygo/hotplex"
    labels: ["bug", "auto-diagnosed"]
```

---

## 5. 实现路线

### Phase 0: 基础设施准备

**目标**: 实现消息历史查询能力

**交付物**:
- `internal/persistence/history.go`
- `MessageHistoryStore` 接口
- 基于 `plugins/storage` 的实现

**工作量**: 1-2 天

### Phase 1: 诊断核心

**目标**: 实现诊断核心逻辑

**交付物**:
- `internal/diag/types.go` - 数据结构
- `internal/diag/collector.go` - 上下文收集器
- `internal/diag/redact.go` - 脱敏工具
- `internal/diag/prompt.go` - LLM Prompt
- `internal/diag/diagnostician.go` - 核心逻辑

**工作量**: 2-3 天

### Phase 2: 自动诊断触发

**目标**: 实现错误钩子和通知

**交付物**:
- `internal/diag/notifier.go` - 通道通知
- `internal/diag/issue.go` - Issue 创建
- 错误钩子集成 (`internal/engine/`)
- Slack Block Kit 按钮交互

**工作量**: 2-3 天

### Phase 3: Slash Command

**目标**: 实现 `/diagnose` 命令

**交付物**:
- Slash Command 处理器
- 权限校验（仅固定通道）

**工作量**: 1 天

### Phase 4: 优化

**目标**: 性能和用户体验优化

**交付物**:
- 重复诊断检测（缓存）
- 诊断结果聚合（相同错误合并）
- 监控指标

**工作量**: 1-2 天

---

## 6. 测试策略

### 6.1 单元测试

| 模块 | 测试用例 |
|------|---------|
| `history.go` | 消息查询、时间范围过滤 |
| `collector.go` | 上下文收集完整性 |
| `redact.go` | API Key 脱敏、Token 脱敏 |
| `prompt.go` | Prompt 生成正确性 |
| `diagnostician.go` | 诊断流程正确性 |

### 6.2 集成测试

| 场景 | 测试步骤 |
|------|---------|
| 错误钩子触发 | 模拟 CLI 异常退出 → 验证诊断触发 |
| 通道通知 | 诊断完成 → 验证消息发送到固定通道 |
| 按钮交互 | 点击 [创建 Issue] → 验证 Issue 创建 |
| 超时自动创建 | 5 分钟无操作 → 验证自动创建 |

### 6.3 E2E 测试

| 场景 | 测试步骤 |
|------|---------|
| 自动诊断 | 制造错误 → 验证完整流程 |
| Slash Command | 在固定通道发送 `/diagnose` → 验证 Issue 创建 |
| 通道外禁用 | 在非固定通道发送 `/diagnose` → 验证拒绝 |

---

## 7. 非功能需求

### 7.1 性能

- 诊断完成时间: ≤ 30 秒
- 上下文收集: ≤ 5 秒
- Issue 创建: ≤ 10 秒

### 7.2 可靠性

- 诊断失败不应影响主流程
- Issue 创建失败应记录日志并重试
- 支持配置关闭诊断功能

### 7.3 安全性

- 敏感信息必须脱敏
- 仅授权通道可触发诊断
- GitHub Token 权限最小化

---

## 8. 风险与缓解

| 风险 | 概率 | 影响 | 缓解措施 |
|------|------|------|---------|
| LLM 摘要不准确 | 中 | 低 | 保留原始日志 |
| GitHub API 限流 | 低 | 中 | 实现重试 + 本地缓存 |
| 敏感信息泄露 | 低 | 高 | 多层脱敏 + 人工审核 |
| 诊断风暴 | 低 | 中 | 实现频率限制 |

---

## 9. 成功指标

| 指标 | 目标 |
|------|------|
| 自动诊断覆盖率 | ≥ 80% 的运行时错误 |
| Issue 创建成功率 | ≥ 95% |
| 平均诊断时间 | ≤ 30 秒 |
| 误报率 | ≤ 10% |

---

## 10. 附录

### A. 参考文档

- [brain 模块接口](../brain/brain.go)
- [storage 插件接口](../plugins/storage/interface.go)
- [GitHub Issue API](https://docs.github.com/en/rest/issues/issues)
- [Slack Block Kit](https://api.slack.com/block-kit)

### B. 相关 Issue

- #219 [RFC] 自我诊断能力

### C. 术语表

| 术语 | 定义 |
|------|------|
| DiagTrigger | 诊断触发类型: `auto` 或 `command` |
| Diagnostician | 诊断器，核心诊断逻辑组件 |
| 固定通道 | 配置的诊断结果通知通道 |
