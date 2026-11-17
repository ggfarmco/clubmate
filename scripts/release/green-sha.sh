#!/usr/bin/env bash
set -uo pipefail

REPO=ggfarmco/clubmate
GH="${SHIP_GH:-gh}"
REF="${1:-origin/main}"
GREEN="$("$GH" run list -R "$REPO" --workflow ci.yml --branch main --event push --status success --limit 100 --json headSha -q '.[].headSha')" || exit 1
while read -r sha; do
  if grep -qx "$sha" <<<"$GREEN"; then
    echo "$sha"
    exit 0
  fi
done < <(git rev-list --first-parent -n 200 "$REF")
echo "green-sha.sh: no commit of $REF has a green ci run" >&2
exit 1
