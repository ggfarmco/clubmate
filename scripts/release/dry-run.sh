#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

VERSION="0.0.0-test"
MACOS_MIN="27.0"
BUILD="$(git rev-list --count HEAD)"
APP="dist/Clubmate.app"
DMG="dist/Clubmate-$VERSION.dmg"
APPCAST="dist/appcast.xml"
URL="https://github.com/ggfarmco/clubmate/releases/download/v$VERSION/Clubmate-$VERSION.dmg"
TMP="$(mktemp -d)"
MNT="$TMP/mnt"
trap 'hdiutil detach "$MNT" -quiet >/dev/null 2>&1 || true; rm -rf "$TMP"' EXIT

expect() {
  local what="$1" got="$2" want="$3"
  if [[ "$got" != "$want" ]]; then
    echo "FAIL: $what: got '$got', want '$want'" >&2
    exit 1
  fi
  echo "ok: $what"
}

check() {
  local what="$1"
  shift
  if ! "$@" >/dev/null 2>&1; then
    echo "FAIL: $what" >&2
    exit 1
  fi
  echo "ok: $what"
}

xpath() {
  xmllint --xpath "$1" "$APPCAST"
}

mkdir -p "$TMP/keygen"
cat >"$TMP/keygen/main.go" <<'GO'
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[1], []byte(base64.StdEncoding.EncodeToString(priv.Seed())), 0o600); err != nil {
		panic(err)
	}
	fmt.Print(base64.StdEncoding.EncodeToString(pub))
}
GO
PUBLIC_KEY="$(cd "$TMP/keygen" && GO111MODULE=off go run main.go "$TMP/sparkle.key")"

SPARKLE_PUBLIC_KEY="$PUBLIC_KEY" scripts/release/build.sh "$VERSION" 2>&1 | tee "$TMP/build.log"
scripts/release/sign.sh "$APP" -
scripts/release/dmg.sh "$APP" "$VERSION" -
env -u ASC_KEY_ID scripts/release/notarize.sh "$DMG"
SPARKLE_PRIVATE_KEY="$(<"$TMP/sparkle.key")" scripts/release/appcast.sh "$DMG" "$VERSION" "$BUILD"

PLIST="$APP/Contents/Info.plist"
check "codesign --verify --strict $APP" codesign --verify --strict --deep "$APP"
ENTITLEMENTS="$(codesign -d --entitlements - --xml "$APP" 2>/dev/null)"
check "no get-task-allow entitlement" test "${ENTITLEMENTS/get-task-allow/}" == "$ENTITLEMENTS"
check "no disable-library-validation entitlement" test "${ENTITLEMENTS/disable-library-validation/}" == "$ENTITLEMENTS"
expect "CFBundleShortVersionString" "$(plutil -extract CFBundleShortVersionString raw "$PLIST")" "$VERSION"
expect "CFBundleVersion" "$(plutil -extract CFBundleVersion raw "$PLIST")" "$BUILD"
expect "LSMinimumSystemVersion" "$(plutil -extract LSMinimumSystemVersion raw "$PLIST")" "$MACOS_MIN"
expect "SUPublicEDKey" "$(plutil -extract SUPublicEDKey raw "$PLIST")" "$PUBLIC_KEY"

check "no binary linked against objects newer than its deployment target" test -z "$(grep 'built for newer' "$TMP/build.log")"

check "hdiutil verify $DMG" hdiutil verify "$DMG"
check "codesign --verify $DMG" codesign --verify --strict "$DMG"
mkdir -p "$MNT"
hdiutil attach "$DMG" -readonly -nobrowse -noautoopen -mountpoint "$MNT" -quiet
for f in LICENSE NOTICE THIRD_PARTY_LICENSES.md; do
  check "dmg contains $f" test -f "$MNT/$f"
done
check "dmg links /Applications" test "$(readlink "$MNT/Applications")" == /Applications
check "dmg app signature" codesign --verify --strict --deep "$MNT/Clubmate.app"
hdiutil detach "$MNT" -quiet

expect "enclosure url" "$(xpath "string(//item/enclosure/@url)")" "$URL"
expect "sparkle:shortVersionString" "$(xpath "string(//item/*[local-name()='shortVersionString'])")" "$VERSION"
expect "sparkle:version" "$(xpath "string(//item/*[local-name()='version'])")" "$BUILD"
expect "sparkle:minimumSystemVersion" "$(xpath "string(//item/*[local-name()='minimumSystemVersion'])")" "$MACOS_MIN"
expect "clubmate:sessionProtocol" "$(xpath "string(//item/*[local-name()='sessionProtocol' and namespace-uri()='https://github.com/ggfarmco/clubmate'])")" "$(go run ./cmd/clubmate version --json | plutil -extract sessionProtocol raw -)"
check "clubmate: prefix" grep -q '<clubmate:sessionProtocol>' "$APPCAST"
check "non-empty edSignature" test -n "$(xpath "string(//item/enclosure/@*[local-name()='edSignature'])")"
check "dist/ is gitignored" git check-ignore -q dist/Clubmate.app

echo "dry run ok"
