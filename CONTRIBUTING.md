# Contributing to HotPlex

First off, thank you for considering contributing to HotPlex! It's people like you that make HotPlex such a great tool.

## 🌟 Our Philosophy

### First Principles
1.  **Leverage vs Build**: Bridge existing AI CLI tools into production, don't reinvent the agent's core reasoning.
2.  **CLI-as-a-Service**: Transform one-off CLI turns into long-lived, stable services.
3.  **Security First**: Every execution is isolated; WAF interception is mandatory.

### Architectural Rules
- **Public Thin, Private Thick**: Root package provides minimal API surface.
- **Strategy Pattern**: Provider interface decouples engine from specific AI tools.
- **PGID-First Security**: Every process runs in a dedicated process group for clean lifecycle management.
- **IO-Driven State Machine**: No fixed sleeps; use IO markers for sync.

---

## 🛠 Code Standards

### Go Style
Follow the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md). 
- **MANDATORY**: All interface implementations must have compile-time verification:
  ```go
  var _ ChatAdapter = (*SlackAdapter)(nil)
  ```

### Error Handling & Concurrency
- **No Panics**: Never use `panic()` in the core engine.
- **Wrap Errors**: Use `fmt.Errorf("context: %w", err)` for explicit wrapping.
- **Mutex Safety**: Use `sync.RWMutex` for stateful pools. Always `defer mu.Unlock()` immediately after locking.

---

## 🚀 Development Workflow

### Useful Commands
We use a `Makefile` to standardize development:

| Command       | Description                                 |
| ------------- | ------------------------------------------- |
| `make build`  | Compiles the `hotplexd` binary to `dist/`   |
| `make test`   | Runs unit tests with race detection         |
| `make lint`   | Runs `golangci-lint` to ensure code quality |
| `make verify` | Runs all checks (fmt, lint, test)           |

### PR Checklist
- [ ] Code follows style guide.
- [ ] Unit tests pass (`make test`).
- [ ] Documentation updated for API changes.
- [ ] PR description includes `Resolves #IssueID`.

### Commit Convention
Follow [Conventional Commits](https://www.conventionalcommits.org/):
`feat(scope): description` or `fix(pool): memory leak`.

---

## 🛡 Git Safety Protocol

To prevent accidental data loss in shared development, avoid these commands on the `main` branch:
- `git reset --hard` (unless explicitly intended)
- `git clean -fd` without checking `git status`

**Best Practices**:
1. Run `git status` before any batch operation.
2. Commit frequently to "claim" your progress.
3. Use `git stash` for temporary context switching.

---

## 📜 Documentation Policy

We follow a **"Docs-First"** mentality. Any PR modifying public APIs *must* update:
- **API**: `docs/server/api.md`
- **Features**: Relevant manual in `docs/`
- **User-Facing**: Root `README.md`

---

## 🤝 Community & Support

- **Bugs/Features**: Use [GitHub Issues](https://github.com/hrygo/hotplex/issues).
- **Discussions**: Share ideas in [GitHub Discussions](https://github.com/hrygo/hotplex/discussions).

Released under the [MIT License](LICENSE).
