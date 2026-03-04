# 🚀 HotPlex 飞书机器人全功能手册

本手册基于 **飞书 2026 最新官方标准** 编写，旨在引导你以最专业、最快的方式完成 **HotPlex 飞书适配器** 的集成。

---

## ⚡ 快捷集成：使用应用配置清单 (App Manifest)

这是最推荐的安装方式。你无需手动点击几十个按钮，只需复制以下代码即可一键配置。

1.  访问 [飞书开发者后台](https://open.feishu.cn/app) → **创建企业自建应用** → **从模板创建**
2.  选择 **空白应用**，在应用配置页面填写以下信息：

### 基础配置

```yaml
应用名称：HotPlex
应用描述：HotPlex AI Agent - 智能助手
应用图标：上传你的 Logo（建议 512x512 PNG）
```

### 权限配置

在「权限管理」页面添加以下权限：

| 权限名称     | 权限标识                 | 用途                     |
| ------------ | ------------------------ | ------------------------ |
| 发送消息     | `im:message`             | 向用户/群组发送消息      |
| 接收消息     | `im:message.receive_v1`  | 接收用户消息事件         |
| 机器人配置   | `im:bot`                 | 配置机器人能力           |
| 发送互动消息 | `im:message:send_as_bot` | 以机器人身份发送卡片消息 |

### 命令配置

在「机器人」→「命令配置」页面添加以下命令：

| 命令     | 描述                            | 权限     |
| -------- | ------------------------------- | -------- |
| `/reset` | 重置当前会话上下文并冷启动      | 所有成员 |
| `/dc`    | 强制终止后台 CLI 进程但保留进度 | 所有成员 |

### 事件订阅配置

在「事件订阅」页面：

1.  启用「事件订阅」开关
2.  填写请求地址：`https://your-domain.com/feishu/events`
3.  订阅以下事件：
    - ✅ `im.message.receive_v1` - 接收消息

---

## 🗝️ 第一步：获取权限密钥 (Credentials)

完成上述配置后，请前往以下页面复制密钥：

| 变量名                 | 推荐格式  | 获取路径 | 作用说明                        |
| ---------------------- | --------- | -------- | ------------------------------- |
| **App ID**             | `cli_...` | 应用凭证 | 应用唯一标识                    |
| **App Secret**         | 字符串    | 应用凭证 | 应用密钥，用于获取 Access Token |
| **Verification Token** | 字符串    | 事件订阅 | 验证请求来源合法性              |
| **Encrypt Key**        | 字符串    | 事件订阅 | 消息加密密钥，必须配置          |

> [!IMPORTANT]
> **Encrypt Key 必须配置**：飞书要求所有消息必须加密传输，未配置将无法接收消息。

---

## 📡 第二步：运行模式配置

HotPlex 飞书适配器使用 **Webhook 模式**（飞书标准通信方式）：

### 配置环境变量

```bash
# 必填：飞书应用凭证
export FEISHU_APP_ID=cli_xxxxxxxxxxxxx
export FEISHU_APP_SECRET=xxxxxxxxxxxxxxxx
export FEISHU_VERIFICATION_TOKEN=xxxxxxxx
export FEISHU_ENCRYPT_KEY=xxxxxxxxxxxxxxxx

# 可选：服务器配置
export FEISHU_SERVER_ADDR=:8082
export FEISHU_MAX_MESSAGE_LEN=4096
```

### 验证部署

```bash
# 检查端点可达性
curl -X POST https://your-domain.com/feishu/events \
  -H "Content-Type: application/json" \
  -d '{"challenge": "test"}'

# 预期响应：{"challenge":"test"}
```

> [!NOTE]
> 飞书要求 Webhook 端点必须使用 **HTTPS** 协议，请确保你的域名已配置有效的 SSL 证书。

---

## ⌨️ 第三步：全场景指令 (Slash Commands)

飞书机器人支持两种触发方式：

| 场景          | 触发方式 | 说明                                  |
| ------------- | -------- | ------------------------------------- |
| **私聊/群聊** | `/reset` | 输入 `/` 会弹出自动补全，操作门槛最低 |
| **私聊/群聊** | `/dc`    | 强制中断 AI 后台执行的任务            |

> [!NOTE]
> - `/reset`：彻底销毁当前 Session 的上下文，冷启动新对话
> - `/dc`：对当前执行进程发送终止信号，暂停执行但保留进度

---

## ✨ 交互反馈：如何读懂机器人

### 1. 卡片消息 (Interactive Cards)

飞书适配器使用 **互动卡片** 提供丰富的交互体验：

| 卡片类型         | 用途             | 示例                          |
| ---------------- | ---------------- | ----------------------------- |
| **思考卡片**     | 展示 AI 推理路径 | [状态机] 正在分析您的问题...  |
| **工具调用卡片** | 展示工具执行状态 | "执行 Bash: git status"       |
| **高危拦截卡片** | WAF 拦截高危操作 | ⚠️ [危险] 发现高危指令，请审批 |
| **回答卡片**     | 展示 AI 核心回答 | 支持 Markdown 格式            |
| **错误卡片**     | 展示错误信息     | "发生错误：网络连接失败"      |
| **统计卡片**     | 展示会话统计     | Token 用量、耗时等            |

### 2. 按钮交互 (Button Actions)

卡片中的按钮支持以下操作：

- ✅ **确认操作**：批准高危工具调用
- ✅ **取消操作**：中止当前任务
- ✅ **刷新操作**：重新获取最新状态

### 3. 消息分区 (Zones)

你会看到一条消息内包含多个变动区域：

- **状态感知区**：基于 `assistant_status` 的轻量化反馈。
- **思考区**：展示推理路径（前序记录会自动折叠，仅保留首条锚点）
- **行动区**：展示 `Bash`、`FileRead` 等工具调用状态，支持 **Space Folding (空间折叠)**。
- **展示区**：AI 的核心回答，支持打字机效果流式输出

---

## ✅ 高级配置全解 (feishu.yaml)

在代码库的 `chatapps/configs/feishu.yaml` 中可进行细粒度控制：

| 参数                     | 可选值             | 说明                   |
| ------------------------ | ------------------ | ---------------------- |
| **`max_message_len`**    | 整数 (默认 4096)   | 单消息最大长度（字节） |
| **`command_rate_limit`** | 浮点数 (默认 10.0) | 命令速率限制（次/秒）  |
| **`command_rate_burst`** | 整数 (默认 20)     | 命令突发容量           |
| **`system_prompt`**      | 字符串             | 系统提示词（可选）     |

---

## 🚑 常见故障排查

### 1. 事件收不到？

**检查清单**：

- ✅ 飞书开发者后台事件订阅已启用
- ✅ Webhook URL 可公网访问（使用 ngrok 测试）
- ✅ Verification Token 配置正确
- ✅ 服务器日志无签名验证错误

**调试命令**：

```bash
# 检查端点可达性
curl -X POST https://your-domain.com/feishu/events \
  -H "Content-Type: application/json" \
  -d '{"challenge": "test"}'

# 查看服务器日志
tail -f /var/log/hotplex/feishu.log | grep -i error
```

### 2. 卡片回调不触发？

**可能原因**：

1. 卡片 action 配置错误
2. 回调 URL 未配置
3. 签名验证失败

**解决方案**：

```go
// 确保卡片 action 配置正确
card := builder.BuildAnswerCard("内容").
    AddButton("点击", "action_value").
    SetCallbackURL("/feishu/interactive")
```

### 3. Token 频繁过期？

**原因**：飞书 access_token 有效期为 2 小时

**解决方案**：

- ✅ 适配器已实现自动刷新（过期前 5 分钟）
- ✅ 检查日志确认刷新逻辑正常工作
- ✅ 如仍过期，检查系统时间是否同步

### 4. 消息发送失败？

**排查步骤**：

1. 检查错误码（参考错误码表）
2. 验证用户/群组 ID 格式
3. 确认应用权限已授予
4. 检查消息内容是否违规

**常见错误码**：

| 错误码     | 说明                       | 解决方案                            |
| ---------- | -------------------------- | ----------------------------------- |
| `99991663` | app access token invalid   | 检查 AppID/AppSecret 是否正确       |
| `99991668` | Invalid access token       | Token 过期，等待自动刷新（30 分钟） |
| `99991671` | No permission              | 检查应用权限配置，重新授权          |
| `99991664` | Invalid verification token | 检查 Verification Token 配置        |
| `99991670` | Encrypt key error          | 检查 Encrypt Key 配置               |

---

## 🎯 最佳实践

### 1. 开发环境配置

使用 **ngrok** 快速搭建本地开发环境：

```bash
# 安装 ngrok
brew install ngrok

# 启动本地服务
hotplex serve --config feishu.yaml

# 在另一个终端启动 ngrok
ngrok http 8082

# 将 ngrok 地址配置到飞书开发者后台
# https://xxxx.ngrok.io/feishu/events
```

### 2. 生产环境配置

**安全建议**：

- ✅ 使用环境变量管理敏感信息（不要硬编码）
- ✅ 配置防火墙规则，仅允许飞书 IP 访问
- ✅ 启用日志审计，记录所有命令执行
- ✅ 定期轮换 App Secret 和 Encrypt Key

**监控建议**：

- ✅ 配置 Prometheus 指标监控
- ✅ 设置错误率告警（>5% 触发）
- ✅ 监控 Token 过期时间（<1 小时告警）

---

## 📚 相关参考

- [飞书开放平台](https://open.feishu.cn/)
- [事件订阅机制](https://open.feishu.cn/document/ukTMukTMukTM/uYjNwUjL2YDM14iN2ATN)
- [消息发送 API](https://open.feishu.cn/document/ukTMukTMukTM/uYjNwUjL2YDM14iN2ATN)
- [互动卡片指南](https://open.feishu.cn/document/ukTMukTMukTM/uQjNwUjLyYDM14iO2ATN)
- [ChatApps 架构设计](./chatapps-architecture.md)
- [飞书适配器文档](../chatapps/feishu/README.md)

---

*最后更新*: 2026-03-03  
*维护者*: HotPlex Team
