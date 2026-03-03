# Slack Chat App 顶级体验优化方案：AI Assistant 原生化演进

**版本**: v2.1 (Official Release)
**最后更新**: 2026-03-03
**验证状态**: ✅ API 真实性已核验

本文本着"近细远粗"的原则，结合 Slack 2026 最新 API 与 OpenClaw 最佳实践，制定 HotPlex Slack 端的原生化体验升级路线。

---

## 1. 核心愿景：从"对话框机器人"走向"原生 AI 助手"

依托 Slack **Agents & AI Apps** 框架，将 HotPlex 深度嵌入 Slack 核心 UI，利用流式输出、状态反馈和画布协作，打造媲美 Claude 原生 App 的研发助手体验。

### 1.1 核心视觉与交互特效

| 特效                                     | 实现方式                                                          | 状态      |
| ---------------------------------------- | ----------------------------------------------------------------- | --------- |
| **名字流光渐变 (Flowing Gradient Name)** | Slack Dashboard 开启 "Agents & AI Apps" + `assistant:write` Scope | ✅ 可行    |
| **原生状态反馈 (Assistant Status)**      | `assistant.threads.setStatus` API                                 | ✅ 官方 GA |
| **流式输出 (Text Streaming)**            | `slack-go/slack` 库的 `StartStream/AppendStream/StopStream`       | ✅ 库支持  |

> **技术实现细节**: 官方 Slack Web API 并不直接提供名为 `chat.startStream` 的方法。HotPlex 采用 `slack-go/slack` 库封装的流式接口，通过库提供的 `StartStream()` / `AppendStream()` / `StopStream()` 方法实现与 Slack 平台的高级通信协议交互。

---

## 2. 交叉模块分析：Brain、Storage 与 Slack 的深度协同

本方案并非隔离的 UI 优化，而是依赖于 HotPlex 核心模块升级的"感官体现"：

| 相关任务                          | 核心价值             | 对 Slack UX 的具体增强                                                                                                                 |
| :-------------------------------- | :------------------- | :------------------------------------------------------------------------------------------------------------------------------------- |
| **`issues/124` (Native Brain)**   | 统一 LLM 调用抽象    | `LLMAdapter` 抛出的中间推理事件（Reasoning Chunks）将直接驱动 `AssistantStatus` 的微光文字切换，实现"脑中思考，眼见跳动"。             |
| **`issues/125` (Native Brain)**   | 上下文记忆与压缩     | 基于 `MemoryManager` 的会话摘要，智能生成 `Suggested Prompts`，让推荐问题与对话历史强相关。                                            |
| **`issues/126` (Native Brain)**   | 安全守卫与内容过滤   | 在消息发送前由 Brain Guard 审查输出内容，过滤敏感信息（API Keys、内部路径），确保 Slack 消息安全性。                                   |
| **`issues/127` (Native Brain)**   | 意图预处理与路由     | 入口层意图识别，轻量请求（如状态查询）直接 Brain 响应，跳过 Engine 启动，实现毫秒级响应。                                              |
| **`issues/151` (Storage Plugin)** | 结构化消息与持久会话 | 基于存储插件提供的历史 Context，驱动回复后的 `Suggested Prompts` 生成。同时，利用 Session 状态判定何时触发 `AssistantTitle` 自动命名。 |

---

## 3. 第一阶段：基础架构原生化 (近期 - 极详)

**目标**：实现名字流光效果、原生状态文字，以及毫秒级响应的流式输出。

### 3.1 平台配置与基础接口扩展

1. **功能开关**：Slack App Dashboard -> App Home -> **Agents & AI Apps** 切换为 `On`。

2. **Manifest 更新**：
    ```yaml
    features:
      assistant_view:
        assistant_description: "HotPlex AI: 您的研发全栈助手"
    oauth_config:
      scopes:
        bot:
          - assistant:write  # 核心权限：驱动状态文字与流式输出
          - chat:write
    ```

