#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <path/to/Clubmate.app>" >&2
  exit 2
fi

APP="$1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin:$PATH"

make -C "$ROOT" build VERSION="${MARKETING_VERSION:-0.0.0-dev}" COMMIT="${CLUBMATE_COMMIT:-}" FLAVOR="${CLUBMATE_FLAVOR:-dev}"

DEST="$APP/Contents/Resources/bin"
rm -rf "$DEST"
mkdir -p "$DEST"
cp "$ROOT/bin/clubmate" "$ROOT/bin/clubmate-core" "$ROOT/bin/clubmate-session" "$DEST/"
cp -R "$ROOT/bin/plugins" "$DEST/plugins"
cp "$ROOT/LICENSE" "$ROOT/NOTICE" "$ROOT/THIRD_PARTY_LICENSES.md" "$APP/Contents/Resources/"
"$ROOT/scripts/vendor-tools.sh" "$APP/Contents/Resources"

SIGN=(codesign --force --options runtime --sign -)
SPARKLE="$APP/Contents/Frameworks/Sparkle.framework"

"${SIGN[@]}" \
  "$DEST/clubmate" "$DEST/clubmate-core" "$DEST/clubmate-session" "$DEST/plugins/terminals/terminals" "$DEST/plugins/editor-nvim/editor-nvim" "$DEST/plugins/files/files" "$DEST/plugins/git/git" "$DEST/plugins/processes/processes" "$DEST/plugins/search/search" "$DEST/plugins/agents/agents" "$DEST/plugins/remote/remote"
"${SIGN[@]}" "$APP/Contents/Resources/tools/rg"
ADHOC=(codesign --force --sign -)
for parser in "$APP"/Contents/Resources/nvim/lib/nvim/parser/*.so; do
  "${ADHOC[@]}" "$parser"
done
"${ADHOC[@]}" "$APP/Contents/Resources/nvim/bin/nvim"
"${SIGN[@]}" "$SPARKLE/Versions/B/XPCServices/Installer.xpc"
"${SIGN[@]}" --preserve-metadata=entitlements "$SPARKLE/Versions/B/XPCServices/Downloader.xpc"
"${SIGN[@]}" "$SPARKLE/Versions/B/Autoupdate"
"${SIGN[@]}" "$SPARKLE/Versions/B/Updater.app"
"${SIGN[@]}" "$SPARKLE"
ENTITLEMENTS="${TARGET_TEMP_DIR:-}/${FULL_PRODUCT_NAME:-}.xcent"
if [[ -n "${TARGET_TEMP_DIR:-}" && -f "$ENTITLEMENTS" ]]; then
  "${SIGN[@]}" --entitlements "$ENTITLEMENTS" "$APP"
else
  "${SIGN[@]}" --preserve-metadata=entitlements "$APP"
fi
