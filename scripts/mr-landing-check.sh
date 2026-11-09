#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
AGENT="${MR_AGENT:-fake}"
NAME="${MR_INSTANCE:-mr-landing}"
APP="${MR_APP:-$ROOT/build/DerivedData/Build/Products/Debug/Clubmate Dev.app}"
WORK="$ROOT/build/mr-landing"
LIMIT="${MR_TIMEOUT:-$([[ "$AGENT" == fake ]] && echo 120 || echo 1500)}"
SLUG=mrcheck
LINE="Touched by the $SLUG task."
DIR="/tmp/clubmate-test-$NAME"
A="$WORK/repos/pr-flow"
B="$WORK/repos/plain"

usage() {
  cat >&2 <<EOF
usage: MR_AGENT=fake|<model> $0 [run|stop]
  run   set up two scratch repos under build/mr-landing, start test instance $NAME,
        run one multi-repo task over them and check how each repo landed
  stop  stop the instance
MR_AGENT  fake (default) scripts the sessions with fakeagent; a model name (haiku, sonnet) runs real Claude
MR_KEEP=1 keeps the instance running after the check
EOF
  exit 2
}

cm() { "$ROOT/scripts/test-app.sh" cm "$NAME" -- "$@"; }
run() { cm --json command run "$1" --args "$2"; }
gitq() { git -C "$1" -c user.name=mrcheck -c user.email=mrcheck@example.com "${@:2}"; }

make_repos() {
  rm -rf "$WORK"
  mkdir -p "$WORK/repos" "$WORK/shots"
  git init -q --bare -b main "$WORK/repos/pr-flow-origin.git"
  for repo in "$A" "$B"; do
    git init -q -b main "$repo"
    echo "# $(basename "$repo")" > "$repo/README.md"
  done
  cat > "$A/CLAUDE.md" <<'EOF'
# Landing

Changes to this repo land through a pull request only: commit on your branch, push it to origin and stop there; a maintainer opens and merges the pull request. Never merge into main locally.
EOF
  local excludes="[]" dir="$WORK"
  while [[ "$dir" != / ]]; do
    dir="$(dirname "$dir")"
    for f in "$dir/CLAUDE.md" "$dir/CLAUDE.local.md" "$dir/.claude/CLAUDE.md"; do
      [[ -f "$f" ]] && excludes="$(jq -c --arg f "$f" '. + [$f]' <<<"$excludes")"
    done
  done
  for repo in "$A" "$B"; do
    mkdir -p "$repo/.claude"
    jq -n --argjson e "$excludes" '{claudeMdExcludes: $e}' > "$repo/.claude/settings.json"
    gitq "$repo" add -A
    gitq "$repo" commit -qm initial
  done
  gitq "$A" remote add origin "$WORK/repos/pr-flow-origin.git"
  gitq "$A" push -q origin main
}

fake_script() {
  local cli; cli="$(cat "$DIR/cli")"
  local env="CLUBMATE_STATE_DIR=$DIR/state CLUBMATE_RUNTIME_DIR=$DIR/run CLUBMATE_WORKSPACE_ID=$1"
  local repos; repos="$(jq -nc --arg a "$A" --arg b "$B" '[{path:$a,order:1,landsBy:"merge",source:"plan"},{path:$b,order:1,landsBy:"merge",source:"plan"}]')"
  local orchestrate child land
  orchestrate="$(cat <<EOF
[ -d .git ] || exit 0
$env '$cli' command run agents.task --args '{"slug":"$SLUG","repos":$repos}' &&
for r in '$A' '$B'; do $env '$cli' command run agents.new --args "{\"repo\":\"\$r\",\"slug\":\"$SLUG\",\"prompt\":\"MR-CHILD add the line\"}"; done
EOF
)"
  child="$(cat <<EOF
