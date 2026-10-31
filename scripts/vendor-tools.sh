#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <Resources-dir>" >&2
  exit 2
fi

RES="$1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE="$ROOT/build/vendor"
mkdir -p "$CACHE"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

verify() {
  [[ "$(shasum -a 256 "$1" | awk '{print $1}')" == "$2" ]]
}

fetch() {
  local name="$1" version="$2" sha="$3" url="$4"
  local ext="tar.gz"
  [[ "$url" == *.tar.gz ]] || ext="gz"
  local archive="$CACHE/$name-$version.$ext"
  if [[ -f "$archive" ]] && ! verify "$archive" "$sha"; then
    echo "error: $name $version checksum mismatch (delete ${archive#"$ROOT/"})" >&2
    exit 1
  fi
  if [[ ! -f "$archive" ]]; then
    /usr/bin/curl -fsSL --retry 3 -o "$archive.part" "$url"
    if ! verify "$archive.part" "$sha"; then
      rm -f "$archive.part"
      echo "error: $name $version checksum mismatch" >&2
      exit 1
    fi
    mv "$archive.part" "$archive"
  fi
  mkdir -p "$TMP/$name"
  if [[ "$ext" == gz ]]; then
    gunzip -c "$archive" > "$TMP/$name/$name"
    chmod 755 "$TMP/$name/$name"
  else
    tar -xzf "$archive" -C "$TMP/$name"
  fi
}

while read -r name version sha url; do
  [[ -z "$name" || "$name" == \#* ]] && continue
  fetch "$name" "$version" "$sha" "$url"
done < "$ROOT/scripts/tools.lock"

RG="$(find "$TMP/ripgrep" -type f -name rg | head -n 1)"
NVIM_BIN="$(find "$TMP/neovim" -type f -path '*/bin/nvim' | head -n 1)"
if [[ -z "$RG" ]]; then
  echo "error: rg not found in the ripgrep archive" >&2
  exit 1
fi
if [[ -z "$NVIM_BIN" ]]; then
  echo "error: nvim/bin/nvim not found in the neovim archive" >&2
  exit 1
fi
NVIM="$(dirname "$(dirname "$NVIM_BIN")")"

rm -rf "$RES/tools" "$RES/nvim"
mkdir -p "$RES/tools" "$RES/nvim"
cp "$RG" "$RES/tools/rg"
mkdir -p "$CACHE/bin"
cp "$TMP/tree-sitter/tree-sitter" "$CACHE/bin/tree-sitter"
cp -R "$NVIM/bin" "$NVIM/lib" "$NVIM/share" "$RES/nvim/"
