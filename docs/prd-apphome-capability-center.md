# PRD: Slack App Home 智能能力中心

**Issue**: [#215](https://github.com/hrygo/hotplex/issues/215)
**Version**: 1.0.0
**Status**: Draft
**Created**: 2026-03-08

---

## 1. 背景与问题陈述

### 1.1 现状痛点

| 痛点 | 描述 |
|------|------|
| **重复输入** | 用户每次需手动输入完整 prompt，效率低下 |
| **静态推荐局限** | 预设提示词无法参数化，灵活性差 |
| **动态推荐偏差** | 基于上下文推断可能偏离用户真实意图 |
| **最佳实践分散** | 新人难以快速掌握高效使用方式 |

### 1.2 目标用户

- **开发者**: 日常代码审查、调试、重构
- **团队新人**: 需要标准化最佳实践引导
- **高频用户**: 重复执行相似任务

### 1.3 成功指标

| 指标 | 目标值 | 衡量方式 |
|------|--------|----------|
| 能力使用率 | >30% 请求通过能力中心触发 | 日志统计 |
| 完成率 | >90% 提交的能力任务完成 | Modal → 最终响应 |
| 用户满意度 | NPS > 7 | 可选反馈收集 |

---

## 2. 解决方案概览

### 2.1 核心概念

**能力 (Capability)**: 预定义的可参数化任务模板，用户通过 App Home 一键触发。

```
┌──────────────────────────────────────────────────────────────┐
│                      App Home Tab                             │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │
│  │ 🔍 代码审查  │  │ 📝 代码解释  │  │ 🐛 错误诊断  │           │
│  └─────────────┘  └─────────────┘  └─────────────┘           │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │
│  │ 📦 Commit   │  │ 🔀 PR 审查   │  │ ♻️ 重构建议  │           │
│  └─────────────┘  └─────────────┘  └─────────────┘           │
└──────────────────────────────────────────────────────────────┘
                          │
                          ▼ 点击能力卡片
┌──────────────────────────────────────────────────────────────┐
│                     Parameter Modal                            │
│  ┌──────────────────────────────────────────────────────────┐│
│  │ 文件路径: [________________________] (必填)               ││
│  │ 审查重点: [安全 ▼] [性能 ▼] [风格 ▼] (多选)              ││
│  │ 额外说明: [________________________] (可选)               ││
│  │                                          [取消] [执行]    ││
│  └──────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────┘
```

### 2.2 与现有系统集成

```
                    ┌─────────────────┐
                    │   App Home      │
                    │  能力中心 UI    │
                    └────────┬────────┘
                             │ 用户选择能力
                             ▼
                    ┌─────────────────┐
                    │  Native Brain   │◀─────────────────────────┐
                    │  - 意图确认      │                          │
                    │  - 智能路由      │                          │
                    │  - 上下文压缩    │                          │
                    └────────┬────────┘                          │
                             │ 渲染 Prompt                       │
                             ▼                                   │
                    ┌─────────────────┐                          │
                    │   DM Channel    │                          │
                    │   Engine 执行   │                          │
                    └────────┬────────┘                          │
                             │ 结果响应                          │
                             └──────────────────────────────────┘
```

---

## 3. 功能需求

### 3.1 能力定义系统

#### 3.1.1 Capability 数据结构

```go
// chatapps/slack/apphome/capability.go

type Capability struct {
    ID             string         `yaml:"id" json:"id"`
    Name           string         `yaml:"name" json:"name"`
    Icon           string         `yaml:"icon" json:"icon"`
    Description    string         `yaml:"description" json:"description"`
    Category       string         `yaml:"category" json:"category"`
    Parameters     []Parameter    `yaml:"parameters" json:"parameters"`
    PromptTemplate string         `yaml:"prompt_template" json:"prompt_template"`
    BrainOpts      BrainOptions   `yaml:"brain_opts" json:"brain_opts"`
    Enabled        bool           `yaml:"enabled" json:"enabled"`
}

type Parameter struct {
    ID          string   `yaml:"id" json:"id"`
    Label       string   `yaml:"label" json:"label"`
    Type        string   `yaml:"type" json:"type"` // text, select, multiline
    Required    bool     `yaml:"required" json:"required"`
    Default     string   `yaml:"default" json:"default"`
    Options     []string `yaml:"options" json:"options"`
    Placeholder string   `yaml:"placeholder" json:"placeholder"`
}

type BrainOptions struct {
    IntentConfirm   bool   `yaml:"intent_confirm"`   // 是否需要意图确认
    CompressContext bool   `yaml:"compress_context"` // 是否压缩历史上下文
    PreferredModel  string `yaml:"preferred_model"`  // 偏好模型
}
```

#### 3.1.2 YAML 配置格式

```yaml
# capabilities.yaml
capabilities:
  - id: code_review
    name: 代码审查
    icon: ":mag:"
    description: 对指定文件进行安全/性能/风格审查
    category: code
    enabled: true
    parameters:
      - id: file_path
        label: 文件路径
        type: text
        required: true
        placeholder: 例如: src/main.go
      - id: focus
        label: 审查重点
        type: select
        required: false
        options:
          - security
          - performance
          - style
          - maintainability
    prompt_template: |
      请对以下文件进行代码审查:
      文件: {{.file_path}}
      重点关注: {{.focus}}

      请从以下角度进行分析:
      1. 安全性
      2. 性能
      3. 代码风格
      4. 可维护性
    brain_opts:
      intent_confirm: false
      compress_context: true
      preferred_model: claude-sonnet-4-6
```

### 3.2 App Home 页面构建

#### 3.2.1 Home Tab 结构

```
┌────────────────────────────────────────────────────────────┐
│ 🔥 HotPlex 能力中心                                         │
│                                                            │
│ 💻 代码                                                    │
│ ┌──────────────┐ ┌──────────────┐ ┌──────────────┐        │
│ │🔍 代码审查    │ │📝 代码解释    │ │♻️ 重构建议    │        │
│ │安全/性能审查  │ │解释工作原理   │ │优化建议      │        │
│ └──────────────┘ └──────────────┘ └──────────────┘        │
│                                                            │
│ 🐛 调试                                                    │
│ ┌──────────────┐                                          │
│ │🐛 错误诊断    │                                          │
│ │分析错误信息   │                                          │
│ └──────────────┘                                          │
│                                                            │
│ 🔀 Git                                                    │
│ ┌──────────────┐ ┌──────────────┐                         │
│ │📦 Commit生成  │ │🔀 PR审查     │                         │
│ │Conventional   │ │审查变更建议   │                         │
│ └──────────────┘ └──────────────┘                         │
└────────────────────────────────────────────────────────────┘
```

#### 3.2.2 事件处理

需要处理的新事件类型:

| 事件类型 | 触发条件 | 处理动作 |
|----------|----------|----------|
| `app_home_opened` | 用户点击 App Home Tab | 构建 Home 页面 |
| `block_actions` | 点击能力卡片 | 打开参数 Modal |
| `view_submission` | Modal 表单提交 | 执行能力任务 |

### 3.3 参数 Modal 构建

#### 3.3.1 动态表单生成

根据 Capability 定义动态构建 Modal:

```go
// chatapps/slack/apphome/form.go

func BuildCapabilityModal(cap Capability) *slack.ModalViewRequest {
    blocks := []slack.Block{}

    for _, param := range cap.Parameters {
        switch param.Type {
        case "text":
            blocks = append(blocks, buildTextInput(param))
        case "select":
            blocks = append(blocks, buildSelectInput(param))
        case "multiline":
            blocks = append(blocks, buildTextarea(param))
        }
    }

    return &slack.ModalViewRequest{
        Type:       slack.VTModal,
        Title:      slack.NewTextBlockObject("plain_text", cap.Name, false, false),
        Submit:     slack.NewTextBlockObject("plain_text", "执行", false, false),
        Close:      slack.NewTextBlockObject("plain_text", "取消", false, false),
        Blocks:     slack.Blocks{BlockSet: blocks},
        PrivateMetadata: cap.ID, // 用于回调时识别能力
    }
}
```

### 3.4 与 Native Brain 集成

#### 3.4.1 智能路由

```go
// chatapps/slack/apphome/brain_integration.go

type BrainIntegration struct {
    brain brain.Brain
    router *brain.IntentRouter
}

func (bi *BrainIntegration) PreparePrompt(
    ctx context.Context,
    cap Capability,
    params map[string]string,
) (string, error) {

    // 1. 渲染 Prompt 模板
    prompt := renderTemplate(cap.PromptTemplate, params)

    // 2. 可选: 意图确认
    if cap.BrainOpts.IntentConfirm {
        confirmed, err := bi.confirmIntent(ctx, prompt)
        if err != nil {
            return "", err
        }
        if !confirmed {
            return "", ErrIntentNotConfirmed
        }
    }

    // 3. 可选: 上下文压缩
    if cap.BrainOpts.CompressContext {
        compressed, err := bi.compressContext(ctx, prompt)
        if err != nil {
            // 非致命错误，继续使用原始 prompt
            log.Warn("Context compression failed", "error", err)
        } else {
            prompt = compressed
        }
    }

    return prompt, nil
}
```

### 3.5 能力执行流程

```go
// chatapps/slack/apphome/handler.go

func (h *Handler) HandleCapabilitySubmit(
    ctx context.Context,
    callback *slack.InteractionCallback,
) error {

    // 1. 解析提交数据
    capID := callback.View.PrivateMetadata
    params := extractParams(callback.View.State.Values)

    // 2. 加载能力定义
    cap, err := h.registry.Get(capID)
    if err != nil {
        return err
    }

    // 3. 参数校验
    if err := validateParams(cap, params); err != nil {
        return h.showValidationErrors(callback, err)
    }

    // 4. Brain 预处理
    prompt, err := h.brain.PreparePrompt(ctx, cap, params)
    if err != nil {
        return err
    }

    // 5. 获取或创建 DM 频道
    dmChannel, err := h.getOrCreateDMChannel(ctx, callback.User.ID)
    if err != nil {
        return err
    }

    // 6. 发送渲染后的 Prompt 到 DM
    if err := h.sendToDM(ctx, dmChannel, prompt); err != nil {
        return err
    }

    // 7. 触发 Engine 执行
    return h.triggerEngine(ctx, callback.User.ID, dmChannel, prompt)
}
```

---

## 4. 技术架构

### 4.1 文件结构

```
chatapps/slack/apphome/
├── builder.go           # Home Tab 页面构建
├── capability.go        # Capability 数据结构定义
├── registry.go          # 能力注册中心 (加载/缓存 YAML)
├── handler.go           # 事件处理入口
├── form.go              # Modal 表单动态构建
├── brain_integration.go # Native Brain 集成
├── executor.go          # 能力执行器
└── capabilities.yaml    # 预定义能力配置
```

### 4.2 与现有模块集成

```
chatapps/slack/
├── adapter.go          # 新增 AppHome Handler 注册
├── events.go           # 扩展处理 app_home_opened
├── interactive.go      # 扩展处理 view_submission
└── apphome/            # 新增包
    └── ...
```

### 4.3 依赖关系

```
apphome/
    ├── brain/          # Native Brain 智能路由
    ├── engine/         # Engine 触发执行
    └── slack-go/slack  # Slack SDK
```

---

## 5. API 设计

### 5.1 公开接口

```go
// Registry - 能力注册中心
type Registry interface {
    // GetAll 返回所有已启用的能力
    GetAll() []Capability

    // GetByID 根据 ID 获取能力
    GetByID(id string) (Capability, error)

    // Reload 重新加载配置文件
    Reload() error

    // GetByCategory 按分类获取能力
    GetByCategory(category string) []Capability
}

// Handler - App Home 事件处理器
type Handler interface {
    // HandleHomeOpened 处理 app_home_opened 事件
    HandleHomeOpened(ctx context.Context, event *slack.AppHomeOpenedEvent) error

    // HandleCapabilityClick 处理能力卡片点击
    HandleCapabilityClick(ctx context.Context, callback *slack.InteractionCallback) error

    // HandleCapabilitySubmit 处理 Modal 表单提交
    HandleCapabilitySubmit(ctx context.Context, callback *slack.InteractionCallback) error
}
```

---

## 6. 预定义能力清单

### 6.1 MVP 能力 (Phase 1)

| ID | 名称 | 分类 | 描述 | 优先级 |
|----|------|------|------|--------|
| `code_review` | 代码审查 | code | 对文件进行安全/性能/风格审查 | P0 |
| `explain_code` | 代码解释 | code | 解释代码片段工作原理 | P0 |
| `debug_error` | 错误诊断 | debug | 分析错误信息并给出修复方案 | P0 |
| `git_commit` | Commit 生成 | git | 生成 Conventional Commit 消息 | P1 |
| `pr_review` | PR 审查 | code | 审查 PR 变更并给出建议 | P1 |
| `refactor` | 重构建议 | code | 分析代码并提供重构建议 | P1 |

### 6.2 扩展能力 (Phase 2+)

| ID | 名称 | 分类 | 描述 |
|----|------|------|------|
| `test_gen` | 测试生成 | code | 为代码生成单元测试 |
| `doc_gen` | 文档生成 | docs | 生成代码文档 |
| `api_design` | API 设计 | design | 设计 RESTful API 规范 |
| `sql_opt` | SQL 优化 | db | 分析并优化 SQL 查询 |

---

## 7. 配置与部署

### 7.1 环境变量

```bash
# 能力中心开关
HOTPLEX_APPHOME_ENABLED=true

# 配置文件路径 (支持热更新)
HOTPLEX_CAPABILITIES_PATH=/etc/hotplex/capabilities.yaml
```

### 7.2 Slack App 权限

需要添加以下 OAuth Scope:

- `home` - App Home Tab 访问
- `im:write` - 发送 DM 消息
- `im:history` - 读取 DM 历史
- `views:write` - Modal 创建和更新

### 7.3 热更新

支持不重启服务更新能力定义:

```bash
# 方式 1: 修改 YAML 后触发重载
kill -HUP <pid>

# 方式 2: API 触发 (可选)
curl -X POST http://localhost:8080/api/v1/apphome/reload
```

---

## 8. 测试策略

### 8.1 单元测试

| 模块 | 测试重点 |
|------|----------|
| `registry.go` | YAML 加载、缓存、热更新 |
| `form.go` | 动态 Modal 构建、参数提取 |
| `executor.go` | Prompt 渲染、Engine 触发 |

### 8.2 集成测试

- 端到端流程: 点击卡片 → 填写表单 → 收到响应
- Brain 集成: 意图确认、上下文压缩

### 8.3 手动测试清单

- [ ] 打开 App Home 显示能力网格
- [ ] 点击卡片弹出正确参数 Modal
- [ ] 必填参数校验
- [ ] 提交后收到 DM 响应
- [ ] YAML 热更新生效

---

## 9. 风险与缓解

| 风险 | 影响 | 缓解措施 |
|------|------|----------|
| Modal 表单复杂度高 | 用户体验差 | 限制参数数量 ≤5，提供默认值 |
| Brain API 延迟 | 响应变慢 | 提供快速路径跳过 Brain |
| 能力滥用 | Token 消耗过大 | 增加频率限制、确认机制 |
| 配置文件损坏 | 功能不可用 | 验证配置格式、备份恢复 |

---

## 10. 里程碑

### Phase 1: MVP (v0.24.0)

- [ ] 基础 Capability 定义和注册
- [ ] App Home 页面构建
- [ ] Modal 动态表单
- [ ] 基础能力执行 (无 Brain 集成)
- [ ] 3 个核心能力: code_review, explain_code, debug_error

### Phase 2: Brain 集成 (v0.25.0)

- [ ] Native Brain 智能路由
- [ ] 意图确认
- [ ] 上下文压缩
- [ ] 额外 3 个能力: git_commit, pr_review, refactor

### Phase 3: 增强 (v0.26.0)

- [ ] YAML 热更新
- [ ] 自定义能力支持
- [ ] 使用统计和反馈
- [ ] 能力搜索/筛选

---

## 11. 附录

### A. Slack Block Kit 参考

```go
// 能力卡片 Button Block
slack.NewSectionBlock(
    slack.NewTextBlockObject("mrkdwn", fmt.Sprintf("%s *%s*", cap.Icon, cap.Name), false, false),
    nil,
    slack.NewAccessory(slack.NewButtonBlockElement(
        "cap_click",
        cap.ID,
        slack.NewTextBlockObject("plain_text", "执行", false, false),
    )),
)
```

### B. 相关 Issue

- #215: feat(slack): App Home 智能能力中心
