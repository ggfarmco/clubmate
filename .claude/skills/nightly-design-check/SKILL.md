---
name: nightly-design-check
description: Use when running a node of the Clubmate repo's `design-check` graph (Scope, Judge, Fixer, Report; the "Nightly design check" cron job runs it), or when asked to check the Mac app or the companion against the Claude Design boards by hand — captures the screens whose UI changed (all of them on Sundays), judges them against the boards with Claude Design's own craft rules, fixes small token, contrast and copy drift, files the rest and leaves a package for the Claude Design chat.
---

# Nightly design check

Nobody is watching. The user should wake to at most a few small squashed UI commits on `main`, one short report, backlog items and a ready request for the Claude Design chat, never a red `main` or a redesign. When in doubt, file instead of editing.

Every node is an AFK session: never ask the user and never wait for them. Decide yourself and ask the advisor (Fable) on every non-trivial call; what still needs the user goes to the backlog tagged `needs-you`, and the run moves on.

## The graph

`.clubmate/graphs/design-check.json` (`docs/agent-graphs.md` § Design check). Steps are scripts in `scripts/design/`, run in the main checkout; agents are the nodes below, each doing only its section.

Scope → **Capture** (`capture.sh`) → Judge → Fixer → **Tests** (`tests.sh`; red → **Revert**, `revert.sh`) → Report → **Clean up** (`cleanup.sh`). Capture with nothing to capture, a Judge or Fixer with nothing to do, and every failure go to Report; Report always goes to Clean up.

- `MAIN` is the main checkout, `NOTES` is `MAIN/../clubmate-notes` (absolute paths).
- Run dir `RUN` = `MAIN/.worktrees/design-check/<run>` (the run id lower-cased, `_` as `-`, e.g. `gr-1a2b`): `boards`, `base`, `tree/` (the capture checkout), `shots/<Board>.png`, `manifest.json` (`head`, `base`, `shots` board → png, `skipped` board → reason), `logs/`, `findings.md`, `fixes.md`, `merged` (`<sha> <branch>` per landed fix), `reverted`, `tests.log`. Clean up deletes it.
- The scripts take the run from `CLUBMATE_GRAPH_RUN` (steps) or `DESIGN_CHECK_RUN=<run>` (you, in a Bash call).
- The design project is in `design/README.md`; you reach it with the `Artifact` tool, which the nodes have from the main checkout's settings and children in worktrees don't. No `Artifact` (or the project refuses): judge against the repo copy in `design/*/source` and `design/*/png`, skip the package and say so.

## Scope (graph node)

`capture.sh --plan` lists the boards whose UI changed since the last run and writes them to `RUN/boards`:

- Window: from the `head` of the newest `NOTES/design-check/*/manifest.json`, else the date of the newest `NOTES/design-check/*.md`, else 24 h.
- Boards: a path map in `capture.sh` (`boards_for`): `client/Clubmate/Theme`, `pkg/sdui`, `client/ClubmateProtocol`, `client/ClubmateDocument` → every board; `client/Companion*` → every phone board; `client/Clubmate`, `client/ClubmateKit` and plugin views it doesn't know → every Mac board; a plugin's `view*.go` → that plugin's boards.
- Sunday, or the run's input `all`: every board of `capture.sh` (`MAC_BOARDS`, `PHONE_BOARDS` in `scripts/design/lib.sh`).

1. `DESIGN_CHECK_RUN=<run> scripts/design/capture.sh --plan` in `MAIN`.
2. Read the window's commits (`git log -p <base>..HEAD` in `MAIN`, `base` in `RUN/base`). The map only sees paths: add the boards of a view whose change it missed (e.g. a `needsYou` rule in `plugins/agents/sessions.go` → the Agents boards), one name per line, only names from `lib.sh`. Never drop a board the map picked.
3. End your turn. An empty `boards` makes Capture end with `nothing`.

## Capture (step)

`capture.sh` does what a person did by hand (`design/brief/README.md`, `design/check/mac/notes.md`, `design/companion-brief/README.md`, `design/check/companion/notes.md`):

