# CHANGELOG.md

## [v0.20.0] - 2026-03-06

### 🐳 Docker All-in-One Deployment & Multi-Bot Architecture

This release introduces a comprehensive Docker all-in-one deployment model with full Claude Code integration, multi-bot isolation support, and GHCR publishing pipeline.

### Added

#### Docker All-in-One Image
- **Complete Development Toolchain** - Single image includes Go 1.25, Node.js, Claude Code CLI, golangci-lint, air (hot reload), delve (debugger), and essential CLI tools (ripgrep, fd, fzf, eza, jq, yq, etc.).
- **Claude Code Integration** - Pre-installed `@anthropic-ai/claude-code@latest` for seamless AI-assisted development inside containers.
- **WebSocket Debugging** - Included `websocat` for WebSocket gateway debugging.
- **Go Cache Persistence** - Named volumes for `go/pkg/mod` and `.cache/go-build` to accelerate repeated builds.

#### Multi-Bot Support
- **Secondary Bot Instance** - `docker-compose.yml` now defines both `hotplex` (primary) and `hotplex-secondary` services.
- **Bot Isolation** - Each bot uses isolated project directories (`~/.slack/BOT_<ID>`) and separate Git configurations.
- **Environment Separation** - Primary bot uses `.env`, secondary bot uses `.env.secondary` for independent Slack credentials.

#### CI/CD & Publishing
- **GHCR Integration** - Automated Docker image publishing to GitHub Container Registry via GoReleaser.
- **Docker Makefile Targets** - New targets: `docker-build`, `docker-up`, `docker-down`, `docker-logs`, `docker-sync`.

### Fixed
- **ENTRYPOINT Path** - Fixed container startup failure by using absolute path `/app/hotplexd` instead of relative `hotplexd`.
- **Secondary Bot Token** - Fixed `hotplex-secondary` to correctly load `.env.secondary` instead of `.env`, ensuring each bot connects with its own Slack credentials.
- **Docker Path Resolution** - Fixed critical typo in volume mount (`${HOME}.slack` → `${HOME}/.slack`).
- **Work Directory Mapping** - Unified container work directory to `/home/hotplex/projects/hotplex`.

### Changed
- **Config Loading Strategy** - Primary bot relies on user-level configs (`~/.hotplex/configs`) synchronized via `make docker-sync`.
- **State Persistence** - Full `.claude/` and `.claude.json` mounting for complete conversation and preferences persistence.


## [v0.19.0] - 2026-03-05

### 🚀 Processor Chain Refactoring & Configuration Optimization

This release delivers significant architectural simplifications to the Processor Chain, improves platform initialization efficiency, and introduces comprehensive documentation updates.

### Added
- **Slack Free Plan Compatibility** - Added `docs/plans/slack_free_plan_compatibility.md` documenting compatibility considerations for Slack's free tier limitations.
- **Architecture SVG Diagram** - Enhanced `docs-site/reference/chatapps.md` with a polished SVG-based architecture visualization for better documentation clarity.

### Changed
- **DRY/SOLID Processor Chain** - Removed redundant processors (`processor_aggregator`, `processor_rate_limit`, `processor_zone_order`, `processor_chain`) and consolidated logic into cleaner, single-purpose components.
- **Efficient Platform Initialization** - Platforms now skip initialization when required environment variables are missing, avoiding unnecessary YAML config loading and engine creation.
- **Transient Message Tracking** - Refactored message tracking to use explicit `is_transient` metadata flag instead of zone-based filtering, improving clarity and maintainability.
- **Enhanced Initialization Logging** - Added detailed startup logs showing which platforms were initialized and from which source (config file vs environment variables).
- **Config Key Renaming** - Renamed `MaxBytes` to `MaxRunes` in all configurations for semantic accuracy with CJK content support.

### Fixed
- **Config Loading Priority** - Fixed environment variable override behavior to properly respect the intended precedence order.
- **Status Label Consistency** - Consolidated all AI status labels into named constants in `engine_handler.go` for better maintainability.

### 🚀 Processor Chain Optimization & Rune-based Buffering

This release delivers significant architectural refinements to the Processor Chain and implements rune-based (character) counting for correct handling of CJK content in Slack.

### Added
- **Non-blocking Rate Limiting** - Refactored `RateLimitProcessor` to a non-blocking drop model with new `RateLimitDroppedTotal` Prometheus metric.
- **Ordering Guards** - Added compile-time static checks to ensure `ZoneOrderProcessor` always runs before `MessageFilterProcessor`.

### Changed
- **DRY/SOLID Optimization** - Eliminated triple-redundant filtering logic and removed the empty `RichContentProcessor` shell.
- **Rune-based Counting** - All buffer limits and flush thresholds in `MessageAggregatorProcessor` and `NativeStreamingWriter` are now rune-based (characters) instead of bytes, providing full support for Chinese and multi-byte content.
- **Config Cleanup** - Removed dead entries from `defaultEventConfig` to improve maintainability.

### Fixed
- **AppendStream Error Visibility** - Fixed silent error swallowing in Slack streaming; errors are now properly logged with context.
- **Consistent Naming** - Renamed `MaxBytes` to `MaxRunes` across all configurations, structs, and metrics for semantic consistency.

## [v0.18.2] - 2026-03-05

### 🚀 Robust Restart & Slack Assistant Stability

This release delivers major improvements to the development workflow with a robust daemon restart mechanism and critical stability fixes for the Slack Assistant API.

### Added

#### Robust Restart Mechanism
- **Restart Helper Script** - New `scripts/restart_helper.sh` providing a POSIX-compliant, cross-platform process management lifecycle.
- **Port Cleanup** - Automated waiting for old process termination and port release to prevent "Port already in use" errors during restart.
- **Atomic Verification** - Explicit validation of the new process PID and Commit ID to ensure the latest code is actually running.
- **Improved Makefile** - Updated `make restart` and `make stop` targets with better feedback and configuration transparency.

