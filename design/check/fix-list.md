# Clubmate — fix the build after the 1 Nov design check

Claude Design's answer to `design/check/prompt.md`. Boards: `design/language/png/` (Mac, `K1-Check-Mac`) and `design/companion/png/` (`C-*`, `K2-Check-Companion`); `design/png/…`, `design/html/…` and `design/source/…` below mean those folders.

## 1. What this is

The Mac app and the iPhone companion were checked against the design boards on 1 Nov 2026 (build b91c54f2). The results are in two report boards:

- `design/png/K1-Check-Mac.png`: Mac. Each divergence has an impact level, a class and a fix.
- `design/png/K2-Check-Companion.png`: companion, cross-screen rules, verdicts, decisions and open questions.

Class, as in the reports:
- **a**: token or renderer change in the native client (one place, all views).
- **b**: needs an SDUI prop or node.
- **c**: plugin content (words, grouping, order).
- **d**: the board changed to match the build. Nothing to do.

Your task: implement every **a**, **b** and **c** item, in the priority order below. Match the boards named in each item. HTML versions of every board are in `design/html/`, and the raw canvas files are in `design/source/`. Ignore seed data (names, counts, text) unless the structure differs.

## 2. Decisions that are final

1. **Permission answers are the same on both clients.**
   - Title "Run a command?". The command goes in a mono box.
   - The request's `description` becomes the caption, always attributed: "Claude says: …".
   - Buttons: **Allow once ⌥1 · Always allow… ⌥2 · Reject ⌥3**. Reject always stays in the card's ••• menu.
   - Boards: `Agents-1280`, `Agents-640`, `C-Permission`.
2. **Always allow on the Mac asks first.**
   - ⌥2 opens a popover anchored to the button. It shows the rule that will be stored (e.g. `Bash(go test:*)`) and its scope ("in shop … on this Mac").
   - Cancel ⎋ is the default. Pressing ⌥2 again confirms. Nothing is stored before that.
   - The phone already has the matching confirm sheet.
   - Board: `Mac-AlwaysAllow`.
3. **Offline Mac.** Its waiting items stay in "Needs you", dimmed and read-only, under a caption "‹Mac› is offline — answer when it's back". Rows still say "as of 13:43". Board: `C-Offline`.
4. **Code chat on a graph step.**
   - The chat panel never opens on a graph step by default. It opens the last regular session, or a new chat.
   - If the user picks a step by hand, the panel shows these, and the composer stays off until the loop has started that step:
     - "‹Step› is a step of ‹Graph›";
     - the state of the steps;
     - Open in Graphs and New chat, with no keys printed. ⌘1…⌘9 follow the user's rail order and ⌘N does nothing in Code, so neither key would be true (decided 7 Nov).
   - Board: `Mac-ChatGraphStep`. `Code-1280` now shows a regular new chat.
5. **⌥1–3 mean permission answers only.** Chat suggestions use bare 1 2 3 while the composer is empty, or no keys at all.

6. **The interface is always Mate; the terminal and editor themes belong to the user.**
   - Every window and plugin panel uses the Mate palette, dark or light following the macOS appearance. The interface no longer takes its roles from Ghostty's palette.
   - Clubmate ships `themes/ghostty/Clubmate`, `themes/ghostty/Clubmate Light` and `themes/clubmate.nvim` (with a lualine theme). The embedded Ghostty and the Code editor use them by default, switching with the appearance.
   - If the user's Ghostty config sets `theme =` or their nvim sets a colorscheme, theirs wins and the interface stays Mate.
   - Boards: `T-Themes`, `Principles`, and the section "Mac interface in Mate" (`Agents-Mate-1280`, `*-1280-Mate`, `*-640-MateLight`).

