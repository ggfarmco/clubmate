#!/usr/bin/env bash
set -uo pipefail

SIM=cm-nightly-ui
RESULTS=build/CompanionDerivedData/Logs/Test
CM=/Applications/Clubmate.app/Contents/Resources/bin/clubmate
export PATH="/opt/homebrew/bin:$PATH"
cd "$(dirname "$0")/../.." || exit 1

notify() {
  [[ -n "${CLUBMATE_WORKSPACE_ID:-}" && -x "$CM" ]] || return 0
  env -i HOME="$HOME" PATH=/usr/bin:/bin "$CM" notify --workspace "$CLUBMATE_WORKSPACE_ID" --kind stuck --title "Nightly: Companion UI tests" "$*"
}

mkdir -p build
MARK="$(mktemp build/companion-ui.XXXXXX)" || exit 1
trap 'rm -f "$MARK"' EXIT

make companion-ui-test COMPANION_SIM="$SIM"
status=$?
[[ $status -eq 0 ]] && exit 0

RESULT="$(find "$RESULTS" -maxdepth 1 -name '*.xcresult' -newer "$MARK" 2>/dev/null | sort | tail -1)"
echo
if [[ -z "$RESULT" ]]; then
  echo "companion-ui.sh: make companion-ui-test failed (exit $status) before it wrote test results; see the output above"
  notify "The run failed before any test ran (exit $status); main was not pushed."
  exit "$status"
fi
FAILED="$(xcrun xcresulttool get test-results summary --path "$RESULT" | jq -r '.testFailures[] | "\(.testIdentifierString): \(.failureText | split("\n")[0])"' | sort -u)"
echo "xcresult: $PWD/$RESULT"
if [[ -z "$FAILED" ]]; then
  echo "companion-ui.sh: no test failure recorded (exit $status): the build failed, or the run crashed or was killed; see the output above"
  notify "The build or the test run failed with no failing test (exit $status); main was not pushed."
  exit "$status"
fi
NAMES="$(printf '%s\n' "$FAILED" | sed 's/:.*//; s/^.*\///' | sort -u)"
COUNT="$(printf '%s\n' "$NAMES" | wc -l | tr -d ' ')"
echo "companion-ui.sh: $COUNT failing test(s), exit $status"
printf '%s\n' "$FAILED"
notify "$COUNT failed: $(printf '%s\n' "$NAMES" | paste -sd, - | sed 's/,/, /g'); main was not pushed."
exit "$status"
