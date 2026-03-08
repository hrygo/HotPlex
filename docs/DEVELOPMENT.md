# HotPlex Development Guide

> This guide covers local development setup, testing, and common workflows.
> For architecture details, see [ARCHITECTURE.md](ARCHITECTURE.md).
>
> **[中文版](development_zh.md)** | **English**

## Table of Contents

- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Building](#building)
- [Testing](#testing)
- [Code Quality](#code-quality)
- [Configuration](#configuration)
- [Debugging](#debugging)
- [Common Tasks](#common-tasks)

---

## Prerequisites

### Required

| Tool | Version | Purpose          |
| ---- | ------- | ---------------- |
| Go   | 1.25+   | Primary language |
| Make | Any     | Build automation |
| Git  | 2.x     | Version control  |

### Optional

| Tool          | Purpose          |
| ------------- | ---------------- |
| golangci-lint | Advanced linting |
| Docker        | Container builds |
| claude CLI    | Local AI testing |

### Environment Setup

```bash
# Clone the repository
git clone https://github.com/hrygo/hotplex.git
cd hotplex

# Copy environment template
cp .env.example .env

# Edit with your credentials
vim .env
```

---

## Getting Started

### Quick Commands

```bash
make help        # Show all available commands
make build       # Build the daemon
make run         # Build and run in foreground
make test        # Run unit tests
make lint        # Run linter
```

### First Build

```bash
# Install dependencies
go mod download

# Build the daemon
make build

# Run with default config
make run
```

---

## Building

### Development Build

```bash
# Fast build (no lint)
go build -o dist/hotplexd ./cmd/hotplexd

# Standard build (with fmt, vet, tidy)
make build
```

### Cross-Platform Build

```bash
# Build for all platforms
make build-all

# Outputs in dist/:
# - hotplexd-linux-amd64
# - hotplexd-linux-arm64
# - hotplexd-darwin-amd64
# - hotplexd-darwin-arm64
# - hotplexd-windows-amd64.exe
```

### Build with Version Info

```bash
# Version is automatically derived from git tags
VERSION=v1.0.0 make build
```

---

## Testing

### Unit Tests

```bash
# Fast unit tests (default)
make test

# With verbose output
go test -v -short ./...

# Specific package
go test -v ./engine/...
```

### Race Detection

```bash
# Run with race detector
make test-race

# Or directly
go test -v -race ./...
```

### Integration Tests

```bash
# Heavy integration tests
make test-integration

# All tests
make test-all
```

### CI-Optimized Tests

```bash
# Optimized for CI (parallel, timeout)
make test-ci
```

### Writing Tests

Follow these conventions:

```go
// Use table-driven tests
func TestSessionPool(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"valid input", "test", "test", false},
        {"empty input", "", "", true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

**Testing Guidelines:**
- Mock heavy I/O (use echo/cat for CLI mocking)
- `go test -race` must pass
- One test file per source file: `foo.go` -> `foo_test.go`

---

## Code Quality

### Formatting

```bash
make fmt        # Format Go code
go fmt ./...
```

### Vetting

```bash
make vet        # Check for suspicious constructs
go vet ./...
```

### Linting

```bash
make lint       # Run golangci-lint
```

**Note:** Linter errors (e.g., `unused`) indicate incomplete integration. Link the code, don't delete it.

### Module Maintenance

```bash
make tidy       # Clean up go.mod
go mod tidy
```

---

## Configuration

### Configuration Priority

1. **Command flags** (highest priority)
2. **Environment variables** (`.env` file)
3. **YAML config files** (`chatapps/configs/*.yaml`)
4. **Defaults** (lowest priority)

### Environment Variables

```bash
# Core server
HOTPLEX_PORT=8080
HOTPLEX_LOG_LEVEL=INFO
HOTPLEX_API_KEY=your-secret-key

# Engine
HOTPLEX_EXECUTION_TIMEOUT=30m
HOTPLEX_IDLE_TIMEOUT=1h

# Provider
HOTPLEX_PROVIDER_TYPE=claude-code
HOTPLEX_PROVIDER_MODEL=sonnet

# Slack (example)
HOTPLEX_SLACK_BOT_USER_ID=UXXXXXXXXXX
HOTPLEX_SLACK_BOT_TOKEN=xoxb-...
HOTPLEX_SLACK_APP_TOKEN=xapp-...
```

### YAML Config Structure

```yaml
# chatapps/configs/slack.yaml
platform: slack

provider:
  type: claude-code
  default_model: sonnet

engine:
  work_dir: ~/projects/myproject
  timeout: 30m
  idle_timeout: 1h

features:
  chunking:
    enabled: true
    max_chars: 4000
  threading:
    enabled: true
```

### Hot Reload

Configuration files are watched for changes. Edit YAML files and the daemon will reload automatically.

---

## Debugging

### Enable Debug Logging

```bash
# In .env
HOTPLEX_LOG_LEVEL=DEBUG
HOTPLEX_LOG_FORMAT=text
```

### View Logs

```bash
# Foreground mode (logs to stdout)
make run

# Background mode
make restart
tail -f .logs/daemon.log
```

### Common Issues

| Issue                       | Solution                                            |
| --------------------------- | --------------------------------------------------- |
| "command not found: claude" | Install Claude CLI or set `HOTPLEX_PROVIDER_BINARY` |
| "permission denied"         | Check `work_dir` permissions                        |
| Session not persisting      | Check `idle_timeout` setting                        |
| Slack not responding        | Verify `HOTPLEX_SLACK_BOT_USER_ID` is correct       |

---

## Common Tasks

### Run with Specific Config

```bash
# Use --config flag (highest priority)
hotplexd --config /path/to/configs

# Or via environment
export HOTPLEX_CHATAPPS_CONFIG_DIR=/path/to/configs
hotplexd
```

### Service Management

```bash
make service-install    # Install as system service
make service-start      # Start service
make service-status     # Check status
make service-logs       # View logs
make service-stop       # Stop service
make service-uninstall  # Remove service
```

### Docker Development

```bash
make docker-build       # Build image (no cache, ensures fresh binary)
make docker-build-cache # Build image (cached, faster for iteration)
make docker-up          # Start containers
make docker-logs        # View logs
make docker-down        # Stop containers
make docker-restart     # Restart with config sync
```

### Clean Build Artifacts

```bash
make clean              # Remove dist/ and clean Go cache
```

---

## Git Workflow

### Branch Naming

```
<type>/<issue-id>-short-description

# Examples:
feat/123-add-user-auth
fix/456-memory-leak
docs/789-update-readme
```

### Commit Format

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(scope): description

# Types: feat, fix, refactor, docs, test, chore, wip
# Examples:
feat(auth): add OAuth login (Refs #123)
fix(pool): resolve memory leak (Refs #456)
wip: checkpoint for feature X
```

### Pre-commit Checks

```bash
# Run before committing
make lint test
```

---

## Related Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) - System architecture
- [configuration.md](configuration.md) - Configuration reference
- [CONTRIBUTING.md](../CONTRIBUTING.md) - Contribution guidelines
- [sdk-guide.md](sdk-guide.md) - SDK developer guide
