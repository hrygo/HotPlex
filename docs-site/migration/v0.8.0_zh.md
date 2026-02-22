# HotPlex 开发者迁移指南 (v0.7.x → v0.8.0)

本指南概述了 HotPlex v0.8.0 中引入的关键 API 和架构变更，重点在于解决并发竞态条件（Race Conditions）和架构约束违反（SOLID）问题。

## 1. 有状态 SessionStats 管理（竞态条件修复）

### 变更内容
`hotplex.Engine`（以及公开的 `hotplex.SessionController` 接口）不再依赖全局共享的 Singleton 结构来管理 `SessionStats`。遥测和 Token 使用数据现在具有强作用域，围绕单个 `SessionID` 的生命周期进行确定性累积。

这彻底解决了在密集的热多路复用（Hot-Multiplexing）流量下，Session A 可能获取到 Session B 计费指标的竞态条件问题。

### 如何迁移
如果您直接使用 Go SDK 而不是服务端包装器（REST/WebSocket），则必须更新获取指标的方法签名以提供目标 `sessionID`。

**v0.7.x (已弃用):**
```go
engine.Execute(ctx, cfg, prompt, callback)
stats := engine.GetSessionStats() // 危险：热多路复用时容易产生竞态条件
```

**v0.8.0 (新):**
```go
engine.Execute(ctx, cfg, prompt, callback)
stats := engine.GetSessionStats("my_custom_session_id") // 安全：确定性的作用域
```

### 关键行为升级
> [!NOTE] 
> `SessionStats` 的属性（如 `TotalDurationMs`、`InputTokens` 和 `OutputTokens`）现在在持续活跃的 `intengine.Session` 生命周期内使用 `+=` 算术安全地**累加**。这解决了之前在连续对话中错误地清零早期轮次指标的问题。

## 2. 依赖倒置原则 (DIP) 修复

### 变更内容
底层 WAF 机制 `*security.Detector` 已被封装在可扩展的 `SecurityRule` 接口之后。因此，通过 `HotPlexClient` 向消费者暴露具体的 WAF 结构体违反了依赖倒置原则。

### 如何迁移
`GetDangerDetector()` 方法已从 `hotplex.HotPlexClient` 和 `hotplex.Engine` 中移除。

**如果您之前使用此方法绕过检查：**
请直接在 Engine/Client 上使用提供的抽象 Setter `SetDangerBypassEnabled(token string, enabled bool)`，而不是获取底层对象。

## 3. 服务端抽象层整合 (DRY)

*这主要影响提交 PR 的维护者，不影响外部 SDK 使用者。*

在 `internal/server/` 内部，`hotplex_ws.go` 和 `opencode_http.go` 中存在的冗余 Session 路由逻辑已被重构。现在，这两个连接网关都完全通过 `ExecutionController`（位于 `internal/server/controller.go`）进行路由。

任何新的接入传输层（例如 gRPC 或特定的厂商 Webhook 包装器）都应直接利用 `ExecutionController.Execute(...)`。
