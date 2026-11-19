# Question card fold — rules (decided 19 Nov 2026)

Direction **B · Peek**, picked by the user (the designer's recommendation) in Claude Design round 1, canvas "Clubmate · Question card fold" (https://claude.ai/artifact/UTjAgXjG9U3Dtjgta34ZfT, page r1). Boards: `R1-B-Mac.dc.html`, `R1-B-Companion.dc.html`, both in `boards.html`. Rejected A · Bar and C · In place, and the round page, are in `rounds/`. Brief, screenshots of today and the prompt: `design/question-card-fold-brief/`.

The problem (user, 19 Nov): on the phone and the iPad the question card can't be collapsed and covers the agent's preamble, which you often need to read before answering. Hiding must be possible, but nothing may be sent past a pending question.

## States

The card (AskUserQuestion or an MCP form, docked above the composer) has three heights.

| State | What shows | Where it is the default |
|---|---|---|
| Full | Today's card: on the Mac every question open; on the phone the paged card ("1 of 2", dots) with option descriptions, Skip / Next / Send | Mac in a tall window |
| Peek | Header ("Claude is asking", dots, "2 of 2", Fold ⌥Q), the current question in at most 2 lines, its options as 44 pt rows without descriptions, Skip / Send. Answered questions fold to one row with their pick (Mac). | Phone and iPad; the Mac when the full card would take more than about 45% of the transcript height (the 640 board) |
| Bar | One line in the card's place above the composer: warning glyph, "Claude is asking", then each question's header chip with its pick or "not answered" (Mac 1280); narrow (640, phone): "1 of 2 answered · Grouping next" / "Claude is asking · 2 of 2", "Grouping next · Lists ✓ Add an SDUI prop"; it ends with Show ⌥Q (Mac) or a chevron, plus Skip on the Mac | Only by folding |

- No count badge on the bar: the warning tone already says Claude needs you.
- iPad: as the phone, except that options keep their description on one line in the peek, and Full is capped at 60% of the column so the preamble never disappears.
- Light and dark differ only in ink: warning ink on the light surface, the card's warning tone in both.

## No bypass: the composer answers the question

While a question is pending, the composer is the current question's Other, never a plain prompt.

- The composer carries a target chip, **Other · 2 Grouping** (Mac) / **Other · for Grouping…** (phone), placeholder "Type your own answer…", and the hint "Typing here answers question 2 · ↑↓ in the card moves it".
- Typing picks that question's Other. On the phone, typing in the bar's composer picks Other and opens the peek.
- Folded on the Mac, the composer's button reads **Send answers ⌘↩**: read the preamble, type the answer, send without unfolding.
- What the bottom of the screen is:
  - Phone full: the card only; the composer is hidden and Other is a row in the card ("Other… (opens the keyboard)").
  - Phone peek: the peek, then the composer aimed at Other.
  - Phone bar: the bar, then the same composer.
  - Mac (any state): the card, peek or bar, then the Other composer.
- Plugin guard (the UI alone can be bypassed): while a question is pending, the send/queue path refuses a plain prompt and stores composer text as Other on the target question. Today's "Queue a message" under the card is the bypass this closes.

## Keys and gestures

| Where | Input | Action |
|---|---|---|
| Mac, iPad with a keyboard | ⌥Q | Fold / unfold (Full or Peek ↔ Bar). A menu shortcut, so it works from a focused composer instead of typing œ. |
| Mac | ↑ ↓ | Move between questions in the card (moves the composer's target) |
| Mac | 1–4 | Pick an option of the focused question (drawn on the card footer; check against today's ⌥1–⌥9 rules in `docs/shortcuts.md` when building) |
| Mac | o | Other of the focused question (into the composer) |
| Mac, iPad keyboard | ⌘↩ | Send the card / Send answers when folded |
| iPad keyboard | ⌥1–⌥4 | Pick, as on the Mac |
| everywhere | ⎋ | Stays Stop the turn; Skip has no key |
| Phone, iPad | tap the header or chevron | Toggle |
| Phone, iPad | drag the grabber | Between bar, peek and full |
| Phone, iPad | scroll the transcript up | Full drops to peek, never to the bar. The Mac never folds by itself. |

Rejected keys: ⌥F (word-forward in shells), ⇧⌘H (next to ⌘H Hide).

## Stickiness

- The fold is remembered per question set (the elicitation id) on the device, so it survives leaving the session and coming back.
- A new question set opens at its default (peek on the phone and iPad, full or peek by height on the Mac).

## Mac / phone / iPad differences

| | Mac | iPhone | iPad |
|---|---|---|---|
| Default | Full; peek in a short window | Peek | Peek |
| Full | All questions open | Paged card | Paged, capped at 60% of the column |
| Peek options | Answered questions fold to a row | 44 pt rows, no descriptions | Description kept on one line |
| Fold | ⌥Q, Show ⌥Q on the bar | Tap / drag the grabber | Tap / drag, ⌥Q with a keyboard |
| Composer | Always shown, aimed at Other | Hidden while full; aimed at Other in peek and bar | As the phone |
| Auto fold on scroll | Never | Full → peek | Full → peek |

## Handoff (designer's tags)

- (c) Plugin content: the bar's summary ("1 of 2 answered · Grouping next"), the composer target label ("Other · 2 Grouping") and hint ("Typing here answers question 2"), the ⌥Q hints in the card footer and status bar. Header chips and questions come from AskUserQuestion's `header` and `question`.
- (b) New SDUI props: on the card node `fold: open | peek | bar` with a default per device, and `summary`; on the composer node `target: {ask, question, field: "other"}`.
- (a) Renderer and tokens: no new colour tokens (the bar and target chip reuse the warning border, warning ink and warning surface mix). Mac: automatic peek above ~45% of the transcript height; ⌥Q as a menu shortcut. SwiftUI: three stops with a grabber, the peek layout, scroll-up drops full to peek, iPad full capped at 60%, ⌥Q / ⌥1–⌥4 / ⌘↩ as hardware key commands. Fold state on the device, keyed by question-set id.
- (d) New plugin logic: refuse a plain prompt while a question is pending and store composer text as Other on the target question; accept an Other answer arriving from the composer rather than from the card.

## Not done here

- Design language: the bar, the peek and the composer target chip are new patterns; adding them to the language boards and to `.claude/skills/clubmate-ui/references/design-language.md` / `companion.md` is a separate step (design/README.md step 6).
- The designer did not render the boards; the exported page was checked only in a headless Chrome render.
