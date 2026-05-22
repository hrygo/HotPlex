---
title: 安全模型
weight: 6
description: HotPlex Gateway 安全设计哲学与多层防护体系：白名单策略、API Key 认证、SSRF 防御与进程隔离
---

# 安全模型

> HotPlex Gateway 的安全设计哲学与多层防护体系解析

## 设计哲学：白名单优先 + 纵深防御

HotPlex Gateway 的安全模型遵循两条核心原则：

1. **白名单优先**（Whitelist-first）：默认拒绝一切，仅显式允许已知安全的操作
2. **纵深防御**（Defense-in-Depth）：独立的安全层叠加，单层被突破不影响整体安全

这意味着即使某一层防护被绕过，其他层仍然提供保护。例如：即使 Worker 进程被攻破，环境变量隔离和命令白名单仍然限制其行动范围。

## 5 层安全体系

```
┌─────────────────────────────────────┐
│  Layer 5: AI Execution Safety       │  Agent 行为约束、Tool 限制
├─────────────────────────────────────┤
│  Layer 4: Network Security          │  SSRF 防护、绑定 localhost
├─────────────────────────────────────┤
│  Layer 3: Input Validation          │  Envelope 校验、XML Sanitizer
├─────────────────────────────────────┤
│  Layer 2: Authentication            │  API Key + X-Bot-ID
├─────────────────────────────────────┤
│  Layer 1: Protocol Security         │  AEP 版本协商、命令白名单
└─────────────────────────────────────┘
```

### Layer 1：Protocol Security（协议安全）

- AEP `version` 字段强制为 `aep/v1`，版本不匹配立即拒绝
- 首帧必须为 `init`（30s 超时），否则 `PROTOCOL_VIOLATION`
- Envelope 大小限制 1MB（`MaxEnvelopeBytes`）
- NDJSON 编码，每行独立解析，防止格式混淆攻击

### Layer 2：Authentication（认证）

认证采用 **API Key + Bot ID** 双字段模型，实现网关级别访问控制与多 Bot 隔离。

#### 传输方式

| 通道 | API Key | Bot ID | 适用场景 |
|------|---------|--------|---------|
| HTTP Header | `X-API-Key` | `X-Bot-ID` | REST API、CLI、服务端客户端 |
| Query Param | `api_key` | `bot_id` | 浏览器 WebSocket（无法发送自定义 Header） |
| Init Envelope | `auth.token` | `auth.bot_id` | 浏览器 WebSocket 延迟认证 |

#### 认证流程

**标准客户端（HTTP Header）**：

```
Client ──X-API-Key──> HTTP Upgrade ──> Authenticator.AuthenticateRequest()
         X-Bot-ID                     ├─ 提取 API Key（header 优先，query param 兜底）
                                      ├─ 校验 Key 合法性（恒定时间比较）
                                      ├─ 解析 Bot ID
                                      └─ 返回 (userID, botID, nil) 或 ErrUnauthorized
```

**浏览器 WebSocket 客户端（延迟认证）**：

```
Browser ──WS Upgrade (no headers)──> Hub.HandleHTTP
                                     └─ pendingAuth = true（标记延迟认证）

Browser ──init envelope──> Conn.ReadPump
         auth.token         ├─ ExtractAPIKey 失败 → 拒绝
         auth.bot_id        ├─ AuthenticateKey 校验 token
                            ├─ 提取 bot_id
                            └─ 认证成功，清除 pendingAuth
```

浏览器 WebSocket 客户端因 CORS 限制无法发送自定义 HTTP Header，因此认证被延迟到首帧 `init` Envelope。服务端在 `HandleHTTP` 阶段检测到无 API Key 时设置 `pendingAuth` 标记，待 `ReadPump` 收到 `init` 后从 `auth.token` 字段提取并校验。

#### Dev 模式

当未配置任何 API Key 时，所有请求以 `"anonymous"` 身份放行，无需认证。这一行为在 `AuthenticateRequest` 和 `AuthenticateKey` 中均有处理：`len(validKey) == 0` 时直接返回 `"anonymous"`。

#### API Key 到用户身份的映射

默认情况下，所有合法 Key 的用户身份为 `"api_user"`。可通过 `SetKeyResolver` 注入自定义映射（如从数据库关联 API Key 到具体用户 ID）。

#### Bot ID 与多租户隔离

Bot ID 通过 `security.BotIDFromRequest(r)` 从 `X-Bot-ID` Header 或 `bot_id` Query Param 中提取。连接中的 `botID` 必须与 Session 所属 Bot 精确匹配，跨 Bot 操作被 Session 层拒绝。

### Layer 3：Input Validation（输入验证）

- JSON Schema 验证所有 Envelope 必填字段
- `DisallowUnknownFields` 防止注入未知字段
- **XML Sanitizer**：对保留标签进行 HTML 转义，预防 XML 注入
- Bash 命令策略引擎（P0 自动拒绝、P1 警告）

### Layer 4：Network Security（网络安全）

- SSRF 4 层防护（详见下文）
- 默认仅绑定 `localhost`（安全基线）
- 路径安全验证（`SafePathJoin`、`ValidateWorkDir`）

### Layer 5：AI Execution Safety（AI 执行安全）

