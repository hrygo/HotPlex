#!/bin/bash
# HotPlex Asset Orchestrator (SSOT System)
#
# This script manages all visual assets for the project.
# Single Source of Truth: docs/images/*.svg
# Generated Output: docs/images/png/ & docs-site/public/
#
# Usage:
#   ./scripts/generate_assets.sh                # Sync + Core Assets (logo/favicon/og)
#   ./scripts/generate_assets.sh --all-pngs     # Core + Full SVG-to-PNG Conversion
#

set -e

# --- Configuration ---
# All paths relative to project root
SRC_DIR="docs/images"
PNG_DIR="docs/images/png"
SITE_PUBLIC="docs-site/public"
SITE_IMAGES="$SITE_PUBLIC/images"
SITE_PNG="$SITE_PUBLIC/images/png"
SITE_ASSETS="$SITE_PUBLIC/assets"

# Temporary buffers for intermediate generation
TMP_ASSETS="/tmp/hotplex-generation-$(date +%u%s)"

echo "🚀 HotPlex Asset Pipeline Started"

# 0. Preparation
# Ensure all target directories exist
mkdir -p "$PNG_DIR" "$SITE_PUBLIC" "$SITE_IMAGES" "$SITE_PNG" "$SITE_ASSETS" "$TMP_ASSETS"

# --- Tools Discovery ---
if command -v rsvg-convert &> /dev/null; then
    CONVERTER="rsvg"
elif command -v cairosvg &> /dev/null; then
    CONVERTER="cairosvg"
else
    echo "::warning title=Asset Tool Missing::No SVG converter found (rsvg-convert or cairosvg). PNG generation will be limited or skipped."
    CONVERTER="none"
fi

# Detect ImageMagick (v6/v7) for Favicon/OG composition
if command -v magick &> /dev/null; then
    IM_CMD="magick"
elif command -v convert &> /dev/null; then
    IM_CMD="convert"
else
    IM_CMD="none"
fi

# --- Helper Logic ---
convert_svg_to_png() {
    local src="$1"
    local dest="$2"
    local zoom="${3:-4}"
    
    [ "$CONVERTER" == "none" ] && return
    
    if [ "$CONVERTER" == "rsvg" ]; then
        rsvg-convert -z "$zoom" -a -o "$dest" "$src"
    else
        cairosvg "$src" --scale "$zoom" -o "$dest"
    fi
}

# --- Pipeline Steps ---

# 1. CORE BRAND ASSETS (Favicon, Logo, OG)
# These are the high-priority assets used for site identity and social sharing.
if [ -f "$SRC_DIR/logo.svg" ] && [ "$IM_CMD" != "none" ] && [ "$CONVERTER" != "none" ]; then
    echo "🎨 Step 1: Generating Brand Infrastructure (Favicon, OG, Logos)..."
    
    # 1.1 Master Logo PNG (4x zoom)
    convert_svg_to_png "$SRC_DIR/logo.svg" "$TMP_ASSETS/master-logo.png" 4
    cp "$TMP_ASSETS/master-logo.png" "$PNG_DIR/logo.png"
    
    # 1.2 Multi-size Favicon
    $IM_CMD -background none "$TMP_ASSETS/master-logo.png" \
        -define icon:auto-resize=256,128,64,48,32,16 \
        "$SITE_PUBLIC/favicon.ico"
    
    # 1.3 Social Preview (Open Graph) - 1200x630 with padded logo
    $IM_CMD -size 1200x630 xc:"#FFFFFF" "$TMP_ASSETS/bg.png"
    $IM_CMD "$TMP_ASSETS/master-logo.png" -resize 600x600 "$TMP_ASSETS/logo-og.png"
    $IM_CMD "$TMP_ASSETS/bg.png" "$TMP_ASSETS/logo-og.png" -gravity center -composite "$SITE_ASSETS/hotplex-og.png"

    # 1.4 Sync SVG version for web use
    cp "$SRC_DIR/logo.svg" "$SITE_PUBLIC/logo.svg"
    cp "$TMP_ASSETS/master-logo.png" "$SITE_PUBLIC/logo.png"
    echo "   ✓ Core brand assets ready"
else
    echo "   ⚠ Skipping Step 1 (logo.svg missing or tools missing)"
fi

# 2. SSOT SYNC (SVG)
# Synchronize all original SVGs to the runtime directory.
echo "📦 Step 2: Synchronizing SVGs (SSOT) to Docs Site..."
# Using find to handle potential empty dir safely
find "$SRC_DIR" -maxdepth 1 -name "*.svg" -exec cp {} "$SITE_IMAGES/" \;
echo "   ✓ SVGs synchronized"

# 3. CONTENT PNGS (Conditional / CI)
# Generate high-resolution PNG alternatives for all SVGs.
# Always runs in GITHUB_ACTIONS or if --all-pngs flag is provided.
if [[ "$*" == *"--all-pngs"* ]] || [ -n "$GITHUB_ACTIONS" ]; then
    echo "🖼️ Step 3: Converting all SVGs to high-res PNG fallback..."
    if [ "$CONVERTER" != "none" ]; then
        for svg in "$SRC_DIR"/*.svg; do
            [ -e "$svg" ] || continue
            filename=$(basename "$svg" .svg)
            convert_svg_to_png "$svg" "$PNG_DIR/$filename.png" 4
        done
        echo "   ✓ PNG conversion complete"
    else
        echo "   ⚠ Skipping Step 3 (No converter found)"
    fi
fi

# 4. RUNTIME SYNC (PNG)
# Ensure any generated PNGs (new or cached) are available to the documentation site.
echo "🔄 Step 4: Finalizing Runtime Assets..."
if [ -d "$PNG_DIR" ] && [ "$(ls -A "$PNG_DIR" 2>/dev/null)" ]; then
    cp -r "$PNG_DIR"/* "$SITE_PNG/"
    echo "   ✓ PNGs synchronized to site"
fi

# Cleanup
rm -rf "$TMP_ASSETS"

echo "✅ Asset Processing Complete. SSOT is maintained."
