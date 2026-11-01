# Clubmate — design check: implementation vs boards

Paste this into the Claude Design project that holds the Clubmate boards (`design/language`, `design/companion`). Attach `mac/*.png` and `companion/*.png` from this folder.

## 1. Your task

The boards you made (Mac design language, companion adaptation, Mac colours) are now implemented. Each screenshot here is named after the board it should match. Check the build against the boards:

1. **Per screen**: list every visible divergence from its board, ranked by impact (wrong state or meaning > hierarchy > spacing/type > colour nuance). For each say:
   - (a) token / renderer change in the native client (one place, all views),
   - (b) needs an SDUI prop or node (say which),
   - (c) plugin content (words, grouping, order),
   - (d) the board should change instead (the build is better, or the board is impossible with the constraints below).
2. **Cross-screen**: inconsistencies between Mac and phone, or between screens on one client, that a single board doesn't show.
3. **Verdict per board**: matches · minor drift · needs work.
4. **Updated boards** only where (d) applies or where a divergence needs a picture to be fixed.

Ignore differences caused by seed data (names, counts, text) unless the structure differs.

## 2. Capture conditions

- Mac: invisible test instance at 2×, seeded with a fake agent, backlog, cron, triggers, a graph run and terminals. Details per file in `mac/notes.md`.
- Phone: iPhone 18 Pro simulator (393×852), fixtures, status bar 9:41, dark and light; iPad for `C-iPad`. Details in `companion/notes.md`.
- Mac themes match the boards: `*-1280` in Kanagawa Wave (dark), `*-640` in Catppuccin Latte (light). The window can't go under 800pt, so every `*-640.png` is really **800pt wide** (1600×1600 px): the sidebar stays and the 640 layout without a sidebar never shows. `Agents-Mate-1280` is not captured (the Mate theme is only a fallback when Ghostty gives no palette). Settings, the approval sheet and popovers follow the system appearance, not the theme.
- The phone shots are iPhone 18 Pro at 402×874pt (3×), not the boards' 393×852 (2×). `C-iPad` is iPad Pro 13" in portrait (the headless simulator can't rotate). `C-Fold` is not captured (no foldable simulator).
- Window chrome: traffic lights are grey because the test window is never key; ignore that.

## 3. Decisions that overrule the boards (don't flag these)

- **Failed tool calls stay folded** on the Mac and the phone: the failure shows as the danger title and border; the output opens on tap like any card (the boards had "failures expand").
- **The Mac window's minimum width is 800pt**: no < 800 breakpoint exists. For the 640 boards, check only what applies at 800pt: the Code chat panel stacked below 1100pt, and the header overflow menu under 480pt.
- Diff line backgrounds use the 15% tone fill, not 13%.
- Backlog autosaves; a ghost "✓ Saved ⌘S" sits where Save was, not top-right of the header.
- The phone's accent is Mate green; a Mac's colour shows only with two or more paired Macs.

## 4. Known gaps (already tracked — confirm, don't re-derive)

- Graphs: no node/edge diagram at ≥ 800pt, no turns progress bar, no relative start time, no Edit definition, no Definitions sidebar section, no per-node state icons, no Copy id.
- Processes: ⌘C on Copy command, `/` and search glyph on Filter, filter beside the title.
- Code chat: no line/symbol chip, doc icon sits before the file chip, status glyph has no tone, stacked panel not resizable, no collapse chevron.
- Agents sidebar: the graph run row has no `turn N of M` and doesn't open Graphs.
- Plan card caption is `Plan · <done> of <all>` on one line (board puts the count at the right end).

## 5. Screens

### Mac (`mac/`, boards in `design/language/png/`)

| File | Board | Shows |
|---|---|---|
| `Agents-1280.png` | Agents-1280 | Sessions tree with a graph run, plan, action group with an expanded edit, reply, docked permission, composer |
| `Agents-640.png` | Agents-640 | Same at 800pt, Latte |
| `Code-1280.png` | Code-1280 | Neovim with the chat panel on the right (graph Reviewer session) |
| `Code-640.png` | Code-640 | Chat panel stacked under the editor, Latte |
| `Backlog-1280.png`, `Backlog-640.png` | Backlog-* | Tree by status, task detail with header, properties and autosave |
| `Cron-1280.png` | Cron-1280 | Job detail |
| `Triggers-640.png` | Triggers-640 | Graph edge trigger detail (`x-Triggers-1280.png` is the same at full width) |
| `Graphs-1280.png`, `Graphs-640.png` | Graphs-* | A review-loop run with the Coder waiting |
| `Processes-1280.png`, `Processes-640.png` | Processes-* | All processes grouped by owner, totals strip |
| `Mac-Settings-Remote.png` | Mac-Settings-Remote | Settings → Remote with This Mac and its colour |
| `x-Title-Dot-1280.png` | Agents-Mate-1280 (pill only) | The Mac's colour dot in the title pill |
| `x-Palette.png`, `x-Switcher.png`, `x-Worktree-Picker.png`, `x-Inbox-popover.png`, `x-Approval-Sheet.png` | Components / Patterns | Overlays with no screen board |

### Phone (`companion/`, boards in `design/companion/png/`)

| File | Board | Shows |
|---|---|---|
| `C-Macs.png`, `C-Macs-Light.png` | C-Macs* | Three paired Macs (one going offline), Needs you on top |
| `C-Workspace.png`, `C-Workspace-Light.png` | C-Workspace* | A workspace's sessions with the Agents / Backlog control |
| `C-Permission.png`, `C-Permission-Light.png` | C-Permission* | Session with a docked permission request |
| `C-Question.png`, `C-Question-Light.png` | C-Question* | A child session asking a question |
| `C-Inbox.png`, `C-Inbox-Light.png` | C-Inbox* | Notifications |
| `C-MacInfo.png`, `C-MacInfo-Light.png` | C-MacInfo* | Mac details with the colour picker |
| `C-iPad.png` | C-iPad | Three columns: Macs, workspace, session with a permission |
| `x-iPad11.png`, `x-Backlog.png`, `x-Offline.png` | — | iPad 11" (sidebar collapses), the workspace's Backlog, a single offline Mac |

`mac/notes.md` and `companion/notes.md` list what the person capturing saw as different. Use them as hints, not as the answer.

## 6. What to deliver

1. Table per screen: divergence · impact · class (a–d) · fix.
2. Cross-screen list.
3. Verdicts.
4. Updated boards for (d) items, if any.
5. Open questions for me.
