## [v1.0.0] - 2026-02-23

### 🚀 ChatApps-as-a-Service Milestone (v1.0.0)

This major release marks the transformation of HotPlex into a comprehensive **ChatApps-as-a-Service** platform. We've introduced a centralized engine integration layer that enables seamless connections between top-tier AI agents and various chat platforms (DingTalk, Discord, Slack, Telegram, WhatsApp).

### Added
- **ChatApps Integration Core**: 
  - `EngineHolder` and `EngineMessageHandler` for bridging chat platforms with the HotPlex engine.
  - `StreamCallback` providing real-time UI feedback for Thinking, Tool Use, and Results.
  - `ConfigLoader` for YAML-based multi-platform configuration.
- **Multi-Platform Support**: Official adapters for DingTalk, Discord, Slack, Telegram, and WhatsApp.
- **Enhanced Robustness**: 
  - Periodic session cleanup and stale session removal for adapter implementations.
  - Improved message queuing and retry logic for high-traffic chat scenarios.
- **Documentation**: New [ChatApps 接入层指南](docs/chatapps-guide.md) with architecture diagrams and platform comparison.

### Changed
- **Architecture**: Decoupled engine execution from platform-specific delivery logic.
- **SDKs**: TypeScript SDK promoted to officially supported status (Browser & Node.js).
- **Repo Maintenance**: Archived `roadmap-2026.md` as all core milestones for H1 2026 are achieved.

### Fixed
- **Code Quality**: Project-wide lint cleanup and formatting for the `chatapps` package.
- **Security**: Hardened terminal command validation in both WebSocket and ChatApp gateways.

---

## [v0.9.3] - 2026-02-23

### 🎉 Version Bump

Minor version update to reflect latest codebase changes.

### Changed
- **Version**: Bumped to v0.9.3

---

## [v0.9.2] - 2026-02-23

### 🛡️ Quality Audit Fixes v1.0
## [v0.9.2] - 2026-02-23

### 🛡️ Quality Audit Fixes v1.0

This version addresses critical findings from the first comprehensive quality audit, focusing on concurrency safety, security hardening, and error handling improvements.

### Fixed
 **Concurrency Safety**: Resolved race conditions in session pool management and event dispatching.
 **Security Hardening**: Fixed potential security issues identified in the audit report.
 **Error Handling**: Improved error propagation and cleanup in the engine lifecycle.
 **Documentation**: Added favicon to docs-site for proper browser tab icon display.

### Changed
 **Code Formatting**: Applied `go fmt` formatting across the codebase to maintain consistent style.

### Documentation
 Added comprehensive Quality Audit Report v1.0 (`docs/quality-audit-report.md`)


# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.9.0] - 2026-02-23

### 🌟 High-Performance Multi-Language & Observability Milestone

This version marks a significant evolution of HotPlex into a production-grade **AI Agent Control Plane**, shifting focus towards observability, multi-language support, and enterprise stability.

### Added
- **Official TypeScript SDK MVP**: Introduced a fully-typed JavaScript/TypeScript client in `sdks/typescript`. Supports both **Node.js** and **Browser** environments, enabling seamless integration of AI CLI agents into web dashboards and backend services.
- **Enterprise-Grade Observability**:
  - **OpenTelemetry Integration**: Implemented tracing for the entire execution lifecycle, from the gateway layer to individual tool invocations.
  - **Prometheus Metrics**: Exported real-time performance data (active sessions, error rates, tool usage) via the `/metrics` endpoint.
  - **Industrial Health Probes**: Added `/health`, `/health/ready`, and `/health/live` endpoints to support Kubernetes-native monitoring and liveness detection.
- **Reliability & Performance**:
  - **Hot Configuration Reload**: The server now watches for configuration changes using `fsnotify` and reloads without downtime.
  - **Stress Testing Suite**: Validated single-instance stability under 100+ concurrent AI sessions via new automated tests in `engine/stress_test.go`.
- **Documentation Overhaul**:
  - Launched the **VitePress Documentation Site** in `docs-site/`, featuring a cleaner UI and cross-linked SDK guides.
  - Added comprehensive guides for Docker execution and security best practices.

### Changed
- **Strategy Pivot**: Realigned all documentation to the **Cli-as-a-Service** model, moving away from simple CLI wrapping to providing a managed, stateful service layer.
- **Package Refactoring**: Optimized internal package structures for better modularity and cleaner separation of concerns.

