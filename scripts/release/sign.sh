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

sign_resources() {
  local file kind args
  {
    IFS= read -r -d '' _
    for file in "$@"; do
      IFS= read -r -d '' kind || true
      if [[ "${kind%%$'\n'*}" != *Mach-O* ]]; then
        continue
      fi
      args=(codesign --force --options runtime --sign "$IDENTITY")
      if [[ "$IDENTITY" != "-" ]]; then
        args+=(--timestamp)
      fi
      if [[ "$IDENTITY" == "-" && "$file" == "$APP/Contents/Resources/nvim/"* ]]; then
        args=(codesign --force --sign -)
      fi
      if [[ "$file" == "$NVIM_BIN" && "$IDENTITY" != "-" ]]; then
        args+=(--entitlements "$NVIM_ENTITLEMENTS")
      fi
      "${args[@]}" "$file" || exit 255
    done
  } < <(file -0 "$@")
}
export -f sign_resources
export APP IDENTITY NVIM_BIN NVIM_ENTITLEMENTS

find "$APP/Contents/Resources" -type f -print0 |
  xargs -0 -n 100 -P "$(sysctl -n hw.ncpu)" bash -c 'sign_resources "$@"' _
"${SIGN[@]}" "$SPARKLE/Versions/B/XPCServices/Installer.xpc"
"${SIGN[@]}" --preserve-metadata=entitlements "$SPARKLE/Versions/B/XPCServices/Downloader.xpc"
"${SIGN[@]}" "$SPARKLE/Versions/B/Autoupdate"
"${SIGN[@]}" "$SPARKLE/Versions/B/Updater.app"
"${SIGN[@]}" "$SPARKLE"
"${SIGN[@]}" --entitlements "$ENTITLEMENTS" "$APP"

codesign --verify --strict --deep "$APP"
echo "signed $APP"
