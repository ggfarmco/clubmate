APP="$TREE/build/DerivedData/Build/Products/Debug/Clubmate Dev.app"
TDIR="/tmp/clubmate-test-$NAME"
FAKE="$DIR/fakeagent"
FAKE_SCRIPT="$DIR/fakeagent-script.json"

tapp() {
  env -i HOME="$HOME" USER="$USER" LOGNAME="${LOGNAME:-$USER}" PATH="/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" TMPDIR="${TMPDIR:-/tmp}" \
    CLUBMATE_AGENTS_COMMAND="$FAKE" FAKEAGENT_SCRIPT="$FAKE_SCRIPT" "$TREE/scripts/test-app.sh" "$@"
}

cm() {
  tapp cm "$NAME" -- "$@"
}

ws() {
  cat "$TDIR/workspace"
}

focus() {
  cm command run ui.focus section="$1" ${2:+slot="$2"} >/dev/null
  sleep "${3:-1.5}"
}

select_in() {
  cm ui event --workspace "$(ws)" --slot "$1" --node "$2" --action "${4:-select}" --value "\"$3\"" >/dev/null
  sleep 1.5
}

snap() {
  local board="$1" size="${2:-1280x800}" kind="${3:-window}" out="$DIR/raw/$1" png
  rm -rf "$out"
  mkdir -p "$out"
  if ! cm --json ui snapshot --workspace "$(ws)" --size "$size" --out "$out" > "$out/result.json" 2> "$out/err"; then
    skip "$board" "ui snapshot failed: $(tail -1 "$out/err")"
    return 1
  fi
  if [[ "$kind" == other ]]; then
    png="$(jq -r '[.windows[] | select(.kind != "window") | .png] | last // empty' "$out/result.json")"
  else
    png="$(jq -r --arg k "$kind" '[.windows[] | select(.kind == $k) | .png] | last // empty' "$out/result.json")"
  fi
  if [[ -z "$png" || ! -f "$png" ]]; then
    skip "$board" "no $kind window in the snapshot ($(jq -r '[.windows[].kind] | join(",")' "$out/result.json"))"
    return 1
  fi
  cp "$png" "$SHOTS/$board.png"
  shot "$board" "$SHOTS/$board.png"
}

relaunch() {
  local theme="$1"
  shift
  printf 'theme = %s\n' "$theme" > "$TDIR/state/ghostty.conf"
  tapp relaunch "$NAME" "$APP" -- "$@" >> "$LOGS/test-app.log" 2>&1 || { echo "relaunch with $theme failed: $(tail -1 "$LOGS/test-app.log")"; return 1; }
  sleep 3
}

json_field() {
  jq -r "$1 // empty" 2>/dev/null
}

seed() {
  local t ti pr st id
  while IFS='|' read -r ti pr st; do
    case "$st" in
      idea|todo) cm command run backlog.add title="$ti" priority="$pr" status="$st" >/dev/null || return 1 ;;
      *)
        id="$(cm --json command run backlog.add title="$ti" priority="$pr" status=todo | json_field .id)"
        [[ -n "$id" ]] || return 1
        if [[ "$st" == in_progress ]]; then
          cm command run backlog.claim id="$id" >/dev/null || return 1
        else
          cm command run backlog.update id="$id" status="$st" >/dev/null || return 1
        fi
        ;;
    esac
  done <<EOF
Rate-limit notifications per workspace|high|in_progress
Flaky switcher test on CI|high|todo
Docs: transcript folding on the phone|normal|todo
Inbox groups by workspace|normal|idea
Trim the release bundle|low|idea
Processes: sort by memory|normal|todo
Remove the old pairing code path|low|done
EOF
  cm command run cron.add name="Nightly dependency audit" command=agents.new cron="0 3 * * *" owner=none \
    args='{"title":"Dependency audit","prompt":"Audit go.mod and Package.resolved for outdated or vulnerable dependencies and open a backlog task per finding."}' >/dev/null || return 1
  cm command run cron.add name="Poll CI for main" command=backlog.list every=2h owner=none >/dev/null || return 1
  id="$(cm --json command run cron.add name="Weekly release notes" command=agents.new cron="0 9 * * 1" owner=none args='{"title":"Release notes","prompt":"Draft release notes"}' | json_field .id)"
  [[ -n "$id" ]] && cm command run cron.pause id="$id" >/dev/null
  cm command run triggers.add name="Review finished sessions" on=agents.turn match='{"status":"done"}' command=notify.post args='{"message":"{{payload.title}} finished"}' owner=none >/dev/null || return 1
  cm command run triggers.add name="Tidy done → release" on=event.tidy.done command=agents.new args='{"title":"Release","prompt":"Cut a release"}' owner=none >/dev/null || return 1
  cm command run terminals.new >/dev/null
  cm command run terminals.new >/dev/null
  cm notify "Build finished on main" >/dev/null
  cm notify "Nightly tidy merged 2 commits" >/dev/null
  for t in "Bump golang.org/x/net to v0.31" "Migrate the store to the new schema" "Port cron to the shared scheduler" "Fix flaky switcher test"; do
    id="$(cm --json agent spawn "$t" | json_field .session)"
    [[ "$t" == Migrate* ]] && MIGRATE="$id"
    sleep 2
  done
  mkdir -p "$TREE/.clubmate/graphs"
  cp "$TREE/plugins/graphs/testdata/.clubmate/graphs/review-loop.json" "$TREE/.clubmate/graphs/"
  cm --json command run graphs.run graph=review-loop input="fix the flaky switcher test" > "$DIR/raw/graph-run.json" || return 1
  REVIEWER="$(json_field '.nodes[] | select(.id == "reviewer") | .session' < "$DIR/raw/graph-run.json")"
  GRAPH_TRIGGER="$(json_field '.edges[0].trigger' < "$DIR/raw/graph-run.json")"
  sleep 3
  RATE="$(cm --json agent spawn "Rate-limit notifications per workspace" | json_field .session)"
  sleep 8
  [[ -n "$RATE" && -n "$MIGRATE" && -n "$REVIEWER" && -n "$GRAPH_TRIGGER" ]] || { echo "seed: missing ids rate=$RATE migrate=$MIGRATE reviewer=$REVIEWER trigger=$GRAPH_TRIGGER"; return 1; }
}

