# Guided reading components — rules (26 Nov 2026)

Ruling by Claude Design: round 1 and one follow-up in the chat of the canvas "Clubmate · guided reading" (https://claude.ai/artifact/DwTbyRojLTBm1dTuj2j74w, page Round 1). The user has not picked against it yet; two points are left to the user (below). Build #1294 and #1295 from this folder.

Boards (PNG next to each `.dc.html`; all on one page in `boards.html`, built from `boards.json`; styles `cm.css`, `c.css`):

| Board | Shows |
|---|---|
| `Final-Mac-Ask` | 1–3 at 1280 Mate dark: asking, answered, states (range, 2 s hold, text answer, correct / close / sure-and-wrong), the directions not picked as side panes |
| `Final-Mac-Tour` | 4 + 5 at 1280 Mate dark: controls under the step, Continue, Go deeper aside, Tour tab, map rows, header strip |
| `Final-Mac-640-Light` | 1–5 at 640 Mate light: peek, bar, header strip with popover |
| `Final-iPhone` | 1–3: full, peek, sending, reveal, bar (dark and light) |
| `Final-iPad` | 1–3 |
| `Rules` | states, fields, keys, folds, platforms, open-point rulings |

`rounds/R1-*.dc.html`: round 1 as the designer first drew it, with all directions.

## Ruling

1. **Code excerpt · free pick in the card.** The excerpt is the file at the commit, never the work tree. ↑ ↓ line, ⇧↑ ⇧↓ or drag for a range, ↩ or click picks. A pick never sends. Lettered candidate lines stay a field value (`pick: "candidates"`); pick in nvim stays the way out through Open in nvim.
2. **Reveal · in place.** The asking card leaves the dock and becomes the record in the transcript: ✓ / ✕ marks with labels on its lines, the verdict with the user's confidence, one line of why (clamped to 2), `path:line`. No undo after the reveal; the guide can ask again.
3. **Confidence · the chip sends.** Chips are off until a line or a full range is picked; then a chip rates and sends. A 2 s hold with a bar under the card: ⌘Z or Undo goes back to the pick, another digit changes the rating and restarts the bar. A typed answer: chips only rate, ⌘↩ sends; ⌘↩ with no rating moves focus to the chips.
4. **Step controls · under the latest step only**, hidden while a question is open: I know this · test me, Show me, Go deeper (opens an aside under the step, not graded, own composer target, ⎋ goes back), Smaller steps. No permanent Next: the guide moves on by itself after a graded answer. When a step has no check the plugin adds Continue ⌘↩ as the only primary (⌘↩ continues only while the composer is empty).
5. **Concept map · Tour tab of the right panel, next to Context.** A guide session opens the panel on Tour. Pane when the main column (window minus rail and sessions list) is ≥ 960 px and the panel is open; else a header strip whose click opens the same tree as a popover. Tree in tour order; an edge to another branch is a ↗ chip. A click on a concept shows only the offers the plugin sent for it (ask again, go back to that step).

## What the plugin sends and decides

The plugin decides every state and sends it as a field; the UI only draws it. The Mac, iPhone and iPad show the same card.

| Component | States | Fields | Plugin decides |
|---|---|---|---|
| 1 Excerpt | asking (docked, warning tone) · line picked · range picked · waiting for nvim · sending (hold) · answered | `file` `commit` `range` · `pick: line \| range \| candidates \| nvim` · `candidates` · `cursor` · `picked` · `folds` · `marks` (after grading only) · `state` · `fold` | state, range and folds shown, pick mode, every mark and its label |
| 2 Reveal | correct · close · wrong · sure-and-wrong · checking (typed answer waiting for the guide) | `verdict: correct \| close \| wrong` · `verdict_label` · `flag: sure_wrong` · `note` · `yours {lines, confidence}` · `answer {lines}` · `why` · `ref path:line` · `grading: instant \| pending` | correct / close / wrong (close = e.g. an overlapping range) and sure-and-wrong. The UI draws ✓, half-disc or ✕; sure-and-wrong adds a red confidence tag and the plugin's one-line note |
| 3 Confidence | off · ready · sending (hold) · set (text: rated, not sent) · stored (tag in the record) | `levels ["guess","think so","sure"]` · `value` · `sends` (true for line, range, choice; false for text) · `hold_ms 2000` · `required true` | whether a rating is required and whether a chip sends; stores the value with the answer |
| 4 Controls | under the latest step only · hidden while a question is open · Go deeper pressed (aside open) · action disabled (42%) · Continue present or not | `step` · `actions: test_me, show_me, go_deeper, smaller_steps, continue`, each `{label, enabled}` · `pressed` · `aside {step, title, open}` | which actions show and are enabled, whether Continue exists, whether an aside is open; each button is a fixed request to the guide node |
| 5 Map | per concept: known · shaky · missed · current · not yet; shown as pane · header strip · popover | `concepts [{id, label, state, step}]` · `edges` (from the run's tour.json) · `current` · offers per concept: `ask_again`, `go_back` · `counts` | every state and the current concept; rule today: correct + sure → known; correct + guess or think so → shaky; close → shaky; wrong → missed; sure-and-wrong → missed and asked again later |

Map states are shapes, not only colour: filled ✓ (known) · half disc (shaky) · ring ✕ (missed) · ring with a dot and the row selected (current) · dashed ring (not yet).

## Keys (proposals; `docs/shortcuts.md` decides)

| Where | Keys |
|---|---|
| Excerpt | ↑ ↓ line · ⇧↑ ⇧↓ or drag range · ↩ or click pick · ⌘O Open in nvim · ⌥Q fold · typing goes to Other |
| Confidence | 1–3 rate and send (pending choice a) · ⌘Z undo during the hold · ⌘↩ sends a text answer |
| Controls | ⌥T test me · ⌥S show me · ⌥D go deeper · ⌥M smaller steps · ⌘↩ continue (empty composer) · ⎋ leaves an aside |
| Map | ⌘⇧M shows or hides the panel (or the panel's own key) · ↑ ↓ ↩ in the tree |

The designer kept off ⌘1–⌘8 (sections), ⌥1–⌥3 (permission card) and ⌘K. Found on the check against `docs/shortcuts.md`: ⌘O is Open Folder… app-wide, and the transcript opens a block's file in nvim with `gf`; the build settles Open in nvim's key against `docs/shortcuts.md` (`gf` as on a Read or Edit block is the near match). Only ⌘O was checked: ⌥T, ⌥S, ⌥D, ⌥M and ⌘⇧M are not checked against `docs/shortcuts.md` yet, and the build of #1294/#1295 settles every key in this table there.

## Fold behaviour (question card rules, decided 19 Nov)

| State | Mac | iPhone / iPad |
|---|---|---|
| Full | the whole range, in a tall window | iPhone: drag the peek up. iPad: capped at 60% of the column |
| Peek | opens by itself when full would take > ~45% of the pane; six lines around the cursor and "All N lines ⌥Q"; scrolling the transcript drops full to peek, never further | iPhone default: three lines around the pick plus the capsules |
| Bar | ⌥Q; keeps the pick ("line 32 · 1–3 rate, send") | drag the peek down; a tap opens the peek to rate |

⌥Q toggles full or peek ⇄ bar on the Mac and on the iPad with a keyboard. The composer is the question's Other in every fold state: typed words are an answer the guide grades. An answered card leaves the dock and stays in the transcript as a record.

## Mac · iPhone · iPad

| | Mac | iPhone | iPad |
|---|---|---|---|
| Pick | keys or click; ⇧ or drag for a range | tap a line, ▲ ▼ to nudge; Line / Range segment, then tap the last line | touch as iPhone; with a keyboard as Mac |
| Excerpt | 12 px mono, 19 px rows, long lines end in "…" (full on hover) | 34 pt rows, long lines wrap, common indent removed | 34 pt rows, eight lines in full |
| Confidence | chips with 1–3 | three capsules; a tap sends | capsules with 1–3 shown |
| Open the file | Open in nvim | Open on Mac (opens nvim there) | Open on Mac |
| Reveal | full `path:line` | short path | full path |
| 4 Controls | buttons with ⌥ keys under the step | a row of capsules under the latest step, Continue first; Go deeper opens the aside inline | as Mac; keys with a keyboard |
| 5 Map | Tour tab ≥ 960 px, else header strip | tap "step 5 of 8" under the title: a sheet with the tree; a concept tap shows its offers | header strip and popover; the pane when the transcript column is ≥ 960 pt |

On iPhone a typed answer shows the three ratings as a segmented control above Send.

## Decided by the user (26 Nov 2026)

- **(a) Confidence keys:** in guide sessions the choice card's options take a–d, and digits 1–3 always rate confidence (the designer's recommendation). Outside guide sessions the choice card keeps 1–4.
- **(b) Send hold:** 2 s, with the bar under the card; ⌘Z or Undo goes back to the pick.
