# Home workspace — designer's ruling, 22 Nov 2026

Claude Design round 1, canvas "Clubmate · Home workspace" (https://claude.ai/artifact/KgyEvkpPMwwVdiwymsy2W5, page r1), chat https://claude.ai/chat/23e4d8d6-ff9b-4357-9bb9-58223b53b0ce. Brief: `design/home-workspace-brief/`. Spec: `clubmate-notes/superpowers/specs/2026-11-22-home-workspace-design.md` § Clients.

Boards (`.dc.html`, PNG in `png/`, all in one page `boards.html`): `R1-Switcher` (Mac switcher, (a) picked, (b) drawn small), `R1-Recent` (Welcome + File › Open Recent), `R1-Phone` (iPhone + iPad capsule menu), `R1-Empty` (empty Home on the Mac), `R1-EmptyPhone` (empty Home on the iPhone), `R1-Rules` (ruling, glyph, every text, change tags).

**Status:** approved by the user on 22 Nov 2026, with the answers in § User's answers. Not built.

## Behaviour: (a), not (b)

Home is first in every list, and a window can still close it. The window model does not change (cost S).

1. The user's rule: Home is not a different kind of workspace. (b) gives Home its own window lifecycle (auto-open, restore, a refused close). A tile that ignores ⌫ is a different kind of workspace.
2. "Always there" is about finding Home, not about Home being open. Home is first in Open Recent, first on Welcome, first under each Mac on the phone, and ⇧⌘H opens it from anywhere. No cap, no Clear Menu and no trash can lose it.
3. Windows are for focus. A window for shop does not carry Home's tile, terminals and agents.
4. One rule fewer: × and ⌫ do the same on every tile.

What (a) gives up: a window where Home was never opened has no Home tile. No ghost tile (that is (b) by another name); ⇧⌘H opens Home in the current window.

## The look

- **Glyph:** SF Symbol `house`, outline, muted, before the name. Mac: 12 pt in tile labels and the title bar, 14 pt in Welcome and Open Recent; the name stays fg. Phone and iPad: `Label("Home", systemImage: "house")`; the system checkmark replaces it when selected, as it replaces `folder` today. Never `house.fill`, never accent or a tone, never the gourd mark, never "~". The glyph stays when Home is renamed.
- **No pin, no badge, no "always" or "can't be removed" text** anywhere. The core refuses removal.
- **Mac switcher (⌃⌘W):** a normal 240×150 tile with a live thumbnail, first when Home is open in this window; × and ⌫ close it from this window as any tile; × tooltip "Close from this window" on every tile. "Open folder…" stays last.
- **File › Open Recent:** item 1 "Home" with the `house` image and ⇧⌘H, then a separator, then up to 10 recents (Home does not count), then Clear Menu. Clear Menu clears only the recents. Other items get no image.
- **Welcome:** a one-row group above "Recent": the glyph in the left margin, "Home", `~/.clubmate/home`, "open" when open in a window, the ⇧⌘H keycap, never a trash. Drawn also when Recent is empty. ⏎ or click opens it in this window. "New terminal ⇧⌘N" is unchanged (Home with a terminal in `$HOME`).
- **⌘K palette:** "Open Home ⇧⌘H", always listed.
- **iPhone and iPad capsule menu:** in each Mac section the `home: true` workspace is the first row, then the rest in core order, then Mac Details; Pair a Mac last. Subtitle "n waiting" only when n > 0. The capsule reads "● Home ⌄"; the dot is the Mac's identity colour, so two Homes on two Macs are told apart by the dot and the section. A Mac whose core sends no Home gets no Home row.

## Empty Home

Same `empty` nodes as a project; one text changes per section. The phone shows the same words without keycaps; the action is a capsule.

| Section | Part | Home | Project today |
|---|---|---|---|
| Agents | sidebar | No agent sessions | same |
| Agents | title | Start a session about anything | Start an agent session with + New (the only text) |
| Agents | text | Agents in Home can read every workspace's backlog, sessions and wiki, and never change them. Try "What waits for me today?" | none |
| Agents | action | New session ⌘N (the header's + action) | none |
| Backlog | title | Nothing in the backlog | same |
| Backlog | text | Tasks are ideas and todos outside your projects, for you or for an agent. Start agent hands a task to a session; it owns the task until it ends or you take it back. | "Tasks are ideas and todos, for you or for an agent. …" |
| Wiki | title | No wiki pages yet | same |
| Wiki | text | Agents write here as they work: plans, lists and notes for your life outside projects. | Agents write here as they work: how things work, why they were decided, examples. You can add a page too. |
| Wiki | action | the project's New page (kept as is) | New page |

Gone: "Agents aren't available in the home workspace", "Open a project workspace to see its wiki.", and "~" everywhere. "What waits for me today?" is text only: not prefilled, not sent.

## Changes, tagged

(a) client SwiftUI · (b) SDUI node or prop · (c) plugin content · (d) new plugin or core logic. Result: 7 × (a), 0 × (b), 3 × (c), 0 × (d).

1. (a) Put the `home: true` workspace first: switcher (when open in this window), Open Recent, Welcome, phone menu per Mac section. Mac, phone.
2. (a) Stop hiding Home in Open Recent and Welcome (`Workspace.recents`, `client/ClubmateKit/Sources/ClubmateKit/SDUI.swift:42`); show the core's name. Mac.
3. (a) Open Recent: Home as its own item + separator, outside the 10-item cap; Clear Menu clears only the recents (`client/Clubmate/App/ClubmateApp.swift:145-175`). Mac.
4. (a) Welcome: the Home row group above Recent (`client/Clubmate/Windows/WelcomeView.swift`). Mac.
5. (a) `house` glyph in the tile label, title bar, Open Recent item and Welcome row (`client/Clubmate/Windows/SwitcherGrid.swift`); `systemImage: "house"` on the phone row (`client/Companion/App/WorkspaceCapsule.swift`). Mac, phone.
6. (a) ⇧⌘H "Open Home" opens Home in the current window: the key on Open Recent item 1 and in ⌘K. Updates `docs/shortcuts.md`. Mac.
7. (a) Switcher × tooltip "Close from this window" on every tile. Mac.
8. (c) Agents plugin: in Home, the empty title, text and the one action New session ⌘N.
9. (c) Backlog plugin: in Home, sentence 1 of the empty text.
10. (c) Wiki plugin: in Home, the empty text.

## Not doing

- A Home tile in windows where Home is not open.
- Folder icons on the other recents.
- A "pinned", "always" or lock badge.
- Prefilling or sending "What waits for me today?".
- A Home row in the phone menu built by the client when the core sends none.

## Facts for the designer's open points (checked in the repo, 22 Nov)

- ⇧⌘H is free: `docs/shortcuts.md` has no ⇧⌘H.
- A project's wiki empty state has a primary New page action (`plugins/wiki/view.go:369-370`); Home keeps it.
- A project's Agents main pane is a plain `sdui.Empty(pickSession)` (`plugins/agents/ui.go:324`; `pickSession` in `plugins/agents/view.go:20` reads "Start an agent session with + New"): no title, no action.

## User's answers (22 Nov)

- Home keeps its own empty-state words (changes 8–10). The three `home` checks only pick words; no feature is switched off.
- A project's Agents empty state also gets the one action New session ⌘N, so Home and a project match; the project's text stays.
- The switcher's last tile (today "Open folder…", the folder dialog at once) reads "Open…" and shows the start-up workspace picker (`WelcomeView`: the Home row, Recent, Open folder…) in place of the tiles inside the same panel; Esc goes back to the tiles; a pick opens the workspace in this window and closes the switcher. This replaces "Open folder… stays last" above.
- The empty-phone board puts Wiki under More; since device-ux round 2 the Wiki is its own tab when the Mac offers it. Only the place differs; the words are the same.