3. **基础层扩展 (`chatapps/base/types.go`)**：
    为了保持平台中立并遵循 **Issue #151** 的 ISP 原则，建议在 `MessageOperations` 中增加以下抽象接口：
    ```go
    type MessageOperations interface {
        // ... 原有 DeleteMessage, UpdateMessage ...

        // SetAssistantStatus 设置线程底部的原生助手状态文字
        SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error
        // StartStream 开启一个原生流式消息，返回 message_ts 作为后续锚点
        StartStream(ctx context.Context, channelID, threadTS string) (string, error)
        // AppendStream 向现有流增量推送内容
        AppendStream(ctx context.Context, channelID, messageTS, content string) error
        // StopStream 结束流并固化消息
        StopStream(ctx context.Context, channelID, messageTS string) error
    }
    ```

### 3.2 Adapter 接口扩展 (`chatapps/slack/adapter.go`)

利用 `slack-go/slack v0.18.0` 库能力，封装高性能通信接口。

> **库版本确认**: 项目当前使用 `slack-go/slack v0.18.0`，已完全支持 Assistant Threads API 和流式消息 API。

* **状态反馈封装**：
    ```go
    // SetAssistantStatus 用于驱动线程底部的动态文字（如："正在思考..."、"正在搜索代码..."）
    func (s *SlackAdapter) SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
        params := slack.AssistantThreadsSetStatusParameters{
            ChannelID: channelID,
            ThreadTS:  threadTS,
            Status:    status,
        }
        return s.api.SetAssistantThreadsStatusContext(ctx, params)
    }
    ```

* **流式输出封装**：
    ```go
    // StartStream 开启流式消息
    func (s *SlackAdapter) StartStream(ctx context.Context, channelID, threadTS string) (string, error) {
        // slack-go 库的 StartStream 返回 (channelID, timestamp, error)
        _, ts, err := s.api.StartStreamContext(ctx, channelID,
            slack.MsgOptionMarkdownText(""), // 空内容启动流
        )
        return ts, err
    }

    // AppendStream 追加流式内容
    func (s *SlackAdapter) AppendStream(ctx context.Context, channelID, messageTS, content string) error {
        _, err := s.api.AppendStreamContext(ctx, channelID, messageTS,
            slack.MsgOptionMarkdownText(content),
        )
        return err
    }

    // StopStream 结束流式消息
    func (s *SlackAdapter) StopStream(ctx context.Context, channelID, messageTS string) error {
        _, err := s.api.StopStreamContext(ctx, channelID, messageTS)
        return err
    }
    ```

* **原生流式封装 (`NativeStreamingWriter`)**：
    实现一个 `io.Writer` 的包装器，内部维护 `stream_id` 生命周期：
    1. **Write(p []byte)**: 首次调用执行 `StartStream` 获取 TS；后续调用执行 `AppendStream` 增量推送。
    2. **Close()**: 调用 `StopStream` 最终固化消息。

### 3.3 Engine 状态流转重构 (`chatapps/engine_handler.go`)

配合 **Issue #124 (Brain)** 的事件抛出机制，升级 `StreamCallback`：

1. **感知启动**：收到 Brain 的 `Reasoning` 事件瞬间调用 `SetAssistantStatus("正在思考...")`。
2. **过程感知**：
   - 进入 Tool 调用前：`SetAssistantStatus("正在搜索项目文件...")`。
   - 开始生成回复：通过 `StartStream` 开启原生推流，并更新状态为 `正在组织回答...`。
3. **结果交付**：
   - **关键变动**：一旦启用原生状态，将抑制旧版 `MessageTypeThinking` 气泡的发送，彻底消除"流式输出时有个思考气泡占位"的顽疾。
   - 任务结束：调用 `StopStream`，Slack 会自动清理 Assistant Status。

> **关联 Issue #127**：在入口层（`engine_handler.go` 请求入口）接入 `brain.IntentRouter`，轻量请求（如"当前模型是什么？"、"查看状态"）直接由 Brain 响应，跳过 Engine 启动，实现**毫秒级响应**。

