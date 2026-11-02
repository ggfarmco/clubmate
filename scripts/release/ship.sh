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
CM=/Applications/Clubmate.app/Contents/Resources/bin/clubmate
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

run_of() {
  local id=""
  for _ in $(seq 30); do
    id="$(gh run list -R "$REPO" "$@" --limit 1 --json databaseId -q '.[0].databaseId')"
    [[ -n "$id" ]] && break
    sleep 10
  done
  echo "$id"
}

echo "=== $(date) ship v$VERSION on $SHA"
CI_RUN="$(run_of --workflow ci.yml --commit "$SHA")"
[[ -n "$CI_RUN" ]] || stuck "no ci run for $SHA; no tag pushed"
echo "ci run $CI_RUN"
gh run watch "$CI_RUN" -R "$REPO" --exit-status --interval 60 >/dev/null || stuck "ci run $CI_RUN failed; no tag pushed"

git fetch -q --tags origin
git rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null && stuck "tag v$VERSION already exists; not tagging"
git tag "v$VERSION" "$SHA" || stuck "tagging failed"
git push -q origin "v$VERSION" || stuck "tag push failed"

RUN="$(run_of --workflow release.yml --branch "v$VERSION")"
[[ -n "$RUN" ]] || stuck "no release run for v$VERSION"
echo "release run $RUN"
gh run watch "$RUN" -R "$REPO" --exit-status --interval 60 >/dev/null || stuck "release run $RUN failed"
gh release view "v$VERSION" -R "$REPO" >/dev/null || stuck "release v$VERSION not published"
notify "done" "v$VERSION published (release run $RUN)"
