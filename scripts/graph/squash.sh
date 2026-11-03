#!/usr/bin/env bash
set -uo pipefail

fail() {
  echo "squash.sh: $*" >&2
  exit 1
}

BRANCH="${CLUBMATE_GRAPH_BRANCH_CODER:-}"
[[ -n "$BRANCH" ]] || fail "CLUBMATE_GRAPH_BRANCH_CODER is not set: run me as a step after the coder node"
git rev-parse -q --verify "refs/heads/$BRANCH" >/dev/null || fail "no branch $BRANCH"
TARGET="$(git symbolic-ref -q --short HEAD)" || fail "HEAD is detached in $PWD: check out the branch to squash into"
[[ "$TARGET" != "$BRANCH" ]] || fail "$BRANCH itself is checked out in $PWD"
git diff --quiet && git diff --cached --quiet || fail "$PWD has uncommitted changes: commit or set them aside, then continue the run"
COUNT="$(git rev-list --count --no-merges "HEAD..$BRANCH")" || fail "cannot compare $BRANCH with $TARGET"
[[ "$COUNT" -gt 0 ]] || fail "$BRANCH has no commits that $TARGET lacks"

SUBJECTS="$(git log --reverse --no-merges --format=%s "HEAD..$BRANCH")"
if [[ "$COUNT" -eq 1 ]]; then
  MSG="$(git log -1 --no-merges --format=%B "HEAD..$BRANCH")"
else
  MSG="$(head -1 <<<"$SUBJECTS")"$'\n\n'"$(tail -n +2 <<<"$SUBJECTS" | sed 's/^/- /')"
fi

if ! git merge --squash -q "$BRANCH" >/dev/null; then
  git reset -q --merge
  fail "$BRANCH does not squash cleanly onto $TARGET: rebase it on $TARGET first"
fi
if git diff --cached --quiet; then
  fail "$BRANCH changes nothing on $TARGET"
fi
if ! git commit -q -m "$MSG"; then
  git reset -q --merge
  fail "commit on $TARGET failed"
fi
echo "squashed $COUNT commit(s) of $BRANCH into $TARGET: $(git log -1 --format='%h %s')"
