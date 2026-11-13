#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
mkdir -p "$ROOT/build"
TMP="$(mktemp -d "$ROOT/build/ship-test.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT

VERSION=9.9.9
ORIGIN="$TMP/origin.git"
WORK="$TMP/repo"
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=test GIT_AUTHOR_EMAIL=test@example.invalid
export GIT_COMMITTER_NAME=test GIT_COMMITTER_EMAIL=test@example.invalid

fail() {
  echo "FAIL: $*" >&2
  [[ -f "$TMP/out" ]] && sed 's/^/  ship.sh: /' "$TMP/out" >&2
  [[ -f "$TMP/gh.log" ]] && sed 's/^/  gh: /' "$TMP/gh.log" >&2
  exit 1
}

git init -q --bare "$ORIGIN"
git init -q -b main "$WORK"
mkdir -p "$WORK/scripts/release"
cp "$ROOT/scripts/release/ship.sh" "$WORK/scripts/release/ship.sh"
git -C "$WORK" add .
git -C "$WORK" commit -q -m fixture
git -C "$WORK" remote add origin "$ORIGIN"
git -C "$WORK" push -q origin main
[[ "$(git -C "$WORK" remote get-url origin)" == "$ORIGIN" ]] || fail "fixture origin is not the local bare repo"
SHA="$(git -C "$WORK" rev-parse HEAD)"

cat >"$TMP/gh" <<EOF
#!/usr/bin/env bash
set -uo pipefail
STATE="$TMP"
ORIGIN="$ORIGIN"
TAG="v$VERSION"
EOF
cat >>"$TMP/gh" <<'EOF'
echo "$*" >>"$STATE/gh.log"
sub="$1 $2"
id="${3:-}"
shift 2
q="" wf="" branch=""
while (($#)); do
  case "$1" in
    -q) q="$2"; shift 2 ;;
    --workflow) wf="$2"; shift 2 ;;
    --branch) branch="$2"; shift 2 ;;
    *) shift ;;
  esac
done
case "$sub" in
  "run list")
    if [[ "$wf" == ci.yml ]]; then
      runs='[{"databaseId":50}]'
    elif [[ "$wf" == release.yml && "$branch" == "$TAG" ]]; then
      runs='[{"databaseId":100}]'
      if git --git-dir "$ORIGIN" rev-parse -q --verify "refs/tags/$TAG" >/dev/null; then
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
  "run watch")
    [[ "$id" == 200 ]] || exit 1
    touch "$STATE/published"
    ;;
  "run view") echo success ;;
  "release view") [[ -e "$STATE/published" ]] ;;
  *)
    echo "unexpected gh $sub" >&2
    exit 97
    ;;
esac
EOF
cat >"$TMP/clubmate" <<EOF
#!/bin/bash
echo "\$*" >>"$TMP/notify.log"
EOF
chmod +x "$TMP/gh" "$TMP/clubmate"

status=0
SHIP_GH="$TMP/gh" SHIP_CLUBMATE="$TMP/clubmate" SHIP_POLL_SECONDS=0 \
  "$WORK/scripts/release/ship.sh" "$VERSION" "$SHA" ws_test >"$TMP/out" 2>&1 || status=$?

((status == 0)) || fail "ship.sh exited $status"
grep -qx "release run 200" "$TMP/out" || fail "ship.sh did not pick the new release run 200"
grep -q "^run watch 100 " "$TMP/gh.log" && fail "ship.sh watched the stale release run 100"
git --git-dir "$ORIGIN" rev-parse -q --verify "refs/tags/v$VERSION" >/dev/null || fail "tag v$VERSION was deleted from origin"
grep -q -- "--kind done .*v$VERSION published (release run 200)" "$TMP/notify.log" || fail "no done notification for run 200"
echo "ok: ship.sh ignores the stale release run and watches the new one"
