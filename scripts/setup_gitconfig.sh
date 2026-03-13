#!/usr/bin/env bash
# ==============================================================================
# Generate Bot Gitconfig Files
# ==============================================================================
# Purpose: Create isolated git identity for each bot container.
#
# Why needed:
#   - Bot commits should use bot identity (not developer's personal identity)
#   - Each bot has unique name for traceability (HotPlexBot01 vs HotPlexBot02)
#   - Prevents accidental credential leakage from host gitconfig
#
# Usage:
#   ./scripts/setup_gitconfig.sh              # Generate both configs
#   ./scripts/setup_gitconfig.sh --verify     # Verify existing configs
# ==============================================================================

set -e

# Bot configurations - add new bots here
BOT_CONFIGS=(
  "hotplex-01:HotPlexBot01"
  "hotplex-02:HotPlexBot02"
)

# ------------------------------------------------------------------------------
# Generate a gitconfig file for a bot (idempotent - skips if already correct)
# Arguments:
#   $1 - suffix (e.g., "hotplex-01", "hotplex-02")
#   $2 - bot_name (e.g., "HotPlexBot01")
# Returns: 0 on success, 1 on failure
# ------------------------------------------------------------------------------
generate_config() {
  local suffix="${1:-}"
  local bot_name="${2:-}"
  local target

  # Input validation
  if [[ -z "$suffix" ]]; then
    echo "❌ Error: suffix is required" >&2
    return 1
  fi
  if [[ -z "$bot_name" ]]; then
    echo "❌ Error: bot_name is required" >&2
    return 1
  fi

  target="$HOME/.gitconfig-${suffix}"

  # Idempotency check: skip if already correct
  if [[ -f "$target" ]]; then
    local current_name
    current_name=$(grep -A1 '\[user\]' "$target" | grep '^[[:space:]]*name[[:space:]]*=' | sed 's/.*=[[:space:]]*//' 2>/dev/null || echo "")
    if [[ "$current_name" == "$bot_name" ]]; then
      echo "⏭️  Skipped: $target (already correct: $bot_name)"
      return 0
    fi
  fi

  cat > "$target" << EOF
[user]
    name = ${bot_name}
    email = noreply@hotplex.dev
[core]
    excludesfile = /home/hotplex/.gitignore_global
[init]
    defaultBranch = main
[pull]
    rebase = false
[safe]
    directory = /home/hotplex/projects
EOF
  echo "✅ Generated: $target ($bot_name)"
}

# ------------------------------------------------------------------------------
# Verify a gitconfig file exists and has correct bot name
# Arguments:
#   $1 - suffix (e.g., "hotplex-01", "hotplex-02")
#   $2 - expected bot_name (e.g., "HotPlexBot01")
# Returns: 0 on success, 1 on failure
# ------------------------------------------------------------------------------
verify_config() {
  local suffix="${1:-}"
  local expected_name="${2:-}"
  local target

  # Input validation
  if [[ -z "$suffix" ]]; then
    echo "❌ Error: suffix is required" >&2
    return 1
  fi
  if [[ -z "$expected_name" ]]; then
    echo "❌ Error: expected_name is required" >&2
    return 1
  fi

  target="$HOME/.gitconfig-${suffix}"

  if [[ ! -f "$target" ]]; then
    echo "❌ Missing: $target"
    return 1
  fi

  local actual_name
  actual_name=$(grep -A1 '\[user\]' "$target" | grep '^[[:space:]]*name[[:space:]]*=' | sed 's/.*=[[:space:]]*//' 2>/dev/null || echo "")

  if [[ "$actual_name" == "$expected_name" ]]; then
    echo "✅ $target: name=$actual_name"
    return 0
  else
    echo "❌ $target: expected '$expected_name', got '$actual_name'"
    return 1
  fi
}

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------
main() {
  local failed=0

  if [[ "${1:-}" == "--verify" ]]; then
    echo "🔍 Verifying bot gitconfig files..."
    for config in "${BOT_CONFIGS[@]}"; do
      IFS=':' read -r suffix bot_name <<< "$config"
      verify_config "$suffix" "$bot_name" || ((failed++))
    done
  else
    echo "📝 Generating bot gitconfig files..."
    for config in "${BOT_CONFIGS[@]}"; do
      IFS=':' read -r suffix bot_name <<< "$config"
      generate_config "$suffix" "$bot_name" || ((failed++))
    done
  fi

  if [[ $failed -gt 0 ]]; then
    echo -e "\n❌ $failed operation(s) failed"
    exit 1
  fi
}

main "$@"