### Fixed

#### Slack Assistant API Stability
- **Thread Context Enforcement** - Fixed `invalid_arguments` errors in `chat.startStream` and `assistant.threads.setStatus` by ensuring `thread_ts` is always provided.
- **Automatic Thread Fallback** - Implemented automatic fallback to message `ts` when `thread_ts` is missing, ensuring all top-level interactions correctly initialize as threads.
- **Socket Mode Consistency** - Unified thread ID extraction across both HTTP Webhooks and Socket Mode events.

### Documentation

- **Dev vs. Prod Restarts** - Clarified the distinction between `make restart` (rebuild & run) and `make service-restart` (system service management) in the Makefile documentation.

---

## [v0.18.1] - 2026-03-05

## [v0.18.0] - 2026-03-04

### 🚀 Native Brain & ChatApps Platform Expansion

This release introduces the **Native Brain** system for intelligent LLM routing, complete **Feishu (飞书) adapter** support, **Slack Native Streaming**, and comprehensive **Danger Block WAF** security闭环. ChatApps is now positioned as the primary access channel.

### Added

#### Native Brain System (#177)
- **LLM Router** - Intelligent multi-LLM routing with priority-based failover, circuit breaker, and rate limiting.
- **Budget Management** - Token budget tracking and cost control across multiple LLM providers.
- **Health Monitoring** - Provider health checks with automatic failover and recovery.
- **Cache Layer** - Multi-tier caching (Redis-compatible) for LLM responses.
- **Retry & Circuit Breaker** - Exponential backoff retry with configurable circuit breaker patterns.
- **Metrics & Observability** - Comprehensive metrics collection for LLM calls, costs, and performance.

#### Feishu (飞书) Adapter
- **Complete Adapter Implementation** - Full-featured Feishu custom bot integration.
- **Card Builder** - Rich card message templates (Thinking, ToolUse, Permission, Answer, Error, SessionStats).
- **Interactive Handler** - Button callbacks, URL verification, permission dialogs.
- **Command Handler** - `/reset`, `/help` and other slash commands support.
- **Signature Verification** - HMAC-SHA256 webhook signature validation.
- **Pressure Testing Framework** - Load testing tools for production readiness.

#### Slack Native Features
- **Native Streaming** - Real-time token-by-token streaming using Slack Block Kit.
- **Assistant Status API** - Visual indicator showing AI thinking/responding state.
- **Socket Mode** - Full Socket Mode support for real-time bidirectional communication.
- **Slash Commands** - `/ai`, `/reset`, `/help` command support.
- **Interactive Messages** - Button interactions, modal dialogs, block actions.

#### Security Enhancements
- **Danger Block WAF 闭环** - Interactive security confirmation blocks requiring user approval before executing dangerous commands.
- **Rate Limiting** - Per-user and per-channel rate limiting to prevent abuse.
- **Security Hardening** - Enhanced permission policies and user filtering.

#### CLI Enhancements
- **--config Flag** - New command-line parameter to specify ChatApps config directory (highest priority).

### Changed

#### Documentation & Quick Start
- **ChatApps as Primary Channel** - Updated Quick Start and README to position ChatApps as the recommended access method.
- **Platform-First Documentation** - Comprehensive manuals for Slack, Feishu, and other platforms.
- **Quick Start Simplification** - Streamlined README with links to detailed docs.

#### Architecture Refactoring
- **Engine Handler Simplification** - Refactored `engine_handler.go` for cleaner separation of concerns.
- **Processor Chain Cleanup** - Removed redundant processors and streamlined the chain.
- **Status Manager** - New internal status management for AI state tracking.

### Fixed

#### Rate Limiting
- **Dedup TTL Adjustment** - Reduced deduplication TTL from 30s to 5s to prevent normal messages being skipped.
- **Rate Limit Bug** - Fixed rate limiting logic to properly handle burst traffic.

#### Session Management
- **Session Cleanup Race Condition** - Fixed race condition in `/reset` command by ensuring process termination before marker deletion.
- **Stale Session Files** - Added automatic deletion of stale CLI session files to prevent "Session ID is already in use" errors.
- **Engine API Consolidation** - Unified session cleanup into Engine API, removing duplicated code.

#### Bug Fixes
- **Issue #130** - Removed unused result variable.
- **Lint Errors** - Fixed various lint warnings across Feishu adapter.

### Documentation

- **Quick Start Guides** - English and Chinese quick start documentation with ChatApps as primary method.
- **Slack Manual** - Comprehensive Slack integration manual with Socket Mode, streaming, and Assistant Status.
- **Feishu Manual** - Complete Feishu (飞书) adapter documentation in both languages.
- **Production Checklists** - Platform-specific production deployment checklists.
- **Architecture Docs** - Updated architecture documentation reflecting Native Brain and ChatApps-first design.
- **AI Native UX Plan** - v3.0 AI Native UX implementation plan with black hole and space folding concepts.

### Resolved Issues

- Closes #177 - NativeBrain production-grade enhancements (Phase 1)
- Closes #150/#168 - Slack native assistant integration and security enhancements
- Closes #168 - Feishu configuration fix
- Closes #167 - Feishu manual creation
- Closes #166 - Feishu bilingual documentation
- Closes #165/#141/#143 - Feishu Phase 3 production readiness
- Closes #149 - ChatApp message storage plugin design
- Closes #148/#140 - Feishu Phase 2.3 command handler
- Closes #147 - Session cleanup v0.17.0
- Closes #146/#138 - Feishu Phase 2 card builder
- Closes #144/#134 - Feishu Phase 1 adapter
- Closes #132 - Dedup TTL fix
- Closes #129 - Dedup TTL optimization

---

## [v0.17.0] - 2026-03-02

### 🚀 ChatApps UX & Stability Enhancements

