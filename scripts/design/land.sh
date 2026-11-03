#!/usr/bin/env bash
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

[[ $# -ge 1 && $# -le 2 ]] || { echo "usage: $0 <branch> [run]   squash a fix branch into the main checkout's branch and record it for this run" >&2; exit 2; }
BRANCH="$1"
[[ $# -lt 2 ]] || DESIGN_CHECK_RUN="$2"
unset CLUBMATE_GRAPH_RUN
MAIN="$(main_checkout)"
cd "$MAIN" || exit 1
DIR="$(run_dir)"

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

if [[ "$COUNT" -eq 1 ]]; then
  MSG="$(git log -1 --no-merges --format=%B "HEAD..$BRANCH")"
else
  SUBJECTS="$(git log --reverse --no-merges --format=%s "HEAD..$BRANCH")"
  MSG="$(head -1 <<<"$SUBJECTS")"$'\n\n'"$(tail -n +2 <<<"$SUBJECTS" | sed 's/^/- /')"
fi

if ! git merge --squash -q "$BRANCH" >/dev/null; then
  git reset -q --merge
  fail "$BRANCH does not squash cleanly onto $TARGET: rebase it first"
fi
if git diff --cached --quiet; then
  fail "$BRANCH changes nothing on $TARGET"
fi
if ! git commit -q -m "$MSG"; then
  git reset -q --merge
  fail "commit on $TARGET failed"
fi
SHA="$(git rev-parse HEAD)"
mkdir -p "$DIR"
printf '%s %s\n' "$SHA" "$BRANCH" >> "$DIR/merged"
printf '%s %s\n' "$SHA" "$BRANCH" >> "$(check_root)/landed"
echo "landed $BRANCH as $(git log -1 --format='%h %s')"
