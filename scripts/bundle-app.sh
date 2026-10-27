#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <path/to/Clubmate.app>" >&2
  exit 2
fi

APP="$1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH="/opt/homebrew/bin:/usr/local/go/bin:/usr/local/bin:$PATH"

make -C "$ROOT" build

DEST="$APP/Contents/Resources/bin"
rm -rf "$DEST"
mkdir -p "$DEST"
cp "$ROOT/bin/clubmate" "$ROOT/bin/clubmate-core" "$ROOT/bin/clubmate-session" "$DEST/"
cp -R "$ROOT/bin/plugins" "$DEST/plugins"

codesign --force --options runtime --sign - \
  "$DEST/clubmate" "$DEST/clubmate-core" "$DEST/clubmate-session" "$DEST/plugins/terminals/terminals" "$DEST/plugins/editor-nvim/editor-nvim"
codesign --force --options runtime --sign - --deep "$APP"
