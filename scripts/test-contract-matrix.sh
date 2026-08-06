#!/usr/bin/env bash

set -euo pipefail

if ! command -v jq >/dev/null 2>&1; then
	echo "contract matrix: jq is required" >&2
	exit 1
fi

raw_jsonl=$(mktemp)
records=$(mktemp)
trap 'rm -f "$raw_jsonl" "$records"' EXIT

test_pattern='^TestPlatformWorkerContractMatrix_(WebChat|Feishu|Slack)$'
matrix_pattern='/(?<id>F-C|F-O|F-X|F-A|S-C|S-O|S-X|S-A|W-C|W-O|W-X|W-A)/[^/]+/[^/]+/[^/]+/(?<scenario>C0[1-8])-[^/]+$'

set +e
go test -count=1 -race -json -run "$test_pattern" \
	./internal/gateway \
	./internal/messaging/feishu \
	./internal/messaging/slack >"$raw_jsonl"
go_status=$?
set -e

if [[ "$go_status" -ne 0 ]]; then
	cat "$raw_jsonl"
	exit "$go_status"
fi

jq -r --arg pattern "$matrix_pattern" '
	select(.Test != null)
	| select(.Action == "pass" or .Action == "skip" or .Action == "fail")
	| . as $event
	| (try ($event.Test | capture($pattern)) catch null) as $match
	| select($match != null)
	| [$match.id, $match.scenario, $event.Action]
	| @tsv
' "$raw_jsonl" >"$records"

expected_ids=(F-C F-O F-X F-A S-C S-O S-X S-A W-C W-O W-X W-A)
expected_scenarios=(C01 C02 C03 C04 C05 C06 C07 C08)
skipped=$(awk -F '\t' '$3 == "skip" { count++ } END { print count + 0 }' "$records")
failed=0

contains() {
	local needle=$1
	shift
	local item
	for item in "$@"; do
		if [[ "$item" == "$needle" ]]; then
			return 0
		fi
	done
	return 1
}

while IFS=$'\t' read -r id scenario action; do
	if ! contains "$id" "${expected_ids[@]}" || ! contains "$scenario" "${expected_scenarios[@]}"; then
		echo "::error::unexpected contract matrix result: ${id}/${scenario} (${action})"
		failed=$((failed + 1))
	fi
done <"$records"

for id in "${expected_ids[@]}"; do
	for scenario in "${expected_scenarios[@]}"; do
		count=$(awk -F '\t' -v expected_id="$id" -v expected_scenario="$scenario" \
			'$1 == expected_id && $2 == expected_scenario { count++ } END { print count + 0 }' "$records")
		if [[ "$count" -ne 1 ]]; then
			echo "::error::expected exactly one pass for ${id}/${scenario}, got ${count}"
			failed=$((failed + 1))
			continue
		fi

		action=$(awk -F '\t' -v expected_id="$id" -v expected_scenario="$scenario" \
			'$1 == expected_id && $2 == expected_scenario { print $3; exit }' "$records")
		if [[ "$action" != "pass" ]]; then
			echo "::error::contract matrix scenario ${id}/${scenario} finished with ${action}"
			failed=$((failed + 1))
		fi
	done
done

if [[ "$skipped" -ne 0 ]]; then
	echo "::error::contract matrix contains ${skipped} skipped scenario(s)"
	failed=$((failed + skipped))
fi

if [[ "$failed" -ne 0 ]]; then
	echo "contract matrix: 12 combinations, 96 core scenarios, ${skipped} skipped, ${failed} failed"
	exit 1
fi

echo "contract matrix: 12 combinations, 96 core scenarios, 0 skipped, 0 failed"
