---

---

# Security: Authentication & Authorization Design

> HotPlex WebSocket 认证授权设计，基于 API Key + Bot ID 模型。

---

## 1. 设计原则

### 1.1 核心决策

| 决策 | 方案 | 说明 |
|------|------|------|
| 认证方式 | API Key | 简单、无状态、适合 WebSocket 长连接 |
| Bot 隔离 | `X-Bot-ID` Header | 请求级隔离，每个 Bot 只能操作自己的 Session |
| 多用户映射 | `APIKeyResolver` | 可选，将 API Key 映射到不同 userID |
| 生产要求 | 至少配置一个 API Key | 未配置时自动降级为 `anonymous`（开发模式） |

### 1.2 设计优势

- **简洁**：无需签名/验证 JWT，减少攻击面
- **无状态**：API Key 在内存 map 中验证，支持热重载
- **无损轮转**：编号式环境变量（`_1`、`_2`）支持在线替换
- **灵活**：`APIKeyResolver` 可按需启用多用户隔离

---

## 2. API Key 认证

### 2.1 密钥传递方式

`internal/security/auth.go` 提供双层 Key 提取：

1. **HTTP Header**（`X-API-Key`，可自定义）
2. **Query Parameter**（`api_key`，浏览器 WebSocket 客户端专用）

```bash
# 方式 1：HTTP Header（推荐，CLI/服务端）
curl -i --no-buffer \
  -H "X-API-Key: your-api-key" \
  -H "X-Bot-ID: your-bot-id" \
  -H "Upgrade: websocket" \
  -H "Connection: Upgrade" \
  http://localhost:8080/ws

# 方式 2：Query Param（浏览器 WS 客户端）
ws://localhost:8080/ws?api_key=your-api-key&bot_id=your-bot-id
```

### 2.2 密钥配置

```yaml
security:
  api_keys:
    - "ak-xxxxx"
    - "ak-yyyyy"
```

或通过环境变量（编号式，支持无损轮转）：

```bash
HOTPLEX_SECURITY_API_KEY_1=ak-xxxxx
HOTPLEX_SECURITY_API_KEY_2=ak-yyyyy
```

**零密钥 = 开发模式**：未配置 API Key 时自动降级为 `anonymous` 用户，**生产环境必须配置至少一个 Key**。

### 2.3 热重载

`Authenticator.ReloadKeys()` 支持运行时更新密钥列表，无需重启：

```bash
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/config/reload
```

---

## 3. Bot ID 隔离

### 3.1 传递方式

Bot ID 通过以下方式传递：

1. **HTTP Header**：`X-Bot-ID: your-bot-id`
2. **Query Parameter**：`?bot_id=your-bot-id`
3. **init 信封**：`data.bot_id` 字段

```json
{
  "event": {
    "type": "init",
    "data": {
      "version": "aep/v1",
      "worker_type": "claude_code",
      "auth": { "token": "your-api-key" },
      "bot_id": "B12345"
    }
  }
}
```

### 3.2 提取方式

```go
botID := security.BotIDFromRequest(r)
```

### 3.3 隔离规则

- `bot_id` 必须与 Session 所属 Bot **精确匹配**
- 跨 Bot 操作被硬拒绝，返回 `403 Forbidden`
- 未指定 `bot_id` 时不执行 Bot 级隔离检查

---

## 4. APIKeyResolver（多用户映射）

### 4.1 默认行为

未设置 `APIKeyResolver` 时，所有 API Key 认证的请求统一使用 `api_user` 身份：

```
所有 API Key → api_user → Session ID 派生不区分用户
```

### 4.2 自定义 Resolver

通过 `security.SetKeyResolver()` 设置自定义映射：

```go
security.SetKeyResolver(func(key string) (userID string, ok bool) {
    // 从数据库/配置/API 查询 key 对应的 userID
    userID, err := db.ResolveKeyToUser(key)
    if err != nil {
        return "", false
    }
    return userID, true
})
```

设置后，不同 API Key 可映射到不同 userID，实现用户级会话隔离：

```
key: ak-alice → resolver → userID: "alice" → Session A
key: ak-bob   → resolver → userID: "bob"   → Session B
```

`ListSessions` API 按 userID 过滤，每个用户只看到自己的会话。

---

## 5. WebSocket 认证流程

### 5.1 双保险认证机制

