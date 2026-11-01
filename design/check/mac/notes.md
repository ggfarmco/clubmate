# Mac app vs design boards — re-capture after the 1 Nov fixes

Captured 2026-11-01 from `46e46e47` (Debug build, `make app`), one test instance (`scripts/test-app.sh start`), `ui snapshot` at 2× scale. Capture and compare only, nothing fixed. The first capture (`b91c54f`) is in git history; the fix list is `design/check/fix-list.md`.

## Setup

- Theme: no test-app switch exists. Write `theme = <name>` to the instance's `state/ghostty.conf`, kill only the app pid and relaunch it with the `open -n -g -j --env …` line of `scripts/test-app.sh start`, then `cm workspace open <root>` again; core and seed data survive. Check that the pid changed after every relaunch.
- Appearance: this Mac's system appearance is **Dark**. `*-1280`: Kanagawa Wave with `-AppleInterfaceStyle Dark`. `*-640`: Catppuccin Latte with `-NSRequiresAquaSystemAppearance YES` (a light Mac). `x-Title-Dot-1280`: Kanagawa with the system as is. The light-system/dark-theme case that broke the Inbox before was re-checked with Kanagawa + `-NSRequiresAquaSystemAppearance YES`; Latte + dark system with `x-Inbox-popover-Latte`.
- Agents: `plugins/agents/internal/fakeagent` with `fakeagent-script.json` (steps keyed by prompt via `match`; diff paths are absolute to this worktree). The `go test` permission now carries Claude's real option labels ("Yes, and don't ask again for go test commands", plus "No, and don't ask again"), so the ⌥2 popover shows the rule `Bash(go test:*)` and Always reject lands in •••. `cm agent spawn` for Bump, Migrate (fails, retries once, fails again), Port cron (running), Fix flaky (permission), then `graphs.run review-loop` (copy of `plugins/graphs/testdata`), then Rate-limit. One more idle "New session" comes from New chat in the Code panel (visible in the later shots).
- Seed: 7 backlog tasks, 3 cron jobs (one paused), 2 triggers + the 3 graph edges, 2 terminals, 2 `cm notify`.
- Missed cron run: a 2-min job with `missed: skip`, the cron plugin frozen with `kill -STOP` past the 1-min grace, then `kill -CONT` → one skipped/missed run (`x-Cron-Missed-1280`). The nightly job can't be made to miss without waiting for 03:00.
- 640 boards: `--size 640x800` is clamped to contentMinSize, so every `*-640.png` is **800 pt (1600×1600 px)** (§4: the Mac minimum width is 800 pt).
- Popovers, sheets and Settings are snapshotted as their own window. The test window is never key: no traffic-light colours, no blue default button, switches/accents grey in Settings.
- Never `screencapture`; `test-app.sh stop` deletes `/tmp/clubmate-test-<name>`.

## Files

| File | Size px | Theme | Shows |
|---|---|---|---|
| Agents-1280 | 2560×1600 | Kanagawa | Rate-limit session, action group + edit card expanded (`ui present card:…/tool/e1`), permission card |
| Agents-640 | 1600×1600 | Latte | same at 800 pt |
| Mac-AlwaysAllow | 892×374 | Kanagawa | ⌥2 on the docked permission: the confirm popover window only (⎋ closed it, nothing stored) |
| x-Failed-Session-1280 | 2560×1600 | Kanagawa | Migrate session after its failed retry |
| Code-1280 | 2560×1600 | Kanagawa | `internal/notify/notify.go:40` in nvim, chat panel on a new chat |
| Code-640 | 1600×1600 | Latte | same, chat under the editor |
| Mac-ChatGraphStep | 760×1476 | Kanagawa | chat panel cropped from the window, Review loop · Reviewer picked by hand (`codeSelect`) |
| Backlog-1280 / -640 | 2560×1600 / 1600×1600 | Kanagawa / Latte | task #2 selected |
| Cron-1280 | 2560×1600 | Kanagawa | Nightly dependency audit |
| x-Cron-Missed-1280 | 2560×1600 | Kanagawa, light system | Poll CI for main with one missed run |
| Triggers-640 | 1600×1600 | Latte | graph edge "Review loop: Coder done → Reviewer" |
| x-Triggers-1280 | 2560×1600 | Kanagawa | same at full width |
| Graphs-1280 / -640 | 2560×1600 / 1600×1600 | Kanagawa / Latte | Review loop run, Coder waiting |
| Processes-1280 | 2560×1600 | Kanagawa | All, no row selected |
| x-Processes-Selected-1280 | 2560×1600 | Kanagawa | Terminal 2's zsh selected, action bar |
| Processes-640 | 1600×1600 | Latte | All, Terminal 2's zsh selected |
| Mac-Settings-Remote | 920×1296 | system light (Settings ignores the theme) | `--settings-tab remote --remote-fixture` + `ui present settings` |
| x-Title-Dot-1280 | 2560×1600 | Kanagawa, dark system | blue identity dot in the title pill (`--remote-fixture`) |
| x-Palette, x-Switcher | 2560×1600 | Kanagawa | `ui present palette / switcher` |
| x-Worktree-Picker | 772×764 | Kanagawa | popover window only |
| x-Inbox-popover | 812×1012 | Kanagawa | popover window only (identical with a forced light system) |
| x-Inbox-popover-Latte | 812×1012 | Latte, dark system | popover window only |
| x-Approval-Sheet | 1184×1096 | system light | `ui present approval` (fixture text) |