- `RUN/tree`: a detached worktree of `HEAD`, with GhosttyKit, `Resources/ghostty`, `Resources/terminfo` and the stamp copied from `MAIN` (refuses without them), `make app` (log `RUN/logs/make-app.log`).
- Mac: a test instance `dc-<run>` with the fake agent (`plugins/agents/internal/fakeagent`, never a real Claude) running `design/check/mac/fakeagent-script.json` with its paths moved into the tree; seed: 7 tasks, 3 cron jobs (one paused), 2 triggers, 2 terminals, 2 notifications, 5 sessions and a review-loop run. Three passes, each an app relaunch (`scripts/test-app.sh relaunch`): Kanagawa Wave with a dark app (`*-1280`, overlays, popovers), Catppuccin Latte with `-NSRequiresAquaSystemAppearance YES` (`*-640`, the approval sheet), Kanagawa with the Remote settings fixture (Mac-Settings-Remote, x-Title-Dot-1280). Popovers, sheets and Settings are their own window; Mac-ChatGraphStep is cut from the window.
- Phone: `capture-phone.sh` on its own simulator `cm-design-<run>`, deleted afterwards. C-iPad needs a UI-test rotation and is skipped: judge the committed `design/check/companion/C-iPad.png`.
- Not captured: `x-Cron-Missed-1280` (needs a missed run), `x-Processes-Selected-1280` (Processes-640 has the selection).
- A board that fails goes to `skipped` with the reason; the step fails only when the build, the instance or every board failed.

## Judge (graph node)

You judge the way Claude Design does, so read its rules first: `Artifact read` the project's `SKILL.md` § Designing well and `artifact-type/reference/craft.md` (accessible as drawn, 4.5:1 text contrast, ≥44 pt phone targets, no filler, follow the existing vocabulary). Then our own language: `design/language/README.md` and the `Principles`, `Tokens`, `Components`, `AgentNodes`, `Patterns` boards, the companion's `C-Language` and `C-Components`, and the `clubmate-ui` skill.

The truth per screen is the board: its `.dc.html` from the project (pull as in `design/README.md`; it holds the exact values: sizes, weights, tokens, copy) and its PNG in `design/*/png/` for the picture. A board that changed in the project since the repo copy is judged in its project version, and the report says the repo copy is stale.

Not a divergence: what `design/check/prompt.md` §3 decides and §4 already tracks, decisions in `design/check/fix-list.md`, seed data (the workspace is the capture tree: `tree`, detached at the captured sha), the capture conditions in §2 (800 pt minimum width, grey traffic lights, iPhone 18 Pro size), anything already open in the backlog (`backlog_list`, tags `design-check`, `tidy`).

