#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
arch="${1:-$(go env GOARCH)}"
case "$arch" in arm64|amd64) ;; *) echo 'Use arm64 or amd64' >&2; exit 1 ;; esac
app="dist/macos-$arch/OpenPixelBar.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -trimpath -ldflags '-s -w' -o "$app/Contents/MacOS/OpenPixelBar" ./cmd/openpixelbar
cp packaging/Info.plist "$app/Contents/Info.plist"
cp internal/branding/icon.icns "$app/Contents/Resources/OpenPixelBar.icns"
cp README.md THIRD_PARTY_NOTICES.txt "$app/Contents/Resources/"
chmod 755 "$app/Contents/MacOS/OpenPixelBar"
if [ "$(uname -s)" = Darwin ]; then
  codesign --force --deep --sign - "$app"
  ditto -c -k --keepParent "$app" "dist/OpenPixelBar-macos-$arch.zip"
fi
echo "$app"
