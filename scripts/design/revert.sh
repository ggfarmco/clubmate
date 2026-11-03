#!/usr/bin/env bash
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

[[ $# -eq 0 ]] || { echo "usage: $0   revert the fixes this run landed (<run dir>/merged), newest first" >&2; exit 2; }
MAIN="$(main_checkout)"
cd "$MAIN" || exit 1
DIR="$(run_dir)"
LANDED="$(check_root)/landed"

if [[ ! -s "$DIR/merged" ]]; then
  echo "nothing landed in this run: nothing to revert"
  exit 0
fi
git symbolic-ref -q HEAD >/dev/null || fail "HEAD is detached in $MAIN"
git diff --cached --quiet || fail "the index of $MAIN is not empty: another session is committing"

SHAS="$(awk '{print $1}' "$DIR/merged" | tail -r)"
for sha in $SHAS; do
  git merge-base --is-ancestor "$sha" HEAD 2>/dev/null || fail "$sha is not on $(git symbolic-ref --short HEAD)"
done
for sha in $SHAS; do
  if ! git revert --no-edit "$sha" >/dev/null 2>&1; then
    git revert --abort >/dev/null 2>&1
    fail "reverting $sha conflicts; reverted so far: $(cat "$DIR/reverted" 2>/dev/null | tr '\n' ' ')"
  fi
  printf '%s %s\n' "$sha" "$(git rev-parse HEAD)" >> "$DIR/reverted"
  if [[ -f "$LANDED" ]]; then
    { grep -v "^$sha " "$LANDED" || true; } > "$LANDED.tmp"
    mv "$LANDED.tmp" "$LANDED"
  fi
  echo "reverted $(git log -1 --format='%h %s' "$sha") as $(git rev-parse --short HEAD)"
done
