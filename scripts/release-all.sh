#!/bin/bash
# release-all.sh - 构建全平台 CLI 发布产物到 dist/（不上传、不改 git）。
#
# 版本唯一来源是 desktop/package.json；buildinfo.go 与之不一致就直接失败——
# v0.0.34 的教训：tag 是 0.0.34，二进制里嵌的却是 0.0.33。
# 桌面安装包不在这里构建：cd desktop && npm run pack:win（产物在系统临时目录 gleam-pack/）。
# 上传是显式动作：脚本结尾打印 gh release upload 命令，确认后手动执行。

set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

VERSION=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' desktop/package.json | head -1)
GO_VERSION=$(sed -n 's/^var Version = "\(.*\)"$/\1/p' internal/buildinfo/buildinfo.go)
if [ -z "$VERSION" ] || [ "$VERSION" != "$GO_VERSION" ]; then
  echo "版本不一致：desktop/package.json='$VERSION'，internal/buildinfo/buildinfo.go='$GO_VERSION'" >&2
  exit 1
fi
echo "构建 v$VERSION"

LDFLAGS="-s -w -X gleam/internal/buildinfo.Version=$VERSION"
TARGETS="windows/amd64 linux/amd64 darwin/amd64 darwin/arm64"

for target in $TARGETS; do
  os=${target%/*}
  arch=${target#*/}
  name="gleam-v$VERSION-$os-$arch"
  [ "$os" = "windows" ] && name="$name.exe"
  mkdir -p "dist/$os/$arch"
  echo "  -> dist/$os/$arch/$name"
  CGO_ENABLED=0 GOOS=$os GOARCH=$arch \
    go build -trimpath -ldflags "$LDFLAGS" -o "dist/$os/$arch/$name" ./cmd/gleam
done

# 官网下载按钮指向 releases/latest/download/gleam-<os>-<arch>，裸二进制名不能带版本号。
cp "dist/windows/amd64/gleam-v$VERSION-windows-amd64.exe" dist/gleam-windows-amd64.exe
cp "dist/linux/amd64/gleam-v$VERSION-linux-amd64" dist/gleam-linux-amd64
cp "dist/darwin/amd64/gleam-v$VERSION-darwin-amd64" dist/gleam-darwin-amd64
cp "dist/darwin/arm64/gleam-v$VERSION-darwin-arm64" dist/gleam-darwin-arm64

cd dist
# 校验和必须对应 Release 上的扁平资产名，下载后 sha256sum -c 才能直接通过。
sha256sum gleam-windows-amd64.exe > windows-amd64-checksums.txt
sha256sum gleam-linux-amd64 > linux-amd64-checksums.txt
sha256sum gleam-windows-amd64.exe gleam-linux-amd64 gleam-darwin-amd64 gleam-darwin-arm64 > "CHECKSUMS-v$VERSION.txt"

for target in linux/amd64 darwin/amd64 darwin/arm64; do
  os=${target%/*}
  arch=${target#*/}
  tar -czf "Gleam_v${VERSION}_${os}_${arch}.tar.gz" -C "$os/$arch" "gleam-v$VERSION-$os-$arch"
done

echo ""
echo "CLI 产物就绪。后续步骤："
echo "  1. 桌面安装包：cd desktop && npm run pack:win"
echo "  2. 上传（把系统临时目录 gleam-pack/ 的三件套拷进 dist/ 后）："
echo "     gh release upload v$VERSION dist/Gleam-Setup.exe dist/Gleam-Setup.exe.blockmap dist/latest.yml \\"
echo "       dist/gleam-windows-amd64.exe dist/gleam-darwin-amd64 dist/gleam-darwin-arm64 dist/gleam-linux-amd64 \\"
echo "       dist/Gleam_v${VERSION}_darwin_amd64.tar.gz dist/Gleam_v${VERSION}_darwin_arm64.tar.gz dist/Gleam_v${VERSION}_linux_amd64.tar.gz \\"
echo "       dist/CHECKSUMS-v$VERSION.txt dist/windows-amd64-checksums.txt dist/linux-amd64-checksums.txt --clobber"