if grep -qs 'pull request' CLAUDE.md; then $env '$cli' command run agents.task --args '{"slug":"$SLUG","repos":[{"path":"$A","landsBy":"pr","source":"CLAUDE.md"}]}'; fi &&
echo '$LINE' >> README.md && git add README.md && git -c user.name=fake -c user.email=fake@example.com commit -qm 'docs: $SLUG' &&
$env '$cli' command run agents.task-report state=ready
EOF
)"
  land="$(cat <<EOF
if grep -qs 'pull request' CLAUDE.md; then git push -q origin HEAD && $env '$cli' command run agents.task-report state=prOpen pr=feat/$SLUG;
else $env '$cli' command run agents.task-report state=onBranch; fi
EOF
)"
  jq -n --arg o "$orchestrate" --arg c "$child" --arg l "$land" \
    '{prompt: [{match: "MR-TASK", run: $o}, {match: "MR-CHILD", run: $c}, {match: "[Clubmate] Land ", run: $l}]}' > "$WORK/fakeagent.json"
}

task_prompt() {
  local text="Add the line \"$LINE\" to README.md in both repos of this workspace, $A and $B. Run it as a task across repos, one child session per repo, slug $SLUG."
  [[ "$AGENT" == fake ]] && text="MR-TASK $text"
  echo "$text"
}

pick_model() {
  local session="$1" options value=""
  for _ in $(seq 1 60); do
    options="$(run agents.transcript "{\"session\":\"$session\"}" | jq -c '.configOptions // [] | map(select(.id == "model"))[0] // empty')"
    [[ -n "$options" ]] && break
    sleep 1
  done
  [[ -n "$options" ]] || { echo "no model option on $session" >&2; exit 1; }
  value="$(jq -r --arg m "$AGENT" '[.options[]? | .value // .id | select(ascii_downcase | contains($m))][0] // empty' <<<"$options")"
  [[ -n "$value" ]] || { echo "no model matching $AGENT in $options" >&2; exit 1; }
  run agents.config "$(jq -nc --arg s "$session" --arg v "$value" '{session:$s,id:"model",value:$v}')" >/dev/null
  echo "model $value"
}

task_of() { run agents.sessions "{\"session\":\"$1\"}" | jq -c '.[0]'; }

settled() { jq -e '.task.repos | length == 2 and all(.state | IN("merged","onBranch","prOpen","failed","left"))' >/dev/null <<<"$1"; }

wait_settled() {
  local owner="$1" deadline=$((SECONDS + LIMIT)) row="" quiet=0
  while ((SECONDS < deadline)); do
    row="$(task_of "$owner")"
    if settled "$row" && jq -e '.status != "running"' >/dev/null <<<"$row"; then
      ((++quiet >= 2)) && { echo "$row"; return; }
    else
      quiet=0
    fi
    if jq -e '.needsYou and (.pendingSummary.kind // "") != "land"' >/dev/null <<<"$row"; then
      echo "the orchestrator needs you: $(jq -c '.pendingSummary' <<<"$row")" >&2
    fi
    sleep 5
  done
  echo "$row"
}

check() {
  local ok=0
  if [[ "$2" == pass ]]; then echo "pass  $1"; else echo "FAIL  $1: $2"; ok=1; fi
  return $ok
}

