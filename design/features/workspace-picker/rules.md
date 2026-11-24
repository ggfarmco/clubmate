# Workspace picker (companion capsule menu)

Claude Design round 1, 23 Nov 2026, and round 2 (last used workspace on another Mac in 1 tap), 24 Nov 2026. Canvas "Clubmate · workspace picker": https://claude.ai/artifact/K2F99UwNrVjsXFNUBSucj2. Brief: `design/workspace-picker-brief/`.

Boards: `r2-a-Pairs.dc.html` (`png/r2-a-Pairs.png`) and `r2-Rules.dc.html` (round 2, compares a, b, c; this round's pick is the user's and the designer's), `r1-A-Menu.dc.html` (`png/r1-A-Menu.png`), `r1-Rules.dc.html` (compares A, B, C; its "pick C" is the designer's, the ruling below wins), all in `boards.html`. Not picked: `rounds/` (round 1: B · Mac cards sheet, C · Mac strip sheet, their light board, the iPad board; round 2: `r2-b-Recent` (a Recent section on top), `r2-c-Sections` (a section per Mac with "All N workspaces"), and `r2-Today` (the seed shots of the menu in main)). `canvas.json` is a snapshot of the canvas's layout file; it lists boards that are not in this folder (`Main`, the `C-*` language boards, r1 B and C at the top level), so it does not match the folder layout and is kept only as a record of the canvas.

## Ruling

| Decision | By |
|---|---|
| Direction **A · Refined system Menu**: the capsule keeps a system `Menu`; it changes shape and shows status per workspace. Cost S, all system. | user (designer picked C) |
| One NEW field per workspace in the workspace list, `activity`, decided in the Go plugin | designer, required by CLAUDE.md (meaning lives in the plugin) |
| Round 2 · direction **a · Last used under each Mac**: each Other Macs row keeps its submenu, and that Mac's last used workspace sits right under it as a direct row. 1 tap to the last used workspace on another Mac (was 2). Cost S, no NEW field. The user: "show the last used workspace from another Mac, so I can switch in 1 tap, not in 2". | user and designer |

## Menu layout

