#!/bin/bash
set -e

# Single Source of Truth
SVG_SOURCE="docs/images/logo.svg"
GITHUB_ASSETS=".github/assets"
DOC_IMAGES_PNG="docs/images/png"
DOCS_PUBLIC="docs-site/public"
DOCS_ASSETS="docs-site/public/assets"

mkdir -p $GITHUB_ASSETS $DOC_IMAGES_PNG $DOCS_ASSETS

echo "1/4: 生成基础底图 (1024x1024)..."
# 生成 GitHub 预览图并作为中间件使用
cairosvg $SVG_SOURCE -W 1024 -H 1024 -o $GITHUB_ASSETS/hotplex-logo.png
# 同步一份到项目文档图片库
cp $GITHUB_ASSETS/hotplex-logo.png $DOC_IMAGES_PNG/logo.png

echo "2/4: 生成 Open Graph 社交预览图 (1200x630)..."
# 使用透明背景或根据需要设置背景 (这里维持 SSOT 逻辑)
magick -size 1200x630 xc:"#FFFFFF" \
  \( $GITHUB_ASSETS/hotplex-logo.png -resize 600x600 \) \
  -gravity center -composite \
  $GITHUB_ASSETS/hotplex-og.png

echo "3/4: 生成多尺寸 favicon.ico..."
magick -background none $GITHUB_ASSETS/hotplex-logo.png -define icon:auto-resize=256,128,64,48,32,16 $GITHUB_ASSETS/favicon.ico

echo "4/4: 同步到文档站点演示目录..."
# 同步源 SVG 到 public 根目录 (用于 site logo)
[ -d "$DOCS_PUBLIC" ] && cp $SVG_SOURCE $DOCS_PUBLIC/logo.svg
# 同步 favicon 和 社交预览图
[ -d "$DOCS_PUBLIC" ] && cp $GITHUB_ASSETS/favicon.ico $DOCS_PUBLIC/favicon.ico
[ -d "$DOCS_ASSETS" ] && cp $GITHUB_ASSETS/hotplex-og.png $DOCS_ASSETS/hotplex-og.png
# 把高分辨率 PNG 也同步一份过去
[ -d "$DOCS_PUBLIC" ] && cp $GITHUB_ASSETS/hotplex-logo.png $DOCS_PUBLIC/logo.png

echo "完成！资产已基于 SSOT ($SVG_SOURCE) 重新生成并同步到 $DOC_IMAGES_PNG 和 $DOCS_PUBLIC。"
