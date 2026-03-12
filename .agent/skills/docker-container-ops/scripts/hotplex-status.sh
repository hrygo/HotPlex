#!/bin/bash
# ==============================================================================
# HotPlex Container Status Checker
# Usage: ./scripts/hotplex-status.sh [--json]
# ==============================================================================

set -e

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$PROJECT_DIR"

OUTPUT_JSON=false
if [[ "$1" == "--json" ]]; then
    OUTPUT_JSON=true
fi

# Get container status
containers=$(docker compose ps --format json 2>/dev/null || docker compose ps)

if $OUTPUT_JSON; then
    echo "$containers" | jq -s 'map({
        name: .Service,
        status: .State,
        health: .Health // "N/A",
        ports: .Ports
    })'
else
    echo "HotPlex Container Status"
    echo "========================="
    docker compose ps
    echo ""
    echo "Resource Usage"
    echo "=============="
    docker stats --no-stream --format "table {{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.NetIO}}" hotplex hotplex-secondary 2>/dev/null || echo "No containers running"
fi
