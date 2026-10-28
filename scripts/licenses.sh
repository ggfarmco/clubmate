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
  fonts_section
  sparkle_section
} >"$OUT.tmp"
mv "$OUT.tmp" "$OUT"
echo "wrote $OUT"