This release focuses on ChatApps user experience refinements, goroutine safety improvements, and session lifecycle optimizations. The core enhancement is the **6-Zone UX Architecture** for Slack with sliding window management, plus comprehensive fixes for goroutine context propagation and panic recovery.

### Added

#### 6-Zone UX Architecture (#122)
- **Slack UI Zone System** - Refined UX with 6 distinct interaction zones for optimal information density and action availability.
- **Action Zone Window** - Limited to 2 active elements to reduce cognitive load and prevent Block Kit overflow errors.
- **Thinking Zone Improvements** - Fixed 64-character truncated scrolling display with 1-second update throttle to prevent rate limit hits.
- **UI Specification** - Updated `docs/chatapps/ux-zone-spec.md` documenting the 5-layer zoning design philosophy.

#### Session Resume Detection
- **IsResuming Flag** - New `StreamCallback` field to distinguish hot-multiplex (resume) from cold start sessions.
- **Context Preservation** - Enables adapters to render different UI states based on session continuity detection.

#### Platform-Specific Security Config
- **YAML Configuration** - Support for platform-specific security and permission settings in centralized config files.
- **Adapter Factories** - Enhanced factory pattern with nil-return handling to prevent initialization panics.

### Changed

#### Session Lifecycle Refactoring
- **TurnState Elimination** - Removed redundant `TurnState` abstraction in favor of streamlined session/message lifecycle management.
- **Deterministic Session IDs** - Slack adapter now generates deterministic session IDs for top-level messages, enabling reliable thread tracking.
- **Double-Deletion Prevention** - Resolved race conditions in session cleanup APIs that could cause concurrent map deletion panics.

#### Block Kit Formatting
- **Emoji Handling** - Simplified emoji formatting in Block Kit messages using SDK-native methods.
- **Session ID Display** - Shortened session ID presentation in Slack messages for improved readability.

#### Thinking Stream UX
- **Removed Answer Header** - Eliminated redundant "Answer" header text for cleaner message presentation.
- **Turn Complete Indicator** - Removed explicit "Turn Complete" text in favor of implicit state via action zone rendering.

### Fixed

#### Goroutine Safety (#64)
- **Context Propagation** - Ensured all goroutines respect `ctx.Done()` for proper cancellation and resource cleanup.
- **Panic Recovery** - Added `recover()` in `ProcessorChain.Close()` to prevent cascading panics during shutdown sequences.
- **Timer Leak Prevention** - Fixed potential goroutine leaks in processor aggregation logic.

#### Initialization Race Conditions
- **Adapter Factory Nil Handling** - Prevented panics when adapter factories return nil due to configuration errors.
- **Reaction Timing** - Moved `setReaction` calls before early returns in `handleThinking` to ensure consistent emoji feedback.

#### Script Improvements
- **macOS Launchctl** - Corrected PID extraction logic in launch daemon scripts for reliable process management.
- **CI Arithmetic** - Fixed arithmetic evaluation failures under `set -e` in SVG-to-PNG conversion scripts.

#### Daemon & Session Management
- **Make Restart POSIX Compliance** - Replaced `nohup` with POSIX-compliant stdin redirection (`< /dev/null`) in daemon restart target to ensure background process survives Ctrl+C and works across all Unix platforms.
- **Reset Command Race Condition** - Fixed `/reset` command execution order (terminate process → delete marker → delete session file) to prevent "Session ID is already in use" errors caused by concurrent message processing recreating markers after deletion.
- **Session Cleanup Consolidation** - Refactored session file deletion into unified Engine API, removing duplicated cleanup code across ResetExecutor and SessionPool. Added `CleanupSession` method to Provider interface for extensible session file management.

#### Engine Session File Management
- **Stale Session File Cleanup** - Added automatic deletion of stale CLI session files (`.jsonl`) on session start to prevent "Session ID is already in use" errors after daemon restart.
- **Provider Abstraction** - Added `CleanupSession` to Provider interface with default no-op implementation; ClaudeCodeProvider now properly deletes `.jsonl` files.

### Documentation

- **Slack Architecture** - Fixed broken link to `slack-architecture.md` in developer documentation.
- **UX Zone Spec** - Updated specification document reflecting the 5-layer zoning design.
- **Slack App Manifest** - Updated to standard JSON format and synchronized latest SVG assets.

### Resolved Issues

- Closes #122 - 6-Zone UX Architecture and sliding window management
- Closes #64 - Goroutine context propagation and panic recovery
- Resolves initialization race conditions in ChatApps adapter layer
- Fixes macOS daemon management script reliability

---

## [v0.16.0] - 2026-03-01

### 🔧 Code Quality & Defensive Architecture Enhancements

This release delivers major code quality improvements, strict defensive programming patterns, and enterprise-grade reliability enhancements across the ChatApps layer, while introducing our comprehensive "Craw Layer" Slack extension strategy.

### Added

#### Event Deduplication (#95)
- **New `chatapps/dedup` Package** - High-performance LRU cache-based event deduplication designed for concurrent webhook environments.
- **Thread-Safe Architecture** - Backed by `sync.RWMutex` and an isolated `cleanupLoop` goroutine to prevent memory accumulation.
- **Seamless Integration** - Integrated into `WebhookRunner` providing robust deduplication at 84ns/op (single-threaded) preventing sandbox re-execution.

#### Log Redaction (#59)
- **RedactSensitiveData Function** - Zero-intrusion automatic redaction of sensitive tokens (Slack `xoxb-*`, GitHub `ghp_*`, API Keys) before they hit the log streams.
- **Pre-compiled Detection** - High-speed pattern matching (237ns/op) ensuring sensitive credentials never leak into persistent storage.

### Changed

