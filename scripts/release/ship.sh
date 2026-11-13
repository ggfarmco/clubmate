#!/usr/bin/env bash
set -uo pipefail

if [[ $# -ne 3 ]]; then
  echo "usage: $0 <version> <sha> <workspace>" >&2
  exit 2
fi

VERSION="$1"
SHA="$2"
WS="$3"
REPO=ggfarmco/clubmate
CM="${SHIP_CLUBMATE:-/Applications/Clubmate.app/Contents/Resources/bin/clubmate}"
GH="${SHIP_GH:-gh}"
POLL="${SHIP_POLL_SECONDS:-10}"
export PATH="/opt/homebrew/bin:$PATH"
cd "$(dirname "$0")/../.." || exit 1

notify() {
  local kind="$1"
  shift
  env -i HOME="$HOME" PATH=/usr/bin:/bin "$CM" notify --workspace "$WS" --kind "$kind" --title "Clubmate release v$VERSION" "$*"
}

stuck() {
  echo "STUCK: $*"
  notify stuck "$*"
  exit 1
}

killed() {
  trap - TERM HUP INT
  local at="before tagging"
  [[ -n "${TAGGED:-}" ]] && at="after pushing v$VERSION; check the release run and the tag by hand"
  [[ -n "${RUN:-}" ]] && at="while watching release run $RUN; check it and the v$VERSION tag by hand"
  stuck "ship.sh got SIG$1 $at"
}
trap 'killed TERM' TERM
trap 'killed HUP' HUP
trap 'killed INT' INT

newest_run() {
  "$GH" run list -R "$REPO" "$@" --limit 1 --json databaseId -q '.[0].databaseId'
}

run_of() {
  local after="$1" id=""
  shift
  for _ in $(seq 30); do
    id="$(newest_run "$@")"
    [[ -n "$id" ]] && ((id > after)) && break
    id=""
    sleep "$POLL"
  done
  echo "$id"
}

echo "=== $(date) ship v$VERSION on $SHA"
CI_RUN="$(run_of 0 --workflow ci.yml --commit "$SHA")"
[[ -n "$CI_RUN" ]] || stuck "no ci run for $SHA; no tag pushed"
echo "ci run $CI_RUN"

git fetch -q --tags origin
git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null && stuck "tag v$VERSION already exists; not tagging"
BEFORE="$(newest_run --workflow release.yml --branch "v$VERSION" --commit "$SHA")" || stuck "listing release runs for v$VERSION failed; no tag pushed"
git tag "v$VERSION" "$SHA" || stuck "tagging failed"
git push -q origin "v$VERSION" || stuck "tag push failed"
TAGGED=1

RUN="$(run_of "${BEFORE:-0}" --workflow release.yml --branch "v$VERSION" --commit "$SHA")"
[[ -n "$RUN" ]] || stuck "no release run for v$VERSION newer than ${BEFORE:-none}; tag v$VERSION left in place"
echo "release run $RUN"
if ! "$GH" run watch "$RUN" -R "$REPO" --exit-status --interval 60 >/dev/null; then
  "$GH" release view "v$VERSION" -R "$REPO" >/dev/null 2>&1 && stuck "release run $RUN failed after publishing v$VERSION"
  git push -q origin ":refs/tags/v$VERSION" && git tag -d "v$VERSION" >/dev/null
  ci="$("$GH" run view "$CI_RUN" -R "$REPO" --json conclusion -q .conclusion)"
  [[ -z "$ci" || "$ci" == success ]] || stuck "ci run $CI_RUN ended ${ci:-unknown}; release run $RUN stopped before publishing, tag v$VERSION deleted"
  stuck "release run $RUN failed; tag v$VERSION deleted"
fi
"$GH" release view "v$VERSION" -R "$REPO" >/dev/null || stuck "release v$VERSION not published"
notify "done" "v$VERSION published (release run $RUN)"
