#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
BASE="${1:-}"
BUDGET="${STRESS_BUDGET_SECONDS:-900}"
COUNT="${STRESS_COUNT:-10}"
HANG="${STRESS_HANG_SECONDS:-60}"

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

WORK="$(mktemp -d)"
LOG="$WORK/log"
HOGS=""
trap 'kill $HOGS 2>/dev/null || true; rm -rf "$WORK"' EXIT

pkgs=()
runs=()
while IFS=$'\t' read -r pkg run; do
  pkgs+=("$pkg")
  runs+=("$run")
done <<<"$plan"

status=0
built=$SECONDS
for i in "${!pkgs[@]}"; do
  if ! go test -race -c -o "$WORK/$i.test" "${pkgs[$i]}"; then
    echo "::error::stress could not build ${pkgs[$i]}"
    status=1
  fi
done
echo "built ${#pkgs[@]} test binaries in $((SECONDS - built))s"

oldest() {
  sed -n '/^panic: test timed out/,/^goroutine /p' "$1" | grep -oE '\(([0-9]+m)?[0-9]+(\.[0-9]+)?s\)' | tr -d '()s' | awk -F m '{ t = NF == 2 ? $1 * 60 + $2 : $1; if (t > max) max = t } END { printf "%d\n", max }'
}

stress() {
  local rc=0
  (cd "$2" && TEST_TIMEOUT_SCALE=1 "$1" -test.count="$4" -test.timeout="${5}s" -test.run "$3") >"$LOG" 2>&1 || rc=$?
  cat "$LOG"
  return "$rc"
}

outOfTime() {
  grep -q '^panic: test timed out' "$LOG" && ! grep -qE -- '--- FAIL|WARNING: DATA RACE' "$LOG" && (($(oldest "$LOG") < HANG))
}

for _ in 1 2; do
  yes >/dev/null &
  HOGS="$HOGS $!"
done
disown -a

start=$SECONDS
skipped=()
short=()
for i in "${!pkgs[@]}"; do
  pkg="${pkgs[$i]}" run="${runs[$i]}" bin="$WORK/$i.test"
  [[ -x "$bin" ]] || continue
  left=$((BUDGET - (SECONDS - start)))
  if ((left <= 0)); then
    skipped+=("$pkg $run")
    continue
  fi
  share=$((left / (${#pkgs[@]} - i)))
  began=$SECONDS
  passed=0
  more=0
  limit=$((2 * (share > HANG ? share : HANG)))
  cap=$((limit < left ? limit : left))
  if stress "$bin" "$pkg" "$run" 1 $((cap > 1 ? cap : 1)); then
    passed=1
    took=$((SECONDS - began > 0 ? SECONDS - began : 1))
    more=$(((share - (SECONDS - began)) / took))
    more=$((more < COUNT - 1 ? more : COUNT - 1))
    more=$((more > 0 ? more : 0))
  fi
  if ((more > 0)); then
    left=$((BUDGET - (SECONDS - start)))
    limit=$((2 * more * took + HANG))
    cap=$((limit < left ? limit : left))
    if stress "$bin" "$pkg" "$run" "$more" $((cap > 1 ? cap : 1)); then
      passed=$((passed + more))
    fi
  fi
  echo "stress $pkg: $passed of $COUNT runs in $((SECONDS - began))s (share ${share}s)"
  if ((passed == 1 + more)); then
    ((passed == COUNT)) || short+=("$pkg $passed")
  elif outOfTime; then
    echo "::warning::stress time ran out inside $pkg with no test hung, $passed of $COUNT runs: $run"
  else
    status=1
  fi
done
if ((${#short[@]} > 0)); then
  echo "::warning::stress shares fit fewer than $COUNT runs: $(IFS=';'; echo "${short[*]}")"
fi
if ((${#skipped[@]} > 0)); then
  echo "::warning::stress budget of ${BUDGET}s spent, not stressed: $(IFS=';'; echo "${skipped[*]}")"
fi
exit "$status"
