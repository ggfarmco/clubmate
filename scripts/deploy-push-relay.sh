#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <host>" >&2
  exit 2
fi

HOST="$1"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$(mktemp -d)"
trap 'rm -rf "$OUT"' EXIT

(cd "$ROOT" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o "$OUT/clubmate-push-relay" ./cmd/clubmate-push-relay)

REMOTE_USER="$(ssh "$HOST" 'id -un')"
sed "s/REPLACE_ME_USER/$REMOTE_USER/g" "$ROOT/deploy/push-relay/clubmate-push-relay.service" >"$OUT/clubmate-push-relay.service"

ssh "$HOST" 'mkdir -p ~/push-relay && chmod 700 ~/push-relay'
scp -q "$OUT/clubmate-push-relay" "$HOST:push-relay/clubmate-push-relay.new"
scp -q "$OUT/clubmate-push-relay.service" "$HOST:/tmp/clubmate-push-relay.service"
ssh "$HOST" 'set -e
chmod 755 ~/push-relay/clubmate-push-relay.new
mv -f ~/push-relay/clubmate-push-relay.new ~/push-relay/clubmate-push-relay
touch ~/push-relay/tokens ~/push-relay/admins ~/push-relay/bans
chmod 600 ~/push-relay/tokens ~/push-relay/admins ~/push-relay/bans
mkdir -p ~/push-relay/reports
chmod 700 ~/push-relay/reports
sudo install -m 644 -o root -g root /tmp/clubmate-push-relay.service /etc/systemd/system/clubmate-push-relay.service
rm -f /tmp/clubmate-push-relay.service
sudo systemctl daemon-reload
sudo systemctl enable clubmate-push-relay >/dev/null 2>&1
if [ -f ~/push-relay/relay.env ]; then
  sudo systemctl restart clubmate-push-relay
  echo "clubmate-push-relay restarted"
else
  echo "no ~/push-relay/relay.env yet: copy the key and relay.env (docs/push-relay.md), then sudo systemctl restart clubmate-push-relay"
fi'
