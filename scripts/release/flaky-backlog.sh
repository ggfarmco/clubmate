#!/usr/bin/env bash
set -uo pipefail

SHA="$1"
WS="$2"
REPO=ggfarmco/clubmate
GH="${SHIP_GH:-gh}"
CM="${SHIP_CLUBMATE:-/Applications/Clubmate.app/Contents/Resources/bin/clubmate}"
RUN="$("$GH" run list -R "$REPO" --workflow ci.yml --commit "$SHA" --event push --status success --limit 1 --json databaseId -q '.[0].databaseId')" || exit 1
[[ -n "$RUN" ]] || exit 0
FLAKY=""
for job in $("$GH" api "repos/$REPO/actions/runs/$RUN/jobs" -q '.jobs[].id'); do
  FLAKY+="$("$GH" api "repos/$REPO/check-runs/$job/annotations" -q '.[] | select(.title == "Flaky test") | "- `\(.message)`"')"$'\n'
done
FLAKY="$(sed '/^$/d' <<<"$FLAKY" | sort -u)"
[[ -n "$FLAKY" ]] || exit 0
ARGS="$(jq -n --arg title "Flaky tests in ci run $RUN (${SHA:0:8})" --arg body "These tests failed on CI and passed on a rerun, so they did not block the release. Find each one's race (docs/testing.md) and fix it.

ci run: https://github.com/$REPO/actions/runs/$RUN

$FLAKY" '{title: $title, description: $body, status: "todo", tags: ["flaky", "ci"]}')"
env -i HOME="$HOME" PATH=/usr/bin:/bin "$CM" command run backlog.add --workspace "$WS" --args "$ARGS" >/dev/null
