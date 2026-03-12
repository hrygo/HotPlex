#!/bin/bash
# ==============================================================================
# HotPlex Session Cleanup Script
# Usage: ./scripts/cleanup-sessions.sh [--all|--older-than HOURS]
# ==============================================================================

set -e

HOTPLEX_DIR="${HOME}/.hotplex"
MARKERS_DIR="${HOTPLEX_DIR}/markers"

usage() {
    echo "Usage: $0 [--all|--older-than HOURS]"
    echo ""
    echo "Options:"
    echo "  --all             Remove all session markers"
    echo "  --older-than HOURS Remove markers older than HOURS"
    exit 1
}

if [[ $# -eq 0 ]]; then
    usage
fi

MODE=""
HOURS=0

while [[ $# -gt 0 ]]; do
    case "$1" in
        --all)
            MODE="all"
            shift
            ;;
        --older-than)
            MODE="older"
            HOURS="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

echo "HotPlex Session Cleanup"
echo "======================="
echo "Markers directory: ${MARKERS_DIR}"
echo ""

if [[ ! -d "$MARKERS_DIR" ]]; then
    echo "No markers directory found. Is hotplex running?"
    exit 1
fi

case "$MODE" in
    all)
        count=$(ls -1 "$MARKERS_DIR" | wc -l)
        echo "Removing all $count session markers..."
        rm -f "$MARKERS_DIR"/*
        echo "Done."
        ;;
    older)
        count=$(find "$MARKERS_DIR" -type f -mmin +$((HOURS * 60)) | wc -l)
        echo "Removing $count markers older than $HOURS hours..."
        find "$MARKERS_DIR" -type f -mmin +$((HOURS * 60)) -delete
        echo "Done."
        ;;
esac

echo ""
echo "Remaining markers:"
ls -la "$MARKERS_DIR" 2>/dev/null || echo "(empty)"
