#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DIR="$ROOT/.github/workflows"
CI="$DIR/ci.yml"
RELEASE="$DIR/release.yml"

fail() {
  echo "error: $*" >&2
  exit 1
}

for f in "$CI" "$RELEASE"; do
  [[ -f "$f" ]] || fail "${f#"$ROOT"/} not found"
done

actionlint -ignore 'label "xcode-27" is unknown' "$DIR"/*.yml

[[ "$(yq -o=json -I=0 '.on | keys' "$RELEASE")" == '["push"]' ]] || fail "release.yml must trigger on push only"
[[ "$(yq -o=json -I=0 '.on.push | keys' "$RELEASE")" == '["tags"]' ]] || fail "release.yml push trigger must filter tags only"
[[ "$(yq -o=json -I=0 '.on.push.tags' "$RELEASE")" == '["v*"]' ]] || fail "release.yml must trigger on v* tags"

if grep -n 'secrets\.' "$CI"; then
  fail "ci.yml must not reference secrets"
fi

if grep -n 'pull_request_target' "$DIR"/*.yml; then
  fail "workflows must not use pull_request_target"
fi

if grep -nE '^[[:space:]-]*uses:' "$DIR"/*.yml | grep -vE 'uses:[[:space:]]*[^@[:space:]]+@[0-9a-f]{40}[[:space:]]*$'; then
  fail "actions must be pinned by full commit sha"
fi

echo "workflows ok"
