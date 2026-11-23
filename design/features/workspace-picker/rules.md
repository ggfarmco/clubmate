# Workspace picker (companion capsule menu)

Claude Design round 1, 23 Nov 2026. Canvas "Clubmate · workspace picker": https://claude.ai/artifact/K2F99UwNrVjsXFNUBSucj2. Brief: `design/workspace-picker-brief/`.

Boards: `r1-A-Menu.dc.html` (`png/r1-A-Menu.png`), `r1-Rules.dc.html` (compares A, B, C; its "pick C" is the designer's, the ruling below wins), both in `boards.html`. Not picked: `rounds/` (B · Mac cards sheet, C · Mac strip sheet, their light board, the iPad board). `canvas.json` is a snapshot of the canvas's layout file; it lists boards that are not in this folder (`Main`, the `C-*` language boards, r1 B and C at the top level), so it does not match the folder layout and is kept only as a record of the canvas.

## Ruling

| Decision | By |
|---|---|
| Direction **A · Refined system Menu**: the capsule keeps a system `Menu`; it changes shape and shows status per workspace. Cost S, all system. | user (designer picked C) |
| One NEW field per workspace in the workspace list, `activity`, decided in the Go plugin | designer, required by CLAUDE.md (meaning lives in the plugin) |

## Menu layout

1. **Top row**: a `ControlGroup` with two buttons, `Mac Details` (`info.circle`, the current workspace's Mac) and `Pair a Mac` (`qrcode.viewfinder`). Always first, so never among the workspaces and never cut.
2. **Other Macs** (only with 2 or more Macs): section header "Other Macs", one submenu row per other Mac: label = Mac name, glyph `laptopcomputer` when `hello.computer.model` is `laptop`, else `desktopcomputer` (also for a Mac that does not send `model`), subtitle = that Mac's summary (sum of its workspaces' `activity`, same text rule as a row), or "offline · as of HH:mm". The submenu lists Home, then its workspaces with the same row rules, then `Mac Details` last.
3. **This Mac**: section header "{Mac} · online" (the count "(N)" is dropped), then Home, then each workspace in the order the Mac sends.

## Row

- Label = workspace name, one line. The system `Menu` wraps a long title (it ignores `.lineLimit` and `.truncationMode` on iOS 27), so CompanionKit cuts a name longer than 18 characters to its first 17 and "…" (`WorkspaceSummary.rowName`; "infra-terraform-s…" fits one line of an iPhone menu at the default text size). Subtitle = one line, the state text below.
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
| `agents.sessions[].needsYou` | today | fallback only, for a Mac whose plugin does not send `activity` yet (today's `CompanionModel.waitingCount`) |

Shape: `workspaces: [{"name": "shop", "home": false, "activity": {"needsYou": 2, "running": 1, "failed": 0, "lastActiveAt": "2026-11-23T08:38:00Z"}}]`. Why in the workspace list: the picker shows up to 30 workspaces and must not load 30 session lists; what counts as waiting, running or failed is decided once in Go (`docs/agents.md` § Needs you, § state map). Mac totals are sums on the phone and add no meaning.

## Handoff

- (a) companion SwiftUI: `client/Companion/App/WorkspaceCapsule.swift` (ControlGroup row, Other Macs submenus, glyph and subtitle rules, VoiceOver labels); the subtitle text built from the field, not from sessions.
- (c)/(d) plugin + core: the `activity` object per workspace in the `workspaces` list the phone gets (pull only, `docs/remote.md` § Wire contract); the phone asks for `workspaces` again on `agents.changed` to keep it current; `computer.model` in `hello` (optional); `docs/agents.md` and `docs/remote.md` sections in the same commit.
- Weak spots the user accepted: no colour inside the menu; past ~9 rows it scrolls, and the current workspace can sit below the fold (a `Menu` does not scroll to its checkmark).
