# Changelog

All notable changes to the HotPlex Worker Gateway project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0-rc] - 2026-03-31

### Added

- **Core**: Initial release of the HotPlex Worker Gateway.
- **Protocol**: Implementation of AEP v1 (Agent Exchange Protocol).
- **Security**: WAF (Web Application Firewall) and PGID isolation for worker processes.
- **Workers**: Support for `claudecode`, `opencodecli`, `opencodeserver`, and `pi` worker types.
- **Admin API**: Added endpoints for stats, health checks, session management, and configuration hot-reload.
- **Monitoring**: Integration with Prometheus for metrics and OpenTelemetry for tracing.
- **Governance**: Added `codecov.yml` for enforced coverage checks.
- **Docs**: Comprehensive architecture and testing strategy documents in `docs/`.

### Fixed

- **CI**: Fixed `codecov-action` token configuration and environment access warnings.
- **Session**: Improved session termination logic for cleaner process cleanup.

## [0.1.0] - 2026-03-25

### Added

- Initial internal prototype.
- Base session management and WebSocket gateway.
- Basic SQLite storage for session persistence.
- CI/CD pipeline setup on GitHub Actions.
