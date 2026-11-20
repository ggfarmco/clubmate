#!/usr/bin/env bash
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

usage() {
  echo "usage: $0 check <branch>             check that git_land can land a fix branch into the main checkout's branch" >&2
  echo "       $0 record <sha> <branch> [run]   record a landed fix commit for this run" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage
MODE="$1"
shift
unset CLUBMATE_GRAPH_RUN
MAIN="$(main_checkout)"
cd "$MAIN" || exit 1

case "$MODE" in
check)
  [[ $# -eq 1 ]] || usage
  BRANCH="$1"
  git rev-parse -q --verify "refs/heads/$BRANCH" >/dev/null || fail "no branch $BRANCH"
  TARGET="$(git symbolic-ref -q --short HEAD)" || fail "HEAD is detached in $MAIN"
  [[ "$TARGET" != "$BRANCH" ]] || fail "$BRANCH itself is checked out in $MAIN"
  git diff --cached --quiet || fail "the index of $MAIN is not empty: another session is committing; try again later"
  COUNT="$(git rev-list --count --no-merges "HEAD..$BRANCH")" || fail "cannot compare $BRANCH with $TARGET"
  [[ "$COUNT" -gt 0 ]] || fail "$BRANCH has no commits that $TARGET lacks"
  BASE="$(git merge-base HEAD "$BRANCH")"
  CHANGED="$(git diff --name-only "$BASE" "$BRANCH")"
  DIRTY="$(git status --porcelain --untracked-files=all | cut -c4-)"
  for f in $CHANGED; do
    if printf '%s\n' "$DIRTY" | grep -qxF "$f"; then
      fail "$f has uncommitted changes in $MAIN and $BRANCH changes it too"
    fi
  done
  echo "ok: $BRANCH can land on $TARGET"
  ;;
record)
  [[ $# -ge 2 && $# -le 3 ]] || usage
  SHA="$1"
  BRANCH="$2"
  [[ $# -lt 3 ]] || DESIGN_CHECK_RUN="$3"
  DIR="$(run_dir)"
  SHA="$(git rev-parse -q --verify "$SHA^{commit}")" || fail "no commit $SHA"
  git merge-base --is-ancestor "$SHA" HEAD || fail "$SHA is not on $(git symbolic-ref -q --short HEAD || echo HEAD)"
  mkdir -p "$DIR"
  printf '%s %s\n' "$SHA" "$BRANCH" >> "$DIR/merged"
  printf '%s %s\n' "$SHA" "$BRANCH" >> "$(check_root)/landed"
  echo "recorded $BRANCH as $(git log -1 --format='%h %s' "$SHA")"
  ;;
*)
  usage
  ;;
esac
