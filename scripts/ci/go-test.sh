#!/usr/bin/env bash
set -uo pipefail

cd "$(dirname "$0")/../.." || exit 1
REPORT="$(mktemp)"
trap 'rm -f "$REPORT"' EXIT
go run gotest.tools/gotestsum@v1.12.3 --format pkgname-and-test-fails \
  --rerun-fails="${TEST_RERUNS:-2}" --rerun-fails-report="$REPORT" \
  --packages="$*" -- -race -timeout 30m
status=$?
((status == 0)) && scripts/ci/flaky.sh <"$REPORT"
exit "$status"
