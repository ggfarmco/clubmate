#!/usr/bin/env bash
set -uo pipefail

fail() {
  echo "ship-next.sh: $*" >&2
  exit 1
}

cd "$(dirname "$0")/../.." || exit 1
[[ -n "${CLUBMATE_WORKSPACE_ID:-}" ]] || fail "CLUBMATE_WORKSPACE_ID is not set"
git fetch -q --tags origin || fail "fetching tags from origin failed"
SHA="$(scripts/release/green-sha.sh origin/main)" || fail "no green commit on origin/main to release"
TAGGED="$(git tag -l 'v*' --points-at "$SHA")"
if [[ -n "$TAGGED" ]]; then
  echo "$SHA, the newest green commit, is already tagged $TAGGED: nothing new to release"
  exit 0
fi
scripts/release/flaky-backlog.sh "$SHA" "$CLUBMATE_WORKSPACE_ID" || echo "ship-next.sh: filing flaky tests failed" >&2
LATEST="$(git tag -l 'v*' --sort=-v:refname | head -1)"
[[ "$LATEST" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || fail "newest tag '${LATEST}' is not vX.Y.Z"
VERSION="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
echo "shipping $VERSION on $SHA"
exec scripts/release/ship.sh "$VERSION" "$SHA" "$CLUBMATE_WORKSPACE_ID"
