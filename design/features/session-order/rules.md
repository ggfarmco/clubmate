# Session order — decided 16 Nov 2026

Direction C, "Inbox over a ledger" (Claude Design round 1, canvas "Clubmate · session order"). Boards: `R1-Directions`, `R1-Mac-1280-Mate`, `R1-Phone-Dark`, `R1-Mac-640-MateLight`, `R1-Phone-Light`, `R1-Handoff` (`.dc.html`), all in one page `boards.html`. A and B are drawn on the same boards and were not picked. Brief: `design/session-order-brief/`.

The user's pain: the Agents list jumps whenever something updates, and on the phone a new session takes the place of the one about to be tapped. It must be clearly better than today.

## The rules

1. Inside a group, rows go by start time, oldest first; the session id breaks ties. Activity (`updatedAt`) never orders anything.
2. A new session goes last under its parent: its worktree group, or the session that started it. Children follow the same rule.
3. Groups: the workspace's home (`main`) first, then the other worktrees by the start of their earliest session, closed sessions included, so a group keeps its place when its first session closes. Closed and Service stay folded at the end.
4. A state change never moves a row in the tree: only its glyph, tint and right-hand detail change.
5. A Needs you inbox sits on top of the tree: a copy (mirror) of each top-level session that needs you, longest waiting first; Open follows the same way when it has rows. The session keeps its row in its worktree too. The inbox is hidden when empty.
6. Nothing the user can see moves on its own: a change that would move a visible row waits, a pill names what waits ("1 needs you · 1 new" on the phone, "2 to show" on the Mac), and the queue lands when the user looks away or asks.

## Deviations from the boards

- **No ⌘R.** ⌘R in Agents already opens a session's review (`docs/shortcuts.md`). The Mac pill has no key of its own: it lands on a click, 0.5 s after the pointer leaves the sidebar, or when the sidebar gets keyboard focus.
- **Land review rows.** Handoff item 3 is dropped: another change (session agt_b999a016ba883265) removes reviewer rows from the tree, so there is no reviewer row to place; the lander's detail carries the words.
- **Group key.** "When the worktree was created" is the earliest session's start in that worktree (closed ones count), not a new stored date. No new plugin state.
- **Tint fade and the unseen dot** (handoff 18–20) are left for later (backlog), not in the first landings.

## Landing order

Each landing is strictly better than today on its own; an inbox never ships on a client without that client's hold gate, and the phone never loses Needs you on top before it has the inbox.

1. **Order.** Plugin: rules 1–3 for the Mac tree, and the same order sent to the phone as stable sort keys in `agents.sessions` (not dense positions, so one insert does not change every row's delta hash). Phone: keeps its state groups for now, but sorts Running, Done and children by the plugin's key instead of `updatedAt`. `docs/agents.md` § Session order.
2. **Mac inbox and hold.** Plugin: inbox section with mirror rows (rule 5) in the sidebar tree. Mac renderer: mirror rows select their session; the hold gate and pill (rule 6).
3. **Phone tree, inbox and hold.** The phone renders the worktree tree in the plugin's order with the inbox on top and the All / Needs you / Open / Running chips; state groups go; the hold gate and the top-bar pill (rule 6).

## Handoff (from R1-Handoff, with the deviations applied)

| # | Change | Where | Tag | Landing |
|---|---|---|---|---|
| 1 | Sort by start time, oldest first, id tie-break | plugin | c | 1 |
| 2 | New session last under its parent | plugin | c | 1 |
| 5 | Groups: home first, then by earliest session start | plugin | c | 1 |
| 6 | State never moves a row; counts update in place | plugin | c | 1 (Mac), 3 (phone) |
| 7 | Needs you inbox on top, longest waiting first; Open after it when it has rows; hidden when empty | plugin | c | 2 |
| 8 | Mirror row points at its session; selection, keyboard and the Needs you filter treat both as one | SDUI | b | 2 |
| 9 | Closed and Service unchanged | — | — | — |
| 10 | In-place detail words ("Wants to run git fetch", "Waiting for 1 child", "Done · now") | plugin | c | 1–2 |
| 11–13 | Phone renders the plugin's tree; worktree headers; chips from the plugin's filter definitions | phone | a, c | 3 |
| 14–16 | Hold gate: a change that would move a visible row waits; off-screen changes land at once with scroll anchoring; restyles land at once; pill with counts from the queued diff; when the queue lands (phone: list reappears, app foreground, pull, pill tap; Mac: pointer off 0.5 s, pill click, keyboard focus) | Mac, phone | a | 2, 3 |
| 17 | Selection keyed by session id; mirror and tree row selected together; ↑↓ walks visible rows | Mac | a | 2 |
| 18 | Change tint, fades 1.5 s, off under Reduce Motion | Mac, phone | a | later |
| 19–20 | Unseen dot shared across clients | SDUI, plugin | b, d | later |
