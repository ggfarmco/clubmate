#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <path/to/Clubmate.dmg> <version> <build>" >&2
  exit 2
fi

DMG="$1"
VERSION="$2"
BUILD="$3"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/dist/appcast.xml"
SPARKLE_VERSION="2.10.1"
SPARKLE_SHA256="4c2e48a3e50ebcc2c88a58a4c7264a1a99c3c31857ce754357afb9cdb323022a"
SPARKLE_ZIP_URL="https://github.com/sparkle-project/Sparkle/releases/download/$SPARKLE_VERSION/Sparkle-for-Swift-Package-Manager.zip"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]]; then
  echo "error: version must be X.Y.Z or X.Y.Z-suffix, got '$VERSION'" >&2
  exit 2
fi
if [[ ! "$BUILD" =~ ^[0-9]+$ ]]; then
  echo "error: build must be a number, got '$BUILD'" >&2
  exit 2
fi
if [[ ! -f "$DMG" ]]; then
  echo "error: $DMG not found" >&2
  exit 1
fi
if [[ -z "${SPARKLE_PRIVATE_KEY:-}" ]]; then
  echo "error: SPARKLE_PRIVATE_KEY is not set" >&2
  exit 1
fi

sign_update() {
  local spm="$ROOT/build/DerivedData/SourcePackages/artifacts/sparkle/Sparkle/bin/sign_update"
  local cache="$ROOT/build/sparkle-$SPARKLE_VERSION"
  if [[ -x "$spm" ]]; then
    echo "$spm"
    return
  fi
  if [[ ! -x "$cache/bin/sign_update" ]]; then
    local tmp
    tmp="$(mktemp -d)"
    curl -fsSL -o "$tmp/sparkle.zip" "$SPARKLE_ZIP_URL"
    echo "$SPARKLE_SHA256  $tmp/sparkle.zip" | shasum -a 256 -c - >&2
    rm -rf "$cache"
    ditto -x -k "$tmp/sparkle.zip" "$cache"
    rm -rf "$tmp"
  fi
  echo "$cache/bin/sign_update"
}

html_escape() {
  sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

SIGN_UPDATE="$(sign_update)"
SIGNATURE="$(printf '%s' "$SPARKLE_PRIVATE_KEY" | "$SIGN_UPDATE" --ed-key-file - -p "$DMG")"
if [[ -z "$SIGNATURE" ]]; then
  echo "error: sign_update produced no signature" >&2
  exit 1
fi

VERSION_JSON="$(cd "$ROOT" && go run ./cmd/clubmate version --json)"
SESSION_PROTOCOL="$(plutil -extract sessionProtocol raw - <<<"$VERSION_JSON")"
LENGTH="$(stat -f %z "$DMG")"
PUB_DATE="$(LC_ALL=C date -u '+%a, %d %b %Y %H:%M:%S +0000')"
URL="https://github.com/ggfarmco/clubmate/releases/download/v$VERSION/Clubmate-$VERSION.dmg"
NOTES="$("$ROOT/scripts/release/notes.sh" "v$VERSION" | html_escape | sed -e 's/^- \(.*\)$/<li>\1<\/li>/')"

mkdir -p "$(dirname "$OUT")"
cat >"$OUT" <<XML
<?xml version="1.0" encoding="utf-8"?>
<rss version="2.0" xmlns:sparkle="http://www.andymatuschak.org/xml-namespaces/sparkle" xmlns:clubmate="https://github.com/ggfarmco/clubmate">
  <channel>
    <title>Clubmate</title>
    <link>https://github.com/ggfarmco/clubmate</link>
    <item>
      <title>Clubmate $VERSION</title>
      <pubDate>$PUB_DATE</pubDate>
      <sparkle:version>$BUILD</sparkle:version>
      <sparkle:shortVersionString>$VERSION</sparkle:shortVersionString>
      <sparkle:minimumSystemVersion>27.0</sparkle:minimumSystemVersion>
      <clubmate:sessionProtocol>$SESSION_PROTOCOL</clubmate:sessionProtocol>
      <description><![CDATA[<ul>
$NOTES
</ul>]]></description>
      <enclosure url="$URL" length="$LENGTH" type="application/octet-stream" sparkle:edSignature="$SIGNATURE"/>
    </item>
  </channel>
</rss>
XML

xmllint --noout "$OUT"
echo "wrote $OUT"
