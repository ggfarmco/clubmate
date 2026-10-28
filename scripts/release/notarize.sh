#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <path/to/Clubmate.dmg>" >&2
  exit 2
fi

DMG="$1"

if [[ -z "${ASC_KEY_ID:-}" ]]; then
  echo "ASC_KEY_ID is not set: skipping notarization of $DMG"
  exit 0
fi
if [[ -z "${ASC_ISSUER_ID:-}" || -z "${ASC_KEY_P8_BASE64:-}" ]]; then
  echo "error: ASC_ISSUER_ID and ASC_KEY_P8_BASE64 must be set with ASC_KEY_ID" >&2
  exit 1
fi
if [[ ! -f "$DMG" ]]; then
  echo "error: $DMG not found" >&2
  exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
printf '%s' "$ASC_KEY_P8_BASE64" | base64 --decode >"$TMP/key.p8"
AUTH=(--key "$TMP/key.p8" --key-id "$ASC_KEY_ID" --issuer "$ASC_ISSUER_ID")

xcrun notarytool submit "$DMG" "${AUTH[@]}" --wait --output-format json >"$TMP/submit.json" || true
STATUS="$(plutil -extract status raw "$TMP/submit.json" 2>/dev/null || echo unknown)"
if [[ "$STATUS" != "Accepted" ]]; then
  echo "error: notarization status $STATUS" >&2
  if ID="$(plutil -extract id raw "$TMP/submit.json" 2>/dev/null)"; then
    xcrun notarytool log "$ID" "${AUTH[@]}" >&2 || true
  fi
  exit 1
fi

xcrun stapler staple "$DMG"
xcrun stapler validate "$DMG"
echo "notarized and stapled $DMG"
