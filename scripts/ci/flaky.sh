#!/usr/bin/env bash
set -uo pipefail

flaky=()
while IFS= read -r line; do
  [[ -n "$line" ]] && flaky+=("$line")
done
((${#flaky[@]})) || exit 0
for line in "${flaky[@]}"; do
  echo "::warning title=Flaky test::$line"
done
if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
  {
    echo "### Flaky tests (failed, then passed on a rerun)"
    printf -- '- `%s`\n' "${flaky[@]}"
  } >>"$GITHUB_STEP_SUMMARY"
fi
