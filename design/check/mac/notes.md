# Mac app vs design boards — capture notes

Captured 2026-11-01 from `b91c54f` (Debug build, `make app`), one test instance (`scripts/test-app.sh start`), `ui snapshot` at 2× scale. Capture only, nothing fixed.

## Setup

- Theme: no test-app switch exists. The app reads it from ghostty config; I wrote `theme = <name>` to the instance's overlay `state/ghostty.conf` and restarted only the app (core and seed data kept). `*-1280`: Kanagawa Wave plus `-AppleInterfaceStyle Dark`. `*-640` and the extras marked Latte: Catppuccin Latte with the system's light appearance.
- Agents: `plugins/agents/internal/fakeagent` with `fakeagent-script.json` (steps keyed by prompt via `match`; the diff path is absolute to this worktree). `cm agent spawn` for "Bump ghostty to 1.2", "Migrate agents store to sqlite", "Port cron to the detail pane", "Fix flaky switcher test", "Rate-limit notifications per workspace", then `graphs.run review-loop` (copy of `plugins/graphs/testdata`, input "Rate-limit notifications per workspace"; Coder gets the same script and waits on permission).
- Seed: 7 backlog tasks (#1–#4 todo, #5/#7 idea, #6 not_now, priorities high/normal/low), 3 cron jobs (one paused), 2 triggers plus the 3 graph edges, 2 terminals, 2 `cm notify`.
- 640 boards: `--size 640x800` gets clamped to contentMinSize, so every `*-640.png` is really **800 pt (1600×1600 px)**. That's the same as `design/brief/screens/02-agents-narrow-640.png`. At 800 the sidebar stays and the 640 narrow layout (no sidebar, breadcrumb header) never shows.
- Agents-Mate-1280: **skipped**. Mate is only `GhosttyConfigReader`'s fallback when ghostty gives no palette, and ghostty always supplies its default one, so it can't be selected.
- No traffic-light colours anywhere: the test window is never key.

## Files

| File | Size px | Theme | Shows |
|---|---|---|---|
| Agents-1280 | 2560×1600 | Kanagawa | Rate-limit session, action group + edit card expanded (`ui present card:…/tool/e1`), permission |
| Agents-640 | 1600×1600 | Latte | same at 800 pt |
| Code-1280 | 2560×1600 | Kanagawa | `internal/notify/notify.go:40` in nvim, chat panel (Review loop · Reviewer) |
| Code-640 | 1600×1600 | Latte | same, chat stacked under the editor |
| Backlog-1280 / -640 | 2560×1600 / 1600×1600 | Kanagawa / Latte | task #2 selected |
| Cron-1280 | 2560×1600 | Kanagawa | Nightly dependency audit |
| Triggers-640 | 1600×1600 | Latte | graph edge "Review loop: Coder done → Reviewer" |
| Graphs-1280 / -640 | 2560×1600 / 1600×1600 | Kanagawa / Latte | Review loop run, Coder waiting |
| Processes-1280 / -640 | 2560×1600 / 1600×1600 | Kanagawa / Latte | All, no row selected |
| Mac-Settings-Remote | 920×1296 | system light (Settings ignores the theme) | `--settings-tab remote --remote-fixture` |
| x-Title-Dot-1280 | 2560×1600 | Kanagawa | blue identity dot in the title pill (`--remote-fixture`) |
| x-Triggers-1280 | 2560×1600 | Kanagawa | Triggers at full width |
| x-Palette, x-Switcher | 2560×1600 | Kanagawa | `ui present palette / switcher` |
| x-Worktree-Picker, x-Inbox-popover | 772×764, 812×1012 | Kanagawa | the popover window only |
| x-Approval-Sheet | 1184×1100 | system light | `ui present approval` (fixture text) |

## Looked broken / differs from the boards

- **Inbox popover**: a light system material with Kanagawa's light text, so it's nearly unreadable. There's also a rainbow rim, probably a snapshot artifact of the popover material. This happens whenever the system is light and the ghostty theme is dark.
- **Title bar** follows the system appearance and not the theme. It stays light for a while after launch even with `-AppleInterfaceStyle Dark` (x-Title-Dot is light, the other 1280 shots are dark).
- **Settings → Remote** is a native grouped Form in system light. The board is themed dark with 5 tabs (General/Appearance/Agents/Remote/Shortcuts); the app has 3 (General/Plugins/Remote). Name is read-only text, not a field. Both toggles look off/grey.
- **Agents**:
  - No "Agents" header with search/+; there's a "+ New · New in worktree…" row instead.
  - Row details are free text ("Coder need…", "Waiting for per…", "4 min ago"), not `turn 1/12 / waiting / idle`.
  - The session header has no subtitle (Claude · branch · mode) and no "waiting for you" chip.
  - The edit diff is a raw unified diff (`---/+++` headers, no line-number gutter), with Open in nvim/Diff buttons below it, not in a file header with ⌘O/⌘D.
  - The permission card is one line ("✋ Claude wants to run …") with no command box and no ⌥2 on Always allow.
  - The failed session (fakeagent `fail`) shows as done ✓, not failed ✗ (`agents.sessions` says `done`).
  - The second worktree group (`main`) wasn't reproduced: all sessions are in one worktree.
- **Backlog**:
  - "New task…" field plus a separate "+ Add" row, no ⌘N.
  - Priority is a right-side "high" badge, not a leading "!!".
  - The detail has an extra Title field, todo/high chips next to the title, and "Saved ⌘S" under the form.
  - The description box is small; the board's is big.
  - No priority counts.
- **Cron**:
  - No time on the right of the selected row.
  - No active/missed chips, no Edit/Duplicate, no Worktree row, no "When missed" picker.
  - "Does" shows raw `agents.new`, not "Start an agent · Claude · Default".
  - Runs is plain text, not the dashed empty box.
  - I couldn't seed a missed run.
- **Triggers**:
  - "When" shows raw `agents.turn {json}`, not "an agent turn finishes / status done / session Review loop · Coder".
  - "Does" dumps the whole generated agents.prompt text.
  - No "created by graph" line, no Run now/Open graph.
- **Graphs**:
  - The sidebar has no Runs/Definitions groups (one run row; `release` was not run).
  - No node diagram, no Edit definition.
  - Remove is a button, not in an overflow menu.
- **Processes**:
  - The header (title / counts / filter row) is stacked, not one line.
  - The sidebar has no CPU % column.
  - The column reads "LAST MIN", not "LAST 10 MIN".
  - An nvim/gopls group shows up.
  - No selection footer (no row selected).
