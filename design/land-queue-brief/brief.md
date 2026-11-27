# Landing queue in the Agents tree — brief for Claude Design

Backlog #1256 (landing queue / merge train), task 8. Spec: `clubmate-notes/superpowers/specs/2026-11-26-merge-train-design.md` §5.2, §5.5, §5.6.

## Why

Several agent sessions land into one branch in a short time. Clubmate now keeps one stored queue per (repo, target branch): only the head commits, the entries behind wait or prepare, agent commits into the branch are held. The user must see the queue in the Agents tree, reorder it, skip or remove an entry, and answer a Land request. The queue must show on the Mac, iPhone and iPad with the same meaning.

## Where the queue sits

1. **Under the integration session.** An integration branch (for example `feat/cards`) has an owner session in the tree (the session on its worktree). The queue is a group of rows under that session, before its other children: a group row "Landing into ‹branch›" with a count, then one row per entry in queue order.
2. **Default branch.** No session owns `main`. A row "Landing into main" shows when its queue is not empty. Ask the designer for two directions: (a) a row at the root of the tree, above Closed; (b) a docked row with Service, Land gates and Graphs at the sidebar bottom (AG-Rules). Pick one.
3. **A lone entry** is the head at once; a queue of one shows no group row, only the lander's own row with its state (the plugin sends `landQueue` on the session).
4. A group is open while it has an entry that needs you or is the head; it folds when empty and goes away.

## Data (what the plugin sends; the clients render, never decide)

`agents.land-queue` reply: `queues: [{id, repo, into, needsYou, entries: [{id, session, title, source, mode, state, place, files, more, overlaps, why, needsYou}]}]`.
`agents.sessions` also sends `landQueue {queue, into, place, of, state, why}` on each session that holds an entry.

| Field | Values |
|---|---|
| `state` | `waiting` (has a place), `ready` (called `git_land`, merges clean on the current head), `head` (its turn; may be reviewed, tested, committing), `asks` (head, approved, waits for the user's yes), `blocked` (cannot land: dirty overlap or conflict; `why` names it) |
| `mode` | `branch` (a branch), `index` (a change staged in the target worktree), `dirty` (uncommitted edits that overlap the head's files) |
| `place` | 1-based position; the head is 1 |
| `files` | up to 10 changed files of the landing, `more` = how many are cut |
| `overlaps` | ids of other entries that change the same files |
| `why` | one short line: "waits for edits in a.go", "conflicts in b.go — rebase before your turn", "waits for your yes" |
| `needsYou` | decided in the plugin; the UI never derives it |

Also needed marks, no extra state: **speculative** (entry 2 is reviewed ahead on the head's predicted tree; setting, off by default) and **carried** (its approved review was kept across a base move on other files). Shown as a small neutral mark with a tooltip, never as a tone.

## What the user can do (user-only commands)

- Move up / down (reorder), Skip once, Remove (the session is told). In a menu on the row (•••, right-click) with keys if the design wants any; no key is decided.
- Land card for `asks`: the existing Land request card (`LandRequestCard`, `docs/agents.md` § Transcript folding, boards `design/features/land-review/`) answers it. It also shows in the queue row as the action, not only in the transcript.
- Select an entry row: opens the lander's session, as any child row.
- Overlapping files on demand: expand the row or a popover: the files, the ones shared with another entry marked, "+n more".

## Entry row

Same row anatomy as the tree (`docs/agents.md` § Task tree): a glyph, the title (truncates), a detail on the right. Detail: a state word of at most 9 characters, or the place for a waiting entry.

Proposed words, glyphs and tones (the designer rules; words ≤ 9 chars, tones only success, warning, danger, accent, none):

| State | Word | Glyph | Tone |
|---|---|---|---|
| `head`, running review or test | reviewing / testing | `play.circle` | accent |
| `head`, committing or waiting for its prompt | your turn? / landing | `play.circle` | accent |
| `asks` | asks | `exclamationmark.circle` | warning |
| `blocked`, owner is a session | blocked | `minus.circle` or `xmark.circle` | none or danger |
| `blocked`, owner is the user (needs you) | blocked | `exclamationmark.circle` | warning |
| `ready` | ready | `circle` | none |
| `waiting` | place text "2" or "waits" | `circle` | none |

Group row detail: "3 in line" or, when something needs the user, "1 needs you" amber (as the task parent's "2 need you"). A queue that needs you lifts the owner's summary the same way (`taskSummary`).

## Platforms

- **Mac sidebar**, 1280 Mate dark and 640 Mate light (the sidebar is a drawer there). The current tree: `Main` (Agents-OneTree-1280-Mate) and `Today-Mac-640` on the Today page.
- **iPhone** list (Today-Phone): a queue is a section or a grouped card in the Agents list, with its entries; a tap opens the entry's session; the actions (move, skip, remove) are a swipe or a ••• menu, not a table. Needs-you entries also appear in Needs you at the top.
- **iPad** split view (Today-iPad): the same as the Mac sidebar, in the sidebar column.

## Constraints

- Mate palette only, tokens from `cm.css` and `c.css`; colour is state (warning = needs you, danger = failed, accent = the head that runs); no new colour, no decoration.
- The meaning of every state is decided in the plugin; the board must not depend on a rule the client would have to compute.
- Names over ids: show the lander's title and branch, never an entry id.
- No keys added unless the design needs one; `docs/shortcuts.md` decides.
- Same behaviour on Mac, iPhone and iPad.

## Round 1 (≤ 6 boards on page `r1`)

1. Mac 1280 Mate dark: integration session with the queue — a head that tests, a ready entry with overlapping files shown, a waiting one with a speculative mark, a carried mark, a blocked one; the group row; the row menu open.
2. Mac 1280: the default-branch queue, directions (a) root row and (b) docked row, side by side or one board each; the designer picks.
3. Mac 640 Mate light: the same queue in the drawer; an `asks` entry with its action; overlap popover.
4. iPhone dark and light: the Agents list with a queue section; the entry actions.
5. iPad split view dark.
6. Rules board: the state table (word, glyph, tone, when), what folds when the row is narrow, the marks, what needs you.

Ask for the designer's pick per question and a short ruling, one line per question.
