#!/usr/bin/env bash
set -uo pipefail

fail() {
  echo "capture-phone.sh: $*" >&2
  exit 1
}

board() {
  local name="$1" base="${1%-Light}" look=dark
  [[ "$name" == "$base" ]] || look=light
  case "$name" in
    C-Macs | C-Macs-Light) echo "$look 22 -macs 3 -open sidebar" ;;
    C-Workspace | C-Workspace-Light) echo "$look 9 -macs 3 -open agents" ;;
    C-Permission | C-Permission-Light) echo "$look 9 -macs 3 -open agents/agt_0000000000000001" ;;
    C-Question | C-Question-Light) echo "$look 9 -macs 3 -open agents/agt_0000000000000002" ;;
    C-Inbox | C-Inbox-Light) echo "$look 9 -macs 3 -open inbox" ;;
    C-MacInfo | C-MacInfo-Light) echo "$look 9 -macs 3 -open mac" ;;
    C-Backlog | C-Backlog-Light) echo "$look 9 -macs 3 -open backlog" ;;
    C-Offline) echo "dark 28 -offline 4 -open sidebar" ;;
    C-iPad) echo "ipad" ;;
    *) return 1 ;;
  esac
}

IPAD_SKIP="skipped C-iPad needs a UI-test rotation (judge the committed design/check/companion/C-iPad.png)"
PAIR='clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000'

TREE="" OUT="" SIM="" BUILD=1 DRY=0
BOARDS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --tree) [[ $# -ge 2 ]] || fail "--tree needs a value"; TREE="$2"; shift 2 ;;
    --out) [[ $# -ge 2 ]] || fail "--out needs a value"; OUT="$2"; shift 2 ;;
    --sim) [[ $# -ge 2 ]] || fail "--sim needs a value"; SIM="$2"; shift 2 ;;
    --no-build) BUILD=0; shift ;;
    --dry-run) DRY=1; shift ;;
    -*) fail "unknown option $1" ;;
    *) BOARDS+=("$1"); shift ;;
  esac
done

for b in ${BOARDS[@]+"${BOARDS[@]}"}; do
  if ! board "$b" >/dev/null; then
    echo "capture-phone.sh: unknown board $b" >&2
    exit 2
  fi
done
[[ -n "$TREE" ]] || fail "--tree is required"
[[ "$TREE" == /* ]] || fail "--tree must be an absolute path: $TREE"
[[ -d "$TREE" ]] || fail "--tree $TREE is not a directory"
[[ -n "$OUT" ]] || fail "--out is required"
[[ -n "$SIM" ]] || fail "--sim is required"
[[ ${#BOARDS[@]} -gt 0 ]] || fail "no boards given"

if [[ "$DRY" -eq 1 ]]; then
  for b in "${BOARDS[@]}"; do
    read -r look wait extra <<<"$(board "$b")"
    if [[ "$look" == ipad ]]; then
      echo "$IPAD_SKIP"
    else
      echo "would $b $look ${wait}s $extra"
    fi
  done
  exit 0
fi

SHOTS=0
for b in "${BOARDS[@]}"; do
  [[ "$b" == C-iPad ]] || SHOTS=$((SHOTS + 1))
done
if [[ "$SHOTS" -eq 0 ]]; then
  echo "$IPAD_SKIP"
  exit 0
fi

mkdir -p "$OUT" || fail "cannot create $OUT"
OUT="$(cd "$OUT" && pwd)" || fail "cannot enter $OUT"
APP="$TREE/build/CompanionDerivedData/Build/Products/Debug-iphonesimulator/Companion.app"
FIXTURES="$TREE/client/CompanionKit/Tests/CompanionKitTests/Fixtures"

if [[ "$BUILD" -eq 1 ]]; then
  if ! make -C "$TREE" companion >"$OUT/make-companion.log" 2>&1; then
    tail -20 "$OUT/make-companion.log" >&2
    fail "make companion failed in $TREE (log: $OUT/make-companion.log)"
  fi
fi
[[ -d "$APP" ]] || fail "no app at $APP: build it or drop --no-build"
[[ -d "$FIXTURES" ]] || fail "no fixtures at $FIXTURES"
BUNDLE="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleIdentifier' "$APP/Info.plist" 2>/dev/null)" || fail "no bundle id in $APP/Info.plist"

UDID=""
cleanup() {
  [[ -n "$UDID" ]] || return 0
  xcrun simctl shutdown "$UDID" >/dev/null 2>&1
  xcrun simctl delete "$UDID" >/dev/null 2>&1
}
trap cleanup EXIT
trap 'exit 1' INT TERM

UDID="$(xcrun simctl list devices | grep -F " $SIM (" | grep -oE '[0-9A-F-]{36}' | head -1)"
if [[ -z "$UDID" ]]; then
  UDID="$(xcrun simctl create "$SIM" "iPhone 18 Pro" 2>/dev/null)" || { UDID=""; fail "cannot create simulator $SIM"; }
fi
xcrun simctl bootstatus "$UDID" -b >/dev/null 2>&1 || fail "simulator $SIM did not boot"
xcrun simctl status_bar "$UDID" override --time 9:41 >/dev/null 2>&1 || fail "status bar override on $SIM failed"

for b in "${BOARDS[@]}"; do
  read -r look wait extra <<<"$(board "$b")"
  if [[ "$look" == ipad ]]; then
    echo "$IPAD_SKIP"
    continue
  fi
  read -r -a args <<<"$extra"
  xcrun simctl ui "$UDID" appearance "$look" >/dev/null 2>&1 || fail "$b: setting $look appearance failed"
  xcrun simctl uninstall "$UDID" "$BUNDLE" >/dev/null 2>&1
  xcrun simctl install "$UDID" "$APP" >/dev/null 2>&1 || fail "$b: installing $APP failed"
  xcrun simctl launch "$UDID" "$BUNDLE" -fixtures "$FIXTURES" -autoConfirmPairing YES -pair "$PAIR" "${args[@]}" >/dev/null 2>&1 || fail "$b: launching $BUNDLE failed"
  sleep "$wait"
  xcrun simctl io "$UDID" screenshot "$OUT/$b.png" >/dev/null 2>&1 || fail "$b: screenshot failed"
  xcrun simctl terminate "$UDID" "$BUNDLE" >/dev/null 2>&1
  echo "shot $b $OUT/$b.png"
done
