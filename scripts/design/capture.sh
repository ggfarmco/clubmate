#!/usr/bin/env bash
set -uo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

usage() {
  cat >&2 <<EOF
usage: $0 [--plan] [--dry-run]
  --plan      write the boards whose UI changed since the last run (all on Sundays) to <run dir>/boards and stop
  --dry-run   plan, then print what a capture would do; build and start nothing
Without flags: capture the boards in <run dir>/boards (planned first when missing) and write <run dir>/manifest.json.
Run dir: <main checkout>/.worktrees/design-check/<CLUBMATE_GRAPH_RUN, else DESIGN_CHECK_RUN, else manual-<date>>.
CLUBMATE_GRAPH_INPUT=all captures every board; DESIGN_CHECK_REV captures another revision than HEAD. Exit 3: nothing to capture.
EOF
  exit 2
}

PLAN=0
DRY=0
for a in "$@"; do
  case "$a" in
    --plan) PLAN=1 ;;
    --dry-run) DRY=1 ;;
    *) usage ;;
  esac
done

MAIN="$(main_checkout)"
cd "$MAIN" || exit 1
KEY="$(run_key)"
export DESIGN_CHECK_RUN="$KEY"
DIR="$(run_dir)"
NOTES="${DESIGN_CHECK_NOTES:-$(cd "$MAIN/.." && pwd)/clubmate-notes}"
INPUT="${CLUBMATE_GRAPH_INPUT:-}"
REV="$(git rev-parse --verify -q "${DESIGN_CHECK_REV:-HEAD}^{commit}")" || fail "no revision ${DESIGN_CHECK_REV:-HEAD}"
strip_clubmate_env

boards_for() {
  case "$1" in
    client/Clubmate/Theme/*|pkg/sdui/*|client/ClubmateProtocol/*|client/ClubmateDocument/*) echo ALL ;;
    client/Companion/*|client/CompanionKit/*) echo PHONE ;;
    client/Clubmate/SDUI/Agents/*|plugins/agents/view*.go) echo Agents-1280 Agents-640 Mac-AlwaysAllow x-Failed-Session-1280 Code-1280 Code-640 Mac-ChatGraphStep ;;
    client/Clubmate/Windows/CommandPaletteView.swift) echo x-Palette ;;
    client/Clubmate/Windows/FuzzyPickerView.swift) echo x-Palette x-Worktree-Picker ;;
    client/Clubmate/Windows/SwitcherGrid.swift) echo x-Switcher ;;
    client/Clubmate/Windows/InboxView.swift) echo x-Inbox-popover ;;
    client/Clubmate/App/Settings*) echo Mac-Settings-Remote ;;
    client/Clubmate/*|client/ClubmateKit/*) echo MAC ;;
    plugins/backlog/view*.go) echo Backlog-1280 Backlog-640 ;;
    plugins/cron/view*.go) echo Cron-1280 ;;
    plugins/triggers/view*.go) echo Triggers-640 x-Triggers-1280 ;;
    plugins/graphs/view*.go) echo Graphs-1280 Graphs-640 Mac-ChatGraphStep ;;
    plugins/processes/view*.go) echo Processes-1280 Processes-640 ;;
    plugins/editor-nvim/view*.go) echo Code-1280 Code-640 ;;
    plugins/remote/view*.go) echo Mac-Settings-Remote x-Title-Dot-1280 ;;
    plugins/*/view*.go) echo MAC ;;
  esac
}

