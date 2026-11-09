#!/usr/bin/env bash
set -uo pipefail

CM=/Applications/Clubmate.app/Contents/Resources/bin/clubmate
export PATH="/opt/homebrew/bin:$PATH"
cd "$(dirname "$0")/../.." || exit 1

notify() {
  [[ -n "${CLUBMATE_WORKSPACE_ID:-}" && -x "$CM" ]] || return 0
  env -i HOME="$HOME" PATH=/usr/bin:/bin "$CM" notify --workspace "$CLUBMATE_WORKSPACE_ID" --kind stuck --title "Nightly: Mac UI tests" "$*"
}

if ! make app; then
  echo "mac-ui.sh: make app failed; see the output above"
  notify "The Debug app did not build; main was not pushed."
  exit 1
fi

mkdir -p build
LOG="$(mktemp build/mac-ui.XXXXXX)" || exit 1
trap 'rm -f "$LOG"' EXIT
make mac-ui-test 2>&1 | tee "$LOG"
status=${PIPESTATUS[0]}
[[ $status -eq 0 ]] && exit 0

NAMES="$(grep -E '^--- FAIL: ' "$LOG" | sed 's/^--- FAIL: //; s/ (.*//' | sort -u)"
if [[ -z "$NAMES" ]]; then
  echo "mac-ui.sh: make mac-ui-test failed (exit $status) with no failing test; see the output above"
  notify "The run failed with no failing test (exit $status); main was not pushed."
  exit "$status"
fi
COUNT="$(printf '%s\n' "$NAMES" | wc -l | tr -d ' ')"
echo "mac-ui.sh: $COUNT failing test(s), exit $status"
notify "$COUNT failed: $(printf '%s\n' "$NAMES" | paste -sd, - | sed 's/,/, /g'); main was not pushed."
exit "$status"