#### Defensive Architecture Enhancements (#106)
- **Timer Leak Prevention** - Added strict `p.ctx.Err() == nil` validation before and inside timer callbacks in `processor_aggregator.go`, eliminating goroutine panics and memory leaks on canceled contexts.
- **Robust Reset Fallbacks** - Established a fallback to `os.TempDir()` in `ResetExecutor` if `os.Getwd()` fails, preventing nil pointer exceptions or system crashes during path resolution.
- **Compile-Time Interface Verification** - Enforced `var _ MessageProcessor = (*XXXProcessor)(nil)` checks across all Processors to eliminate run-time interface mismatch risks.
- **Silent Error Mitigation** - Logged previously swallowed errors in `AdapterManager.Unregister()` to improve debuggability.

#### Documentation & Strategy
- **"Craw Layer" Strategy** - Published `docs/chatapps/slack-extensions-strategy.md` outlining HotPlex's vision as an Enterprise Agentic Execution Engine (Craw Layer) with HITL governance and interactive sandboxes.
- **Site Assets** - Added brand-new custom CSS variables, SVG architecture diagrams (`topology.svg`), mascot, and OpenGraph images to `docs-site`.

### Fixed
- **PR Checks Security** - Replaced direct `createCommitStatus` API calls with native GitHub Actions standard job statuses (`exit 1` / `::error::`), removing the unnecessary `statuses: write` permission requirement and fixing cross-repository PR validation for forks.
- **CI Compatibility** - Switched ImageMagick invocation from `magick` to `convert` to restore backwards compatibility with Ubuntu 24.04 runners (ImageMagick 6.x).

### Resolved Issues
- Closes #106 - Code quality & defensive programming improvements
- Closes #95 - Event deduplication
- Closes #59 - Log redaction
- Closes #96 - `/reset` command enhancement

---

## [v0.15.7] - 2026-03-01

### 📦 Asset & Documentation Updates

Minor release with logo updates and adapter improvements.

### Changed
- **README Logo** - Replaced SVG logo with PNG version (`hotplex-og.png`) in both English and Chinese READMEs.
- **Brand Assets** - Updated logo SVG/PNG assets and favicon.
- **Adapter Formatting** - Improved formatting consistency across chatapps adapters.

---

## [v0.15.6] - 2026-03-01

### 🎨 Brand Asset Optimization & CI Refinement

This release establishes a strict Single Source of Truth (SSOT) for branding assets and optimizes the documentation deployment pipeline.

### Added
- **Single Source of Truth (SSOT)**: Designated `docs/images/logo.svg` as the primary source for all logo-based assets across the repository.
- **Sync Workflow**: Implemented automated synchronization logic to ensure brand consistency between documentation site assets and GitHub profile branding.

### Changed
- **Asset Generation Scripting**: Optimized `scripts/generate_assets.sh` to dynamically produce multi-format outputs (PNG, ICO, favicon) directly from SVG sources.
- **CI/CD Optimization**: Refined the `deploy-docs.yml` workflow to efficiently handle asset generation and deployment during the documentation build process.
- **Visual Refinement**: Improved layout and element spacing in `session-lifecycle.svg` for better readability and structural integrity.

---

## [v0.15.5] - 2026-03-01

### 🏗️ Interface-Based Architecture Refactoring

This release delivers a major architectural refactoring of the ChatApps layer, eliminating platform-specific type assertions through dependency injection and interface-based design.

### Added
- **MessageOperations Interface** - New interface for platform-agnostic message operations (DeleteMessage, AddReaction, RemoveReaction, UpdateMessage).
- **SessionOperations Interface** - Abstracted session management operations for better testability.
- **Engine Interface Abstraction** - Decoupled ChatApps from concrete engine implementation via dependency injection.
- **Interface Compliance Tests** - Comprehensive test suite (278 lines) verifying interface implementations across all adapters.
- **Security & Troubleshooting Guides** - New documentation for security overview and operational troubleshooting.
- **Architecture Diagrams** - Added architecture-flow, security-layers, and session-lifecycle SVG diagrams.

### Changed
- **Type Assertions Eliminated** - Removed 16+ platform-specific type assertions (`adapter.(*slack.Adapter)`) in favor of interface-based dependency injection.
- **SOLID Compliance** - Achieved full SOLID principle compliance (5/5) with dependency inversion and interface segregation.
- **Documentation Restructure** - Reorganized docs-site navigation with improved sidebar structure and enhanced SDK guides.

### Fixed
- **Code Formatting** - Corrected indentation issues in engine_handler.go and interface_test.go after merge.

---

## [v0.15.4] - 2026-03-01

### 📚 Documentation & Script Refinements

This release streamlines documentation assets and refines the ecosystem overview.

### Changed
- **Ecosystem Spotlight** - Highlighted Slack as the flagship ChatApp integration in the ecosystem overview.
- **Architecture Diagram** - Simplified architecture assets by consolidating to `topology.svg`.

### Fixed
- **Docs Image Reference** - Corrected architecture image path from logo to proper diagram.

### Removed
- **Redundant Asset** - Removed `agent-architecture.svg` in favor of unified `topology.svg`.

### Refactored
- **Script Update** - Updated `svg2png.sh` with current resource paths for asset conversion.

---

## [v0.15.3] - 2026-03-01

### 📚 Documentation Polish & Build Fix

This release fixes critical VitePress configuration issues and enhances SDK documentation.

### Fixed
- **VitePress Config Structure** - Fixed nested `sidebar: { sidebar: }` duplication and moved `socialLinks`, `footer`, `search`, `editLink`, `lastUpdated` to correct `themeConfig` level.
- **Build Verification** - Docs site now builds successfully with zero errors.

### Changed
- **Enhanced SDK Guides** - Expanded Go, Python, and TypeScript SDK documentation with comprehensive examples and usage patterns.
- **Ecosystem Docs** - Refined ChatApps integration guides for better clarity.

---

## [v0.15.2] - 2026-03-01

### ✨ Artisanal Docs Portal & Narrative Soul

This release transforms the HotPlex documentation site into a premium, vision-driven portal, blending high-end design with deep technical storytelling.

