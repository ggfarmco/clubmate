# Clubmate Companion — iPhone · iPad · Fold

## png/  (2× exports)
- `C-Audit` — 18 problems from the simulator shots, ranked by mis-tap risk
- `C-Language` — accent decision (Mate green), colour roles dark/light, phone deltas, width breakpoints
- `C-Components` — session row, transcript nodes, permission request + Always-allow confirm, question, composer, inbox, empty/offline
- `C-Handoff` — (a) companion SwiftUI · (b) data needed from the `remote` plugin · (c) shared with Mac renderers · open questions
- iPhone 393×852: `C-Macs`, `C-Workspace`, `C-Permission`, `C-Question`, `C-Inbox` (dark) and `*-Light`
- `C-iPad` — split view, three columns · `C-Fold` — Fold open (assumed ~4:3), two columns
- `C-MacColour` — a colour for each paired Mac (spec) · `C-MacInfo` (+ `-Light`) — the picker on the iPhone
- `Mac-Settings-Remote`, `Agents-Mate-1280` — the Mac side: picker in Settings → Remote, dot in the title-bar pill

## html/
Same boards as standalone HTML; open in a browser. `assets/c.css` holds the companion roles (`.th-cdark` / `.th-clight`) on top of `assets/cm.css`; the Mac colour tokens (`--id-*`) are in `cm.css`.

## source/
Raw canvas files as they live in the Claude Design artifact. `*-Light.dc.html` import the dark board with `theme="clight"`.