7. **Drawn views: cards, ports, edges, lanes (3 Nov).** The Graphs drawn view (main 43f02ac2) is the bar. The same parts now draw Cron, Triggers and Backlog where the thing has a shape; everything else stays a form or a list.
   - Graphs: the run view is on `Graphs-1280-Mate` / `Graphs-640-MateLight` as built; the definition is on `Graphs-Def-*`, where each edge opens as its trigger.
   - Cron: "Does" is drawn (schedule → action card with outcome ports → owner session or Inbox), and Runs are a time strip (ok, failed, missed, next, now, paused). Identical consecutive results collapse to one row. Boards: `Cron-Runs-1280-Mate`, `Cron-Paused-640-MateLight`, `Cron-Empty-640-MateLight`.
   - Triggers: a trigger is an edge, cause card → effect card. A graph's trigger is drawn inside its graph; a user's trigger shows where its event came from last time. Runs are lanes, cause → effect. Boards: `Triggers-Edge-1280-Mate`, `Triggers-Chain-640-MateLight`, `Triggers-Empty-640-MateLight`.
   - Backlog: the list and the form stay; Owners are lanes (you, sessions, graph runs). A task has Links to other tasks (blocked by, blocks, relates to; `#n` in the description renders as a link) and Attachments (worktree files, design boards and other artifacts, images, URLs); Start agent passes the description, links and attachments to the session. The primary action follows the owner: Start agent while you own it, Open ‹owner› ⌘↩ while a session or graph run does, Take back returns it. Boards: `Backlog-Owned-1280-Mate`, `Backlog-Start-640-MateLight`, `Backlog-Empty-640-MateLight`.
   - The vocabulary, the per-screen table and the rules are on `Drawn-Views`.

8. **Agents sidebar tree (main cdc26f6a, 3 Nov): accepted, with three rules.**
   - Closed (archivebox, folded) and Service (gearshape.2, folded; opens on its own while a service session needs you, its count in warning ink) sit once at the root, after every branch.
   - A branch row shows when open sessions live in two or more worktrees, **or** when they all live in one worktree that is not the window's worktree (the one in the title pill). Otherwise the list has no header: the pill and the session header already name the branch.
   - When Closed holds sessions from two or more worktrees, each closed row's detail is its worktree's short name (`design-brief`, `3.1`, `main`) instead of the age. Closed lists the newest first and shows a handful, then "N more · search to find one".
   - Boards: `Agents-1280`, `Agents-Mate-1280` (two worktrees), `Agents-OneTree-1280-Mate` (one worktree, Closed and Service open), `Agents-Drawer-640-MateLight` (⌘B at 640). The phone does not change.

9. **Steps in graphs: nodes Clubmate runs itself (3 Nov).** A check's exit picks the next edge; an action does the work. Full rules on `Graph-Steps`.
   - Kind by shape, not colour: agents are round cards with a sparkle; steps are square with a left rule and a `$ command` strip. A small word at the right says agent, check or action. On the phone, a round state badge marks an agent and a square one a step.
   - States: not started · running (elapsed, progress bar with a timeout) · waiting for you (amber) · pass (teal) · fail (neutral: a route, not an alarm) · error (red: timed out, interrupted by a restart, rejected, not found). A step that's done shows its duration.
   - Approval reuses the permission card: the exact command, the folder, Allow once ⌥1 · Always allow… ⌥2 · Reject ⌥3. It is a needs-you state on the card, the run row, the bell and the phone.
   - Output: selecting a step opens its output panel beside History (the tail, exit code, duration, Full output ⌘O, Copy). An empty output says so. A fail edge passes the tail to the next agent.
   - Edge caps: "at most N" in the definition and ×n/N on a run. When the cap is hit, the edge turns amber and the run waits for you: Continue ⌘↩ allows one more round, or Stop run ⌘.
   - Trust: a graph file whose steps changed since you trusted it is untrusted. Run becomes Trust… ⌘↩, which lists every command and marks what changed.
   - History: a step's move is a bar from start to exit; rounds get a bracket, so a loop reads as a loop.
   - Boards: `Graphs-Gate-1280-Mate`, `Graphs-Nightly-1280-Mate`, `Graphs-GateDef-1280-Mate`, `Graphs-Approve-640-MateLight`, `Graphs-Cap-640-MateLight`, `Graphs-NightlyDef-640-MateLight`, `C-Step-Approve`, `C-Step-NeedsYou-Light`.

