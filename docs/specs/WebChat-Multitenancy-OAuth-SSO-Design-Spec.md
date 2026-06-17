# WebChat 多租户 spec ④：企业 SSO（OIDC 统一认证）

**日期**: 2026-06-17
**状态**: 设计稿（待 plan）
**分支**: feat/webchat-oauth-sso · **基线**: v1.29.1（`5bc8c0ce`，含 spec ⑤ PR #755）
**路线图**: [`WebChat-Multitenancy-Roadmap-Spec.md`](./WebChat-Multitenancy-Roadmap-Spec.md) §4 spec ④
**前置**: spec ①（PR #746，User 实体 + IdentityProvider 接口 + CookieAuth + auth_handlers）

---

## 目录

- [1. 背景与调研](#1-背景与调研)
- [2. 目标与非目标](#2-目标与非目标)
- [3. 关键决策](#3-关键决策)
- [4. 架构总览](#4-架构总览)
- [5. 数据模型](#5-数据模型)
- [6. 配置模型](#6-配置模型)
- [7. OIDC 流程详解](#7-oidc-流程详解)
- [8. 账号关联策略](#8-账号关联策略)
- [9. API 端点清单](#9-api-端点清单)
- [10. 安全设计](#10-安全设计)
- [11. 错误码](#11-错误码)
- [12. 测试策略](#12-测试策略)
- [13. 迁移策略](#13-迁移策略)
- [14. 后续 spec 路线](#14-后续-spec-路线)

---

## 1. 背景与调研

### 1.1 WebChat 的 SSO 需求本质

WebChat 是团队共享一个 HotPlex 实例的多用户前端。spec ① 已建立账号密码登录（`LocalAccountProvider` + bcrypt + 邀请制）。但企业内部通常已有统一身份认证（SSO）基础设施，要求所有应用通过企业 IdP 登录，而非在各应用中维护独立密码。

spec ④ 的目标：让 WebChat 接入企业现有 SSO，员工用企业 IdP 账号登录，映射到 HotPlex `users.id`。**飞书/Slack 用户不需要 WebChat SSO**（它们在各自平台用 Message Channel 轨），SSO 纯粹是企业统一身份认证集成。

### 1.2 国内主流统一认证厂商协议调研

| 厂商 | 产品 | OIDC | OAuth2 | SAML | CAS | LDAP |
|---|---|:---:|:---:|:---:|:---:|:---:|
| 派拉 | 统一身份认证 / IDaaS | ✅ | ✅ | ✅ | ✅ | ✅ |
| 玉符（腾讯） | IDaaS | ✅ | ✅ | ✅ | ✅ | ✅ |
| 阿里云 | IDaaS EIAM | ✅ | ✅(OIDC 子集) | ✅ | — | ✅ |
| 腾讯云 | Cloud IDaaS | ✅ | ✅ | ✅ | — | ✅ |
| 宁盾 | IAM / SSO | ✅ | ✅ | ✅ | ✅ | ✅ |
| Authing | 身份云 | ✅ | ✅ | ✅ | ✅ | ✅ |
| 竹云 | IDaaS | ✅ | ✅ | ✅ | ✅ | ✅ |
| 华为云 | OneAccess | ✅ | ✅ | ✅ | ✅ | ✅ |
| TOPIAM（开源） | SSO 平台 | ✅ | ✅ | ✅ | ✅ | — |

**关键结论**：**OIDC 是所有国内主流厂商的最大公约数**，100% 覆盖。一个标准 OIDC 客户端实现即可对接全部上述厂商 + 国际 IdP（Keycloak / Okta / Azure AD / Google Workspace）。

### 1.3 协议优先级

| 协议 | 厂商覆盖率 | 实现成本 | 本 spec 决策 |
|---|---|---|---|
| **OIDC** | 100% | 低 | ✅ **第一期实现** |
| SAML 2.0 | ~85% | 高（XML 签名、SP/IdP 元数据、双重流程） | 后续 spec（YAGNI，验证实际需求后做） |
| CAS | ~50%（高校/老政企） | 中 | 不做（用户可 CAS→OIDC 桥接） |

---

## 2. 目标与非目标

### 2.1 目标

1. **标准 OIDC 客户端** — 一个实现覆盖所有标准 OIDC IdP（国内全部主流厂商 + 国际）
2. **多 provider 配置** — 支持配置多个 IdP（如同时对接测试/生产 Keycloak），用户在登录页选择
3. **与密码登录并存** — OIDC SSO 和 spec ① 的账号密码登录都是一等公民，企业可选其一或同时启用
4. **首次登录自动建号** — SSO 认证成功且本地无对应用户时，自动创建 `users` 行
5. **安全合规** — Authorization Code flow + PKCE + state 防 CSRF

### 2.2 非目标（明确不做）

- **SAML 2.0** — 后续独立 spec（XML 协议栈重量级，Go 生态 SAML 库不如 OIDC 成熟）
- **CAS 协议** — YAGNI，用户可部署 CAS→OIDC 桥接
- **飞书/Slack OAuth 复用** — 它们是 Message Channel，与 WebChat SSO 无关
- **账号自动合并** — 不基于 email 自动关联已有密码账号（欺骗风险），见 §8
- **多 identity 显式绑定 UI** — 首期不做链接/解绑 UI，见 §8
- **WebChat 前端登录页** — 归 spec ⑥（本 spec 交付后端 + API，HTTP 可验证）
- **Token 持久化** — 不存 access_token/refresh_token（HotPlex 只需登录时身份确认，不做 IdP API 代理）

---

## 3. 关键决策

### 3.1 协议：OIDC Authorization Code flow + PKCE

| 决策 | 选择 | 理由 |
|---|---|---|
| 协议族 | OIDC（非裸 OAuth2） | OIDC 提供标准化身份层（ID Token + UserInfo），覆盖全部厂商 |
| 流程 | Authorization Code flow | 最安全的服务端流程；token 不经过浏览器 |
| PKCE | 强制启用 | 防 authorization code 拦截（即使 server-side confidential client 也无成本） |
| Discovery | `.well-known/openid-configuration` 自动发现 | 配置只需 `issuer` URL，不需手填每个 endpoint |
| Token 验证 | ID Token signature 验证（JWKS） | 确保 token 来自可信 IdP，防伪造 |

### 3.2 账号关联：不自动合并

首次 SSO 登录 → 按 `(provider, subject)` 查找 → 不存在则自动建号。不基于 email 自动关联已有密码账号（email 欺骗风险）。admin 可后续手动绑定（待独立 spec）。

### 3.3 配置位置：独立 `oauth` 段

不复用 bot 配置（飞书/Slack 的 `app_id`/`app_secret` 是 Message Channel 用的，语义不同）。独立 `oauth.providers[]` 配置段，与 bot 配置零耦合。

### 3.4 前端：spec ④ 不含 UI

与路线图一致。本 spec 交付后端 OIDC 客户端 + API 端点，用 HTTP/curl 端到端验证。前端登录页（含 provider 选择、SSO 入口、callback 落地页）归 spec ⑥。

---

## 4. 架构总览

```
用户浏览器                    HotPlex Gateway                      企业 IdP
    |                              |                                  |
    |  1. 点击 "SSO登录"            |                                  |
    |  GET /api/auth/oauth/{p}/login                                  |
    |----------------------------->|                                  |
    |                              |  2. 生成 state+PKCE              |
    |                              |  3. 302 重定向到 IdP             |
    |<-----------------------------|                                  |
    |                              |                                  |
    |  4. 用户在 IdP 登录          |                                  |
    |----------------------------------------------------->|         |
    |                              |                     5. IdP 认证  |
    |<-----------------------------------------------------|         |
    |  6. 302 回调到 HotPlex       |                                  |
    |  GET /api/auth/oauth/{p}/callback?code=...&state=...            |
    |----------------------------->|                                  |
    |                              |  7. 校验 state                   |
    |                              |  8. code → token exchange        |
    |                              |  9. 验证 ID Token signature      |
    |                              |  10. 提取 subject + claims       |
    |                              |  11. 查找/创建 users 行          |
    |                              |  12. 签发 cookie                 |
    |  13. 302 回 webchat 首页     |                                  |
    |<-----------------------------|                                  |
```

**新增组件**：

| 组件 | 位置 | 职责 |
|---|---|---|
| `OAuthProvider` | `internal/security/oauth_provider.go` | 实现 `IdentityProvider`，OIDC 客户端 |
| `OAuthManager` | `internal/security/oauth_manager.go` | 多 provider 注册表 + state 管理 |
| `OAuthHandlers` | `internal/gateway/oauth_handlers.go` | HTTP 端点（login / callback） |
| `OAuthConfig` | `internal/config/config_types.go` | `oauth.providers[]` 配置解析 |
| migration 020 | `sql/migrations/` + `sql/migrations-postgres/` | `user_identities` 表 |

**已有组件（复用）**：

| 组件 | 复用点 |
|---|---|
| `IdentityProvider` 接口 | `OAuthProvider` 作为第二实现，不改接口 |
| `CookieAuth` | SSO 成功后签发 cookie，与密码登录同一 cookie |
| `AuthHandlers` | `Login`/`Logout`/`Me` 已实现，`OAuthManager` 注入后 `Login` 可增加 SSO 入口返回 |
| `UserStore` 接口 | 新增 `GetOrCreateUserByIdentity` 方法 |

---

## 5. 数据模型

### 5.1 新增表：`user_identities`（migration 020）

将 OAuth 身份与 `users` 解耦——一个用户可关联多个 IdP（未来），`users` 表不污染 OAuth 字段：

```sql
CREATE TABLE user_identities (
    id              TEXT PRIMARY KEY,          -- UUID
    user_id         TEXT NOT NULL,             -- FK → users.id
    provider        TEXT NOT NULL,             -- provider name (config key)
    subject         TEXT NOT NULL,             -- IdP subject (OIDC "sub" claim)
    display_name    TEXT NOT NULL DEFAULT '',  -- 从 IdP 同步
    email           TEXT NOT NULL DEFAULT '',  -- 从 IdP 同步（仅记录，不用于自动合并）
    created_at      INTEGER NOT NULL,          -- Unix epoch seconds
    updated_at      INTEGER NOT NULL,
    UNIQUE(provider, subject)                  -- 一个 IdP+subject 只映射一个 user
);

CREATE INDEX idx_user_identities_user_id ON user_identities(user_id);
CREATE INDEX idx_user_identities_lookup ON user_identities(provider, subject);
```

**设计理由**：
- **独立表 vs `users` 加字段**：独立表支持未来多 IdP 关联（一用户绑多个 SSO），且不污染 `users` 表（密码账号无 provider/subject 概念）。`UNIQUE(provider, subject)` 保证登录确定性。
- **`subject` 而非 `email` 作为唯一键**：OIDC `sub` claim 是 IdP 内全局唯一且不可变的用户标识，email 可变且可重复。
- **`email` 存储但不用于合并**：纯展示用途，不参与自动关联逻辑。

### 5.2 `users` 表不变

`users` 表不增加任何字段。SSO 建号时：
- `username` = `{provider}:{subject}`（保证唯一且可追溯；非登录用）
- `password_hash` = `''`（空，表示此账号只能通过 SSO 登录，不能密码登录——复用 spec ① 已有的"空 hash = 不可密码登录"语义）
- `role` = `user`（SSO 默认普通用户；admin 通过 admin CLI/界面提升）
- `status` = `active`

---

## 6. 配置模型

### 6.1 YAML 配置

```yaml
oauth:
  # 外部基础 URL（用于构造 OAuth callback URL）。
  # 不配置时从请求 Host header 推导（同源场景）。
  # 反向代理后端或多 URL 场景必须显式配置。
  external_url: "https://hotplex.example.com"

  providers:
    - name: "keycloak"              # 唯一标识，出现在 URL 路径和 user_identities.provider
      display_name: "企业 SSO"      # 登录页展示名（spec ⑥ 用）
      issuer: "https://sso.example.com/realms/main"
      client_id: "hotplex"
      client_secret: "${OAUTH_KEYCLOAK_SECRET}"  # 支持 env var 引用
      scopes: ["openid", "profile", "email"]     # 默认 ["openid", "profile"]
      # 可选 claim 映射（不配则用 OIDC 标准 claim 名）
      username_claim: "preferred_username"
      display_name_claim: "name"
      email_claim: "email"

    - name: "authing"
      display_name: "Authing"
      issuer: "https://xxx.authing.cn"
      client_id: "hotplex"
      client_secret: "${OAUTH_AUTHING_SECRET}"
```

### 6.2 配置规则

| 规则 | 说明 |
|---|---|
| `name` | 必填，唯一，`[a-z0-9-]+`，用于 URL 路径（`/api/auth/oauth/{name}/login`） |
| `issuer` | 必填，OIDC issuer URL（自动发现 `.well-known/openid-configuration`） |
| `client_id` + `client_secret` | 必填，confidential client 凭证 |
| `scopes` | 可选，默认 `["openid", "profile"]` |
| `*_claim` | 可选，自定义 claim 映射；不配则用 OIDC 标准名 |
| `external_url` | 可选，全局配置（非 per-provider），构造 callback URL |
| env 引用 | `client_secret` 支持 `${ENV_VAR}` 语法（与现有 config env 引用一致） |

### 6.3 热重载

Provider 列表支持运行时热重载（复用现有 `ConfigStore` watcher）。变更时重建 `OAuthManager` 的 provider 注册表，不影响已进行中的 OAuth 流程（state cookie 已编码 provider name，回调时从注册表查找）。

---

## 7. OIDC 流程详解

### 7.1 login 端点（GET `/api/auth/oauth/{provider}/login`）

```
1. 从 URL path 提取 provider name
2. OAuthManager.Lookup(provider) → OAuthProvider 实例
   - 不存在 → 404
3. 生成 state（32 字节随机 hex）和 PKCE code_verifier（64 字节随机 hex）
4. 计算 code_challenge = S256(code_verifier)
5. state → 短期 cookie（5 分钟 TTL，HttpOnly，SameSite=Lax）
   cookie 值 = Base64(state|code_verifier|provider) — 无需服务端存储
6. 构造 authorization URL：
   {issuer_auth_endpoint}?
     response_type=code
     &client_id={client_id}
     &redirect_uri={external_url}/api/auth/oauth/{provider}/callback
     &scope={space_joined_scopes}
     &state={state}
     &code_challenge={code_challenge}
     &code_challenge_method=S256
7. 302 重定向到 authorization URL
```

**state cookie 设计**（无状态）：
- 名称：`oauth_state`
- 值：HMAC 签名的 `Base64(state|code_verifier|provider|issuedAt)`
- TTL：5 分钟
- SameSite=Lax（允许从 IdP 重定向回来）
- 使用 CookieAuth 的 HMAC secret 签名，防篡改

### 7.2 callback 端点（GET `/api/auth/oauth/{provider}/callback`）

```
1. 从 URL path 提取 provider name
2. 从 query 提取 code + state
3. 读取 oauth_state cookie → 解签 → 校验 state 匹配
   - 不匹配 → 400 CSRF_DETECTED
   - cookie 过期 → 400 STATE_EXPIRED
   - cookie 中 provider 与 path provider 不匹配 → 400 PROVIDER_MISMATCH
4. OAuthManager.Lookup(provider) → OAuthProvider
5. code → token exchange（IdP token endpoint）：
     POST {issuer_token_endpoint}
       grant_type=authorization_code
       &code={code}
       &redirect_uri={...}
       &client_id={...}
       &client_secret={...}
       &code_verifier={code_verifier}
   → 返回 {access_token, id_token, ...}
6. 验证 ID Token：
   a. 从 IdP JWKS endpoint 获取签名密钥
   b. 验证 RS256/ES256 签名
   c. 验证 iss == configured issuer
   d. 验证 aud == client_id
   e. 验证 exp 未过期
7. 提取 claims：sub, username, display_name, email
8. GetOrCreateUserByIdentity(provider, sub, ...) → user_id
9. 签发 webchat_session cookie（同密码登录）
10. 清除 oauth_state cookie
11. 302 重定向到 webchat 首页（`/`）
```

### 7.3 Go 库选择

| 库 | 用途 | 状态 |
|---|---|---|
| `golang.org/x/oauth2` | OAuth2 底层（token exchange、HTTP 客户端） | 已在 go.mod（indirect → 提升为 direct） |
| `github.com/coreos/go-oidc/v3` | OIDC 层（discovery、ID Token 验证、JWKS 缓存） | 需引入 |
| `github.com/go-jose/go-jose/v4` | JOSE（JWT/JWKS，被 go-oidc 依赖） | 传递依赖 |

`go-oidc/v3` 的 `oidc.Provider` 自动从 `.well-known/openid-configuration` 发现 endpoints，`oidc.IDTokenVerifier` 自动验证签名 + 标准 claims（iss/aud/exp），`oidc.UserInfo` 获取用户信息——覆盖全部需求，无需手写 JWT 验证逻辑。

---

## 8. 账号关联策略

### 8.1 首次登录自动建号

```
SSO callback 认证成功
  → GetOrCreateUserByIdentity(provider, subject)
    → 查 user_identities WHERE provider=? AND subject=?
      → 命中：返回 user_id
      → 未命中：
        1. 创建 users 行（username="{provider}:{subject}", password_hash="", role="user", status="active"）
        2. 创建 user_identities 行（provider, subject, user_id, display_name, email）
        3. 返回 user_id
```

**事务**：users + user_identities 在同一事务内创建，保证一致性。

**display_name / email 更新**：每次 SSO 登录时，如果 IdP 返回的 display_name/email 与本地不同，更新 `user_identities` 行（IdP 是权威源）。`users.username` 不更新（避免 session key 漂移）。

### 8.2 不自动合并

| 场景 | 行为 |
|---|---|
| 同一 IdP 同一 subject 首次登录 | 自动建号 |
| 同一 IdP 同一 subject 再次登录 | 复用已有 user_id |
| 同一人换了 IdP（如从 Keycloak 迁到 Authing） | 新建 user_id（两个账号） |
| SSO 账号的 email 与某密码账号的 email 相同 | **不合并**（各自独立 user_id） |

### 8.3 未来：手动绑定（不在本 spec 范围）

admin/用户自行将 SSO 身份关联到已有账号的 UI 流程，作为独立 spec（spec ④.1 或路线图新增项）。本 spec 只做自动建号。

---

## 9. API 端点清单

### 9.1 新增端点

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/auth/oauth/{provider}/login` | 发起 OIDC 流程，302 重定向到 IdP |
| GET | `/api/auth/oauth/{provider}/callback` | OIDC 回调，签发 cookie，302 回 webchat |
| GET | `/api/auth/oauth/providers` | 列出已配置的 provider（供前端登录页渲染按钮） |

### 9.2 现有端点变更

| 方法 | 路径 | 变更 |
|---|---|---|
| POST | `/api/auth/login` | 不变（密码登录独立于 SSO） |
| POST | `/api/auth/logout` | 不变（清 cookie 即可，不调 IdP RP-logout） |
| GET | `/api/auth/me` | 不变（cookie 解析逻辑通用） |

### 9.3 `providers` 响应

```json
GET /api/auth/oauth/providers
{
  "providers": [
    {"name": "keycloak", "display_name": "企业 SSO"},
    {"name": "authing", "display_name": "Authing"}
  ]
}
```

未配置任何 provider 时返回空数组（前端据此决定是否显示 SSO 入口）。

---

## 10. 安全设计

### 10.1 CSRF 防护（state 参数）

- `state` = 32 字节密码学随机数
- 存入短期 HMAC cookie（5 分钟），回调时验证
- state 绑定 provider name，防 provider 混淆攻击

### 10.2 PKCE

- `code_verifier` = 64 字节随机 hex，`code_challenge` = SHA256 base64url
- 即使 authorization code 被截获，无 code_verifier 也无法 exchange token
- 强制 S256 method（不接受 plain）

### 10.3 ID Token 验证

- **签名**：从 IdP JWKS endpoint 获取公钥，验证 RS256/ES256 签名（go-oidc 自动处理 + 缓存 JWKS）
- **iss**：必须等于配置的 `issuer`
- **aud**：必须包含配置的 `client_id`
- **exp**：未过期
- **nonce**：本设计不使用 nonce（Authorization Code flow + PKCE 已足够，go-oidc 推荐但 PKCE 场景可选）

### 10.4 open redirect 防护

- callback 的 `redirect_uri` 固定为 `{external_url}/api/auth/oauth/{provider}/callback`，不接受用户输入
- 登录成功后只重定向到 webchat 首页 `/`（不接受 query 参数指定的任意 URL）

### 10.5 provider name 注入防护

- provider name 从 URL path 提取后，必须与配置中已注册的 provider name 精确匹配
- 配置校验：`name` 只允许 `[a-z0-9-]`，防 path traversal

### 10.6 secret 不暴露

- `client_secret` 永不出现在任何 API 响应中
- 日志中 token/secret 脱敏
- `oauth/providers` 端点只返回 `name` + `display_name`

---

## 11. 错误码

| HTTP | code | 场景 |
|---|---|---|
| 400 | `PROVIDER_NOT_FOUND` | URL 中 provider 未配置 |
| 400 | `CSRF_DETECTED` | state cookie 不匹配或缺失 |
| 400 | `STATE_EXPIRED` | state cookie 过期（> 5min） |
| 400 | `PROVIDER_MISMATCH` | state cookie 中 provider 与 path 不一致 |
| 400 | `CODE_EXCHANGE_FAILED` | token exchange 失败（IdP 返回错误） |
| 400 | `ID_TOKEN_INVALID` | ID Token 验证失败（签名/iss/aud/exp） |
| 400 | `DISCOVERY_FAILED` | 无法获取 IdP `.well-known/openid-configuration` |
| 403 | `USER_DISABLED` | SSO 用户本地状态为 disabled |
| 502 | `IDP_UNREACHABLE` | IdP 不可达（超时/连接失败） |

callback 端点的错误默认重定向到 webchat 前端的错误页（`/?auth_error={code}`），供 spec ⑥ 前端渲染（当前 webchat 前端无登录页，但 HTTP 测试可验证重定向行为）。

---

## 12. 测试策略

### 12.1 单元测试

| 测试 | 文件 | 覆盖 |
|---|---|---|
| `OAuthProvider` 构造 + discovery | `oauth_provider_test.go` | 配置校验、discovery URL 拼接 |
| `OAuthManager` 注册/查找 | `oauth_manager_test.go` | 多 provider 并发注册/查找 |
| state cookie 签发/验证 | `oauth_state_test.go` | 正常/expired/tampered/mismatch |
| `GetOrCreateUserByIdentity` | `multitenancy_store_test.go` | 首次建号 / 复用 / 事务回滚 / disabled user |
| claim 映射 | `oauth_provider_test.go` | 标准 claim + 自定义 claim 名 |

### 12.2 集成测试（mock IdP）

使用 `httptest.Server` 模拟完整 OIDC IdP（discovery + token + JWKS + userinfo），端到端验证：

| 测试 | 覆盖 |
|---|---|
| 完整 login → callback 流程 | 302 链 + cookie 签发 |
| 首次 SSO 登录建号 | users + user_identities 行写入 |
| 二次 SSO 登录复用 | 不重复建号，display_name 更新 |
| CSRF 攻击（无/错 state） | 400 CSRF_DETECTED |
| state 过期 | 400 STATE_EXPIRED |
| provider mismatch | 400 PROVIDER_MISMATCH |
| IdP 返回错误 code | 400 CODE_EXCHANGE_FAILED |
| disabled user | 403 USER_DISABLED |
| 多 provider 独立 | 两个 provider 各自建号 |

### 12.3 手动验证（真实 IdP）

使用 Keycloak Docker 容器作为真实 IdP 验证：
```bash
docker run -p 8080:8080 -e KEYCLOAK_ADMIN=admin -e KEYCLOAK_ADMIN_PASSWORD=admin quay.io/keycloak/keycloak:latest start-dev
# 创建 realm + client + test user
# 配置 hotplex oauth.providers 指向 localhost:8080
# curl /api/auth/oauth/keycloak/login → 跟随重定向 → 验证 cookie 签发
```

---

## 13. 迁移策略

### 13.1 migration 020

新增 `user_identities` 表（SQLite + PostgreSQL 双版本）。

**向后兼容**：
- 纯新增表，不修改现有 `users` 表
- 现有密码账号不受影响（无 `user_identities` 行）
- 不配置 `oauth.providers` 时，SSO 端点返回空列表，系统行为与 spec ① 完全一致

### 13.2 Wire 现有 auth 端点

spec ① 的 auth_handlers 已实现但 `routes.go` 中标注 `TODO(spec ⑥)` 未 wire。本 spec 顺带 wire：
- `POST /api/auth/login` — 密码登录
- `POST /api/auth/logout`
- `GET /api/auth/me`
- `POST /api/auth/accept-invite`
- 新增 `GET /api/auth/oauth/*` — SSO 端点

这样 spec ④ 交付后，WebChat 后端认证体系完整（密码 + SSO），spec ⑥ 只需做前端。

### 13.3 依赖引入

```
go get github.com/coreos/go-oidc/v3/oidc
go get golang.org/x/oauth2    # indirect → direct
```

---

## 14. 后续 spec 路线

| spec | 内容 | 依赖 |
|---|---|---|
| spec ④.1（可选） | 多 identity 手动绑定 UI（admin/用户自助关联 SSO 到已有账号） | spec ④ |
| spec ④.2（可选） | SAML 2.0 provider（如确有 SAML-only 客户需求） | 独立 |
| **spec ⑥** | **WebChat 前端一等公民化**（登录页 + provider 选择 + workspace/worker UI） | spec ④ ⬅ 本 spec |

---

## 附录 A：Go 依赖评估

### `github.com/coreos/go-oidc/v3`

- **维护**：CoreOS（现 Red Hat）维护，活跃
- **依赖**：仅 `golang.org/x/oauth2` + `go-jose`，轻量
- **功能**：OIDC discovery、ID Token 验证、JWKS 缓存、UserInfo
- **license**：Apache 2.0
- **评估**：业界标准 OIDC 库，Keycloak/Okta/Azure 文档示例常用

### `golang.org/x/oauth2`

- **维护**：Go 团队官方
- **已在 go.mod**：v0.36.0（indirect），提升为 direct
- **功能**：OAuth2 底层（token endpoint HTTP 调用、token 缓存接口）

### 不引入的库

- `markbates/goth`：多 provider 抽象层（Google/GitHub/Facebook...），但太重且我们用标准 OIDC
- `dexidp/dex`：IdP 而非 client，角色不对