---

## 4. 第二阶段：交互与语境增强 (中期 - 较详)

**目标**：结合 **`issues/151`** 与 **`issues/125`**，提升对话的连续性和语境感。

### 4.1 智能下一步引导 (Suggested Prompts)

* **接口**：`assistant.threads.setSuggestedPrompts`（通过 `slack-go` 库的 `SetAssistantThreadsSuggestedPrompts` 方法调用）
* **参数结构**：
    ```go
    params := slack.AssistantThreadsSetSuggestedPromptsParameters{
        ChannelID: channelID,
        ThreadTS:  threadTS,
        Title:     "接下来，您可以：",
        Prompts: []slack.AssistantPrompt{
            {Title: "生成单元测试", Message: "请为这段代码生成单元测试"},
            {Title: "解释风险", Message: "这段代码有什么潜在风险？"},
        },
    }
    ```
* **实现**：AI 回复结束后，根据回复内容生成 2-3 个"推荐下一步"按钮（如："生成单元测试"、"解释风险"）。
* **价值**：点击即触发，大幅降低用户输入成本。

> **关联 Issue #125**：上述静态推荐可进一步升级为**记忆驱动的智能推荐**。由 `brain/memory.go` 的 `MemoryManager` 分析会话历史，在压缩上下文时提取关键主题（如"用户正在调试 API 路由"），动态生成与当前任务强相关的 Suggested Prompts（如"检查路由中间件配置"）。

### 4.2 对话标题自动总结 (Thread Titling)

* **接口**：`assistant.threads.setTitle`（通过 `slack-go` 库的 `SetAssistantThreadsTitle` 方法调用）
* **场景**：在对话进行到 2 轮以上时，利用轻量级推理生成会话标题。
* **针对点**：解决 Slack 侧边栏"全是项目名"的痛点，方便用户快速定位历史讨论。

### 4.3 安全守卫与内容过滤 (Safety Guard)

结合 **`issues/126`**，在消息发送前接入 Brain Guard 审查：

* **审查时机**：Engine 输出完成后、Slack 消息发送前。
* **审查内容**：
    - 敏感信息过滤：API Keys、内部服务路径、Token 泄露检测
    - 恶意指令检测：Prompt Injection 防御
    - 格式校验：Block Kit JSON 结构完整性
* **处理方式**：
    - 检测到敏感信息 → 自动脱敏（替换为 `***`）
    - 高风险内容 → 拦截并发送安全警告消息
* **接口**：消息发送前回调 `brain.Guard.Inspect(ctx, content) (filteredContent, riskLevel)`

> **安全价值**：确保 HotPlex 输出的每条 Slack 消息都经过内容安全审查，避免内部信息泄露。

### 4.4 平台自驱管理 (Chat2Config)

结合 **`issues/124`** 的高级能力，管理员可直接在 Slack 中通过自然语言管理平台：

* **典型指令**：
    - "将当前频道的默认模型切换为 GPT-4o"
    - "开启此会话的安全拦截等级为 High"
    - "查看当前系统的流控配置"
* **集成方式**：`brain.IntentRouter` 识别管理意图 -> 解析为指令对象 -> 调用 `config.Manager` 热更新或内存应用。
* **优势**：消除对繁琐 YAML 修改的直接依赖，降低平台运维门槛。

---

## 5. 第三阶段：深度生产力协作 (远期 - 宏观)

**目标**：结合 **`issues/124`**，利用协作组件处理复杂研发产物。

### 5.1 协作画布 (Canvas Integration)

* **方向**：将生成的长篇架构文档、测试报告自动转化为 **Slack Canvas**。
* **优势**：支持实时编辑与收藏，不再淹没在聊天气泡中。

### 5.2 结构化产物交付 (File Upload v2)

* **方向**：升级至三阶段文件上传 API，支持断点续传大尺寸研发产物（如项目 Patches 或 Build Logs）。

---

## 6. 落地实施路线图 (Roadmap)

