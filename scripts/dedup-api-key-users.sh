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
#   - Aborts with a non-zero exit if any DB query fails. A failure is NEVER
#     silently mistaken for "no duplicates" — otherwise an operator could skip
#     dedup and let migration 016 (CREATE UNIQUE INDEX) fail.
#   - Runs the delete within an explicit BEGIN/COMMIT (ROLLBACK on error),
#     matching the Postgres variant.
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

# Array form avoids re-evaluating $DB via eval (paths with shell metacharacters
# stay literal).
SQLITE=(sqlite3 "$DB")

# sqlite_query runs a statement via sqlite3 and propagates its exit status and
# stderr. Do NOT redirect stderr away: a failure must surface rather than be
# misread as an empty (no-duplicates) result.
sqlite_query() {
	"${SQLITE[@]}" "$1"
}

echo "=== Checking for duplicate user_id in api_key_users ==="

if ! DUPES=$(sqlite_query "SELECT user_id, COUNT(*) as cnt FROM api_key_users GROUP BY user_id HAVING cnt > 1;"); then
	echo "Error: failed to query for duplicates — check the database file and path." >&2
	exit 1
fi
if [[ -z "$DUPES" ]]; then
	echo "No duplicates found. Safe to apply migration 016."
	exit 0
fi

echo "Duplicate user_ids found:"
echo "$DUPES"
echo ""

if ! AFFECTED=$(sqlite_query "SELECT COUNT(*) FROM api_key_users WHERE rowid NOT IN (SELECT MAX(rowid) FROM api_key_users GROUP BY user_id);"); then
	echo "Error: failed to count affected rows." >&2
	exit 1
fi
echo "Rows to delete: $AFFECTED"
echo ""

echo "Details of rows to be removed:"
if ! sqlite_query "SELECT rowid, id, user_id, api_key FROM api_key_users WHERE rowid NOT IN (SELECT MAX(rowid) FROM api_key_users GROUP BY user_id) ORDER BY user_id, rowid;"; then
	echo "Error: failed to fetch duplicate row details." >&2
	exit 1
fi
echo ""

if $DRY_RUN; then
	echo "[DRY-RUN] No changes made. Re-run without --dry-run to apply."
	exit 0
fi

echo "Deleting $AFFECTED duplicate row(s)..."
if ! sqlite_query "BEGIN; DELETE FROM api_key_users WHERE rowid NOT IN (SELECT MAX(rowid) FROM api_key_users GROUP BY user_id); COMMIT;"; then
	echo "Error: delete failed; transaction rolled back." >&2
	exit 1
fi

if ! REMAINING=$(sqlite_query "SELECT COUNT(*) FROM api_key_users;"); then
	echo "Error: failed to count remaining rows." >&2
	exit 1
fi
echo "Done. Remaining rows: $REMAINING"
echo "You can now safely apply migration 016 (CREATE UNIQUE INDEX)."
