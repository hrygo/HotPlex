#!/bin/bash
set -e

# Single Source of Truth
SVG_SOURCE="docs/images/logo.svg"
OUTPUT_DIR=".github/assets"
DOCS_PUBLIC="docs-site/public"
DOCS_ASSETS="docs-site/public/assets"

mkdir -p $OUTPUT_DIR
mkdir -p $DOCS_ASSETS

echo "1/4: 生成基础底图 (1024x1024)..."
# 使用 cairosvg 将源 SVG 转换为 PNG 这里的 1000x1000 比例会被拉伸到 1024x1024
cairosvg $SVG_SOURCE -W 1024 -H 1024 -o $OUTPUT_DIR/hotplex-logo.png

echo "2/4: 生成 Open Graph 社交预览图 (1200x630)..."
# 使用白色背景并将 Logo 居中缩小放置
magick -size 1200x630 xc:"#FFFFFF" \
  \( $OUTPUT_DIR/hotplex-logo.png -resize 600x600 \) \
  -gravity center -composite \
  $OUTPUT_DIR/hotplex-og.png

echo "3/4: 生成多尺寸 favicon.ico..."
magick -background none $OUTPUT_DIR/hotplex-logo.png -define icon:auto-resize=256,128,64,48,32,16 $OUTPUT_DIR/favicon.ico

echo "4/4: 同步到文档站点演示目录..."
# 同步 SVG
cp $SVG_SOURCE $DOCS_PUBLIC/logo.svg
cp $SVG_SOURCE $DOCS_ASSETS/hotplex-logo.svg

# 同步 favicon 和 社交预览图
cp $OUTPUT_DIR/favicon.ico $DOCS_PUBLIC/favicon.ico
cp $OUTPUT_DIR/favicon.ico $DOCS_ASSETS/favicon.ico
cp $OUTPUT_DIR/hotplex-og.png $DOCS_ASSETS/hotplex-og.png

echo "完成！资产已基于 SSOT ($SVG_SOURCE) 重新生成并同步到所有目录。"