### Fixed
- **Project-Wide Lint Cleanup**: Addressed multiple `errcheck`, `staticcheck`, and `unused` warnings to ensure the codebase meets high-performance Go standards.
- **Dependency Graph**: Fixed `go.mod` to correctly classify `fsnotify` and other essential libraries as direct dependencies.

## [v0.8.3] - 2026-02-22

### Added
- **Event Hooks System**: Introduced a pluggable hook system in `hooks/` package, supporting Webhooks and structured Logging.
- **Performance Benchmarks**: Added comprehensive benchmarking suite (`engine/benchmark_test.go`) and published the first official Performance Report (`docs/benchmark-report.md`).
- **SDK Enhancements**: 
  - Added public error aliases in the root `hotplex` package for better developer experience.
  - Added detailed error handling examples in `_examples/go_error_handling/`.

### Changed
- **Brand Positioning**: Pivot from "Control Plane" to **"Cli-as-a-Service"** engine, emphasizing the transformation of one-off CLI tools into persistent interactive services.
- **Documentation Overhaul**: Updated `README.md`, `README_zh.md`, `AGENT.md`, and Architecture documents to align with the new strategic positioning.
- **Roadmap 2026**: Published the updated roadmap for 2026 in `docs/roadmap-2026.md`.

### Removed
- **Aider Integration**: Formally removed all references and planned support for Aider to focus on Claude Code and OpenCode ecosystems.

### Fixed
- **Code Quality**: Resolved lint errors in the webhook implementation related to response body closing.

## [v0.8.2] - 2026-02-22

