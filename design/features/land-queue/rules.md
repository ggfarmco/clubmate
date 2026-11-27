# Landing queue in the Agents tree — rules (26 Nov 2026)

Ruling by Claude Design, round 1, in the chat of the canvas "Clubmate · landing queue" (https://claude.ai/artifact/YM34Xk96HUEebMd8cKh7h5; chat https://claude.ai/chat/a082dffe-c600-4a8b-814a-68ca850ca02e). Brief: `design/land-queue-brief/` (`brief.md`, `prompt.md`, `round-1.json`). Spec: `clubmate-notes/superpowers/specs/2026-11-26-merge-train-design.md` §5.5, §5.6. Backlog #1256, task 8.

The user has not picked against it. The user confirmed the Keys ruling on 26 Nov 2026.

Boards (PNG next to each `.dc.html`; all on one page in `boards.html`, config `design/land-queue-brief/round-1.json`):

| Board | Shows |
|---|---|
| `r1-Mac-1280-Queue` | queue under the integration session, 1280 Mate dark: head testing, ready with files expanded, waiting with Reviewed ahead, one with Review kept, one blocked; row menu open |
| `r1-Mac-1280-Main-branch` | "Landing into main": (a) root row, picked; (b) docked |
| `r1-Mac-640-Drawer` | the drawer, 640 Mate light: asking entry with its Land card, files popover |
| `r1-iPhone` | iPhone dark and light: section, swipe, long-press menu |
| `r1-iPad` | iPad split view dark |
| `r1-Rules` | states table, group row, marks, folding, needs you |

## Ruling

1. **Group row.** Text `Landing into ‹branch›`, new glyph (an arrow dropping onto two lines). Detail `5 in line` (count of all entries, head included); when any entry needs you it reads `1 needs you` / `2 need you`, warning tone.
2. **Under an integration session:** the group is the first child of the integration session, then one row per entry in queue order. A session in line shows once, as its entry row, and goes back to its usual place when it lands or is removed.
3. **Default branch:** direction (a), a root row `Landing into main` above Closed (not docked: it sits with its sessions, does not move Service / Land gates / Graphs, its needs-you rows are not hidden in a folded dock).
4. **One entry:** no group on any branch, main too. The queue shows from 2 entries. The lone session keeps its own row; the row shows the head's word; tooltip `Landing into ‹branch› · alone in line`.
5. **Entry states** (plugin state → word · glyph · tone · needs you). The word never truncates, at most 9 characters:

| Plugin says | Word | Glyph | Tone | Needs you | Example `why` |
|---|---|---|---|---|---|
| `head`, review runs | reviewing | `play.circle` | accent | no | review runs on the branch as it is now |
| `head`, tests run | testing | `play.circle` | accent | no | go test before it commits |
| `head`, committing | landing | `play.circle` | accent | no | every action is off until it is in |
| `head`, its review or tests failed | failed | `xmark.circle` | danger | as the plugin says | tests failed in notify_test.go |
| `asks` | asking | `exclamationmark.circle` | warning | yes | waits for your yes |
| `blocked`, the user's own edits or a conflict nobody can fix | blocked | `minus.circle` | warning | yes | waits for your edits in store.go |
| `blocked`, another session's edits | blocked | `minus.circle` | none | no | waits for edits in a.go |
| `ready` | ready | `circle.inset.filled` (circle with a dot) | none | no | waits for the head to land |
| `waiting` | waits | `circle` | none | no | conflicts in b.go, rebase before your turn |
| landed (out of line) | merged, on its session row | `checkmark.circle` | success | no | — |

   The word is `asking` (the Task tree word), not `asks`. A waiting entry shows `waits`, not its place: a bare number on the right of a tree row reads as a count (Closed 144). The place (`2nd of 5`) is in the menu header, the files popover and the session header.
6. **Marks** (never a tone, never in the word's slot): 12 px, faint text level, after the title; on iPhone and iPad they lead the `why` line, and the long-press menu title repeats the tooltip.
   - Reviewed ahead (dashed eye), entry 2 only, only with the setting `Review ahead in line` on (off by default): `Reviewed ahead, on ‹branch› as it will be after “‹head›” lands`.
   - Review kept (check with an arrow): `Review kept: ‹branch› moved in other files only`.
7. **Row menu** (••• on hover or focus, right-click; 232 pt wide, 24 pt items; iPhone: swipe left = Skip once + Remove, long press = full menu). Header `3rd of 5 in line · waits`, then: Move up, Move down, Skip once, —, Show changed files →, Open session ⏎, —, Remove from line (danger ink). On the head Move up is off; while it is landing every action is off. Remove keeps the session and its branch.
8. **Changed files** (→ expands the row): the `why` line; `Shares ‹n› files with ‹title›` groups first (overlap glyph, two circles), then `Also changes`, up to 10 files, then `+‹n› more`. At 640 the files open in a popover (276 pt wide) beside the drawer from the line `9 files · 3 shared`.
9. **Land request:** an `asking` entry shows the existing Land card inline under its row (at 640 and on the iPad), the variant the gate's rounds request uses (`docs/shortcuts.md`: `⌥1 ⌥2 ⌥3` Review… · Merge · Leave it unlanded…, and the same keys with `1 2 3, y n` while the card has the keys). Its buttons keep those words and keys, so no `docs/shortcuts.md` row is added. Texts: title `Land into ‹branch›?`, line `4 commits · review approved · tests pass`, buttons `Review… ⌥1`, `Merge ⌥2`, `Leave it unlanded… ⌥3` (for a queue entry it asks to confirm and takes the entry out of the line, the session and its branch stay; Task 5's `agents.land-answer` maps the three). Opening the row (⏎ or a tap) goes to the session's Land card.
10. **Narrow rows, folding order:** marks hide first; ••• shows only on focus (right-click stays); the title truncates; the group title truncates at the end of the branch. Never folds: glyph, word, tone, the group's detail. Row anatomy: indent · chevron · glyph · title · marks · ••• · word.
11. **Needs you:** only the plugin's `needsYou` (today: asking, and blocked by the user's own edits or a conflict nobody can fix). Then, on every device: warning glyph and word on the entry row; the group detail `1 needs you`; the count in the Needs you chip, rail dot, status bar and bell; iPhone and iPad list the entry under Needs you with the subtitle `Land into ‹branch›?` or its `why`.
12. **In the head's transcript** one muted line: `Agent commits into ‹branch› are held while the line moves · 4 behind this one`; the header caption reads `Claude · landing into ‹branch› · 1st of 5`.
13. **iPhone:** a section headed `Landing into ‹branch›` with the count on the right, the integration session row and its entries nested; the `why` is the subtitle. **iPad:** the session column follows the Mac tree with touch-height rows; the asking entry opens with the Land card at the bottom.

