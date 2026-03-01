#!/bin/bash
#
# SVG to PNG Converter for HotPlex
# Converts all SVG files in docs-site/public to high-resolution PNG
#
# Usage:
#   ./scripts/svg2png.sh [options]
#
# Options:
#   -z, --zoom         Zoom factor for resolution (default: 4)
#   -b, --background   Background color in hex (default: transparent)
#   -h, --help         Show this help message
#
# Examples:
#   ./scripts/svg2png.sh                    # Convert all with defaults
#   ./scripts/svg2png.sh -z 8               # 8x resolution (8K+)
#   ./scripts/svg2png.sh -b "#FFFFFF"       # White background
#

set -e

# Default values
ZOOM=4
BACKGROUND=""

# Source directories (relative to project root)
SOURCE_DIR="docs/images"
OUTPUT_DIR="docs/images/png"

# Target directories for synchronization
DOCS_SITE_PNG="docs-site/public/images/png"
DOCS_SITE_PUBLIC="docs-site/public"
GITHUB_ASSETS=".github/assets"

# Colors for output (only if TTY)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    BLUE='\033[0;34m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    BLUE=''
    NC=''
fi

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -z|--zoom)
            ZOOM="$2"
            shift 2
            ;;
        -b|--background)
            BACKGROUND="$2"
            shift 2
            ;;
        -h|--help)
            sed -n '2,18p' "$0" | sed 's/^# //'
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Check dependencies
check_dependencies() {
    if ! command -v rsvg-convert &> /dev/null; then
        echo -e "${RED}Missing: librsvg${NC}"
        echo "Install: brew install librsvg"
        exit 1
    fi
}

# Convert single SVG to PNG
convert_svg() {
    local svg_file="$1"
    local output_file="$2"
    local filename=$(basename "$svg_file")

    # Build command
    local cmd="rsvg-convert -z $ZOOM"
    [ -n "$BACKGROUND" ] && cmd="$cmd --background-color=\"$BACKGROUND\""
    cmd="$cmd -o \"$output_file\" \"$svg_file\""

    echo -e "  ${BLUE}→${NC} $filename"
    # Execute and capture error without exiting script
    if ! eval $cmd 2>/tmp/svg_err; then
        echo -e "  ${RED}⚠ Warning:${NC} Failed to convert $filename. Skipping..."
        if [ -s /tmp/svg_err ]; then
            echo -e "    ${RED}Error:${NC} $(cat /tmp/svg_err)"
        fi
        return 0
    fi
}

# Main
main() {
    check_dependencies

    local total=0

    # Print header
    echo ""
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}  SVG to PNG Converter${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "  Zoom:       ${BLUE}${ZOOM}x${NC}"
    echo -e "  Background: ${BLUE}${BACKGROUND:-transparent}${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""

    # 1. 核心转换逻辑：docs/images/*.svg -> docs/images/png/*.png
    if [ -d "$SOURCE_DIR" ]; then
        echo -e "${BLUE}Generating high-res PNGs in $OUTPUT_DIR...${NC}"
        mkdir -p "$OUTPUT_DIR"
        for svg_file in "$SOURCE_DIR"/*.svg; do
            [ -e "$svg_file" ] || continue
            local filename=$(basename "$svg_file" .svg)
            convert_svg "$svg_file" "${OUTPUT_DIR}/${filename}.png"
            ((total++))
        done
        echo ""
    fi

    # 2. 同步到文档站点 (用于 Favicon/OG 或潜在的下载链接)
    if [ -d "docs-site/public" ]; then
        echo -e "${BLUE}Synchronizing assets to docs-site...${NC}"
        
        # 同步所有 SVG (SSOT)
        mkdir -p "docs-site/public/images"
        cp "$SOURCE_DIR"/*.svg "docs-site/public/images/"
        
        # 同步所有 PNG 预览图
        mkdir -p "$DOCS_SITE_PNG"
        cp "$OUTPUT_DIR"/*.png "$DOCS_SITE_PNG/"
        
        # 复制品牌核心资产到根目录
        for asset in "logo.png" "author-avatar.png" "mascot_primary.png"; do
            [ -f "$OUTPUT_DIR/$asset" ] && cp "$OUTPUT_DIR/$asset" "$DOCS_SITE_PUBLIC/"
        done
        
        echo -e "  ${GREEN}✓${NC} Assets synced to docs-site/public"
        echo ""
    fi

    echo -e "${GREEN}✓ Success! $total files processed.${NC}"
    echo ""
}

main
