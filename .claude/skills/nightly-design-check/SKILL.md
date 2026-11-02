---
name: nightly-design-check
description: Use when running the nightly design check (started by the "Nightly design check" cron job) or when asked to check the Mac app or the companion against the Claude Design boards — captures the screens whose UI changed (all of them on Sundays), judges them against the boards with Claude Design's own craft rules, fixes small token, contrast and copy drift, files the rest and leaves a package for the Claude Design chat.
---

# Nightly design check

Nobody is watching. The user should wake to at most a few small squashed UI commits on `main`, one short report, backlog items and a ready request for the Claude Design chat, never a red `main` or a redesign. When in doubt, file instead of editing.

This is an AFK session: first call `agents_afk on=true` for yourself. Never ask the user and never wait for them. Decide yourself and ask the advisor (Fable) on every non-trivial call; what still needs the user goes to the backlog tagged `needs-you`, and the run moves on.

`NOTES` is `<main checkout>/../clubmate-notes` (absolute paths). The project is in `design/README.md`; you reach it with the `Artifact` tool, which this session has from the main checkout's settings and children in worktrees don't. No `Artifact` (or the project refuses): judge against the repo copy in `design/*/source` and `design/*/png`, skip step 7 and say so in the report.

## Scope of one run

- Commits since the last run (date of the newest `$NOTES/design-check/*.md`; none → 24 h) that touch `client/Clubmate`, `client/Companion`, `client/CompanionKit`, `pkg/sdui`, `plugins/*/view.go` or anything else a view renders. None and not Sunday → write a one-line report, no notification, stop.
- From the diff, the boards that show the changed views: Agents (+ `Mac-AlwaysAllow`, `Mac-ChatGraphStep`), Code, Backlog, Cron, Triggers, Graphs, Processes, `Mac-Settings-Remote`, the `C-*` phone boards, or every board when a theme token, a shared renderer or the SDUI catalog changed.
- Sunday: every board listed in `design/check/prompt.md` §5, Mac and phone.

## The judge

You judge the way Claude Design does, so read its rules first: `Artifact read` the project's `SKILL.md` § Designing well and `artifact-type/reference/craft.md` (accessible as drawn, 4.5:1 text contrast, ≥44 pt phone targets, no filler, follow the existing vocabulary). Then our own language: `design/language/README.md` and the `Principles`, `Tokens`, `Components`, `AgentNodes`, `Patterns` boards, the companion's `C-Language` and `C-Components`, and the `clubmate-ui` skill.

The truth per screen is the board: its `.dc.html` from the project (pull as in `design/README.md`; it holds the exact values: sizes, weights, tokens, copy) and its PNG in `design/*/png/` for the picture. A board that changed in the project since the repo copy is judged in its project version, and the report says the repo copy is stale.

Not a divergence: what `design/check/prompt.md` §3 decides and §4 already tracks, decisions in `design/check/fix-list.md`, seed data, the capture conditions in §2 (800 pt minimum width, grey traffic lights, iPhone 18 Pro size), anything already open in the backlog (`backlog_list`, tags `design-check`, `tidy`).

## Run

1. **Capture, read-only**, in a throwaway tree: `git worktree add --detach <scratchpad>/design HEAD`, `make app` there (copy GhosttyKit, `Resources/ghostty`, `Resources/terminfo` and the stamp from the main checkout first). Mac: the recipe in `design/brief/README.md` with the setup in `design/check/mac/notes.md` (fake agent script, seed, theme per board, `ui snapshot`, popovers as their own window); the diff paths in `design/check/mac/fakeagent-script.json` are absolute to the tree it was captured in, so rewrite them to the capture tree in a scratchpad copy first. Phone: `design/companion-brief/README.md` with `design/check/companion/notes.md`, on a simulator of your own. Name each shot after its board; keep them in the scratchpad.
2. **Judge** each shot against its board: every divergence with impact (wrong state or meaning > hierarchy > spacing/type > colour nuance) and class — (a) token or renderer, (b) SDUI prop or node, (c) plugin copy or order, (d) the board should change. Then cross-screen: the Mac and the phone saying one thing two ways.
3. **Fix the small ones in children.** At most 3 children with `agents_new`, `branch: design/<YYYY-MM-DD>-<area>`, one area each. Only (a) and (c), each a single place: a token, a weight, a colour role, contrast, a missing key hint, wording or order in a plugin's `view.go`. The prompt stands alone: the divergence with board and screenshot path, these rules, the test commands, "AFK: never ask the user; follow the `clubmate-ui` skill; `make app` in your worktree (copy GhosttyKit, Resources/ghostty, Resources/terminfo and the stamp from the main checkout), snapshot before and after with the same recipe and put both paths in your reply; run tests in the foreground, rebase on current main, stop at one conventional commit and reply with its sha; a fix you can't finish within the rules goes back to me in your reply".
4. **Fix rules** (in every child prompt):
   - At most 5 files and 150 changed lines; over that, commit nothing and report.
   - No SDUI catalog, protocol, wire or MCP changes, no new views, no layout rework: those are (b) and go to the backlog.
   - The repo's CLAUDE.md holds: a key change updates `docs/shortcuts.md`; transcript folding changes land on the Mac and the phone together; a rule about what something means to the user lives in the plugin, never re-derived in a client.
   - A behaviour change comes with a test; a pure look change comes with the before/after snapshots.
5. **Tests and merge** as in the `nightly-tidy` skill steps 4 and 5 (the env-stripped `go test`, plus `make app` and the swift tests of the touched clients (`make kit-test` for `client/ClubmateKit` and `client/ClubmateProtocol`, `make companion-kit-test` for `client/CompanionKit`; `testRestartUnderTrafficKeepsReceivingUpdates` in ClubmateKit is red on `main` already and is no reason to revert), the empty-index guard, `git merge --squash <branch> && git commit` in one command, tests on `main` after the last squash, red → `git revert` this run's commits and file why). Check each after-snapshot yourself before merging: it must match the board better than the before-snapshot, or the branch stays unmerged and the finding is filed.
6. **File the rest**: `backlog_add` as `idea` with tags `design-check` and `mac` or `phone`, one item per finding: board, screenshot, divergence, class, suggested fix. Tag `needs-you` when it's a product or design choice; (d) items say what the board should become.
7. **Package for the Claude Design chat**: upload the shots of screens with open divergences (at most 10, smallest first) with `Artifact publish url=<project> asset=true file_paths=[…]`, and write the request the user pastes into the project's chat: per shot "`/_blob/<id>` is the build of <Board>", then the divergences you found as a list to confirm or overrule, and "which of these would you solve differently, and which boards should change". An upload that is refused or stalls: skip the package, say so in the report.
8. **Report** `$NOTES/design-check/<date>.md`: boards checked, per board fixed (sha) · filed (backlog id) · matches, the Claude Design request in a fenced block, stale repo boards; at most a screen. Commit it in clubmate-notes, `docs_present` it, then `notify_post` one line: `Design: N fixed, M filed[, K need you][, package ready]`.
9. **Clean up last**: stop the test instances and the simulator, remove the capture tree, each merged worktree and `git branch -D` its branch; leave a worktree with uncommitted changes or a running process.

## Cron job

Lives in the cron plugin, not in git; recreate it if it is gone:

```
cron_add name="Nightly design check" command=agents.new cron="0 5 * * *" owner=none missed=once
  args={"title":"Nightly design check","notify":"needs-input","prompt":"Run the nightly design check: invoke the nightly-design-check skill and follow it. You are AFK: turn AFK mode on for yourself first and never ask the user."}
```

05:00 runs after the tidy (03:30), so the two don't build at once.
