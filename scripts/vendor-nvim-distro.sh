#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <Resources-dir>" >&2
  exit 2
fi

RES="$(cd "$1" && pwd)"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOCK="$ROOT/scripts/nvim-distro.lock"
CACHE="$ROOT/build/vendor/nvim-distro"
SRC="$CACHE/src"
TREE_SITTER="$ROOT/build/vendor/bin/tree-sitter"
NVIM="$RES/nvim/bin/nvim"
DEST="$RES/nvim-distro"

if [[ ! -x "$NVIM" ]]; then
  echo "error: ${NVIM} not found, run vendor-tools.sh first" >&2
  exit 1
fi
if [[ ! -x "$TREE_SITTER" ]]; then
  echo "error: ${TREE_SITTER#"$ROOT/"} not found, run vendor-tools.sh first" >&2
  exit 1
fi
mkdir -p "$CACHE"
rm -rf "$SRC"
mkdir -p "$SRC"
[[ -f "$ROOT/build/go.mod" ]] || echo "module clubmate.build" > "$ROOT/build/go.mod"

PARSERS_LUA="$ROOT/scripts/nvim-distro/parsers.lua"
PARSERS_KEY="$({
  awk '$1 == "parser" || ($1 == "plugin" && $2 == "nvim-treesitter")' "$LOCK"
  "$TREE_SITTER" --version
  shasum -a 256 "$TREE_SITTER" | awk '{print $1}'
  "$NVIM" --version | head -n 1
  shasum -a 256 "$PARSERS_LUA" | awk '{print $1}'
  uname -m
  /usr/bin/cc --version | head -n 1
} | shasum -a 256 | cut -c 1-16)"
PARSERS_CACHE="$CACHE/parsers-$PARSERS_KEY"

STAGE="$(mktemp -d "$RES/.nvim-distro.XXXXXX")"
TMP="$(mktemp -d)"
trap 'rm -rf "$STAGE" "$TMP"' EXIT

verify() {
  [[ "$(shasum -a 256 "$1" | awk '{print $1}')" == "$2" ]]
}

fetch() {
  local name="$1" rev="$2" sha="$3" url="$4" archive="$5"
  if [[ -f "$archive" ]] && ! verify "$archive" "$sha"; then
    echo "error: $name $rev checksum mismatch (delete ${archive#"$ROOT/"})" >&2
    exit 1
  fi
  if [[ ! -f "$archive" ]]; then
    /usr/bin/curl -fsSL --retry 3 -o "$archive.part" "$url"
    if ! verify "$archive.part" "$sha"; then
      rm -f "$archive.part"
      echo "error: $name $rev checksum mismatch" >&2
      exit 1
    fi
    mv "$archive.part" "$archive"
  fi
}

helptags() {
  local doc="$1"
  compgen -G "$doc/*.txt" >/dev/null || return 0
  env -i HOME="$TMP" PATH=/usr/bin:/bin DOC="$doc" "$NVIM" --clean --headless -i NONE \
    -c 'execute "helptags" fnameescape($DOC)' -c 'qall!'
  if [[ ! -f "$doc/tags" ]]; then
    echo "error: helptags produced no ${doc#"$STAGE/"}/tags" >&2
    exit 1
  fi
}

/usr/bin/rsync -a --exclude '*.go' --exclude 'testdata/' "$ROOT/nvim-distro/" "$STAGE/"