10. **Review of an agent's changes (Code section, main dbb3ef65): a native panel plus nvim panes.** Full rules on `Review-Rules`.
   - nvim draws only the two Diffview panes in the Clubmate theme (T3 diff colours).
     - Review profile: `showtabline=0`, absolute line numbers, no relative numbers, no cwd or `Help: g?` lines.
     - Winbars lead with what you compare ("Before turn 3" | "Working tree"), then a tag, then a path that truncates from the left.
   - Comments are virtual lines that wrap at the pane width, with a left rule in their tone. The other pane gets the same number of filler lines, so both sides stay in line. A resolved comment collapses to one faint line.
   - New file: the left pane is filler with "New file — nothing before turn 3", and the right pane is all added fill. A deleted file is the mirror.
   - The native Mate panel is the Code section's right column, with tabs Review | Chat (⌘B). It runs on the diffview host API.
     - Its header is pinned: progress, +/−, open comments, and Show filters as checkboxes.
     - File rows read: state (○ to review · ✓ reviewed · ● changed since you reviewed), name, a kind badge (new · deleted · renamed), an amber comment count, then +/−. Folders show reviewed/total.
     - Below the tree: the comment list, the keys, Send and Copy ,re, and a legend.
   - Done: when the last file is ✓, the header turns teal and "Send review" becomes primary. The send sheet lists the comments and takes an optional note. Comments go to the session that made the changes, and the review skill reads them as a structured Review message.
   - Below ~900 pt of editor width the review uses Diffview's inline layout, and the panel becomes a drawer under a one-line strip.
   - Agents: a session row shows "review 5/15 · 1", and its header shows the review state with Open review ⌘R. After Send, the transcript shows a Review card. Files the agent then changes turn ● in the review.
   - Boards: `Review-Open-1280-Mate`, `Review-Mid-1280-Mate`, `Review-NewFile-1280-Mate`, `Review-Done-1280-Mate`, `Review-Agents-1280-Mate`, `Review-Mid-640-MateLight`, `Review-Deleted-640-MateLight`, `Review-Done-640-MateLight`.

## 3. Work list, by priority

### P1b: drawn views (decision 7)

- **SDUI nodes** *(b)*: `flow` (cards, rows with ports, edges with a state: taken · last · untaken · definition · selected · dim · ghost; markers: start, end, schedule), `lanes` (actors, moves, causes), `runstrip` (runs on a time axis, now, next, paused span). One renderer serves Graphs, Cron, Triggers and Backlog.
- **Results in words** *(c)*: no `null` or JSON blobs in runs tables; Show as JSON per run. Group identical consecutive results ("ok ×18 · same result").
- **Trigger chain** *(c)*: for a user's trigger, the session that emitted the event last time, and the cron job or trigger that started it.
- **Task owners** *(c)*: owner and last owner on every task (you, a session, a graph run), with the moves between them.
- **Task links and attachments** *(c)*: `links` (blocked by, blocks, relates to) and `attachments` (file, artifact, image, url) on a task, passed to the session by Start agent; `#n` in Markdown renders as a task link.
- **Empty lists** *(a)*: ghost cards plus 2–3 templates, as on the `*-Empty-640-MateLight` boards.

### P0: wrong state or meaning, fix first

- **Inbox popover (Mac) is unreadable.** It mixes the theme's light text with the system light background. A surface is either wholly themed (theme bg + fg) or wholly system (system bg + label colours). Popovers use system labels. *(a)*
- **Approval sheet (Mac).**
  - It says "2 entries, shown below" but lists three file headers, with teardown.sh twice. De-duplicate it; the count must match what is shown. *(a)*
  - ⏎ and Esc must trigger **Deny**. Allow is never the default. *(a)*
  - Hierarchy: the question, workspace and command on top. Repo, worktree, size and sha256 below, smaller and mono. *(a)*
- **One status map for both clients:** glyph + word + tone. *(a)*
  - Failed = danger ✕ and "exit 1" / "Stopped · error". The Mac currently shows ✓ for a failed session.
  - Running = accent play-in-circle. The Mac shows a dotted grey circle.
  - Waiting = amber everywhere. Today it is accent blue in the Graphs list and grey in the Agents sidebar.
- **Companion workspace list.** Never fold a session that needs you. The subagent asking a question is a nested row, one step in (24pt), with no separator between it and its parent. Board: `C-Workspace`. *(a)*
- **Stop, one per surface, never beside send.**
  - Mac: keep the header Stop (with the ⎋ hint) and remove Stop from the composer. *(a)*
  - Phone: remove the full-width Stop from subagent cards. The card shows a spinner and "Running · 1 min", and Stop goes in the card's menu with a confirm. Board: `C-Permission`. *(a)*
