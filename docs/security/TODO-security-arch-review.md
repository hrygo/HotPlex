# Security 模块架构审计 TODO

> **来源**: Architecture Review Cycle 205 — [GitHub Issue #761](https://github.com/hrygo/hotplex/issues/761)
> **模块**: `internal/security`
> **方面**: resource-mgmt, performance, scalability

---

## P0 — 立即修复

### 1. 登录用户名枚举时序攻击

- **文件**: `local_account_provider.go:43-59`
- **问题**: 用户不存在时立即返回（0ms），密码错误时执行 bcrypt 约 200ms，攻击者可通过响应时间差异枚举有效用户名
- [x] 用户不存在或无密码哈希时，执行 dummy `bcrypt.CompareHashAndPassword` 对齐耗时
- [x] 添加测试验证存在与不存在用户的登录响应时间统计不可区分

### 2. API Key 验证 O(N) 循环分配

- **文件**: `auth.go:158-173`
- **问题**: `authenticateKey` 在每次 HTTP/WS 请求的热路径中执行 `[]byte` 转换 + 顺序遍历，堆分配压力大，且 early return 破坏常量时间特性
- [x] 在 startup/reload/CRUD 时预计算所有 API Key 的 SHA256 哈希，存入 `map[[32]byte]bool`
- [x] `authenticateKey` 改为一次 hash + map 查找（O(1)）
- [x] 验证所有 authenticator 单元测试通过

---

## P1 — 尽快修复

### 3. OIDC Discovery 无超时挂起

- **文件**: `oauth_provider.go:60-64`
- **问题**: `NewOAuthProvider` 调用 `oidc.NewProvider` 时使用默认 `http.DefaultClient`（无超时），若 IdP 端点不可达则配置热重载永久阻塞
- [x] 通过 `oidc.ClientContext` 注入 `&http.Client{Timeout: 10 * time.Second}`
- [x] 添加测试验证慢速/不可达 issuer 在超时窗口内中止

### 4. DBResolver 缓存穿透 DoS

- **文件**: `apikey_resolver.go:93-123`
- **问题**: 无效 API Key 查询返回 `sql.ErrNoRows` 时不缓存负结果，攻击者可用无效 Key 绕过缓存直接打 DB
- [x] 对 `sql.ErrNoRows` 缓存负结果（TTL 5 秒）
- [x] 添加测试验证连续无效 Key 请求在 TTL 内只查询 DB 一次

---

## P2 — 计划修复

### 5. DBResolver 缓存内存泄漏

- **文件**: `apikey_resolver.go:61-123`
- **问题**: `sync.Map` 缓存仅被动淘汰（再次查询时才删除过期条目），Key 轮换场景下过期条目永久驻留
- [x] 实现后台清理 goroutine（ticker 定期扫描删除过期条目）
- [x] 确保清理 goroutine 在系统关闭时干净退出
- [x] 添加测试验证过期条目被自动清理

---

## 验证清单

- [x] `go test -v ./internal/security/...` 全部通过
- [ ] `golangci-lint run ./internal/security/...` 无新警告
- [ ] `make test` 无回归