### Added
- **Glassmorphism UI System** - Implemented a sophisticated visual layer with backdrop-blur, shimmering gradients, and multi-layered "umbra" shadows for a premium feel.
- **Micro-Interaction Engine** - Introduced smooth `cubic-bezier` transitions, spring-based hover effects, and rounded, translucent scrollbars.

### Changed
- **Philosophical Narrative** - Completely re-architected the `Introduction` and `Architecture` guides to emphasize the "Philosophy of the Bridge" and the HotPlex engineering ethos.
- **Technical Storytelling** - Reframed the `Protocol` and `API` specifications as "The Design of a Conversation," ensuring artisanal quality even in technical references.
- **Rhythmic Typography** - Optimized vertical rhythm and letter-spacing for balanced, high-performance reading.

### Fixed
- **Environment-Driven Docs** - Corrected `Getting Started` guides to accurately reflect the real-world environment-variable configuration logic of `hotplexd`.
- **Build Integrity** - Optimized VitePress build pipeline to ensure zero dead links and 100% documentation accuracy.

---

## [v0.15.1] - 2026-03-01

### ✨ Reliability Hardening & UI Polish

This maintenance release focuses on system reliability, concurrency safety, and a premium visual refresh for the Slack integration.

### Added
- **Premium Emoji System** - Redesigned tool emojis using guaranteed standard Slack shortcodes (`:keyboard:`, `:floppy_disk:`, `:eyes:`, `:mag:`) for universal compatibility and a professional look.
- **Initialization Synchronization** - New 500ms buffering logic in `ZoneOrderProcessor` ensures "Starting session" messages always appear at the top during cold starts.

### Fixed
- **Critical Concurrency Hardening** - Performed a full audit and hardening of all Mutex usage in the `chatapps` package, introducing `defer` patterns to prevent lock leakage under high concurrency.
- **Memory Leak Prevention** - Implemented `ResetSession` lifecycle hooks to ensure buffers and synchronization states are thoroughly cleaned up after session completion.
- **Build & Variable Safety** - Fixed variable re-declaration and scope issues in the `dingtalk` adapter.

### Changed
- **Slack UX Polish** - Simplified the Session ID display in initialization messages and upgraded the tool failure icon from `:x:` to a more professional `:warning:`.

---

## [v0.15.0] - 2026-02-28

### ✨ Support for Plan Mode & Interaction Refinements

This release introduces support for Claude **Plan Mode** and the `AskUserQuestion` tool, enabling more collaborative and structured AI interactions. It also includes significant stability fixes and UI enhancements for the Slack integration.