```
┌─────────────────────────────────────────────────────────────┐
│                  WebSocket Authentication                     │
│                                                              │
│  1. 握手阶段 (Handshake)                                     │
│     Client ──── X-API-Key Header ────► Gateway               │
│                    │                                         │
│                    ▼                                         │
│     API Key 无效 ──► 401 Unauthorized                        │
│                    │                                         │
│                    ▼                                         │
│     API Key 有效 ──► 允许 Upgrade (101 Switching Protocols)   │
│                                                              │
│  2. 首条消息认证 (First Message)                              │
│     Client ──── init {auth.token} ────► Gateway               │
│                    │                                         │
│                    ▼                                         │
│     验证失败 ──────► error + WS Close (1008)                 │
│                    │                                         │
│                    ▼                                         │
│     验证成功 ──────► 绑定 userID + botID + session_id        │
│                                                              │
│  3. 消息循环 (Message Loop)                                  │
│     Client ──── Envelope ──► Gateway                         │
│                    │                                         │
│                    ▼                                         │
│     session_id 一致性验证 ──► 处理事件                       │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 握手阶段认证

**方案 A：API Key Header（CLI/Desktop）**

```http
GET /gateway HTTP/1.1
Host: hotplex.example.com
Upgrade: websocket
Connection: Upgrade
X-API-Key: ak-xxx
X-Bot-ID: bot-123
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
```

**方案 B：Query Parameter（浏览器 WebSocket）**

```http
GET /gateway?api_key=ak-xxx&bot_id=bot-123 HTTP/1.1
Host: hotplex.example.com
Upgrade: websocket
Connection: Upgrade
Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
Sec-WebSocket-Version: 13
```

### 5.3 首条消息认证（init.envelope）

**API Key 嵌入 Envelope**：

```json
{
  "version": "aep/v1",
  "kind": "init",
  "data": {
    "protocol_version": "aep/v1",
    "client_caps": ["streaming", "tools"],
    "auth": {
      "token": "your-api-key"
    },
    "bot_id": "bot-123"
  }
}
```

**验证流程**：

```go
func (g *Gateway) HandleInit(env *Envelope) (*Envelope, error) {
    // 1. 提取 API Key
    tokenStr := env.Data["auth"].(map[string]interface{})["token"].(string)

    // 2. 验证 API Key
    userID, ok := g.auth.Authenticate(tokenStr)
    if !ok {
        return nil, &AuthError{Code: "AUTHENTICATION_FAILED", Reason: "invalid API key"}
    }

    // 3. 使用 APIKeyResolver 映射用户身份（可选）
    if resolver != nil {
        if resolvedID, ok := resolver(tokenStr); ok {
            userID = resolvedID
        }
    }

    // 4. 提取 Bot ID
    botID := env.Data["bot_id"].(string)

    // 5. 创建 Session
    session := sm.CreateSession(userID, botID)

    return &Envelope{
        Kind: "init_ack",
        Data: map[string]interface{}{
            "session_id": session.ID,
            "server_caps": g.getServerCaps(),
        },
    }, nil
}
```

---

## 6. Session Ownership 验证

### 6.1 Session 绑定

```go
type Session struct {
    ID        string
    OwnerID   string    // userID（来自 APIKeyResolver 或默认 api_user）
    BotID     string    // bot_id（来自 X-Bot-ID 或 init data）
    State     SessionState
    CreatedAt int64
}
```

### 6.2 Ownership 验证流程

```go
func (sm *SessionManager) ValidateOwnership(sessionID, userID string) error {
    session, err := sm.GetSession(sessionID)
    if err != nil {
        return ErrSessionNotFound
    }

    if session.OwnerID != userID {
        // 记录安全日志
        log.Warn("session ownership mismatch",
            "session_id", sessionID,
            "expected_owner", session.OwnerID,
            "actual_owner", userID,
        )
        return ErrSessionOwnershipMismatch
    }

    return nil
}
```

### 6.3 Admin API 权限矩阵

| 端点 | Required Scope | 说明 |
|------|----------------|------|
| `GET /admin/sessions` | `admin:read` | 列出所有 session |
| `DELETE /admin/sessions/{id}` | `admin:delete` | 强制终止 |
| `GET /admin/stats` | `admin:read` | 统计信息 |
| `GET /admin/metrics` | `admin:read` | Prometheus metrics |

---

## 7. 密钥轮转

### 7.1 API Key 无损轮转

编号式环境变量支持在线替换：

```bash
# 1. 添加新密钥
export HOTPLEX_SECURITY_API_KEY_2="ak-new-key"

# 2. 所有客户端切换到新密钥

# 3. 移除旧密钥
unset HOTPLEX_SECURITY_API_KEY_1
```

### 7.2 Admin Token 轮转（零停机）

```bash
# 1. 生成新 Token
NEW_TOKEN=$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)

# 2. 更新 _2（保留 _1）
export HOTPLEX_ADMIN_TOKEN_2="$NEW_TOKEN"

# 3. 所有客户端切换到 _2

# 4. 更新 _1 为新 Token，清除旧 _2
```

---

## 8. 多 Bot 隔离方案

### 8.1 决策：API Key + X-Bot-ID Header

> **采用 API Key + Bot ID 隔离**，简化密钥分发运维。

```go
// Session 包含 BotID，Gateway 按 BotID 路由
type Session struct {
    ID        string
    OwnerID   string  // userID（来自 APIKeyResolver 或默认 api_user）
    BotID     string  // bot_id（来自 X-Bot-ID Header 或 init data）
}
```

**为何不用 JWT**：
- JWT 增加了签名/验证复杂度和攻击面
- API Key + Bot ID 在内部服务间足够安全
- 无状态验证，无需密钥轮转机制
- 编号式环境变量原生支持无损轮转

---

## 9. 安全检查清单

- 生产环境至少配置一个 API Key
- 配置 `X-Bot-ID` 实现多 Bot 隔离
- 使用 `APIKeyResolver` 实现多用户隔离（可选）
- WebSocket 握手阶段 Header/Query 认证
- init.envelope 中 API Key 验证
- Session Ownership 绑定
- 编号式环境变量支持密钥轮转
- TLS 强制（生产环境）
- 安全日志（认证失败、Ownership 不匹配）
