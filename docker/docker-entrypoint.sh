#!/usr/bin/env bash
set -e

# ==============================================================================
# HotPlex Docker Entrypoint
# Handles permission fixes, config env expansion, git identity, privilege drop.
# Inspired by hotplex-legacy/docker/docker-entrypoint.sh patterns.
# ==============================================================================

HOTPLEX_HOME="/home/hotplex"

# ------------------------------------------------------------------------------
# Helper: Run commands as the hotplex user if currently root
# ------------------------------------------------------------------------------
run_as_hotplex() {
    if [[ "$(id -u)" = "0" ]]; then
        runuser -u hotplex -- env HOME="${HOTPLEX_HOME}" "$@"
    else
        "$@"
    fi
}

# ------------------------------------------------------------------------------
# 1. Fix Permissions & Create Directories (if running as root)
# ------------------------------------------------------------------------------
if [[ "$(id -u)" = "0" ]]; then
    mkdir -p "${HOTPLEX_HOME}/.claude" "${HOTPLEX_HOME}/projects" \
        /var/lib/hotplex/data /var/log/hotplex

    chown -R hotplex:hotplex /var/lib/hotplex /var/log/hotplex 2>/dev/null || true
    chown -R hotplex:hotplex "${HOTPLEX_HOME}/.claude" "${HOTPLEX_HOME}/projects" 2>/dev/null || true

    # Fix .claude.json permissions
    if [[ -f "${HOTPLEX_HOME}/.claude.json" ]]; then
        chown hotplex:hotplex "${HOTPLEX_HOME}/.claude.json" 2>/dev/null || true
    fi
fi

# ------------------------------------------------------------------------------
# 2. Expand Environment Variables in YAML Config Files
# ------------------------------------------------------------------------------
CONFIG_DIR="/etc/hotplex"
if [[ -d "${CONFIG_DIR}" ]]; then
    VARS=$(compgen -A export | grep -E "^(HOTPLEX_|GIT_|GITHUB_|POSTGRES_)" | sed 's/^/$/' | tr '\n' ' ')
    for yaml in "${CONFIG_DIR}"/*.yaml; do
        [[ -f "$yaml" ]] || continue
        if envsubst "${VARS}" < "$yaml" > "${yaml}.tmp"; then
            mv "${yaml}.tmp" "${yaml}"
        else
            rm -f "${yaml}.tmp"
        fi
    done
fi

# ------------------------------------------------------------------------------
# 3. Git Identity Injection (from environment variables)
# ------------------------------------------------------------------------------
if [[ -n "${GIT_USER_NAME:-}" ]]; then
    run_as_hotplex git config --global user.name "${GIT_USER_NAME}" || true
fi
if [[ -n "${GIT_USER_EMAIL:-}" ]]; then
    run_as_hotplex git config --global user.email "${GIT_USER_EMAIL}" || true
fi

# Auto-configure safe.directory for mounted project volumes
if [[ -d "${HOTPLEX_HOME}/projects" ]]; then
    run_as_hotplex git config --global --add safe.directory "${HOTPLEX_HOME}/projects" || true
fi

# ------------------------------------------------------------------------------
# 4. Execute CMD with privilege drop
# ------------------------------------------------------------------------------
if [[ "$(id -u)" = "0" ]]; then
    export HOME="${HOTPLEX_HOME}"
    exec runuser -u hotplex -m -- "$@"
else
    exec "$@"
fi