| 节点   | 核心任务                                     | 状态     | 相关依赖               |
| :----- | :------------------------------------------- | :------- | :--------------------- |
| **P1** | Dashboard 配置 + `base` 接口扩展             | ✅ 完成   | -                      |
| **P1** | Adapter 封装 `SetAssistantStatus` 与原生流式 | ✅ 完成   | `slack-go v0.18.0`     |
| **P2** | Engine 逻辑重构，启用流式感知流转            | ✅ 完成   | `issues/124` (Brain)   |
| **P2** | 连贯对话：Suggested Prompts & Thread Titling | 待办   | `issues/151` (Storage) |
| **P2** | 记忆驱动智能推荐 (Issue #125)                | 待办   | `issues/125` (Memory)  |
| **P2** | Brain Guard 消息发送前安全审查               | 待办   | `issues/126` (Guard)   |
| **P2** | Chat2Config 平台自驱配置能力                 | 待办   | `issues/124` (Ops)     |
| **P3** | 深度协作：Canvas 画布与 File v2 集成         | 待办   | `issues/124`           |


---

## 7. API 验证报告

### 7.1 官方 API 核验

| API 方法                                | Slack Web API     | slack-go 库                                  | 状态     |
| :-------------------------------------- | :---------------- | :------------------------------------------- | :------- |
| `assistant.threads.setStatus`           | ✅                 | `SetAssistantThreadsStatusContext`           | ✅ GA     |
| `assistant.threads.setTitle`            | ✅                 | `SetAssistantThreadsTitleContext`            | ✅ GA     |
| `assistant.threads.setSuggestedPrompts` | ✅                 | `SetAssistantThreadsSuggestedPromptsContext` | ✅ GA     |
| `chat.startStream`                      | ❌ (非官方 API 名) | `StartStreamContext`                         | ✅ 库方法 |
| `chat.appendStream`                     | ❌ (非官方 API 名) | `AppendStreamContext`                        | ✅ 库方法 |
| `chat.stopStream`                       | ❌ (非官方 API 名) | `StopStreamContext`                          | ✅ 库方法 |

### 7.2 所需 Scopes

| Scope             | 用途                       | 状态   |
| :---------------- | :------------------------- | :----- |
| `assistant:write` | 驱动 Assistant Threads API | ✅ 必需 |
| `chat:write`      | 基础消息发送               | ✅ 已有 |

### 7.3 Rate Limits

| API                                   | Rate Limit | 备注               |
| :------------------------------------ | :--------- | :----------------- |
| `assistant.threads.*`                 | Special    | 特殊限制，按线程计 |
| `StartStream/AppendStream/StopStream` | Tier 4     | 标准消息操作       |

---

## 8. 为什么选择该方案？

1. **专业度**：这是 2026 年 Slack 平台上顶级 AI 应用（如 Claude, OpenClaw）的统一种植方案。
2. **低延迟**：流式输出通过 `slack-go` 库封装，相比频繁的 `chat.update` 有更低的通信开销和更高的前端渲染效率。
3. **品牌感**：名字的流光效果是 Slack 官方对"正牌 AI"的视觉背书。

**结论**：本方案确保 HotPlex 在 Slack 平台上始终保持最顶级的 AI 原生体验，将 IM 彻底转型为高效的生产力工作空间。

---

## 附录：参考资料

1. [Slack Web API - assistant.threads.setStatus](https://api.slack.com/methods/assistant.threads.setStatus)
2. [Slack Web API - assistant.threads.setTitle](https://api.slack.com/methods/assistant.threads.setTitle)
3. [Slack Web API - assistant.threads.setSuggestedPrompts](https://api.slack.com/methods/assistant.threads.setSuggestedPrompts)
4. [Slack Go SDK - v0.18.0](https://github.com/slack-go/slack)
5. [assistant:write Scope](https://api.slack.com/scopes/assistant:write)
6. [Slack Block Kit 文档](https://docs.slack.dev/block-kit/)
