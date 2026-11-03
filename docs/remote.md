# Remote access (Clubmate Companion)

The `remote` plugin lets the Clubmate Companion app (iPhone / iPad) control agents on this Mac over your Tailscale tailnet. There is no backend: the phone talks straight to the Mac.

## Enable and pair
- Off by default. Settings → Remote → "Pair Device…" (or `remote.pair`) turns it on and shows a QR code.
- The phone scans the QR code (`clubmate://pair?...`) and gets a device token. A phone can pair with many Macs.
- Settings → Remote lists paired devices (connected / last seen) and revokes them. "Allow companion devices" (`remote.enable {enabled:false}`) stops listening without forgetting devices.

From the CLI (app-scoped commands, no workspace needed):
```
clubmate command run remote.pair
clubmate command run remote.status
clubmate command run remote.devices
clubmate command run remote.revoke device=<id>
clubmate command run remote.set-port port=<port>
clubmate command run remote.set-colour colour=<token>
clubmate command run remote.set-title-bar-colour enabled=false
clubmate command run remote.set-push-relay url=https://<vm>.exe.xyz token=cmr_…
```

## A colour for each Mac
- Every Mac has an identity colour, a name tag shown next to its name on paired phones and in the Mac title bar. Tokens: `sky`, `blue`, `indigo`, `purple`, `magenta`, `graphite`. The wire carries the token, never a hex; an unknown or missing token renders as `graphite`.
- The name is the system computer name (read-only). The colour is stored with the computer (plugin storage key `computer`, `{id, name, colour}`).
- Default: while the stored colour is empty, the first `macs` call picks the first token of `blue`, `magenta`, `sky`, `purple`, `indigo`, `graphite` that no paired phone reports for another Mac; with nothing reported (or every token taken) it is `blue`. It is never re-picked afterwards. Duplicates are allowed.
- Each phone reports the other Macs it has paired with the `macs` rpc, only after pairing succeeded and the Mac's id matched the QR code, so a host answering a pairing code never learns them. They are stored on its device record (`device/<id>`). Older phones report nothing, read as `[]`. Pairing never picks the colour; the pair response carries an empty one until the phone's first `macs` call, which publishes `remote.identity`.
- Changed from the phone (`setIdentityColour`) or the Mac (`remote.set-colour`, Settings → Remote). Every change publishes `remote.identity` `{computer: {id, name, colour}}` (forwarded to phones) and `remote.changed`.
- Title bar: the workspace pill shows the dot iff `remote.status.titleDot` = `titleBarColour` (setting, default on, storage key `titleBarColour`) and at least one other Mac is reported. The Mac UI renders `titleDot` and never re-derives the rule.

