#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <tag>" >&2
  exit 2
fi

TAG="$1"
cd "$(dirname "$0")/../.."

END=HEAD
if git rev-parse -q --verify "refs/tags/$TAG^{commit}" >/dev/null; then
  END="refs/tags/$TAG"
fi

RANGE="$END"
if PREV="$(git describe --tags --abbrev=0 --match 'v*' "$END^" 2>/dev/null)"; then
  RANGE="$PREV..$END"
fi

git log --no-merges --format='- %s' "$RANGE"
