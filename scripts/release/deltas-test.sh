#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$ROOT/build"
TMP="$(mktemp -d "$ROOT/build/deltas-test.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
source "$ROOT/scripts/release/sparkle.sh"

fail() {
  echo "FAIL: $*" >&2
  [[ -f "$TMP/out" ]] && sed 's/^/  deltas.sh: /' "$TMP/out" >&2
  exit 1
}

plist() {
  /usr/libexec/PlistBuddy -c "Add :CFBundleIdentifier string $2" -c "Add :CFBundleExecutable string $3" \
    -c "Add :CFBundlePackageType string $4" -c "Add :CFBundleVersion string ${5:-1}" \
    -c "Add :CFBundleShortVersionString string ${6:-0.0.1}" "$1" >/dev/null
}

make_app() {
  local app="$1/Clubmate.app" fw
  fw="$app/Contents/Frameworks/Sparkle.framework"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources" "$fw/Versions/A/Resources/de.lproj"
  echo 'int main(void) { return 0; }' | clang -x c - -o "$app/Contents/MacOS/Clubmate"
  echo 'int sparkle(void) { return 0; }' | clang -x c - -dynamiclib -install_name @rpath/Sparkle.framework/Versions/A/Sparkle -o "$fw/Versions/A/Sparkle"
  echo '"a" = "b";' >"$fw/Versions/A/Resources/de.lproj/Sparkle.strings"
  plist "$fw/Versions/A/Resources/Info.plist" org.sparkle-project.Sparkle Sparkle FMWK
  ln -s A "$fw/Versions/Current"
  ln -s Versions/Current/Sparkle "$fw/Sparkle"
  ln -s Versions/Current/Resources "$fw/Resources"
  plist "$app/Contents/Info.plist" com.example.clubmate-test Clubmate APPL "$2" "$3"
  echo "$3" >"$app/Contents/Resources/data"
  [[ -z "${4:-}" ]] || xattr -w com.example.tag "$4" "$app/Contents/Resources/data"
  codesign -s - -f "$fw/Versions/A" >/dev/null 2>&1
  codesign -s - -f "$app" >/dev/null 2>&1
  hdiutil create -volname Clubmate -srcfolder "$1" -fs HFS+ -format UDZO "$1.dmg" -quiet
}

make_app "$TMP/old" 1 0.0.1
make_app "$TMP/new" 2 0.0.2

DELTAS_OUT="$TMP/dist" "$ROOT/scripts/release/deltas.sh" "$TMP/new.dmg" 0.0.2 "$TMP/old.dmg" >"$TMP/out" 2>&1 || fail "deltas.sh exited $?"
[[ "$(cat "$TMP/dist/deltas.tsv")" == "Clubmate-0.0.1-0.0.2.delta	1	$(stat -L -f %z "$TMP/old/Clubmate.app/Contents/Frameworks/Sparkle.framework/Sparkle")	de" ]] ||
  fail "deltas.tsv: $(cat "$TMP/dist/deltas.tsv")"
echo "ok: deltas.sh writes a delta with build, Sparkle size and locales of the old app"

mkdir -p "$TMP/patched"
"$(sparkle_bin)/BinaryDelta" apply "$TMP/old/Clubmate.app" "$TMP/patched/Clubmate.app" "$TMP/dist/Clubmate-0.0.1-0.0.2.delta" >/dev/null ||
  fail "BinaryDelta apply"
[[ "$(tree_digest "$TMP/patched/Clubmate.app")" == "$(tree_digest "$TMP/new/Clubmate.app")" ]] || fail "patched app differs from the new app"
codesign --verify --strict --deep "$TMP/patched/Clubmate.app" || fail "patched app signature"
echo "ok: the delta turns the old app into the new one, signature included"

make_app "$TMP/tagged" 3 0.0.3 tag
DELTAS_OUT="$TMP/dist" "$ROOT/scripts/release/deltas.sh" "$TMP/tagged.dmg" 0.0.3 "$TMP/old.dmg" >"$TMP/out" 2>&1 || fail "deltas.sh exited $?"
[[ ! -s "$TMP/dist/deltas.tsv" && -z "$(find "$TMP/dist" -name '*.delta')" ]] || fail "a delta that loses an xattr was kept"
grep -q 'the patched app is not the new app' "$TMP/out" || fail "no warning for the dropped delta"
echo "ok: deltas.sh drops a delta whose patched app differs from the new app (an xattr BinaryDelta does not carry)"

DELTAS_OUT="$TMP/dist" "$ROOT/scripts/release/deltas.sh" "$TMP/old.dmg" 0.0.1 "$TMP/new.dmg" >"$TMP/out" 2>&1 || fail "deltas.sh exited $?"
[[ ! -s "$TMP/dist/deltas.tsv" ]] || fail "a delta from a newer build: $(cat "$TMP/dist/deltas.tsv")"
echo "ok: deltas.sh skips a release that is not older"
