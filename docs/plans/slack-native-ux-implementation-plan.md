# Slack Chat App 极致体验演进方案：AI Native UX 架构重建 (v3.0)

**版本**: v3.0 (Ultimate AI Native Focus)
**最后更新**: 2026-03-04
**状态**: 核心架构范式翻新就绪
**核心受众**: 高级开发者 / 研发工程师 (对接 Claude Code 等高权限/强力 CLI Agent)

本方案彻底抛弃“把 AI 伪装成普通聊天机器人”的历史包袱。在毫无真实用户与前向兼容压力的当下，我们将秉持 **Clean Architecture** 的原则，全面拥抱 Slack 的 **Agents & AI Apps** 框架。

**核心平衡点：**
我们需要**极简的 Slack UI（抗刷屏降噪）**，但面向开发者，我们**绝不牺牲透明度与确切的执行感知**。AI Agent (如 Claude Code) 权限极大，它在系统里“思考了什么”、“执行了哪一步”、“敲了什么命令”，必须做到**降级但不消音**，让开发者随时掌握全盘上下文。

---

## 核心设计哲学与 21 种事件映射

我们将 21 种 HotPlex 引擎核心事件进行精细化重构，整体划分为五大体验层：

### 1. 🌟 感官流光层 (Sensory Layer) —— 永远在线的“生命体征”

这是此次重构的**灵魂所在**。引擎的任何事件流转，都必然伴随 `slack.SetAssistantStatus` 的动态更新，让 AI 助手感觉“一直是活着的”，彻底取代占用屏幕空间的假消息。

* **核心原则**：所有 21 个事件的入口点，都会先映射并触发一次 `SetAssistantStatus` 更新。
* **状态映射示例**：
  - `session_start` / `engine_starting` -> *Status: 🚀 正在初始化上下文...*
  - `thinking` -> *Status: 🧠 深度推演规划中...*
  - `tool_use` -> *Status: 🛠️ 正在执行 {tool_name}...*
  - `tool_result` -> *Status: 📥 解析执行结果 (耗时: {duration}ms)...*
  - `plan_mode` -> *Status: 📝 正在制定作战计划...*
  - `ask_user_question` -> *Status: ⏳ 等待您提供更多信息...*
  - `danger_block` -> *Status: ⚠️ 拦截到高危操作，等待提权...*
  - `answer` (流式推流中) -> *Status: ✍️ 正在组织最终回答...*
  - `session_stats` / 任务结束 -> *Status: 清空状态 (设置为空字符串 `""`，恢复默认态)*

### 2. 🔍 极客透明层 (Geek Transparency Layer) —— 降维折叠，脉络清晰

开发者**必须**知道 AI 正在干什么。对于思考过程、步骤推进和工具调用，我们摒弃刷屏的 `Raw Text`，全部降维成**高度压缩、带图标的高信噪比 Block Kit**。
* **对应事件**: `plan_mode`, `exit_plan_mode`, `step_start`, `step_finish`, `tool_use`, `command_complete`
* **重构路径**:
  1. **结构化认知树 (`plan_mode`, `exit_plan_mode`)**：
     - 当大语言模型进入 Plan 阶段，以带有强烈结构性（如蓝边 Quote 或者 Slack 的 Collapsible Section）的单条 Block 展示它的 Plan 步骤，绝不静默，开发者需要借此了解其意图。
  2. **微缩里程碑 (`step_start`, `step_finish`, `command_complete`)**：
     - 将原本啰嗦的消息改为一句带 Emoji 的单行 Context Block（例：`▶️ Step: 分析现有架构` -> `✅ Step 完成`；`⚡ Command 执行成功`）。
     - 支持 In-place 更新：`step_start` 创建一行；当收到对应的 `finish` 时，直接 `chat.update` 把那行的 icon 换成绿勾。
  3. **单行微积分 (`tool_use`)**：
     - 仅渲染为 1 行极其紧凑的 🛠️ Context Block（例：`🛠️ grep_search | args: "SetAssistant" | 耗时: 120ms`）。开发者一眼能看明白使用的刀是什么。

### 3. 🧬 空间折叠层 (Space Folding Layer) —— 治理长文本怪物