- Tool 分类与限制（Safe / Risky / Network / System）
- 环境变量隔离，防止凭证泄露到 Worker 子进程
- 嵌套 Agent 防护（`StripNestedAgent`）
- Permission 交互协议，敏感操作需人类审批

## 为什么使用命令白名单

HotPlex 仅允许执行两个二进制：`claude` 和 `opencode`。

### 设计理由

- **消除 shell 注入**：直接 `exec.Command`，不通过 shell（`sh -c`），参数不经过 shell 解释
- **禁止路径分隔符**：命令名不允许包含 `/` 或 `\`，防止指定任意路径
- **危险字符检测**：命令名包含 `;|&`$` 等字符直接拒绝

```go
// command.go — 白名单 + 安全检查
var allowedCommands = map[string]bool{
    "claude":   true,
    "opencode": true,
}

func ValidateCommand(name string) error {
    if strings.Contains(name, "/") || strings.Contains(name, "\\") {
        return fmt.Errorf("must not contain path separators")
    }
    if !allowedCommands[name] {
        return fmt.Errorf("not in whitelist")
    }
    return nil
}
```

### 为什么不用 sh -c

使用 `sh -c` 会引入 shell 元字符解释（`$()`、`` ` ``、`&&`、`||` 等），攻击者可通过精心构造的输入注入任意命令。直接 `exec.Command` 将参数作为 argv 传递给子进程，完全绕过 shell。

## 为什么需要环境变量隔离

Worker 子进程继承 Gateway 的环境变量，如果不加过滤，所有敏感信息（API Key、数据库密码、云凭证）都会泄露给 Worker。

### 三层防护

1. **BaseEnvWhitelist**：只传递系统必要变量（`HOME`、`PATH`、`USER`、`SHELL`）
2. **ProtectedEnvVars**：禁止 Worker 覆盖的关键变量（`CLAUDECODE`、`GATEWAY_ADDR`、`GATEWAY_TOKEN`）
3. **Sensitive 检测**：自动脱敏前缀匹配 `AWS_*`、`ANTHROPIC_*`、`SLACK_*` 等敏感变量

### 嵌套 Agent 防护

`StripNestedAgent()` 从环境变量中移除 `CLAUDECODE=`，防止 Worker 进程内的 AI Agent 递归启动新的 Agent 实例。

## SSRF 4 层防护

Server-Side Request Forgery（SSRF）允许攻击者通过 Worker 发起请求访问内部网络。HotPlex 使用 4 层检查防御：

```
Layer 1: 协议限制 → 仅允许 http / https
Layer 2: 裸 IP 检查 → 直接拒绝私有 IP 地址的 URL
Layer 3: DNS 解析  → 解析域名获取 IP
Layer 4: IP 段检查 → 所有解析结果与 BlockedCIDRs 比对
```

### 被阻止的 IP 范围

| CIDR | 用途 |
|------|------|
| `127.0.0.0/8`、`::1/128` | Loopback |
| `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16` | RFC 1918 私有网络 |
| `169.254.169.254/32` | AWS/GCP/Azure 元数据服务 |
| `100.100.100.200/32` | 阿里云元数据服务 |
| `224.0.0.0/4`、`ff00::/8` | 多播 |
| `0.0.0.0/8` | 当前主机 |
| `fc00::/7` | IPv6 唯一本地地址 |

### DNS 重新绑定防护

高敏感场景可使用 `ValidateURLDoubleResolve`：第一次解析后等待 1 秒，第二次解析验证 IP 未变化，防止攻击者通过短 TTL DNS 记录实施 DNS rebinding 攻击。

## Permission 审批协议

### 为什么有 5 分钟超时

当 AI Agent 请求执行敏感操作（如写入文件、执行命令），Worker 发送 `permission_request` 等待用户审批。5 分钟超时的设计理由：

1. **防止无限等待**：如果用户已离线，Worker 进程不应永久挂起，占用系统资源
2. **安全默认值**：超时后自动拒绝（auto-deny），而非自动允许
3. **资源回收**：5 分钟足以让用户在移动端收到通知并审批

### 为什么选择 auto-deny

`auto-deny`（而非 `auto-allow`）遵循最小权限原则：未经明确批准的操作一律拒绝。这确保了即使用户未及时响应，系统也不会执行未授权的操作。

## Dev 模式 vs 生产模式

| 维度 | Dev 模式 | 生产模式 |
|------|---------|---------|
| API Key | 未配置时允许所有请求（`anonymous`） | 必须配置，所有请求需认证 |
| SSRF | 标准检查 | 标准 + Double Resolve |
| Tool 限制 | 所有 Tool | 仅 Safe 类 |
| Bash 策略 | P1 改为警告 | P1 严格阻止 |
| 网络绑定 | `localhost` | 按需配置 |

**核心权衡**：Dev 模式优先开发效率（减少认证摩擦），生产模式优先安全（严格的访问控制）。两者共享同一套安全检查代码，只是策略参数不同。

---

## 相关实践

- [安全模型操作指南](../guides/developer/security-model.md) — 5 层安全体系的日常配置与审计
- [安全策略参数参考](../reference/security-policies.md) — API Key、SSRF、命令白名单、工具控制的完整参数
