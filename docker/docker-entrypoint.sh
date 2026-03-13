#!/usr/bin/env bash
set -e

# ==============================================================================
# HotPlex Docker Entrypoint
# Handles permission fixes, config seeding, Git identity, and privilege drop
# ==============================================================================

HOTPLEX_HOME="/home/hotplex"
CONFIG_DIR="$HOTPLEX_HOME/.hotplex"

# ------------------------------------------------------------------------------
# Helper: Run commands as the hotplex user if currently root
# ------------------------------------------------------------------------------
run_as_hotplex() {
    if [ "$(id -u)" = "0" ]; then
        runuser -u hotplex -m -- "$@"
    else
        "$@"
    fi
}

# ------------------------------------------------------------------------------
# 1. Fix Permissions & Create Directories (if running as root)
#    Solves EACCES issues with host-mounted volumes and ensures paths exist
# ------------------------------------------------------------------------------
if [ "$(id -u)" = "0" ]; then
    echo "--> Ensuring directories exist and fixing permissions..."
    mkdir -p "$CONFIG_DIR" "$HOTPLEX_HOME/.claude" "$HOTPLEX_HOME/projects"
    
    chown -R hotplex:hotplex "$CONFIG_DIR" 2>/dev/null || true
    chown -R hotplex:hotplex "$HOTPLEX_HOME/.claude" 2>/dev/null || true
    chown -R hotplex:hotplex "$HOTPLEX_HOME/projects" 2>/dev/null || true
fi

# ------------------------------------------------------------------------------
# 2. HotPlex Bot Identity & Logging
# ------------------------------------------------------------------------------
echo "==> HotPlex Bot Instance: ${HOTPLEX_BOT_ID:-unknown}"

# ------------------------------------------------------------------------------
# 3. Expand Environment Variables in YAML Config Files
#    Required for ${HOTPLEX_*} variables in slack.yaml, feishu.yaml, etc.
# ------------------------------------------------------------------------------
CONFIG_CHATAPPS_DIR="$HOTPLEX_HOME/configs/chatapps"
if [ -d "$CONFIG_CHATAPPS_DIR" ]; then
    echo "--> Expanding environment variables in config files..."
    
    # 1. Generate variable list for envsubst (only HOTPLEX, GIT, GITHUB variables)
    # This prevents envsubst from clearing out non-environment placeholders like ${issue_id}
    VARS=$(compgen -A export | grep -E "^(HOTPLEX_|GIT_|GITHUB_|HOST_)" | sed 's/^/$/' | tr '\n' ' ')
    
    for yaml in "$CONFIG_CHATAPPS_DIR"/*.yaml; do
        if [ -f "$yaml" ]; then
            # Create a temporary file to avoid partial write issues
            tmp_yaml="${yaml}.tmp"
            if [ -n "$VARS" ]; then
                envsubst "$VARS" < "$yaml" > "$tmp_yaml"
                mv "$tmp_yaml" "$yaml"
                echo "    - Processed $(basename "$yaml")"
            else
                echo "    - Skipping $(basename "$yaml") (No relevant variables exported)"
            fi
        fi
    done
fi

# ------------------------------------------------------------------------------
# 3. Claude Code Configuration - Seeding & Isolation
# ------------------------------------------------------------------------------
CLAUDE_DIR="$HOTPLEX_HOME/.claude"
CLAUDE_SEED="/home/hotplex/.claude_seed"

# Ensure container-private .claude directory exists
run_as_hotplex mkdir -p "$CLAUDE_DIR"

if [ -d "$CLAUDE_SEED" ]; then
    echo "--> Seeding Claude configurations from host..."
    
    # 1. Sync critical capabilities (skills, teams) - Copy only if not exists to avoid overwriting instance-specific changes
    for item in "skills" "teams"; do
        if [ -d "$CLAUDE_SEED/$item" ]; then
             echo "    - Syncing $item..."
             run_as_hotplex cp -rn "$CLAUDE_SEED/$item" "$CLAUDE_DIR/"
        fi
    done

    # 2. Sync core configuration files
    for cfg in "settings.json" "settings.local.json" "config.json"; do
        if [ -f "$CLAUDE_SEED/$cfg" ] && [ ! -f "$CLAUDE_DIR/$cfg" ]; then
            echo "    - Seeding $cfg..."
            run_as_hotplex cp "$CLAUDE_SEED/$cfg" "$CLAUDE_DIR/"
            
            # 3. Dynamic Patching: Only replace 127.0.0.1 with host.docker.internal for Docker network compatibility
            if [ "$cfg" = "settings.json" ]; then
                echo "    - Patching 127.0.0.1 -> host.docker.internal in $cfg"
                run_as_hotplex sed -i 's/127.0.0.1/host.docker.internal/g' "$CLAUDE_DIR/$cfg"
            fi
        fi
    done
fi

# ------------------------------------------------------------------------------
# 4. Git Identity Injection (from environment variables)
#    Allows configuring Git identity via .env without host .gitconfig dependency
# ------------------------------------------------------------------------------
if [ -n "${GIT_USER_NAME:-}" ]; then
    echo "--> Setting Git identity: $GIT_USER_NAME"
    run_as_hotplex git config --global user.name "$GIT_USER_NAME"
fi
if [ -n "${GIT_USER_EMAIL:-}" ]; then
    run_as_hotplex git config --global user.email "$GIT_USER_EMAIL"
fi

# Auto-configure safe.directory for mounted project volumes
if [ -d "$HOTPLEX_HOME/projects" ]; then
    run_as_hotplex git config --global --add safe.directory "$HOTPLEX_HOME/projects" || true
    # Also add all first-level subdirectories (cloned repos)
    for d in "$HOTPLEX_HOME/projects"/*/; do
        [ -d "$d/.git" ] && run_as_hotplex git config --global --add safe.directory "$d" || true
    done
fi

# ------------------------------------------------------------------------------
# 5. Execute CMD (drop privileges if root)
#    Ensures all files created by the app belong to 'hotplex' user
# ------------------------------------------------------------------------------
echo "==> Starting HotPlex Engine..."
if [ "$(id -u)" = "0" ]; then
    exec runuser -u hotplex -- "$@"
else
    exec "$@"
fi
