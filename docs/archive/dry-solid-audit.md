# 🏗️ HotPlex DRY / SOLID Architecture Audit Report

**Date**: 2026-02-22
**Focus**: Structural integrity, maintainability, and extensibility against DRY (Don't Repeat Yourself) and SOLID design principles.

## 🎯 Executive Summary
The recent introduction of the `provider.Provider` abstraction (v0.7.0+) and the dual-protocol proxy server (v0.8.0) significantly improved the hotplex architecture. The system excels in ISP (Interface Segregation Principle) via the well-designed `HotPlexClient` interface tree. However, substantial technical debt remains in the `engine` and `security` packages, violating SRP, OCP, and DIP.

---

## 🚨 1. SRP (Single Responsibility Principle) Violation
**Severity:** HIGH
**Location:** `engine/runner.go` (Specifically lines 530+ through 840+)

### The Issue: "The God Object Leak"
Despite the creation of the `provider` package to handle tool-specific parsing, the `hotplex.Engine` remains heavily burdened with legacy Claude Code parsing logic. Over 300 lines of code (`dispatchCallback`, `handleThinkingEvent`, `handleToolUseEvent`, `handleAssistantEvent`, etc.) still exist in `runner.go` to deal with the deprecated `types.StreamMessage` structure.

### The Impact
The `Engine` holds two concurrent responsibilities:
1. High-level orchestrator of OS Processes (PGID) and Hot-Multiplexing.
2. Low-level JSON abstract syntax tree (AST) parser for legacy events.

### Recommended Fix (Refactor)
- **Purge Dead Code**: Delete the `dispatchCallback` and all legacy `handle[Event]` methods from `engine/runner.go`. 
- **Enforce Provider Abstraction**: Ensure `Engine` **only** communicates via `provider.ProviderEvent`. The fallback logic in `createEventBridge` should be cleaned up so the engine merely routes standard `pevt` events without touching CLI-specific payload mapping.

---

## 🚨 2. DRY (Don't Repeat Yourself) & Separation of Concerns
**Severity:** MEDIUM
**Location:** `internal/server/hotplex_ws.go` vs `internal/server/opencode_http.go`

### The Issue: Duplicated Engine Execution Hooks
Both the WebSocket and OpenCode SSE handlers manually configure context timeouts, instantiate `hotplex.Config` payloads, and invoke `engine.Execute()` with complex closure wrappers. 

### The Impact
If hotplex needs to introduce global rate-limiting, distributed tracing spans, or new engine context requirements, developers will have to duplicate this operational logic across every protocol adapter.

### Recommended Fix (Refactor)
- **Extract Controller Layer**: Introduce an `AdapterService` or shared controller function within `internal/server` that orchestrates the `hotplex.HotPlexClient.Execute` call. Protocol adapters (WS/HTTP) should simply parse the transport layer and pass a normalized Request struct to the unified Controller.

---

## 🚨 3. OCP (Open/Closed Principle) Violation
**Severity:** MEDIUM
**Location:** `internal/security/danger.go`

### The Issue: Rigid Security Rules
The Web Application Firewall (WAF) detector is built as a concrete struct `security.Detector` filled with hardcoded regex patterns (`isDangerousCommand`, command injection checks, null byte checks). 

### The Impact
To add a new security heuristic (e.g., Semantic intent validation via external AI, or custom organizational policies), developers must directly modify the core internal `danger.go` file. It is closed for extension.

### Recommended Fix (Refactor)
- **Rule Interface Pattern**: Define a `type SecurityRule interface { Evaluate(cmd string) error }`.
- **Registry**: Update `Detector` to act as an execution chain for a slice of `[]SecurityRule`. Provide default registry lists, allowing enterprise users to inject custom `SecurityRule` integrations.

---

## 🚨 4. DIP (Dependency Inversion Principle) Violation
**Severity:** LOW
**Location:** `engine/runner.go` Interface Exports

### The Issue: Concrete Struct Dependency
While `hotplex.HotPlexClient` defines a great `SafetyManager` interface (`SetDangerBypassEnabled`, etc.), the underlying `Engine` exposes a concrete pointer method: `GetDangerDetector() *security.Detector`.

### The Impact
This forces anything interacting deeply with the Engine's security layer to depend on the `internal/security` package directly rather than depending on an abstraction constraint.

### Recommended Fix (Refactor)
- Change `GetDangerDetector()` to return a defined interface `SafetyInspector` (or similar) that `security.Detector` implements, fully decoupling the SDK layers.

---

## 🗺️ Execution Roadmap

| Phase       | Task                                                        | Effort | Impact                                   |
| :---------- | :---------------------------------------------------------- | :----- | :--------------------------------------- |
| **Phase 1** | Purge `engine/runner.go` of legacy event parsers.           | Low    | Huge (Removes 300+ LOC)                  |
| **Phase 2** | Extract Controller shared execution from `internal/server`. | Mid    | High (Prevents protocol rot)             |
| **Phase 3** | Refactor `security` package to `SecurityRule` interface.    | Mid    | High (Provides Enterprise Extensibility) |
| **Phase 4** | Fix DIP exposures on `Engine.GetDangerDetector()`.          | Low    | Mid (Pure architectural hygiene)         |
