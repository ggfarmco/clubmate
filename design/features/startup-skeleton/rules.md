# Startup skeleton — designer's ruling, 22 Nov 2026

Claude Design round 1, canvas "Clubmate · Startup skeleton" (https://claude.ai/artifact/PqiJvgogi3py4ZHcwDAGaB, page r1), chat https://claude.ai/chat/30857035-230c-4019-84bf-7a453b61766d. Brief: `design/startup-skeleton-brief/`.

Boards (`.dc.html`, PNG in `png/`, all in one page `boards.html`): `R1-A-Remembered-layout` (Agents and Code + chat at 1280 dark, Agents at 640 light, Welcome, shapes per kind), `R1-Timeline-A` (0 ms → ready, slow, failure, reconnect, Welcome), `R1-Rules-A` (states, timings, texts, placeholders, changes). Not picked: `rounds/R1-B-Neutral-frame`, `rounds/R1-C-Remembered-geometry`. `R1-Timeline-A` imports the language board `Agents-Mate-1280`; to render it outside the canvas run `design/startup-skeleton-brief/inline-imports.py` first (see `design/startup-skeleton-brief/README.md`).

**Status:** approved by the user on 22 Nov 2026: direction A, timings 5 s / 15 s, Retry ⌘R. Not built.

## Ruling: A · Remembered layout

The window opens as it was last time. The rail icons, the selected section and the pane layout come from a small client cache; each pane shows placeholder shapes of its kind. No text before 5 s. At ready the content cross-fades in.

Why A, not B (no cache) or C (geometry only):

1. The rail shows where ⌘1–⌘9 go from the first frame.
2. No layout jump at ready: the sidebar, the chat column and their widths are already right. B jumps when the sidebar was hidden or the chat column was shown.
3. A terminal pane stays plain background; B and C draw list rows that nvim then replaces.
4. C needs the same cache as A and gives less.
5. Cost: client only, about 1 KB per window. If a plugin changed since the last run, one icon changes during the fade.

## States

| State | When | Window | Status bar |
|---|---|---|---|
| Skeleton | window shown, core not ready, t < 5000 ms | real layout from the cache, placeholders per pane kind, no text, nothing moves; rail clicks and ⌘1–⌘9 are kept and applied at ready | shapes only |
| Slow | t ≥ 5000 ms | skeleton unchanged | mini progress + "Starting core…" (core does not answer) or "Connecting to workspace…" (core answers, this window's store not connected) |
| Long | t ≥ 15000 ms | skeleton unchanged | "Still starting core…" + Show log ⌘L |
| Ready | core connected and this window's first SDUI tree arrived | cross-fade 180 ms ease-out; panes ready within 400 ms of the first fade with it; a later pane keeps its skeleton and fades alone | "● core", as today |
| Failed at start | `.failed(message)` before ready | chrome stays, placeholders go; the message in danger ink, selectable, at the top of the main content column; Retry ⌘R (primary) and Show log ⌘L | danger dot + "core didn't start" |
| Reconnecting | `.reconnecting(attempt:)` after ready, lasting ≥ 500 ms | last real content at full ink, never dimmed, never back to the skeleton; scroll, select, copy, find work; actions that go to the core are disabled with the tooltip "Waiting for the core"; the composer keeps its draft, Send disabled | warning dot + "core reconnecting (attempt n)"; from attempt 3: Retry now |
| Reconnect failed | reconnect gives up | as Reconnecting | danger dot + "core stopped" + Retry ⌘R |
| Welcome | a window with no workspace | "Clubmate", Open folder… ⌘O, New terminal ⇧⌘N are real and work at once; Recent: the real label + placeholder rows (cached count); the slow text and the failure take the Recent slot; the block is top-anchored | no status bar |

First launch or no cache: the "unknown" kind (list sidebar + list main) and plain squares in the rail, as `rounds/R1-B-Neutral-frame`.

## Timings

| What | ms |
|---|---|
| skeleton: in the window's first frame | 0 |
| fast path: ready ≤ 250 ms after the window appeared → plain switch, no fade | 250 |
| slow text (the test app is ready at 3600; 3000 would show the text on most launches) | 5000 |
| long text + Show log | 15000 |
| hand-over cross-fade, ease-out | 180 |
| group: panes ready within this of the first fade with it | 400 |
| reconnect: status text only after | 500 |
| Retry now during reconnect | from attempt 3 |
| Reduce Motion: every fade is a plain switch; the mini progress is the text alone | 0 |

## Texts

| Text | Where, when |
|---|---|
| Starting core… | status bar (Welcome: Recent slot), ≥ 5000 ms, core does not answer |
| Connecting to workspace… | status bar, ≥ 5000 ms, core answers, store not connected (replaces "Connecting…") |
| Still starting core… | status bar / Recent slot, ≥ 15000 ms |
| Show log ⌘L | ≥ 15000 ms and on failure |
| `.failed(message)` | as today, danger ink, selectable |
| Retry ⌘R | failure (primary), reconnect failed |
| core didn't start | status bar on failure, danger |
| core reconnecting (attempt n) | status bar, warning ink (as today) |
| Retry now | status bar, reconnect attempt ≥ 3 |
| core stopped | status bar, danger, reconnect gave up |
| Waiting for the core | tooltip on disabled actions while reconnecting |
| Starting core | VoiceOver label of the skeleton (one element); the slow text is announced once |
| workspace name | window title (Window menu, Mission Control) from the cache, else "Clubmate"; not drawn in the skeleton |

Gone: the centred "Starting core…" and "Connecting…" in `EmptyNodeView`.

## Placeholders

| Element | Rule |
|---|---|
| bar | `pressed` role (fg 11 % overlay), as the Components loading row; height 8, radius 4; title bar 10, radius 5 |
| icon | 12×12, radius 3; rail without cache 16×16, radius 4 |
| chip | height 16, radius 8 |
| cards | the real card (`surface2` + border) with placeholders inside; the composer card empty |
| rail from cache | icons in `faint` ink; the selected one gets the `hover` fill, no accent bar until ready |
| buttons | none drawn: actions come with the content |
| widths | fixed per kind, never random |
| never | accent, tones, shimmer, gradient, text |

Why an overlay and not `surface2` bars: `surface2` on `surface1` is a 3 % step and is not visible in Mate dark; one overlay reads the same on the rail, the sidebar and the background panes.

## Kinds → shapes

The kind of a pane is the root node type of the last SDUI tree it showed, cached on the client. No plugin declares anything.

| Kind | Shape |
|---|---|
| list | header bar + 2 icon squares (Agents: 3 filter chips); section label bar; rows: icon + bar, indented by the cached depth |
| transcript | header bar + meta bar; user block on `surface1`; plan card with 4 rows; 3 action rows; paragraph of 3 bars; empty composer card |
| detail | icon + heading bar; 3 property rows (key / value bars); section label + `surface1` block |
| table | header row at 70 %; rows of column bars on the real column grid |
| terminal | plain background, nothing drawn (the terminal paints itself in the user's theme) |
| unknown | list sidebar + list main, rail squares |

## Launch cache

One record per window, written after each layout change once ready; JSON in Application Support, about 1 KB; dropped when the app version or the plugin set changes.

- sections: id, icon, kind (per pane), selected
- sidebar: shown per section, width, side
- chat column: shown, width
- workspace name
- Recent row count (Welcome)

## Changes, tagged

(a) client SwiftUI · (b) SDUI node or prop · (c) plugin content · (d) new plugin or core logic. Result: 9 × (a), 0 × (b), (c), (d).

| # | Change | Files and functions |
|---|---|---|
| 1 | Skeleton views for the five kinds + unknown, shown before any SDUI arrives; replaces `EmptyNodeView(text:)` in the connecting branch | new `client/Clubmate/Windows/Workspace/StartupSkeleton.swift`; `WorkspaceView.content` and `placeholder` (`client/Clubmate/Windows/Workspace/WorkspaceView.swift:95-112`); `WelcomeView.body` (`client/Clubmate/Windows/WelcomeView.swift:14-30`) |
| 2 | Launch cache per window (fields above) | new `client/ClubmateKit/Sources/ClubmateKit/LaunchCache.swift` beside `WindowLayout.swift`; written from `WorkspaceUIStore` (`adopt(_:)` `:124`, `selectedSection`, `chatWidth`, `sidebarSlot` `:250`, `columnShown` `:259`, `restore(from:)` `:458`); tests in `ClubmateKitTests` |
| 3 | Pane kind = root node type of the last tree each slot showed, stored with #2 | `WorkspaceUIStore` where it replaces a slot's tree (`replace(with:)`) |
| 4 | All windows from `WindowLayout` open at launch in the skeleton, not after the core lists workspaces; a workspace the core no longer lists closes its window as today | `WindowRouter.restore(_:)` (`client/Clubmate/App/WindowRouter.swift:169`), its call in `AppModel` (`client/Clubmate/App/AppModel.swift:275`), `WindowGroup` in `client/Clubmate/App/ClubmateApp.swift:28` |
| 5 | Status bar start-up slot: client phases (core not answering · core answers, store not connected) and the 5 s / 15 s timers; new texts | `ConnectionStatus` and `StatusBar` (`client/Clubmate/Windows/StatusBar.swift`); `WorkspaceLayout.connection` (`client/Clubmate/Windows/Workspace/WorkspaceLayout.swift:158`); `ConnectionSupervisor.Phase` (`client/ClubmateKit/Sources/ClubmateKit/ConnectionSupervisor.swift`) |
| 6 | Show log ⌘L opens the core log | the core already writes `<state>/core.log` (`internal/paths/paths.go:202`, `CoreLog()`); open that file, do not add a new log (the board's `~/Library/Logs/Clubmate/core.log` is replaced by this) |
| 7 | Hand-over: per-window cross-fade with the 400 ms group and the 250 ms fast path; Reduce Motion switches | `WorkspaceLayout`, `StartupSkeleton.swift` |
| 8 | ⌘1–⌘9 and rail clicks during the skeleton are kept and applied at ready through the section selection | `WorkspaceUIStore.select(_:)`, the rail in `WorkspaceLayout` |
| 9 | Reconnecting: no dispatch of core-bound actions, their controls disabled with "Waiting for the core"; scroll, selection, find stay; Retry now resets the backoff | `ConnectionSupervisor.retry()`, `AppModel.retry()` (`client/Clubmate/App/AppModel.swift:171`), `StatusBar` |

Keys: Retry ⌘R and Show log ⌘L are new app keys, active only in the failure and slow states; ⌘R is also a plugin key (`docs/shortcuts.md`: cron/triggers Run now, agents Open review), and no plugin view is on screen in those states. The build commit adds both to `docs/shortcuts.md`.

Not covered by this round: the iPhone/iPad Companion start-up (it has no local core start); a skeleton there is a separate question.