last_head() {
  local m h
  for m in $(ls -r "$NOTES"/design-check/*/manifest.json 2>/dev/null); do
    h="$(jq -r '.head // empty' "$m" 2>/dev/null)"
    if [[ -n "$h" ]] && git merge-base --is-ancestor "$h" "$REV" 2>/dev/null; then
      printf '%s' "$h"
      return 0
    fi
  done
  return 1
}

last_report_date() {
  ls "$NOTES"/design-check/ 2>/dev/null | grep -E '^[0-9]{4}-[0-9]{2}-[0-9]{2}\.md$' | sort -r | head -1 | cut -c1-10
}

plan() {
  local day mode since="" base range files f b all=0 picked=""
  day="$(date -j -f %F "$(today)" +%u 2>/dev/null)" || fail "DESIGN_CHECK_TODAY '$(today)' is not YYYY-MM-DD"
  if [[ "$INPUT" == "all" ]]; then
    mode="all (input)"
    all=1
  elif [[ "$day" == 7 ]]; then
    mode="all (Sunday)"
    all=1
  else
    mode="changed"
  fi
  if base="$(last_head)"; then
    range="$base..${REV:0:8}"
    files="$(git log --format= --name-only "$base..$REV" | sort -u)"
  else
    since="$(last_report_date)"
    since="${since:+$since 00:00}"
    since="${since:-24 hours ago}"
    range="since $since"
    files="$(git log --since="$since" --format= --name-only "$REV" | sort -u)"
  fi
  mkdir -p "$DIR" || fail "cannot create $DIR"
  echo "mode: $mode"
  echo "range: $range"
  if [[ "$all" == 0 ]]; then
    for f in $files; do
      b="$(boards_for "$f")"
      [[ -n "$b" ]] || continue
      echo "changed: $f -> $b"
      picked="$picked $b"
    done
    case " $picked " in
      *" ALL "*) all=1 ;;
    esac
  fi
  if [[ "$all" == 1 ]]; then
    picked="$MAC_BOARDS $PHONE_BOARDS"
  else
    case " $picked " in *" MAC "*) picked="$picked $MAC_BOARDS" ;; esac
    case " $picked " in *" PHONE "*) picked="$picked $PHONE_BOARDS" ;; esac
  fi
  : > "$DIR/boards"
  for b in $MAC_BOARDS $PHONE_BOARDS; do
    case " $picked " in *" $b "*) echo "$b" >> "$DIR/boards" ;; esac
  done
  printf '%s\n' "${base:-}" > "$DIR/base"
  echo "boards: $(tr '\n' ' ' < "$DIR/boards")"
  echo "wrote $DIR/boards"
}

read_boards() {
  local b
  BOARDS=""
  while IFS= read -r b; do
    b="${b%%#*}"
    b="$(printf '%s' "$b" | tr -d '[:space:]')"
    [[ -n "$b" ]] || continue
    known_board "$b" || fail "$DIR/boards: unknown board $b"
    BOARDS="$BOARDS $b"
  done < "$DIR/boards"
}

wanted() {
  case " $BOARDS " in
    *" $1 "*) return 0 ;;
  esac
  return 1
}

any_wanted() {
  local b
  for b in "$@"; do
    wanted "$b" && return 0
  done
  return 1
}

if [[ "$PLAN" == 1 ]]; then
  plan
  read_boards
  [[ -n "$BOARDS" ]] || { echo "nothing to capture"; exit 3; }
  exit 0
fi
[[ -f "$DIR/boards" ]] || plan
read_boards
if [[ -z "$BOARDS" ]]; then
  echo "nothing to capture"
  exit 3
fi

MAC=""
PHONE=""
for b in $BOARDS; do
  if mac_board "$b"; then MAC="$MAC $b"; else PHONE="$PHONE $b"; fi
done

TREE="$DIR/tree"
SHOTS="$DIR/shots"
LOGS="$DIR/logs"
NAME="$(instance_name)"
SIM="$(sim_name)"
HEAD_SHA="$REV"

if [[ "$DRY" == 1 ]]; then
  echo "would capture from $HEAD_SHA in $TREE"
  [[ -z "$MAC" ]] || echo "would make app and start test instance $NAME for:$MAC"
  [[ -z "$PHONE" ]] || echo "would capture on simulator $SIM:$PHONE"
  echo "would write $DIR/manifest.json"
  exit 0
fi

for f in client/GhosttyKit.xcframework client/Clubmate/Resources/ghostty client/Clubmate/Resources/terminfo .ghostty-build-stamp; do
  [[ -e "$MAIN/$f" ]] || fail "$MAIN/$f is missing: run make ghostty in the main checkout"
done

rm -rf "$SHOTS" "$LOGS" "$DIR/raw" "$DIR/shot.lst" "$DIR/skip.lst" "$DIR/manifest.json"
mkdir -p "$SHOTS" "$LOGS" "$DIR/raw"
: > "$DIR/shot.lst"
: > "$DIR/skip.lst"

if [[ -e "$TREE" ]]; then
  git worktree remove --force "$TREE" >/dev/null 2>&1 || rm -rf "$TREE"
  git worktree prune
fi
git worktree add -q --detach "$TREE" "$REV" || fail "git worktree add $TREE failed"
mkdir -p "$TREE/client/Clubmate/Resources"
cp -R "$MAIN/client/GhosttyKit.xcframework" "$TREE/client/" &&
  cp -R "$MAIN/client/Clubmate/Resources/ghostty" "$MAIN/client/Clubmate/Resources/terminfo" "$TREE/client/Clubmate/Resources/" &&
  cp "$MAIN/.ghostty-build-stamp" "$TREE/" || fail "copying the ghostty build from $MAIN failed"
echo "tree $TREE at $HEAD_SHA"

shot() {
  printf '%s\t%s\n' "$1" "$2" >> "$DIR/shot.lst"
  echo "shot $1 $2"
}

skip() {
  printf '%s\t%s\n' "$1" "$2" >> "$DIR/skip.lst"
  echo "skipped $1 $2"
}

write_manifest() {
  jq -n \
    --arg run "$KEY" --arg head "$HEAD_SHA" --arg base "$(cat "$DIR/base" 2>/dev/null)" \
    --arg tree "$TREE" --arg dir "$DIR" --arg at "$(date -u +%FT%TZ)" \
    --rawfile shots "$DIR/shot.lst" --rawfile skipped "$DIR/skip.lst" \
    'def tsv: split("\n") | map(select(length > 0) | split("\t") | {key: .[0], value: .[1]}) | from_entries;
     {run: $run, head: $head, base: $base, capturedAt: $at, dir: $dir, tree: $tree, shots: ($shots | tsv), skipped: ($skipped | tsv)}' \
    > "$DIR/manifest.json"
}

. "$HERE/capture-mac.sh"

STATUS=0
if [[ -n "$MAC" ]]; then
  capture_mac || STATUS=1
fi
if [[ -n "$PHONE" ]]; then
  if "$HERE/capture-phone.sh" --tree "$TREE" --out "$SHOTS" --sim "$SIM" $PHONE > "$LOGS/phone.out" 2> "$LOGS/phone.err"; then
    :
  else
    STATUS=1
    echo "phone capture failed: $(tail -3 "$LOGS/phone.err")"
    for b in $PHONE; do
      grep -q "^\(shot\|skipped\) $b " "$LOGS/phone.out" || skip "$b" "phone capture failed: $(tail -1 "$LOGS/phone.err")"
    done
  fi
  while read -r kind b rest; do
    case "$kind" in
      shot) shot "$b" "$rest" ;;
      skipped) skip "$b" "$rest" ;;
    esac
  done < "$LOGS/phone.out"
fi

write_manifest
COUNT="$(jq '.shots | length' "$DIR/manifest.json")"
SKIPPED="$(jq '.skipped | length' "$DIR/manifest.json")"
echo "manifest $DIR/manifest.json: $COUNT shots, $SKIPPED skipped"
[[ "$COUNT" -gt 0 ]] || fail "no board was captured; logs in $LOGS"
[[ "$STATUS" == 0 ]] || echo "some boards failed; see skipped in the manifest and $LOGS"
exit 0
