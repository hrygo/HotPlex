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
#   - Aborts with a non-zero exit if any DB query/connection fails. A failure is
#     NEVER silently mistaken for "no duplicates" — otherwise an operator could
#     skip dedup and let migration 016 (CREATE UNIQUE INDEX) fail.
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

# psql_query runs a statement via psql and propagates psql's exit status and
# stderr. Do NOT redirect stderr away: a connection/auth failure must surface
# rather than be misread as an empty (no-duplicates) result.
psql_query() {
	eval "$PSQL" -c "$1"
}

echo "=== Checking for duplicate user_id in api_key_users ==="

if ! DUPES=$(psql_query "SELECT user_id, COUNT(*) as cnt FROM api_key_users GROUP BY user_id HAVING cnt > 1;"); then
	echo "Error: failed to query for duplicates — check the Postgres URI, credentials, and network." >&2
	exit 1
fi
if [[ -z "$DUPES" ]]; then
	echo "No duplicates found. Safe to apply migration 016."
	exit 0
fi

echo "Duplicate user_ids found:"
echo "$DUPES"
echo ""

if ! AFFECTED=$(psql_query "SELECT COUNT(*) FROM api_key_users WHERE id NOT IN (SELECT MAX(id) FROM api_key_users GROUP BY user_id);"); then
	echo "Error: failed to count affected rows." >&2
	exit 1
fi
AFFECTED=$(echo "$AFFECTED" | tr -d ' ')
echo "Rows to delete: $AFFECTED"
echo ""

echo "Details of rows to be removed:"
if ! psql_query "SELECT id, user_id, api_key FROM api_key_users WHERE id NOT IN (SELECT MAX(id) FROM api_key_users GROUP BY user_id) ORDER BY user_id, id;"; then
	echo "Error: failed to fetch duplicate row details." >&2
	exit 1
fi
echo ""

if $DRY_RUN; then
	echo "[DRY-RUN] No changes made. Re-run without --dry-run to apply."
	exit 0
fi

echo "Deleting $AFFECTED duplicate row(s)..."
if ! psql_query "BEGIN; DELETE FROM api_key_users WHERE id NOT IN (SELECT MAX(id) FROM api_key_users GROUP BY user_id); COMMIT;"; then
	echo "Error: delete failed; transaction rolled back." >&2
	exit 1
fi

if ! REMAINING=$(psql_query "SELECT COUNT(*) FROM api_key_users;"); then
	echo "Error: failed to count remaining rows." >&2
	exit 1
fi
REMAINING=$(echo "$REMAINING" | tr -d ' ')
echo "Done. Remaining rows: $REMAINING"
echo "You can now safely apply migration 016 (CREATE UNIQUE INDEX)."
