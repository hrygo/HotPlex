# Webchat Cookie 认证设计规格

**日期**: 2026-06-02
**状态**: Draft
**范围**: 消除构建时 API Key 泄露，改用运行时 HMAC 签名 Cookie 认证

---

## 1. 问题

当前 webchat 的 API Key 通过 `.env.local` → `next.config.mjs` → JS bundle → `go:embed` 链路硬编码到二进制中。导致：

1. **二进制泄露密钥**：发布的 hotplex 二进制包含明文 API Key
2. **部署绑定**：不同部署需要重新构建 webchat
3. **违反 OWASP**：API Key 存储在 JS 中可被 XSS 读取

## 2. 方案

采用 HMAC-SHA256 签名 Cookie（code-server 验证模式）：

- Gateway 启动时通过 `crypto/rand` 生成 HMAC 密钥，二进制不含任何 secret
- 首次访问 webchat 时自动签发 HttpOnly Cookie
- 后续 REST/WebSocket 请求通过 Cookie 认证，无需前端携带 API Key

## 3. 认证流程

```
浏览器 GET / (无 cookie)
  → webchat handler 检测无 webchat_session cookie
  → 生成 HMAC 签名 cookie: timestamp|userID|HMAC(timestamp|userID, serverSecret)
  → Set-Cookie: webchat_session=<signed_value>; HttpOnly; SameSite=Strict; Path=/
  → 返回 index.html

浏览器后续请求 (自动携带 cookie)
  REST API: security.AuthenticateRequest 优先级: header → query → cookie
  WebSocket: hub.HandleHTTP 升级时检查 cookie → 无 cookie 则 defer to init envelope
```

## 4. 修改范围

### 4.1 Go 后端

#### `internal/security/cookie.go` (新增)

Cookie 签发和验证模块：

```go
package security

type CookieAuth struct {
    secret  []byte        // crypto/rand 生成的 HMAC 密钥
    maxAge  time.Duration // cookie 有效期，默认 24h
}

func NewCookieAuth() *CookieAuth
func (c *CookieAuth) SetCookie(w http.ResponseWriter, userID string) error
func (c *CookieAuth) Authenticate(r *http.Request) (userID string, ok bool)
```

- `SetCookie`: 生成 `timestamp|userID` payload，计算 HMAC-SHA256，Base64 编码后写入 cookie
- `Authenticate`: 解析 cookie，验证 HMAC 签名 + 检查过期时间
- Cookie 属性: `HttpOnly=true`, `SameSite=Strict`, `Path=/`, 条件性 `Secure`（仅 HTTPS）
- `Secure` flag 判断: 请求的 `r.TLS != nil` 或 `r.Header.Get("X-Forwarded-Proto") == "https"`

#### `internal/security/auth.go` (修改)

`AuthenticateRequest` 增加第三优先级 cookie 回退：

```go
func (a *Authenticator) AuthenticateRequest(r *http.Request) (string, string, error) {
    // 1. Header (现有)
    // 2. Query param (现有)
    // 3. Cookie (新增)
    if key == "" && a.cookieAuth != nil {
        if userID, ok := a.cookieAuth.Authenticate(r); ok {
            botID := BotIDFromRequest(r)
            return userID, botID, nil
        }
    }
    // 原有 unauthorized 路径
}
```

Authenticator 新增 `cookieAuth *CookieAuth` 字段，通过构造函数注入。当 `cookieAuth != nil` 时启用 cookie 认证路径。

#### `internal/gateway/hub.go` (修改)

`HandleHTTP` WS 升级时增加 cookie 认证路径：

```go
// 现有: key found → authenticate immediately
// 现有: key not found → pendingAuth = true
// 新增: key not found but cookie valid → authenticate immediately
if !found && cookieAuth != nil {
    if uid, ok := cookieAuth.Authenticate(r); ok {
        userID = uid
        botID = security.BotIDFromRequest(r)
        // pendingAuth stays false
    } else {
        pendingAuth = true
    }
}
```

#### `internal/webchat/server.go` (修改)

`Handler` 接收 `cookieAuth *security.CookieAuth` 参数。在 SPA fallback（`index.html` 响应）时：

- 检查请求是否已有有效 cookie
- 无效或不存在 → 签发新 cookie
- 已有效 → 不重复签发（避免每次刷新重置过期时间）

签名注入点在 `SPA fallback` 分支（当前第 70-73 行），因为只有浏览器直接访问页面才需要 cookie，`/_next/*` 静态资源不需要。

#### `cmd/hotplex/gateway_run.go` (修改)

- 创建 `security.NewCookieAuth()`
- 注入到 `Authenticator`、`Hub.HandleHTTP`、`webchat.Handler`
- Cookie auth 仅在 `cfg.WebChat.Enabled` 时创建

### 4.2 TypeScript 前端

#### `webchat/lib/config.ts` (修改)

移除 `apiKey` 构建时变量。新增运行时同源检测：

