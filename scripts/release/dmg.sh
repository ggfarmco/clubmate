#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <path/to/Clubmate.app> <version> <identity|->" >&2
  exit 2
fi

APP="$1"
VERSION="$2"
IDENTITY="$3"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DMG="$ROOT/dist/Clubmate-$VERSION.dmg"

if [[ ! -d "$APP" ]]; then
  echo "error: $APP not found" >&2
  exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
ditto "$APP" "$STAGE/Clubmate.app"
ln -s /Applications "$STAGE/Applications"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$ROOT/THIRD_PARTY_LICENSES.md" "$STAGE/"

mkdir -p "$ROOT/dist"
rm -f "$DMG"
hdiutil create -volname Clubmate -srcfolder "$STAGE" -fs HFS+ -format UDZO -ov "$DMG" >/dev/null

SIGN=(codesign --force --sign "$IDENTITY")
if [[ "$IDENTITY" != "-" ]]; then
  SIGN+=(--timestamp)
fi
"${SIGN[@]}" "$DMG"
codesign --verify --strict "$DMG"
echo "wrote $DMG"
