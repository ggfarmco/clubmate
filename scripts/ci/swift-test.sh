#!/usr/bin/env bash
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT/$1" || exit 1
shift
LOG="$(mktemp)"
trap 'rm -f "$LOG"' EXIT

failed() {
  sed -n "s/^Test Case '-\[\([^] ]*\) \([^]]*\)\]' failed.*/\1\/\2/p" "$LOG" | sort -u
}

finished() {
  grep -Eq "^Test Suite '(All tests|Selected tests)' (passed|failed)" "$LOG" && ! grep -q "Exited with" "$LOG"
}

ran() {
  grep -Eq "Executed [1-9][0-9]* tests?, with 0 failures" "$LOG"
}

swift test "$@" 2>&1 | tee "$LOG"
status=${PIPESTATUS[0]}
first="$(failed)"
for ((i = 1; i <= ${TEST_RERUNS:-2}; i++)); do
  ((status != 0)) || break
  finished || break
  names="$(failed)"
  if [[ -z "$names" ]] || (($(wc -l <<<"$names") > 10)); then
    break
  fi
  echo "rerun $i: $(paste -sd' ' - <<<"$names")"
  swift test "$@" --skip-build --filter "^($(paste -sd'|' - <<<"$names"))\$" 2>&1 | tee "$LOG"
  status=${PIPESTATUS[0]}
  if ((status == 0)) && ! ran; then
    echo "::error::the rerun of $(paste -sd' ' - <<<"$names") ran no test"
    status=1
  fi
done
if ((status == 0)) && [[ -n "$first" ]]; then
  sed 's/$/: failed, then passed on a rerun/' <<<"$first" | "$ROOT/scripts/ci/flaky.sh"
fi
exit "$status"