## What still differs, per board

"Seed" = data or set-up only. "Gap" = a real difference; the fix-list item is named, or "not in the fix list".

The `*-640` boards (except Agents-640) draw the 640 pt narrow layout: no sidebar, a "‹ Section / group · n of m" breadcrumb with ↑↓, compact headers. The build stops at 800 pt (§4), so the 800 pt shots keep the sidebar; that difference is not repeated below. Code-640's board still shows the chat on Review loop · Reviewer with a transcript, which decision 4 overrules (the shot shows a new chat, as Code-1280's board now does).

**Agents-1280 / -640** — now match: status map (failed ✕ danger, running accent ▶, waiting amber), session header (status, agent, branch, mode, "waiting for you" badge), one Stop in the header, permission card (title, mono box, "Claude says:", ⌥1–3, ••• for Always reject), diff card (file bar, hunk header, line numbers, no `---/+++`), "Thought for 1 s", filled filter chip, Latte warning fills.
- Seed: the second worktree group (`main`) isn't reproduced; the hunk starts at line 1 because the fake diff has no file context.
- Gap: the failed row has no "exit 1" on the right (P0 status map: "exit 1" / "Stopped · error").
- Gap: the Review loop row says "Coder need…" instead of "turn 1/12"; the Bump row has no age (not in the fix list).
- Gap: Open in nvim has no ⌘O; Read/Search rows have no "0.1s" / "4 hits" (P1 diff card names ⌘O; the hits are not in the fix list).
- Board vs list: the composer reads "Queue a message" while the permission waits; the board reads "Reply, / for commands…". The build follows P2 composer placeholder: "Answer Claude…" is only for a pending question/elicitation (`asked` in `plugins/agents/view.go`), a pending permission is a running turn, so busy copy. The board predates the list.
- Gap: the status bar has no "⌥1 allow · ⌥2 always · ⌥3 reject · ⎋ stop" hints (not in the fix list).

**Mac-AlwaysAllow** — matches: title, rule `Bash(go test:*)`, scope sentence, "Press ⌥2 again to confirm.", Cancel ⎋ / Always allow ⌥2; ⎋ closed it and the session stayed waiting.
- Seed: workspace "check-recapture-mac" instead of "shop".
- Gap: Cancel is not drawn as the default (board: bold Cancel); can't tell from a non-key window whether it is (decision 2).

**x-Failed-Session-1280** — ✕ in the sidebar and the header.
- Gap: the error line reads "acp error -32603: exit status 1" (raw ACP code), and the header has no "Stopped · error" (P0 status map, names over ids).

**Code-1280 / -640** — the panel opens on the last regular session, never on a graph step; New chat works; suggestions have no ⌥ keys (decision 5 allows no keys).
- Gap: the title reads "New session", the board "New chat"; the empty state is still "Claude is idle / Ask anything, @ to attach a file…", the board "New chat in ‹branch› / It sees the file and selection you're on." (K1 Code row; not in the fix list §3).
- Gap: context shows one `notify.go` chip, the board `notify.go:40` + `Limiter` (not in the fix list).
- Gap: the app status bar shows "⌘B chat · ⌘↩ send", the board "⌘J chat · ⌘L focus chat" (P1 Code).

**Mac-ChatGraphStep** — matches the structure: "Reviewer is a step of Review loop", step states (Coder waiting for you in amber, Reviewer not started), Open in Graphs, New chat.
- Gap: no composer at all; the board keeps a disabled one ("Reviewer runs as part of Review loop") (decision 4).
- Gap: buttons have no ⌘8 / ⌘N; the note is generic ("when the graph reaches it" vs "when Coder finishes its turn, and gets Coder's result"); no "runs after Coder"; the header glyph is a plain circle, not the graph icon; the content sits at the bottom of the panel, the board centres it (decision 4).

