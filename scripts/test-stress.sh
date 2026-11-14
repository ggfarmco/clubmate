#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
BASE="${1:-}"
BUDGET="${STRESS_BUDGET_SECONDS:-900}"
CAP="${STRESS_PACKAGE_SECONDS:-300}"

if [[ -z "$BASE" || "$BASE" =~ ^0+$ ]] || ! git cat-file -e "$BASE^{commit}" 2>/dev/null; then
  echo "base ${BASE:-<none>} is missing, using HEAD~1"
  BASE="$(git rev-parse HEAD~1)"
fi

plan="$(go run ./tools/changedtests "$BASE")"
if [[ -z "$plan" ]]; then
  echo "no changed tests since $BASE"
  exit 0
fi
echo "changed tests since $BASE:"
echo "$plan"

HANG="${STRESS_HANG_SECONDS:-60}"

oldest() {
  sed -n '/^panic: test timed out/,/^goroutine /p' "$1" | grep -oE '\(([0-9]+m)?[0-9]+(\.[0-9]+)?s\)' | tr -d '()s' | awk -F m '{ t = NF == 2 ? $1 * 60 + $2 : $1; if (t > max) max = t } END { printf "%d\n", max }'
}

LOG="$(mktemp)"
HOGS=""
trap 'kill $HOGS 2>/dev/null; rm -f "$LOG"' EXIT
for _ in 1 2; do
  yes >/dev/null &
  HOGS="$HOGS $!"
done
disown -a

start=$SECONDS
status=0
skipped=()
while IFS=$'\t' read -r pkg run; do
  left=$((BUDGET - (SECONDS - start)))
  if ((left <= 0)); then
    skipped+=("$pkg $run")
    continue
  fi
  limit=$((left < CAP ? left : CAP))
  if TEST_TIMEOUT_SCALE=1 go test -race -count=10 -timeout "${limit}s" -run "$run" "$pkg" 2>&1 | tee "$LOG"; then
    continue
  fi
  if grep -q '^panic: test timed out' "$LOG" && ! grep -qE -- '--- FAIL|WARNING: DATA RACE' "$LOG" && (($(oldest "$LOG") < HANG)); then
    echo "::warning::stress time of ${limit}s ran out inside $pkg with no test hung, not fully stressed: $run"
  else
    status=1
  fi
done <<<"$plan"
if ((${#skipped[@]} > 0)); then
  echo "::warning::stress budget of ${BUDGET}s spent, not stressed: $(IFS=';'; echo "${skipped[*]}")"
fi
exit "$status"
