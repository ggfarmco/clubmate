#!/usr/bin/env bash
set -uo pipefail

usage() {
  echo "usage: $0 <simulator name> <device type> [test class or test, e.g. CompanionE2ETests/PushTests | --hold]" >&2
  echo "  --hold starts the core, the helper and the simulator, prints how to reach them and waits" >&2
  exit 2
}
[[ $# -ge 2 ]] || usage
SIM="$1" SIM_TYPE="$2" ONLY="${3:-}"
[[ "$SIM" =~ ^cm-e2e ]] || { echo "simulator name must start with cm-e2e: the run erases it" >&2; exit 2; }

cd "$(dirname "$0")/../.." || exit 1
ROOT="$PWD"
OUT="$ROOT/build/companion-e2e/$SIM"
for v in $(env | grep -o '^CLUBMATE_[A-Z_]*\|^FAKEAGENT_[A-Z_]*' || true); do unset "$v"; done

free_port() { python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'; }

rm -rf "$OUT" && mkdir -p "$OUT"/{state,signals,bin,home,repo/.clubmate/graphs} || exit 1
RUN="$(mktemp -d /tmp/cm-e2e.XXXXXX)" || exit 1
chmod 700 "$RUN"
CORE="" HELPER="" UDID=""

cleanup() {
  [[ -n "$HELPER" ]] && kill "$HELPER" 2>/dev/null
  if [[ -n "$CORE" ]]; then
    kill "$CORE" 2>/dev/null
    for _ in $(seq 1 75); do kill -0 "$CORE" 2>/dev/null || break; sleep 0.2; done
    kill -9 "$CORE" 2>/dev/null
  fi
  [[ -n "$UDID" ]] && xcrun simctl shutdown "$UDID" 2>/dev/null
  rm -rf "$RUN"
}
trap cleanup EXIT

[[ -x bin/clubmate-core && -x bin/clubmate ]] || { echo "run make build first" >&2; exit 1; }
go build -o "$OUT/bin/fakeagent" ./plugins/agents/internal/fakeagent || exit 1
go build -o "$OUT/bin/e2e-helper" ./scripts/companion-e2e/helper || exit 1
printf '#!/bin/sh\ncat >/dev/null\necho "The agent ticked twice and stopped."\n' > "$OUT/bin/claude" && chmod +x "$OUT/bin/claude"
sed "s#__SIGNALS__#$OUT/signals#g" scripts/companion-e2e/fakeagent.json > "$OUT/fakeagent.json" || exit 1

REMOTE_PORT="$(free_port)" HELPER_PORT="$(free_port)"
cm() {
  env CLUBMATE_STATE_DIR="$OUT/state" CLUBMATE_RUNTIME_DIR="$RUN" ${WORKSPACE:+CLUBMATE_WORKSPACE_ID="$WORKSPACE"} bin/clubmate "$@"
}

env -i HOME="$OUT/home" USER="$USER" LOGNAME="${LOGNAME:-$USER}" TMPDIR="${TMPDIR:-/tmp}" SHELL=/bin/zsh \
  PATH="$OUT/bin:/usr/bin:/bin:/usr/sbin:/sbin" \
  CLUBMATE_STATE_DIR="$OUT/state" CLUBMATE_RUNTIME_DIR="$RUN" CLUBMATE_TEST_MODE=1 CLUBMATE_SHELL_ENV=0 \
  CLUBMATE_REMOTE_LISTEN="127.0.0.1:$REMOTE_PORT" CLUBMATE_AGENTS_COMMAND="$OUT/bin/fakeagent" \
  FAKEAGENT_SCRIPT="$OUT/fakeagent.json" FAKEAGENT_LOG="$OUT/fakeagent.log" \
  bin/clubmate-core > "$OUT/core.log" 2>&1 &
CORE=$!
for _ in $(seq 1 100); do cm core status >/dev/null 2>&1 && break; sleep 0.2; done
cm core status >/dev/null 2>&1 || { echo "core did not start, see $OUT/core.log" >&2; exit 1; }
cm plugin disable voice >/dev/null && cm plugin disable editor-nvim >/dev/null || exit 1
for _ in $(seq 1 100); do cm command run remote.status >/dev/null 2>&1 && cm command run agents.sessions >/dev/null 2>&1 && break; sleep 0.2; done

cp scripts/companion-e2e/graph.json "$OUT/repo/.clubmate/graphs/e2e.json" || exit 1
git -C "$OUT/repo" init -q -b main && git -C "$OUT/repo" add -A && git -C "$OUT/repo" -c user.name=e2e -c user.email=e2e@example.com commit -q -m init || exit 1
WORKSPACE="$(cm --json workspace create e2e --repo "$OUT/repo" | python3 -c 'import json, sys; print(json.load(sys.stdin)["id"])')" || exit 1
cm command run graphs.trust graph=e2e >/dev/null || exit 1
cm command run remote.set-push-relay url="http://127.0.0.1:$HELPER_PORT" token=e2e >/dev/null || exit 1

UDID="$(xcrun simctl list devices | grep " $SIM (" | grep -oE '[0-9A-F-]{36}' | head -1)"
[[ -n "$UDID" ]] || UDID="$(xcrun simctl create "$SIM" "$SIM_TYPE")" || exit 1
xcrun simctl shutdown "$UDID" 2>/dev/null
xcrun simctl erase "$UDID" && xcrun simctl boot "$UDID" && xcrun simctl bootstatus "$UDID" >/dev/null || exit 1
xcrun simctl spawn "$UDID" defaults write com.apple.keyboard.preferences DidShowContinuousPathIntroduction -bool true

"$OUT/bin/e2e-helper" -addr "127.0.0.1:$HELPER_PORT" -socket "$RUN/core.sock" -workspace "$WORKSPACE" \
  -signals "$OUT/signals" -repos "$OUT/repos" -agent-log "$OUT/fakeagent.log" -sim "$UDID" > "$OUT/helper.log" 2>&1 &
HELPER=$!

if [[ "$ONLY" == "--hold" ]]; then
  echo "helper http://127.0.0.1:$HELPER_PORT  simulator $UDID  workspace $WORKSPACE"
  echo "export CLUBMATE_STATE_DIR=$OUT/state CLUBMATE_RUNTIME_DIR=$RUN CLUBMATE_WORKSPACE_ID=$WORKSPACE"
  wait "$HELPER"
  exit 0
fi

cd client && xcodegen generate --spec companion.yml >/dev/null || exit 1
TEST_RUNNER_E2E_HELPER="http://127.0.0.1:$HELPER_PORT" TEST_RUNNER_E2E_OUT="$OUT" \
  xcodebuild test -project Companion.xcodeproj -scheme CompanionE2E -destination "id=$UDID" -parallel-testing-enabled NO \
  -configuration Debug -derivedDataPath ../build/CompanionDerivedData CODE_SIGN_IDENTITY=- CODE_SIGN_STYLE=Manual \
  ${ONLY:+-only-testing:"$ONLY"}
status=$?
xcrun simctl spawn "$UDID" log show --last 30m --style compact --predicate 'process == "Companion" OR process == "apsd" OR subsystem == "com.clubmate.companion"' > "$OUT/simulator.log" 2>&1
echo "companion-e2e: simulator log $OUT/simulator.log"
echo "companion-e2e: core log $OUT/core.log, fake agent log $OUT/fakeagent.log, helper log $OUT/helper.log"
exit $status
