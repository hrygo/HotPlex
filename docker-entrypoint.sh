#!/bin/bash
set -e

# ============================================
# HotPlex Docker Entrypoint
# Handles container initialization before main process
# ============================================

# Claude Code configuration - create if not exists
CLAUDE_CONFIG="/home/hotplex/.claude.json"
if [ ! -f "$CLAUDE_CONFIG" ]; then
    echo '{}' > "$CLAUDE_CONFIG"
    echo "[entrypoint] Created $CLAUDE_CONFIG"
fi

# Execute the main command
exec "$@"