### Fixed
- **Provider (OpenCode)**: Resolved a critical issue where `OpenCodeProvider` failed to start sessions in CLI mode (Issue #17).
  - Implemented **Cold Start argument injection** to pass the initial prompt via the `--command` flag.
  - Added **Session ID normalization**, prefixing `ses_` to satisfy strict OpenCode CLI validation.
  - Removed unsupported `--mode` and `--non-interactive` flags that caused CLI parsing errors.
- **Engine**: Optimized cold start behavior to skip redundant `stdin` injection for providers that handle the initial prompt via command-line arguments.

## [v0.8.1] - 2026-02-22

### Fixed
- **Example Security**: Corrected invalid permission mode `bypass-permissions` to `bypassPermissions` in Go examples, fixing "session is dead" errors.
- **SDK Stability**: Fixed an unused variable in `go_opencode_lifecycle` example that prevented compilation.

### Changed
- **Example Optimization**: Updated Go and Node.js examples to demonstrate the new stateful `GetSessionStats(sessionID)` interface introduced in v0.8.0.
- **Internal Refactoring**: Renamed `CCSessionID` to `ProviderSessionID` across the engine and pool for better semantic consistency across different providers.

### Added
- **Migration Guides**: Added comprehensive developer migration guides for v0.8.0 (`docs/migration/migration-guide-v0.8.0.md` and its Chinese translation).
- **Bug Documentation**: Created a dedicated issue report for the OpenCode CLI startup bug in `docs/issues/` and tracked it in GitHub Issue #17.

## [v0.8.0] - 2026-02-22

### Added
- **OpenCode Provider Support**: Integrated `OpenCodeProvider` to support the OpenCode CLI ecosystem alongside Claude Code.
- **Dual-Protocol Proxy Server**: `hotplexd` now acts as a comprehensive proxy server supporting both native WebSocket and OpenCode-compatible HTTP/SSE protocols.
- **OpenCode HTTP API**: Implementation of `POST /session`, `GET /global/event` (SSE), and `POST /session/{id}/message` for seamless integration with OpenCode clients.
- **New Examples**: Added comprehensive Python and Go examples for the OpenCode provider and HTTP API.

### Changed
- **Server Package Refactoring**: Renamed and restructured server-related files (`hotplex_ws.go`, `opencode_http.go`, `security.go`) for better semantic clarity and maintainability.
- **Brand Normalization**: Unified project branding to lowercase `hotplex` across all documentation and visual assets.
- **Documentation Overhaul**: Synchronized all documentation (README, AGENT.md, SDK Guide) and architecture maps with the latest codebase and dual-protocol features.
- **SDK Naming Correlation**: Aligned `ClientRequest`/`ServerResponse` JSON field names with the internal protocol for better consistency.

### Refactored
- **Internal Engine Types**: Renamed `TaskSystemPrompt` to `TaskInstructions` and moved Engine/Session options to internal packages to prevent circular dependencies.
- **Session ID Persistence**: Enhanced mapping of business identifiers to deterministic UUID v5 sessions.

## [v0.7.4] - 2026-02-22

## [v0.7.3] - 2026-02-22

### Added
- **Structured Prompting (XML + CDATA)**: Prompts are now encapsulated in `<task>` and `<user_input>` tags with CDATA protection to prevent parsing interference from complex inputs or code snippets.

### Changed
- **Conditional Prompt Construction**: When `TaskInstructions` is empty, the user prompt is passed as raw text, maintaining simplicity and directness for simple queries.

### Fixed
- **OpenCode Turn Detection**: Fixed `DetectTurnEnd` in `OpenCodeProvider` to properly handle `EventTypeResult`.
- **Provider Tests**: Updated and validated all provider-specific unit tests.

## [v0.7.2] - 2026-02-22

### Added
- **Session-level Persistence**: `TaskInstructions` are now stored in the session and automatically reused across turns unless explicitly overridden.

### Changed
- **Terminology Refinement**: Renamed `TaskSystemPrompt` to `TaskInstructions` throughout the codebase, SDK examples, and WebSocket API (`instructions`) to better reflect its role as the user's objective.

## [v0.7.1] - 2026-02-21

### Added
- **Request ID Correlation**: WebSocket requests and responses now support `request_id` field for proper request-response tracking on shared connections

### Changed
- **SessionStats JSON**: Internal fields now excluded from serialization (`json:"-"`), standardized field naming in `ToSummary()`

## [v0.7.0] - 2026-02-21

### Added
- **Provider Abstraction**: Introduced `provider.Provider` interface for multi-CLI support (Claude Code, OpenCode, etc.)
- **Async WebSocket Execution**: Non-blocking task execution with context-based cancellation
- **New WebSocket Commands**: `version`, `stats` for observability and telemetry
- **Extended Error Types**: Added sentinel errors (`ErrSessionNotFound`, `ErrSessionDead`, `ErrTimeout`, `ErrInputTooLarge`, `ErrProcessStart`, `ErrPipeClosed`)

### Changed
- **Layered Architecture**: Refactored into clean package structure (`engine/`, `event/`, `types/`, `provider/`, `internal/`)
- **JSON Field Naming**: Standardized all API responses to `snake_case` for consistency
- **SDK Package Structure**: Flattened to root level for simpler imports

### Fixed
- **Concurrency Safety**: Resolved deadlock in `Shutdown()`, data race in `Session.close()`
- **Resource Leaks**: Properly close stdin/stdout/stderr pipes on session termination
- **Process Lifecycle**: `cmd.Wait()` now updates session status and notifies callbacks
- **Security Detection**: Added nested command, null byte, and control character detection in WAF
- **Windows Compatibility**: Used absolute path for `taskkill` in process termination

### Security
- **Admin Token Warning**: Added startup validation for admin token configuration
- **WAF Bypass Prevention**: Enhanced regex patterns to detect obfuscated malicious commands

### Refactored
- **Examples**: Consolidated WebSocket examples into unified `client.js` with full lifecycle demo


## [v0.6.2] - 2026-02-21

### Added
- **Project Governance**: Released the official **Project Audit Report (V2.1)**, documenting the roadmap for multi-layer isolation, semantic WAF, and plugin-based architectures.

## [v0.6.1] - 2026-02-21

### Refactored
- **Code Quality**: Addressed gocyclo warnings for cyclomatic complexity > 15 by extracting logic and helper methods in `runner.go` (`executeWithMultiplex`, `dispatchCallback`) and `session_manager.go` (`startSession`).

## [v0.6.0] - 2026-02-21

### Changed
- **Visual Identity**: Completely revamped the `README.md` and `README_zh.md` with high-quality SVG architectures (`features.svg`, `topology.svg`, `async-stream.svg`) and unified badging for better developer experience and premium look.

## [v0.5.2] - 2026-02-21

### Added
- **Project Guidelines**: Added `CLAUDE.md` to provide standardized build, test, and lint instructions for AI-assisted development.

## [v0.5.1] - 2026-02-20

### Fixed
- **Cross-Platform Compatibility**: Resolved build failures on Windows by abstracting Unix-specific syscalls (PGID isolation and signals) into OS-specific files using build tags.


## [v0.5.0] - 2026-02-20

### Added
- **Developer Experience (DX) Suite**: Added a colorized, self-documenting `Makefile` for streamlined development.
- **Robust Git Hooks**: Implemented a comprehensive suite of local Git hooks (`pre-commit`, `commit-msg`, `pre-push`) to ensure code quality and Conventional Commit adherence.
- **GitHub Metadata Optimization**: Enhanced repository with SEO-friendly descriptions, topics, and performance-focused taglines.

### Fixed
- **CI/CD Reliability**: Downgraded Go version to 1.24 across all workflows and `go.mod` to resolve `golangci-lint` compatibility issues.
- **Terminal Compatibility**: Standardized script outputs using `printf` to resolve garbled emoji characters on various terminal emulators.


## [v0.4.0] - 2026-02-20

### Added
- **CI/CD Pipelines**: Integrated GitHub Actions for automated Builds, Tests (with Race detection), and Linters.
- **Automated Releases**: Configured `GoReleaser` to automatically build and release multi-platform binaries (Linux, macOS, Windows) upon tag push.
- **Community Standards**: Added `LICENSE` (MIT), `CONTRIBUTING.md`, `SECURITY.md`, and Issue/PR templates to follow open-source best practices.
- **Documentation Localization**: Added a full English version of the architecture design document (`docs/architecture.md`) with cross-language navigation.
- **Unit Testing**: Added comprehensive unit tests for the `Danger Detector` (WAF) to verify security boundaries.

### Changed
- **Installation Docs**: Updated README to reflect Claude Code's native installation methods and official `go get` SDK integration.
- **Reference Syntax**: Standardized `AGENT.md` to use relative paths and updated reference syntax for better AI readability.
- **Architecture Files**: Renamed `architecture.md` to `architecture_zh.md` for the Chinese version.


## [v0.3.0] - 2026-02-20

### Added
- **Full-Lifecycle Examples**: Added comprehensive examples for both Go SDK (`full_sdk`) and WebSocket protocol (`full_websocket`), covering cold starts, hot-multiplexing, and session recovery.
- **Process Robustness**: Implemented `shutdownOnce` and enhanced `SIGKILL` logic to ensure clean termination of process groups (PGID).
- **GitHub Integration**: Official repository initialization and CI-ready structure.

### Changed
- **Documentation Overhaul**: Synchronized `architecture.md`, `README.md`, and `README_zh.md` to reflect the v0.2.0+ security posture (Native Tool Constraints).
- **WAF Refinement**: Updated `danger.go` to support context-aware interception and improved logging for security forensics.
- **Session Recovery**: Enhanced deterministic UUID v5 mapping to support seamless session resumption across engine restarts.

## [v0.2.0] - 2026-02-20

### Changed
- **Security Posture Pivot**: Removed `ForbiddenPaths`, `GlobalAllowedPaths`, and `SessionAllowedPaths` from the SDK. Since HotPlex wraps a native binary (Claude CLI), it cannot reliably intercept raw OS syscalls mid-flight.
- **Native Tool Constraints**: Replaced path restrictions with native `--allowed-tools` and `--disallowed-tools` configurations.
- **Engine-Level Exclusivity**: Tool capabilities (`AllowedTools` / `DisallowedTools`) are now strictly defined on the `hotplex.EngineOptions` struct. `Config` no longer holds any capability boundaries, enforcing a single source of truth for Sandboxing.

## [v0.1.0] - 2026-02-20

### Added
- **Core Engine**: Implemented `hotplex.Engine` singleton for routing and process multiplexing.
- **Session Manager**: `SessionPool` functionality to manage long-lived OS processes with deterministic UUID mapping for Hot-Multiplexing.
- **WebSocket Gateway**: Standalone `hotplexd` server supporting persistent bi-directional streams over `ws://`.
- **Pre-flight Sandbox**: Introduced regex-based WAF (`danger.go`) to inherently block destructive shell commands (`rm -rf`, network shells, etc).
- **Security Boundaries**: Global static boundaries vs Per-session dynamic contexts cleanly separated between `EngineOptions` and `Config`.
- **Example Projects**: Provided Go native integration examples (`basic_sdk`) and pure JavaScript UI examples (`websocket_client`).

### Changed
- Refactored `Config` API: Migrated `Mode`, `PermissionMode`, `ForbiddenPaths` to global `EngineOptions` to prevent sandbox escape via API abuse.
- Streamlined Session identification to accept completely arbitrary context strings globally without breaking UUID persistence constraints.
