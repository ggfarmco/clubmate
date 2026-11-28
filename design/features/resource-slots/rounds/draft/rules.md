# Resource slots UI — rules (DRAFT, 28 Nov 2026)

**DRAFT: not a ruling.** Claude Design was not reached: the Claude in Chrome extension was not connected (`list_connected_browsers` empty), and the session ran AFK. The agent drew round 1 and marked its own picks. Do not build from this until the designer round replaces it (backlog #1353; the real round is backlog #1360). No canvas was made; the brief for it is `brief/prompt.md`.

Boards (PNG next to each `.dc.html`; all on one page in `boards.html`, config `brief/round-1.json`). Open `boards.html` or the PNGs: `boards.html` is self-contained. The `.dc.html` boards link only `./rs.css` and `./support.js` (absent, as in the other feature folders), so opened by themselves they render without the base tokens of `design/language/source/cm.css` and `design/companion/source/c.css`:

| Board | Shows |
|---|---|
| `R1-Mac-Row` | Q1 · Agents row at 248 pt, Mate dark: today / A short word + glyph / B full word keeps priority |
| `R1-Settings-Form` | Q2 · Resource limits Edit form: A inline, fixed chrome / B modal sheet |
| `R1-Wait-Mac` | Q3 · Mac session header and Bash card: today / A |
| `R1-Wait-Companion` | Q3 · iPhone header and tool row: today / A dark / A light |

## Draft picks

1. **Mac Agents row · A, a short word.** The row's word never truncates and has at most 9 characters (the landing queue rule, `design/features/land-queue/rules.md` item 5), so RowFit never splits the row for it.
   - Waiting for a slot: word `‹kind› slot` (`test slot`, `sim slot`) when it has 9 characters or less, else `waits`; glyph `hourglass`, no tone, no needs you. The glyph, not the word, says that the call waits.
   - Waiting for trust: word `to trust` (the dock word, `design/features/dock-state/rules.md`), glyph `exclamationmark.shield` (drawn; not checked against the glyph the plugin sends today), warning tone as today.
   - Tooltip: the full `landing.line` (`waits for test slot · 2nd in line · 1 of 1 in use · 3 min`).
   - The plugin sends it: `landing.words` gets the short Mac word, `landing.line` keeps `waits for ‹kind› slot · ‹why›` for the phone, iPad, tooltip and header, and the session's status icon is `hourglass` while a call waits. The Mac renders the fields and derives nothing.
   - B (the full word keeps priority) is rejected: the titles cut to their first letters, so the waiting sessions lose their names.
2. **Settings Edit form · A, inline, fixed.** A modal sheet does not present from the Settings window today (docs/agents.md § Resource slots), and inline keeps the kind lines, which update while you edit, in view. The fixes:
   - The form is an inner card under the edited kind's row: 14 pt padding, its own title `Edit ‹name›` with `Saved values apply at once` on the right; the row's Edit button reads `Editing` while open.
   - Labels right-aligned in a 104 pt column, 12 pt to the value, 8 pt between rows.
   - `mono` values (Id, Patterns, commands) in the interface's mono font, as everywhere else.
   - A multi-line value (Patterns, 5 lines) grows its row; the next row starts under it. Holding and Waiting are form rows (one line each, cut with a tooltip; Release as a link button at the end).
   - Footer under a divider: Remove (danger, outlined) at the left, a spacer, then Reset to defaults · Cancel · Save ⌘↩ with 8 pt gaps; the footer honours the stack `gap`.
   - B (a modal sheet) is drawn for the designer; it needs a client change to present a root `sheet` from the Settings window.
3. **Where the wait shows · A, the card and the header too.** The wait happens before the tool starts, so a spinner and `Running` are false.
   - Bash card (Mac): `statusWord` `waits for ‹kind› slot`, hourglass in place of the spinner, no tone; under the command one muted line `‹n› of ‹max› in use by “‹title›” · starts when it ends`. On grant it goes back to the spinner and `Running`.
   - Session header (Mac): the status glyph is the hourglass; the meta line is `landing.line` (it already repeats the sidebar detail in full) in place of `15 min · Bash`.
   - iPhone and iPad: the header subtitle is `waits for ‹kind› slot · ‹age›` in place of `Working…`; the tool row word is the card's `statusWord`. Same fields, same words on all three clients.

## Open for the designer

- The words: `test slot` vs another short form (`slot: test`), and `to trust` on a session row (it is the dock's word today).
- The glyph pair: `hourglass` for a slot wait, `exclamationmark.shield` for trust.
- Q2: inline (A) vs modal sheet (B) once the sheet can present from Settings.
- Q3: the card's second line and the header age (`3 min` since the wait started).
