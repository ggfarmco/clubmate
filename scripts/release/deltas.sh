#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <path/to/Clubmate.dmg> <version> [old.dmg...]" >&2
  echo "without old.dmg, it uses the dmg of the last \$DELTA_COUNT (3) GitHub releases" >&2
  exit 2
fi

DMG="$1"
VERSION="$2"
shift 2
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${DELTAS_OUT:-$ROOT/dist}"
LIST="$OUT/deltas.tsv"
COUNT="${DELTA_COUNT:-3}"
LOCALES=(de he ar el ja fa uk)
source "$ROOT/scripts/release/sparkle.sh"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "error: version must be X.Y.Z or X.Y.Z-suffix, got '$VERSION'" >&2
  exit 2
fi
if [[ ! -f "$DMG" ]]; then
  echo "error: $DMG not found" >&2
  exit 1
fi

TMP="$(mktemp -d)"
MOUNTS=()
cleanup() {
  local m
  for m in ${MOUNTS[@]+"${MOUNTS[@]}"}; do
    hdiutil detach "$m" -quiet -force >/dev/null 2>&1 || true
  done
  rm -rf "$TMP"
}
trap cleanup EXIT

skip() {
  echo "::warning::$*"
}

mount_dmg() {
  mkdir -p "$2"
  hdiutil attach "$1" -readonly -nobrowse -noautoopen -mountpoint "$2" -quiet || return 1
  MOUNTS+=("$2")
}

mkdir -p "$OUT"
rm -f "$OUT"/Clubmate-*.delta
: >"$LIST"

if [[ $# -eq 0 ]]; then
  if ! TAGS="$(gh release list --limit 20 --exclude-drafts --json tagName -q '.[].tagName')"; then
    skip "cannot list releases: no delta updates"
    exit 0
  fi
  n=0
  for tag in $TAGS; do
    [[ "$tag" != "v$VERSION" ]] || continue
    ((n < COUNT)) || break
    mkdir -p "$TMP/download/$tag"
    if ! gh release download "$tag" --pattern 'Clubmate-*.dmg' --dir "$TMP/download/$tag"; then
      skip "$tag: cannot download its dmg"
      continue
    fi
    n=$((n + 1))
    for old in "$TMP/download/$tag"/Clubmate-*.dmg; do
      set -- "$@" "$old"
    done
  done
fi
if [[ $# -eq 0 ]]; then
  echo "no previous release: no delta updates"
  exit 0
fi

mount_dmg "$DMG" "$TMP/new"
NEW_APP="$TMP/new/Clubmate.app"
NEW_BUILD="$(plutil -extract CFBundleVersion raw "$NEW_APP/Contents/Info.plist")"
NEW_DIGEST="$(tree_digest "$NEW_APP")"
BIN="$(sparkle_bin)"

i=0
for old in "$@"; do
  i=$((i + 1))
  mnt="$TMP/old$i"
  if ! mount_dmg "$old" "$mnt"; then
    skip "$old: cannot mount"
    continue
  fi
  app="$mnt/Clubmate.app"
  old_build="$(plutil -extract CFBundleVersion raw "$app/Contents/Info.plist" 2>/dev/null || true)"
  old_version="$(plutil -extract CFBundleShortVersionString raw "$app/Contents/Info.plist" 2>/dev/null || true)"
  if [[ ! "$old_build" =~ ^[0-9]+$ || ! "$old_version" =~ ^[0-9A-Za-z.-]+$ ]]; then
    skip "$old: unknown build or version"
    continue
  fi
  if ((old_build >= NEW_BUILD)); then
    skip "$old: build $old_build is not older than $NEW_BUILD"
    continue
  fi
  name="Clubmate-$old_version-$VERSION.delta"
  if ! "$BIN/BinaryDelta" create "$app" "$NEW_APP" "$OUT/$name"; then
    rm -f "$OUT/$name"
    skip "$name: BinaryDelta create failed"
    continue
  fi
  mkdir -p "$TMP/check$i"
  check="$TMP/check$i/Clubmate.app"
  if ! "$BIN/BinaryDelta" apply "$app" "$check" "$OUT/$name" || [[ "$(tree_digest "$check")" != "$NEW_DIGEST" ]] || ! codesign --verify --strict --deep "$check"; then
    rm -f "$OUT/$name"
    skip "$name: the patched app is not the new app"
    continue
  fi
  rm -rf "$TMP/check$i"
  sparkle="$app/Contents/Frameworks/Sparkle.framework"
  size="$(stat -L -f %z "$sparkle/Sparkle")"
  locales=""
  for l in "${LOCALES[@]}"; do
    if [[ -d "$sparkle/Resources/$l.lproj" ]]; then
      locales="${locales:+$locales,}$l"
    fi
  done
  printf '%s\t%s\t%s\t%s\n' "$name" "$old_build" "$size" "$locales" >>"$LIST"
  echo "wrote $OUT/$name ($old_build -> $NEW_BUILD, $(stat -f %z "$OUT/$name") bytes)"
done
echo "wrote $LIST"
