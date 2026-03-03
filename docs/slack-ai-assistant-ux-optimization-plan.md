# Slack Chat App 顶级体验优化方案：AI Assistant 原生化演进

本文本着“近细远粗”的原则，结合 Slack 2026 最新 API 与 OpenClaw 最佳实践，制定 HotPlex Slack 端的原生化体验升级路线。

## 1. 核心愿景：从“对话框机器人”走向“原生 AI 助手”

依托 Slack **Agents & AI Apps** 框架，将 HotPlex 深度嵌入 Slack 核心 UI，利用流式输出、状态反馈和画布协作，打造媲美 Claude 原生 App 的研发助手体验。

### 1.1 核心视觉与交互特效
*   **名字流光渐变 (Flowing Gradient Name)**：由 Slack 客户端对启用 **"Agents & AI Apps"** 功能的应用自动进行的 AI 品牌化渲染。在 Dashboard 开启并配置正确 Scopes 后自动生效。
*   **原生状态反馈 (Assistant Status)**：使用 `assistant.threads.setStatus` 接口，在线程底部显示低调的微光（Shimmer）动画和状态文字。相比传统的“正在输入”点点动画，它更具高级感，且不会产生消息噪音。
*   **原生流式输出 (Native Text Streaming)**：使用 `chat.startStream` / `chat.appendStream` / `chat.stopStream`。相比传统 `chat.update` 模拟流式，原生接口响应更快、无 Rate Limit 压力。

---

## 2. 交叉模块分析：Brain、Storage 与 Slack 的深度协同

本方案并非隔离的 UI 优化，而是依赖于 HotPlex 核心模块升级的“感官体现”：

| 相关任务                          | 核心价值             | 对 Slack UX 的具体增强                                                                                                                 |
| :-------------------------------- | :------------------- | :------------------------------------------------------------------------------------------------------------------------------------- |
| **`issues/124` (Native Brain)**   | 统一 LLM 调用抽象    | `LLMAdapter` 抛出的中间推理事件（Reasoning Chunks）将直接驱动 `AssistantStatus` 的微光文字切换，实现“脑中思考，眼见跳动”。             |
| **`issues/151` (Storage Plugin)** | 结构化消息与持久会话 | 基于存储插件提供的历史 Context，驱动回复后的 `Suggested Prompts` 生成。同时，利用 Session 状态判定何时触发 `AssistantTitle` 自动命名。 |

---

## 3. 第一阶段：基础架构原生化 (近期 - 极详)

**目标**：实现名字流光效果、原生状态文字、以及毫秒级响应的流式输出。

### 2.1 平台配置与基础接口扩展
1.  **功能开关**：Slack App Dashboard -> App Home -> **Agents & AI Apps** 切换为 `On`。
2.  **Manifest 更新**：
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
3.  **基础层扩展 (`chatapps/base/types.go`)**：
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

### 2.2 Adapter 接口扩展 (`chatapps/slack/adapter.go`)
利用 `slack-go/slack v0.18.0` 本地库能力，封装高性能通信接口。

*   **状态反馈封装**：
    ```go
    // SetAssistantStatus 用于驱动线程底部的动态文字（如：“正在思考...”、“正在搜索代码...”）
    func (s *SlackAdapter) SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
        params := slack.AssistantThreadsSetStatusParameters{
            ChannelID: channelID,
            ThreadTS:  threadTS,
            Status:    status,
        }
        return s.api.SetAssistantThreadsStatusContext(ctx, params)
    }
    ```
*   **原生流式封装 (`NativeStreamingWriter`)**：
    实现一个 `io.Writer` 的包装器，内部维护 `stream_id` 生命周期：
    1.  **Write(p []byte)**: 首次调用执行 `StartStream` 获取 TS；后续调用执行 `AppendStream` 增量推送。
    2.  **Close()**: 调用 `StopStream` 最终固化消息。

### 2.3 Engine 状态流转重构 (`chatapps/engine_handler.go`)
配合 **Issue #124 (Brain)** 的事件抛出机制，升级 `StreamCallback`：
1.  **感知启动**：收到 Brain 的 `Reasoning` 事件瞬间调用 `SetAssistantStatus("正在思考...")`。
2.  **过程感知**：
    *   进入 Tool 调用前：`SetAssistantStatus("正在搜索项目文件...")`。
    *   开始生成回复：通过 `StartStream` 开启原生推流，并更新状态为 `正在组织回答...`。
3.  **结果交付**：
    *   **关键变动**：一旦启用原生状态，将抑制旧版 `MessageTypeThinking` 气泡的发送，彻底消除“流式输出时有个思考气泡占位”的顽疾。
    *   任务结束：调用 `StopStream`，Slack 会自动清理 Assistant Status。

---

## 4. 第二阶段：交互与语境增强 (中期 - 较详)

**目标**：结合 **`issues/151`**，提升对话的连续性和语境感。

### 3.1 智能下一步引导 (Suggested Prompts)
*   **接口**：`SetAssistantThreadsSuggestedPrompts`。
*   **实现**：AI 回复结束后，根据回复内容生成 2-3 个“推荐下一步”按钮（如：“生成单元测试”、“解释风险”）。
*   **价值**：点击即触发，大幅降低用户输入成本。

### 3.2 对话标题自动总结 (Thread Titling)
*   **接口**：`SetAssistantThreadsTitle`。
*   **场景**：在对话进行到 2 轮以上时，利用轻量级推理生成会话标题。
*   **针对点**：解决 Slack 侧边栏“全是项目名”的痛点，方便用户快速定位历史讨论。

---

## 5. 第三阶段：深度生产力协作 (远期 - 宏观)

**目标**：结合 **`issues/124`**，利用协作组件处理复杂研发产物。

### 4.1 协作画布 (Canvas Integration)
*   **方向**：将生成的长篇架构文档、测试报告自动转化为 **Slack Canvas**。
*   **优势**：支持实时编辑与收藏，不再淹没在聊天气泡中。

### 4.2 结构化产物交付 (File Upload v2)
*   **方向**：升级至三阶段文件上传 API，支持断点续传大尺寸研发产物（如项目 Patches 或 Build Logs）。

---

## 6. 落地实施路线图 (Roadmap)

| 节点   | 核心任务                                     | 状态   | 相关依赖               |
| :----- | :------------------------------------------- | :----- | :--------------------- |
| **P1** | Dashboard 配置 + `base` 接口扩展             | 规划中 | -                      |
| **P1** | Adapter 封装 `SetAssistantStatus` 与原生流式 | 进行中 | `slack-go v0.18.0`     |
| **P2** | Engine 逻辑重构，启用流式感知流转            | 待办   | `issues/124` (Brain)   |
| **P2** | 连贯对话：Suggested Prompts & Thread Titling | 待办   | `issues/151` (Storage) |
| **P3** | 深度协作：Canvas 画布与 File v2 集成         | 待办   | `issues/124`           |

---

## 7. 为什么选择该方案？
1.  **专业度**：这是 2026 年 Slack 平台上顶级 AI 应用（如 Claude, OpenClaw）的统一种植方案。
2.  **低延迟**：原生流式接口相比不断的 `chat.update` 有更低的通信开销和更高的前端渲染效率。
3.  **品牌感**：名字的流光效果是 Slack 官方对“正牌 AI”的视觉背书。

**结论**：本方案确保 HotPlex 在 Slack 平台上始终保持最顶级的 AI 原生体验，将 IM 彻底转型为高效的生产力工作空间。
