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

NVIM_ENTITLEMENTS="$(cd "$(dirname "$0")" && pwd)/nvim.entitlements"
NVIM_BIN="$APP/Contents/Resources/nvim/bin/nvim"

while IFS= read -r -d '' file; do
  if ! file -b "$file" | grep -q "Mach-O"; then
    continue
  fi
  args=("${SIGN[@]}")
  if [[ "$IDENTITY" == "-" && "$file" == "$APP/Contents/Resources/nvim/"* ]]; then
    args=(codesign --force --sign -)
  fi
  if [[ "$file" == "$NVIM_BIN" ]]; then
    args+=(--entitlements "$NVIM_ENTITLEMENTS")
  fi
  "${args[@]}" "$file"
done < <(find "$APP/Contents/Resources" -type f -print0 | sort -z -r)
"${SIGN[@]}" "$SPARKLE/Versions/B/XPCServices/Installer.xpc"
"${SIGN[@]}" --preserve-metadata=entitlements "$SPARKLE/Versions/B/XPCServices/Downloader.xpc"
"${SIGN[@]}" "$SPARKLE/Versions/B/Autoupdate"
"${SIGN[@]}" "$SPARKLE/Versions/B/Updater.app"
"${SIGN[@]}" "$SPARKLE"
"${SIGN[@]}" --entitlements "$ENTITLEMENTS" "$APP"

codesign --verify --strict --deep "$APP"
echo "signed $APP"
