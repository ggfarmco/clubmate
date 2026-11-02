# Push relay

`clubmate-push-relay` (`cmd/clubmate-push-relay`) is a small server that holds the APNs auth key. Each Mac's `remote` plugin sends its pushes to the relay with its own token; the relay signs them with the key and forwards them to APNs. No Mac holds the `.p8`. What gets pushed and when is decided on the Mac (see [Remote § Real push](remote.md#real-push)); the relay only authenticates, validates and forwards.

## Configuration

Flags, each with an environment fallback:

| Flag | Env | Default | |
|---|---|---|---|
| `-listen` | `PUSH_RELAY_LISTEN` | `:8000` | address to listen on (plain HTTP) |
| `-key` | `PUSH_RELAY_KEY` | – | path to the `AuthKey_<KEYID>.p8` |
| `-key-id` | `PUSH_RELAY_KEY_ID` | `<KEYID>` of the file name | APNs key id |
| `-team-id` | `PUSH_RELAY_TEAM_ID` | – | Apple Developer team id |
| `-tokens` | `PUSH_RELAY_TOKENS` | – | Mac tokens file |

APNs topic is fixed: `com.clubmate.companion`. JWTs (ES256) are reused for 50 minutes.

## Mac tokens

The tokens file has one line per Mac: `<name> <sha256 hex of the token>` (blank lines and `#` lines are skipped). The relay re-reads it when its modification time or size changes, so adding or revoking a Mac needs no restart.

```
clubmate-push-relay token add <name> -tokens <file>   # prints a new cmr_… token once, appends its hash
clubmate-push-relay token rm <name> -tokens <file>    # removes the Mac's line
clubmate-push-relay token ls -tokens <file>           # lists names
```

The token itself is never stored on the relay; lose it and `token rm` + `token add` the Mac again.

## API

`POST /v1/push`, `Authorization: Bearer <mac token>`:

```json
{"token": "<hex device token>", "environment": "development", "type": "alert", "collapseID": "ntf_1", "payload": {"aps": {…}}}
```

- `401`: missing or unknown token (compared as sha256 hashes in constant time).
- `429`: more than 10 pushes a second per Mac (bursts up to 50).
- `400`: device token not hex or longer than 200 characters, `environment` not `development`/`production`, `type` not `alert`/`background`, `collapseID` longer than 64 bytes, `payload` not a JSON object or larger than 4096 bytes, or a body over 16 KiB.
- `200` `{"status": <APNs HTTP status>, "reason": "<APNs reason>"}` whenever APNs answered, also when it refused the push (e.g. `{"status": 410, "reason": "Unregistered"}`).
- `502`: APNs could not be reached.

`environment` picks `api.sandbox.push.apple.com` or `api.push.apple.com`; `apns-priority` comes from `type` (alert 10, background 5). `GET /healthz` → `200`.

Every push logs one line, `push <mac name> <type> <APNs status> <reason>`, never the payload.

## Deploy on exe.dev

exe.dev VMs are x86_64 Ubuntu 24.04 with systemd. A service listening on `0.0.0.0:<port>` is reachable at `https://<vm>.exe.xyz:<port>` with a Let's Encrypt `*.exe.xyz` certificate, so the relay itself serves plain HTTP on `0.0.0.0:8000`.

1. Create the VM: `ssh exe.dev new --name <vm>`.
2. From the repo: `scripts/deploy-push-relay.sh <vm>.exe.xyz`. It cross-builds a static linux/amd64 binary, copies it to `~/push-relay/` with `deploy/push-relay/clubmate-push-relay.service` (its `REPLACE_ME_USER` set to the VM login), creates an empty `~/push-relay/tokens` (mode 600), installs and enables the unit and restarts it once `~/push-relay/relay.env` exists. Run it again for every update; it never touches the key, `relay.env` or the tokens.
3. Copy the key and write `relay.env` without either passing through a terminal:

   ```bash
   ssh <vm>.exe.xyz 'umask 077 && cat > ~/push-relay/AuthKey_<KEYID>.p8' < ~/Downloads/AuthKey_<KEYID>.p8
   printf 'PUSH_RELAY_KEY=%s\nPUSH_RELAY_TEAM_ID=%s\n' "/home/<login>/push-relay/AuthKey_<KEYID>.p8" "<team id>" \
     | ssh <vm>.exe.xyz 'umask 077 && cat > ~/push-relay/relay.env'
   ssh <vm>.exe.xyz 'sudo systemctl restart clubmate-push-relay'
   ```

4. Check: `curl -fsS https://<vm>.exe.xyz:8000/healthz`.

The unit sets `PUSH_RELAY_LISTEN=0.0.0.0:8000` and `PUSH_RELAY_TOKENS=~/push-relay/tokens`, runs as the VM login with `NoNewPrivileges`, a read-only home and `Restart=always`.

Logs: `ssh <vm>.exe.xyz 'journalctl -u clubmate-push-relay -f'`.

Locally, `make push-relay` builds `bin/clubmate-push-relay` and `make push-relay-linux` the linux/amd64 one.

## Adding a Mac

1. On the VM: `ssh <vm>.exe.xyz '~/push-relay/clubmate-push-relay token add <mac name> -tokens ~/push-relay/tokens'` prints `cmr_…`.
2. On the Mac: `clubmate command run remote.set-push-relay url=https://<vm>.exe.xyz:8000 token=cmr_…`; `remote.status` shows `push: true`.

## Revoking a Mac

`ssh <vm>.exe.xyz '~/push-relay/clubmate-push-relay token rm <mac name> -tokens ~/push-relay/tokens'`. Its next push gets `401`; the Mac logs it and its phones fall back to the background refresh. On the Mac itself `remote.set-push-relay url=` clears the relay.
