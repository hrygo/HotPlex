---
title: 企业安全加固指南
weight: 22
description: HotPlex Gateway 生产安全加固全流程：API Key、Bot ID、SSRF 防御、命令白名单、网络隔离与合规审计
---

# Security Hardening 企业安全加固指南

HotPlex Gateway 采用 **7 层纵深防御**架构，从网络边界到进程输出全链路阻断攻击面。本指南逐层拆解安全机制，帮助企业安全团队完成审计与合规配置。

---

## 1. 网络安全（Network Security）

### TLS 与绑定地址

Gateway 默认监听 `localhost:8888`，仅接受本地连接。生产部署应通过 **Reverse Proxy**（Nginx/Caddy）暴露 TLS：

```
# Nginx 反向代理示例
location /ws {
    proxy_pass http://127.0.0.1:8888;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

**关键配置**：

| 配置项 | 默认值 | 生产建议 |
|--------|--------|---------|
| `gateway.addr` | `localhost:8888` | 保持 localhost，由反向代理暴露 |
| `admin.addr` | `localhost:9999` | 禁止公网暴露 |
| TLS 终止 | 不内置 | 由 Nginx/Caddy 处理 |
| CORS | 默认限制 | 按需配置 `allowed_origins` |
| security.txt | 默认禁用 | 配置 `security.security_contact` 启用（RFC 9116） |

> **禁止**将 Gateway 直接绑定到 `0.0.0.0`。所有外部流量必须经过反向代理。

---

## 2. 认证（Authentication）

### 2.1 API Key 认证

请求通过 `X-API-Key` Header 或 `?api_key=` Query Param 携带密钥。`Authenticator` 在内存 `map` 中验证，支持热重载（`ReloadKeys`）。

**零密钥 = 开发模式**：未配置 API Key 时自动降级为 `anonymous` 用户，**生产环境必须配置至少一个 Key**。

### 2.2 Bot ID 隔离

通过 `X-Bot-ID` Header 或 `bot_id` 查询参数指定 Bot 身份。每个 Bot 只能操作属于自己的 Session，**禁止跨 Bot 访问**。使用 `security.BotIDFromRequest(r)` 提取 Bot ID。

### 2.3 APIKeyResolver（多用户映射）

通过 `security.SetKeyResolver()` 设置自定义的 `APIKeyResolver`，可将 API Key 映射到不同的 userID，实现用户级会话隔离。未设置 resolver 时，所有 API Key 认证的请求统一使用 `api_user` 身份。

### 2.4 WebChat 用户认证（内建账号 + 企业 SSO）

WebChat 多租户化后（spec ⑥）**强制登录**，废除匿名 `webchat_user` 身份。除上文的 API Key（适用于服务端客户端 / SDK 直连）外，WebChat 浏览器端使用两套用户身份认证：

**内建账号**（spec ①）：username/password 本地账号，由 admin 通过邀请码（`POST /api/admin/invitations`）创建。首次部署尚无任何账号时，登录页依据 `GET /api/auth/bootstrap-status` 引导创建首个管理员。

**企业 SSO**（spec ④）：标准 OIDC Authorization Code flow + PKCE，一套实现覆盖全部主流 IdP（Keycloak / Okta / Azure AD / Google Workspace 等）。配置 `oauth.providers[]` 后，登录页通过 `GET /api/auth/oauth/providers` 渲染 SSO 按钮，点击跳转 `GET /api/auth/oauth/{provider}/login` → IdP 认证 → `GET /api/auth/oauth/{provider}/callback` 回调。回调链路依次执行 **state 校验（防 CSRF）→ token exchange → id_token 签名验证 → 用户创建/查找 → 签发 HMAC Cookie**。

> 配置详见 [配置参考 — oauth](../../reference/configuration.md#314-oauth--webchat--ssooidc)。`client_secret` 当前为明文写入 YAML（暂不支持 `${ENV_VAR}` 展开），须通过文件权限和密钥管理体系保护。

### 2.5 HMAC Cookie 认证（登录态）

`POST /api/auth/login`（内建账号）或 OAuth 回调成功后，Gateway 签发 HMAC-SHA256 签名 cookie 作为登录态凭证，后续 WebSocket 连接自动携带，无需 API Key。

| 安全属性 | 值 | 作用 |
|---------|------|------|
| `HttpOnly` | 启用 | JavaScript 无法读取，防 XSS 窃取 |
| `SameSite` | `None` | 支持跨域 WebChat 部署（须配合 Secure） |
| `Secure` | HTTPS 自动启用 | 跨域时网关必须 HTTPS（浏览器拒绝非 Secure 的 SameSite=None cookie） |
| 签名算法 | HMAC-SHA256 | 服务端密钥签名，无法伪造 |
| 密钥持久化 | `~/.hotplex/data/cookie_secret.key`（权限 `0600`） | 重启后仍有效；可由 `security.cookie_secret` 显式配置覆盖 |
| 有效期 | 7 天 | 限制凭证有效期 |

**CSRF 权衡**：HMAC 签名仅防伪造，不防 CSRF。OAuth flow 的 CSRF 由 OIDC `state` token 防护（见 2.4 回调链路）；内建账号登录的 CSRF 由前端另行防护。

**认证优先级**：HTTP Header → Query Param → Cookie → Init Envelope。Cookie 作为第 3 优先级 fallback，不影响 Header/Query 类客户端。认证原理详见 [安全模型 — HMAC Cookie 认证](../../explanation/security-model.md#hmac-cookie-认证webchat-多租户)。

---

## 3. SSRF 防护（4 层校验）

`ValidateURL()` 依次执行 4 层检查，阻止 Worker 进程访问内网资源：

```
Layer 1: Protocol  → 仅允许 http / https
Layer 2: Bare IP   → 直接 IP 匹配 BlockedCIDRs
Layer 3: DNS       → 解析域名获取 IP 列表
Layer 4: Resolved  → 所有解析 IP 匹配 BlockedCIDRs
```

**BlockedCIDRs 覆盖范围**：

| 类别 | CIDR | 用途 |
|------|------|------|
| Loopback | `127.0.0.0/8`, `::1/128` | 本地回环 |
| Private | `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | RFC 1918 私有网络 |
| IPv6 ULA | `fc00::/7` | IPv6 唯一本地地址 |
| Link-local | `169.254.0.0/16`, `fe80::/10` | 链路本地 |
| Cloud Metadata | `169.254.169.254/32`, `100.100.100.200/32` | AWS/GCP/Azure/阿里云元数据 |
| DHCP Broadcast | `192.0.0.0/24` | RFC 8520 DHCP broadcast |
| Multicast | `224.0.0.0/4`, `ff00::/8` | 组播 |
| Reserved | `0.0.0.0/8`, `100.64.0.0/10` | 当前主机 / Carrier-grade NAT |

