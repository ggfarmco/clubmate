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
     - Open in Graphs ⌘8 and New chat ⌘N.
   - Board: `Mac-ChatGraphStep`. `Code-1280` now shows a regular new chat.
5. **⌥1–3 mean permission answers only.** Chat suggestions use bare 1 2 3 while the composer is empty, or no keys at all.

## 3. Work list, by priority

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
  - the accent and Mac-colour rules.
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

## 6. Done when

- Every item above is implemented, or answered in §5.
- The screens are re-captured with the recipes in `design/brief/README.md` and `design/companion-brief/README.md`, and each shot matches its board apart from seed data.
- One short note per item: what changed, which file, and anything left over.