close_pickers() {
  local nvim s
  nvim="$(find "$APP" -name nvim -type f -perm +111 | head -1)"
  [[ -n "$nvim" ]] || return 0
  for s in "$TDIR"/run/p/editor-nvim/*.sock; do
    [[ -S "$s" ]] || continue
    "$nvim" -i NONE --clean --headless --server "$s" --remote-expr 'luaeval("(function() for _,p in ipairs(Snacks.picker.get()) do p:close() end return 1 end)()")' >/dev/null 2>&1 || true
  done
}

open_code() {
  focus editor-nvim "" 3
  close_pickers
  sleep 1
  cm command run editor-nvim.open path="$TREE/internal/notify/notify.go" line=40 >/dev/null
  sleep 2
  focus editor-nvim editor-nvim.sidebar 2
}

agents_card() {
  focus agents
  select_in agents.sidebar sessions "$RATE"
  cm ui present "card:$RATE/tool/e1" --workspace "$(ws)" >/dev/null
  sleep 1.5
}

terminal_pid() {
  cm --json ui dump --workspace "$(ws)" | jq -r '
    [.[] | select(.slot == "processes.main") | .. | objects | select(.id == "processes") | .props.rows[]] as $r
    | ([range(0; $r | length) | select($r[.].group == true and ($r[.].cells[0].text // "" | test("^Terminal")))] | last) as $i
    | if $i != null and $i + 1 < ($r | length) then $r[$i + 1].id else ([$r[] | select(.group != true) | .id] | last) end // empty'
}

present_snap() {
  local target="$1" board="$2" kind="$3"
  cm ui present "$target" --workspace "$(ws)" >/dev/null
  sleep 1.5
  snap "$board" 1280x800 "$kind"
  cm ui present "$target" --hide --workspace "$(ws)" >/dev/null 2>&1
  sleep 0.5
}

pass_dark() {
  any_wanted Agents-1280 Mac-AlwaysAllow x-Failed-Session-1280 Code-1280 Mac-ChatGraphStep Backlog-1280 Cron-1280 x-Triggers-1280 Graphs-1280 Processes-1280 x-Palette x-Switcher x-Inbox-popover x-Worktree-Picker || return 0
  relaunch "Kanagawa Wave" -AppleInterfaceStyle Dark || return 1
  if any_wanted Agents-1280 Mac-AlwaysAllow; then
    agents_card
    wanted Agents-1280 && snap Agents-1280
    if wanted Mac-AlwaysAllow; then
      cm ui key opt+2 --workspace "$(ws)" >/dev/null
      sleep 1.5
      snap Mac-AlwaysAllow 1280x800 other
      cm ui key esc --workspace "$(ws)" >/dev/null
      sleep 1
    fi
  fi
  if wanted x-Failed-Session-1280; then
    focus agents
    select_in agents.sidebar sessions "$MIGRATE"
    snap x-Failed-Session-1280
  fi
  if any_wanted Code-1280 Mac-ChatGraphStep; then
    open_code
    wanted Code-1280 && snap Code-1280
    if wanted Mac-ChatGraphStep; then
      select_in editor-nvim.sidebar code/session "$REVIEWER" codeSelect
      snap Mac-ChatGraphStep && sips -c 1520 762 --cropOffset 80 1798 "$SHOTS/Mac-ChatGraphStep.png" >/dev/null 2>&1
      select_in editor-nvim.sidebar code/session "$RATE" codeSelect
    fi
  fi
  if wanted Backlog-1280; then
    focus backlog
    select_in backlog.sidebar tasks 2
    snap Backlog-1280
  fi
  if wanted Cron-1280; then
    focus cron
    snap Cron-1280
  fi
  if wanted x-Triggers-1280; then
    focus triggers
    select_in triggers.sidebar triggers "$GRAPH_TRIGGER"
    snap x-Triggers-1280
  fi
  if wanted Graphs-1280; then
    focus graphs
    snap Graphs-1280
  fi
  if wanted Processes-1280; then
    focus processes "" 2.5
    snap Processes-1280
  fi
  focus agents
  wanted x-Palette && present_snap palette x-Palette window
  wanted x-Switcher && present_snap switcher x-Switcher window
  wanted x-Inbox-popover && present_snap inbox x-Inbox-popover other
  wanted x-Worktree-Picker && present_snap worktree x-Worktree-Picker other
  return 0
}

pass_light() {
  local pid
  any_wanted Agents-640 Code-640 Backlog-640 Triggers-640 Graphs-640 Processes-640 x-Approval-Sheet || return 0
  relaunch "Catppuccin Latte" -NSRequiresAquaSystemAppearance YES || return 1
  if wanted Agents-640; then
    agents_card
    snap Agents-640 640x800
  fi
  if wanted Code-640; then
    open_code
    snap Code-640 640x800
  fi
  if wanted Backlog-640; then
    focus backlog
    select_in backlog.sidebar tasks 2
    snap Backlog-640 640x800
  fi
  if wanted Triggers-640; then
    focus triggers
    select_in triggers.sidebar triggers "$GRAPH_TRIGGER"
    snap Triggers-640 640x800
  fi
  if wanted Graphs-640; then
    focus graphs
    snap Graphs-640 640x800
  fi
  if wanted Processes-640; then
    focus processes "" 2.5
    pid="$(terminal_pid)"
    [[ -n "$pid" ]] && select_in processes.main processes "$pid" select-pid
    snap Processes-640 640x800
  fi
  if wanted x-Approval-Sheet; then
    focus agents
    present_snap approval x-Approval-Sheet other
  fi
  return 0
}

pass_settings() {
  local png
  any_wanted Mac-Settings-Remote x-Title-Dot-1280 || return 0
  relaunch "Kanagawa Wave" -NSRequiresAquaSystemAppearance YES --settings-tab remote --remote-fixture || return 1
  focus agents
  cm ui present settings --workspace "$(ws)" >/dev/null
  sleep 2
  wanted Mac-Settings-Remote && snap Mac-Settings-Remote 1280x800 settings
  if wanted x-Title-Dot-1280; then
    png="$(jq -r '[.windows[] | select(.kind == "window") | .png] | last // empty' "$DIR/raw/Mac-Settings-Remote/result.json" 2>/dev/null)"
    if [[ -n "$png" && -f "$png" ]]; then
      cp "$png" "$SHOTS/x-Title-Dot-1280.png"
      shot x-Title-Dot-1280 "$SHOTS/x-Title-Dot-1280.png"
    else
      snap x-Title-Dot-1280
    fi
  fi
  cm ui present settings --hide --workspace "$(ws)" >/dev/null 2>&1
  return 0
}

stop_instance() {
  [[ -d "$TDIR" ]] && tapp stop "$NAME" >> "$LOGS/test-app.log" 2>&1
  return 0
}

mac_fail() {
  local b
  echo "mac capture failed: $*"
  for b in $MAC; do
    grep -q "^$b	" "$DIR/shot.lst" "$DIR/skip.lst" 2>/dev/null || skip "$b" "mac capture failed: $*"
  done
  stop_instance
  return 1
}

capture_mac() {
  echo "make app in $TREE (log $LOGS/make-app.log)"
  if ! (cd "$TREE" && make app) > "$LOGS/make-app.log" 2>&1; then
    tail -20 "$LOGS/make-app.log"
    mac_fail "make app failed"
    return 1
  fi
  [[ -d "$APP" ]] || { mac_fail "make app built no $APP"; return 1; }
  (cd "$TREE" && go build -o "$FAKE" ./plugins/agents/internal/fakeagent) > "$LOGS/fakeagent.log" 2>&1 || { mac_fail "building the fake agent failed"; return 1; }
  sed -E "s#\"/[^\"]+/((internal|plugins|client|pkg|cmd|docs|scripts)/[^\"]*)\"#\"$TREE/\\1\"#g" "$TREE/design/check/mac/fakeagent-script.json" > "$FAKE_SCRIPT" || { mac_fail "rewriting the fake agent script failed"; return 1; }
  [[ ! -d "$TDIR" ]] || stop_instance
  trap stop_instance EXIT
  tapp start "$NAME" "$APP" >> "$LOGS/test-app.log" 2>&1 || { mac_fail "test instance $NAME did not start: $(tail -1 "$LOGS/test-app.log")"; return 1; }
  seed >> "$LOGS/seed.log" 2>&1 || { mac_fail "seeding failed: $(tail -1 "$LOGS/seed.log")"; return 1; }
  pass_dark || { mac_fail "dark pass"; return 1; }
  pass_light || { mac_fail "light pass"; return 1; }
  pass_settings || { mac_fail "settings pass"; return 1; }
  stop_instance
  trap - EXIT
  local b
  for b in $MAC; do
    grep -q "^$b	" "$DIR/shot.lst" "$DIR/skip.lst" || skip "$b" "not captured"
  done
  return 0
}
