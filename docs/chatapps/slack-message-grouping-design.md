# Slack 消息分组设计 (优化版)

**创建日期**: 2026-02-28
**最后更新**: 2026-02-28
**状态**: 已实施
**版本**: v2.2

---

## 设计目标

将 Slack 消息按功能分组，各组之间相对顺序固定，组内消息持续更新。

---

## 区域划分

### 1. 思考区 (Thinking Zone)

**包含事件**: `thinking`, `plan_mode`

**更新策略**: 首条固化 + 滑动窗口
- **Index 0 固化**: 第一条思考记录始终保留，作为意图起始锚点
- **后续裁剪**: 超过 N 条时，从 Index 1 开始裁剪，保留最近 N 条记录

**配置**:
```go
ZoneConfig{
    MaxMsgs:  5,       // 最多保留 5 条
    MaxBytes: 1500,    // 总内容上限
}
```

**展示格式**:
```
:brain: *Thinking*
> 思考内容预览...
```

---

### 2. 行动区 (Action Zone)

**包含事件**:
- `tool_use`, `tool_result` - 工具调用与结果
- `permission_request`, `danger_block` - 权限与危险操作确认
- `command_progress`, `command_complete` - 命令执行
- `step_start`, `step_finish` - 步骤标记
- `session_start`, `engine_starting` - 会话/引擎启动

**更新策略**: 滑动窗口 + 摘要形式 + 错误穿透
- **成功路径**: 仅展示摘要（图标、状态、耗时、数据长度）
- **失败路径**: **主动穿透**，在摘要下方追加代码块回显报错信息（前 200 字符）
- **阻塞确认**: `permission_request` 等交互事件使用 Header/Divider 视觉提权
- 限制条目数量，避免刷屏

**配置**:
```go
ZoneConfig{
    MaxMsgs:  8,       // 最多保留 8 条
    MaxBytes: 2000,    // 摘要模式，内容上限较低
}
```

**展示格式示例**:
```
:computer: *Read*
Using tool: `file.txt`

:white_check_mark: *Read* (1.2s) • 3.5KB

┌────────────────────────────────────────────────────────┐
│ 🔍 *Glob* | `*.go`                                     │
│ 📚 *Read* | `main.go`                                  │
│ 💻 *Bash* | `python3 verify...`                       │
├────────────────────────────────────────────────────────┤
│ ✅ *Glob* • 2B                                         │
│ ✅ *Read* • 1.2KB                                      │
│ ❌ *Bash* (1.5s) • 512B  [查看详情]                    │
└────────────────────────────────────────────────────────┘
```

**错误穿透示例**:
```
:computer: *Read*
Using tool: `file.txt`

:x: *Read* (1.2s)
```


---

### 3. 展示区 (Output Zone)

**包含事件**: `answer`, `ask_user_question`, `error`, `exit_plan_mode`

**更新策略**: 限频流式更新 + 超长自动分页
- **限频更新**: 引入 800ms - 1s 的自适应防抖更新，规避 Slack Rate Limit
- 全程保持单消息内打字机效果，超过 4000 字符自动分页

**配置**:
```go
ZoneConfig{
    MaxBytes:  4000,      // Slack 单消息上限
    UseUpdate: true,      // 启用 chat.update 流式更新
}
```

**展示格式**:
```
[Markdown 格式的回答内容]
```

---

### 4. 总结区 (Summary Zone)

**包含事件**: `session_stats`

**更新策略**: 固定最后一条消息，持续更新 + 修改清单
- 始终位于消息流末尾
- 每轮对话结束时更新指标及**修改的文件清单**

**配置**:
```go
ZoneConfig{
    MaxMsgs:   1,         // 仅保留最新
    UseUpdate: true,      // 原地更新
}
```

**展示格式**:
```
:white_check_mark: *Done*
⏱️ 12s • ⚡ 1.2K/350 •  3 tools
📝 *Changes*: `file1.go`, `file2.md`
```

---

## 区域顺序

```
┌─────────────────────────┐
│    思考区 (Thinking)     │  Zone 0
├─────────────────────────┤
│    行动区 (Action)       │  Zone 1
├─────────────────────────┤
│    展示区 (Output)       │  Zone 2
├─────────────────────────┤
│    总结区 (Summary)      │  Zone 3
└─────────────────────────┘
```

**保障机制**: `ZoneOrderProcessor` 确保顺序，仅依赖内存/Session 状态，无需外部存储

---

## 不显示的事件 (过滤列表)

以下事件对用户是噪音，**直接过滤不发送**:

| 事件                    | 原因                 |
| ----------------------- | -------------------- |
| `system`                | 系统级信息           |
| `user`                  | 用户消息反射（冗余） |
| `raw`                   | 未解析内容           |
| `user_message_received` | 收到消息确认（冗余） |

---

## 轮次语义

- 每轮对话（用户输入 → AI 完成）**按需出现**区域
- 某些区域可能为空（如无工具调用时行动区为空）
- 区域框架固定，内容动态

---

## 实现架构

### 组件分层