1. **Top row**: a `ControlGroup` with two buttons, `Mac Details` (`info.circle`, the current workspace's Mac) and `Pair a Mac` (`qrcode.viewfinder`). Always first, so never among the workspaces and never cut.
2. **Other Macs** (only with 2 or more Macs): section header "Other Macs", then one pair of rows per other Mac, each pair a group of its own (a divider between pairs), in the order of today: (1) the Mac row, (2) that Mac's last used workspace (§ Last used). The Mac row is a submenu row: label = Mac name, glyph `laptopcomputer` when `hello.computer.model` is `laptop`, else `desktopcomputer` (also for a Mac that does not send `model`), subtitle = that Mac's summary (sum of its workspaces' `activity`, same text rule as a row), or "offline · as of HH:mm". The submenu lists Home, then its workspaces with the same row rules, then `Mac Details` last. The last used row: the same row as in that Mac's submenu (status glyph, workspace name cut as below, subtitle by the same rule), a `Toggle` that is never on (it is never the current workspace); a tap switches to that workspace. The workspace stays in the submenu too.
3. **This Mac**: section header "{Mac} · online" (the count "(N)" is dropped), then Home, then each workspace in the order the Mac sends.

## Row

- Label = workspace name, one line. The system `Menu` wraps a long title (it ignores `.lineLimit` and `.truncationMode` on iOS 27), so CompanionKit cuts a name longer than 18 characters to its first 17 and "…" (`WorkspaceSummary.rowName`; "infra-terraform-s…" fits one line of an iPhone menu at the default text size). Subtitle = one line, the state text below.
- Larger Dynamic Type (checked 24 Nov 2026, iOS 27, `design/workspace-picker-brief/check-1.html`): the cut stays at 18 characters for every size, because the system draws the menu and one limit cannot keep a line at every size. At Large and on the iPad (list shown or hidden) every name and subtitle is one line. At XXL the menu keeps its width and the text wraps, even "MacBook Pro" and the subtitles: 2 lines are accepted. From AX3 the system draws a full-width list without glyphs: one line again, the checkmark stays, the status glyph is not drawn (the subtitle keeps the state text). Not checked: XL, XXXL, AX1, AX2, and names of wide letters ("MMMM…" can take 2 lines at Large).
- Glyph = status (monochrome SF Symbols). Several at once: needs you › failed › running › idle picks the glyph.

| Status | SF Symbol | Subtitle part |
|---|---|---|
| Needs you | `hand.raised` | `{n} waiting` |
| Failed | `xmark.circle` | `{n} failed` |
| Running | `play` | `{n} running` |
| Idle | `folder` | `idle · {age}` (age: now, 5 min, 3 h, 2 d, 1 w) |
| Home | `house` | the same text rule |
| Current | system checkmark in front of the glyph (each row is a `Toggle`, on for the current workspace) | status stays in the subtitle |

- Subtitle order: `{n} waiting · {n} failed · {n} running`, only the parts above zero; with none, `idle · {age}`; without `lastActiveAt`, no subtitle.
- Offline Mac: rows stay enabled and are not dimmed (a `Menu` dims only by disabling); the text carries the state: `as of HH:mm · {state}`. "as of" is the time the phone last received that Mac's state (phone-local, `ComputerConnection.lastSeen`).
- VoiceOver: the system reads the row title and subtitle with the selected trait, for example "selected, shop, 2 waiting · 1 running". A `Menu` row ignores `.accessibilityLabel` on iOS 27 (tried on the `Button`, the `Label`, a `Picker` option), so "current workspace" is the selected trait of the `Toggle`, not text. A `Picker` also gives the selected trait but drops the subtitle and the section header, so the rows are `Toggle`s. The XCUITest `testCapsuleRowReadsSelectedNameAndState` checks the label and the selected state.
- No Mac colour, amber, red or dimming inside the menu (a system `Menu` cannot draw them); the capsule dot keeps the Mac colour.

## Last used (round 2)

- Meaning: per Mac, the workspace that was last current on this phone while the user was on that Mac. It is the phone's own navigation, not the Mac's view: the workspace last active on the Mac itself is not used (it would need a NEW plugin field, and the user asked for where they were).
- Set when a workspace becomes current on this phone: a pick in the capsule menu or a submenu, a notification, an Inbox row, a deep link. Opening a submenu without a pick sets nothing.
- Shown for every Mac except the current one (the current Mac's workspace is the checked row in its own list).
- Stored on the phone only: key = the Mac's pairing ID (`computer.id`), value = the workspace id (Home included), beside the saved tab stacks (`TabStacks` in `UserDefaults`). It survives a relaunch, is not synced, and is removed with Forget this Mac.
- None yet (a new pairing), or the workspace is no longer in that Mac's list: the row shows that Mac's Home.
- 1 Mac: no other Macs, so no last used row; the menu is the same as round 1.
- Offline Mac: the Mac row "offline · as of HH:mm"; the last used row stays enabled, subtitle "as of HH:mm · {state}" (the same rule as other rows).
- Long names: the Mac name stays in its own row, never in a subtitle, so nothing new is cut; both labels use the 18-character cut.
- VoiceOver: swiping reads the Mac row first ("MacBook Pro, 2 waiting · 1 running", submenu), then the last used row ("infra, 2 waiting · 1 running"). The last used row does not say the Mac (a `Menu` row ignores a custom label); the row before it does.
- Taps after the capsule: the last used workspace on another Mac 1; any other workspace there 2 (the same path as before); back to the Mac the user left 1, because its last used row is the workspace they left.
- Accepted weak spots: +1 row per other Mac moves this Mac's list down (2 rows with 3 Macs); when only that workspace is busy, the Mac row and its last used row show the same subtitle.

## iPad

The same menu from the capsule when the sidebar is hidden. The sidebar keeps its own workspace rows; it may take the same glyph and subtitle rules (same field), nothing else changes.

## Fields

| Field | State | Use |
|---|---|---|
| `mac.name`, presence | today | section header, submenu subtitle |
| `workspace.name`, `.home` | today | row label, Home first (`Workspace.homeFirst`) |
| `workspace.activity.needsYou` | NEW | `{n} waiting`, hand glyph |
| `workspace.activity.running` | NEW | `{n} running`, play glyph |
| `workspace.activity.failed` | NEW | `{n} failed`, ✕ glyph; the plugin decides what counts (e.g. ended in an error and not opened since) |
| `workspace.activity.lastActiveAt` | NEW | `idle · {age}`; no source today |
| `hello.computer.model` | NEW, optional: `laptop` (the Mac has an internal battery) or `desktop`, found once at plugin start | laptop or desktop glyph on Other Macs rows |
| last used workspace per Mac | phone-local, round 2 | the last used row under each other Mac; no plugin field |
| `agents.sessions[].needsYou` | today | fallback only, for a Mac whose plugin does not send `activity` yet (today's `CompanionModel.waitingCount`) |

Shape: `workspaces: [{"name": "shop", "home": false, "activity": {"needsYou": 2, "running": 1, "failed": 0, "lastActiveAt": "2026-11-23T08:38:00Z"}}]`. Why in the workspace list: the picker shows up to 30 workspaces and must not load 30 session lists; what counts as waiting, running or failed is decided once in Go (`docs/agents.md` § Needs you, § state map). Mac totals are sums on the phone and add no meaning.

## Handoff

- (a) companion SwiftUI: `client/Companion/App/WorkspaceCapsule.swift` (ControlGroup row, Other Macs submenus, glyph and subtitle rules, VoiceOver labels); the subtitle text built from the field, not from sessions.
- (c)/(d) plugin + core: the `activity` object per workspace in the `workspaces` list the phone gets (pull only, `docs/remote.md` § Wire contract); the phone asks for `workspaces` again on `agents.changed` to keep it current; `computer.model` in `hello` (optional); `docs/agents.md` and `docs/remote.md` sections in the same commit.
- Round 2 (a), companion only: `Navigation.workspaces` (`client/Companion/App/Navigation.swift`) already holds a workspace per Mac, but only in memory (filled at launch from the current stack), and `workspace(for:)` falls back to the first workspace in the list, not Home. Store it beside `TabStacks`, fall back to Home (`Workspace.homeFirst`), remove it on Forget this Mac, and add the last used row under each Mac row in `WorkspaceCapsule.swift`; a test restarts the app's navigation and reads the value back.
- Weak spots the user accepted: no colour inside the menu; past ~9 rows it scrolls, and the current workspace can sit below the fold (a `Menu` does not scroll to its checkmark).
