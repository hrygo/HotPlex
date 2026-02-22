#!/usr/bin/env bash

# HotPlex Documentation SSOT Sync Script
# This script serves as the Single Source of Truth (SSOT) builder.
# It copies markdown files from their core repository locations into the VitePress docs-site structure.

set -e

echo "🔄 Synchronizing documentation sources to docs-site..."

# Ensure target directories exist
mkdir -p docs-site/guide
mkdir -p docs-site/sdks
mkdir -p docs-site/reference
mkdir -p docs-site/public/images

# --- Guides ---
cp README.md docs-site/guide/getting-started.md
cp README_zh.md docs-site/guide/getting-started_zh.md
cp docs/quick-start.md docs-site/guide/quick-start.md
cp docs/quick-start_zh.md docs-site/guide/quick-start_zh.md
cp docs/architecture.md docs-site/guide/architecture.md
cp docs/architecture_zh.md docs-site/guide/architecture_zh.md
cp SECURITY.md docs-site/guide/security.md
cp SECURITY_zh.md docs-site/guide/security_zh.md
cp docs/server/api.md docs-site/guide/websocket.md
cp docs/providers/opencode.md docs-site/guide/opencode-http.md
cp docs/providers/opencode_zh.md docs-site/guide/opencode-http_zh.md
cp docs/hooks-architecture.md docs-site/guide/hooks.md
cp docs/hooks-architecture_zh.md docs-site/guide/hooks_zh.md
cp docs/observability-guide.md docs-site/guide/observability.md
cp docs/observability-guide_zh.md docs-site/guide/observability_zh.md
cp docs/docker-deployment.md docs-site/guide/docker.md
cp docs/docker-deployment_zh.md docs-site/guide/docker_zh.md
cp docs/production-guide.md docs-site/guide/deployment.md
cp docs/production-guide_zh.md docs-site/guide/deployment_zh.md
cp docs/benchmark-report.md docs-site/guide/performance.md
cp docs/benchmark-report_zh.md docs-site/guide/performance_zh.md
cp docs/roadmap-2026.md docs-site/guide/roadmap.md
cp docs/roadmap-2026_zh.md docs-site/guide/roadmap_zh.md

# --- SDKs ---
cp docs/sdk-guide.md docs-site/sdks/go-sdk.md
cp docs/sdk-guide_zh.md docs-site/sdks/go-sdk_zh.md
cp sdks/python/README.md docs-site/sdks/python-sdk.md
cp sdks/typescript/README.md docs-site/sdks/typescript-sdk.md

# --- Reference ---
cp docs/server/api.md docs-site/reference/api.md
cp docs/server/api_zh.md docs-site/reference/api_zh.md
cp docs/README.md docs-site/reference/index.md
cp docs/README_zh.md docs-site/reference/index_zh.md

# --- Migration ---
mkdir -p docs-site/migration
cp docs/migration/migration-guide-v0.8.0.md docs-site/migration/v0.8.0.md
cp docs/migration/migration-guide-v0.8.0_zh.md docs-site/migration/v0.8.0_zh.md
cp docs/migration/migration-guide-v0.9.0.md docs-site/migration/v0.9.0.md
cp docs/migration/migration-guide-v0.9.0_zh.md docs-site/migration/v0.9.0_zh.md

# --- Assets ---
if [ -d "docs/images" ]; then
    cp -r docs/images/* docs-site/public/images/
fi

if [ -d ".github/assets" ]; then
    mkdir -p docs-site/public/assets
    cp -r .github/assets/* docs-site/public/assets/
fi

# --- Path Fixes for VitePress ---
# Fix image paths
find docs-site -name "*.md" -type f -exec sed -i.bak 's|docs/images|/images|g' {} +
find docs-site -name "*.md" -type f -exec sed -i.bak 's|\./images|/images|g' {} +
find docs-site -name "*.md" -type f -exec sed -i.bak 's|\.github/assets|/assets|g' {} +

# Fix Bilingual Cross-links & Internal VitePress Links using regex (sed -E)
# Architecture Links
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?architecture(_zh)?(\.md)?\)|](/guide/architecture\2.md)|g' {} +

# Go SDK Links
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?sdk-guide(_zh)?(\.md)?\)|](/sdks/go-sdk\2.md)|g' {} +

# OpenCode Links
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?(providers/)?opencode(_zh)?(\.md)?\)|](/guide/opencode-http\3.md)|g' {} +

# API Reference Links
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?(server/)?api(_zh)?(\.md)?\)|](/reference/api\3.md)|g' {} +

# Getting Started / README Links (Match exact README.md or README_zh.md)
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?README(_zh)?\.md\)|](/guide/getting-started\1.md)|g' {} +

# Other Internal Guides rewritten for VitePress
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?quick-start(_zh)?(\.md)?\)|](/guide/quick-start\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?observability-guide(_zh)?(\.md)?\)|](/guide/observability\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?docker-deployment(_zh)?(\.md)?\)|](/guide/docker\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?production-guide(_zh)?(\.md)?\)|](/guide/deployment\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?benchmark-report(_zh)?(\.md)?\)|](/guide/performance\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?roadmap-2026(_zh)?(\.md)?\)|](/guide/roadmap\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?hooks-architecture(_zh)?(\.md)?\)|](/guide/hooks\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's!\]\(\.?/?SECURITY(_zh)?(\.md)?\)!](/guide/security\1.md)!g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?migration/migration-guide-v0\.8\.0(_zh)?(\.md)?\)|](/migration/v0.8.0\2.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?migration/migration-guide-v0\.9\.0(_zh)?(\.md)?\)|](/migration/v0.9.0\2.md)|g' {} +
# Fix self-referencing links in migration files themselves
find docs-site/migration -name "*.md" -type f -exec sed -E -i.bak 's|\]\(migration-guide-v0\.9\.0(_zh)?(\.md)?\)|](/migration/v0.9.0\1.md)|g' {} +
find docs-site/migration -name "*.md" -type f -exec sed -E -i.bak 's|\]\(migration-guide-v0\.8\.0(_zh)?(\.md)?\)|](/migration/v0.8.0\1.md)|g' {} +

# Redirect GitHub-only URLs (Examples, CONTRIBUTING, LICENSE, Roadmap, ClaudeCode)
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?\.\./_examples/([^)]*)\)|](https://github.com/hrygo/hotplex/tree/main/_examples/\1)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?_examples/([^)]*)\)|](https://github.com/hrygo/hotplex/tree/main/_examples/\1)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?CONTRIBUTING(\.md)?\)|](https://github.com/hrygo/hotplex/blob/main/CONTRIBUTING.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?LICENSE\)|](https://github.com/hrygo/hotplex/blob/main/LICENSE)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?docs/roadmap-2026(\.md)?\)|](https://github.com/hrygo/hotplex/blob/main/docs/roadmap-2026.md)|g' {} +
find docs-site -name "*.md" -type f -exec sed -E -i.bak 's|\]\(\.?/?(docs/)?(providers/)?claudecode(_zh)?(\.md)?\)|](https://github.com/hrygo/hotplex/blob/main/docs/providers/claudecode\3.md)|g' {} +

# Clean up sed backups
find docs-site -name "*.bak" -type f -delete

echo "✅ Documentation successfully synchronized."
