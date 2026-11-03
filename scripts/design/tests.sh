#!/usr/bin/env bash
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

[[ $# -eq 0 || "$1" == "--dry-run" ]] || { echo "usage: $0 [--dry-run]   test the main checkout after this run's fixes landed" >&2; exit 2; }
DRY="${1:-}"
MAIN="$(main_checkout)"
cd "$MAIN" || exit 1
DIR="$(run_dir)"
export DESIGN_CHECK_RUN="$(run_key)"
strip_clubmate_env

if [[ ! -s "$DIR/merged" ]]; then
  echo "nothing landed in this run: nothing to test"
  exit 0
fi
FILES="$(for sha in $(awk '{print $1}' "$DIR/merged"); do git show --name-only --format= "$sha"; done | sort -u)"
touched() {
  printf '%s\n' "$FILES" | grep -q "^$1"
}

SUITES="go jsonrpc2"
touched client/ && SUITES="$SUITES app"
{ touched client/ClubmateKit/ || touched client/ClubmateProtocol/; } && SUITES="$SUITES kit"
{ touched client/CompanionKit/ || touched client/ClubmateProtocol/; } && SUITES="$SUITES companion-kit"
{ touched client/Companion/ || touched client/CompanionKit/ || touched client/ClubmateDocument/; } && SUITES="$SUITES companion"

WORK="$MAIN"
SHA="$(git rev-parse HEAD)"
if [[ -d "$DIR/tree" ]] && git -C "$DIR/tree" rev-parse --git-dir >/dev/null 2>&1; then
  WORK="$DIR/tree"
fi

if [[ "$DRY" == "--dry-run" ]]; then
  echo "would run in $WORK at ${SHA:0:8}:$SUITES"
  exit 0
fi

if [[ "$WORK" != "$MAIN" ]] && ! git -C "$WORK" checkout -q --detach "$SHA" 2>/dev/null; then
  echo "could not check out ${SHA:0:8} in $WORK; testing $MAIN itself"
  WORK="$MAIN"
fi
cd "$WORK" || exit 1
echo "testing ${SHA:0:8} in $WORK"

LOG="$DIR/tests.log"
: > "$LOG"
RED=""
suite() {
  local name="$1"
  shift
  echo "== $name" >> "$LOG"
  if (eval "$*") >> "$LOG" 2>&1; then
    echo "pass $name"
  else
    echo "FAIL $name"
    RED="$RED $name"
  fi
}

for s in $SUITES; do
  case "$s" in
    go) suite go 'go test $(go list ./... | grep -v -e /dist/ -e /.worktrees/)' ;;
    jsonrpc2) suite jsonrpc2 'cd third_party/jsonrpc2 && go test ./...' ;;
    app) suite app 'make app' ;;
    kit) suite kit '(cd client/ClubmateProtocol && swift test) && cd client/ClubmateKit && swift test --skip testRestartUnderTrafficKeepsReceivingUpdates' ;;
    companion-kit) suite companion-kit 'cd client/CompanionKit && swift test' ;;
    companion) suite companion 'make companion' ;;
  esac
done

if [[ -n "$RED" ]]; then
  echo "red:$RED (log $LOG)"
  grep -E -- '^(--- FAIL|FAIL|ok  )|error:|failed' "$LOG" | grep -v '^ok' | tail -40
  exit 1
fi
echo "green:$SUITES"
