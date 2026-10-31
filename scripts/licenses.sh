#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="THIRD_PARTY_LICENSES.md"
GHOSTTY_SRC="build/ghostty-src"
GHOSTTY_REV="$(sed -n 's/^GHOSTTY_REV="\(.*\)"$/\1/p' scripts/build-ghostty.sh)"
SPARKLE_SRC="build/DerivedData/SourcePackages/checkouts/Sparkle"
SPARKLE_PIN="$(sed -n '/Sparkle:/,/exactVersion/s/^ *exactVersion: "\(.*\)"$/\1/p' client/project.yml)"
ZIG_BUILD_ONLY=" aro translate_c pixels "
FRAGMENTS="scripts/licenses"
SIMDUTF_VERSION="9.0.0"

fail() {
  echo "error: $*" >&2
  exit 1
}

[[ "$(git -C "$GHOSTTY_SRC" rev-parse HEAD 2>/dev/null || true)" == "$GHOSTTY_REV" && -d "$GHOSTTY_SRC/zig-pkg" ]] ||
  fail "$GHOSTTY_SRC is not a built ghostty $GHOSTTY_REV checkout (run make ghostty)"
VENDORED_SIMDUTF="$(sed -n 's/^#define SIMDUTF_VERSION "\(.*\)"$/\1/p' "$GHOSTTY_SRC/pkg/simdutf/vendor/simdutf.h")"
[[ "$VENDORED_SIMDUTF" == "$SIMDUTF_VERSION" ]] ||
  fail "ghostty vendors simdutf $VENDORED_SIMDUTF, $FRAGMENTS/simdutf has $SIMDUTF_VERSION licences"
[[ -f "$SPARKLE_SRC/LICENSE" ]] || fail "$SPARKLE_SRC missing (run make app to resolve Swift packages)"
SPARKLE_VERSION="$(sed -n 's/^let version = "\(.*\)"$/\1/p' "$SPARKLE_SRC/Package.swift")"
[[ "$SPARKLE_VERSION" == "$SPARKLE_PIN" ]] || fail "Sparkle checkout is $SPARKLE_VERSION, project.yml pins $SPARKLE_PIN"

licence_files() {
  find "$1" -maxdepth 1 -type f \( -iname 'licen[cs]e' -o -iname 'licen[cs]e.*' -o -iname 'licen[cs]e-*' -o -iname 'copying*' -o -iname 'ofl.txt' \) ! -iname '*.cfg' | LC_ALL=C sort
}

block() {
  printf '#### %s\n\n````text\n' "$1"
  cat "$2"
  printf '\n````\n\n'
}

