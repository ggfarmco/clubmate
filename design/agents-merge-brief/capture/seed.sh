#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "$0")/../../.." && pwd)"
B="$REPO/build/agents-merge-brief"
S="$B/shop"
W="$S/.worktrees"
NAME="${NAME:-agents-merge}"
TDIR="/tmp/clubmate-test-$NAME"
APP="${APP:-$REPO/build/DerivedData/Build/Products/Debug/Clubmate Dev.app}"
HERE="$(cd "$(dirname "$0")" && pwd)"

tapp() {
  env -i HOME="$HOME" USER="$USER" LOGNAME="${LOGNAME:-$USER}" PATH="/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin" TMPDIR="${TMPDIR:-/tmp}" \
    CLUBMATE_AGENTS_COMMAND="$B/fakeagent" FAKEAGENT_SCRIPT="$B/fakeagent-script.json" "$S/scripts/test-app.sh" "$@"
}
cm() { tapp cm "$NAME" -- "$@"; }
ws() { cat "$TDIR/workspace"; }
rpc() { python3 -I "$HERE/rpc.py" "$TDIR/run/core.sock" "$@"; }
run_as() { rpc command.run "{\"id\":\"$2\",\"workspaceId\":\"$(ws)\",\"caller\":\"$1\",\"args\":$3}"; }
sessions() { cm --json command run agents.sessions; }
reviewer_of() { sessions | jq -r --arg l "$1" '[.[] | select(.gateFor.lander == $l and (.gateFor.done | not))] | last | .id // empty'; }
tree_of() { sessions | jq -r --arg r "$1" '.[] | select(.id == $r) | .gateFor.tree'; }
land() { run_as "$1" git.land "{\"path\":\"$W/$2\",\"message\":\"$3\"}" | jq -r '.message // .commit // .' || true; }
verdict() {
  local reviewer="$1" outcome="$2" findings="${3:-[]}"
  local deferred="${4:-[]}"
  run_as "$reviewer" agents.land-verdict "{\"tree\":\"$(tree_of "$reviewer")\",\"outcome\":\"$outcome\",\"findings\":$findings,\"deferred\":$deferred}" >/dev/null
  sleep 2
}
spawn_b() { cm --json agent spawn --branch "$1" --title "$2" "$2" | jq -r .session; }
commit_in() { (cd "$W/$1" && mkdir -p "$(dirname "$2")" && printf '%b' "$3" >> "$2" && git add -A && git commit -qm "$4"); }

rm -rf "$S"
mkdir -p "$B" "$S/scripts" "$S/internal/notify" "$S/docs" "$S/.clubmate/graphs"
go build -C "$REPO" -o "$B/fakeagent" ./plugins/agents/internal/fakeagent
sed -E "s#\"/[^\"]+/((internal|plugins|client|pkg|cmd|docs|scripts)/[^\"]*)\"#\"$S/\\1\"#g" "$HERE/fakeagent-script.json" > "$B/fakeagent-script.json"
cp "$REPO/scripts/test-app.sh" "$S/scripts/"
cp "$REPO/plugins/graphs/testdata/.clubmate/graphs/review-loop.json" "$S/.clubmate/graphs/"
cd "$S"
git init -q -b main
git config user.name test
git config user.email test@example.invalid
printf 'module example.com/shop\n\ngo 1.26\n' > go.mod
printf 'package notify\n\ntype Bus struct{ subs []func(string) }\n' > internal/notify/notify.go
printf '# Agents\n\nHow sessions work.\n' > docs/agents.md
printf '# shop\n' > README.md
git add -A
git commit -qm initial
printf '.worktrees/\n' >> .git/info/exclude

tapp start "$NAME" "$APP" -- "$@"