## Keys

The designer drew `⌥↑` / `⌥↓` for Move up / Move down (menu, footer hint, focused row). They clash with the task repo crumb keys `⌥↑ ⌥↓` in `docs/shortcuts.md`. The user ruled (26 Nov 2026): **no keys, menu and swipe only**, so no `docs/shortcuts.md` change. Draw the menu without the two key caps and drop the footer hint `⌥↑ ⌥↓ move in line · → files · ⏎ open`. `→` (files) and `⏎` (open) in the menu stay; they are row-focus keys, not new bindings.

## Deviations from the boards

- Drop `⌥↑ ⌥↓` from the menu and the status-bar hint (Keys).
- The Land card on `r1-Mac-640-Drawer` and `r1-iPad` is a stand-in with the buttons `Land ⌥1` / `Not yet ⌥2`. Build the existing Land card with `Review… ⌥1`, `Merge ⌥2`, `Leave it unlanded… ⌥3` (item 9); the boards' two buttons are not built.
- The boards show the Reviewed-ahead mark without the setting; build it only when `agents.landQueue.speculate` is on.
- Built (Task 9): a mark (Review kept) is an outlined badge after the title on the Mac, not 12 px faint text (item 6), because a tree row has no prop for faint text after the label yet. A tree-item prop for it is backlog #1325; until then the badge has no tone, the mark's text is in the row tooltip, and the badge (it has no tone) hides when the row is too narrow for it and the whole word (item 10); toned attention badges never hide.

## Left open

- **`failed` head state.** The designer added `failed · xmark.circle · danger` for a head whose review or tests failed. The plugin's `agents.land-queue` has no such state (`waiting|ready|head|asks|blocked`). Either the plugin adds it (`state: failed` or `head` + a `failed` flag) or the head stays `blocked` with its `why`. Needs a decision in Task 9 or a plugin change.
- **Undo for Remove.** The designer said ⌘Z undoes Remove from line. `agents.land-queue-remove` has no undo; not decided, not built until the plugin has one.
- **Names for the setting.** The mark text uses `Review ahead in line` for `agents.landQueue.speculate`; the setting's UI label is not decided.
- **Real-device and light-theme checks** of the iPhone and iPad boards were not made by the designer (drawn dark and light on iPhone, dark on iPad).

## Where it lands (agent's note, not the designer's)

- Plugin: `agents.sessions` `landQueues` and `landQueue` (spec §5.5) → tree items for the group and its entries in `plugins/agents/view.go`; the word, glyph and tone per row come from the plugin as fields, per the table above; `needsYou` decides item 11.
- Mac: `client/Clubmate/SDUI/` tree rows, a row menu, a files popover; iPhone and iPad in `client/Companion`, in the same commit (CLAUDE.md).
- Docs with the build: `docs/agents.md` § Landing queue (this table), § Needs you, `docs/sdui-protocol.md` if a new node, `.claude/skills/clubmate-ui/references/design-language.md` Status line.