- **Graph-owned triggers.** *(c)*
  - No Delete in the bar; it goes in ••• with a warning.
  - Show "created by graph ‹name›" plus Open graph G.
  - Owner is the graph, not "none".
  - Board: `Triggers-640`.
- **Graphs.** Remove is not in the bar while a run is running; it goes in •••, only when stopped. *(c)*

### P1: hierarchy

- **Mac session header.** Add status, agent, branch and mode ("Claude · docs/… · Ask before edits", plus a "waiting for you" badge), as on the board. *(b: the session header node needs `status`, `agent`, `branch`, `mode`)*
- **Mac permission card.** Apply decision 1. *(a)*
- **Mac diff card.**
  - File bar on top with Open in nvim ⌘O and Full diff ⌘D.
  - Hunk header and line numbers.
  - No raw `--- a/` or `+++ b/` lines.
  - *(a)*
- **Companion "Needs you" banner** (a request from another session).
  - It sits inline at the top of the scroll area, never over the transcript.
  - It uses a neutral surface with an outline glyph; the filled amber glyph is reserved for this session's own request.
  - The second line names the Mac when two or more are paired: "● MacBook Pro · infra · wants to run …".
  - Board: `C-Permission`. *(a)*
- **Companion question screen.** Restore the "Started by · ‹parent›" link to the parent session. *(a)*
- **Running row on the phone.** Say what the session is doing ("Editing reserve.go · 4 min"), not "Running". *(b: the last tool title per session in the list payload)*
- **Triggers detail.** *(c)*
  - "When" in words: "an agent turn finishes · status done · session Review loop · Coder".
  - "Does" in words, with the prompt in a card clamped to 3 lines, plus Show all and Show as JSON.
  - Group graph triggers under "from graph ‹name› · N".
  - Add Run now ⌘R.
- **Cron detail.** *(c)*
  - Split "Missed" (what happened, in warning ink, with a Run it now button) from "When missed" (the policy).
  - Header badges: "active" and "missed 1 run".
  - "Does" in words, not `agents.new`.
  - Next run with a relative time ("in 13 h").
  - Owner "nobody — results go to the inbox"; add the Worktree row.
  - Add Edit E and Duplicate. Delete goes at the far end of the bar.
  - Every list row shows its next time, or "paused".
  - Board: `Cron-1280`.
- **Mac Inbox popover copy.** Use the phone's: kind + what it wants as the title ("Permission · Run go test ./…"), session as the subtitle, and an age on every row. *(c)*
- **Backlog, Mac.** *(a)*
  - Drop the status and priority badges from the header.
  - Show the title once.
  - "#n" goes at the right of list rows.
  - Delete goes at the far end.
  - Board: `Backlog-1280`.
- **Backlog, phone.**
  - Inset grouped lists, as on Agents.
  - Section counts at the right.
  - Board: `C-Backlog`. *(a)*
- **Code.** When nvim draws its own statusline, Clubmate's bar shows only app items plus "⌘J chat · ⌘L focus chat". *(a)*

### P2: spacing, type and colour

- **Priority.** Neutral glyphs on both clients: `!!` high, none for normal, `↓` low, in secondary ink. No accent, amber or orange badges. Boards: `Backlog-1280`, `C-Backlog`. *(a)*
- **Latte.** Warning fills use the warning tone with a dark glyph; the warning ink is for text only (fills look brown today). *(a)*
- **Selected filter chip.** Filled pill. The accent ring is for keyboard focus only. *(a)*
- **Needs-you state words.** Short, in warning ink: "waiting". *(a)*
- **Processes.** Truncate long commands in the middle and keep the executable and args. Owner list: icon at the left, CPU % at the right. *(a, c)*
- **Thinking row.** Show the duration ("Thought for 4 s"). *(b: the `thought` node carries its duration)*
- **Phone workspace row.** Truncate the command and keep the time. *(a)*
- **Phone bullets.** Body size, in the label colour. *(a)*
- **Title bar with the Mac's dot.** It must keep the theme's surface-1 whether the dot is on or off and whether the window is key or not (the dot capture showed it system grey in Kanagawa). *(a)*
- **Names over ids.** No `agents.new`, `agents.prompt`, `agt_…` or `gr_…` in the UI. A detached worktree shows as "detached · 03a9ff5f" in faint ink. *(c)*
- **Case.** Sentence case in themed UI ("New worktree…"). Title Case only in system Settings and menus. *(c)*
- **Composer placeholder by state.**
  - idle: "Message Claude…";
  - busy: "Queue a message";
  - asked: "Answer Claude…".
  - "/ for commands, @ for files" moves to the hint row.
  - *(a, c)*