```typescript
// 移除:
// export const apiKey: string = process.env.HOTPLEX_WEBCHAT_API_KEY ?? "dev";

// 新增: 运行时判断是否同源部署
export function isSameOrigin(): boolean {
  return typeof window !== "undefined" && !process.env.HOTPLEX_WEBCHAT_WS_URL;
}
```

#### `webchat/lib/api/sessions.ts` (修改)

所有 `fetch` 调用移除 `X-API-Key` header，改为 `credentials: 'same-origin'`：

```typescript
// 之前: { headers: { 'X-API-Key': apiKey } }
// 之后: { credentials: 'same-origin' }
```

浏览器同源请求会自动携带 cookie，无需手动传 header。

#### `webchat/lib/adapters/hotplex-runtime-adapter.ts` (修改)

WebSocket 连接不再在 init envelope 传 `authToken`：

```typescript
// 之前: authToken: apiKey
// 之后: authToken: undefined  // cookie 已通过 WS 升级自动携带
```

#### `webchat/next.config.mjs` (可选清理)

不再需要 `HOTPLEX_WEBCHAT_API_KEY` 构建时注入。保留其他 env var 的自动转发。

### 4.3 构建配置

#### `webchat/.env.local` (删除)

不再需要 `HOTPLEX_WEBCHAT_API_KEY=hpk_...`。这个文件是泄露根源。

#### `webchat/.env.example` (修改)

移除 `HOTPLEX_WEBCHAT_API_KEY` 行，添加注释说明认证由 cookie 自动处理。

## 5. Cookie 规格

| 属性 | 值 | 说明 |
|------|-----|------|
| Name | `webchat_session` | |
| Value | Base64(timestamp\|userID\|HMAC) | |
| HttpOnly | `true` | 防 JS 读取 (XSS) |
| SameSite | `Strict` | 完全阻断 CSRF |
| Secure | 条件性 | 仅 HTTPS 时设置 |
| Path | `/` | 覆盖所有路径 |
| Max-Age | 24h | 过期后自动重新签发 |

## 6. 安全分析

### 防护矩阵

| 威胁 | 防护 |
|------|------|
| 二进制泄露 | HMAC 密钥运行时 `crypto/rand` 生成，不存在于二进制 |
| XSS 窃取 | `HttpOnly` — JS 无法读取 cookie |
| CSRF | `SameSite=Strict` — 浏览器阻断跨站请求 |
| 伪造 cookie | HMAC-SHA256 签名验证 — 无密钥无法伪造 |
| 重放攻击 | 内置 timestamp，24h 过期 |
| 中间人 | 生产环境 `Secure` flag 强制 HTTPS |

### 攻击面对比

| 攻击面 | 当前 (API Key in JS) | 改后 (HMAC Cookie) |
|--------|----------------------|---------------------|
| 二进制逆向 | 明文 Key | 无密钥 |
| XSS | 可读 Key | HttpOnly 不可读 |
| CSRF | 无防护 | SameSite=Strict |
| 网络嗅探 | Key 明文 | HttpOnly Cookie |

## 7. Dev 模式兼容

- **HTTP 开发环境**: `Secure` flag 不设置，cookie 在 HTTP 下正常工作
- **无 API Key 配置**: 现有 anonymous dev mode 保持不变（`devModeLocked` 机制）
- **Cookie + Dev Mode**: 两者独立。Cookie 解决的是"webchat 如何认证"；dev mode 解决的是"是否需要认证"

## 8. 向后兼容

### 第三方客户端（SDK/API 调用）

- 现有 `X-API-Key` header 认证**完全保留**，不受影响
- 现有 `api_key` query param 认证**完全保留**，不受影响
- Cookie 是**第三优先级回退**，仅当 header 和 query param 都为空时生效

### 外部部署的 webchat

- 设置了 `HOTPLEX_WEBCHAT_WS_URL` 的外部部署（非同源），cookie 不会发送
- 这种场景继续使用 `X-API-Key` header（通过 env var 注入）
- `webchat/lib/config.ts` 保留 `apiKey` 的 fallback 逻辑：同源用 cookie，非同源仍用 header

## 9. 测试计划

| 测试 | 类型 | 验证点 |
|------|------|--------|
| `TestCookieAuthSignVerify` | 单元 | 签名 → 验证 round-trip |
| `TestCookieAuthExpiry` | 单元 | 过期 cookie 被拒绝 |
| `TestCookieAuthTamper` | 单元 | 篡改 payload 被拒绝 |
| `TestAuthenticateRequestCookie` | 单元 | 第三优先级回退 |
| `TestWSUpgradeCookie` | 集成 | WS 升级时 cookie 认证 |
| `TestWebchatSPACookieIssuance` | 集成 | 首次访问自动签发 |
| `TestCookieSecureFlag` | 单元 | HTTP 不设 Secure，HTTPS 设 Secure |

## 10. 依赖

- 无新增外部依赖
- HMAC-SHA256 使用标准库 `crypto/hmac` + `crypto/sha256`
- 随机数使用标准库 `crypto/rand`
