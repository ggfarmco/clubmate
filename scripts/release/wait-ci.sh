#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <sha>" >&2
  exit 2
fi

SHA="$1"
APPEAR_SECONDS="${WAIT_CI_APPEAR_SECONDS:-300}"
TOTAL_SECONDS="${WAIT_CI_TOTAL_SECONDS:-3600}"
INTERVAL="${WAIT_CI_INTERVAL:-20}"

start=$SECONDS
while :; do
  run="$(gh run list --workflow ci.yml --commit "$SHA" --event push --limit 1 --json databaseId,status,conclusion -q '.[0] | select(.) | "\(.databaseId) \(.status) \(.conclusion)"')"
  elapsed=$((SECONDS - start))
  if [[ -z "$run" ]]; then
    if ((elapsed >= APPEAR_SECONDS)); then
      echo "error: no ci run on main for $SHA after ${APPEAR_SECONDS}s" >&2
      exit 1
    fi
  else
    read -r id status conclusion <<<"$run"
    if [[ "$status" == completed ]]; then
      if [[ "$conclusion" == success ]]; then
        echo "ci run $id passed"
        exit 0
      fi
      echo "error: ci run $id for $SHA ended $conclusion" >&2
      exit 1
    fi
  fi
  if ((elapsed >= TOTAL_SECONDS)); then
    echo "error: ci run for $SHA still ${status:-missing} after ${TOTAL_SECONDS}s" >&2
    exit 1
  fi
  sleep "$INTERVAL"
done