verdict() {
  local row="$1" failed=0 a b
  a="$(jq -r --arg p "$A" '.task.repos[]? | select(.path == $p) | .state' <<<"$row")"
  b="$(jq -r --arg p "$B" '.task.repos[]? | select(.path == $p) | .state' <<<"$row")"
  [[ "$a" == prOpen || "$a" == onBranch ]] && r=pass || r="state ${a:-missing}"
  check "pr-flow stops at a pull request or its branch" "$r" || failed=1
  [[ "$(git -C "$A" rev-list --count main)" == 1 ]] && r=pass || r="main moved: $(git -C "$A" log --oneline -3 main | tr '\n' ';')"
  check "pr-flow main has no squash or merge" "$r" || failed=1
  [[ "$a" != prOpen ]] || { git -C "$WORK/repos/pr-flow-origin.git" rev-parse -q --verify "refs/heads/feat/$SLUG" >/dev/null && r=pass || r="origin has no feat/$SLUG"; check "pr-flow branch pushed for the pull request" "$r" || failed=1; }
  [[ "$b" == onBranch ]] && r=pass || r="state ${b:-missing}"
  check "plain stops on its branch" "$r" || failed=1
  [[ "$(git -C "$B" rev-list --count main)" == 1 ]] && r=pass || r="main moved: $(git -C "$B" log --oneline -3 main | tr '\n' ';')"
  check "plain main untouched" "$r" || failed=1
  for repo in "$A" "$B"; do
    git -C "$repo" show "feat/$SLUG:README.md" 2>/dev/null | grep -qF "$LINE" && r=pass || r="no line on feat/$SLUG"
    check "$(basename "$repo") feat/$SLUG has the change" "$r" || failed=1
  done
  jq -e '.task.repos[]? | select(.path == $p) | .landsBy == "pr"' --arg p "$A" >/dev/null <<<"$row" && r=pass || r="landsBy $(jq -r --arg p "$A" '.task.repos[]? | select(.path == $p) | .landsBy' <<<"$row")"
  check "pr-flow rule reads pull request" "$r" || failed=1
  return $failed
}

start() {
  [[ -d "$APP" ]] || { echo "app not found: $APP (make app, or MR_APP=<Clubmate Dev.app>)" >&2; exit 1; }
  make_repos
  "$ROOT/scripts/test-app.sh" stop "$NAME" >/dev/null 2>&1 || true
  if [[ "$AGENT" == fake ]]; then
    go build -C "$ROOT" -o "$WORK/fakeagent" ./plugins/agents/internal/fakeagent
    touch "$WORK/fakeagent.json"
    export CLUBMATE_AGENTS_COMMAND="$WORK/fakeagent" FAKEAGENT_SCRIPT="$WORK/fakeagent.json" FAKEAGENT_LOG="$WORK/fakeagent.log"
  else
    unset CLUBMATE_AGENTS_COMMAND FAKEAGENT_SCRIPT FAKEAGENT_LOG
  fi
  "$ROOT/scripts/test-app.sh" start "$NAME" "$APP" >/dev/null
  local workspace
  workspace="$(cm --json workspace open "$A" | jq -r '.id')"
  echo "$workspace" > "$DIR/workspace"
  cm workspace add-folder "$workspace" "$B" >/dev/null
  [[ "$AGENT" == fake ]] && fake_script "$workspace"
  echo "workspace $workspace, agent $AGENT"
}

main() {
  case "${1:-run}" in
    stop) "$ROOT/scripts/test-app.sh" stop "$NAME"; return ;;
    run) ;;
    *) usage ;;
  esac
  start
  local owner row failed=0
  owner="$(run agents.new '{"finish":"merge"}' | jq -r '.session')"
  echo "orchestrator $owner"
  [[ "$AGENT" == fake ]] || pick_model "$owner"
  run agents.prompt "$(jq -nc --arg s "$owner" --arg t "$(task_prompt)" '{session:$s,text:$t}')" >/dev/null
  row="$(wait_settled "$owner")"
  run agents.reveal "{\"session\":\"$owner\"}" >/dev/null
  sleep 2
  cm ui snapshot --out "$WORK/shots" >/dev/null || true
  run agents.sessions '{}' | jq -c '.[] | {id, title, status, repo, parentSession}' > "$WORK/sessions.jsonl"
  jq '{task, taskSummary, pendingSummary}' <<<"$row" > "$WORK/task.json"
  echo "sessions: $WORK/sessions.jsonl, task: $WORK/task.json, shots: $WORK/shots"
  verdict "$row" || failed=1
  [[ "${MR_KEEP:-}" == 1 ]] || "$ROOT/scripts/test-app.sh" stop "$NAME" >/dev/null
  return $failed
}

main "$@"
