# HotPlex Developer Migration Guide (v0.7.x → v0.8.0)

This guide outlines the critical API and architectural changes introduced in HotPlex v0.8.0, focusing strictly on resolving Concurrency Race Conditions and architectural constraint violations (SOLID).

## 1. Stateful `SessionStats` Management (Race Condition Fix)

### What Changed
`hotplex.Engine` (and the public `hotplex.SessionController` interface) no longer relies on a globally shared Singleton struct to manage `SessionStats`. Telemetry and token usage data are now strongly scoped, accumulating deterministically around the lifecycle of individual `SessionID`s.

This completely resolves the infamous Race Condition where dense Hot-Multiplexing traffic could cause Session A to fetch the billing metrics of Session B.

### How to Migrate
If you are directly interacting with the Go SDK instead of using the REST/WebSocket Server wrappers, you must update your metric fetching signatures to provide the target `sessionID`.

**v0.7.x (Deprecated):**
```go
engine.Execute(ctx, cfg, prompt, callback)
stats := engine.GetSessionStats() // Danger: Prone to race conditions during Hot-Multiplexing
```

**v0.8.0 (New):**
```go
engine.Execute(ctx, cfg, prompt, callback)
stats := engine.GetSessionStats("my_custom_session_id") // Safe: Scoped deterministically
```

### Key Behavioral Upgrade
> [!NOTE] 
> `SessionStats` properties such as `TotalDurationMs`, `InputTokens`, and `OutputTokens` now safely **aggregate** using `+=` arithmetic sequentially across the entire lifecycle of a continuously active `intengine.Session`. This resolves previous bugs where continuous dialogues incorrectly zeroed out early turn metrics.

## 2. Dependency Inversion Principle (DIP) Resolutions

### What Changed
The underlying WAF mechanism `*security.Detector` has been encapsulated behind the extensible `SecurityRule` interface. Because of this, exposing the concrete WAF struct to consumers via the `HotPlexClient` violates the Dependency Inversion Principle.

### How to Migrate
The method `GetDangerDetector()` has been purged from `hotplex.HotPlexClient` and `hotplex.Engine`.

**If you were using this to bypass checks:**
Use the provided abstract setter `SetDangerBypassEnabled(token string, enabled bool)` directly on the Engine/Client instead of fetching the underlying object.

## 3. Server-Layer Abstract Consolidation (DRY)

*This primarily affects maintainers submitting PRs, not external SDK consumers.*

Inside `internal/server/`, boilerplate session routing present in both `hotplex_ws.go` and `opencode_http.go` has been refactored. Both connection gateways now route entirely through `ExecutionController` (found in `internal/server/controller.go`).

Any new ingress transport layers (e.g. gRPC or specific vendor webhook wrappers) should leverage `ExecutionController.Execute(...)` directly.
