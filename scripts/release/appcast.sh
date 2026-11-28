#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 3 || $# -gt 4 ]]; then
  echo "usage: $0 <path/to/Clubmate.dmg> <version> <build> [deltas.tsv]" >&2
  exit 2
fi

DMG="$1"
VERSION="$2"
BUILD="$3"
DELTAS="${4:-}"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/dist/appcast.xml"
source "$ROOT/scripts/release/sparkle.sh"

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
if [[ -n "$DELTAS" && ! -f "$DELTAS" ]]; then
  echo "error: $DELTAS not found" >&2
  exit 1
fi
if [[ -z "${SPARKLE_PRIVATE_KEY:-}" ]]; then
  echo "error: SPARKLE_PRIVATE_KEY is not set" >&2
  exit 1
fi
declare +x PRIVATE_KEY="$SPARKLE_PRIVATE_KEY"
unset SPARKLE_PRIVATE_KEY

html_escape() {
  sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'
}

SIGN_UPDATE="$(sparkle_bin)/sign_update"
sign() {
  local signature
  signature="$(printf '%s' "$PRIVATE_KEY" | "$SIGN_UPDATE" --ed-key-file - -p "$1")"
  if [[ -z "$signature" ]]; then
    echo "error: sign_update produced no signature for $1" >&2
    exit 1
  fi
  echo "$signature"
}

SIGNATURE="$(sign "$DMG")"
RELEASE_URL="https://github.com/ggfarmco/clubmate/releases/download/v$VERSION"
DELTA_XML=""
if [[ -n "$DELTAS" ]]; then
  DELTA_DIR="$(dirname "$DELTAS")"
  while IFS=$'\t' read -r name from size locales; do
    file="$DELTA_DIR/$name"
    if [[ ! "$name" =~ ^Clubmate-[0-9A-Za-z.-]+\.delta$ || ! "$from" =~ ^[0-9]+$ || ! "$size" =~ ^[0-9]+$ || ! "$locales" =~ ^[a-zA-Z_,-]*$ || ! -f "$file" ]]; then
      echo "error: bad line in $DELTAS: $name $from $size $locales" >&2
      exit 1
    fi
    DELTA_XML+="
        <enclosure url=\"$RELEASE_URL/$name\" sparkle:deltaFrom=\"$from\" length=\"$(stat -f %z "$file")\" type=\"application/octet-stream\" sparkle:deltaFromSparkleExecutableSize=\"$size\" ${locales:+sparkle:deltaFromSparkleLocales=\"$locales\" }sparkle:edSignature=\"$(sign "$file")\"/>"
  done <"$DELTAS"
fi
if [[ -n "$DELTA_XML" ]]; then
  DELTA_XML="
      <sparkle:deltas>$DELTA_XML
      </sparkle:deltas>"
fi

VERSION_JSON="$(cd "$ROOT" && go run ./cmd/clubmate version --json)"
SESSION_PROTOCOL="$(plutil -extract sessionProtocol raw - <<<"$VERSION_JSON")"
LENGTH="$(stat -f %z "$DMG")"
PUB_DATE="$(LC_ALL=C date -u '+%a, %d %b %Y %H:%M:%S +0000')"
URL="$RELEASE_URL/Clubmate-$VERSION.dmg"
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
      <sparkle:minimumSystemVersion>26.0</sparkle:minimumSystemVersion>
      <clubmate:sessionProtocol>$SESSION_PROTOCOL</clubmate:sessionProtocol>
      <description><![CDATA[<ul>
$NOTES
</ul>]]></description>
      <enclosure url="$URL" length="$LENGTH" type="application/octet-stream" sparkle:edSignature="$SIGNATURE"/>$DELTA_XML
    </item>
  </channel>
</rss>
XML

xmllint --noout "$OUT"
echo "wrote $OUT"
