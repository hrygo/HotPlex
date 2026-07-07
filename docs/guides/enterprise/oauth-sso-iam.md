# 企业 OAuth/SSO IAM 对接指南

HotPlex WebChat 通过标准 OpenID Connect（OIDC）接入企业单点登录（SSO）。
认证流程采用 OAuth2 Authorization Code flow + PKCE，并验证 OIDC ID Token。
该模式是 Keycloak、Okta、Microsoft Entra ID、Google Workspace、Authing、
TOPIAM 以及多数企业 IAM/IDaaS 系统的通用对接方式。

HotPlex 当前不直接实现 SAML 或 CAS。如果企业 IAM 只提供 SAML/CAS，请使用
IAM 产品自带的 OIDC 桥接能力，或在前面部署 Keycloak 等身份代理，将 SAML/CAS
转换为标准 OIDC 后再接入 HotPlex。

## 对接要求

企业 IAM provider 必须具备以下能力：

| 能力 | 要求 |
|------|------|
| Discovery | 在配置的 issuer 下提供 `/.well-known/openid-configuration` |
| 授权流程 | Authorization Code flow |
| 客户端类型 | Web / confidential client，提供 `client_id` 和 `client_secret` |
| PKCE | 支持 S256 code challenge |
| Token | token endpoint 返回 `id_token` |
| 签名密钥 | 提供 JWKS endpoint，用于验证 ID Token 签名 |
| Claims | 稳定的 `sub`；可选 `preferred_username`、`name`、`email`，可位于 ID Token 或 UserInfo |

## 在 IAM 中注册 HotPlex

在企业 IAM 管理控制台中创建一个 confidential OIDC client。

当配置了 `oauth.external_url` 时，回调地址格式为：

```text
https://hotplex.example.com/api/auth/oauth/<provider-name>/callback
```

如果 HotPlex 部署在反向代理之后，建议显式配置 `oauth.external_url` 为公网
HTTPS 访问源。若未配置，HotPlex 会根据请求头 `Forwarded`、
`X-Forwarded-Proto`、`X-Forwarded-Host` 和 `Host` 推导回调地址源。

推荐 IAM client 配置：

| 设置项 | 建议值 |
|--------|--------|
| 应用类型 | Web / confidential client |
| Grant type | Authorization Code |
| PKCE | Required 或 Allowed，method 为 `S256` |
| Redirect URI | 上文 HotPlex callback URI |
| Scopes | `openid profile email` |
| Front-channel logout | 不需要 |
| Refresh tokens | 不需要 |

## 配置 HotPlex

生产环境建议通过环境变量引用密钥，避免把 `client_secret` 明文写入 YAML：

```yaml
oauth:
  external_url: "https://hotplex.example.com"
  providers:
    - name: "keycloak"
      display_name: "企业 SSO"
      issuer: "https://sso.example.com/realms/main"
      client_id: "${OAUTH_KEYCLOAK_CLIENT_ID}"
      client_secret: "${OAUTH_KEYCLOAK_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
```

Provider `name` 必须匹配 `[a-z0-9-]+`。该名称会出现在 URL 和
`user_identities.provider` 数据库列中，因此应选择稳定标识，例如 `keycloak`、
`okta`、`entra` 或 `google`。

## Claim 映射

默认情况下，HotPlex 读取以下 OIDC claims：

| HotPlex 字段 | 默认 claim |
|--------------|------------|
| 用户名 | `preferred_username`，缺失时回退到 `sub` |
| 显示名 | `name` |
| 邮箱 | `email` |

如果企业 IAM 使用自定义 claim 名称，可以显式配置映射：

```yaml
oauth:
  providers:
    - name: "iam"
      issuer: "https://iam.example.com"
      client_id: "${OAUTH_IAM_CLIENT_ID}"
      client_secret: "${OAUTH_IAM_CLIENT_SECRET}"
      username_claim: "uid"
      display_name_claim: "displayName"
      email_claim: "mail"
```

HotPlex 使用 `(provider, sub)` 映射本地用户，不会因为 SSO 用户与密码用户
邮箱相同而自动合并账号。

HotPlex 会先验证 ID Token。如果用户名、显示名或邮箱没有出现在 ID Token
中，HotPlex 会使用 access token 调用 OIDC UserInfo endpoint 补齐资料，并且
只有当 UserInfo 返回的 `sub` 与已验证 ID Token 的 `sub` 一致时才接受该结果。

## 多 Provider

可以同时配置多个企业 IAM provider。WebChat 登录页会为每个 provider 渲染一个
SSO 登录按钮：

```yaml
oauth:
  external_url: "https://hotplex.example.com"
  providers:
    - name: "entra"
      display_name: "Microsoft Entra ID"
      issuer: "https://login.microsoftonline.com/<tenant-id>/v2.0"
      client_id: "${OAUTH_ENTRA_CLIENT_ID}"
      client_secret: "${OAUTH_ENTRA_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
    - name: "google"
      display_name: "Google Workspace"
      issuer: "https://accounts.google.com"
      client_id: "${OAUTH_GOOGLE_CLIENT_ID}"
      client_secret: "${OAUTH_GOOGLE_CLIENT_SECRET}"
      scopes: ["openid", "profile", "email"]
```

OAuth 配置支持运行时热重载。新增、移除或轮换 provider 不需要重启 Gateway。
如果用户正在进行 SSO 登录，而该 provider 在过程中被修改，建议重新发起登录。

## 验证方式

修改配置后，打开：

```text
https://hotplex.example.com/api/auth/oauth/providers
```

期望响应：

```json
{
  "providers": [
    { "name": "keycloak", "display_name": "企业 SSO" }
  ]
}
```

然后打开 `/login`，确认页面出现对应 SSO 按钮。点击按钮后，应跳转到企业 IAM
authorization endpoint，并且请求中的 `redirect_uri` 与 IAM 中注册的 callback
URI 完全一致。

## 故障排查

| 现象 | 检查项 |
|------|--------|
| 登录页没有 SSO 按钮 | `GET /api/auth/oauth/providers` 是否返回 provider；WebChat 是否能通过 CORS 访问 Gateway |
| IAM 拒绝 redirect URI | `oauth.external_url` 是否匹配公网 HTTPS 访问源；provider name 是否与注册回调一致 |
| `STATE_EXPIRED` | 是否在 5 分钟内完成 IAM 登录；浏览器是否启用 Cookie |
| `CSRF_DETECTED` | IAM 跳转回 HotPlex 时浏览器是否保留 `oauth_state` cookie |
| `CODE_EXCHANGE_FAILED` | client secret、redirect URI、PKCE 策略或 grant type 是否与 IAM 配置一致 |
| `ID_TOKEN_INVALID` | issuer、audience/client ID、JWKS 或服务器时间是否正确 |
| 显示名或邮箱不正确 | 根据企业 IAM 的实际 claim 名配置 `display_name_claim` / `email_claim` |