plugins=()
parsers=()
libs=()
while read -r kind name rev sha url; do
  [[ -z "$kind" || "$kind" == \#* ]] && continue
  case "$kind" in
    plugin)
      archive="$CACHE/$name-$rev.tar.gz"
      fetch "$name" "$rev" "$sha" "$url/archive/$rev.tar.gz" "$archive"
      mkdir -p "$STAGE/plugins/$name"
      tar -xzf "$archive" -C "$STAGE/plugins/$name" --strip-components 1
      helptags "$STAGE/plugins/$name/doc"
      plugins+=("    [\"$name\"] = { url = \"$url\", commit = \"$rev\" },")
      ;;
    parser)
      if [[ ! -d "$PARSERS_CACHE" ]]; then
        archive="$CACHE/$name-$rev.tar.gz"
        fetch "$name" "$rev" "$sha" "$url/archive/$rev.tar.gz" "$archive"
        mkdir -p "$SRC/$name"
        tar -xzf "$archive" -C "$SRC/$name" --strip-components 1
      fi
      parsers+=("    \"$name\",")
      ;;
    blink-lib)
      archive="$CACHE/$name-$rev.dylib"
      fetch "$name" "$rev" "$sha" "$url" "$archive"
      libs+=("$name $rev $archive")
      ;;
    *)
      echo "error: unknown kind '$kind' in ${LOCK#"$ROOT/"}" >&2
      exit 1
      ;;
  esac
done < "$LOCK"

for lib in ${libs[@]+"${libs[@]}"}; do
  read -r name rev archive <<<"$lib"
  plugin_rev="$(awk -v n="$name" '$1 == "plugin" && $2 == n { print $3 }' "$LOCK")"
  if [[ "$plugin_rev" != "$rev" ]]; then
    echo "error: blink-lib $name $rev does not match plugin $name ${plugin_rev:-missing}" >&2
    exit 1
  fi
  mkdir -p "$STAGE/plugins/$name/target/release"
  cp "$archive" "$STAGE/plugins/$name/target/release/libblink_cmp_fuzzy.dylib"
  chmod 644 "$STAGE/plugins/$name/target/release/libblink_cmp_fuzzy.dylib"
done

if [[ ${#parsers[@]} -gt 0 ]]; then
  env -i HOME="$TMP" PATH=/usr/bin:/bin "$NVIM" --clean --headless -i NONE -l "$PARSERS_LUA" \
    check "$STAGE/plugins/nvim-treesitter" "$LOCK"
  if [[ -d "$PARSERS_CACHE" ]]; then
    mkdir -p "$STAGE/site"
    /usr/bin/rsync -a "$PARSERS_CACHE/" "$STAGE/site/"
  else
    env -i HOME="$TMP" XDG_CACHE_HOME="$TMP/cache" PATH="$(dirname "$TREE_SITTER"):/usr/bin:/bin" TREE_SITTER_JS_RUNTIME=native \
      "$NVIM" --clean --headless -i NONE -l "$PARSERS_LUA" \
      build "$STAGE/plugins/nvim-treesitter" "$STAGE/site" "$SRC"
    for query in "$STAGE/site/queries"/*; do
      [[ -L "$query" ]] || continue
      target="$(cd "$query" && pwd -P)"
      rm "$query"
      cp -R "$target" "$query"
    done
    rm -rf "$CACHE"/parsers-*
    /usr/bin/rsync -a "$STAGE/site/" "$PARSERS_CACHE.tmp.$$/"
    [[ -d "$PARSERS_CACHE" ]] || mv "$PARSERS_CACHE.tmp.$$" "$PARSERS_CACHE"
    rm -rf "$PARSERS_CACHE.tmp.$$"
  fi
fi

mkdir -p "$STAGE/lua/clubmate"
{
  echo "return {"
  echo "  lock = \"$(shasum -a 256 "$LOCK" | awk '{print $1}')\","
  echo "  plugins = {"
  printf '%s\n' "${plugins[@]}"
  echo "  },"
  echo "  parsers = {"
  if [[ ${#parsers[@]} -gt 0 ]]; then
    printf '%s\n' "${parsers[@]}"
  fi
  echo "  },"
  echo "}"
} > "$STAGE/lua/clubmate/manifest.lua"

GOFLAGS=-mod=readonly go build -C "$STAGE/plugins/diffview.nvim/nrev" -trimpath -o "$RES/tools/nrev" .

chmod 755 "$STAGE"
rm -rf "$DEST"
mv "$STAGE" "$DEST"
