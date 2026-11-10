# Push relay

`clubmate-push-relay` (`cmd/clubmate-push-relay`) is a small server that holds the APNs auth key. Each Mac's `remote` plugin sends its pushes to the relay with its own token; the relay signs them with the key and forwards them to APNs. No Mac holds the `.p8`. What gets pushed and when is decided on the Mac by the workspace's triggers that call `remote.push` (see [Remote § Real push](remote.md#real-push)); the relay only authenticates, validates and forwards.

The same server also takes **problem reports** from any Clubmate install (signed, anonymous or trusted by a Mac token), stores them on disk and hands them only to an admin token — see [Problem reports](#problem-reports).

## Configuration

Flags, each with an environment fallback:

| Flag | Env | Default | |
|---|---|---|---|
| `-listen` | `PUSH_RELAY_LISTEN` | `:8000` | address to listen on (plain HTTP) |
| `-key` | `PUSH_RELAY_KEY` | – | path to the `AuthKey_<KEYID>.p8` |
| `-key-id` | `PUSH_RELAY_KEY_ID` | `<KEYID>` of the file name | APNs key id |
| `-team-id` | `PUSH_RELAY_TEAM_ID` | – | Apple Developer team id |
| `-tokens` | `PUSH_RELAY_TOKENS` | – | Mac tokens file |
| `-admins` | `PUSH_RELAY_ADMINS` | – | admin tokens file for the reports API; unset → the private API answers `401` to everyone |
| `-bans` | `PUSH_RELAY_BANS` | – | banned report keys and IPs; unset or missing → no bans |
| `-reports` | `PUSH_RELAY_REPORTS` | `reports` | directory the reports are stored in (created `700`) |
| `-devices` | `PUSH_RELAY_DEVICES` | – | JSON file of the device tokens each Mac registered (see [Device binding](#device-binding)); unset → kept in memory only, lost on restart |

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
- `403` `device token not registered for this Mac`: the Mac has registered its devices and this device token is not one of them (see [Device binding](#device-binding)). Logged as `push <mac name> <type> refused: device not registered`; APNs is not called.
- `400`: device token not hex or longer than 200 characters, `environment` not `development`/`production`, `type` not `alert`/`background`, `collapseID` longer than 64 bytes, `payload` not a JSON object or larger than 4096 bytes, or a body over 16 KiB.
- `200` `{"status": <APNs HTTP status>, "reason": "<APNs reason>"}` whenever APNs answered, also when it refused the push (e.g. `{"status": 410, "reason": "Unregistered"}`).
- `502`: APNs could not be reached.

`environment` picks `api.sandbox.push.apple.com` or `api.push.apple.com`; `apns-priority` comes from `type` (alert 10, background 5). `GET /healthz` → `200`.

Every push logs one line, `push <mac name> <type> <APNs status> <reason>`, never the payload.

`PUT /v1/devices`, `Authorization: Bearer <mac token>`, `{"tokens": ["<hex device token>", …]}`: replaces the set of device tokens this Mac may push to. At most 32 tokens, each hex and at most 200 characters (`400` otherwise); `401` and `429` as for a push (the same bucket); `500` when the file can't be written (the old set stays); `204` on success. Logs `devices <mac name> <count>`.

### Device binding

Without it, any Mac token could push to any device token, and a phone that removed a Mac still got that Mac's pushes. Now:

- A Mac that has never called `PUT /v1/devices` (a Mac from before 10 Nov 2026) may push to any device token, as before. Old Macs and old phones keep working until they update.
- Once a Mac has registered a set, even an empty one, a push to a device token outside it gets `403`. There is no way back to the open mode for that Mac name short of deleting its entry from the devices file.
- A current Mac sends its full set after a pair, a `pushToken` from a phone (an empty one when the phone removes that Mac, see [Remote § Real push](remote.md#real-push)), a revoke, and before a push when the set changed. A `403` makes it register again and retry once, so a relay that lost its file (or runs without `-devices`) catches up on the next push.
- The file stores the sha256 of each device token (lower-case hex), not the token, keyed by Mac name: `{"<mac name>": ["<sha256 hex>", …]}`, written to a temp file and renamed. `token rm` leaves the Mac's entry; a Mac added again under the same name registers its set on its first push.

What it does not stop: the registration is authenticated by the Mac token alone. Whoever holds a Mac's relay token (a copied state dir) can register any device token for it and push there. Unpairing on the phone stops an honest Mac; against a leaked token the fix is still `token rm` on the relay. A phone-side proof (the phone, not the Mac, authorising each binding) is a backlog item.

## Problem reports

### `POST /v1/reports` (public)

The body is the report zip itself (`Content-Type: application/zip`, at most 25 MiB) with `report.json` at its root (`pkg/report.Meta`; `text` required, `text` and `summary` at most 20 KiB each). Every request is signed by the install's Ed25519 key (`pkg/report.Sign`): headers `X-Clubmate-Key`, `X-Clubmate-Time`, `X-Clubmate-Signature` over `method + path + time + sha256(body)`. An optional `Authorization: Bearer <mac token>` marks the report **trusted** with that Mac's name; an unknown bearer is ignored and the report stays anonymous. A trusted report still needs a valid signature.

Checks, cheapest first; the first that fails answers:

| Check | Answer |
|---|---|
| client IP banned | `403` |
| IP limit (anonymous only): 10 an hour | `429` |
| no `Content-Length` (chunked) | `411` |
| `Content-Length` or body over 25 MiB | `413` |
| anonymous: an upload from the same client IP still in flight | `429` |
| 4 anonymous (or 2 trusted) uploads already in flight | `429` |
| missing signature headers, bad signature, time more than 5 min off | `401` |
| the same signature seen in the last 10 min (replay) | `401` |
| key banned | `403` |
| trusted Mac: 30 an hour; anonymous key: 3 an hour and 10 a day, all anonymous reports together 200 a day | `429` |
| `report.json` missing or invalid, or its fields over 64 KiB; an entry the dev Mac would refuse to unpack (`pkg/report.CheckEntries`: a name with a control character or invalid UTF-8, a path component over 255 bytes or a name over 1000 bytes, `../` or absolute, a first component `stored.json` or `.handled` in any case, not a regular file or folder, over 10 000 entries, an entry declared over 25 MiB, all declared over 100 MiB) | `400` |
| storage over the 2 GiB quota, or an anonymous report past the 1.5 GiB anonymous share | `507` |

A signature is recorded as seen only after the bans and limits have passed, so a report refused with `403`/`429` can be sent again with the same signature. The IP bucket (anonymous only) is charged before the body is read and stays charged when a later check refuses the request. The key and all-anonymous buckets are charged together only when all of them allow it, so a request refused by one takes no token from the others.

The body has a progressive read deadline: start + 10 s + bytes received so far / 64 KiB/s, never past start + 2 min. Whatever `Content-Length` it claimed, a sender is cut off as soon as the bytes it has sent fall below (elapsed − 10 s) × 64 KiB/s, so one that sends nothing is cut off after 10 s. The deadline is cleared once the whole body has arrived, so a keep-alive connection is left untouched afterwards. If the upload fails, the expired deadline stays in place and the reply carries `Connection: close`, so the server never waits on the rest of the body.

The 2 min cap applies to trusted Macs too: a full 25 MiB report needs about 213 KiB/s (~1.7 Mbit/s) of uplink, and a slower Mac has to send a smaller bundle. A cut-off upload gets `400` and frees its slot.

Success is `201 {"id": "rpt_…"}`; nothing else about stored reports ever goes back to a sender. Every report logs one line, `report <id> <mac name | key <8 hex>> <ip> <bytes>`, never the content.

The client IP is the last `X-Forwarded-For` hop when the connection comes from a loopback or private address (the exe.dev proxy), otherwise the connection's own address, so a client that talks to the relay directly can't pick its IP with a header. For the IP limit and the one-upload-per-IP rule, an IPv6 address counts as its /64; an IP ban matches its exact address or CIDR range. Trusted Macs skip the IP limit and the one-upload-per-IP rule: they authenticate with their token, are limited per Mac name (30 an hour) and have their own 2 upload slots.

Limits are token buckets; a bucket idle for 24 h is dropped. Each limiter and the seen-signature set hold at most 100 000 entries. When a limiter is full, a new key or IP drops the buckets that have refilled to their burst (at most once a minute) and gets `429` only if none had; a new signature past 100 000 gets `429` until old ones expire (10 min).

### Private API (admin token only)

`Authorization: Bearer <admin token>`; a Mac token, a signature or nothing → `401`.

- `GET /v1/reports?after=<id>` → `[]` of `pkg/report.Stored` (`id`, `received`, `trusted`, `mac`, `key` (8 hex), `ip`, `bytes`, `report` = `report.json`) with an id after `after`, oldest first, at most 100. Ids are given out in arrival order and always increase. They have the `pkg/report.NewID` format; a report that arrives in the same second as the last one, or after the clock went back, gets the last id's suffix + 1. So, while the relay runs, a report stored after a page can never get an id below that page's last id, and paging with `after=` doesn't skip it. After a restart, the relay continues from the newest id still on disk. If the newest reports were deleted before the restart and the clock has also gone back, a new id can sort below an older cursor.
- `GET /v1/reports/{id}/bundle` → the zip; `404` for an unknown id.
- `DELETE /v1/reports/{id}` → `204`, also when the report is already gone.

### Admin tokens

Same file format as the Mac tokens (`<name> <sha256 hex>`, re-read on change), a separate file:

```
clubmate-push-relay admin add <name> -admins <file>   # prints a new cmr_… token once
clubmate-push-relay admin rm <name> -admins <file>
clubmate-push-relay admin ls -admins <file>
```

An admin token works only on the private API; it can't push and a Mac token can't read reports. If the admins file is gone or doesn't parse, the private API answers `401` to everyone until it is fixed (`reload admins: …` in the log).

### Bans

The bans file is re-read when its modification time or size changes. One ban per line, `#` lines and blank lines skipped:

```
key cecc1507         # hex prefix of the public key (the 8-hex `key` in the listing) or the full key
ip 203.0.113.5       # exact client address
ip 2001:db8::/48     # a CIDR range
```

A line that doesn't parse keeps the previous bans in force and logs `reload bans: …`.

### Storage

One directory per report, `<reports>/<id>/bundle.zip` + `meta.json`, both written to temp names and renamed. At start, an `rpt_…` directory without a valid `meta.json` is an interrupted write and is removed; nothing else in the directory is touched. The relay keeps only an index (id, time, size) in memory and reads `meta.json` from disk when listing. The total size on disk (`bundle.zip` plus `meta.json`) is capped at 2 GiB, of which anonymous reports may use at most 1.5 GiB, so the last 0.5 GiB stays free for trusted Macs: a report that would cross its class's cap gets `507` and pushes keep working. Reports nobody fetched are deleted 30 days after they arrived — checked on every report and once an hour.

## Deploy on exe.dev

exe.dev VMs are x86_64 Ubuntu 24.04 with systemd. A new VM's HTTPS proxy `https://<vm>.exe.xyz` (Let's Encrypt `*.exe.xyz` certificate) forwards to port 8000 (`ssh exe.dev share port <vm> 8000` sets it), so the relay itself serves plain HTTP on `0.0.0.0:8000`. The proxy is private (exe.dev login) until `ssh exe.dev share set-public <vm>`; Macs need it public, the relay checks their tokens itself.

1. Create the VM: `ssh exe.dev new --name=<vm>`.
2. From the repo: `scripts/deploy-push-relay.sh <vm>.exe.xyz`. It cross-builds a static linux/amd64 binary, copies it to `~/push-relay/` with `deploy/push-relay/clubmate-push-relay.service` (its `REPLACE_ME_USER` set to the VM login), creates empty `~/push-relay/tokens`, `admins` and `bans` (mode 600) and `~/push-relay/reports/` and `~/push-relay/state/` (mode 700) if missing, installs and enables the unit and restarts it once `~/push-relay/relay.env` exists. Run it again for every update; it never touches the key, `relay.env`, the tokens, the bans, the stored reports or the device bindings.
3. Copy the key and write `relay.env` without either passing through a terminal:

   ```bash
   ssh <vm>.exe.xyz 'umask 077 && cat > ~/push-relay/AuthKey_<KEYID>.p8' < ~/Downloads/AuthKey_<KEYID>.p8
   printf 'PUSH_RELAY_KEY=%s\nPUSH_RELAY_TEAM_ID=%s\n' "/home/<login>/push-relay/AuthKey_<KEYID>.p8" "<team id>" \
     | ssh <vm>.exe.xyz 'umask 077 && cat > ~/push-relay/relay.env'
   ssh <vm>.exe.xyz 'sudo systemctl restart clubmate-push-relay'
   ```

4. Check: `curl -fsS https://<vm>.exe.xyz/healthz`.

The unit sets `PUSH_RELAY_LISTEN=0.0.0.0:8000`, `PUSH_RELAY_TOKENS`, `PUSH_RELAY_ADMINS`, `PUSH_RELAY_BANS`, `PUSH_RELAY_REPORTS` and `PUSH_RELAY_DEVICES` (`~/push-relay/state/devices.json`) to the files above, runs as the VM login with `NoNewPrivileges`, a read-only home except `~/push-relay/reports` and `~/push-relay/state` (`ReadWritePaths=`) and `Restart=always`.

An admin token for the dev Mac: `ssh <vm>.exe.xyz '~/push-relay/clubmate-push-relay admin add <name> -admins ~/push-relay/admins'`.

Logs: `ssh <vm>.exe.xyz 'journalctl -u clubmate-push-relay -f'`.

Locally, `make push-relay` builds `bin/clubmate-push-relay` and `make push-relay-linux` the linux/amd64 one.

## Adding a Mac

1. On the VM: `ssh <vm>.exe.xyz '~/push-relay/clubmate-push-relay token add <mac name> -tokens ~/push-relay/tokens'` prints `cmr_…`.
2. On the Mac: `clubmate command run remote.set-push-relay url=https://<vm>.exe.xyz token=cmr_…`; `remote.status` shows `push: true`.

## Revoking a Mac

`ssh <vm>.exe.xyz '~/push-relay/clubmate-push-relay token rm <mac name> -tokens ~/push-relay/tokens'`. Its next push gets `401`; the Mac logs it and its phones fall back to the background refresh. On the Mac itself `remote.set-push-relay url=` clears the relay.