**高安全场景**：`ValidateURLDoubleResolve()` 在首次校验后延迟 1 秒再次 DNS 解析，检测 **DNS Rebinding** 攻击。

---

## 4. 命令白名单（Command Whitelist）

Worker 进程仅允许启动两个二进制：

```go
allowedCommands = map[string]bool{
    "claude":   true,   // Claude Code CLI
    "opencode": true,   // OpenCode Server
}
```

**注册校验**：`RegisterCommand()` 拒绝含路径分隔符（`/`、`\`）和危险字符（`;|&`$` 等 20+ 字符）的命令名，从源头阻断命令注入。

**Bash 策略引擎**：Worker 执行 Bash 命令时，`CheckBashCommand()` 进行模式匹配：

- **P0（自动拒绝）**：`rm -rf /`、`dd of=/`、`mkfs`、`fork bomb` — 无需确认直接阻断
- **P1（警告+确认）**：SSH 密钥读取、Cloud Metadata 探测、Crontab 修改 — 记录日志并要求人工确认

---

## 5. 环境变量隔离（Environment Isolation）

`BuildEnv()` 按 7 阶段构建 Worker 进程环境，实现三层隔离：

### 5.1 Blocklist 过滤

配置 `worker.env_blocklist` 指定禁止传递的变量名。支持前缀匹配（`AWS_` 结尾带下划线 = 阻断所有 `AWS_*` 变量）。

### 5.2 HOTPLEX_WORKER_ 前缀剥离

仅 `HOTPLEX_WORKER_*` 前缀的变量被剥离前缀后注入 Worker：

```
HOTPLEX_WORKER_GITHUB_TOKEN=xxx  →  GITHUB_TOKEN=xxx（Worker 环境可见）
HOTPLEX_GATEWAY_TOKEN=yyy           →  完全不可见（Gateway 内部变量）
```

当剥离后的 Key 与系统变量冲突时，系统版本被**动态阻断**，防止 Gateway 自身密钥泄漏到 Worker。

