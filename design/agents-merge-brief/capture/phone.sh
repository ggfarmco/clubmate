#!/usr/bin/env bash
set -uo pipefail

REPO="$(cd "$(dirname "$0")/../../.." && pwd)"
B="$REPO/build/agents-merge-brief"
OUT="${OUT:-$B/phone-shots}"
SIM="${SIM:-cm-agents-merge}"
APP="$REPO/build/CompanionDerivedData/Build/Products/Debug-iphonesimulator/Companion.app"
PAIR='clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000'
GATE="${GATE:?gate id from the Mac scene}"
RUN="${RUN:?waiting run id from the Mac scene}"

UDID="$(xcrun simctl list devices | grep -F " $SIM (" | grep -oE '[0-9A-F-]{36}' | head -1)"
[[ -n "$UDID" ]] || UDID="$(xcrun simctl create "$SIM" "iPhone 18 Pro")"
xcrun simctl bootstatus "$UDID" -b >/dev/null 2>&1
xcrun simctl status_bar "$UDID" override --time 9:41 >/dev/null 2>&1
BUNDLE="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Info.plist")"
mkdir -p "$OUT"

shot() {
  local name="$1" fixtures="$2"; shift 2
  for look in dark light; do
    xcrun simctl ui "$UDID" appearance "$look" >/dev/null 2>&1
    xcrun simctl uninstall "$UDID" "$BUNDLE" >/dev/null 2>&1
    xcrun simctl install "$UDID" "$APP" >/dev/null 2>&1
    xcrun simctl launch "$UDID" "$BUNDLE" -fixtures "$fixtures" -autoConfirmPairing YES -pair "$PAIR" "$@" >/dev/null 2>&1
    sleep 9
    xcrun simctl io "$UDID" screenshot "$OUT/phone-$name-$look.png" >/dev/null 2>&1 && echo "shot phone-$name-$look"
    xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1
  done
}

shot agents-runs "$B/phone-fixtures" -runs YES -workspace prj_00000000000000f2 -open agents
shot run-detail "$B/phone-fixtures" -runs YES -workspace prj_00000000000000f2 -open agents/gr_0000000000000004
shot more "$B/phone-fixtures" -open more
shot gates-list "$B/phone-fixtures" -open more/agents.gates
shot gates-detail "$B/phone-fixtures" -open "more/agents.gates/$GATE"
shot graphs-list "$B/phone-fixtures" -open more/graphs
shot graphs-run "$B/phone-fixtures" -open "more/graphs/$RUN"
shot graphs-definition "$B/phone-fixtures-def" -open more/graphs/graph:review-loop
