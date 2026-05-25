---
paths:
  - "**/security/*.go"
  - "**/config/config.go"
---

# 安全规范

> Mutex / 反模式规范 → 见 AGENTS.md 约定与规范

## Bot ID 传输

Bot ID 通过 `X-Bot-ID` HTTP header 或 `bot_id` query param 传输。服务端通过 `security.BotIDFromRequest(r)` 提取，无需 JWT。

### 信任边界

Bot ID **未与 API Key 密码学绑定**——任何已认证客户端可指定任意 bot ID。这是可接受的设计：
1. Bot ID 仅决定路由行为（使用哪套 bot 配置），不决定授权
2. API Key 认证已在连接层网关限制访问
3. 跨 Bot 数据隔离由下游 session key 派生强制执行

### 多 Bot 隔离
连接中的 `botID` 必须与请求的 Session 所属 Bot 精确匹配，禁止跨 Bot 操作。

---

## 命令白名单

仅允许 `claude` 和 `opencode`，禁止 shell 执行。`ValidateCommand` 拒绝含路径分隔符的命令名。

---

## 路径安全（SafePathJoin）

5 步流程：Clean → 拒绝绝对路径 → Join → EvalSymlinks → 前缀验证

**规则**：路径操作必须通过 `SafePathJoin`，禁止手动拼接用户路径。

## ValidateWorkDir（SwitchWorkDir 专用）

SwitchWorkDir 必须**同时使用** `config.ExpandAndAbs` + `security.ValidateWorkDir`，缺一不可。

---

## SSRF 防护

验证链路：协议限制 → 主机名黑名单 → IP 段阻止（loopback/private/link-local/IPv6）→ DNS 解析后检查所有返回 IP

**规则**：所有外部 URL 请求必须经过 `ValidateURL`，阻止 DNS 重新绑定攻击。

---

## 环境变量隔离

三层防护：BaseEnvWhitelist（系统变量）→ ProtectedEnvVars（禁止 Worker 覆盖）→ Sensitive 检测（自动脱敏 `AWS_*/ANTHROPIC_*/SLACK_*` 等）

**嵌套 Agent 防护**：`StripNestedAgent()` 剥离 `CLAUDECODE=` 环境变量。

---

## Tool / Model 限制

- Tool 分 4 类：Safe / Risky / Network / System，生产环境仅允许 Safe 类
- Model 白名单见 `security/tool.go` — `AllowedModels`（case-insensitive）

---

## API Key 恒定时间比较

```go
subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
```

---

## SDK 日志脱敏

第三方 SDK URL 中的 `app_secret`、`token`、`access_token` 等参数必须在日志输出前清除为 `[REDACTED]`。
