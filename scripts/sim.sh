#!/usr/bin/env bash
set -uo pipefail

IDLE="${SIM_IDLE_MIN:-120}"
SIMS="$HOME/Library/Developer/CoreSimulator/Devices"

usage() {
  cat >&2 <<EOF
usage: $0 up <cm-name> [device type]   shut down idle cm-* simulators, create <cm-name> if missing, boot it, print its udid
       $0 down <cm-name>               shut down and delete <cm-name>
       $0 reap [--dry-run]             shut down every booted cm-* simulator that is idle:
                                       no host process names it and no boot or app install in the last $IDLE min (SIM_IDLE_MIN)
EOF
  exit 2
}

udid_of() {
  xcrun simctl list devices | grep -F " $1 (" | grep -oE '[0-9A-F-]{36}' | head -1
}

check_name() {
  [[ "$1" == cm-* ]] || { echo "simulator name must start with cm-: reap and disk cleanup only look at those" >&2; exit 2; }
}

reap() {
  local dry="$1" procs line udid name dir
  procs="$(ps -axww -o command= | grep -v '/CoreSimulator/')"
  while read -r line; do
    udid="$(grep -oE '[0-9A-F-]{36}' <<<"$line")"
    name="$(sed -E 's/^ *//; s/ \([0-9A-F-]{36}\).*//' <<<"$line")"
    dir="$SIMS/$udid"
    [[ -n "$udid" && -d "$dir" ]] || continue
    grep -qF -e "$udid" -e "$name" <<<"$procs" && continue
    [[ -n "$(find "$dir/device.plist" -mmin "-$IDLE" 2>/dev/null)" ]] && continue
    [[ -n "$(find "$dir/data/Containers/Bundle/Application" -maxdepth 1 -mmin "-$IDLE" -print -quit 2>/dev/null)" ]] && continue
    if [[ "$dry" == 1 ]]; then
      echo "would shut down idle simulator $name"
    else
      xcrun simctl shutdown "$udid" 2>/dev/null && echo "shut down idle simulator $name"
    fi
  done < <(xcrun simctl list devices booted 2>/dev/null | grep -E '^ +cm-')
}

case "${1:-}" in
  up)
    [[ $# -ge 2 ]] || usage
    check_name "$2"
    reap 0 >&2
    udid="$(udid_of "$2")"
    [[ -n "$udid" ]] || udid="$(xcrun simctl create "$2" "${3:-iPhone 18 Pro}")" || exit 1
    xcrun simctl bootstatus "$udid" -b >/dev/null || exit 1
    echo "$udid"
    ;;
  down)
    [[ $# -eq 2 ]] || usage
    check_name "$2"
    udid="$(udid_of "$2")"
    [[ -n "$udid" ]] || exit 0
    xcrun simctl shutdown "$udid" 2>/dev/null
    xcrun simctl delete "$udid"
    ;;
  reap)
    [[ $# -le 2 && "${2:---dry-run}" == "--dry-run" ]] || usage
    reap "$([[ "${2:-}" == "--dry-run" ]] && echo 1 || echo 0)"
    ;;
  *) usage ;;
esac
