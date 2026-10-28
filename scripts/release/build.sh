#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

VERSION="$1"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "error: version must be X.Y.Z or X.Y.Z-suffix, got '$VERSION'" >&2
  exit 2
fi
if [[ -z "${SPARKLE_PUBLIC_KEY:-}" ]]; then
  echo "error: SPARKLE_PUBLIC_KEY is not set" >&2
  exit 1
fi

COMMIT="$(git rev-parse HEAD)"
BUILD="$(git rev-list --count HEAD)"
PRODUCT="build/DerivedData/Build/Products/Release/Clubmate.app"

make ghostty
(cd client && xcodegen generate)
rm -rf "$PRODUCT"
xcodebuild -project client/Clubmate.xcodeproj -scheme Clubmate -configuration Release \
  -derivedDataPath build/DerivedData \
  MARKETING_VERSION="$VERSION" \
  CURRENT_PROJECT_VERSION="$BUILD" \
  CLUBMATE_COMMIT="$COMMIT" \
  CLUBMATE_FLAVOR=release \
  SPARKLE_PUBLIC_KEY="$SPARKLE_PUBLIC_KEY" \
  build

mkdir -p dist
rm -rf dist/Clubmate.app
ditto "$PRODUCT" dist/Clubmate.app
echo "built dist/Clubmate.app $VERSION ($BUILD, ${COMMIT:0:7})"
