#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: $0 <path/to/Clubmate.app> <identity|->" >&2
  exit 2
fi

APP="$1"
IDENTITY="$2"
ENTITLEMENTS="$(cd "$(dirname "$0")" && pwd)/Clubmate.entitlements"
SPARKLE="$APP/Contents/Frameworks/Sparkle.framework"

if [[ ! -d "$APP" ]]; then
  echo "error: $APP not found" >&2
  exit 1
fi

SIGN=(codesign --force --options runtime --sign "$IDENTITY")
if [[ "$IDENTITY" != "-" ]]; then
  SIGN+=(--timestamp)
fi

while IFS= read -r -d '' bin; do
  "${SIGN[@]}" "$bin"
done < <(find "$APP/Contents/Resources/bin" -type f -perm -u=x -print0)
"${SIGN[@]}" "$SPARKLE/Versions/B/XPCServices/Installer.xpc"
"${SIGN[@]}" --preserve-metadata=entitlements "$SPARKLE/Versions/B/XPCServices/Downloader.xpc"
"${SIGN[@]}" "$SPARKLE/Versions/B/Autoupdate"
"${SIGN[@]}" "$SPARKLE/Versions/B/Updater.app"
"${SIGN[@]}" "$SPARKLE"
"${SIGN[@]}" --entitlements "$ENTITLEMENTS" "$APP"

codesign --verify --strict --deep "$APP"
echo "signed $APP"
