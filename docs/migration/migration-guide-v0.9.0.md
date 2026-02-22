*Read this in other languages: [English](migration-guide-v0.9.0.md), [简体中文](migration-guide-v0.9.0_zh.md).*

# HotPlex Developer Migration Guide (v0.8.x → v0.9.0)

This guide outlines the critical API and architectural changes introduced in HotPlex v0.9.0, focusing on **Observability Integration** and **SDK Synchronization**.

## 1. `GetSessionStats` Signature Change (Breaking Change)

### What Changed
To support strict session-based observability and telemetry, the `GetSessionStats` method now requires an explicit `sessionID`. This ensures that metrics are fetched for the correct context in high-concurrency "Hot-Multiplexing" scenarios.

### How to Migrate

**v0.8.x (Deprecated):**
```go
stats := engine.GetSessionStats()
```

**v0.9.0 (New):**
```go
stats := engine.GetSessionStats("my_session_id")
```

---

## 2. Default Security & Authentication

### What Changed
The server mode now defaults to a safer configuration. While v0.8.0 allowed unauthenticated access if no keys were configured, v0.9.0 introduces stronger warnings and prepares for mandatory authentication.

### How to Migrate
If you are running `hotplexd` in production, you **must** configure `HOTPLEX_API_KEYS`.

```bash
export HOTPLEX_API_KEYS="key1,key2"
```

The OpenCode HTTP/SSE compatible endpoints now also respect the same security configuration as the WebSocket gateway.

---

## 3. Observability Endpoints

### What Changed
HotPlex v0.9.0 introduces native support for Prometheus and OpenTelemetry.

### New Endpoints
- `/metrics`: Prometheus metrics (execution time, session count, errors).
- `/health`: Liveness and readiness probes for Kubernetes.

---

## 4. SDK Version Synchronization

### What Changed
All SDKs (Go, Python, TypeScript) are now being synchronized to the same versioning scheme.

### Recommendation
Update your dependencies to match the `v0.9.0` release tag for consistent behavior across multi-language environments.

- **Go**: `go get github.com/hrygo/hotplex@v0.9.0`
- **Python**: `pip install hotplex==0.9.0`
- **TypeScript**: `npm install @hrygo/hotplex@0.9.0`

---

*Last Updated: 2026-02-23*
