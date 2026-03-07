## 背景

用户在 Slack 中使用 HotPlex 时，需要每次手动输入完整的 prompt。现有方案存在局限：
- 静态推荐提示词：不够灵活，无法参数化
- 动态推荐提示词：依赖上下文推断，可能偏离用户意图

## 目标

构建 Slack App Home 智能能力中心，提供预定义的可参数化能力模板，让用户一键触发常见任务。

## 与 Native Brain 集成

App Home 能力中心与 Native Brain 模块深度协同：
1. **智能路由**：Brain 分析用户选择的能力，智能选择最优 Engine Provider
2. **上下文压缩**：复杂任务执行前，Brain 压缩历史上下文释放 Token 空间
3. **视觉推理**：执行过程中，Brain 将中间状态转化为可读的 AssistantStatus 文案

## 技术方案

### 1. Capability 定义结构

```go
type Capability struct {
    ID             string      // 能力唯一标识
    Name           string      // 显示名称
    Icon           string      // emoji 图标
    Description    string      // 能力描述
    Category       string      // 分类: code/analysis/debug/git
    Parameters     []Parameter // 可填充参数
    PromptTemplate string      // Prompt 模板
    BrainOpts      BrainOptions // Native Brain 配置
}

type Parameter struct {
    ID          string   // 参数标识
    Label       string   // 显示标签
    Type        string   // text/select/multiline
    Required    bool     // 是否必填
    Default     string   // 默认值
    Options     []string // 下拉选项
    Placeholder string   // 占位提示
}
```

### 2. 预定义能力清单

| ID | 名称 | 分类 | 描述 |
|----|------|------|------|
| code_review | 代码审查 | code | 对指定文件进行安全/性能/风格审查 |
| explain_code | 代码解释 | code | 解释代码片段的工作原理 |
| debug_error | 错误诊断 | debug | 分析错误信息并给出修复方案 |
| git_commit | Commit 生成 | git | 生成 Conventional Commit 消息 |
| pr_review | PR 审查 | code | 审查 PR 变更并给出建议 |
| refactor | 重构建议 | code | 分析代码并提供重构建议 |

### 3. 用户交互流程

```
用户点击 App Home 标签
       ↓
展示能力卡片网格 (按类别分组)
       ↓
用户点击能力卡片
       ↓
弹出参数填写 Modal (动态生成)
       ↓
用户填写参数并提交
       ↓
Native Brain 预处理 (意图确认/路由)
       ↓
渲染 Prompt 发送到 DM
       ↓
Engine 执行任务
```

### 4. 文件结构

```
chatapps/slack/apphome/
├── builder.go          # Home 页面构建
├── capability.go       # 能力定义与注册
├── registry.go         # 能力注册中心
├── handler.go          # 事件处理
├── form.go             # Modal 表单构建
├── brain_integration.go # Native Brain 集成
└── capabilities.yaml   # 预定义能力配置
```

### 5. 事件处理

需要处理的事件类型：
- `app_home_opened`: 用户打开 App Home 标签页
- `block_actions`: 能力卡片点击、表单提交
- `view_submission`: Modal 表单提交

## 价值

- **用户价值**：一键触发常见任务，无需重复输入 prompt
- **团队价值**：标准化最佳实践，新人快速上手
- **技术价值**：解耦 prompt 管理，支持热更新配置
- **成本优化**：结合 Native Brain 智能路由，降低重型模型调用

## 范围

- 新增 `chatapps/slack/apphome/` 包
- 扩展 `events.go` 处理 `app_home_opened` 事件
- 扩展 `interactive.go` 处理能力选择和提交
- 与 `brain/` 模块集成

## 成功标准

1. 用户打开 App Home 能看到能力卡片网格
2. 点击能力后弹出参数填写 Modal
3. 提交后正确渲染 prompt 并触发 Engine
4. 支持 YAML 配置热更新能力定义
5. 与 Native Brain 智能路由集成