## Network
- Plain HTTP/1.1 on the Mac's Tailscale addresses only. Never `0.0.0.0` or LAN addresses.
- The addresses come from Tailscale itself (`Self.TailscaleIPs` of `tailscale status --json`, the app's bundled CLI or `tailscale` on `PATH`). Without the CLI, it falls back to `utun*` interface addresses in `100.64.0.0/10` or `fd7a:115c:a1e0::/48` (`100.64.0.0/10` is shared CGNAT space, so the interface filter alone is not trusted). Either way only addresses in those ranges are used.
- Port `47621` by default (plugin storage key `port`); `remote.set-port {port}` changes it. A paired phone keeps the port from its pairing link, so after a change every paired device stays offline until it pairs again. The port is ignored while `CLUBMATE_REMOTE_LISTEN` is set.
- Addresses are rescanned every 30 s, so Tailscale may come up after Clubmate.
- The QR link lists hosts in order: MagicDNS name (from `tailscale status --json`, optional), IPv4, IPv6.
- `CLUBMATE_REMOTE_LISTEN=host:port` (env of the core, inherited by the plugin) replaces the tailnet scan with that one address, e.g. `127.0.0.1:47621` for the iOS simulator or tests. A wildcard host (`:47621`, `0.0.0.0`, `::`) is refused.

## Security model
- WireGuard (Tailscale) encrypts and authenticates the hosts; the device token authorizes the app. No app-level TLS in v1.
- Pairing code: 128 random bits, base32, valid 5 minutes, single use, one active code at a time (a new `remote.pair` replaces it). A source address with 5 wrong guesses is refused for the rest of the code's life while the code stays valid for others; 50 wrong guesses in total invalidate it. If saving the new device fails, the pair request gets `500` and the code stays valid.
- Device token: `cmt_` + base64url of 32 random bytes. Only its hex SHA-256 is stored (plugin storage `device/<id>`, one key per device; the old `devices` list moves there on load); comparisons are constant-time. Tokens and codes are never logged.
- Pairing the same device id again replaces its old token. Revoking a device closes its event streams; its next request gets 401.
- A paired device acts as you: it reads every workspace's plugin UI and sends it events (`uiSections`, `uiGet`, `uiEvent`, `uiWatch` below), like a window on the Mac; it can run every command whose manifest has `"remote": true` (the agents commands: sessions, transcript, docs, image, blame, prompt, cancel, stop-subagent, permission, elicitation, new, close, mode, config, resume, handoff, reviewed, notify, afk, trace, export, import; the backlog commands: list, get, add, update, claim, release, done; `docs.read` and `docs.list`, see [docs viewer](docs-viewer.md); the voice commands: start, audio, end, cancel, state, confirm, dictionary, dictionary-set, settings, see [voice](voice.md); `editor-nvim.settings`) and answer any pending permission. Commands run through core `command.run` with `agent: false` and no caller; the core marks them `via: "remote"`.
- Responses of `/v1/pair` and `/v1/rpc` are gzip-compressed when the request's `Accept-Encoding` allows gzip (URLSession sends it and decompresses on its own); `/v1/events` is never compressed, so every SSE frame is flushed as it is.
- Request bodies are limited to 16 MiB (room for photos and files attached to prompts; the phone caps attachments at 10 MiB per message, and a file it sends is saved under the agents plugin's `uploads/<session>/` and attached by path); the server has header/read/idle timeouts and per-write deadlines; an `/v1/rpc` body may take the full request timeout (2 min) to arrive, so a large attachment over a slow link is not cut at the 30 s read timeout.

## Wire contract (v1)
All bodies are JSON. Authenticated endpoints need `Authorization: Bearer <token>`; a missing, unknown or revoked token gets `401 {"error":{"code":"unauthorized","message":"..."}}`.

- `POST /v1/pair` (no auth) `{"code","device":{"id","name","model"}}` → `{"token","computer":{"id","name","colour"}}`; a `macs` field from 0.0.20 phones is ignored; a wrong, expired or used code → `403 pairing_failed`; a malformed body → `400 invalid_params`.
- `POST /v1/rpc` `{"method","params"}` → `200 {"result":...}` or `200 {"error":{"code","message"}}` (`not_allowed`, `invalid_params`, `failed`):
  - `hello` → `{computer: {id, name, colour}, version, commands, topics, ui}`; `ui: true` means the UI methods below exist; `commands` = registered commands with `"remote": true` whose plugin is running (`command.list` carries `running`). The phone reads `hello` on each connection and again on every `plugin.changed` event, so a plugin that starts or stops later is reflected live.
  - `workspaces` → `[{id, name, folders, roots}]` (`folders` as in `workspace.list`; `roots` is deprecated and kept for Companion builds that predate `folders`).
  - `command {id, workspace, args}` → the command's result verbatim; ids missing from `hello.commands` (non-remote or plugin not running) fail with `not_allowed`. The plugin caches that list and drops it on every `plugin.changed`.
  - `macs {macs: [{name, colour}]}` → `null`; replaces this device's list of the other Macs it has paired. The phone sends it after every `hello` and whenever its Mac list or another Mac's colour changes.
  - `setIdentityColour {colour}` → `{computer}`; an unknown token fails with `invalid_params`.
  - `pushToken {token, environment}` → `{push}`; stores this device's APNs device token (hex, at most 200 characters) and its `environment` (`development` or `production`) on its device record (`pushToken`, `pushEnvironment`); another device holding the same token loses it; an empty `token` clears it. `push` is true when the Mac has a push relay set (see [real push](#real-push)).
  - `uiSections` → the core's `ui.sections` entries with `scope: workspace`, verbatim (`[{id, title, icon, order, scope, autoNew, plugin, slots?, host?, rail?}]`).
  - `uiGet {workspace, slots}` → `[{workspaceId, slot, owner, rev, tree}]`, the core's `ui.get` views (full trees) of that workspace whose slot is in `slots`; `*.popup` in `slots` stands for every `<section>.popup` slot.
  - `uiEvent {workspace, slot, nodeId, action, value}` → core `ui.event` with `workspaceId: workspace`, result verbatim (`null` when empty). Any slot is allowed.
  - `uiWatch {stream, workspace, slots}` → `null`; replaces what the event stream `stream` (the id from its `event: stream` frame) watches in that workspace; `slots` as in `uiGet` (at most 64, exact slot names or `*.popup`), `[]` stops watching it. A stream of another device, or one that is closed, fails with `invalid_params`, as does an unknown workspace. Slots that the call adds and that already have a view are sent on that stream at once, so a render between `uiGet` and `uiWatch` is not lost.
- `GET /v1/events` (SSE): starts with a `: connected` comment, then `event: stream` `data: {"id": "<random>"}` (the stream id for `uiWatch`, new on every connection), then `id: <seq>\nevent: bus\ndata: {"seq","topic","workspace","payload"}` for `agents.changed`, `backlog.changed`, `docs.changed` (`{workspace, path, rev}`, a doc the phone read or the Mac shows changed), `docs.opened` (`{workspace, session, path}`, an agent opened a doc), `notify.posted`, `plugin.changed` (`{id, running}`, published by the core when a plugin starts running and when a run or start attempt ends, after its new state — backoff, failed, stopped — is set), `voice.changed` (`{workspace, utterance, rev}`, an utterance's state changed; read it with `voice.state`, see [voice](voice.md)), `voice.settings-changed` (the `voice.settings` result, voice was turned on or off or its model changed) and `remote.identity` (`{computer: {id, name, colour}}`, this Mac's colour changed), and `: ping` every 15 s. `Last-Event-ID` (or `?after=<seq>`) replays later events from the bus first; without it, or when it is above the bus head (the Mac's event store was reset), the stream starts live and the phone takes the next event's seq as its cursor. The plugin subscribes to and replays only these topics. A burst past a stream's 256-event buffer does not close it: the stream sends what it buffered, then replays the rest from the bus, in order. Events only say "something changed"; the phone refetches with `command`. An open transcript is refetched with `agents.transcript {compact: true, since: <the last result's cursor>}` (on `agents.changed` and every 2 s while the session runs), so only the entries that changed come back. An `agents.changed` of kind `resolved` names the request that stopped pending (`request`) and what it was (`requestKind`: `permission` or `elicitation`; absent in older events, read as `permission`): the phone drops its card and that session's delivered notifications of the matching kind (`permission` for a permission, `needs-input` for an elicitation) at once, wherever it was answered.

### Plugin UI on the phone
- While any stream watches a slot of a workspace, the plugin holds one core `ui.subscribe` for that workspace (full trees, no deltas), shared by every stream and device, and drops it when the last stream stops watching it or closes (revoke, disconnect). It counts toward presence (`presence: true`, plugins render as for an open window) only while some stream watches a slot that is not a popup; with only popups watched it subscribes with `presence: false`. When that changes it subscribes anew and then drops the old subscription.
- Views go out as `event: ui` `data: {"workspace", "slot", "rev", "tree"}` frames, without `id:`, only to streams that watch that slot; they are not replayed after a reconnect: the phone calls `uiGet` and `uiWatch` again with the new stream id. A view whose `rev` is not above the last one the plugin got for that slot is dropped. A stream that cannot keep up gets only the latest view of each slot (the newer view replaces the pending one), like the core does for slow windows; it is never closed for it.
- The plugin decodes every view of a watched workspace, also of slots no phone watches (e.g. a streaming `agents.main`), since the core filters by workspace only.
- Phones never select a section on the Mac, so plugins that render only the Mac's selected section learn what phones show from `remote.watching` `{workspace, sections}`: the sorted, unique sections (the slot name before the first dot) of the non-popup slots all streams watch in that workspace, published whenever that set changes, and with `sections: []` when the last stream stops watching them or disconnects. On start the plugin publishes `sections: []` for every workspace in `workspace.list`, so watches left by a crashed plugin are cleared (phones watch again when they reconnect). A plugin that starts later restores the set from the bus history with `pluginsdk.Host.WatchedSections` (the latest event per workspace up to its ready seq) and then follows live events. `processes` and `agents` treat a watched section as shown (see [SDUI § Visible section](sdui-protocol.md#visible-section)).

## Mac app commands
All hidden, app-scoped (`"scope": "app"`), never agent or remote. Call `command.run {id, workspaceId: "" (or any), args}`.

| Command | Args | Result |
|---|---|---|
| `remote.status` | – | `{enabled, port, listening: ["100.x.y.z:47621"], hosts: ["mac.tailnet.ts.net", "100.x.y.z"], error, computer: {id, name, colour}, titleBarColour, otherMacs: [{name, colour, devices: [deviceName]}], titleDot, push}`; `push` = a push relay is set; `otherMacs` is the union of every device's `macs` by name (sorted by name; the most recently seen device's colour wins), `titleDot` = `titleBarColour` and `otherMacs` is not empty |
| `remote.enable` | `{enabled}` | status |
| `remote.set-port` | `{port}` (1–65535) | status; restarts listening, closes open streams, drops the active pairing code; destructive, confirm "Change the companion port? Paired devices stay offline until you pair them again." |
| `remote.set-colour` | `{colour}` | status; publishes `remote.identity` |
| `remote.set-title-bar-colour` | `{enabled}` | status |
| `remote.pair` | – | `{link, code, expiresAt}` (RFC 3339); enables remote access; fails without issuing a code when there is no address to listen on (e.g. Tailscale is down) |
| `remote.cancel-pair` | – | `null` |
| `remote.devices` | – | `[{id, name, model, pairedAt, lastSeen, connected, macs: [{name, colour}]}]`, newest first |
| `remote.revoke` | `{device}` | `null`; destructive, confirm "Revoke this device's access to Clubmate?" |
| `remote.set-push-relay` | `{url, token}` | status; stores the [push relay](push-relay.md)'s `url` (must be `https://`) and this Mac's relay `token` in plugin storage (`pushRelayURL`, `pushRelayToken`); an empty `url` clears both. On start the plugin deletes the old `apnsKey`, `apnsKeyID`, `apnsTeamID` keys |

The plugin publishes `remote.changed` `{}` when a device pairs, is revoked, connects or disconnects, reports a different `macs` list, when access is turned on or off or the listening addresses change, and when the colour or the title-bar setting changes.

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

## Companion app: plugin screens

- A Mac whose `hello` has `ui: true` gets a fourth segment, **More**, next to Agents, Backlog and Docs on the workspace screen. It lists the workspace sections from `uiSections` by `order`, without the ones the phone has its own screen for (`agents`, `backlog`, `docs`), the terminal-only ones (`terminals`, `editor-nvim`), `editor-vscode` (a loopback page only the Mac can load) and hosted or `rail: false` sections.
- A section opens on its `<section>.sidebar` slot; tapping a row sends the row's event and pushes `<section>.main`. A section without a sidebar view (or one cleared to an `empty` with no text) opens on `main`. The section screen watches both slots while it is open; `status` is not shown.
- The phone does not watch `*.popup`: a popup open on the Mac (a doc in `docs.popup`) stays on the Mac, and nothing the phone does closes it. A doc an agent opens shows on the phone as the "Opened <file name>" banner, which opens the phone's own doc sheet (see [docs viewer § Entry points](docs-viewer.md#entry-points)).

## Companion app: notifications

While its event stream is open the app posts **local** notifications from it; otherwise the Mac sends a [push](#real-push) when it has a push relay set:
- When a `notify.posted` event arrives and the app is not active, it shows a notification for kinds `needs-input`, `permission`, `question`, `stuck` and `done` (not `info`) posted less than an hour ago. The identifier is `<computer id>|<notification id>`. A replayed event with `"pushed": true` (see [real push](#real-push)) goes into the Inbox unread and shows no notification: the push already alerted. Tapping it opens the session (or the Inbox when the notification has no session). A notification posted while the app is inactive (e.g. Control Center pulled over it) is shown as a banner too.
- A notification about an agent session first asks `agents.sessions` for the session's applied notification policy (`notifyEffective`); one the policy holds back goes into the Inbox as read and shows no notification (see [per-session policy](notifications.md#per-session-policy)). This applies to notifications replayed by the background refresh too.
- The app asks for notification permission after the first pairing (or at launch when a Mac is already paired).
- Limit: iOS suspends the app shortly after it goes to the background. The app asks for background time (about 30 s) to keep the stream open; after that the stream is gone and nothing arrives over it until the app is opened again, when `Last-Event-ID` replays the missed events into the Inbox. So without a push relay on the Mac this covers "switched to another app for a moment", not "phone in the pocket".
- Background refresh fallback: on going to the background the app schedules a `BGAppRefreshTask` (`com.clubmate.companion.refresh`, earliest in 15 min). When iOS runs it, the app reconnects to every Mac, replays the events since the last seen `seq` (kept per Mac in `UserDefaults`, so it survives the app being terminated), posts local notifications for what it missed and waits until each Mac is online and the stream has been quiet for 2 s (at most 20 s), then schedules the next run. iOS decides when (and whether) it runs, based on how often the app is used, so alerts can be late by much more than 15 min; it is a fallback, not push. Debug a run on a device from Xcode's debugger with `e -l objc -- (void)[[BGTaskScheduler sharedScheduler] _simulateLaunchForTaskWithIdentifier:@"com.clubmate.companion.refresh"]`.

### Real push
The Mac decides what to push and when, and sends each push to the [push relay](push-relay.md), which holds the APNs key and forwards it to APNs: Mac → relay → APNs. No Mac holds the `.p8`; each Mac has its own relay token.
- The phone sends its APNs device token with the `pushToken` rpc; revoking the device drops it.
- On `notify.posted` of kind `needs-input`, `permission`, `question`, `stuck` or `done` (not `info`, nothing posted over an hour ago; an agent session's permission requests and questions, and a top-level session's turn end as `needs-input`, `done` or `stuck`, arrive this way too, see [notifications](notifications.md#in-the-app)) the `remote` plugin pushes an alert to every paired device that has a token and no open event stream right now (a device with an open stream gets the event over SSE and posts a local notification itself). Only while companion access is on.
- The [per-session policy](notifications.md#per-session-policy) is applied on the Mac the same way the phone does: for a notification about an agent session (`agt_…`) it runs `agents.sessions {session}` in that workspace and reads `notifyEffective`; `muted` sends nothing, `needs-input` only `needs-input`, `permission` and `question`; no answer or an unknown session counts as All.
- Each push is `POST <relay url>/v1/push` with `Authorization: Bearer <relay token>` and `{"token": <device token>, "environment", "type", "collapseID", "payload"}`; the relay sets the APNs headers (`apns-topic: com.clubmate.companion`, `apns-push-type` = `type`, `apns-priority` 10 for alerts and 5 for background).
- Alert: `type: alert`, `collapseID: <notification id>` (first 64 bytes); payload `{"aps": {"alert": {"title", "subtitle", "body"}, "sound": "default", "thread-id": <computer id>}, "id": "<computer id>|<notification id>", "computer", "workspace", "session" (omitted without one), "kind"}`. Title = `<Mac name> · <workspace name>` (empty parts dropped), subtitle = the notification's title or the kind's label (Needs input, Permission needed, Question, Done, Stuck), body = the message. `id` matches the local notification's identifier.
- Replay: the plugin remembers in memory, for an hour, which notifications it pushed to which device (a relay error or a non-200 APNs reply forgets it). When that device reconnects and `Last-Event-ID` replays a `notify.posted` it was pushed, the replayed payload carries `"pushed": true`, so the phone does not post a second, local notification for it (background refresh, a launch by a background push or opening the app). Live events never carry it. After the plugin restarts the record is empty and a replay can alert once more.
- Resolved: on `agents.changed` of kind `resolved` the same devices get a background push (`type: background`): `{"aps": {"content-available": 1}, "resolved": {"computer", "session", "requestKind"}}` (`requestKind` `permission` or `elicitation`), so the phone drops the delivered notification.
- Environment: a device token from a development build (Xcode) goes to `api.sandbox.push.apple.com`, one from a TestFlight/App Store build to `api.push.apple.com`; the phone says which in `environment` and the relay picks the host.
- The relay answers `200 {"status", "reason"}` with APNs' reply. A relayed `410`, or `400` with `BadDeviceToken` or `Unregistered`, clears that device's token; other APNs failures and the relay's own errors (`401`, `429`, `5xx`, unreachable) are logged and the push is dropped. Sends run in the background with a timeout and never hold up the event stream.

Setup (user):
1. A paid Apple Developer Program membership (done).
2. The Push Notifications capability: Xcode's automatic signing adds it to the App ID `com.clubmate.companion` when the app is built with `-allowProvisioningUpdates`.
3. Create an APNs auth key in developer.apple.com → Certificates, IDs & Profiles → Keys (enable Apple Push Notifications service) and download the `AuthKey_<KEYID>.p8`.
4. Deploy the [push relay](push-relay.md#deploy-on-exedev) with that key, then [add each Mac](push-relay.md#adding-a-mac): `token add <mac name>` on the relay host, `clubmate command run remote.set-push-relay url=https://<vm>.exe.xyz token=cmr_…` on the Mac; `remote.status` shows `push: true`.

Without a relay the background refresh fallback above is the only way alerts reach a phone that has been in the pocket for a while.

## Testing on a real iPhone

The simulator covers loopback only. On a device, check:
1. **Phone**: iOS 27 or later (the deployment target), Tailscale installed and logged in to the same tailnet as the Mac. MagicDNS on is recommended.
2. **Signing**: `companion.yml` has `DEVELOPMENT_TEAM: ""`. Build with your team, e.g.
   ```
   cd client && xcodegen generate --spec companion.yml
   xcodebuild -project Companion.xcodeproj -scheme Companion -destination 'platform=iOS,id=<udid>' DEVELOPMENT_TEAM=<team id> -allowProvisioningUpdates build
   ```
   A free/personal team needs a unique bundle id (`PRODUCT_BUNDLE_IDENTIFIER=<your.prefix>.companion`). Find the udid with `xcrun devicectl list devices`; install with `xcrun devicectl device install app --device <udid> <path to Companion.app>`.
3. **Mac**: `clubmate command run remote.status` shows `enabled: true` and a non-empty `listening` with `100.x` / `fd7a:` addresses (and `hosts` with the MagicDNS name).
4. **Pair by camera**: Settings → Remote → "Pair Device…", scan the QR code with the in-app scanner (camera permission prompt appears once). Also try the system Camera app: the `clubmate://pair` link must open the Companion.
5. **First request**: iOS shows the Local Network prompt on the first connection (never in the simulator); allow it. The Macs list shows the Mac online, and `remote.devices` shows the phone `connected: true`.
6. **Agents**: open a session, send a prompt, answer a permission request from the phone.
7. **Revoke live**: with a transcript open, revoke the phone on the Mac. The open transcript and the list switch to "Access Revoked" within a few seconds.
8. **Reconnects**: put the Mac to sleep and wake it; switch the phone between Wi-Fi and cellular. The app reconnects (backoff up to 30 s) and replays missed events.
9. **Notifications**: allow notifications, switch to another app, make an agent ask for permission within ~30 s; a notification appears and opens the session when tapped.
10. **MagicDNS off**: pairing still works via the IP hosts in the link.
11. **Background refresh**: put the app in the background for over a minute (stream gone), make an agent ask for permission, then pause in Xcode and run the `_simulateLaunchForTaskWithIdentifier` command above; the notification appears. Also: Control Center pulled over the app shows a banner for a new notification.
12. **Plugin restart**: with the Agents tab open, restart the `agents` plugin on the Mac (Settings → Agents → Restart); the tab disappears and comes back without reconnecting.
