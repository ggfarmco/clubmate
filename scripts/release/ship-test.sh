#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$ROOT/build"
TMP="$(mktemp -d "$ROOT/build/ship-test.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

VERSION=9.9.9
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid

cat >"$TMP/gh" <<'EOF'
#!/usr/bin/env bash
set -uo pipefail
echo "$*" >>"$STATE/gh.log"
sub="$1 $2"
id="${3:-}"
shift 2
q="" wf="" branch="" commit="" status=""
while (($#)); do
  case "$1" in
    -q) q="$2"; shift 2 ;;
    --workflow) wf="$2"; shift 2 ;;
    --branch) branch="$2"; shift 2 ;;
    --commit) commit="$2"; shift 2 ;;
    --status) status="$2"; shift 2 ;;
    *) shift ;;
  esac
done
tagged() { git --git-dir "$ORIGIN" rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null; }
case "$sub" in
  "run list")
    if [[ "$wf" == ci.yml && -z "$commit" ]]; then
      runs="[{\"headSha\":\"$GREEN\"}]"
    elif [[ "$wf" == ci.yml && "$commit" == "$GREEN" ]]; then
      runs='[{"databaseId":40}]'
    elif [[ "$wf" == ci.yml ]]; then
      runs='[{"databaseId":50}]'
    elif [[ "$wf" == release.yml && "$branch" == "v$VERSION" ]]; then
      runs='[{"databaseId":100}]'
      if tagged; then
        n=$(($(cat "$STATE/polls" 2>/dev/null || echo 0) + 1))
        echo "$n" >"$STATE/polls"
        ((n > 2)) && runs='[{"databaseId":200},{"databaseId":100}]'
      fi
    else
      echo "unexpected run list: wf=$wf branch=$branch" >&2
      exit 97
    fi
    jq -r "($q) | values" <<<"$runs"
    ;;
  "run view")
    if [[ "$id" == 50 ]]; then
      n=$(($(cat "$STATE/views" 2>/dev/null || echo 0) + 1))
      echo "$n" >"$STATE/views"
      if ((n <= 2)); then
        tagged && touch "$STATE/tagged-while-queued"
        run='{"status":"queued","conclusion":""}'
      else
        run="$CI_LATER"
      fi
    else
      run='{"status":"completed","conclusion":"success"}'
    fi
    jq -r "$q" <<<"$run"
    ;;
  "api repos/ggfarmco/clubmate/actions/runs/40/jobs") jq -r "$q" <<<'{"jobs":[{"id":7}]}' ;;
  "api repos/ggfarmco/clubmate/check-runs/7/annotations") jq -r "$q" <<<'[{"title":"Flaky test","message":"TestRace"}]' ;;
  "run watch")
    [[ "$id" == 200 ]] || exit 1
    touch "$STATE/published"
    ;;
  "release view") [[ -e "$STATE/published" ]] ;;
  *)
    echo "unexpected gh $sub" >&2
    exit 97
    ;;
esac
EOF
chmod +x "$TMP/gh"

ship() {
  local name="$1" ci_later="$2" status=0
  STATE="$TMP/$name"
  ORIGIN="$STATE/origin.git"
  WORK="$STATE/repo"
  mkdir -p "$STATE"
  git init -q --bare "$ORIGIN"
  git init -q -b main "$WORK"
  mkdir -p "$WORK/scripts/release"
  cp "$ROOT/scripts/release/ship.sh" "$ROOT/scripts/release/green-sha.sh" "$ROOT/scripts/release/flaky-backlog.sh" "$WORK/scripts/release/"
  git -C "$WORK" add .
  git -C "$WORK" commit -q -m green
  GREEN="$(git -C "$WORK" rev-parse HEAD)"
  git -C "$WORK" commit -q --allow-empty -m pending
  SHA="$(git -C "$WORK" rev-parse HEAD)"
  git -C "$WORK" remote add origin "$ORIGIN"
  git -C "$WORK" push -q origin main
  [[ "$(git -C "$WORK" remote get-url origin)" == "$ORIGIN" ]] || fail "fixture origin is not the local bare repo"
  printf '#!/bin/bash\necho "$*" >>"%s/notify.log"\n' "$STATE" >"$STATE/clubmate"
  chmod +x "$STATE/clubmate"
  export STATE ORIGIN VERSION GREEN CI_LATER="$ci_later"
  SHIP_GH="$TMP/gh" SHIP_CLUBMATE="$STATE/clubmate" SHIP_POLL_SECONDS=0 \
    "$WORK/scripts/release/ship.sh" "$VERSION" "$SHA" ws_test >"$STATE/out" 2>&1 || status=$?
  ((status == 0)) || fail "ship.sh exited $status"
  (($(cat "$STATE/views" 2>/dev/null || echo 0) >= 3)) || fail "ship.sh did not wait for queued ci run 50 to start"
  [[ -e "$STATE/tagged-while-queued" ]] && fail "ship.sh pushed the tag while ci run 50 was queued"
  grep -qx "release run 200" "$STATE/out" || fail "ship.sh did not pick the new release run 200"
  grep -q "^run watch 100 " "$STATE/gh.log" && fail "ship.sh watched the stale release run 100"
  grep -q -- "--kind done .*v$VERSION published (release run 200)" "$STATE/notify.log" || fail "no done notification for run 200"
  return 0
}

fail() {
  echo "FAIL: $*" >&2
  [[ -f "$STATE/out" ]] && sed 's/^/  ship.sh: /' "$STATE/out" >&2
  [[ -f "$STATE/gh.log" ]] && sed 's/^/  gh: /' "$STATE/gh.log" >&2
  exit 1
}

tag_sha() { git --git-dir "$ORIGIN" rev-parse -q --verify "refs/tags/v$VERSION^{commit}"; }

ship started '{"status":"in_progress","conclusion":""}'
[[ "$(tag_sha)" == "$SHA" ]] || fail "tag v$VERSION is not on $SHA"
echo "ok: ship.sh tags only after the ci run started and watches the new release run"

ship replaced '{"status":"completed","conclusion":"cancelled"}'
[[ "$(tag_sha)" == "$GREEN" ]] || fail "tag v$VERSION is not on the green commit $GREEN"
for want in "backlog.add" '"title": "Flaky tests in ci run 40 ' "TestRace"; do
  grep -qF -- "$want" "$STATE/notify.log" || fail "the flaky tests of the green commit's ci run 40 were not filed: no $want"
done
echo "ok: ship.sh ships the newest green commit when the ci run was replaced"
