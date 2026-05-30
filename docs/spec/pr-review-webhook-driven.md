# PR Review Webhook-Driven 触发 + Prompt 优化 Spec

**版本**: v1.2 · **日期**: 2026-05-30 · **状态**: Code Complete (§6/§7/§8 implemented)

---

## 目录

- [1. 目标](#1-目标)
- [2. 当前状态与问题](#2-当前状态与问题)
- [3. 架构设计](#3-架构设计)
- [4. 网络拓扑与安全](#4-网络拓扑与安全)
- [5. GitHub 侧配置](#5-github-侧配置)
- [6. HotPlex 代码改动](#6-hotplex-代码改动)
- [7. HotPlex 配置](#7-hotplex-配置)
- [8. P1: Prompt 优化（预读 + 2-agent）](#8-p1-prompt-优化预读--2-agent)
- [9. Acceptance Criteria](#9-acceptance-criteria)
- [10. 迁移计划](#10-迁移计划)
- [12. 监控与回滚](#12-监控与回滚)

---

## 1. 目标

将 `pr-review-hotplex` cronjob 从 **3 分钟轮询** 改为 **GitHub Webhook 事件驱动**，同时优化 REVIEW 阶段的 agent 并行策略。

**量化目标**：

| 指标 | 当前 | 目标 | 改善 |
|------|------|------|------|
| 日均执行轮次 | ~480 | ~15-25 | -95% |
| SKIP 轮次/天 | ~355 | 0（消除） | -100% |
| REVIEW 单轮成本 | ~$3.30 | ~$1.50-2.00 | -45% |
| 日均总成本 | ~$479 | ~$30-50 | -90% |
| PR 审查响应延迟 | 平均 90s（轮询间隔） | ~5s（事件驱动） | -94% |

---

## 2. 当前状态与问题

### 2.1 当前架构

```
GitHub PR 事件
       ↓ (无感知)
  Cron Timer (every:3min)
       ↓
  HotPlex Executor → 新 Session → Claude Code Worker
       ↓                              ↓
  §0 身份验证                    读取 system prompt (~41k tokens)
  §1 CI 批准 + 去重              执行 bash 命令 (gh CLI)
  §2-§5 (仅当有待审 PR)          74.2% 概率: 输出 "All reviewed" 后终止
```

### 2.2 问题分析

**P0: SKIP 轮浪费**（占 74.2%）

每轮创建全新 session（`DeriveCronSessionKey(job.ID, nanotime)`），全新 Claude Code 进程 → 冷启动 → 每次付 ~41k tokens cache_write + ~82k cache_read + ~600 tokens output。绝大部分仅执行 `gh pr list` + `gh pr checks` 后输出 "All reviewed."。这些操作是纯 shell，不需要 Claude 推理。

**P1: REVIEW 轮 4-agent 上下文膨胀**

4 个并行 agent 各自独立加载完整上下文（system prompt + CLAUDE.md + rules + diff + 源文件），产生 ~618k cache_read × 4 = ~2.5M tokens 的冗余。Agent A 和 B 都要读同一段 diff，Agent C 和 D 都要读同一份 CLAUDE.md。

### 2.3 基础设施现状

```
服务器公网 IP:     <PUBLIC_IP>（无服务监听）
Caddy 反代:       <TAILSCALE_IP>:80（仅 Tailscale 内网静态站点）
HotPlex Gateway:  127.0.0.1:8888（仅本地）
Tailscale:        已部署
```

---

## 3. 架构设计

### 3.1 目标架构

```
GitHub Webhook (pull_request / check_suite)
       │
       ▼ HTTPS
  Caddy 反代 (<PUBLIC_IP>:443)
       │ 仅放行 /api/webhook/github
       ▼
  HotPlex Gateway (:8888)
       │
       ▼
  WebhookHandler
    ├─ 1. HMAC-SHA256 签名验证
    ├─ 2. 事件过滤 (opened/synchronize/completed)
    ├─ 3. CI 状态检查 (仅 CI 通过触发)
    └─ 4. 调用 CronExecutor.Trigger(jobName, prContext)
              │
              ▼
         Claude Code Worker (预读 + 2-agent)
              │
              ▼
         PR Review 提交
```

### 3.2 双通道保障

Webhook 是主通道，但保留降频 cron 作为兜底：

| 通道 | 频率 | 用途 |
|------|------|------|
| **Webhook** | 事件驱动（~0-10s 延迟） | 即时响应 PR 变更 |
| **Cron fallback** | `every:30m`（从 3min 降频） | 兜底：捕获 webhook 漏投、网络中断、服务重启期间的事件 |

Cron fallback 的 prompt 简化为仅执行 §0+§1 门控，不执行完整 review（避免与 webhook 重复审）。

### 3.3 幂等保护

同一 PR 的同一 commit SHA 只审一次：

```
Webhook 事件 → 检查 hotplex-ai 对该 commit SHA 是否已有非 DISMISSED review
  → 已有: SKIP
  → 无: 触发 review
```

这复用了当前 §1 的去重逻辑，在 webhook handler 层和 worker prompt 层双重执行。

---

## 4. 网络拓扑与安全

### 4.1 是否需要公网 IP？

**是的，GitHub Webhook 要求 HTTPS 端点可公网访问。** 但不需要暴露整个 HotPlex 服务。

### 4.2 推荐方案：Caddy 反代（最小暴露面）

在现有 Caddy 中新增一个 site block，**仅代理 webhook 路径**，其余全部拒绝：

```caddyfile
# 现有 Tailscale 静态站点（不变）
http://<TAILSCALE_IP>:80 {
    root * /var/www/static
    file_server
    # ... 现有配置不变
}

# 新增：公网 webhook 端点（仅此一个路径）
# 注意: 纯 IP 无法获取 Let's Encrypt 证书，使用 Caddy 内置 CA 自签名
# 绑定域名后去掉 tls internal，改为自动 Let's Encrypt
<PUBLIC_IP> {
    tls internal

    # 仅放行 webhook 路径
    handle /api/webhook/github {
        reverse_proxy 127.0.0.1:8888
    }

    # 其他所有路径拒绝
    handle {
        respond 404
    }

    log {
        output file /var/log/caddy/webhook.log {
            roll_size 10mb
            roll_keep 5
        }
        format json
    }
}
```

**安全层级（纵深防御）**：

| 层级 | 机制 | 说明 |
|------|------|------|
| L1: 网络层 | Caddy 路由隔离 | 仅 `/api/webhook/github` 可达，其余 404 |
| L2: 传输层 | TLS (Caddy 内置 CA 自签名) | 纯 IP 无法用 Let's Encrypt，绑定域名后可升级 |
| L3: 应用层 | HMAC-SHA256 签名验证 | `X-Hub-Signature-256` header，常量时间比较 |
| L4: 逻辑层 | 事件过滤 + 仓库校验 | 仅处理 `hrygo/hotplex` 的特定事件类型 |
| L5: 限流 | Token bucket | 每秒 ≤2 请求，突发 ≤10 |
| L6: 幂等 | commit SHA 去重 | 同一 commit 不重复审 |

### 4.3 备选方案（如不愿暴露公网 IP）

| 方案 | 优点 | 缺点 |
|------|------|------|
| **Tailscale Funnel** | 零公网暴露，自动 TLS | 暴露整个 gateway，非路径级隔离 |
| **Cloudflare Tunnel** | 免费，DDoS 防护，自动 TLS | 依赖第三方，延迟增加 |
| **GitHub Actions → HotPlex API** | 无需入站连接 | 需 HotPlex API 可从 GitHub 访问（同样需公网） |
| **GitHub App (Polling)** | 无需入站连接 | 轮询模式，延迟 30-60s |

**推荐**：Caddy 反代（方案 4.2）。理由：已有 Caddy + 公网 IP，改动最小，安全隔离最强。

### 4.4 Caddy 配置步骤

```bash
# 1. 确保 80/443 端口在防火墙放行
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp

# 2. 备份当前 Caddyfile
sudo cp /etc/caddy/Caddyfile /etc/caddy/Caddyfile.bak

# 3. 编辑 Caddyfile，在末尾添加上述公网 site block
sudo vim /etc/caddy/Caddyfile

# 4. 验证配置
caddy validate --config /etc/caddy/Caddyfile

# 5. 重载 Caddy（自动申请证书）
sudo systemctl reload caddy

# 6. 验证
curl -s -o /dev/null -w "%{http_code}" https://<PUBLIC_IP>/api/webhook/github
# 期望: 405 (Method Not Allowed, 因为 GET 不被处理)
# 或 400 (Bad Request, 缺少签名头)

curl -s -o /dev/null -w "%{http_code}" https://<PUBLIC_IP>/
# 期望: 404
```

---

## 5. GitHub 侧配置

### 5.1 生成 Webhook Secret

```bash
# 生成 32 字节随机 secret
openssl rand -hex 32
# 示例输出: a1b2c3d4e5f6...（64 字符 hex）
```

将此值妥善保存，后续需同时配置到 GitHub 和 HotPlex。

### 5.2 配置 Repository Webhook

1. 打开 `https://github.com/hrygo/hotplex/settings/hooks`
2. 点击 **Add webhook**
3. 填写：

| 字段 | 值 |
|------|------|
| **Payload URL** | `https://<PUBLIC_IP>/api/webhook/github` |
| **Content type** | `application/json` |
| **Secret** | 上一步生成的 secret |
| **SSL verification** | ☐ Disable (自签名证书，绑定域名后开启) |
| **Which events** | ☑ **Let me select individual events**: |
| | ☑ Pull requests → opened, synchronize, reopened, ready_for_review |
| | ☑ Check suites → completed |
| | ☑ Check runs → completed |
| **Active** | ✅ |

> **注意**：不要勾选 `Push`、`Issues` 等无关事件，减少噪音。

### 5.3 验证 Webhook 连通性

配置完成后 GitHub 会发送一个 `ping` 事件。检查：

```bash
# 查看 Caddy 日志确认收到请求
sudo journalctl -u caddy --since "1 min ago" | grep webhook

# 查看 HotPlex 日志
hotplex gateway logs 2>/dev/null | tail -20
```

期望看到 HotPlex 日志中输出：`webhook: ping received from hrygo/hotplex`。

### 5.4 （可选）GitHub App 模式

如果未来需要更细粒度的权限控制（如跨仓库 review），可升级为 GitHub App：

1. 在 `https://github.com/settings/apps/new` 创建 App
2. Permissions: `pull_requests:read`, `checks:read`, `contents:read`
3. Subscribe to events: 同上
4. Webhook URL: 同上
5. 安装到 `hrygo/hotplex` 仓库

当前阶段使用 Repository Webhook 即可，无需 GitHub App 的复杂度。

---

## 6. HotPlex 代码改动

### 6.1 新增文件

#### `internal/gateway/webhook.go`

Webhook 处理器，职责：
- HMAC-SHA256 签名验证
- 事件过滤与分发
- 触发 CronExecutor

```go
package gateway

import (
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "io"
    "log/slog"
    "net/http"
    "time"
)

// WebhookConfig webhook 配置
type WebhookConfig struct {
    Enabled    bool   `mapstructure:"enabled"`
    Secret     string `mapstructure:"secret"`
    Path       string `mapstructure:"path"`        // default: "/api/webhook/github"
    MaxBodySize int64 `mapstructure:"max_body_size"` // default: 1MB
}

// GitHubEvent GitHub webhook payload 的通用字段
type GitHubEvent struct {
    Action     string `json:"action"`
    Repository struct {
        FullName string `json:"full_name"`
    } `json:"repository"`
    Number     int    `json:"number"`               // PR number
    PullRequest *struct {
        Number int    `json:"number"`
        State  string `json:"state"`
        Draft  bool   `json:"draft"`
        Head   struct {
            SHA string `json:"sha"`
        } `json:"head"`
    } `json:"pull_request"`
    CheckSuite *struct {
        Conclusion string `json:"conclusion"`
        HeadSHA    string `json:"head_sha"`
        PullRequests []struct {
            Number int `json:"number"`
        } `json:"pull_requests"`
    } `json:"check_suite"`
}

// WebhookHandler 处理 GitHub webhook 请求
type WebhookHandler struct {
    cfg     WebhookConfig
    trigger JobTrigger
    limiter *rateLimiter
    log     *slog.Logger
}

// JobTrigger 触发 cron job 的接口（由 CronExecutor 实现）
type JobTrigger interface {
    TriggerByName(ctx context.Context, jobName string, context map[string]string) error
}

// ServeHTTP 处理 webhook 请求
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 1. 方法检查
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }

    // 2. 限流
    if !h.limiter.Allow() {
        http.Error(w, "rate limited", http.StatusTooManyRequests)
        return
    }

    // 3. 读取 body（带大小限制）
    body := io.LimitReader(r.Body, h.cfg.MaxBodySize)
    payload, err := io.ReadAll(body)
    if err != nil {
        http.Error(w, "bad request", http.StatusBadRequest)
        return
    }

    // 4. HMAC-SHA256 签名验证
    sig := r.Header.Get("X-Hub-Signature-256")
    if !h.verifySignature(payload, sig) {
        h.log.Warn("webhook: invalid signature")
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }

    // 5. Ping 事件（GitHub 配置完成时发送）
    if r.Header.Get("X-GitHub-Event") == "ping" {
        h.log.Info("webhook: ping received")
        w.WriteHeader(http.StatusOK)
        return
    }

    // 6. 解析事件
    var event GitHubEvent
    if err := json.Unmarshal(payload, &event); err != nil {
        http.Error(w, "bad request", http.StatusBadRequest)
        return
    }

    // 7. 仓库校验
    if event.Repository.FullName != "hrygo/hotplex" {
        h.log.Warn("webhook: unexpected repo", "repo", event.Repository.FullName)
        w.WriteHeader(http.StatusOK) // 返回 200 避免重试
        return
    }

    // 8. 事件过滤 → 提取需要 review 的 PR number
    prNumbers := h.extractPRs(r.Header.Get("X-GitHub-Event"), &event)
    if len(prNumbers) == 0 {
        w.WriteHeader(http.StatusOK)
        return
    }

    // 9. 异步触发（不阻塞 HTTP 响应）
    for _, pr := range prNumbers {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        go func(prNum int) {
            defer cancel()
            err := h.trigger.TriggerByName(ctx, "pr-review-hotplex", map[string]string{
                "trigger":   "webhook",
                "pr_number": strconv.Itoa(prNum),
            })
            if err != nil {
                h.log.Error("webhook: trigger failed", "pr", prNum, "err", err)
            }
        }(pr)
    }

    w.WriteHeader(http.StatusAccepted)
}

// verifySignature 验证 HMAC-SHA256 签名（常量时间比较）
func (h *WebhookHandler) verifySignature(payload []byte, sig string) bool {
    if h.cfg.Secret == "" || sig == "" {
        return false
    }
    if !strings.HasPrefix(sig, "sha256=") {
        return false
    }
    sigHex := strings.TrimPrefix(sig, "sha256=")
    sigBytes, _ := hex.DecodeString(sigHex)

    mac := hmac.New(sha256.New, []byte(h.cfg.Secret))
    mac.Write(payload)
    expected := mac.Sum(nil)

    return hmac.Equal(sigBytes, expected)
}

// extractPRs 从事件中提取需要 review 的 PR number
func (h *WebhookHandler) extractPRs(eventType string, e *GitHubEvent) []int {
    switch eventType {
    case "pull_request":
        if e.PullRequest == nil || e.PullRequest.State != "open" || e.PullRequest.Draft {
            return nil
        }
        switch e.Action {
        case "opened", "synchronize", "reopened", "ready_for_review":
            return []int{e.PullRequest.Number}
        }

    case "check_suite":
        if e.CheckSuite == nil || e.CheckSuite.Conclusion != "success" {
            return nil
        }
        var prs []int
        for _, pr := range e.CheckSuite.PullRequests {
            prs = append(prs, pr.Number)
        }
        return prs
    }
    return nil
}
```

#### `internal/gateway/webhook_test.go`

```go
package gateway

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/hex"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/require"
)

func TestWebhookHandler_SignatureVerification(t *testing.T) {
    t.Parallel()

    secret := "test-secret"
    h := &WebhookHandler{
        cfg: WebhookConfig{Secret: secret, MaxBodySize: 1 << 20},
    }

    payload := []byte(`{"action":"opened"}`)

    t.Run("valid signature", func(t *testing.T) {
        mac := hmac.New(sha256.New, []byte(secret))
        mac.Write(payload)
        sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

        ok := h.verifySignature(payload, sig)
        require.True(t, ok)
    })

    t.Run("invalid signature", func(t *testing.T) {
        ok := h.verifySignature(payload, "sha256=deadbeef")
        require.False(t, ok)
    })

    t.Run("missing signature", func(t *testing.T) {
        ok := h.verifySignature(payload, "")
        require.False(t, ok)
    })

    t.Run("wrong prefix", func(t *testing.T) {
        mac := hmac.New(sha256.New, []byte(secret))
        mac.Write(payload)
        ok := h.verifySignature(payload, "sha1="+hex.EncodeToString(mac.Sum(nil)))
        require.False(t, ok)
    })
}

func TestWebhookHandler_EventFiltering(t *testing.T) {
    t.Parallel()

    h := &WebhookHandler{cfg: WebhookConfig{MaxBodySize: 1 << 20}}

    t.Run("draft PR ignored", func(t *testing.T) {
        e := &GitHubEvent{
            PullRequest: &struct {
                Number int    `json:"number"`
                State  string `json:"state"`
                Draft  bool   `json:"draft"`
                Head   struct{ SHA string `json:"sha"` }
            }{Number: 42, State: "open", Draft: true},
        }
        prs := h.extractPRs("pull_request", e)
        require.Empty(t, prs)
    })

    t.Run("check_suite success triggers PRs", func(t *testing.T) {
        e := &GitHubEvent{
            CheckSuite: &struct {
                Conclusion string `json:"conclusion"`
                HeadSHA    string `json:"head_sha"`
                PullRequests []struct{ Number int `json:"number"` }
            }{
                Conclusion: "success",
                PullRequests: []struct{ Number int `json:"number"` }{{Number: 42}},
            },
        }
        prs := h.extractPRs("check_suite", e)
        require.Equal(t, []int{42}, prs)
    })

    t.Run("check_suite failure ignored", func(t *testing.T) {
        e := &GitHubEvent{
            CheckSuite: &struct {
                Conclusion string `json:"conclusion"`
                HeadSHA    string `json:"head_sha"`
                PullRequests []struct{ Number int `json:"number"` }
            }{
                Conclusion: "failure",
                PullRequests: []struct{ Number int `json:"number"` }{{Number: 42}},
            },
        }
        prs := h.extractPRs("check_suite", e)
        require.Empty(t, prs)
    })
}
```

### 6.2 修改文件

#### `cmd/hotplex/routes.go`

在 gateway mux 注册 webhook 路由：

```go
// 在 setupGatewayRoutes() 中添加：
if cfg.Webhook.Enabled {
    webhookHandler := gateway.NewWebhookHandler(
        gateway.WebhookConfig{
            Secret:      cfg.Webhook.Secret,
            Path:        cfg.Webhook.Path,
            MaxBodySize: cfg.Webhook.MaxBodySize,
        },
        cronExecutor,  // 实现 JobTrigger 接口
        logger,
    )
    mux.Handle(cfg.Webhook.Path, webhookHandler)
}
```

#### `internal/cron/executor.go`

新增 `TriggerByName` 方法，供 webhook handler 调用：

```go
// TriggerByName 按名称查找 cron job 并触发执行（供 webhook 调用）
func (e *Executor) TriggerByName(ctx context.Context, jobName string, extraContext map[string]string) error {
    job, err := e.store.GetByName(ctx, jobName)
    if err != nil {
        return fmt.Errorf("webhook trigger: job %q not found: %w", jobName, err)
    }
    if !job.Enabled {
        return fmt.Errorf("webhook trigger: job %q is disabled", jobName)
    }

    // 注入 webhook 上下文到 platformKey
    platformKey := make(map[string]string)
    if job.PlatformKey != nil {
        maps.Copy(platformKey, job.PlatformKey)
    }
    platformKey["cron_job_id"] = job.ID
    platformKey["trigger_source"] = "webhook"
    if pr, ok := extraContext["pr_number"]; ok {
        platformKey["target_pr"] = pr
    }

    // 异步执行（不阻塞 webhook 响应）
    go e.Execute(ctx, job, time.Duration(job.TimeoutSec)*time.Second)
    return nil
}
```

#### `internal/config/config.go`

新增 WebhookConfig 结构：

```go
// WebhookConfig GitHub webhook 配置
type WebhookConfig struct {
    Enabled     bool   `mapstructure:"enabled"`
    Secret      string `mapstructure:"secret"`
    Path        string `mapstructure:"path"`         // default: "/api/webhook/github"
    MaxBodySize int64  `mapstructure:"max_body_size"` // default: 1048576 (1MB)
}

// 在 RootConfig 中添加字段：
type RootConfig struct {
    // ... 现有字段
    Webhook WebhookConfig `mapstructure:"webhook"`
}
```

### 6.3 改动清单汇总

| 文件 | 类型 | 说明 |
|------|------|------|
| `internal/gateway/webhook.go` | **新增** | Webhook handler + 签名验证 + 事件过滤 |
| `internal/gateway/webhook_test.go` | **新增** | 单元测试 |
| `cmd/hotplex/routes.go` | 修改 | 注册 webhook 路由 |
| `internal/cron/executor.go` | 修改 | 新增 `TriggerByName()` |
| `internal/config/config.go` | 修改 | 新增 `WebhookConfig` |

---

## 7. HotPlex 配置

### 7.1 config.yaml

在 `~/.hotplex/config.yaml` 中新增：

```yaml
# ─────────────────────────────────────────────────────────────────────────────
# WEBHOOK
# ─────────────────────────────────────────────────────────────────────────────
webhook:
  enabled: true
  # HMAC-SHA256 签名密钥（与 GitHub Webhook Settings 中的 Secret 一致）
  # ⚠️ 推荐通过环境变量 HOTPLEX_WEBHOOK_SECRET 注入，不写明文到配置文件
  secret: ""
  # 监听路径
  path: "/api/webhook/github"
  # 请求体大小限制
  max_body_size: 1048576  # 1MB
```

### 7.2 环境变量（推荐）

```bash
# 在 ~/.hotplex/.env 中添加：
HOTPLEX_WEBHOOK_SECRET=<第 5.1 步生成的 secret>
```

环境变量 `HOTPLEX_WEBHOOK_SECRET` 优先级高于 `config.yaml` 中的 `webhook.secret`。

### 7.3 Cron Job 调整

将现有 `pr-review-hotplex` 从 `every:180000`（3 分钟）调整为 `every:1800000`（30 分钟）兜底：

```bash
hotplex cron update pr-review-hotplex \
  --schedule "every:30m"
```

Cron fallback 的 prompt 保持不变（仍执行 §0+§1 门控，自动去重）。

---

## 8. P1: Prompt 优化（预读 + 2-agent） ✅ 已实施 (2026-05-30)

### 8.1 修改后的完整 Prompt

将 `pr-review-hotplex` cron job 的 `-m` 参数替换为以下内容：

```
你是 hrygo/hotplex 的自动化 PR Review 系统。身份: hotplex-ai。

## 铁律
1. §1 去重+CI门控是硬性门控，必须先输出结果再决定是否继续
2. 一个 PR 每次最多提交一条 review
3. 无 P0/P1 必须 APPROVE
4. 不确定标记 [UNCERTAIN]，不凑数

## §0 身份
export GH_TOKEN=$(cat <GH_TOKEN_PATH>)
gh api user --jq ".login"  # 必须 hotplex-ai

## §1 去重 + CI 门控（强制首步）

检查环境变量 TARGET_PR：
- 若 $TARGET_PR 非空 → 仅检查该 PR（webhook 触发）
- 若 $TARGET_PR 为空 → 检查所有 open PR（cron 兜底）

for PR in ${TARGET_PR:-$(gh pr list --repo hrygo/hotplex --state open --json number --jq '.[].number')}; do
  HEAD=$(gh pr view $PR --repo hrygo/hotplex --json headRefOid --jq '.headRefOid')
  COMMIT_DATE=$(gh api "repos/hrygo/hotplex/commits/$HEAD" --jq '.commit.committer.date')
  LAST_REVIEW=$(gh api "repos/hrygo/hotplex/pulls/$PR/reviews?per_page=100" --jq "[.[]|select(.user.login==\"hotplex-ai\")|select(.state!=\"DISMISSED\")]|max_by(.submitted_at).submitted_at//\"\"")
  NEED=false
  [ -z "$LAST_REVIEW" ] && NEED=true
  [ "$NEED" = false ] && [[ "$COMMIT_DATE" > "$LAST_REVIEW" ]] && NEED=true
  [ "$NEED" = false ] && { echo "SKIP PR#$PR (reviewed)"; continue; }
  CI=$(gh pr checks $PR --repo hrygo/hotplex 2>&1 || true)
  [ -z "$CI" ] && { echo "SKIP PR#$PR (no CI yet)"; continue; }
  echo "$CI" | grep -qiE "pending|queued|in progress" && { echo "SKIP PR#$PR (CI running)"; continue; }
  echo "$CI" | grep -qiE "fail|neutral|timed.out" && { echo "SKIP PR#$PR (CI failed)"; continue; }
  echo "REVIEW PR#$PR"
  TARGET=$PR
done
[ -z "$TARGET" ] && { echo "All reviewed."; exit; }
PR=$TARGET

## §2 预读（主进程执行一次，结果共享给 agent）

DIFF=$(gh pr diff $PR --repo hrygo/hotplex)
CLAUDE_MD=$(cat <REPO_ROOT>/CLAUDE.md)
# >8000 行仅保留最近 8000 行
DIFF_LINES=$(echo "$DIFF" | wc -l)
[ "$DIFF_LINES" -gt 8000 ] && DIFF=$(echo "$DIFF" | tail -8000)
# 汇总为共享上下文
SHARED_CTX="## PR#$PR Diff\n$DIFF\n\n## CLAUDE.md 摘要\n$CLAUDE_MD"

## §3 并行 Review（2 agent，共享上下文）

将 §2 的 SHARED_CTX 完整嵌入每个 agent 的 prompt。Agent 无需重新读取 diff 或 CLAUDE.md。

### Agent A — 正确性/安全/并发/合规（合并原 A+B）

Prompt:
"审查以下 PR diff。专注：安全漏洞、nil 解引用、竞态条件、逻辑错误、错误处理缺失、
goroutine 泄漏、锁序(mu→ms必须先mu)、持锁IO/写channel死锁、context未传播、
WG Add/Done 不配对、Mutex 嵌入/传指针、math/rand 加密、shell 执行、硬编码路径、
错误格式(Err哨兵+%w)、日志(slog)、同包内 DRY 重复、命名/错误处理与已有代码一致性。
忽略 linter 能抓的。

$SHARED_CTX

返回: [SEVERITY] 简述 (file:line) — 原因"

### Agent B — 历史/架构/性能/文档（合并原 C+D）

Prompt:
"审查以下 PR diff。专注：git blame 历史上下文、API 签名是否破坏下游、
migration 对生产数据影响、配置格式向后兼容、SRP(函数职责过多应拆)、
OCP(switch链应多态)、DIP(依赖具体应依赖接口)、N+1查询、不必要分配、
循环字符串拼接、大锁粒度可缩窄(仅热路径)、注释与代码不一致、过期TODO、
注释掉的代码、误导 godoc。

$SHARED_CTX

返回: [SEVERITY] 简述 (file:line) — 原因"

## §4 过滤
置信度 <75 丢弃。P0>P1>P2>P3 排序。
误报直接丢: 预存问题(PR未改行)、linter能抓、风格偏好(CLAUDE.md未要求)、
缺测试(通用)、功能变更(intentional)、lint-ignore静默、跨包DRY需新抽象、
大规模SOLID重构、非热路径性能。

## §5 提交
重跑§1确认，然后提交。P0/P1→REQUEST_CHANGES，仅P2/P3→COMMENT+APPROVE，全PASS→APPROVE。
格式: ## Code Review — hotplex-ai\nVerdict: ... | P0:X P1:X P2:X P3:X\n[发现列表]
inline: --input - '{"comments":[{"path":"f","line":N,"body":"..."}]}'
链接: https://github.com/hrygo/hotplex/blob/{FULL_SHA}/path#L{start}-L{end} ±1行

## §6 约束
Go 1.26+ tab|Mutex显式mu|Err哨兵+%w|slog|testify/require|filepath.Join
```

### 8.2 Prompt 变更摘要

| 变更 | 原始 | 优化后 | 效果 |
|------|------|--------|------|
| Agent 数量 | 4 并行 | 2 并行 | 上下文加载减半 |
| Diff 读取 | 每个 agent 独立 `gh pr diff` | 主进程预读 1 次，内嵌到 agent prompt | 消除 4× 重复文件读取 |
| CLAUDE.md | 每个 agent 独立 `cat` | 主进程预读 1 次，内嵌到 agent prompt | 消除 4× 重复 |
| Agent 分组 | A(安全) B(合规) C(历史) D(架构) | A(安全+合规) B(历史+架构) | 2 agent 覆盖 4 维度 |
| TARGET_PR | 无 | 检查环境变量，支持 webhook 定向触发 | 避免扫描全部 PR |
| CI 批准循环 | 始终执行 | 移除（CI 批准由 check_suite webhook 事件替代） | SKIP 轮 bash 调用减少 |

### 8.3 更新 Prompt

```bash
hotplex cron update pr-review-hotplex \
  -m "$(cat <<'PROMPT'
<上述完整 prompt 内容>
PROMPT
)"
```

---

## 9. Acceptance Criteria

### AC-1: Caddy 反代与网络隔离（§4）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-1.1 | Caddy 在公网 IP 的 443 端口监听，TLS 证书有效 | `curl -v https://<PUBLIC_IP>/api/webhook/github 2>&1 \| grep "SSL connection\|subject\|issuer"` |
| AC-1.2 | 仅 `/api/webhook/github` 路径可到达 gateway，其余路径返回 404 | `curl -s -o /dev/null -w "%{http_code}" https://<PUBLIC_IP>/` → `404`；`curl -s -o /dev/null -w "%{http_code}" https://<PUBLIC_IP>/ws` → `404` |
| AC-1.3 | 非 POST 请求返回 405 | `curl -s -o /dev/null -w "%{http_code}" https://<PUBLIC_IP>/api/webhook/github` → `405` 或 `400` |
| AC-1.4 | 请求体超过 1MB 被拒绝 | `dd if=/dev/urandom bs=1M count=2 \| curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" --data-binary @- https://<PUBLIC_IP>/api/webhook/github` → `413` |
| AC-1.5 | 现有 Tailscale 静态站点不受影响 | `curl -s -o /dev/null -w "%{http_code}" http://<TAILSCALE_IP>:80/` → `200` |
| AC-1.6 | Let's Encrypt 证书自动续期正常 | `echo \| openssl s_client -connect <PUBLIC_IP>:443 -servername <PUBLIC_IP> 2>/dev/null \| openssl x509 -noout -dates` |

### AC-2: GitHub Webhook 配置（§5）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-2.1 | Webhook 配置为 `application/json`，非 `form-encoded` | GitHub repo Settings → Webhooks → Recent Deliveries，检查 Content-Type |
| AC-2.2 | 仅订阅 `pull_requests` 和 `check_suite`/`check_run` 事件 | GitHub repo Settings → Webhooks → 编辑 webhook → 确认只有这 3 项勾选 |
| AC-2.3 | SSL verification 开启 | GitHub webhook 设置页面确认 |
| AC-2.4 | `ping` 事件返回 200 | GitHub Recent Deliveries 中 ping 事件 Response 为 `200 OK` |
| AC-2.5 | 无效签名的 delivery 返回 403 | GitHub Recent Deliveries 中发送 test delivery，确认非 200 |

### AC-3: Webhook Handler 签名验证（§6）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-3.1 | 无 `X-Hub-Signature-256` 头的请求返回 403 | `curl -s -o /dev/null -w "%{http_code}" -X POST -H "Content-Type: application/json" -d '{}' https://<PUBLIC_IP>/api/webhook/github` → `403` |
| AC-3.2 | 错误签名返回 403 | 用错误 secret 构造 `X-Hub-Signature-256: sha256=deadbeef` → `403` |
| AC-3.3 | 正确签名返回 200/202 | 用正确 secret + payload 构造签名 → `200`（ping）或 `202`（业务事件） |
| AC-3.4 | 签名比较为常量时间（防 timing attack） | 代码审查：使用 `hmac.Equal()`，非 `bytes.Equal()` 或 `==` |
| AC-3.5 | `webhook.secret` 为空时所有请求返回 403 | 设置 `webhook.secret: ""` → 所有请求被拒 |
| AC-3.6 | `webhook.enabled: false` 时路由不注册 | 启动日志无 webhook 路由注册，curl 返回 404 |

### AC-4: 事件过滤（§6）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-4.1 | `pull_request` + `opened` → 触发 review | 构造 payload 发送 → 日志显示 `webhook: trigger pr-review-hotplex pr=42` |
| AC-4.2 | `pull_request` + `synchronize` → 触发 review | 同上 |
| AC-4.3 | `pull_request` + `closed` → 不触发 | 发送 closed 事件 → 日志无 trigger，HTTP 200 |
| AC-4.4 | `pull_request` + draft=true → 不触发 | 发送 draft PR 事件 → 日志无 trigger，HTTP 200 |
| AC-4.5 | `check_suite` + `completed` + `conclusion: success` → 触发关联 PR review | 构造 payload → 日志显示 trigger |
| AC-4.6 | `check_suite` + `conclusion: failure` → 不触发 | 发送 failure 事件 → 日志无 trigger |
| AC-4.7 | 非 `hrygo/hotplex` 仓库的事件被忽略 | payload 中 `repository.full_name` 改为其他 → 日志 `unexpected repo`，HTTP 200 |
| AC-4.8 | `ping` 事件返回 200 且不触发 review | 发送 ping → 日志 `ping received`，无 trigger |
| AC-4.9 | 未知事件类型返回 200 且不触发 | 发送 `X-GitHub-Event: push` → 无 trigger，HTTP 200 |

### AC-5: 触发与执行（§6）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-5.1 | Webhook 请求在 1s 内返回 202，review 异步执行 | 发送事件 → HTTP 202 在 <1s 返回，gateway 日志后续显示 session 创建 |
| AC-5.2 | `TriggerByName` 传入 `target_pr` 环境变量到 worker | 检查 executor 注入 `platformKey["target_pr"]` 的代码路径 |
| AC-5.3 | Job 不存在时返回错误日志，不影响 HTTP 响应 | 调用不存在 job name → 日志 `job not found`，HTTP 仍为 202 |
| AC-5.4 | Job 被禁用时不触发 | `hotplex cron update pr-review-hotplex --enabled=false` → 触发被拒 |
| AC-5.5 | 同一 PR 的并发 webhook 事件不产生重复 review | 快速发送 2 个相同事件 → 仅触发 1 次 review（幂等保护） |
| AC-5.6 | Worker 执行使用与 cron 相同的 agent config 加载路径 | webhook 触发的 session 与 cron 触发的 session 使用相同的 bot_id/platform 配置 |

### AC-6: 限流（§6）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-6.1 | 正常请求速率（<2/s）全部通过 | 连续 5 个请求间隔 500ms → 全部 200/202 |
| AC-6.2 | 超过突发限制（>10/s）返回 429 | 连续快速发送 15 个请求 → 部分 `429 Too Many Requests` |
| AC-6.3 | 限流状态在短时间窗口内恢复 | 触发限流后等待 5s → 新请求正常通过 |

### AC-7: HotPlex 配置（§7）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-7.1 | `HOTPLEX_WEBHOOK_SECRET` 环境变量覆盖 `config.yaml` 的 `webhook.secret` | 设置环境变量 → 重启 → 环境变量值生效 |
| AC-7.2 | `webhook.path` 可配置，默认 `/api/webhook/github` | 不配置 → 路由注册在默认路径 |
| AC-7.3 | `webhook.max_body_size` 默认 1MB | 不配置 → 发送 1.1MB 请求被拒 |
| AC-7.4 | Cron 频率已从 3min 降为 30min | `hotplex cron get pr-review-hotplex` → `every:1800000` |
| AC-7.5 | 降频后 cron fallback 仍能正确执行门控 | 手动 `hotplex cron trigger pr-review-hotplex` → 正常执行 §0+§1 |

### AC-8: 端到端集成（E2E）

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-8.1 | 新建 PR → CI 通过 → webhook 触发 → review 提交（全链路） | 创建测试 PR → 等待 CI → 检查 PR 上出现 hotplex-ai 的 review |
| AC-8.2 | PR push 新 commit → CI 通过 → 重新 review | PR 上 push 修复 → CI 通过 → 出现新的 review（旧 review 已被去重跳过） |
| AC-8.3 | Draft PR 不触发 review | 创建 draft PR → CI 通过 → 无 review |
| AC-8.4 | Mark ready for review → 触发 review | Draft 转 ready → webhook 触发 → review 提交 |
| AC-8.5 | 已被 hotplex-ai review 的 PR 不重复审 | 人工确认 review 后推送新 commit → 仅新 commit 被审 |
| AC-8.6 | 服务重启期间漏掉的事件被 cron fallback 捕获 | 重启 gateway → 等待 30min → cron fallback 审查漏掉的 PR |

### AC-9: 安全

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-9.1 | 未加密 HTTP 请求被拒 | `curl http://<PUBLIC_IP>/api/webhook/github` → 拒绝或无响应 |
| AC-9.2 | Gateway 未暴露除 webhook 外的任何端点到公网 | 扫描 `https://<PUBLIC_IP>/` 的所有路径 → 仅 `/api/webhook/github` 非 404 |
| AC-9.3 | Webhook secret 不出现在 HotPlex 日志中 | `grep -r "secret" /var/log/hotplex/` → 无 secret 值 |
| AC-9.4 | Webhook secret 不出现在 Caddy 日志中 | `grep -r "secret" /var/log/caddy/` → 无 secret 值 |
| AC-9.5 | 畸形 JSON payload 不导致 panic | 发送 `{invalid json` → 返回 400，gateway 日志无 panic stack trace |
| AC-9.6 | 空 body 不导致 panic | 发送空 POST → 返回 403（签名验证失败），gateway 正常运行 |
| AC-9.7 | 超大 body（>1MB）被截断/拒绝 | 发送 >1MB payload → 返回 413 或 403 |

### AC-10: 代码质量

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-10.1 | `make check` 全通过（fmt + lint + test + build） | `make check` → exit code 0 |
| AC-10.2 | 单元测试覆盖签名验证全部路径（valid/invalid/missing/wrong-prefix） | `go test -v -run TestWebhookHandler_SignatureVerification ./internal/gateway/` → 全 PASS |
| AC-10.3 | 单元测试覆盖事件过滤全部场景 | `go test -v -run TestWebhookHandler_EventFiltering ./internal/gateway/` → 全 PASS |
| AC-10.4 | 无 `sync.Mutex` 嵌入或指针传递 | `make lint` → 无 mutex 相关 warning |
| AC-10.5 | 错误使用 `fmt.Errorf("%w")` 包装 | 代码审查 `webhook.go` + `executor.go` 的 error 返回 |
| AC-10.6 | 日志使用 `log/slog` | `grep -n "log\." internal/gateway/webhook.go` → 无 `log.Println` 等标准库调用 |

### AC-11: 成本与性能目标

| ID | 验收条件 | 验证方法 |
|----|---------|---------|
| AC-11.1 | 部署后 24h 内日均执行轮次 < 50 | `SELECT COUNT(*) FROM sessions WHERE source='cron' AND created_at > datetime('now', '-1 day')` |
| AC-11.2 | 部署后 24h 内 SKIP 轮次占比 < 30%（cron fallback 的 SKIP 除外） | 对比 webhook 触发 vs cron 触发的 session 数量 |
| AC-11.3 | REVIEW 单轮成本 < $2.50 | `SELECT AVG(cost_usd) FROM turns WHERE session_id IN (SELECT id FROM sessions WHERE source='cron') AND tokens_out > 2000` |
| AC-11.4 | Webhook 事件处理延迟 < 1s（HTTP 响应时间） | Caddy access log 中 webhook 请求的响应时间 |
| AC-11.5 | PR review 端到端延迟 < 10min（从 PR 事件到 review 提交） | GitHub webhook delivery 时间 vs review submitted_at |

---

## 11. 实施状态

| 章节 | 状态 | 说明 |
|------|------|------|
| §4 网络拓扑与安全 | 🔧 脚本就绪 | `scripts/setup-webhook-pr-review.sh`（需 sudo 执行）|
| §5 GitHub 侧配置 | 🔧 脚本就绪 | 脚本自动注册（需 `admin:repo_hook` scope）|
| **§6 HotPlex 代码改动** | **✅ 已实施** | **webhook.go + webhook_test.go + routes + executor + config** |
| **§7 HotPlex 配置** | **✅ 已实施** | **WebhookConfig + env 绑定（部署时配置 secret）** |
| **§8 Prompt 优化** | **✅ 已实施** | **2026-05-30 完成** |
| §9 迁移计划 | 🔧 脚本就绪 | 脚本自动降频 cron + 重启 gateway |

---

## 10. 迁移计划

### 阶段 1：安全加固（Day 1，无服务中断）

```bash
# 1. 配置 Caddy 反代
sudo cp /etc/caddy/Caddyfile /etc/caddy/Cibbonfile.bak
# 编辑 Caddyfile 添加公网 site block（仅 /api/webhook/github）
caddy validate --config /etc/caddy/Caddyfile
sudo systemctl reload caddy

# 2. 验证公网可达但被拒
curl -s -o /dev/null -w "%{http_code}" https://<PUBLIC_IP>/
# 期望: 404
curl -s -o /dev/null -w "%{http_code}" -X POST https://<PUBLIC_IP>/api/webhook/github
# 期望: 403 (签名验证失败)
```

### 阶段 2：代码实现 + 测试（Day 2-3）

```
1. fork → feature branch: feat/webhook-pr-review
2. 实现 webhook.go + webhook_test.go
3. 修改 routes.go, executor.go, config.go
4. make check 全通过
5. PR + review + merge
```

### 阶段 3：配置 + 切换（Day 3）

```bash
# 1. 生成 webhook secret
openssl rand -hex 32

# 2. 配置 GitHub webhook（§5.2）

# 3. 更新 HotPlex 配置
echo "HOTPLEX_WEBHOOK_SECRET=<secret>" >> <HOTPLEX_ENV_FILE>

# 4. 部署新版本并重启
hotplex gateway restart

# 5. 验证 webhook 连通（GitHub ping 事件）

# 6. 降低 cron 频率
hotplex cron update pr-review-hotplex --schedule "every:30m"

# 7. 更新 prompt（P1 优化）
hotplex cron update pr-review-hotplex -m "<新 prompt>"
```

### 阶段 4：观察 + 调优（Day 4-7）

- 监控 webhook 命中率和 cron fallback 触发频率
- 如果 webhook 覆盖率 > 95%，可考虑完全禁用 cron fallback
- 监控 REVIEW 单轮成本是否降至 $1.50-2.00 区间

---

## 12. 监控与回滚

### 12.1 监控指标

| 指标 | 来源 | 预期 |
|------|------|------|
| Webhook 请求/天 | Caddy access log | 5-50 |
| Webhook 200/202 响应率 | HotPlex gateway log | > 99% |
| Webhook 触发 → review 完成延迟 | Cron history `created_at` | < 10min |
| Cron fallback 触发/天 | `hotplex cron history` | < 5 |
| REVIEW 单轮成本 | `turns` 表 `cost_usd` | $1.50-2.00 |
| 每日总成本 | `turns` 表按日聚合 | $30-50 |

### 12.2 回滚方案

如果 webhook 出现问题：

```bash
# 1. 禁用 webhook（不删 GitHub 配置）
# 在 GitHub webhook settings 取消 Active

# 2. 恢复 cron 高频轮询
hotplex cron update pr-review-hotplex --schedule "every:3m"

# 3. 可选：恢复原始 prompt
hotplex cron update pr-review-hotplex -m "<原始 prompt>"

# 4. 可选：关闭 Caddy 公网监听
sudo cp /etc/caddy/Caddyfile.bak /etc/caddy/Caddyfile
sudo systemctl reload caddy
```

### 12.3 已知风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| Webhook secret 泄露 | 任何人可伪造事件触发 review | 轮换 secret + IP 层限制 |
| GitHub webhook 漏投 | PR 未被 review | 30min cron fallback 兜底 |
| 服务重启期间事件丢失 | 重启期间的 PR 变更被遗漏 | cron fallback + GitHub 重试（最长 ~20h） |
| Caddy 证书申请失败 | Webhook 不可达 | 监控证书过期 + HTTP fallback |
| Let's Encrypt 速率限制 | 首次配置失败 | 使用 staging 先验证，再切生产 |

---

## 附录 A: 数据模型

### Webhook 事件处理流水线

```
GitHub POST → Caddy (TLS + 路由隔离)
  → WebhookHandler
    ├─ MethodCheck  (POST only)
    ├─ RateLimit    (2/s burst 10)
    ├─ BodyRead     (≤1MB)
    ├─ HMACVerify   (sha256(secret, body))
    ├─ RepoFilter   (hrygo/hotplex only)
    ├─ EventFilter  (pull_request opened/sync/reopened/ready + check_suite completed/success)
    ├─ PRExtract    (draft=false, state=open)
    └─ AsyncTrigger (goroutine → CronExecutor.TriggerByName)
         → Executor.Execute
           → Bridge.StartSession
             → Claude Code Worker
               → §0-§5 (预读 + 2-agent)
```

### Cron fallback 触发条件

30 分钟兜底轮询中，只有以下情况会执行完整 review：
1. Webhook 漏投（服务重启 / 网络中断期间有 PR 变更）
2. CI 通过时 webhook 事件未被正确处理
3. 首次部署前积累的未审 PR

正常情况下 cron fallback 每轮都应输出 "All reviewed."（成本 ~$0.20 × 48 轮/天 = ~$10/天）。
