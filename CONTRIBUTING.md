# Contributing to HotPlex Worker Gateway

Thank you for your interest in contributing to HotPlex! This project aims to provide a robust, high-performance gateway for AI worker sessions.

## Code of Conduct

Help us keep this project open and inclusive. By participating, you agree to abide by our Code of Conduct (standard Go community practices).

## Getting Started

### Prerequisites

- **Go 1.26** or later.
- **golangci-lint** v1.64.5+.
- **Make** (optional, but recommended).

### Development Environment Setup

1. Fork the repository and clone it locally.
2. Initialize the module:
   ```bash
   go mod download
   ```
3. Run tests to ensure everything is working:
   ```bash
   make test
   ```

## Development Workflow

1. **Pick an issue**: Check the GitHub issue tracker for open tasks.
2. **Create a branch**: Use a descriptive name like `feat/new-worker-type` or `fix/session-leak`.
3. **Write code**: Follow the project's coding style (enforced by `golangci-lint`).
4. **Write tests**:
   - Every feature must have unit tests.
   - Core security and gateway logic requires high coverage (see `docs/testing/Testing-Strategy.md`).
   - Use table-driven tests where possible.
5. **Run tests & lint**:
   ```bash
   make lint
   make test
   ```
6. **Commit & Push**: Follow [Conventional Commits](https://www.conventionalcommits.org/).
7. **Open a PR**: Fill out the Pull Request template provided.

## Pull Request Guidelines

- Ensure your code builds and passes all tests.
- Update documentation if you're adding or changing functionality.
- Link the PR to a relevant issue.
- Maintain a clean git history (squash commits if necessary).

## Testing Standards

- **Unit Tests**: Place in the same package as the code being tested.
- **Integration Tests**: Place in the `test/integration/` or use build tags if needed.
- **Coverage**: Aim for 80% project-wide coverage. Critical modules (Security, Engine, Config) require higher thresholds.

## Questions?

If you have questions, feel free to open a "Discussion" or an "Issue" for clarification.

---

*Thank you for making HotPlex better!*
