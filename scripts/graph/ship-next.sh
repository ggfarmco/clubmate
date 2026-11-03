#!/usr/bin/env bash
set -uo pipefail

fail() {
  echo "ship-next.sh: $*" >&2
  exit 1
}

cd "$(dirname "$0")/../.." || exit 1
[[ -n "${CLUBMATE_WORKSPACE_ID:-}" ]] || fail "CLUBMATE_WORKSPACE_ID is not set"
git fetch -q --tags origin || fail "fetching tags from origin failed"
SHA="$(git rev-parse HEAD)" || fail "no HEAD"
TAGGED="$(git tag -l 'v*' --points-at "$SHA")"
[[ -z "$TAGGED" ]] || fail "$SHA is already tagged $TAGGED: nothing to release"
LATEST="$(git tag -l 'v*' --sort=-v:refname | head -1)"
[[ "$LATEST" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]] || fail "newest tag '${LATEST}' is not vX.Y.Z"
VERSION="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.$((BASH_REMATCH[3] + 1))"
echo "shipping $VERSION on $SHA"
exec scripts/release/ship.sh "$VERSION" "$SHA" "$CLUBMATE_WORKSPACE_ID"
