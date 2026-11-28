# Resource slots UI — rules (Claude Design, round 1, 28 Nov 2026)

Ruled by Claude Design on the canvas "Clubmate · resource slots" (https://claude.ai/artifact/FYiPn85steeaVozzQAYSVF, page r1), from real test-app and simulator shots (`today/`). Backlog #1353, #1360. The agent's earlier draft is in `rounds/draft/` (not a ruling).

| Board | Shows |
|---|---|
| `r1-Mac-1280-Today` | Mac 1280 Mate dark today: row cut to "Companion U… waits for tes…", spinner on the Bash card, header says nothing |
| `r1-Mac-1280-Pick` | Mac 1280 pick: row word `test slot` / `trust`, hourglass / shield, header line after the title, card status `waits for test slot` |
| `r1-Mac-640-Light` | Mac 640 Mate light pick: header line drops segments; expanded card with "Held by" and "waited 3 min of 60"; drawer rows |
| `r1-Settings-Edit` | Settings › Agents › Resource limits › Edit, today / pick (inline, fixed) |
| `r1-Phone-iPad` | iPhone (dark, light) and iPad: header, Bash card, list rows, today / pick |
| `r1-Rules` | The rules below as a board |

PNG next to each `.dc.html`; all on one page in `boards.html` (config `brief/round-1.json`).

## Principle

The plugin decides every field: `landing.words` (the Mac row word, 9 characters at most), `landing.line` (a list of segments for the phone and the headers), the status icon and the card's `statusWord`. Clients draw them. They never shorten a word or work out a state.

## 1. Mac Agents row: the word wins

Both options together: the plugin sends a short word, the word keeps its full width, the title cuts.

| Plugin state | Mac word | Glyph | Tone | Needs you | Line (iPhone · iPad · headers) |
|---|---|---|---|---|---|
| waits for a slot, kind `test` | `test slot` | `hourglass` | none | no | `Waits for test slot · next in line · 1 of 1 in use · 3 min` |
| waits for a slot, kind `sim` | `sim slot` | `hourglass` | none | no | `Waits for sim slot · 2 ahead · 2 of 2 in use · 40 s` |
| waits for a slot, `‹id› slot` > 9 characters | `‹id›` (e.g. `android`) | `hourglass` | none | no | `Waits for android slot · …` (the line always names the kind in full) |
| holds a slot | duration (`6 min`) | `play.circle` | accent | no | `Running · 6 min`. Holding has no mark on the row; it shows in Settings › Now and on the waiter's card |
| waits for trust | `trust` | `exclamationmark.shield` | warning | yes | `Waits for trust · 2 min` |
| max wait passed, command not run | duration | `play.circle` | accent | no | back to running. Only the card keeps it: `not run · no test slot in 60 min`, tone none; the agent is told why |

- RowFit's half-row rule for a detail goes. The word slot is never truncated (9 characters ≈ 65 pt at 12 pt mono). At a 248 pt sidebar the title gets 18 characters at depth 0 and 16 at depth 1, so "Companion UI tests" fits whole. A word over 9 characters is a plugin bug: it is drawn whole and the title gives way.
- Tooltip on the row: `‹title› · ‹full line›`.
- `waits for trust` leaves the Mac row: the row says `trust` with the shield; the phone and the header keep the sentence.
- Settings caps a kind's Id at 9 characters (`a–z`, `0–9`, `-`). The plugin sends `‹id› slot` when it fits in 9, else `‹id›` alone; the hourglass already says "waits".
- Case: phone lines start with a capital (`Waits for test slot`, like `Running`, `Editing`); Mac words and the Mac header line stay lower case, like every other Mac row word.
- Phone and iPad list row: the status circle turns neutral grey with the hourglass (today's green play says "running" while nothing runs); trust keeps the filled warning circle; the line is cut at its end.

## 2. Settings › Resource limits › Edit: inline, fixed

Not a modal sheet: a sheet would need presentation work in the Settings window for a form that edits one kind at a time, and inline keeps the live Now block next to the kind's own row.

- Edit opens the kind's own row in place: Name, then the mono Id and its live summary. Add kind opens an empty one above the Add kind button. One open at a time; Edit on another kind asks whether to keep or discard unsaved changes.
- Title: the kind's Name (`Tests`), never "Tests slots".
- Card: window background inside the group, 1 pt hairline, radius 8, 14/16 pt inside, 12 pt between blocks. The note sits inside the card, never against its edge.
- Fields: a 120 pt label column, leading-aligned, 12 pt to the control; 24 pt controls, 8 pt between rows. The Patterns label lines up with its first line; under Patterns the hint `One regular expression per line, matched against the whole Bash command.`
- Mono: Id, Patterns, and the commands in Now. Commands truncate in the middle, so the matched word (`test`) stays visible.
- Now, under a hairline: summary (`1 of 1 in use · CPU 34 %`), then `Holding` (title · command · time · Release) and `Waiting` (place · title · command · time). Release only on holders. An empty list hides its heading; both empty: `Nothing in use`.
- Footer, under a hairline: `Remove…` leading and alone (it confirms); `Reset to defaults · Cancel · Save` trailing, 8 pt apart; Save is the default (⌘↩), Cancel answers esc.

## 3. Where the wait shows: the card and the header too

| Surface | Mac | iPad | iPhone |
|---|---|---|---|
| Tree / list row | hourglass + `test slot` | neutral circle with the hourglass; the line, cut at its end | same as iPad |
| Session header | status glyph becomes the hourglass; the line follows the title in the secondary ink, then `Claude · main` in the faint ink | subtitle: the whole line · workspace | subtitle: first and last segments · workspace: `Waits for test slot · 3 min · clubmate` |
| Bash card | no spinner: hourglass + `waits for test slot` at the right edge. Expanded, under the command: `Held by ‹session› · ‹command› · ‹time›` (the title opens that session) and `Next in line · waited 3 min of 60` | right edge, as on the Mac | no room at the right: a second line under the command, `Waits for test slot · next in line` |
| "Working…" row | — | hidden while every running call in the turn waits for a slot; back with its spinner when one starts | same |

- `landing.line` is a list of segments joined by ` · `. The first segment is the state and never drops.
- Place reads `next in line` or `‹n› ahead`, not "2nd in line": "2nd" counts the holder, while `1 of 1 in use` counts holders apart, so the two would disagree.
- Mac header folding: `Claude · main` folds first, then segments drop from the end, then the title truncates.
- Time counts from when the wait began and ticks each minute.
- Counts: a slot wait counts in Running and never in Needs you, the rail dot, the status bar or the bell. Trust counts in all of them.
- Slot granted: hourglass → spinner on the card and `play.circle` on the row; the word goes back to the duration. No toast, no sound.

## Glyphs

SF Symbols `hourglass` (slot wait, 14 pt, secondary ink; card 13 pt), `exclamationmark.shield` (trust, warning ink), `play.circle` (running). Light and dark share glyph and layout; only the ink comes from the theme (matelight, clight). The header subtitle is the only place where iPhone and iPad differ.

## Open (the designer's additions, to confirm or strike)

- The 9-character cap on a kind's Id.
- `landing.line` sent in segments, so clients drop them from the end.
- The "Held by ‹session›" line (with "waited ‹n› of ‹max›") on the expanded Mac card.
- The trust row on the boards ("Audit vendored deps") is illustrative; it is not in today's shots.
