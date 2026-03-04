# Contributing to hotplex

First off, thank you for considering contributing to hotplex! It's people like you that make hotplex such a great tool.

Our **First Principle** is to leverage and bridge existing elite AI CLI tools (like Claude Code) into the production ecosystem. Contributions should align with this vision of building a "Control Plane" rather than reinventing the agent's core reasoning or tool logic.

**CRITICAL**: Contributors must read and adhere to [AGENT.md](AGENT.md) for architectural boundaries, concurrency safety, and process lifecycle rules.

## 🚀 How Can I Contribute?

### Reporting Bugs
- Check the [Issues](https://github.com/hrygo/hotplex/issues) page to see if the bug has already been reported.
- If not, create a new issue using the **Bug Report** template.
- Include as much detail as possible: steps to reproduce, expected behavior, and actual behavior.

### Suggesting Enhancements
- Check the [Issues](https://github.com/hrygo/hotplex/issues) page to see if your idea has already been suggested.
- If not, create a new issue using the **Feature Request** template.

### Pull Requests
1. Fork the repository and create your branch from `main`.
2. If you've added code that should be tested, add tests.
3. If you've changed APIs, update the documentation.
4. Ensure the test suite passes (`go test ./...`).
5. Make sure your code follows the Go standard formatting (`go fmt ./...`).
6. Issue that pull request!

## 🛠 Development Setup

### Prerequisites
- Go 1.25 or later
- Required AI CLI tools (e.g., `Claude Code` or `OpenCode`)
- Make

### Clone the Repository
```bash
git clone https://github.com/hrygo/HotPlex.git
cd HotPlex
```

### Useful Commands
We use a `Makefile` to standardize development workflows:

| Command | Description |
|---------|-------------|
| `make build` | Compiles the `hotplexd` binary to `dist/` |
| `make test` | Runs unit tests with race detection |
| `make lint` | Runs `golangci-lint` to ensure code quality |
| `make run` | Builds and starts the daemon locally |
| `make clean` | Removes build artifacts |
| `make verify` | Runs all verification checks (fmt, lint, test) |

### Running Individual Components

```bash
# Run the daemon
go run cmd/hotplexd/main.go

# Run tests for a specific package
go test ./chatapps/...

# Run with race detector
go test -race ./...
```

## 📜 Documentation Policy

We follow a **"Docs-First"** mentality for releases. Any PR modifying public APIs or core behavior *must* update the relevant documentation:

- **API Changes**: Update `docs/server/api.md` and SDK documentation
- **New Features**: Add to appropriate manual in `docs/` or `docs-site/`
- **User-Facing Changes**: Update README.md if necessary

### Building Documentation Site
```bash
cd docs-site
npm install
npm run dev
```

## 🔄 CI/CD Pipeline

All pull requests must pass the following checks:

1. **Code Format**: `go fmt ./...`
2. **Linting**: `golangci-lint run`
3. **Tests**: `go test -race ./...`
4. **Build**: `go build ./...`

## 📝 Commit Convention

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

Types: feat, fix, refactor, docs, test, chore, wip
```

Example:
```
feat(slack): add native streaming support for Assistant Status API
```

## 📄 Code of Conduct

Please note that this project is released with a Contributor Code of Conduct. By participating in this project you agree to abide by its terms.