- **Order of "Needs you".** Oldest first. The phone already does it; the Mac popover and the Agents filter should too (confirm, see §5). *(c)*

## 4. Don't change

- The decisions that already overrule the boards:
  - failed tool calls stay folded;
  - the Mac minimum width is 800pt;
  - diff fills at 15%;
  - Backlog autosave with "✓ Saved ⌘S";
  - the accent and Mac-colour rules;
  - decided during the fixes (1–2 Nov): done plan steps fold behind "Show done" on the Mac, iPhone and iPad, and the plan count is steps done ("1 of 3"), not the phone board's "2 of 3"; on iPad the actions card lists its first three calls (like the Mac's open group) while the iPhone keeps one folded row; Cron edits save explicitly with ⌘S (no autosave); board keys that clash with `docs/shortcuts.md` are not bound (⌘8 Open in Graphs, ⌘N New chat, ⌘J/⌘L chat, ⌘O Open in nvim; Processes Kill stays ⇧K); command titles are sentence case at the source (`plugin.json`); the iPad section switch (Agents / Backlog / Docs) lives in the folder menu.
- Gaps already tracked in Graphs, Processes, the Code chat, the Agents graph row and the Plan caption.
- What the boards took from the build (class d):
  - the Settings → Remote native form;
  - "+ New · New in worktree…";
  - "Context ⌘I";
  - "Claude says:";
  - the ••• in the permission card;
  - the composer footer hint;
  - the grey disabled Send;
  - "Context compacted";
  - Agents | Backlog;
  - the Done section;
  - the escaped control characters in the approval sheet.

## 5. Answered (1 Nov 2026)

1. Approval sheet: ⏎ and Esc trigger **Deny**; Allow is never the default. Yes.
2. "Needs you" oldest first on the Mac too (bell popover and the Agents filter). Yes.
3. The Mac's name stays read-only, taken from the computer name. Yes.
4. The coloured rim around the Inbox popover is not ours: no code draws it and it stays on a wholly system popover; it is how the off-screen capture renders the popover's glass edge.
5. Processes-640, x-Palette, x-Switcher and the Processes action bar with a row selected: re-capture them with the rest.
6. Duo boards are out of scope for now; iPad is in scope again since 1 Nov (the user started testing it).
7. Processes Kill stays on ⇧K (the board's bare K is not taken).
8. The phone gets the Mac's recovery actions under a failed session where they work remotely (from the `failure` field the Go plugin sends); Mac-local flows such as sign-in read "Sign in on ‹Mac›".
9. The "Notifications" row in `C-MacInfo` is not built: no per-Mac notification setting exists yet (board change / later; backlog #115).
10. Remaining Title Case in themed UI (Welcome, workspace switcher) goes to sentence case; app menus and system dialogs keep Title Case.
11. Cron owner "nobody" (3 Nov): only failures and missed runs go to the inbox; ok results stay in Runs.
12. Graphs-Def (3 Nov): a definition has no triggers until it runs, so its edges show a tooltip only; an edge opens its trigger on the run view. "Open in Triggers ⌘7" is not bound (⌘7 is the Triggers section).
13. Empty Cron and Triggers lists (3 Nov): ghost cards and one sentence, no templates; there is no create UI for jobs or triggers yet.

## 6. Done when

- Every item above is implemented, or answered in §5.
- The screens are re-captured with the recipes in `design/brief/README.md` and `design/companion-brief/README.md`, and each shot matches its board apart from seed data.
- One short note per item: what changed, which file, and anything left over.
