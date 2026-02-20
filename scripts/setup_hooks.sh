#!/bin/bash
# HotPlex Git Hooks Installer
# Links scripts from /scripts to .git/hooks for a consistent dev experience

set -e

# Get repo root
REPO_ROOT=$(git rev-parse --show-toplevel)
HOOK_SOURCE_DIR="$REPO_ROOT/scripts"
HOOK_TARGET_DIR="$REPO_ROOT/.git/hooks"

# ANSI colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

printf "${BLUE}🔗 Installing HotPlex Git Hooks...${NC}\n"

HOOKS=("pre-commit" "commit-msg" "pre-push")

for hook in "${HOOKS[@]}"; do
    if [ -f "$HOOK_SOURCE_DIR/$hook" ]; then
        # Ensure executable
        chmod +x "$HOOK_SOURCE_DIR/$hook"
        # Create symbolic link
        ln -sf "$HOOK_SOURCE_DIR/$hook" "$HOOK_TARGET_DIR/$hook"
        printf "${GREEN}✅ Linked: $hook${NC}\n"
    else
        printf "⚠️  Skip: $hook (not found in $HOOK_SOURCE_DIR)\n"
    fi
done

printf "${BLUE}Done! Hooks are now active.${NC}\n"
