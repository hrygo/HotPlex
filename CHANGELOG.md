# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