**Backlog-1280 / -640** — now match: title once, no header badges, `!!` / `↓` glyphs, `#n` at the right, Delete at the far end, "✓ Saved ⌘S" (§4).
- Gap: "New task…" field plus a separate "+ Add" row, no ⌘N; no "Backlog" title/search (not in the fix list).
- Gap: `#2` sits under the title, the board puts it before; the description is rendered text with "Edit description", the board an open box; Status/Priority pickers have no glyphs; no priority counts (not in the fix list).

**Cron-1280 / x-Cron-Missed-1280** — now match: "active" and "missed 1 run" badges, Missed (warning ink, Run it now) split from When missed, Next run with "in 4 h", Owner "nobody — results go to the inbox", Worktree row, Does in words with the prompt in a card + Show as JSON, Duplicate, Delete at the far end, times on active rows.
- Gap: no Edit E (P1 Cron).
- Gap: the paused row has no "paused" on the right (P1 Cron: "Every list row shows its next time, or paused").
- Gap: Runs is plain text, not the dashed box with Run now; Does has no mode ("Default") and no icon (not in the fix list).
- Gap: a job running a plain command reads "Run the command backlog.list" (P2 names over ids).

**Triggers-640 / x-Triggers-1280** — now match: graph triggers grouped "from graph Review loop · 3", "created by graph Review loop", Owner = graph, Run now ⌘R, Open graph G, no Delete in the bar (•••), When and Does in words, prompt clamped with Show all, Show as JSON.
- Seed: the Does text reads "Send a prompt to Review loop · Reviewer" because the reviewer session already exists.
- Gap: When is one line, the board splits it (event, then status chip and session rows); Runs is plain text, not the dashed box; no breadcrumb header at 800 pt (not in the fix list).

**Graphs-1280 / -640** — now match: no Remove in the bar while running, waiting in amber with the warning glyph in the list.
- Tracked (§4): no Runs/Definitions groups, no node diagram, no Edit definition.
- Gap: no ••• in the bar; the list row shows "Nov 1 22:30 · 1…" instead of an age; the node table has no state glyphs; at 800 pt the Session column is dropped (not in the fix list).

**Processes-1280 / -640 / x-Processes-Selected-1280** — now match: owner icon left + CPU % right, commands truncated in the middle, action bar with Copy command / Reveal ⇧⌘R / Kill ⇧K (§5.7).
- Seed: an nvim/gopls group, one fakeagent process for all sessions.
- Gap: title, counts and filter are stacked rows, the board has one line; "LAST MIN", not "LAST 10 MIN" (P2 Processes; K1 allows "keep 1 min and say so").
- Gap: the selection footer stacks pid/command over "started …" and the actions follow it, not at the right; Copy command has no ⌘C; at 800 pt TIME and LAST MIN columns drop and "MEM · avg" wraps to three lines (not in the fix list).

**Mac-Settings-Remote** — matches (system form, read-only name per §5.3).
- Gap: the iPhone preview row is still mono; the board uses SF (K1 Settings row, class a; not in the fix list §3).

**x-Title-Dot-1280** — fixed: the title bar keeps Kanagawa's surface with the dot on, under a dark and a light system.

**x-Inbox-popover / -Latte** — fixed: the popover follows the theme's appearance (dark under Kanagawa even on a light system, light under Latte on a dark system); titles "Permission · …", session subtitle, age on every row, waiting items oldest first.
- Gap: the coloured rim is still in every capture; whether it is ours is still open (§5.4).
- Seed/gap: History repeats the waiting items (the same notifications) — not in the fix list.

**x-Approval-Sheet** — now matches: question, workspace and command on top; repo, sha256, size, trusts and worktree below in smaller mono; the fixture's two look-alike `teardown.sh` lines render as escaped body text, not as headers, so "2 entries" matches the two real headers (`setup.sh`, `worktree-include`); Deny is the ⏎ button in code (`InputViews.swift`).
- Gap: Allow is a red-tinted button on the left; nothing marks Deny as the default in a non-key capture (P0 approval sheet; checked in code only).

**x-Worktree-Picker**
- Gap: the detached worktree is a bare "03a9ff5f" (P2 names over ids: "detached · 03a9ff5f").
- Gap: "New Worktree…", "Remove Worktree…" in Title Case (P2 case); the title still says "worktree or branch" with only worktrees listed (K1, not in the fix list).

**x-Palette, x-Switcher**
- Gap: the palette lists commands in Title Case ("New Backlog Task") with their ids on the right (`backlog.new`, `editor-nvim.reveal`) (P2 case, names over ids).
- Switcher: nothing new.
