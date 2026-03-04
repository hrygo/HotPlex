# 钉钉双向全双工通讯技术方案

> 📅 最后更新: 2026-03-04 | 状态: **Alpha / 技术分析中**
>
> 本文档深入分析 HotPlex 钉钉适配器的技术实现方案，包括 API 调用、认证机制、消息格式等细节。2026 年新特性（如 WAF 闭环、Status API）正在规划中。

---

## 1. 架构概述

### 1.1 通讯模型

```
┌─────────────────────────────────────────────────────────────────────┐
│                      钉钉双向通讯架构                                   │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│   用户 ──发送消息──▶ 钉钉服务器 ──HTTP POST──▶ HotPlex           │
│                        (Webhook 回调)          (handleCallback)     │
│                                                                      │
│   用户 ◀──回复消息── 钉钉服务器 ◀──HTTP POST── HotPlex          │
│                        (Robot API)            (SendMessage)         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### 1.2 当前实现状态

| 功能               | 状态     | 说明             |
| ------------------ | -------- | ---------------- |
| 消息接收 (Webhook) | ✅ 已实现 | handleCallback   |
| 消息发送 (API)     | ✅ 已实现 | SendMessage      |
| 签名验证           | ✅ 已实现 | verifySignature  |
| Access Token 管理  | ✅ 已实现 | Token 缓存与刷新 |
| Session 管理       | ✅ 已实现 | 会话创建与清理   |
| 消息分片           | ✅ 已实现 | chunkMessage     |

---

## 2. API 端点分析

### 2.1 当前使用的 API

| 用途              | API 端点                                 | 认证方式           | 代码位置        |
| ----------------- | ---------------------------------------- | ------------------ | --------------- |
| 获取 Access Token | `POST /v1.0/oauth2/oAuth2/accessToken`   | AppKey + AppSecret | dingtalk.go:352 |
| 发送消息          | `POST /v1.0/robot/oToMessages/batchSend` | Access Token       | dingtalk.go:282 |

### 2.2 Access Token 获取

```go
// 代码位置: dingtalk.go:337-377
func (a *DingTalkAdapter) getAccessToken() (string, error) {
    url := fmt.Sprintf("https://api.dingtalk.com/v1.0/oauth2/oAuth2/accessToken?appKey=%s&appSecret=%s",
        a.config.AppID, a.config.AppSecret)
    
    resp, err := http.Get(url)
    // 解析 JSON 响应
    // 返回 accessToken
}
```

**请求参数**:
- `appKey`: 企业应用 AppKey
- `appSecret`: 企业应用 AppSecret

**响应格式**:
```json
{
    "accessToken": "xxx",
    "expireIn": 7200
}
```

**Token 刷新逻辑**:
- 有效期: 7200 秒 (2 小时)
- 缓存策略: 提前 5 分钟刷新
- 代码实现: `dingtalk.go:343-348`

### 2.3 消息发送 API

```go
// 代码位置: dingtalk.go:282
url := fmt.Sprintf("https://api.dingtalk.com/v1.0/robot/oToMessages/batchSend?robotCode=%s", 
    msg.Metadata["robot_code"])
