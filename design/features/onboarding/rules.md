# Onboarding — rules (decided 23 Nov 2026)

Claude Design, four rounds on the canvas "Clubmate · Onboarding" (https://claude.ai/artifact/A9ZBWRzQyAWucnyaNdEQZt). Brief and round pages: `design/onboarding-brief/`. Boards here: `R1-*` (Welcome, first run, first agent, pairing, rules), `R3-Editor`, `R3-Rules`, `R4-*` (positioning, README, rules); `boards.html` shows them as one page; `social-preview.png` (2560×1280) and `shots/` (README crops, 2×) are cut from real screenshots of a test build (`design/onboarding-brief/screens/real-home-*.jpg`). Not picked, in `rounds/`: the r1 README, all of r2 (editor A, "Your workshop. Claude Code is the crew."), r3 positioning and README ("Your garage. Agents welcome."). The R4 boards carry the user's line B; the canvas's r4 page still shows the designer's A.

Audience: people who already use Claude Code and want to run agents on a Mac.

## Ruling

| Question | Pick | Board |
|---|---|---|
| Welcome | r1 A · one door: Open folder… ⌘O is the only primary; Home is a keyed row; the New terminal tile goes (⇧⌘N, File › New Terminal and the palette entry stay). Clone repository… later. | R1-Welcome, R3-Rules (final Welcome) |
| First run | r1 A · silent check, just in time: nothing to click through between launch and the first agent | R1-Flow |
| Editor | r2 B · Neovim on silently for every new install; first run asks nothing about an editor; VS Code and "no editor" live in Settings › General › Editors and the palette | R3-Editor |
| Both editors off | hide Review changes, Full diff and Open in editor; edit cards keep their inline diff; no Code in the rail | R3-Editor |
| First agent | r1 A · three in-place hints + one suggested prompt | R1-First-Agent |
| Pairing | r1 A · offered once after the first finished turn; Not now leaves a status bar item | R1-Pairing |
| Positioning | r4 B (user) · "Your workshop, set up your way." The place first, agents of any vendor second; no vendor in a headline, the Welcome line or the About line | R4-Promo |
| Pictures | Real screenshots from a build, Mate dark, 2×; nothing drawn | R4-Promo, R4-README |
| README | hero = social preview on the real aside shot; "What's in the workshop" rows with 1:1 crops | R4-README |

## Flow

1. Launch. The agents plugin checks Node ≥ 22, the Claude Code login and git in the background, and preinstalls claude-agent-acp. All good: nothing is shown (one "ready" caption on the first Agents screen).
2. A failing check shows in place: one row on Welcome and the Agents empty state, the same words, one fix action; it clears by itself when the check passes. Order: Node → sign-in → git, one at a time. Re-checked at launch, on app activate and after a fix exits.
3. Welcome: Open folder… or Home. The first workspace opens on Agents; later ones on their last section.
4. First session: macOS asks for notifications now (not at launch); denied → a Settings › General row.
5. First permission: the card carries the Needs you note once.
6. First finished turn: the phone offer row, once.
7. Code (Neovim): the nvim config import is Neovim's own question, asked once and only when Code opens to edit (not from Review changes / Full diff). Not now leaves a Settings row and a palette command; it never asks again.

## Rules

- Nothing runs between launch and the first agent that the user must click through.
- A check that passes says nothing, except one "ready" caption on the first Agents screen.
- A failing check shows where the user is, with one fix action, and clears by itself.
- A question is asked where its answer is used: notifications at the first session, the phone after the first turn, the nvim import when Code opens to edit.
- A skipped step never comes back as a modal. It leaves at most one quiet row (Settings, the status bar).
- At most three hints, each once per Mac, each a line inside something that exists.
- An editor is a setting, not a step. With no editor, actions that need one are hidden, not disabled or rebuilt.
- No new phone screen. The phone's first screen changes words only.
- Claude Code is named only where it is a fact (requirements, the Agents row, the hero's small print).
- Repo pictures are real screenshots from a build in Mate dark at 2×. README pictures are 1:1 crops (one app point per pixel, at most 506 pt wide); the hero may scale. The hero keeps one 64 px margin on every side.

## Fields (decided in Go, rendered by clients)

```
setup      state      checking | ok | node_missing | node_old | signed_out | git_missing
           found      { node: "22.11.0", git: "2.47.1" }
           fix        { label: "Install Node…", run: "brew install node@22" } | { label, url }
           order      node → sign-in → git, one at a time
           when       launch, app activate, after fix.run exits
companion  state      unpaired | offer | dismissed | paired
           via        tailscale | relay
           offer      once, after the first finished turn of the first session; never at first start
editor     enabled    ["neovim"] on every new install; existing installs unchanged
           review     diffview | vscode | hidden
           openedFor  edit | review   (why Code opens)
firstRun   nvimImport unasked | imported | declined
           hints      emptyFirst, needsYou, phone (each once)
```

## Texts

| Where | Slot | Text |
|---|---|---|
| Welcome | title · line | Clubmate · Your workshop, set up your way. |
| Welcome | primary · row | Open folder… ⌘O · Home · agents across all projects ⇧⌘H |
| Welcome | keys | ↑↓ choose · ⏎ open · ⇧⌘N new terminal (no ⌘K: the palette needs a workspace) |
| Setup node_missing | row / empty title · detail · action | Node 22+ is required to run Claude · Node not found · Install Node… |
| | empty text · hint | Claude runs through claude-agent-acp, which needs Node 22 or later. · Runs brew install node@22 in a terminal tab. Checked again when it finishes. |
| Setup node_old | detail · text | found 18.19 · Found Node 18.19 at /usr/local/bin/node. Claude runs through claude-agent-acp, which needs Node 22 or later. |
| Setup signed_out | Welcome row | Sign in to Claude to start agents · Sign in… |
| | empty title · text | Sign in to Claude · Clubmate runs Claude Code on this Mac with your Claude login. This Mac isn't signed in yet. |
| | action · hint | Sign in… · Opens Claude's sign-in in your browser. Checked again when you come back. |
| Setup git_missing | title · action | git is needed for worktrees and checkpoints · Install… |
| | text · hint | Agents still run, but turns aren't checkpointed and the wiki stays off. · Installs the Xcode Command Line Tools (xcode-select --install). |
| Setup checking / ok | empty | Getting Claude ready… · Claude Code · Node 22.11 · git 2.47 (first run) |
| Agents empty, first run | title · text · action | Start your first agent · Each agent can work in its own worktree, and every turn saves a git checkpoint you can go back to. · New session ⌘N |
| Composer, first session | suggestion | Try "Find one small bug, fix it and run the tests" · ⇥ to use |
| Permission, first | note | The agent waits until you answer. Anything waiting also shows under Needs you and in notifications. |
| Transcript | turn end | Turn 1 · checkpoint saved · 1 file changed |
| Phone offer | row · actions | Answer Claude from your iPhone when you step away. · Pair iPhone… · Not now |
| Status bar | dismissed | Pair iPhone |
| Palette, Clubmate menu | command | Pair iPhone or iPad… |
| Pairing sheet | title · text | Pair an iPhone or iPad · Open Clubmate on your iPhone or iPad, tap Scan code and point it at this code. |
| | route | Connects over Tailscale · ‹host› / Tailscale off on this Mac · the phone connects through the relay |
| | install · close | No Companion yet? Get it on the App Store · iOS and iPadOS 27 · Done ⎋ |
| Companion, unpaired | title · text | Pair a Mac · On your Mac, open Clubmate Settings › Remote › Pair Device…, then scan the code. |
| | route · button · foot | Works over Tailscale, or through the relay without it. · Scan code · No Clubmate on your Mac yet? github.com/ggfarmco/clubmate |
| Settings › General › Editors | group | Editors |
| | Neovim | Neovim · Built in. Opens files, and reviews diffs with line comments. |
| | Neovim config | Neovim config · Bring your ~/.config/nvim keys and plugins in. An agent does it in a session you can watch. · Import… |
| | VS Code | VS Code · Uses VS Code on this Mac and Microsoft's VS Code Server (about 600 MB, once). |
| | caption, Neovim on / VS Code only / both on / both off | Review changes and Full diff open diffview in Neovim, where you comment on lines. / … open in VS Code. / Both on: … open in Neovim (diffview). / Both off: no Code in the rail. Review changes, Full diff and Open in editor are hidden; edit cards keep their inline diff. |
| Settings › General | notifications denied | Notifications · Off · Open System Settings… |
| Palette | editors | Turn on VS Code… · Turn off Neovim · Import ~/.config/nvim… · Open Settings › General › Editors |
| Neovim (Code) | import question (unchanged words) | Bring your ~/.config/nvim keys and plugins into Clubmate? · 1. Import with an agent · 2. Not now |

## GitHub repo

- About (104): "Your workshop on the Mac, set up your way. Agents work with your backlog, wiki, schedules and processes."
- Topics: ai-agents, coding-agent, agent-client-protocol, claude-code, macos, swiftui, local-first, workflow-automation, cron, personal-knowledge-base.
- Social preview = README hero, 1280×640 (`social-preview.png`, 2×): left "Clubmate", "Your workshop," / "set up your way." (accent), "Agents work with your backlog, your wiki, your schedules, your side threads and the processes you build.", small print "macOS 27 · agents over ACP, Claude Code today"; right the real Home › Agents screenshot with an aside (Morning brief started by cron, Needs you, the review loop, a side thread), whole at 819×512. Text column 64–349, gutter 48, screenshot 397–1216 × 64–576.
- README: hero image · "# Clubmate" · lead "Your workshop on the Mac, set up your way. Agents work there with your backlog, your wiki, your schedules, your side threads and your processes, on every project and in Home." · Download for macOS (latest release DMG) · "What's in the workshop" rows with a crop each: Agents (`shots/agents.png`), Backlog (`backlog.png`), Cron and Triggers (`cron.png`), Side threads (`aside.png`), Wiki (`wiki.png`), Your own processes (`triggers.png` for now), Your phone (no shot yet) · Get started (DMG → Applications; ⌘O or ⇧⌘H; ⌘N) · Requirements (macOS 27+; Claude Code login and Node 22+; git; Companion on iOS 27 with Tailscale or the relay) · "Neovim is built in and on; turn on VS Code, or turn both off, in Settings › General › Editors." The full source and the crop table are on R4-README; `<img>` widths are the 1× widths (agents 289, backlog 248, cron 476, aside 495, wiki 500, triggers 506).

## Handoff

Tags: (a) token or renderer, client SwiftUI · (b) new SDUI node or prop · (c) plugin content, words and order · (d) new plugin or core logic.

| # | Change | Tag |
|---|---|---|
| 1 | Welcome: drop the New terminal tile; ⇧⌘N, File › New Terminal and the palette entry stay. | (a) |
| 2 | Welcome: Open folder… is the one primary (accent) tile, focused at launch; Home a one-line row with a detail; bottom key caption; line "Your workshop, set up your way." | (a) |
| 3 | Welcome: setup row bound to `setup.state` (title, detail, one button); hidden when ok. | (a) |
| 4 | Agents plugin: setup checks (Node ≥ 22, Claude Code login, git), preinstall claude-agent-acp in the background, re-check on app activate; emits `setup`. | (d) |
| 5 | Agents empty: first-run title/text, "ready" caption; failing states swap the same empty node's content by `setup.state`. | (c) |
| 6 | The first workspace opens on Agents; later ones on their last section. | (c) |
| 7 | Remove the editor sheet from first start; Neovim is on for every new install. | (c) |
| 8 | Composer: suggestion prop, ⇥ accepts, first session only. | (b) |
| 9 | Permission card: note prop (one caption line), first request only. | (b) |
| 10 | Notifications: request at the first session start, not at launch; denied → Settings › General row. | (a) |
| 11 | Transcript: turn-end line "Turn N · checkpoint saved · K files changed", if not shown today. | (c) |
| 12 | Phone offer: Remote plugin sets `companion.state = offer` after the first finished turn; in-place dashed row with a second action (Not now). | (b) |
| 13 | Status bar: "Pair iPhone" item when `companion.state = dismissed`; opens the pairing sheet. | (a) |
| 14 | Palette and Clubmate menu: Pair iPhone or iPad…. | (c) |
| 15 | Pairing sheet: route line (Tailscale host or relay), App Store line, Done ⎋. The App Store line is deferred until the Companion has a public install path (backlog #1204). | (c) |
| 16 | Companion first screen: new text, button "Scan code", foot line ("Settings → Devices" is wrong today: the Mac tab is Remote; the relay works without Tailscale). | (a) |
| 17 | Settings › General › Editors: Neovim switch, Neovim config row (Neovim on, not imported), VS Code switch, one caption per state (SDUISettingsRows). | (c) |
| 18 | Palette: Turn on VS Code…, Turn off Neovim, Import ~/.config/nvim…, Open Settings › General › Editors. | (c) |
| 19 | Core passes why Code opens (`openedFor` = edit \| review) to the editor plugin. | (d) |
| 20 | editor-nvim: ask the import once, only when `openedFor = edit`; Not now leaves the Settings row and the palette command. | (c) |
| 21 | Agents plugin sends `editor.review` (diffview \| vscode \| hidden); both off hides Review changes, Full diff and Open in editor. | (c) |
| 22 | Repo: README.md, `.github/social-preview.png` and `.github/shots/*.png` from this folder, About, ten topics (set the social preview in the repo settings). | repo |
| 23 | A demo Home fixture for screenshots (Morning brief cron at 08:00, review loop with Coder and Reviewer, four backlog tasks, wiki pages, a trigger, an aside), so the shots can be retaken for each release; today's recipe: `design/onboarding-brief/README.md` § Real screenshots. | (d) |
| 24 | Still to shoot: a review-loop graph (for Your own processes, better than Triggers) and the Companion (for Your phone). | repo |

Later: Clone repository… ⌥⌘O on Welcome (d).

## Open questions

- Login check (answered 23 Nov): yes. After the background install, the plugin runs the agent's own CLI, `node <claude-agent-acp> --cli auth status --json`, which answers `loggedIn` from the same Keychain item and `~/.claude` config a session uses (about 0.3 s). `false` shows `signed_out`; no answer counts as unknown and leaves sign-in inside the session. `ANTHROPIC_API_KEY` does not count: sessions do not get it. `docs/agents.md` § Setup check.
- Worktrees: "+ New" starts a session in the workspace's checkout; "New in worktree…" is separate (seen in the test app, 23 Nov). The README Agents row "Each in its own worktree" and the first-run empty text must say "can work in its own worktree", not "each".
- Universal link domain: one QR code that installs and pairs the Companion needs one.
- The Companion has no public install path yet (App Store / TestFlight); the pairing sheet's App Store line waits for it.