### 5.3 嵌套 Agent 防护

`StripNestedAgent()` 从 Worker 环境中移除 `CLAUDECODE=` 变量，防止 Worker 进程意外启动子 Agent 导致无限递归。

### 5.4 受保护变量

`cliProtectedVars` 保护核心系统变量（`HOME`、`PATH`、`USER`、`SHELL`、`CLAUDECODE`、`GATEWAY_ADDR`、`GATEWAY_TOKEN`），禁止 `.env` 文件覆盖。

---

## 6. Tool Access Control（工具访问控制）

`AllowedTools` 定义两套工具集，按环境严格区分：

| 工具 | Dev Mode | Production | 分类 |
|------|----------|------------|------|
| Read / Grep / Glob | ✅ | ✅ | Safe |
| Edit / Write | ✅ | ❌ | Safe |
| Bash | ✅ | ❌ | Risky |
| WebFetch | ✅ | ❌ | Network |
| Agent / NotebookEdit / TodoWrite | ✅ | ❌ | System |

**生产环境仅允许 3 个只读工具**（Read、Grep、Glob），通过 `ProductionAllowedTools` 严格限制。所有工具通过 `--allowed-tools` 参数注入 Claude Code CLI，未授权工具完全不可调用。

### 权限审批

Risky / Network / System 类工具在开发模式下可用，但 Bash 命令受策略引擎约束（第 4 层），危险操作触发 P0 自动拒绝或 P1 人工确认。

---

## 7. Output Limits（输出限制）

`OutputLimiter` 在三个维度限制 Worker 输出，防止内存耗尽攻击：

| 限制项 | 值 | 作用 |
|--------|------|------|
| `MaxLineBytes` | **10 MB** | 单行输出上限 |
| `MaxSessionBytes` | **20 MB** | 单 Session 累计输出上限 |
| `MaxEnvelopeBytes` | **1 MB** | 单个 AEP Envelope 上限 |

超出任一限制立即返回错误并终止该 Session 的输出收集。`OutputLimiter` 通过 `sync.Mutex` 保护字节计数器，并发安全。

---

## 安全审计清单

| # | 检查项 | 状态 |
|---|--------|------|
| 1 | Gateway 绑定 localhost，未暴露公网 | ☐ |
| 2 | 至少配置一个 API Key（生产环境） | ☐ |
| 3 | Bot ID 隔离验证生效（Header + Query） | ☐ |
| 4 | WebChat 多租户强制登录生效（无 `webchat_user` 匿名身份残留） | ☐ |
| 5 | OAuth `client_secret` 与 `~/.hotplex/data/cookie_secret.key`（权限 `0600`）妥善保管 | ☐ |
| 6 | 跨域 WebChat 部署网关已启用 HTTPS（SameSite=None cookie 依赖） | ☐ |
| 7 | SSRF BlockedCIDRs 覆盖私有/元数据地址 | ☐ |
| 8 | Worker 命令白名单仅含 claude/opencode | ☐ |
| 9 | `HOTPLEX_WORKER_` 前缀隔离正确配置 | ☐ |
| 10 | 生产环境使用 `ProductionAllowedTools`（3 工具） | ☐ |
| 11 | Output Limits 未被修改 | ☐ |
| 12 | TLS 由反向代理终止 | ☐ |

---

## 相关源码

| 模块 | 文件 |
|------|------|
| API Key 认证 + Bot ID | `internal/security/auth.go` |
| OAuth/OIDC SSO | `internal/security/oauth_provider.go`、`oauth_manager.go`、`oauth_state.go` |
| OAuth 回调路由 | `internal/gateway/oauth_handlers.go` |
| HMAC Cookie 认证 | `internal/security/cookie.go` |
| 内建账号 provider | `internal/security/local_account_provider.go` |
| SSRF 4 层防护 | `internal/security/ssrf.go` |
| 命令白名单 + Bash 策略 | `internal/security/command.go` |
| 环境变量隔离 | `internal/security/env.go` |
| Worker Env 构建 | `internal/worker/base/env.go` |
| 路径安全 | `internal/security/path.go` |
| Tool 访问控制 | `internal/security/tool.go` |
| 输出限制 | `internal/security/limits.go` |
| CORS 中间件 | `internal/security/cors.go` |
| security.txt 端点 | `internal/security/securitytxt.go` |