```
┌─────────────────────────────────────────────────────────┐
│                    Event Handlers                        │
│  (engine_handler.go: handleThinking, handleToolUse...)   │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                  Processor Chain                         │
│  ┌─────────────────────────────────────────────────┐    │
│  │  1. MessageFilterProcessor (Order=5)             │    │
│  │     过滤隐藏事件                                  │    │
│  │  2. ZoneOrderProcessor (Order=15)                │    │
│  │     保障区域顺序                                  │    │
│  │  3. MessageAggregatorProcessor (Order=20)        │    │
│  │     滑动窗口聚合                                  │    │
│  │  4. ZoneConfigProvider                           │    │
│  │     区域配置注入                                  │    │
│  └─────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                    Message Builder                       │
│  (slack/builder.go: BuildThinkingMessage...)             │
│  - 摘要生成：行动区事件生成摘要，不显示具体内容           │
│  - 分页逻辑：超长内容自动分块                             │
│  - 流式更新：支持 chat.update                             │
└─────────────────────────────────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│                    Slack Adapter                         │
│  (slack/adapter.go: SendMessage)                         │
└─────────────────────────────────────────────────────────┘
```

---

## 可行性分析

### ✅ 已实现的支持

| 功能                              | 状态     | 位置                            |
| --------------------------------- | -------- | ------------------------------- |
| `BuildToolResultMessage` 摘要显示 | ✅ 已修复 | `builder.go:213-240`            |
| `EventConfig` 事件策略            | ✅ 已存在 | `processor_aggregator.go:30-70` |
| `ProcessorChain` 扩展支持         | ✅ 已存在 | `processor_chain.go`            |
| 流式更新 (`UseUpdate`)            | ✅ 已支持 | `EventConfig.UseUpdate`         |

### ⚠️ 需补充的实现

| 功能                              | 复杂度 | 估测行数 |
| --------------------------------- | ------ | -------- |
| `MessageFilterProcessor`          | 低     | ~20 行   |
| `ZoneOrderProcessor`              | 中     | ~50 行   |
| `ZoneConfig` 差异化配置           | 低     | ~30 行   |
| 集成到 `NewDefaultProcessorChain` | 低     | ~10 行   |

**总计**: ~110 行新增代码

---

## 实施计划

### Phase 1: 消息过滤器 ✅ 优先

**目标**: 过滤隐藏事件

**任务**:
- [x] 创建 `processor_filter.go`
- [x] 实现 `MessageFilterProcessor`
- [x] 配置 `hiddenEvents` 列表
- [x] 集成到 `NewDefaultProcessorChain` (Order=5)

**验收**: `system`, `user`, `raw`, `user_message_received` 不再发送

---

### Phase 2: 区域顺序保障

**目标**: 确保消息按区域顺序发送

**任务**:
- [x] 创建 `processor_zone_order.go`
- [x] 实现 `ZoneOrderProcessor` 并支持 **Index 0 锚点固化**
- [x] 定义 `zoneOrder` 映射
- [x] 集成到 `NewDefaultProcessorChain` (Order=12)
- [x] 在 Slack Adapter 增加 **自适应限频逻辑** (Phase 1.5)

**验收**: 消息顺序始终为 thinking → action → output → summary

---

### Phase 3: 区域差异化配置

**目标**: 各区域独立配置滑动窗口参数

**任务**:
- [x] 定义 `ZoneConfig` 结构
- [x] 配置各区域 `MaxMsgs`, `MaxBytes`
- [x] 实现 **行动区失败内容回显 (前 200 字符)**
- [x] 集成到 `MessageAggregatorProcessor.getEventConfig`

**验收**: 思考区 5 条上限，行动区 8 条上限

---

### Phase 4: 测试与验证

**任务**:
- [x] 单元测试：各处理器独立测试
- [ ] 集成测试：完整消息流测试
- [ ] 性能测试：高并发场景验证
- [ ] 手动测试：Slack 实际效果验证

---

## 相关文件

### 现有文件
- `chatapps/processor_aggregator.go` - 消息聚合逻辑
- `chatapps/processor_chain.go` - 处理器链
- `chatapps/slack/builder.go` - Slack 消息构建
- `chatapps/engine_handler.go` - 事件处理

### 新增文件
- `chatapps/processor_filter.go` - 消息过滤器
- `chatapps/processor_zone_order.go` - 区域顺序保障

---

## 附录：事件 - 区域映射表

| 事件                    | 区域   | Zone Index | 聚合策略  |
| ----------------------- | ------ | ---------- | --------- |
| `thinking`              | 思考区 | 0          | Immediate |
| `plan_mode`             | 思考区 | 0          | UseUpdate |
| `tool_use`              | 行动区 | 1          | Aggregate |
| `tool_result`           | 行动区 | 1          | Immediate |
| `permission_request`    | 行动区 | 1          | Immediate |
| `danger_block`          | 行动区 | 1          | Immediate |
| `command_progress`      | 行动区 | 1          | UseUpdate |
| `command_complete`      | 行动区 | 1          | Immediate |
| `step_start`            | 行动区 | 1          | Immediate |
| `step_finish`           | 行动区 | 1          | Aggregate |
| `session_start`         | 行动区 | 1          | Immediate |
| `engine_starting`       | 行动区 | 1          | Aggregate |
| `answer`                | 展示区 | 2          | UseUpdate |
| `ask_user_question`     | 展示区 | 2          | Immediate |
| `error`                 | 展示区 | 2          | Immediate |
| `exit_plan_mode`        | 展示区 | 2          | Immediate |
| `session_stats`         | 总结区 | 3          | Immediate |
| `system`                | 过滤   | -          | -         |
| `user`                  | 过滤   | -          | -         |
| `raw`                   | 过滤   | -          | -         |
| `user_message_received` | 过滤   | -          | -         |