1. Each shot in `RUN/manifest.json` against its board: every divergence with impact (wrong state or meaning > hierarchy > spacing/type > colour nuance) and class — (a) token or renderer, (b) SDUI prop or node, (c) plugin copy or order, (d) the board should change. Then cross-screen: the Mac and the phone saying one thing two ways. A skipped board: say why in the findings.
2. `RUN/findings.md`: per board `matches` or its divergences (impact, class, screenshot path).
3. File what no fixer may touch: `backlog_add` as `idea` with tags `design-check` and `mac` or `phone`, one item per finding: board, screenshot, divergence, class, suggested fix. Tag `needs-you` when it's a product or design choice; (d) items say what the board should become. Note each id in `findings.md`.
4. `RUN/fixes.md`: at most 3 fix briefs, one area each, only (a) and (c) in a single place each (a token, a weight, a colour role, contrast, a missing key hint, wording or order in a plugin's `view.go`): area slug, the divergence, board and screenshot path, file:line.
5. Report `fixes` when `fixes.md` has a brief, else `nothing`. Report once: prompted again later, end the turn without reporting.

## Fixer (graph node)

1. Per brief in `RUN/fixes.md` one child: `agents_new` with `branch: design/<YYYY-MM-DD>-<area>`, all at once. The prompt stands alone: the brief, the fix rules below, the test commands of `tests.sh` that the change needs, and "AFK: never ask the user; follow the `clubmate-ui` skill; `make app` in your worktree (copy GhosttyKit, Resources/ghostty, Resources/terminfo and the stamp from the main checkout), snapshot before and after with the same recipe as `scripts/design/capture-mac.sh` and put both paths in your reply; run tests in the foreground, rebase on current main, stop at one conventional commit and reply with its sha; a fix you can't finish within the rules goes back to me in your reply".
2. **Fix rules** (in every child prompt):
   - At most 5 files and 150 changed lines; over that, commit nothing and report.
   - No SDUI catalog, protocol, wire or MCP changes, no new views, no layout rework: those are (b) and go to the backlog.
   - The repo's CLAUDE.md holds: a key change updates `docs/shortcuts.md`; transcript folding changes land on the Mac and the phone together; a rule about what something means to the user lives in the plugin, never re-derived in a client.
   - A behaviour change comes with a test; a pure look change comes with the before/after snapshots.
3. Wait for them (`agents_wait`). Per child: its branch tip is the sha it reported; look at its after-snapshot yourself: it must match the board better than the before-snapshot, or the branch stays and the finding is filed.
4. Land each passing branch with `scripts/design/land.sh <branch> <run>` in `MAIN`, one at a time. It refuses a non-empty index, an uncommitted file the branch changes, a detached HEAD or a squash that conflicts; it squashes and commits in one go and records the commit in `RUN/merged`. A refusal: leave the branch, file why.
5. File what didn't land (as in Judge step 3). Report `landed` when `RUN/merged` has a line, else `nothing`, once, after every child is done: a child's late turn prompts you again, and then you end the turn without reporting. Tests and the revert are steps after you: don't run them.

## Tests and Revert (steps)

`tests.sh` tests what landed, in `RUN/tree` checked out at `MAIN`'s `HEAD` (else in `MAIN`): `go test` with every `CLUBMATE_*` unset (without `dist/` and `.worktrees/`) and `third_party/jsonrpc2`; `make app` when a landed commit touches `client/`; the ClubmateProtocol and ClubmateKit swift tests (without `testRestartUnderTrafficKeepsReceivingUpdates`, red on `main` already), CompanionKit's and `make companion` when it touches those. Log in `RUN/tests.log`. Red or not finished → `revert.sh` reverts exactly the commits in `RUN/merged`, newest first.

## Report (graph node)

The edge message says which path led here (nothing to capture, capture failed, nothing to fix, tests green, reverted, revert failed).

1. Nothing to capture: write `NOTES/design-check/<date>.md` with one line (the window, "no view changed"), commit it in clubmate-notes, no notification, end.
2. **Package for the Claude Design chat**: upload the shots of screens with open divergences (at most 10, smallest first) with `Artifact publish url=<project> asset=true file_paths=[…]`, and write the request the user pastes into the project's chat: per shot "`/_blob/<id>` is the build of <Board>", then the divergences as a list to confirm or overrule, and "which of these would you solve differently, and which boards should change". An upload that is refused or stalls: skip the package, say so.
3. Tests red: the reverts are in `RUN/reverted` and the failing suites in `RUN/tests.log`; file each reverted fix (`needs-you` when the failure isn't obvious). Revert failed: file a high-priority `needs-you` task naming the commits in `RUN/merged`.
4. **Report** `NOTES/design-check/<date>.md`: boards checked, per board fixed (sha) · reverted · filed (backlog id) · matches, skipped boards and why, the Claude Design request in a fenced block, stale repo boards; at most a screen. Copy `RUN/shots/*.png` and `RUN/manifest.json` to `NOTES/design-check/<date>/` (the next run's window starts at its `head`). Commit it in clubmate-notes, `docs_present` it, then `notify_post` one line: `Design: N fixed, M filed[, K need you][, package ready]` (a failure: `Design check failed: <what>`).
5. End your turn: Clean up runs next and deletes `RUN`.

## Clean up (step)

`cleanup.sh`: stops every `dc-*` test instance and deletes every `cm-design-*` simulator of a run dir, removes the run dirs and their trees, then each `design/*` branch that landed (`MAIN/.worktrees/design-check/landed`, across runs), has no commits of its own or whose commits are all on the checked-out branch as the same patch (`git cherry`), with its worktree. Every `design/*` branch counts as a fix branch. A worktree with uncommitted changes or a process still in it stays (a done fix child holds its worktree until it closes) and is tried again next night; a branch with commits that didn't land stays.

## By hand

From the main checkout, `graphs_run {"graph": "design-check"}` (input `all` for every board) runs the graph with you as the caller; its node questions come to you. Without the graph, the same order in `MAIN`, with `DESIGN_CHECK_RUN=manual-<date>` exported: `capture.sh --plan` (`--dry-run` shows what a capture would do), `capture.sh`, the Judge and Fixer sections yourself, `tests.sh` when something landed (red → `revert.sh`), the Report section, `cleanup.sh` (`--dry-run` first). `DESIGN_CHECK_REV=<rev>` captures another revision, e.g. a branch, in the same way.

## Cron job

Lives in the cron plugin, not in git; recreate it if it is gone:

```
cron_add name="Nightly design check" command=graphs.run cron="0 5 * * *" owner=none missed=once
  args={"graph":"design-check"}
```

05:00 runs after the tidy (03:30), so the two don't build at once. The steps need `Bash(scripts/design/capture.sh)`, `Bash(scripts/design/tests.sh)`, `Bash(scripts/design/revert.sh)` and `Bash(scripts/design/cleanup.sh)` in the main checkout's `.claude/settings.local.json` (or bypass mode), and the graph needs `graphs.trust {"graph":"design-check"}` after every change to its steps or wiring.
