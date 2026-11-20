# Tunnel relay

The tunnel relay is the second path between the Companion and a Mac (backlog #966). Tailscale stays the default path; a Mac can also use the relay, or only the relay. The relay is the package `internal/tunnelrelay`. `cmd/clubmate-push-relay` mounts it on `/v1/mac/` and `/v1/d/` in the same process as the push relay (see [Push relay](push-relay.md)).

The phone keeps the v1 contract of the `remote` plugin (SSE stream + `POST /v1/rpc`). It seals each inner request end to end for the Mac and sends it to the relay. The Mac holds one long SSE stream to the relay to get the phone's requests, and POSTs replies and stream frames back. The relay forwards opaque frames only.

**What the relay sees:** the Mac name, channel ids, request ids, sizes, times and IP addresses. It does not see the device token, the method, the path or a body: these are inside the sealed records.

## Constants

From the spike through the exe.dev proxy (`research/2026-11-20-relay-sse-spike.md` in clubmate-notes), in `internal/tunnelrelay/relay.go`:

| Name | Value | Use |
|---|---|---|
| `PingEvery` (`P_ping`) | 25 s | `: ping` on the Mac stream and on each device stream |
| `Liveness` | 55 s (2×`P_ping` + 5 s) | a client that gets no byte for this long closes the stream and connects again |
| `InlineMax` (`P_inline`) | 64 KiB | a request body up to this size goes inline in the `req` frame; a larger one the Mac pulls |
| `MaxBody` (`P_body`) | 17 MiB | the relay's own body limit (`413` above); the proxy has none |
| `MaxInFlight` (`P_h2`) | 90 | requests the phone keeps in flight per Mac (h2 opens a second connection at 101) |
| `BackoffFirst`, `BackoffMax`, `BackoffJitter`, `BackoffReset` (`P_backoff`) | 1 s, 30 s, ±20 %, 60 s | reconnect delay: 1 s, ×2 up to 30 s; back to 1 s after a stream that lived more than 60 s |
| `OpenWait` | 10 s | how long `POST /v1/d/open` waits for the Mac's msg2 |
| `CallWait` | 2 min 10 s | how long `POST /v1/d/call` waits for the Mac's reply to start |

The proxy sends its own `503` HTML when a response has no headers after 300 s. So every long response writes its headers and flushes them early: `/v1/mac/stream` at once; `/v1/d/call` and `/v1/d/stream` at the first byte from the Mac or after `HeadWait` (3 s), whichever comes first. The short wait lets the relay answer `410` when the Mac says it does not know the channel (`POST /v1/mac/close/{ch}`). The long handlers also clear the server's read and write deadlines, so the push relay's 10 s / 30 s server timeouts do not cut them; body transfers get 5 min.

## Auth

- **Mac:** `Authorization: Bearer cmr_…`, the push relay's Mac token and tokens file, a self-signed one too ([Push relay § Self sign-up](push-relay.md#self-sign-up)). `internal/tunnelrelay` gets a `func(bearer) (mac, ok)` from `main.go`. The Mac stream checks the token again at each ping; after `token rm`, a sign-up again with the same key (a new token) or a ban of a self-signed Mac's key, the stream ends and the Mac's channels are dropped.
- **Device:** `X-Clubmate-Ticket: cmk_…`, no `Authorization`. A ticket only lets a phone use relay bandwidth for one Mac. The device token is checked by the Mac inside the tunnel.

### Tickets

`PUT /v1/mac/tickets` `{"tickets": [{"h": "<sha256 hex of the ticket>", "exp": <unix>?}]}` sets the full ticket set of this Mac:

- At most 33 (32 devices + 1 pairing ticket); `h` is 64 lower-case hex characters (`400` otherwise). `exp` only on the pairing ticket; an expired ticket is dropped and refused.
- A hash that another Mac registered gets `409`, unless it is that Mac's pairing ticket and its `exp` has passed: then it is free and moves to this Mac.
- The PUT carries `X-Clubmate-Stream` with the id of this Mac's current stream (see [Mac side](#mac-side)); else `409` `not this Mac's current stream`, checked under the same lock as the write, so a Clubmate whose stream was replaced cannot overwrite the set of the one that holds the stream.
- `204` on success. The relay closes every channel (and every open still waiting) that was opened with a ticket that left the set: the Mac gets `close` with `why: ticket-revoked`, and that device's requests on the channel are cut, a reply that is already piping too (the Mac's `POST /v1/mac/reply/{id}` gets `410`). An `open` still waiting with that ticket gets `401`.
- The file (`-tickets`, see [Push relay § Configuration](push-relay.md#configuration)) stores only hashes, keyed by Mac name: `{"<mac name>": [{"h": "…", "exp": …}]}`, written to a temp file and renamed. It survives a restart; channels do not.

## Mac side

All with `Authorization: Bearer cmr_…`; `401` without a valid token. A Mac gets its token by [self sign-up](push-relay.md#self-sign-up) (`POST /v1/macs`, started by the plugin when companion access is on, see [Remote § Relay for every Mac](remote.md#relay-for-every-mac)) or by hand. After the first sign-up the plugin turns the relay path on when `tunnelOn` was never stored. A `401` on `GET /v1/mac/stream` for a token the Mac got by signing up makes the plugin sign up again with the same key (at most once per 10 minutes), then restart the stream with the new token; `remote.status.tunnel.state` / `detail` tell the UI where it is (`connecting`: "Signing up to the Clubmate relay…" / "Connecting to <host>…", `connected`: "Connected through <host>", `error`: one line).

**Stream id:** two Clubmates with the same token look the same to the relay, so each Mac stream gets a random id, sent in the `X-Clubmate-Stream: st_…` header of the `GET /v1/mac/stream` response. `PUT /v1/mac/tickets`, `POST /v1/mac/push` and `POST /v1/mac/close/{ch}` must carry it in `X-Clubmate-Stream`; a missing id or one that is not the Mac's current stream gets `409` `{"error": "not this Mac's current stream"}` and changes nothing (no ticket set, no `gone` list taken, no quota bytes, no channel closed). `open`, the body pull and `reply` act on an open or a request that the relay sent on one stream and drops when that stream ends, so a replaced stream gets `410` there; the Mac sends the header on them too.

| Endpoint | What |
|---|---|
| `GET /v1/mac/stream` | SSE. One stream per Mac name: a new one ends the old with `event: replaced`. Header `X-Clubmate-Stream` = the stream id. Frames below. `: ping` every 25 s. |
| `POST /v1/mac/open/{ch}` | Body = handshake msg2 (binary, at most 4 KiB). The relay gives it to the device's waiting `open`. `204`; `410` when the device left. |
| `GET /v1/mac/req/{id}/body` | The request body of a `req` over 64 KiB (binary records). Once per request. `410` when the device left. |
| `POST /v1/mac/reply/{id}` | Body = binary records of the reply, piped straight into the device's waiting response. `204` after the last byte; `410` when the device left (or the reply came too late); `413` over 17 MiB. |
| `POST /v1/mac/close/{ch}` | The Mac does not know this channel (its keys are only in memory: a plugin restart, the 10 min idle forget, a relay change). The relay drops the channel; each request on it that is still waiting gets `410` when its headers are not sent yet, else it is cut. Always `204`. |
| `POST /v1/mac/push` | Batch of stream frames: repeated `kind (1 B) \| id (16 B) \| len (u32 BE) \| len bytes`. Kind 1 = stream data (sealed records), kind 2 = stream end (its bytes, if any, go to the phone first). `200 {"gone": ["<id>", …]}`: streams whose device left since the last push, and ids in this batch the relay does not know. Send one batch at a time. |
| `PUT /v1/mac/tickets` | See [Tickets](#tickets). |

### Mac stream frames

Each frame is `event: <name>` + `data: <JSON>`:

- `open` `{"ch", "d": "<base64 msg1>"}`: a device opens a channel. Answer with `POST /v1/mac/open/{ch}`.
- `req` `{"id", "ch", "kind": "call"|"stream", "n": <bytes>, "d"?: "<base64 records>"}`. No `d` when `n` is over 64 KiB: pull the body. `id` is 32 hex characters (the 16 bytes of a push item's id).
- `close` `{"ch", "id"?, "why": "device-gone"|"ticket-revoked"|"idle"|"timeout"|"cut"}`: a channel (no `id`) or one request ended. The relay sends it for every request that ends without a full reply or stream end, so the Mac stops the handler: `device-gone` when a phone leaves a stream or a call before its reply, `timeout` when a call had no reply in 2 min 10 s (`CallWait`), `cut` when the relay cut the response (the Mac stream dropped, the reply upload broke, a stream frame did not fit); `idle` when the relay dropped the channel (see [Quotas](#quotas)).
- `limits` `{"bytesPerDay", "bytesToday", "channels", "calls", "streams", "body", "rate", "burst", "openRate", "openBurst", "idleSeconds"}`: once, first on each new stream. The quotas of this Mac and the bytes it used today (UTC).
- `replaced` `{}`: a newer stream of this Mac took over; this one ends.

The Mac does not trust the relay to keep these limits (`plugins/remote/tunnel_session.go`): it serves at most 80 requests at a time (the relay's 64 calls and 16 streams) and cuts the rest; it inflates a request to at most 16 MiB + 64 KiB (the handlers' body limit plus a head), so a deflate bomb ends before the device token is checked; it ends a call's handler at `CallWait` (2 min 10 s) and when `close` names it; it cuts an inner `GET /v1/events` sent as kind `call` (an event stream comes only as kind `stream`). A cut call gets an empty `POST /v1/mac/reply/{id}`, a cut stream a push end item. A call's reply whose sealed records are over `MaxBody` (17 MiB), or that is over 64 MiB before deflate (the most the Mac holds), is not cut: the Mac sends a sealed reply bound to the request (`re`, seq 0) with inner status `413` and `{"error": {"code": "too_large", "message": "This reply is too large for the relay path."}}`, so the phone shows a clear error and does not take it for "may have run". A stream chunk over 16 MiB ends the stream.

The relay queues at most 4096 frames for a Mac; a Mac that does not read them loses its stream. Queued frames count toward the relay's buffered bytes (see [Quotas](#quotas)). When the Mac stream ends, every request that was sent on it and is not answered yet is cut (see 502 below); channels stay, so a Mac that reconnects keeps them.

## Device side

All with `X-Clubmate-Ticket`. `call` and `stream` also take `X-Clubmate-Channel: ch_…`.

| Endpoint | What |
|---|---|
| `POST /v1/d/open` | Body = msg1 (at most 4 KiB). Waits up to 10 s for msg2. `200` body msg2, header `X-Clubmate-Channel`. `504` when the Mac does not answer in time. |
| `POST /v1/d/call` | Body = the records of one inner request. `200` with the first reply byte or after 3 s, then the Mac's reply records (binary, streamed). |
| `POST /v1/d/stream` | Body = the records of an inner `GET /v1/events`. `200 text/event-stream` with the first Mac frame or after 3 s, then each Mac stream frame as `data: <base64>` and `: ping` every 25 s. Ends on the Mac's end frame or when the channel closes. |

### Errors

Every response the relay makes, success or error, Mac side and device side, carries `X-Clubmate-Relay: 1`. Errors are `application/json` `{"error": "…"}`. A response without that header comes from the exe.dev proxy (the relay is down, or a response had no headers for 300 s): the phone and the Mac treat it as "relay unreachable, not sent" and back off.

| Code | Meaning | Reached the Mac |
|---|---|---|
| `401` | unknown or expired ticket | no |
| `410` | channel unknown (relay restart, idle, not opened with this ticket, or the Mac closed it with `POST /v1/mac/close/{ch}` before the headers went out): open a new one | no, if the relay is honest: the phone sends it again once (see below) |
| `503` | Mac not connected to the relay | no |
| `413` | body over 17 MiB | no |
| `429` | a quota (see [Quotas](#quotas)); wait `Retry-After` seconds | no |
| `504` | `open` only: the Mac did not answer in 10 s | no |
| cut after `200` | `call` and `stream` only: the Mac's stream dropped while the request was out, or no reply in 2 min 10 s | maybe ("may have run") |

`call` and `stream` check the ticket, the channel, the Mac and the body size before they send anything, so all codes above except the last one come as a status. After the headers went out (the headers rule above) the response is already `200`, so the relay cannot send `502` any more: it cuts the response instead (no clean end; with HTTP/1.1 the connection closes before the last chunk, with h2 the stream is reset). The phone treats a cut-off call as the `502` of the plan: it may have run on the Mac. A reply that arrived whole always ends with a record that has the last-part flag inside its seal, so a cut is never taken for a complete reply.

The phone sends a call again once after `410`, on a new channel. A relay that lies can forward the call and still answer `410`, so the call can run twice. So every phone call that changes state and is not idempotent carries a `requestId` (the Mac runs it once, see [remote § Wire contract](remote.md#wire-contract-v1)): outbox writes, prompts with attachments, `report`, `uiEvent`, `agents.graph-rerun` and `voice.start`. Calls without one are reads or idempotent sets (`hello`, `workspaces`, list and get commands, `macs`, `pushToken`, `setIdentityColour`, `uiWatch`, `voice.end`, `voice.cancel`, `voice.confirm`). `voice.audio` is the exception: a repeated audio chunk is fed twice, and the user sees the text before it is sent.

## Quotas

Constants in `internal/tunnelrelay/quota.go` (`DefaultLimits`). Per Mac name:

| Quota | Default | Over it |
|---|---|---|
| bytes in + out per UTC day | 2 GiB | `429` on `open`, `call`, `stream` and `POST /v1/mac/push`, `Retry-After` until 00:00 UTC |
| open channels | 32 | the oldest channel with no request in flight is dropped (`close` `why: idle` to the Mac); when all have one, `429` on `open` |
| calls in flight | 64 | `429` on `call` |
| open device streams | 16 | `429` on `stream` |
| request or reply body | 17 MiB | `413` |
| device requests (`open`, `call`, `stream`) | 20 per s, burst 100 | `429` |
| `open` per ticket | 1 per s, burst 10 | `429` |
| channel idle | 10 min with no request | channel dropped, `close` `why: idle`; the phone then gets `410` |
| Mac streams | 1 | the older one gets `replaced` |

Bytes are the device side: msg1, msg2, request bodies, reply bodies and stream frames. A reply to a call already let in always goes through.

Global caps, so a tunnel flood cannot starve `/v1/push` in the same process:

| Cap | Default | Over it |
|---|---|---|
| Mac streams | 5000 | `429` on `GET /v1/mac/stream`, `Retry-After: 60` |
| channels (open and opening) | 100 000 | idle channels are dropped first, then `429` on `open` |
| tunnel requests in progress | 10 000 | `429` on any tunnel route |
| bytes the relay holds (request bodies, queued Mac frames, stream frames not yet sent) | 256 MiB | `429` on the request that would cross it; a stream frame over it ends that stream (its id goes to `gone`) |

Every refusal is `429` with `Retry-After` (seconds, at least 1), `X-Clubmate-Relay: 1` and a JSON error `quota: <what>`. The counters live in memory: a restart resets them.

## Records and handshake

The frames are opaque to the relay; this is what the Mac and the phone put in them (`pkg/tunnel`, Swift `TunnelCrypto.swift` / `TunnelPack.swift`). Test vectors for both sides: [`pkg/tunnel/testdata/vectors.json`](../pkg/tunnel/testdata/vectors.json).

**Handshake** (like Noise NK: the phone knows the Mac's static X25519 key `S` from the QR, the phone has no static key):

- `h0 = SHA256("clubmate tunnel v1" | S.pub)`.
- msg1 (phone → Mac): `ver=1 (1) | e_d.pub (32) | AES-256-GCM_k1(nonce 0, aad h1, {"dicts": [1], "kid", "th"?})`; with `es = X25519(e_d, S)`, `h1 = SHA256(h0 | e_d.pub)`, `k1 = HKDF-SHA256(es, salt h0, info "clubmate tunnel v1 k1")`. `th` = sha256 hex of the relay ticket the phone uses: the Mac binds the channel to it and tells a pairing ticket from a device ticket (the relay does not tell it).
- msg2 (Mac → phone): `e_m.pub (32) | AES-256-GCM_k2(nonce 0, aad h2, {"dict": 1|0})`, with `ee = X25519(e_m, e_d)`, `h2 = SHA256(h1 | e_m.pub)`, `HKDF-SHA256(es | ee, salt h1, info "clubmate tunnel v1 keys" | h2, 96)` → `k_dm | k_md | k2`. Only a peer that holds `S` can make msg2, so the relay cannot sit in the middle; `ee` gives forward secrecy.

**Records:** a message (one inner request, one reply, one stream chunk) is split into records of at most 64 KiB plaintext. On the wire (POST bodies, `d` fields, SSE `data`) a record is `len (u32 BE) | counter (u64 BE, in clear) | ciphertext`. AES-256-GCM with `k_dm` (phone → Mac) or `k_md` (Mac → phone); nonce = 4 zero bytes | counter, from 1 per direction; AAD = channel id. The receiver keeps a window of 65536 counters (an 8 KiB bitmap per direction), so records may arrive far out of order: call replies are piped straight while stream chunks wait in the push queue, and parallel calls with large bodies interleave. A repeat or a counter below the window is refused. The window holds more than 90 calls in flight × 17 MiB (about 24 500 records) and more than 4 GiB of records the other way, over the daily quota. The sealed plaintext is `type (1) | flags (1) | msg (u32 BE) | part (u16 BE) | payload`, so a cut-off, re-ordered or spliced message fails when the receiver puts it together.

**Message plaintext:** `real length (u32) | deflate?(head JSON | 0x0A | body) | zero padding`, deflate per message (optionally with the public `dict-v1` agreed in the handshake), padded to 64 B steps up to 1 KiB, then 5 % steps.

**Answers are bound to their request:** the records of a reply, a stream chunk or a stream end carry `re (u32 BE) | seq (u32 BE)` before the message plaintext, inside the seal. `re` is the `msg` of the request it answers; `seq` is 0 for a call's reply and counts a stream's messages from 0 (the reply head, then each chunk, then the end) with no gap. The phone keeps the `msg` of each request it sent and refuses an answer with another `re`, a gap or a repeat as a bad response (not "may have run"): so the relay cannot swap the replies of two calls, give a write an old `ok` reply of another call, or drop or move single stream chunks without the phone seeing it. A request has no `re | seq`.

## Logs

`mac <name> stream open|close|replaced`, `open <name> ch=<8>`, `call <name> ch=<8> in=<n> out=<n> ms=<n> status=<n>` (`499` when the phone left, `502` cut, `504` no reply), `stream <name> ch=<8> lived=<n>s`, `tickets <name> n=<n> closed=<n>` (`n` = tickets stored), `close <name> ch=<8> why=mac`, `quota <name> <what>` (once per UTC day for each Mac and quota), `quota relay <what>` (a global cap, at most once a minute), `tunnel panic <method> <path>: <value>`. Never a body, a ticket or a token; `<8>` is the first 8 characters of the channel id after `ch_`.

## Deploy

The tunnel routes run in the push relay process on `clubmate-push.exe.xyz` (exe.dev VM `clubmate-push`, proxy `https://clubmate-push.exe.xyz` → `:8000`). Pushes and problem reports run on the same host, so keep the old binary until the checks pass. First deployed 2026-11-20.

1. Back up the binary and the unit:

   ```bash
   ssh clubmate-push.exe.xyz 'cp -p ~/push-relay/clubmate-push-relay ~/push-relay/clubmate-push-relay.prev && cp -p /etc/systemd/system/clubmate-push-relay.service ~/push-relay/clubmate-push-relay.service.prev'
   ```

2. Deploy: `scripts/deploy-push-relay.sh clubmate-push.exe.xyz` (see [Push relay § Deploy on exe.dev](push-relay.md#deploy-on-exedev); it adds `PUSH_RELAY_TICKETS` and an empty `~/push-relay/state/tickets.json`).
3. `curl -fsS https://clubmate-push.exe.xyz/healthz` → `200`.
4. Add a test Mac and keep its token in a shell variable only: `TOK=$(ssh clubmate-push.exe.xyz '~/push-relay/clubmate-push-relay token add relay-e2e-<date> -tokens ~/push-relay/state/tokens')`, and remove it on exit: `trap "ssh clubmate-push.exe.xyz '~/push-relay/clubmate-push-relay token rm relay-e2e-<date> -tokens ~/push-relay/state/tokens'" EXIT`.
5. Push probe: `POST /v1/push` with `Bearer $TOK` and a device token of 64 zeros (`{"token": "000…", "environment": "development", "type": "alert", "payload": {"aps": {}}}`) → `{"status":400,"reason":"BadDeviceToken"}`: APNs took the key and the JWT.
6. Tunnel check through the exe.dev proxy: `CLUBMATE_TEST_LIVE_RELAY=https://clubmate-push.exe.xyz CLUBMATE_TEST_LIVE_RELAY_TOKEN=$TOK go test -count=1 -run TestHelloAndAnEventStreamThroughTheLiveRelay ./plugins/remote/`. A fake Mac (the `remote` plugin's tunnel client) and a fake phone open a channel, send one `hello` and read one event stream (a replayed event and a live one). The test uses a random ticket and sends an empty ticket set at the end, so the tickets file keeps no entry for the test Mac. Without the two variables the test is skipped; `TestHelloAndAnEventStreamThroughTheRelayBinary` runs the same check against a local relay binary.

`token rm` leaves an empty entry for the test Mac in `~/push-relay/state/devices.json` (see [Push relay § Device binding](push-relay.md#device-binding)); it does no harm.

**Rollback** (any step fails):

```bash
ssh clubmate-push.exe.xyz 'cp -p ~/push-relay/clubmate-push-relay.prev ~/push-relay/clubmate-push-relay && sudo install -m 644 -o root -g root ~/push-relay/clubmate-push-relay.service.prev /etc/systemd/system/clubmate-push-relay.service && sudo systemctl daemon-reload && sudo systemctl restart clubmate-push-relay'
curl -fsS https://clubmate-push.exe.xyz/healthz
```

Then run the push probe again.
