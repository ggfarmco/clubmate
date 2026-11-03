#!/usr/bin/env bash
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

[[ $# -eq 0 || "$1" == "--dry-run" ]] || { echo "usage: $0 [--dry-run]   stop the design check's test instances and simulators, remove its capture trees and the fix worktrees that landed" >&2; exit 2; }
DRY=0
[[ "${1:-}" == "--dry-run" ]] && DRY=1
MAIN="$(main_checkout)"
cd "$MAIN" || exit 1
ROOT="$(check_root)"
LANDED="$ROOT/landed"
strip_clubmate_env

act() {
  if [[ "$DRY" == 1 ]]; then
    echo "would $*"
  else
    "$@"
  fi
}

held() {
  lsof -d cwd -Fn 2>/dev/null | grep -q "^n$1\(/\|$\)"
}

clean_tree() {
  [[ -z "$(git -C "$1" status --porcelain 2>/dev/null)" ]]
}

worktree_of() {
  git worktree list --porcelain | awk -v b="refs/heads/$1" '/^worktree /{p=substr($0, 10)} $0 == "branch " b {print p}'
}

drop_landed() {
  [[ "$DRY" == 1 || ! -f "$LANDED" ]] && return 0
  { grep -v " $1\$" "$LANDED" || true; } > "$LANDED.tmp"
  mv "$LANDED.tmp" "$LANDED"
}

remove_fix() {
  local branch="$1" why="$2" wt
  wt="$(worktree_of "$branch")"
  if [[ -n "$wt" ]]; then
    if ! clean_tree "$wt"; then
      echo "kept $wt: uncommitted changes"
      return 1
    fi
    if held "$wt"; then
      echo "kept $wt: a process still runs in it"
      return 1
    fi
    act git worktree remove "$wt" || { echo "kept $wt: git worktree remove failed"; return 1; }
  fi
  if git rev-parse -q --verify "refs/heads/$branch" >/dev/null; then
    act git branch -q -D "$branch" || return 1
  fi
  [[ "$DRY" == 1 ]] || echo "removed $branch ($why)"
}

for dir in "$ROOT"/*/; do
  [[ -d "$dir" ]] || continue
  dir="${dir%/}"
  key="$(basename "$dir")"
  name="$(printf 'dc-%s' "$key" | cut -c1-64)"
  if [[ -d "/tmp/clubmate-test-$name" ]]; then
    tapp="$dir/tree/scripts/test-app.sh"
    [[ -x "$tapp" ]] || tapp="$MAIN/scripts/test-app.sh"
    act "$tapp" stop "$name" || echo "could not stop test instance $name"
  fi
  for udid in $(xcrun simctl list devices 2>/dev/null | grep " cm-design-$key (" | grep -oE '[0-9A-F-]{36}'); do
    act xcrun simctl shutdown "$udid" 2>/dev/null
    act xcrun simctl delete "$udid" && echo "deleted simulator cm-design-$key"
  done
  if [[ -e "$dir/tree" ]]; then
    act git worktree remove --force "$dir/tree" || act rm -rf "$dir/tree"
  fi
  act rm -rf "$dir"
  [[ "$DRY" == 1 ]] || echo "removed $dir"
done
act git worktree prune

if [[ -f "$LANDED" ]]; then
  while read -r sha branch; do
    [[ -n "$branch" ]] || continue
    remove_fix "$branch" "landed as ${sha:0:8}" && drop_landed "$branch"
  done < "$LANDED"
fi

for branch in $(git for-each-ref --format='%(refname:short)' 'refs/heads/design/'); do
  [[ -f "$LANDED" ]] && grep -q " $branch\$" "$LANDED" && continue
  if [[ "$(git rev-list --count "HEAD..$branch")" == 0 ]]; then
    remove_fix "$branch" "no commits of its own"
  elif ! git cherry HEAD "$branch" | grep -q '^+'; then
    remove_fix "$branch" "its commits are on $(git symbolic-ref -q --short HEAD || echo HEAD)"
  else
    echo "kept $branch: commits that did not land ($(worktree_of "$branch"))"
  fi
done
exit 0
