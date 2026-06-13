#!/usr/bin/env bash
# dedup-api-key-users.sh — Deduplicate api_key_users so each user_id maps to exactly one API key.
#
# Usage:
#   ./scripts/dedup-api-key-users.sh <sqlite-db-path> [--dry-run]
#
# Behavior:
#   --dry-run  Show duplicates and what would be deleted, without modifying data.
#   Default    Delete older rows, keeping the latest (largest rowid) per user_id.
#
# Safety:
#   - Exits if DB file does not exist.
#   - Exits if no duplicates found.
#   - Prints affected rows before deletion.
set -euo pipefail

if [[ $# -lt 1 ]]; then
	echo "Usage: $0 <sqlite-db-path> [--dry-run]" >&2
	echo "  --dry-run  Show duplicates without deleting." >&2
	exit 1
fi

DB="$1"
DRY_RUN=false
if [[ "${2:-}" == "--dry-run" ]]; then
	DRY_RUN=true
fi

if [[ ! -f "$DB" ]]; then
	echo "Error: database file not found: $DB" >&2
	exit 1
fi

SQLITE="sqlite3 \"$DB\""

echo "=== Checking for duplicate user_id in api_key_users ==="

DUPES=$(eval "$SQLITE" "SELECT user_id, COUNT(*) as cnt FROM api_key_users GROUP BY user_id HAVING cnt > 1;")
if [[ -z "$DUPES" ]]; then
	echo "No duplicates found. Safe to apply migration 016."
	exit 0
fi

echo "Duplicate user_ids found:"
echo "$DUPES"
echo ""

AFFECTED=$(eval "$SQLITE" "SELECT COUNT(*) FROM api_key_users WHERE rowid NOT IN (SELECT MAX(rowid) FROM api_key_users GROUP BY user_id);")
echo "Rows to delete: $AFFECTED"
echo ""

echo "Details of rows to be removed:"
eval "$SQLITE" "SELECT rowid, id, user_id, api_key FROM api_key_users WHERE rowid NOT IN (SELECT MAX(rowid) FROM api_key_users GROUP BY user_id) ORDER BY user_id, rowid;"
echo ""

if $DRY_RUN; then
	echo "[DRY-RUN] No changes made. Re-run without --dry-run to apply."
	exit 0
fi

echo "Deleting $AFFECTED duplicate row(s)..."
eval "$SQLITE" "DELETE FROM api_key_users WHERE rowid NOT IN (SELECT MAX(rowid) FROM api_key_users GROUP BY user_id);"

REMAINING=$(eval "$SQLITE" "SELECT COUNT(*) FROM api_key_users;")
echo "Done. Remaining rows: $REMAINING"
echo "You can now safely apply migration 016 (CREATE UNIQUE INDEX)."
