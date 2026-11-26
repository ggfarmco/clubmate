# Guided reading components — rules (DRAFT, 26 Nov 2026)

**DRAFT: not a ruling.** Claude Design was not reached (the Chrome extension was not connected). The agent drew round 1 and marked its own picks. Do not build from this until the designer round and the user's pick replace it. Canvas "Clubmate · guided reading": https://claude.ai/artifact/DwTbyRojLTBm1dTuj2j74w (page Round 1). Brief, shots and the round page: `design/guided-reading-brief/`.

Boards (PNG next to each `.dc.html`; styles: `cm.css`, `c.css`, `gr.css`):

| Board | Shows |
|---|---|
| `R1-Pick-Mac` | 1 · line pick, A gutter / B letters / C nvim |
| `R1-Reveal-Mac` | 2 + 3 · reveal A in place / B verdict card / C the chip sends |
| `R1-Steps-Map-Mac` | 4 + 5 · A button row + map strip / B Show me + More + map in the Context panel |
| `R1-Narrow-Light` | 1–3 · A + A at 640 Mate light |
| `R1-Phone` | 1–3 · iPhone pick, reveal, range (light) |
| `R1-iPad` | 1–3 · iPad landscape |

## Draft picks

1. **Line pick · A, gutter pick in the card.** The excerpt (file at a fixed commit) is in the docked question card. Click a line, or j / k and ↩; ⇧-click or ⇧J / ⇧K for a range. Free choice: the answer is not given away. B (lettered candidate lines) is kept for "Smaller steps". "Open in nvim ⌘O" on every excerpt.
2. **Reveal · A, in place.** After Send the card turns into the reveal: your line amber with a note, the key green with a note, one line of why, `path:line` link. The plugin grades a line pick, so the reveal shows before the guide's next message. Lines far from both marks fold ("40–42 and 51–53 folded · show all").
3. **Confidence · chips + Send.** Guess / Think so / Sure in the card footer, keys 1–3, Send ⌘↩. C ("the chip sends") is the open question for the designer.
4. **Step controls · A, a row of four buttons** under the latest step only: I know this, test me · Show me · Go deeper (aside) · Smaller steps, and Next step ↩ as the one primary.
5. **Concept map · A, one-line strip under the session header** (≤8 chips, edges, state dot; current chip ringed); a chip opens Go back here / Ask me again. B (list in the Context panel) stays as the overflow view for long tours.

## States and tones

| State | Where | Tone |
|---|---|---|
| Asking | excerpt card while pending | warning (as the question card) |
| Picked / cursor | line | selection + accent bar / focus ring |
| Your pick, not the key | reveal | warning ink + amber bar ("not a failure") |
| Key | reveal | success ink + green bar |
| Sure and wrong | reveal header | danger; the concept is marked to ask again |
| Close (range overlap) | reveal header | warning, "Close" |
| known / shaky / missed / current | map | success / warning / danger dot / accent ring |

Concept state is decided in the plugin from the stored answers (right + sure = known; right + guess, or close = shaky; wrong = missed) and sent as a field; the UI only draws it.

## Phone and iPad (1–3)

- iPhone: code rows 36 pt, 13 pt mono; tap a line, ▲ ▼ to correct; Range button for a range; segmented Guess / Think so / Sure; full-width Send. The card is the docked question card (peek by default, fold rules hold). No nvim: the link opens the file on the Mac.
- iPad: the whole excerpt (full card capped at 60% of the column); keyboard ↑ ↓ line, ⇧↑ ⇧↓ range, ⌥1–⌥3 confidence, ⌘↩ send.

## Open for the designer and the user

- Keys in a focused card (j / k, ⌘O, 1–3, K / S / D / −) are proposals; check against `docs/shortcuts.md` and today's transcript keys.
- C "the chip sends" vs chips + Send.
- Map strip vs Context panel list as the default.
- Light Mac boards for 4–5; phone and iPad for 4–5 are out of this round.
