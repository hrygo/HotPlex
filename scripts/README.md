# HotPlex Scripts

This directory contains utility scripts and Git hooks used for development, documentation, and asset generation in the HotPlex project.

## Development Tools

### Git Hooks
These scripts ensure code quality and consistent commit messages.
- `setup_hooks.sh`: Links the following hooks from `scripts/` to `.git/hooks/`.
- `pre-commit`: Runs `go fmt` and dependency checks before each commit.
- `commit-msg`: Validates commit messages based on Conventional Commits.
- `pre-push`: Performs final checks (full test suite) before pushing to remote.

### Documentation Management
- `check_links.py`: Audits internal documentation links to prevent dead links.

### Asset Orchestration (SSOT)
- `generate_assets.sh`: The single entry point for all visual asset processing.
  - Synchronizes SVGs from `docs/images/` to `docs-site/public/images/`.
  - Generates core brand assets (`favicon.ico`, `logo.png`, `hotplex-og.png`).
  - Optionally converts all SVGs to high-resolution PNGs with `--all-pngs`.

## Usage

To set up the development environment hooks, run:
```bash
./scripts/setup_hooks.sh
```

To refresh all documentation assets:
```bash
./scripts/generate_assets.sh --all-pngs
```

To verify documentation links:
```bash
python3 scripts/check_links.py
```
