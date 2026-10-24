#!/bin/sh
# Builds the plugin for this machine's OS and CPU into dist/.
# Usage: sh scripts/build.sh [version]
set -eu
VERSION=${1:-0.1.0}
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
case "$(go env GOOS)" in
  darwin) EXT=dylib ;;
  windows) EXT=dll ;;
  *) EXT=so ;;
esac
OUT="$ROOT/dist/quota-history-v$VERSION.$EXT"
mkdir -p "$ROOT/dist"
cd "$ROOT"
CGO_ENABLED=1 go build -trimpath -buildmode=c-shared -ldflags "-s -w -X main.version=$VERSION" -o "$OUT" .
rm -f "${OUT%.*}.h"
echo "$OUT"
