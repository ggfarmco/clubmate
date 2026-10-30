# Remote access (Clubmate Companion)

The `remote` plugin lets the Clubmate Companion app (iPhone / iPad) control agents on this Mac over your Tailscale tailnet. There is no backend: the phone talks straight to the Mac.

## Enable and pair
- Off by default. Settings → Devices → "Pair Device…" (or `remote.pair`) turns it on and shows a QR code.
- The phone scans the QR code (`clubmate://pair?...`) and gets a device token. A phone can pair with many Macs.
- Settings → Devices lists paired devices (connected / last seen) and revokes them. "Allow companion devices" (`remote.enable {enabled:false}`) stops listening without forgetting devices.

From the CLI (app-scoped commands, no workspace needed):
```
clubmate command run remote.pair
clubmate command run remote.status
clubmate command run remote.devices
clubmate command run remote.revoke device=<id>
```

## Network
- Plain HTTP/1.1 on the Mac's Tailscale addresses only. Never `0.0.0.0` or LAN addresses.
- The addresses come from Tailscale itself (`Self.TailscaleIPs` of `tailscale status --json`, the app's bundled CLI or `tailscale` on `PATH`). Without the CLI, it falls back to `utun*` interface addresses in `100.64.0.0/10` or `fd7a:115c:a1e0::/48` (`100.64.0.0/10` is shared CGNAT space, so the interface filter alone is not trusted). Either way only addresses in those ranges are used.
- Port `47621` (plugin storage key `port`).
- Addresses are rescanned every 30 s, so Tailscale may come up after Clubmate.
- The QR link lists hosts in order: MagicDNS name (from `tailscale status --json`, optional), IPv4, IPv6.
- `CLUBMATE_REMOTE_LISTEN=host:port` (env of the core, inherited by the plugin) replaces the tailnet scan with that one address, e.g. `127.0.0.1:47621` for the iOS simulator or tests. A wildcard host (`:47621`, `0.0.0.0`, `::`) is refused.

## Security model
- WireGuard (Tailscale) encrypts and authenticates the hosts; the device token authorizes the app. No app-level TLS in v1.
- Pairing code: 128 random bits, base32, valid 5 minutes, single use, one active code at a time (a new `remote.pair` replaces it). A source address with 5 wrong guesses is refused for the rest of the code's life while the code stays valid for others; 50 wrong guesses in total invalidate it. If saving the new device fails, the pair request gets `500` and the code stays valid.
- Device token: `cmt_` + base64url of 32 random bytes. Only its hex SHA-256 is stored (plugin storage `devices`); comparisons are constant-time. Tokens and codes are never logged.
- Pairing the same device id again replaces its old token. Revoking a device closes its event streams; its next request gets 401.
- A paired device acts as you: it can run every command whose manifest has `"remote": true` (the agents commands: sessions, transcript, prompt, cancel, permission, new, close, mode, config) and answer any pending permission. Commands run through core `command.run` with `agent: false` and no caller; the core marks them `via: "remote"`.
- Request bodies are limited to 1 MiB; the server has header/read/idle timeouts and per-write deadlines.

## Wire contract (v1)
All bodies are JSON. Authenticated endpoints need `Authorization: Bearer <token>`; a missing, unknown or revoked token gets `401 {"error":{"code":"unauthorized","message":"..."}}`.

- `POST /v1/pair` (no auth) `{"code","device":{"id","name","model"}}` → `{"token","computer":{"id","name"}}`; a wrong, expired or used code → `403 pairing_failed`; a malformed body → `400 invalid_params`.
- `POST /v1/rpc` `{"method","params"}` → `200 {"result":...}` or `200 {"error":{"code","message"}}` (`not_allowed`, `invalid_params`, `failed`):
  - `hello` → `{computer, version, commands, topics}`; `commands` = registered commands with `"remote": true`.
  - `workspaces` → `[{id, name, roots}]`.
  - `command {id, workspace, args}` → the command's result verbatim; non-remote ids fail with `not_allowed`.
- `GET /v1/events` (SSE): starts with a `: connected` comment, then `id: <seq>\nevent: bus\ndata: {"seq","topic","workspace","payload"}` for `agents.changed` and `notify.posted`, and `: ping` every 15 s. `Last-Event-ID` (or `?after=<seq>`) replays later events from the bus first; without it the stream starts live. Events only say "something changed"; the phone refetches with `command`.

## Mac app commands
All hidden, app-scoped (`"scope": "app"`), never agent or remote. Call `command.run {id, projectId: "" (or any), args}`.

| Command | Args | Result |
|---|---|---|
| `remote.status` | – | `{enabled, port, listening: ["100.x.y.z:47621"], hosts: ["mac.tailnet.ts.net", "100.x.y.z"], error}` |
| `remote.enable` | `{enabled}` | status |
| `remote.pair` | – | `{link, code, expiresAt}` (RFC 3339); enables remote access; fails without issuing a code when there is no address to listen on (e.g. Tailscale is down) |
| `remote.cancelPair` | – | `null` |
| `remote.devices` | – | `[{id, name, model, pairedAt, lastSeen, connected}]`, newest first |
| `remote.revoke` | `{device}` | `null`; destructive, confirm "Revoke this device's access to Clubmate?" |

The plugin publishes `remote.changed` `{}` when a device pairs, is revoked, connects or disconnects, and when access is turned on or off or the listening addresses change.

## Companion app: App Transport Security

The Companion app talks plain HTTP to the Mac's `remote` plugin. Its `Info.plist` sets `NSAppTransportSecurity.NSAllowsArbitraryLoads = YES`.

- Why plain HTTP: the plugin listens only on Tailscale addresses. WireGuard encrypts and authenticates the hosts; the device token authorizes the app.
- Why this key (measured in the iOS 27 simulator):
  - `127.0.0.1` works without any ATS key.
  - `100.x.y.z` (CGNAT) is blocked by ATS. `NSAllowsLocalNetworking` does not cover it.
  - `*.ts.net` (MagicDNS) is blocked by ATS. An `NSExceptionDomains` entry for `ts.net` would cover it, but not the IP fallbacks.
- What limits it instead: `HostPolicy` in CompanionKit refuses every host outside `100.64.0.0/10`, `fd7a:115c:a1e0::/48` and `*.ts.net` (strict DNS labels), before any request and before the token is attached. Loopback is allowed only in Debug builds.
- `NSLocalNetworkUsageDescription` is set for Tailscale traffic on device; the simulator never shows that prompt.
- Simulator builds are ad-hoc signed (`CODE_SIGN_IDENTITY=-`). Without a signature the Keychain fails with `-34018` and the token can't be stored.