巨量日志不应污染主信道，但必须“唾手可得”。
* **对应事件**: `tool_result`, `command_progress`, `raw`
* **重构路径**:
  1. **动态多级降级线程池** (`processor_thread.go` 升级)：
     - **短文本**：直接附加在上面的 `tool_use` Block 后面，以内联形式呈现（如 `ls` 的短结果）。
     - **长文本折叠 (超大 `tool_result`, `npm install`, `git diff`)**：一旦探测到巨量输出，**彻底拦截其进入主频道**。Handler 自动将完整日志作为隐蔽的回帖 (Thread Reply) 发送。
     - **主线联动**：在主频道的那条 `tool_use` 区块下，追加一句极简提示：“📋 `输出过长，已收纳至回复 (3.2KB)`”，并带有直接跳到 thread 的深链接。

### 4. 🛡️ 安全阻断层 (Safety Layer) —— 断点挂起与互动防御

对于高危操作和系统反问，实现 **Engine 挂起 + UI 解锁** 的绝对闭环。
* **对应事件**: `permission_request`, `danger_block`, `ask_user_question`, `error`
* **重构路径**:
  1. **Engine 挂起范式**：当抛出 `danger_block`（高危动作拦截，例如 `rm -rf`）或 `permission_request`（提权）时，`engine` 必须进入强阻塞 WAIT 态。
  2. **高保真红黄牌**：向终端投递带高亮警示色彩与 Approve/Reject 回调按钮的交互式卡片。
  3. **Callback 唤醒**：在 `interactive.go` 中拦截用户按钮点击。被点击后的卡片立刻自毁重绘（变为灰色已阅状态，保留历史记录但不允许再次触发），同时提取决策结果，通过内部信道重新拉起/击杀对应的 Engine Goroutine。

### 5. 💬 核心原声浪与仪表盘 (Streaming & Dashboard)

最终的回应交付与会话健康度监控。
* **对应事件**: `answer`, `session_stats`, `user_message_received`, `user`, `system`
* **重构路径**:
  1. **云原生推流 (`answer`)**：强制启动 `NativeStreamingWriter`，通过 `chat.startStream` 与 `chat.appendStream` 原生流式渲染大篇幅报告/代码解答，彻底消除 Update 频率带来的限流困扰。
  2. **健康度仪表盘 (`session_stats`)**：
     - 开发者需要关注 Token 消耗，以决定何时需要重置会话 (`/reset`) 或清理上下文。
     - 这些核心数据将被精美地渲染为一条底部的灰色 `Context Block` 小字（例：`📊 总消耗: 14.2k Tokens | 会话持续: 12m`），绝不刺眼，但一瞥可知生命周期健康度。
  3. **静默吸收**：仅对 `user_message_received`, `user`, `system` 这种 Slack 已经原生展示或纯系统暗语做彻底的不落盘过滤，免除冗余。

---

## 落地实施路线图 (Zero Baggage Roadmap)

为了抵达**极致 Native 的开发者工作流体验**，实施三阶段激进覆盖：

### 🏁 Sprint 1: 视觉流光与生命体征 (Sensory & Streaming)
* **动作**: 入侵 `chatapps/engine_handler.go` 与 `builder.go`。
* **目标**: 杀掉臃肿的 `Thinking` 气泡；完成 21 个事件全天候 `SetAssistantStatus` 探针覆盖；实现任务结束后 Status 清空闭环；对接 `Start/Append/Stop Stream` 系统。

### 🏁 Sprint 2: 极客透明度与空间折叠 (Transparency & Space Folding)
* **动作**: 魔改 `chatapps/processor_thread.go` 聚合层，精雕 `builder.go` 里的微渲染。
* **目标**: 将大模型的心智活动、步骤与 `session_stats` 转化为带有上下文的极简 Block；构建 `Size-based Thread Splitting` 算法，长日志无缝分摊至 Thread。

### 🏁 Sprint 3: 高危交互引擎闭环 (Interactive Engine Loop)
* **动作**: 重置 `chatapps/slack/interactive.go` 回调。
* **目标**: 彻底打通红/黄牌卡片的下发与 Engine Wait/Resume 接口映射，完成高权限大模型动作的硬拦截机制。

---
*我们的愿景：既拥有顶级的原生理性动态交互，又有让骇客们随时掌握全局的上帝视角。HotPlex Slack 端将成为最坚固、最优美、最透明的代码共创生态！*
