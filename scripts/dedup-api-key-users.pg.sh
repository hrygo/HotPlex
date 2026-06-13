#!/usr/bin/env bash
# dedup-api-key-users.pg.sh — Deduplicate api_key_users so each user_id maps to exactly one API key.
#
# Usage:
#   ./scripts/dedup-api-key-users.pg.sh <postgres-uri> [--dry-run]
#
#   postgres-uri format: postgresql://user:pass@host:port/dbname?sslmode=disable
#
# Behavior:
#   --dry-run  Show duplicates and what would be deleted, without modifying data.
#   Default    Delete older rows, keeping the latest (largest id) per user_id.
#
# Safety:
#   - Runs within a transaction (ROLLBACK on error).
#   - Exits if no duplicates found.
#   - Prints affected rows before deletion.
set -euo pipefail

if [[ $# -lt 1 ]]; then
	echo "Usage: $0 <postgres-uri> [--dry-run]" >&2
	echo "  --dry-run  Show duplicates without deleting." >&2
	exit 1
fi

PG_URI="$1"
DRY_RUN=false
if [[ "${2:-}" == "--dry-run" ]]; then
	DRY_RUN=true
fi

PSQL="psql \"$PG_URI\" -t -A"

echo "=== Checking for duplicate user_id in api_key_users ==="

DUPES=$(eval "$PSQL" -c "SELECT user_id, COUNT(*) as cnt FROM api_key_users GROUP BY user_id HAVING cnt > 1;" 2>/dev/null)
if [[ -z "$DUPES" ]]; then
	echo "No duplicates found. Safe to apply migration 016."
	exit 0
fi

echo "Duplicate user_ids found:"
echo "$DUPES"
echo ""

AFFECTED=$(eval "$PSQL" -c "SELECT COUNT(*) FROM api_key_users WHERE id NOT IN (SELECT MAX(id) FROM api_key_users GROUP BY user_id);" 2>/dev/null | tr -d ' ')
echo "Rows to delete: $AFFECTED"
echo ""

echo "Details of rows to be removed:"
eval "$PSQL" -c "SELECT id, user_id, api_key FROM api_key_users WHERE id NOT IN (SELECT MAX(id) FROM api_key_users GROUP BY user_id) ORDER BY user_id, id;" 2>/dev/null
echo ""

if $DRY_RUN; then
	echo "[DRY-RUN] No changes made. Re-run without --dry-run to apply."
	exit 0
fi

echo "Deleting $AFFECTED duplicate row(s)..."
eval "$PSQL" -c "BEGIN; DELETE FROM api_key_users WHERE id NOT IN (SELECT MAX(id) FROM api_key_users GROUP BY user_id); COMMIT;" 2>/dev/null

REMAINING=$(eval "$PSQL" -c "SELECT COUNT(*) FROM api_key_users;" 2>/dev/null | tr -d ' ')
echo "Done. Remaining rows: $REMAINING"
echo "You can now safely apply migration 016 (CREATE UNIQUE INDEX)."