### Added
- **Claude Plan Mode Support** - Full support for `plan` mode events and `exit_plan_mode` tool.
- **`AskUserQuestion` Integration** - Enables AI agents to pause execution and request user feedback directly.
- **Dynamic Status Indicators** - Real-time event type indicators in Slack status messages for better process transparency.
- **Interactive Permission Requests** - Added support for permission requests via `stdin` for platform-independent interaction (Refs #39).
- **Error Visualization** - Visual border styling and improved quoting for error blocks in Slack.

### Fixed
- **Slack Command Reliability** - Improved `#reset` command handling in threads and fixed marker deletion issues.
- **Message Stability** - Enhanced handling of stale `message_ts` to prevent update errors during rapid state changes.
- **Session Context Safety** - Ensured proper `session_id` propagation in security block messages and adapter layers.
- **Link Checker Scope** - Optimized automated link checking by limiting scope to `docs-site` and `scripts` directories.

### Changed
- **Validation Logic Refinement** - Simplified and hardened `BuildPlanItem` validation for better reliability.
- **Type Safety Enhancements** - Improved type safety across Slack component builders and event handlers.
- **Nomenclature Sync** - Aligned internal terminology with the latest SSOT documentation.

### Technical Details
- **Major Features**: Plan Mode, AskUserQuestion, Security Block fixes.
- **Platform**: Slack Adapter refinements and core engine stability.
- **Documentation**: Updated verification reports for Plan Mode and tool findings.

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Verification**: [Plan Mode Findings](docs/verification/exit-plan-mode-findings.md)
- **Issue**: [#39](https://github.com/hrygo/hotplex/issues/39) - Permission request support

---

## [v0.13.1] - 2026-02-28

### ✨ Slack UX Refinements & Verification Systems

This release provides critical refinements to the Slack UX implementation, ensuring better stability, more accurate event aggregation, and enhanced interaction features. It also introduces a comprehensive verification system for engine events and Slack Block Kit mapping.

### Added
- **Real-Time Typing Indicator** - Slack native typing indicator during AI response for improved feedback.
- **Direct Message Support** - Proper handling of DM channels and thread-local context in DMs.
- **Tool ID Mapping** - Persistent tool name tracking across turns using internal tool ID persistence.
- **Exhaustive Verification System** - New Python-based verification suites for event-to-block mapping and session lifecycle.
- **Hot-Multiplexing Statistics** - Enhanced session pool observability with real-time stats for active/idle sessions.

### Changed
- **Session Stats Summary** - Refined compact summary style to match the UX specification exactly (⚡ tokens separated by In/Out).
- **Event Aggregation Logic** - Improved aggregation to prevent skipping `ToolResult` events and preserve tool names accurately.
- **Thinking Event Mapping** - Corrected tool name mapping for `Thinking` and `ToolUse` events.

### Fixed
- **Message Stability** - Fixed `cant_update_message` errors caused by stale `message_ts` in rapid update scenarios.
- **Channel/Thread Safety** - Handled invalid channel/thread combinations for better Slack platform compatibility.
- **Tool Result Timing** - Ensure tool execution duration is only displayed when it exceeds the 500ms threshold.
- **Aggregation Reliability** - Fixed edge cases where rapid tool calls could lose the final tool's state.
- **Error Handling** - Improved resilience when processing empty content or invalid Slack block structures.

### Documentation
- **Verification Reports** - Added comprehensive verification reports for Engine Events and Slack UX (`docs/verification/`).
- **Slack UX Spec Updates** - Refined event definitions and aggregation rules in the unified spec.

### Technical Details
- **Files Changed**: 23 files
- **Lines Added**: +3,044
- **Lines Removed**: -295
- **New Scripts**: `scripts/verify_claude_exhaustive.py`, `scripts/verification_report.md`
- **New Package**: Enhanced stats tracking in `engine/stats.go`

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Spec**: [Slack Block Kit Mapping](docs/chatapps/engine-events-slack-ux-spec.md)
- **Verification**: [Slack UX Verification Report](docs/verification/slack-ux-verification-report.md)

---

## [v0.13.0] - 2026-02-28

### ✨ Major Features: Complete Slack UX Implementation

This release delivers a production-ready Slack UX experience with real-time feedback, session lifecycle events, and comprehensive panic recovery. The ChatApps layer now provides seamless visual feedback for AI agent activities.

### Added
- **Complete Slack UX Event System** - Full implementation per UX spec
  - `Thinking` event: Context block with streaming updates and visual indicator
  - `Tool Use` event: Section block with tool-specific emoji and aggregation
  - `Tool Result` event: Duration display and file path handling
  - `Answer` event: Throttled markdown streaming (1/sec)
  - `Session Summary`: Compact view with ⚡ tokens (In/Out separately)
  - `Session Lifecycle`: `SessionStart` / `SessionEnd` events for state tracking
- **Real-Time Typing Feedback** - Slack native typing indicator during AI response
- **`#command` Prefix Support** - Thread-local slash commands via `#reset`, `#clear`
- **Direct Message Support** - Proper handling of `im` channel type for DMs
- **Panic Recovery System** - Production-grade goroutine panic handling
  - New `internal/panicx` package with `RecoverableGoroutine`
  - Stack trace capture and structured logging
  - Graceful degradation instead of silent failures
- **SDK-First Development Rule** - New `.agent/rules/chatapps-sdk-first.md`
  - Mandates official platform SDKs over custom implementations
  - Prohibits redundant signature verification, rate limiting code

### Changed
- **Thinking Message Management** - Update existing message instead of creating new ones
- **Session Summary Style** - Simplified to Duration + ⚡ Tokens (In/Out)
- **RateLimiter Unification** - Single `golang.org/x/time/rate` across all chatapps
- **Go Version** - Upgraded to Go 1.25 in CI workflow

### Fixed
- **Socket Mode AppToken** - Properly pass AppToken to Slack SDK
- **@mention Routing** - Correctly route @mention and #command messages to handler
- **Quick CLI Response** - Delete thinking message when CLI responds immediately
- **Link Checker** - Fixed several internal broken links in documentation site
- **Docs Synchronization** - Unified synchronization of all ChatApps documents to docs-site

### Documentation
- **Slack UX Spec** - Consolidated research docs into unified spec (`docs/chatapps/engine-events-slack-ux-spec.md`)
- **Claude CLI Verification** - Added verification scripts for reset command, plan mode, and ask user features

### Technical Details
- **Files Changed**: 72 files
- **Lines Added**: +7,905
- **Lines Removed**: -6,657
- **New Package**: `internal/panicx` with 246 lines of tests
- **Test Coverage**: All packages passing

### Breaking Changes
- None - Backward compatible

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Spec**: [Slack Block Kit Mapping](docs/chatapps/engine-events-slack-ux-spec.md)
- **Rule**: [ChatApps SDK First](.agent/rules/chatapps-sdk-first.md)

---

## [v0.12.1] - 2026-02-26

### ✨ Major Features: Slack Block Kit Implementation

This release delivers a complete Slack Block Kit implementation for HotPlex ChatApps, transforming how AI agent messages are displayed in Slack with rich, interactive UI components.

### Added
- **Slack Block Kit Mapping** - Complete event-to-Block Kit transformation
  - `Thinking` event: Context block with streaming updates
  - `Tool Use` event: Section block with tool-specific emoji mapping
  - `Tool Result` event: Enhanced display with duration threshold (500ms) and file path display
  - `Answer` event: Section block with markdown formatting and throttled streaming (1/sec)
  - `Error` event: Visual border styling with quoted error messages
  - `Session Stats` event: Compact style showing only Duration + Tokens
  - `Permission Request` event: Allow/Deny interactive buttons
- **Tool Emoji Mapping** - Automatic emoji assignment by tool type
  - `Bash` → `:computer:`, `Edit` → `:pencil:`, `Read` → `:books:`
  - `FileSearch` → `:mag:`, `WebFetch` → `:globe_with_meridians:`
  - 15+ tool types supported
- **Enhanced Error Handling** - Corrected retry classification logic
  - 422, validation errors classified as non-retryable (UserError)
  - "Not found" errors properly classified as non-retryable
  - Prevents unnecessary retries on client errors
- **Multi-Agent Git Safety** - CRITICAL protocol for shared development
  - Forbidden commands: `git checkout -- .`, `git reset --hard`
  - Mandatory `git status` check before destructive operations
  - Safe alternatives: `git checkout HEAD -- <file>`, `git stash`

### Changed
- **Tool Use Aggregation** - Increased time window from 100ms to 500ms
  - Reduces message spam from rapid consecutive tool calls
  - Added per-event-type `MinContent` threshold (200 → 50 for tool_use)
- **Tool Result Display** - Smart duration display
  - Only shows duration when >500ms threshold
  - File path display with truncation (50 chars max)
- **Session Stats** - Simplified Compact style
  - Shows only Duration + Tokens (In/Out)
  - Removes Cost/Tools/Files from default view

### Documentation
- **Design Specification** - Complete UI/UX design doc (`docs/chatapps/engine-events-slack-ux-spec.md`)
  - 8 event types with Block Kit mappings
  - Slack official docs integration
  - Token counting mechanism (Claude Code research)
  - Permission request types research
- **AGENT.md Updates** - Multi-agent safety guidelines
  - Git destructive operation warnings
  - Forbidden commands table with alternatives
  - Mandatory protocol for shared development

### Technical Details
- **Files Changed**: 15+ files
  - `chatapps/slack/block_builder.go` - Block Kit implementation
  - `chatapps/engine_handler.go` - Event handling updates
  - `chatapps/processor_aggregator.go` - Aggregation config
  - `chatapps/processor_chain.go` - Window time update
  - `chatapps/slack/retry.go` - Error classification fix
  - `CLAUDE.md` / `AGENT.md` - Safety guidelines
- **Test Coverage**: All 22 packages passing
- **Research**: Token counting, Permission request types (Claude Code)

### Breaking Changes
- None - Backward compatible

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Issue**: [#38](https://github.com/hrygo/hotplex/issues/38) - Engine Events → Slack Block Kit
- **Documentation**: [Slack Block Kit Mapping](docs/chatapps/engine-events-slack-ux-spec.md)

---

## [v0.11.5] - 2026-02-26

## [v0.11.5] - 2026-02-26

### ♻️ Documentation Restoration & Site Optimization

This release restores lost work on documentation maintenance, site optimization, and nomenclature changes. It introduces advanced link transformation logic and automated auditing tools.

### Changed
- **Nomenclature Normalization**: Updated all references from `ClaudeCode` to `Claude` across documentation and site configuration
- **VitePress Site Structure**: Reorganized documentation paths for better maintainability and navigation
- **Links & Redirects**: Implemented advanced regex-based link transformation in `sync_docs.sh`
  - Relative links to code files are now automatically converted to GitHub tree/blob URLs
  - Legacy migration guides have been replaced by a streamlined update process
- **CI/CD Hardening**: Optimized `deploy-docs.yml` for faster and more reliable documentation deployment

### Added
- **Link Auditing Utility**: New `scripts/check_links.py` for automated identification and fixing of dead links
- **ChatApps Audit Plan**: New `docs/chatapps-audit-and-fix-plan.md` documenting the current state and path forward for platform documentation

### Contributors
- [@hrygo](https://github.com/hrygo)

---

## [v0.11.3] - 2026-02-25

### 🐛 Bug Fixes

This patch release fixes documentation build issues and CI workflow triggers.

### Fixed
- **VitePress Sidebar Configuration**: Added missing `text` properties in sidebar config that caused build failures
- **ChatApps Documentation Links**: Repaired malformed markdown links in chatapps-guide.md
- **Docs Sync Script**: Added `chatapps-dingtalk-analysis.md` to sync script to fix dead link errors
- **CI Workflow Triggers**: Extended deploy-docs workflow to trigger on `docs/**` and `scripts/sync_docs.sh` changes

### Technical Details
- **Files Changed**: 4 files
- **Test Coverage**: All tests passing

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Release**: [v0.11.3](https://github.com/hrygo/hotplex/releases/tag/v0.11.3)

---

## [v0.11.4] - 2026-02-25

### ✨ New Features

This minor release adds Slack Slash Command support for session context management.

### Added
- **Slack Slash Command `/clear`**: Clear conversation context and start fresh
  - User types `/clear` in Slack
  - HotPlex sends `/clear` to Claude Code via stdin
  - Claude Code clears conversation context
  - User sees ephemeral confirmation: "✅ Context cleared. Ready for fresh start!"
  - Session continues (no restart needed)
  - `CLAUDE.md` project instructions are preserved
- **Slack App Configuration**: Added `/clear` command setup to Slack documentation
- **Engine.GetSession()**: New public method for session retrieval

### Technical Details
- **Files Changed**: 5 files
  - `engine/runner.go` - Added GetSession() method
  - `chatapps/slack/adapter.go` - Added slash command handling
  - `chatapps/slack/slash_command_test.go` - Added unit tests
  - `chatapps/setup.go` - Wired up Engine for slash commands
  - `docs/chatapps/chatapps-slack.md` - Added slash command documentation
- **Test Coverage**: All tests passing

### Documentation
- Updated `docs/chatapps/chatapps-slack.md` with Slash Commands section
- Added Slack App configuration steps for `/clear` command
- Included local development guide with ngrok setup

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Issue**: [#40](https://github.com/hrygo/hotplex/issues/40)
- **Documentation**: [Slack Slash Commands](docs/chatapps/chatapps-slack.md#10-slash-commands)

---

## [v0.11.2] - 2026-02-25

### 🐛 Bug Fixes

This patch release fixes a critical Slack Socket Mode reconnection issue and CI workflow configuration.

### Fixed
- **Slack Socket Mode Permanent Reconnection** ([Issue #33](https://github.com/hrygo/hotplex/issues/33)): Fixed the bot becoming permanently disconnected when Slack closes the WebSocket connection
  - Replaced limited retry (5 attempts) with permanent reconnection loop
  - Added exponential backoff (1s → 30s max) between attempts
  - Added `SetReconnectCallbacks()` for adapter layer notification
  - Connection now keeps retrying until context is cancelled (graceful shutdown)
- **Deploy Docs CI Workflow**: Removed duplicate trigger configuration in `deploy-docs.yml`

### Technical Details
- **Files Changed**: 2 files
- **Test Coverage**: All tests passing

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **Issue**: [#33](https://github.com/hrygo/hotplex/issues/33)
- **Release**: [v0.11.2](https://github.com/hrygo/hotplex/releases/tag/v0.11.2)

---



## [v0.11.1] - 2026-02-25

### 🐛 Bug Fixes & Slack Feature Enhancements

This patch release includes Slack feature enhancements, cross-platform path parsing fixes, and CI deployment fixes.

### Added
- **Slack Slash Commands Support**: New slash command handling capability for Slack bot
- **Slack Reactions Support**: New reaction (emoji) support for Slack messages
- **Config Validation Enhancements**: Improved configuration validation for Slack integration

### Fixed
- **Cross-platform work_dir Parsing** ([PR #30](https://github.com/hrygo/hotplex/pull/30)): Resolved work_dir parsing issues across all platforms (Windows, macOS, Linux)
- **Deploy Docs CI Fix**: Created missing `docs-site/public/assets/` directory before copying assets
- **Documentation Sync**: Added slack-gap-analysis.md to sync_docs.sh

### Technical Details
- **Files Changed**: 9 files
- **Test Coverage**: All tests passing

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **PR**: [#30](https://github.com/hrygo/hotplex/pull/30)
- **Release**: [v0.11.1](https://github.com/hrygo/hotplex/releases/tag/v0.11.1)

---


## [v0.11.0] - 2026-02-25

### 🔐 Slack 安全增强与可靠性提升

This release addresses critical security and reliability gaps identified in the [Slack Gap Analysis Report](docs/chatapps/slack-gap-analysis.md). We've implemented comprehensive path traversal protection, Socket Mode ACK retry mechanism, and extensive documentation.

### Added
- **Path Traversal Attack Protection**:
  - New `expandPath()` function with `~` expansion to user home directory
  - New `isSensitivePath()` function blocking access to system directories (`/etc`, `/var`, `/usr`, `/bin`, `/sbin`, `/root`, `/proc`, `/sys`, `/boot`, `/dev`)
  - Automatic detection and blocking of path traversal attempts (e.g., `../etc/passwd`)
  - Safe path cleaning with `filepath.Clean` for relative paths
  
- **Socket Mode ACK Retry Mechanism**:
  - New `sendACKWithRetry()` function with exponential backoff (1s → 2s → 4s)
  - Maximum 3 retries (4 total attempts) for reliable message delivery
  - Slack API compliant 3-second response requirement
  - Comprehensive logging for debugging connection issues
  
- **Comprehensive Unit Tests**:
  - 26 test cases for `expandPath()` covering normal paths, edge cases, and security scenarios
  - 10 test cases for `isSensitivePath()` covering all blocked directories
  - 93.8% test coverage for path handling functions
  - New test file `chatapps/setup_test.go` (+279 lines)
  
- **Gap Analysis Report** ([Issue #21](https://github.com/hrygo/hotplex/issues/21)):
  - Comprehensive 416-line comparison: HotPlex vs OpenClaw Slack implementations
  - 30+ feature gaps identified across 6 categories (P0/P1/P2 priority)
  - 3-phase implementation roadmap (14-20 weeks estimated)
  - Technical debt risk identification
  
- **Documentation Updates**:
  - System prompt configuration guide with injection flow diagram
  - Security features documentation (path checks, ACK retry, signature verification)
  - Troubleshooting examples (Q5: System prompt not生效，Q6: Path blocked)
  - Example environment files (`.env.development`, `.env.production`)

### Changed
- **Configuration Enhancements** (`chatapps/configs/slack.yaml`):
  - Detailed path security documentation with examples
  - ACK retry mechanism explanation
  - System prompt injection flow description
  - Complete troubleshooting section
  
- **User Manual** (`docs/chatapps/chatapps-slack.md`):
  - Added Chapter 7: System Prompt Configuration
  - Added Chapter 8: Security Features
  - Updated changelog with v0.10.0, v0.9.0, v0.8.0
  
- **Code Quality**:
  - Project-wide lint cleanup
  - Improved error handling in path expansion
  - Enhanced logging for security events

### Fixed
- **Duplicate Message Processing** ([PR #23](https://github.com/hrygo/hotplex/pull/23)):
  - Removed duplicate `handleEventsAPI()` call in Socket Mode
  - Added empty payload validation
  - Fixed potential message duplication issue
  
- **Security Vulnerabilities**:
  - Blocked access to sensitive system directories
  - Prevented path traversal attacks via `..` sequences
  - Hardened path validation with multiple security layers

### Technical Details
- **Files Changed**: 7 files
- **Lines Added**: +1,256
- **Lines Removed**: -140
- **Net Change**: +1,116 lines
- **Test Coverage**: 93.8%+ (41 test cases)

### Verification
```bash
✅ go test ./... - All tests pass
✅ go build ./... - Build succeeds
✅ golangci-lint run - 0 issues
✅ Path security - Blocks /etc, /var, /root successfully
✅ ACK retry - Handles connection failures
```

### Contributors
- [@hrygo](https://github.com/hrygo)

### Related
- **PR**: [#23](https://github.com/hrygo/hotplex/pull/23)
- **Issue**: [#21](https://github.com/hrygo/hotplex/issues/21)
- **Release**: [v0.11.0](https://github.com/hrygo/hotplex/releases/tag/v0.11.0)
- **Gap Analysis**: [docs/chatapps/slack-gap-analysis.md](docs/chatapps/slack-gap-analysis.md)

---

## [v0.10.0] - 2026-02-23

### 🚀 ChatApps-as-a-Service Milestone (v0.10.0)

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
- **Documentation**: New [ChatApps 接入层指南](docs/chatapps/chatapps-guide.md) with architecture diagrams and platform comparison.

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
- **CI/CD Reliability**: Downgraded Go version to 1.25 across all workflows and `go.mod` to resolve `golangci-lint` compatibility issues.
- **Terminal Compatibility**: Standardized script outputs using `printf` to resolve garbled emoji characters on various terminal emulators.


## [v0.4.0] - 2026-02-20

### Added
- **CI/CD Pipelines**: Integrated GitHub Actions for automated Builds, Tests (with Race detection), and Linters.
- **Automated Releases**: Configured `GoReleaser` to automatically build and release multi-platform binaries (Linux, macOS, Windows) upon tag push.
- **Community Standards**: Added `LICENSE` (MIT), `CONTRIBUTING.md`, and Issue/PR templates to follow open-source best practices.
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
