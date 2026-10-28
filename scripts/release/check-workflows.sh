#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
DIR="$ROOT/.github/workflows"
CI="$DIR/ci.yml"
RELEASE="$DIR/release.yml"
HOSTED=" ubuntu-latest ubuntu-slim ubuntu-22.04 ubuntu-22.04-arm ubuntu-24.04 ubuntu-24.04-arm ubuntu-26.04 ubuntu-26.04-arm macos-latest macos-latest-large macos-14 macos-14-large macos-14-xlarge macos-15 macos-15-intel macos-15-large macos-15-xlarge macos-26 macos-26-intel macos-26-large macos-26-xlarge xcode-27 xcode-27-xlarge windows-latest windows-2022 windows-2025 windows-2025-vs2026 windows-11-arm windows-11-vs2026-arm "

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
[[ "$(yq '[.jobs[] | .environment | select(tag == "!!map") |= .name] | all_c(. == "release")' "$RELEASE")" == true ]] || fail "release.yml jobs must use environment release"

for f in "$DIR"/*.yml; do
  triggers="$(yq '(.on | select(tag == "!!map") | keys | .[]), (.on | select(tag == "!!seq") | .[]), (.on | select(tag == "!!str"))' "$f")"
  grep -qx 'pull_request' <<<"$triggers" || continue
  while IFS= read -r label; do
    [[ "$HOSTED" == *" ${label//\"/} "* && "$label" == \"*\" ]] || fail "${f#"$ROOT"/} runs pull_request jobs on non-GitHub-hosted runner $label"
  done < <(yq -o=json -I=0 '.jobs[].runs-on' "$f")
done

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