go_section() {
  local goroot licence
  goroot="$(go env GOROOT)"
  licence="$goroot/LICENSE"
  [[ -f "$licence" ]] || licence="$goroot/../LICENSE"
  [[ -f "$licence" ]] || fail "Go licence not found under $goroot"
  printf '## Go\n\nThe `clubmate`, `clubmate-core` and `clubmate-session` binaries and the bundled plugins are built with Go and link these modules.\n\n'
  printf '### Go standard library %s\n\n' "$(go env GOVERSION)"
  block LICENSE "$licence"
  go mod download
  local modules
  modules="$(GOOS=darwin GOARCH=arm64 go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' ./cmd/... ./plugins/... | LC_ALL=C sort -u)"
  local path version dir files f
  while IFS='|' read -r path version dir; do
    printf '### %s %s\n\n' "$path" "$version"
    files="$(licence_files "$dir")"
    [[ -n "$files" ]] || fail "no licence file in $dir"
    while IFS= read -r f; do block "${f#"$dir"/}" "$f"; done <<<"$files"
  done < <(go list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' $modules)
}

ghostty_section() {
  printf '## GhosttyKit (libghostty)\n\n'
  printf 'Built from ghostty %s together with the dependencies its macOS build fetches. Build-time-only tools (%s) are omitted.\n\n' "$GHOSTTY_REV" "$(echo $ZIG_BUILD_ONLY | sed 's/ /, /g')"
  printf '### ghostty\n\nSource: https://github.com/ghostty-org/ghostty\n\n'
  block LICENSE "$GHOSTTY_SRC/LICENSE"
  local key name url dir files f spdx
  while IFS=$'\t' read -r name key url; do
    dir="$GHOSTTY_SRC/zig-pkg/$key"
    [[ -d "$dir" && "$ZIG_BUILD_ONLY" != *" $name "* ]] || continue
    printf '### %s\n\nSource: %s\n\n' "$name" "$url"
    if [[ "$name" == gettext ]]; then
      files="$dir/gettext-runtime/intl/COPYING.LIB"
    else
      files="$(licence_files "$dir")"
    fi
    if [[ -z "$files" ]]; then
      spdx="$(grep -rhoE 'SPDX-License-Identifier: [A-Za-z0-9.+-]+' "$dir" 2>/dev/null | sed 's/.*: //' | LC_ALL=C sort -u | paste -sd, - || true)"
      if [[ -n "$spdx" ]]; then
        printf 'No licence file in the source archive; its sources declare SPDX %s.\n\n' "$spdx"
      else
        printf 'No licence file in the source archive; see the upstream project.\n\n'
      fi
      continue
    fi
    while IFS= read -r f; do block "${f#"$dir"/}" "$f"; done <<<"$files"
  done < <(jq -r 'to_entries[] | [.value.name, .key, .value.url] | @tsv' "$GHOSTTY_SRC/build.zig.zon.json" | LC_ALL=C sort)
}

vendored_section() {
  printf '### simdutf %s\n\nSource: https://github.com/simdutf/simdutf (vendored in ghostty `pkg/simdutf`), dual-licensed Apache-2.0 OR MIT.\n\n' "$SIMDUTF_VERSION"
  block LICENSE-APACHE "$FRAGMENTS/simdutf/LICENSE-APACHE"
  block LICENSE-MIT "$FRAGMENTS/simdutf/LICENSE-MIT"
  printf '### stb_image, stb_image_resize\n\nSource: https://github.com/nothings/stb (vendored in ghostty `src/stb`).\n\n'
  sed -n '/^This software is available under 2 licenses/,$p' "$GHOSTTY_SRC/src/stb/stb_image.h" | sed -e '/^\*\/$/,$d' | sed '$d' >"$TMP/stb-license"
  [[ -s "$TMP/stb-license" ]] || fail "stb licence text not found in src/stb/stb_image.h"
  block LICENSE "$TMP/stb-license"
}

bundled_tools_section() {
  local name version pinned
  for name in ripgrep neovim; do
    version="$(cat "$FRAGMENTS/$name/VERSION")"
    pinned="$(awk -v n="$name" '$1 == n { print $2 }' scripts/tools.lock)"
    [[ "$version" == "$pinned" ]] || fail "$name licences are for $version, tools.lock pins $pinned"
  done
  printf '## Tools bundled in Clubmate.app\n\n'
  version="$(cat "$FRAGMENTS/ripgrep/VERSION")"
  printf '### ripgrep %s\n\nSource: https://github.com/BurntSushi/ripgrep, dual-licensed Unlicense OR MIT.\n\n' "$version"
  block UNLICENSE "$FRAGMENTS/ripgrep/UNLICENSE"
  block LICENSE-MIT "$FRAGMENTS/ripgrep/LICENSE-MIT"
  cat "$FRAGMENTS/ripgrep/crates.md"
  printf '\n'
  version="$(cat "$FRAGMENTS/neovim/VERSION")"
  printf '### Neovim %s\n\nSource: https://github.com/neovim/neovim\n\n' "$version"
  block LICENSE.txt "$FRAGMENTS/neovim/LICENSE.txt"
  cat "$FRAGMENTS/neovim/deps.md"
  printf '\n'
}

nvim_distro_fragment() {
  local name="$1" rev="$2" dir="$FRAGMENTS/nvim-distro/$1" files f
  [[ -f "$dir/COMMIT" ]] || fail "$dir/COMMIT missing"
  [[ "$(cat "$dir/COMMIT")" == "$rev" ]] || fail "$name licences are for $(cat "$dir/COMMIT"), nvim-distro.lock pins $rev"
  files="$(licence_files "$dir")"
  [[ -n "$files" ]] || fail "no licence file in $dir"
  while IFS= read -r f; do block "${f#"$dir"/}" "$f"; done <<<"$files"
}

nvim_distro_section() {
  local lock="scripts/nvim-distro.lock" kind name rev sha url dir diffview="" locked=" "
  printf '## Neovim distribution bundled in Clubmate.app\n\n'
  printf 'Clubmate.app ships the Neovim plugins below as their complete, unmodified source trees in `Contents/Resources/nvim-distro/plugins/`, tree-sitter parsers compiled from the grammars below into `nvim-distro/site/parser/`, and the prebuilt fuzzy-matching library from the blink.cmp release that matches the bundled blink.cmp source.\n\n'
  printf 'diffview.nvim and nvim-dap are licensed under the GNU General Public License v3.0. Each is a separate program aggregated with Clubmate on the same medium; nothing in Clubmate links them, and their complete corresponding source ships unmodified next to them. `Contents/Resources/tools/nrev` is compiled, without modification, from the `nrev/` directory of diffview.nvim, whose complete source ships at `nvim-distro/plugins/diffview.nvim/nrev/`; it is licensed under GPL-3.0 as well.\n\n'
  while read -r kind name rev sha url; do
    [[ "$kind" == plugin ]] || continue
    locked+="$name "
    printf '### %s %s\n\nSource: %s\n\n' "$name" "$rev" "$url"
    nvim_distro_fragment "$name" "$rev"
    if [[ "$name" == blink.cmp ]]; then
      blink_crates_section "$name" "$rev" "$sha"
    fi
    if [[ "$name" == diffview.nvim ]]; then
      diffview="$name-$rev.tar.gz $sha"
    fi
  done <"$lock"
  while read -r kind name rev sha url; do
    [[ "$kind" == parser ]] || continue
    locked+="$name "
    printf '### %s tree-sitter parser %s\n\nSource: %s\n\n' "$name" "$rev" "$url"
    nvim_distro_fragment "$name" "$rev"
  done <"$lock"
  for dir in "$FRAGMENTS"/nvim-distro/*; do
    [[ "$locked" == *" $(basename "$dir") "* ]] || fail "$dir is not in $lock"
  done
  [[ -n "$diffview" ]] || fail "diffview.nvim is not in $lock"
  nrev_section $diffview
}

crate_licence_files() {
  find "$1" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'unlicense*' -o -iname 'copyright*' \) ! -iname '*.rs' ! -iname '*.toml' | LC_ALL=C sort
}

blink_crates_section() {
  local name="$1" rev="$2" sha="$3" archive="build/vendor/nvim-distro/$1-$2.tar.gz" lib_rev src="$TMP/blink" cache="build/vendor/crates"
  local crate version checksum file dir licence files f hash
  lib_rev="$(awk -v n="$name" '$1 == "blink-lib" && $2 == n { print $3 }' scripts/nvim-distro.lock)"
  [[ "$lib_rev" == "$rev" ]] || fail "blink-lib $name ${lib_rev:-missing} does not match plugin $name $rev"
  [[ -f "$archive" ]] || fail "$archive missing (run make nvim-distro)"
  [[ "$(shasum -a 256 "$archive" | awk '{print $1}')" == "$sha" ]] || fail "$archive checksum mismatch"
  mkdir -p "$src" "$cache" "$TMP/crates" "$TMP/texts"
  tar -xzf "$archive" -C "$src" --strip-components 1
  [[ -f "$src/Cargo.lock" ]] || fail "$name $rev has no Cargo.lock"
  awk '
    /^\[\[package\]\]/ { if (n != "" && s ~ /^"registry\+/) print n, v, c; n = v = s = c = "" }
    /^name = / { n = $3 } /^version = / { v = $3 } /^source = / { s = $3 } /^checksum = / { c = $3 }
    END { if (n != "" && s ~ /^"registry\+/) print n, v, c }
  ' "$src/Cargo.lock" | tr -d '"' | LC_ALL=C sort >"$TMP/crates.list"
  [[ -s "$TMP/crates.list" ]] || fail "no registry crates in $name $rev Cargo.lock"
  : >"$TMP/crates.md"
  : >"$TMP/texts.order"
  while read -r crate version checksum; do
    file="$cache/$crate-$version.crate"
    if [[ ! -f "$file" ]]; then
      /usr/bin/curl -fsSL --retry 3 -o "$file.part" "https://static.crates.io/crates/$crate/$crate-$version.crate"
      mv "$file.part" "$file"
    fi
    [[ "$(shasum -a 256 "$file" | awk '{print $1}')" == "$checksum" ]] || fail "$file checksum does not match $name Cargo.lock"
    dir="$TMP/crates/$crate-$version"
    mkdir -p "$dir"
    tar -xzf "$file" -C "$dir" --strip-components 1
    licence="$(awk '/^\[/ { p = ($0 == "[package]") } p && /^license = / { sub(/^license = /, ""); gsub(/"/, ""); print; exit }' "$dir/Cargo.toml")"
    [[ -n "$licence" ]] || licence="see licence file"
    files="$(crate_licence_files "$dir")"
    [[ -n "$files" ]] || licence+=" (no licence file in the published crate)"
    printf -- '- %s %s: %s\n' "$crate" "$version" "$licence" >>"$TMP/crates.md"
    while IFS= read -r f; do
      [[ -n "$f" ]] || continue
      hash="$(shasum -a 256 "$f" | awk '{print $1}')"
      if [[ ! -f "$TMP/texts/$hash" ]]; then
        cp "$f" "$TMP/texts/$hash"
        printf '%s %s\n' "$hash" "$(basename "$f")" >>"$TMP/texts.order"
      fi
      printf '%s %s\n' "$crate" "$version" >>"$TMP/texts/$hash.users"
    done <<<"$files"
  done <"$TMP/crates.list"
  printf '#### Rust crates in libblink_cmp_fuzzy.dylib (%s %s)\n\n' "$name" "$rev"
  printf 'The prebuilt fuzzy-matching library is built from these crates (every crates.io package in %s %s `Cargo.lock`; the blink-cmp-fuzzy crate itself is covered by the blink.cmp licence above). Licence texts that are identical are shown once.\n\n' "$name" "$rev"
  printf '##### Crates\n\n'
  cat "$TMP/crates.md"
  printf '\n##### Licence texts\n\n'
  while read -r hash f; do
    printf '###### %s\n\nUsed by: %s\n\n````text\n' "$f" "$(paste -sd, - <"$TMP/texts/$hash.users" | sed 's/,/, /g')"
    cat "$TMP/texts/$hash"
    printf '\n````\n\n'
  done <"$TMP/texts.order"
}

nrev_section() {
  local archive="build/vendor/nvim-distro/$1" sha="$2" src="$TMP/diffview" modules path version dir files f
  [[ -f "$archive" ]] || fail "$archive missing (run make nvim-distro)"
  [[ "$(shasum -a 256 "$archive" | awk '{print $1}')" == "$sha" ]] || fail "$archive checksum mismatch"
  mkdir -p "$src"
  tar -xzf "$archive" -C "$src" --strip-components 1
  printf '## nrev\n\n`tools/nrev` is built with Go (standard library licence above) and links these modules.\n\n'
  go -C "$src/nrev" mod download
  modules="$(GOOS=darwin GOARCH=arm64 go -C "$src/nrev" list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}' . | LC_ALL=C sort -u)"
  while IFS='|' read -r path version dir; do
    printf '### %s %s\n\n' "$path" "$version"
    files="$(licence_files "$dir")"
    [[ -n "$files" ]] || fail "no licence file in $dir"
    while IFS= read -r f; do block "${f#"$dir"/}" "$f"; done <<<"$files"
  done < <(go -C "$src/nrev" list -m -f '{{.Path}}|{{.Version}}|{{.Dir}}' $modules)
}

fonts_section() {
  local res="$GHOSTTY_SRC/src/font/res"
  printf '## Fonts bundled in libghostty\n\nghostty embeds fonts from `src/font/res`; JetBrains Mono and Symbols Nerd Font are listed with the GhosttyKit dependencies above.\n\n'
  local f
  for f in README.md OFL.txt MIT.txt BSD-2-Clause.txt; do block "$f" "$res/$f"; done
}

sparkle_section() {
  printf '## Sparkle %s\n\nSource: https://github.com/sparkle-project/Sparkle\n\n' "$SPARKLE_VERSION"
  block LICENSE "$SPARKLE_SRC/LICENSE"
}

adapted_section() {
  printf '## Adapted source code\n\n'
  cat "$FRAGMENTS/adapted.md"
  printf '\n'
}

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

{
  printf '# Third-party licences\n\n'
  printf 'clubmate is licensed under the Apache License, Version 2.0 (see `LICENSE` and `NOTICE`). Clubmate.app bundles the third-party software below. Generated by `scripts/licenses.sh`.\n\n'
  adapted_section
  go_section
  ghostty_section
  vendored_section
  bundled_tools_section
  nvim_distro_section
  fonts_section
  sparkle_section
} >"$OUT.tmp"
mv "$OUT.tmp" "$OUT"
echo "wrote $OUT"
