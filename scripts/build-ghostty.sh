#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

GHOSTTY_REPO="https://github.com/ghostty-org/ghostty"
GHOSTTY_REV="683d8db643b95cf229bfb5fe9fab9ae677920343"
ZIG_FORMULA="zig@0.16"
SRC_DIR="build/ghostty-src"
XCFRAMEWORK_DIR="client/GhosttyKit.xcframework"
RESOURCES_DIR="client/Clubmate/Resources"
STAMP_FILE=".ghostty-build-stamp"

if [[ -f "$STAMP_FILE" && "$(cat "$STAMP_FILE")" == "$GHOSTTY_REV" && -d "$XCFRAMEWORK_DIR" && -d "$RESOURCES_DIR/ghostty" && -d "$RESOURCES_DIR/terminfo" ]]; then
  echo "GhosttyKit $GHOSTTY_REV already built"
  exit 0
fi

ZIG="$(brew --prefix "$ZIG_FORMULA" 2>/dev/null || true)/bin/zig"
if [[ ! -x "$ZIG" ]]; then
  echo "error: $ZIG_FORMULA not installed (brew install $ZIG_FORMULA)" >&2
  exit 1
fi

if ! xcrun metal --version >/dev/null 2>&1; then
  echo "error: Metal Toolchain missing (xcodebuild -downloadComponent MetalToolchain)" >&2
  exit 1
fi

rm -f "$STAMP_FILE"

if [[ "$(git -C "$SRC_DIR" rev-parse HEAD 2>/dev/null || true)" != "$GHOSTTY_REV" ]]; then
  echo "fetching ghostty $GHOSTTY_REV..."
  rm -rf "$SRC_DIR"
  mkdir -p "$SRC_DIR"
  git init -q "$SRC_DIR"
  git -C "$SRC_DIR" remote add origin "$GHOSTTY_REPO"
  git -C "$SRC_DIR" fetch -q --depth 1 origin "$GHOSTTY_REV"
  git -C "$SRC_DIR" -c advice.detachedHead=false checkout -q FETCH_HEAD
fi

echo "building GhosttyKit.xcframework with zig (a few minutes)..."
(
  cd "$SRC_DIR"
  if [[ -n "${CLUBMATE_CA_BUNDLE:-}" ]]; then
    export SSL_CERT_FILE="$CLUBMATE_CA_BUNDLE"
  fi
  "$ZIG" build -Doptimize=ReleaseFast -Demit-xcframework=true \
    -Dxcframework-target=native -Demit-macos-app=false
)

echo "staging GhosttyKit.xcframework and resources..."
rm -rf "$XCFRAMEWORK_DIR" "$RESOURCES_DIR/ghostty" "$RESOURCES_DIR/terminfo"
cp -R "$SRC_DIR/macos/GhosttyKit.xcframework" "$XCFRAMEWORK_DIR"
mkdir -p "$RESOURCES_DIR/ghostty"
cp -R "$SRC_DIR/zig-out/share/ghostty/shell-integration" "$RESOURCES_DIR/ghostty/"
cp -R "$SRC_DIR/zig-out/share/ghostty/themes" "$RESOURCES_DIR/ghostty/"
cp -R "$SRC_DIR/zig-out/share/terminfo" "$RESOURCES_DIR/terminfo"
printf '%s\n' "$GHOSTTY_REV" > "$STAMP_FILE"
echo "GhosttyKit $GHOSTTY_REV staged"