```

**请求头**:
```
Content-Type: application/json
x-acs-dingtalk-access-token: {accessToken}
```

**消息体格式**:

1. 文本消息:
```json
{
    "msgtype": "text",
    "text": {
        "content": "消息内容"
    }
}
```

2. Markdown 消息:
```json
{
    "msgtype": "markdown",
    "markdown": {
        "title": "标题",
        "text": "## Markdown 内容\n- 项目1\n- 项目2"
    }
}
```

---

## 3. 消息接收 (Webhook)

### 3.1 回调验证

```go
// 代码位置: dingtalk.go:162-180
func (a *DingTalkAdapter) handleCallbackVerify(w http.ResponseWriter, r *http.Request) {
    signature := r.URL.Query().Get("signature")
    timestamp := r.URL.Query().Get("timestamp")
    nonce := r.URL.Query().Get("nonce")
    
    // 验证签名
    if !a.verifySignature(signature, timestamp, nonce, a.config.CallbackToken) {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
        return
    }
    
    w.WriteHeader(http.StatusOK)
    _, _ = fmt.Fprint(w, timestamp)
}
```

### 3.2 签名算法

```go
// 代码位置: dingtalk.go:379-385
func (a *DingTalkAdapter) verifySignature(signature, timestamp, nonce, token string) bool {
    stringToSign := timestamp + token + nonce
    mac := hmac.New(sha256.New, []byte(token))
    mac.Write([]byte(stringToSign))
    sign := base64.StdEncoding.EncodeToString(mac.Sum(nil))
    return sign == signature
}
```

**签名算法**:
1. 拼接字符串: `timestamp + token + nonce`
2. 使用 `token` (CallbackToken) 作为密钥进行 HMAC-SHA256 签名
3. Base64 编码签名结果
4. 与传入的 `signature` 比对

### 3.3 消息解析

```go
// 代码位置: dingtalk.go:182-230
type DingTalkCallbackRequest struct {
    MsgType        string `json:"msgtype"`
    ConversationID string `json:"conversationId"`
    SenderID       string `json:"senderId"`
    SenderNick     string `json:"senderNick"`
    IsAdmin        bool   `json:"isAdmin"`
    RobotCode      string `json:"robotCode"`
    Text           struct {
        Content string `json:"content"`
    } `json:"text"`
    EventType string `json:"eventType"`
}
```

**关键字段映射**:

| 回调字段         | 用途       | 保存位置                               |
| ---------------- | ---------- | -------------------------------------- |
| `senderId`       | 用户 ID    | `msg.UserID`                           |
| `conversationId` | 会话 ID    | `msg.Metadata.conversation_id`         |
| `robotCode`      | 机器人编码 | `msg.Metadata.robot_code` (发送时使用) |
| `text.content`   | 消息内容   | `msg.Content`                          |

---

## 4. Session 管理

### 4.1 Session 结构

```go
type DingTalkSession struct {
    SessionID  string
    UserID     string
    Platform   string
    LastActive time.Time
}
```

### 4.2 Session 映射

```go
// 代码位置: dingtalk.go:315-335
func (a *DingTalkAdapter) getOrCreateSession(userID, conversationID string) string {
    key := conversationID + ":" + userID
    // 如果会话已存在，更新 LastActive
    // 否则创建新会话
}
```

**Session Key 规则**: `{conversationId}:{senderId}`

### 4.3 过期清理

```go
// 代码位置: dingtalk.go:442-464
func (a *DingTalkAdapter) cleanupSessions() {
    ticker := time.NewTicker(5 * time.Minute)
    for {
        select {
        case <-a.cleanupDone:
            return
        case <-ticker.C:
            // 清理超过 30 分钟的会话
        }
    }
}
```

- 清理周期: 5 分钟
- 会话超时: 30 分钟

---

## 5. 消息分片机制

### 5.1 分片策略

```go
// 代码位置: dingtalk.go:389-440
func (a *DingTalkAdapter) chunkMessage(content string) []string {
    maxLen := 5000 // 钉钉限制
    
    // 1. 优先按行分割
    // 2. 单行超长则按字符分割
    // 3. 合并到当前分片或创建新分片
}
```

### 5.2 分片格式

当消息被分片时，每片添加编号前缀:
```text
[1/3]
第一部分内容...

[2/3]
第二部分内容...

[3/3]
第三部分内容...
```

---

## 6. 认证机制详解

### 6.1 配置参数

```go
type DingTalkConfig struct {
    AppID         string  // AppKey (dingxxxx)
    AppSecret     string  // AppSecret
    CallbackURL   string  // 回调地址 (可选)
    CallbackToken string  // 回调验证 Token
    CallbackKey   string  // 回调加解密密钥 (可选)
    ServerAddr    string  // HTTP 服务地址
    MaxMessageLen int     // 消息长度限制
    SystemPrompt  string  // 系统提示词
}
```

### 6.2 认证流程

```
┌─────────────────────────────────────────────────────────────────────┐
│                        认证流程                                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  1. 应用启动                                                         │
│     │                                                               │
│     ▼                                                               │
│  2. 加载配置 (AppID, AppSecret, CallbackToken)                       │
│     │                                                               │
│     ▼                                                               │
│  3. 首次调用 API                                                     │
│     │                                                               │
│     ▼                                                               │
│  4. 调用 /oauth2/accessToken 获取 Access Token                        │
│     │                                                               │
│     ▼                                                               │
│  5. 缓存 Token (有效期 - 5 分钟)                                     │
│     │                                                               │
│     ▼                                                               │
│  6. 后续请求使用缓存的 Token                                          │
│     │                                                               │
│     ▼                                                               │
│  7. Token 过期前自动刷新                                             │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

---

## 7. 潜在问题与改进建议

### 7.1 当前限制

| 问题                           | 严重程度 | 说明                           |
| ------------------------------ | -------- | ------------------------------ |
| 仅支持 text 消息接收           | 中       | 无法接收图片、文件等           |
| markdown 发送需要 rich content | 低       | 需要正确设置 RichContent       |
| robotCode 依赖配置正确         | 高       | 必须确保消息能获取到 robotCode |

### 7.2 改进建议

1. **消息类型扩展**
   - 支持接收 image/file/voice 消息
   - 实现消息下载与转发

2. **Rich Content 支持**
   - 完善 actionCard 消息类型
   - 支持按钮交互

3. **错误处理**
   - 添加重试机制
   - 完善错误日志

---

## 8. 测试验证清单

### 8.1 配置验证

- [ ] AppKey 和 AppSecret 正确配置
- [ ] CallbackToken 配置正确 (用于签名验证)
- [ ] ServerAddr 可公网访问

### 8.2 功能验证

- [ ] 接收文本消息正常
- [ ] 发送文本消息正常
- [ ] 发送 Markdown 消息正常
- [ ] 长消息自动分片
- [ ] Session 隔离正确

### 8.3 异常处理

- [ ] Token 自动刷新
- [ ] 网络错误重试
- [ ] 签名验证失败处理

---

## 9. 参考资料

- [钉钉开放平台](https://open.dingtalk.com)
- [企业机器人开发文档](https://developers.dingtalk.com/document/app/send-messages)
- [回调事件文档](https://developers.dingtalk.com/document/app/callback-events)

---

*本文档最后更新: 2026-02-26*