cm command run agents.gate-add repo="$S" rounds=2 >/dev/null
cm command run cron.add name="Nightly dependency audit" command=agents.new cron="0 3 * * *" owner=none args='{"title":"Dependency audit","prompt":"Audit go.mod for outdated dependencies."}' >/dev/null
cm command run cron.add name="Poll CI for main" command=backlog.list every=2h owner=none >/dev/null
cm command run triggers.add name="Review finished sessions" on=agents.turn match='{"status":"done"}' command=notify.post args='{"message":"{{payload.title}} finished"}' owner=none >/dev/null
cm command run triggers.add name="Tidy done → release" on=event.tidy.done command=agents.new args='{"title":"Release","prompt":"Cut a release"}' owner=none >/dev/null

for t in "Bump golang.org/x/net to v0.31" "Migrate the store to the new schema" "Port cron to the shared scheduler"; do
  cm agent spawn "$t" >/dev/null
  sleep 2
done

L1="$(spawn_b feat/stock-expiry "Add stock reservation expiry")"
L2="$(spawn_b fix/cart-total "Refactor the cart total")"
L3="$(spawn_b feat/inbox-groups "Inbox groups by workspace")"
L4="$(spawn_b docs/folding "Docs: transcript folding on the phone")"
sleep 3
commit_in feat/stock-expiry internal/stock/expiry.go 'package stock\n\nconst ReservationTTL = 15 * 60\n' "feat(stock): reservations expire after 15 min"
commit_in fix/cart-total internal/cart/total.go 'package cart\n\nfunc Total(items []int) (t int) {\n\tfor _, i := range items {\n\t\tt += i\n\t}\n\treturn\n}\n' "refactor(cart): one Total for cart and checkout"
commit_in feat/inbox-groups internal/notify/notify.go '\nfunc (b *Bus) Group(ws string) {}\n' "feat(inbox): group by workspace"
commit_in docs/folding docs/agents.md '\n## Transcript folding\n\nFailed tool calls stay folded.\n' "docs: transcript folding on the phone"

echo "L1 approved + landed"
land "$L1" feat/stock-expiry "feat(stock): reservations expire after 15 min"
sleep 3
verdict "$(reviewer_of "$L1")" approved
land "$L1" feat/stock-expiry "feat(stock): reservations expire after 15 min"

echo "L4 approved with a deferred item + landed"
land "$L4" docs/folding "docs: transcript folding on the phone"
sleep 3
verdict "$(reviewer_of "$L4")" approved '[]' '["docs/agents.md:6 — say which client draws the fold first"]'
land "$L4" docs/folding "docs: transcript folding on the phone"

echo "L2 changes x2, then needs you"
land "$L2" fix/cart-total "refactor(cart): one Total for cart and checkout"
sleep 3
verdict "$(reviewer_of "$L2")" changes '["internal/cart/total.go:4 — a nil items slice from checkout returns 0 instead of the stored total","internal/cart/total.go:5 — discounts are no longer subtracted"]'
commit_in fix/cart-total internal/cart/total.go '\nfunc Discounted(t, d int) int { return t - d }\n' "fix(cart): subtract discounts"
land "$L2" fix/cart-total "refactor(cart): one Total for cart and checkout"
sleep 3
verdict "$(reviewer_of "$L2")" changes '["internal/cart/total.go:11 — Discounted can go below zero"]'
commit_in fix/cart-total internal/cart/total.go '\nfunc clamp(v int) int { return max(v, 0) }\n' "fix(cart): clamp discounted totals"
land "$L2" fix/cart-total "refactor(cart): one Total for cart and checkout"

echo "L3 reviewing"
land "$L3" feat/inbox-groups "feat(inbox): group by workspace"

echo "graph runs"
cm command run graphs.run graph=review-loop input="add a CSV export of orders" >/dev/null
sleep 8
cm command run graphs.run graph=review-loop input="fix the flaky switcher test" >/dev/null
sleep 3
cm agent spawn "Rate-limit notifications per workspace" >/dev/null
sleep 20
echo "seeded: L1=$L1 L2=$L2 L3=$L3 L4=$L4"
