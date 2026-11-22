# Chat panel — decided 22 Nov 2026

Picker **A** "One list", header **H1**, Wiki **W1**, key **K1**, iPad **P1 + P2** (Claude Design round 1, canvas "Clubmate · Chat panel", https://claude.ai/artifact/HCLWvvj99fj6sm88VeJnyG, page r1). The user picked A and K1; H1, W1 and P1 + P2 are the designer's recommendation, which the user did not change. Spec: `clubmate-notes/superpowers/specs/2026-11-22-chat-panel-design.md`. Brief and the A/B/C page: `design/chat-panel-brief/` (`prompt.md`, `round-1.json`, `round-1.html`).

Boards (`.dc.html` here, the four R1 and five R2 boards in `boards.html`; the iPad board needs `r1.css` + `r2.css`, copies of Device UX's): `R1-Code-1280-Review` (Code 1280, Mate dark, review current, picker A), `R1-Wiki-1280-Chat` (Wiki 1280, Mate dark, after the pick: H1, page chip, W1), `R1-640-iPad` (Code and Wiki 640 Mate light, iPad P1/P2), `R1-Rules-Handoff` (rules, 20 handoff items). `rounds/R1-Picker-Directions` (A/B/C side by side, row anatomy, the empty state) and `rounds/R1-Header-Wiki-Keys` (H1–H3, W1–W3, K1–K3) hold the picks next to the rejected options; read A, H1, W1 and K1 from them.

**Status:** built in main (43e13defb). Round 2 (below) rules what the build left open; where it changes a rule here, the rule says "R2".

## The rules

1. **One tree.** The agents plugin renders the panel as one SDUI tree. Code and Wiki differ only in the context the plugin is told (worktree, file and selection, current review; workspace and open page).
2. **Opening.** The panel toggle opens the panel on the picker every time, in Code and in Wiki. Focus lands on the suggestion.
3. **Picker A · one list.** Section label "Suggested" with one row: the suggestion. Then the section label "Open in ‹workspace›" with a count: every other open session of the workspace, newest activity first. Closed sessions are not listed (they stay in Agents).
4. **Row.** Status icon · title · caption. The caption is the worktree (only when it is not the current one) · age. On the suggested row the caption is the reason · age: "opened this review", "gets this review's comments", "last used here", "newest". Needs you: amber status icon and one `needs you` badge; no other badges. The suggestion keeps its label when focus moves off it.
5. **Accent** marks only the focused row (and its ↩ key cap).
6. **New session** is a full-width secondary button at the foot of the list, with a boxed key cap `N` at the right and a muted caption that says what it will see (copy: R2 Q4). Label: "New session for this review" while a review tab is current in Code (caption "Starts on ‹branch› with this review", R2 Q4), else "New session". With a review current it starts a session bound to the review (base, head, worktree) and makes it the review's opener.
7. **Long lists.** More than 9 open sessions: a filter field shows above the list ("2 of 23"); `/` focuses it at any count. It matches title and worktree; ↩ in the field takes the first match; ↩ on an empty result starts a new session with the filter text as its title.
8. **Empty** (no open sessions): the `empty` node, one sentence ("No open sessions in ‹workspace›" + "A new session " + the foot caption with a lower-case s + ".", e.g. "A new session starts on ‹branch› with this review." / "…and sees the page ‹title›.", R2 Q4), New session as the only primary (↩ takes it, N too), and the hint "Closed sessions stay in Agents" after the button as muted text (R2 Q3).
9. **Header H1 (after the pick).** Back button `‹ ⌘[` without a label ("All sessions" is its tooltip and accessibility label, R2 Q5), a 1 × 16 vertical divider, status icon, title (bold, one line), worktree caption only when it is not this one, then the hide button last (tooltip prints the toggle key). No session select, no Context button, also when there are no chips (R2 Q1). The picker has the same 32-pt bar: "Chat", muted "pick a session", hide last (R2 F3).
10. **Context chips.** What the session sees shows as chips at the top of the composer: the file and lines, the review (`Review ‹head branch›`, R2 E2), or the wiki page (`‹title› page`). A click on a chip's label opens the context detail at that item; × (always shown) leaves it out of the next message; one `⌘I` key cap ends the chip row; with no chips the row shows one dashed, muted "Context ⌘I" chip (R2 Q1, F6). The chips replace today's Context ⌘I header button.
11. **Send** stays the panel's one primary button. Names over ids everywhere.
12. **Wiki W1.** The panel is the same right column as in Code. While it is open, the margin column (On this page, Changes, Linked from) folds into the one-row fold above the body (Wiki rule 2's narrow fold row); closing the panel brings the margin back.
13. **Width.** One width per workspace, shared by Code and Wiki: default 392, min 320, at most half the window. Below 1100 pt the panel stacks under the editor or the page (Code: as today; Wiki: the same), rows stay 36 high.
14. **iPad P1** (landscape, regular width): a 400 pt side panel on the right; opening it hides the list column, closing it brings the list back. Opened from a Chat button in the detail toolbar, or ⌘B with a hardware keyboard.
15. **iPad P2** (portrait, Split View, compact): a sheet with medium and large detents; the medium detent keeps the top of the page readable.
16. **iPad rows** in the panel are 56 pt; key caps show only with a hardware keyboard. "New session for this review" only where the iPad shows that review.

## Keys

| Key | Where | Action |
|---|---|---|
| ⌘B | Code and Wiki (Mac); iPad with a keyboard | Toggle the chat panel (K1) |
| ⌃⌘S | every section | Toggle the sidebar (the Wiki tree); no longer a chat alias in Code (K1) |
| ↩ | picker focused | Take the focused row |
| ↑ ↓, j k | picker focused | Move |
| N | picker focused | New session |
| / | picker focused | Focus the filter; ⌫ clears it |
| ⌘[ | chat in the panel | Back to the picker |
| ⌘I | chat in the panel | Context detail (also a click on a chip) |

The picker keys act only while the panel has focus. ⌘N stays New in each section; a bare Esc stays with the app. In Wiki, ⌘B changes meaning (it was Toggle Sidebar): show a one-time hint in the panel (R2 Q2). Checked against `docs/shortcuts.md` on 22 Nov: on the Mac, ⌘[ is Back only in the Agents main pane and asides (the same meaning), Wiki history is ⌘Y; no list or tree uses a bare N or /. On the iPad ⌘[ is Back in the detail column: the panel's ⌘[ must act only while the panel has focus.

## Handoff

Tags: (a) client only, (b) SDUI node or prop, (c) what the plugin sends. Numbers as on `R1-Rules-Handoff`.

- **(a) Mac:** 1 open on the picker with the tree's initial focus; 2 panel-scoped keys (↩ ↑↓ j k N / ⌘[), never Esc or ⌘N; 3 ⌘B = chat in Code and Wiki, ⌃⌘S = sidebar, drop Code's ⌃⌘S chat alias, one-time hint in Wiki; 4 Wiki gets the right panel slot and folds the margin while it is open; 5 one width per workspace for Code and Wiki, stacked below 1100 pt.
- **(a) Mac + Companion:** 6 report the view context when the panel opens and when it changes (section, worktree, file + selection, current review, open page).
- **(a) Companion:** 7 iPad P1/P2 and a Chat button in the detail toolbar.
- **(b) SDUI:** 8 list row `caption` (second line) and a "Suggested" section label from the plugin; 9 list `focus` (initial row id) and `filter` `{placeholder, minRows: 10, key: "/"}` filtering title + caption on the client, ↩ takes the first match; 10 button `shortcut` takes a bare letter scoped to the panel; secondary normally, primary inside `empty`; 11 header `back` `{label: "All sessions", shortcut: "⌘["}` as its first control (R2 Q5/F4: the label is the tooltip and accessibility label only, not drawn) and an optional caption after the title; 12 composer `contextChips` `[{kind: file|review|page, label, detail, removable}]`, a chip runs the context action, replaces the header's Context button (R2 Q1/F6: × always shown, one `⌘I` cap ends the row, a dashed muted "Context ⌘I" chip when there are no chips); 13 Companion row height 56 pt for list rows inside the panel.
- **(c) agents plugin:** 14 one picker tree per opening: suggestion first with its reason, then the workspace's other open sessions by last activity; 15 per row title, status, worktree only when not the current one, age, `needsYou`, never an id; 16 suggestion order: (1) Code with a review current: its opener, else its Send recipient; (2) last picked in this panel for this context (worktree in Code, workspace in Wiki); (3) newest open; 17 store the pick per context (survives a plugin restart); 18 New session label, caption and the review binding; 19 the empty state; 20 after the pick: header with back, title, worktree caption, context chips that follow the view.

Each new prop in (b) is a language change: it lands with `pkg/sdui`, `docs/sdui-protocol.md`, both renderers, the gallery fixture and `.claude/skills/clubmate-ui/references/design-language.md` (or `companion.md`), as a separate named step (`design/README.md` § Working with a designer, step 6).

## Round 2 — 22 Nov 2026

Claude Design round 2 on the built panel (canvas page r2; brief `design/chat-panel-brief/prompt-r2.md`, page `design/chat-panel-brief/round-2.html`). The user picked Q1 B, Q2 B, Q5 "no label" and F6 A, all the designer's recommendation; the rest the designer ruled without options. Boards here: `R2-1-Picker-Code`, `R2-2-Empty-Copy`, `R2-3-Header-Chips`, `R2-4-Wiki-Hint`, `R2-5-iPad-Rulings` (all in `boards.html`); the seed board with the shots is `rounds/R2-Today`. The boards crop the 22 Nov shots by canvas asset id: `bd1e952c…` code-1280-final, `b01c7684…` code-1280-picked, `fc1c5da6…` wiki-1280-empty, `9169d7d6…` wiki-1280-sidebar-right, `2ef94a63…` ipad-portrait-sheet (JPEGs of `clubmate-notes/superpowers/shots/2026-11-22-chat-panel/*.png`).

Tags as in Handoff: (a) client, (b) SDUI node or prop, (c) plugin.

**Questions (#1186)**

- **Q1 Context with no chips — B.** No Context button in the header at any time. The `⌘I` control ends the chip row; with no chips it is one dashed, muted chip "Context ⌘I" (not "No context": the session still works in its worktree). (b)(c)
- **Q2 Wiki ⌘B hint — B.** One muted line "⌘B opens chat here now. The page tree moved to ⌃⌘S." with a ghost "Got it", under the picker header, the first times ⌘B opens the panel in Wiki. It goes on Got it, the first ⌃⌘S in Wiki, or after 3 showings, then never again on this Mac. The client draws it above the SDUI tree; ↩, j/k and N still act on the list. No bottom banner, no timer. (a)
- **Q3 Empty order.** Icon + title · sentence · primary New session `N` (label-sized, boxed cap) · the hint as muted caption text, not a chip, with the Agents key: "Closed sessions stay in Agents ‹key›". The key is the one the rail gives Agents (⌘3 on the board; the rail order is the user's, so the client prints the real key). The empty picker keeps the "Chat · pick a session" header. (b)(c)
- **Q4 Copy.** Caption = "Starts on ‹branch›" + one ending; the empty sentence = "A new session " + the caption with a lower-case s + ".". The plugin builds one string; the iPad uses the same strings.

  | Context | Button | Foot caption |
  |---|---|---|
  | Code, review current | New session for this review | Starts on ‹branch› with this review |
  | Code, a file open | New session | Starts on ‹branch› and sees ‹file› |
  | Code, nothing open | New session | Starts on ‹branch› |
  | Wiki, a page open | New session | Starts on ‹branch› and sees the page ‹title› |
  | Wiki, no page | New session | Starts on ‹branch› |

  ‹file› is the base name (path in the tooltip, no line numbers); ‹branch› is the worktree's branch. No commit ids (rule 6's "‹base› → ‹head›" caption is replaced). (c)
- **Q5 Back — no label.** `‹ ⌘[` ghost button, 20 high; the tree may still send "All sessions": the client uses it for the tooltip and the accessibility label and does not draw it. (b)

**Drift from the R1 boards (#1185)**

- **F1 Foot inset.** Picker foot padding 10 top, 12 sides, 12 bottom on the Mac; on the iPad sheet 12 / 20 / 20 plus the home-indicator inset when the sheet reaches the screen's bottom. (b)
- **F2 New session.** In the picker foot: full width, secondary, 24 high, label left, `N` as a boxed key cap at the right. Label-sized and primary only inside the empty state. iPad: full-width 44-pt capsule, no cap without a hardware keyboard. (b)
- **F3 Picker header + hide.** 32 pt, the same bar as the chat header so the hide button never moves: 12 left, 6 right, gap 8; "Chat" 13/600, "pick a session" muted, flex, hide button (20, ghost, tooltip "Hide chat ⌘B"). No count (the section label carries it). iPad: "Chat · pick a session" and the close button; the grabber stays. (a)(b)(c)
- **F4 Back look.** Ghost: chevron 14 + `⌘[` cap, then a 1 × 16 vertical rule (the build draws a long horizontal line), then status, title, worktree (only when not this one), hide. (b)
- **F5 Title.** 13/600, one line, ellipsis at the end, the full title in the tooltip. Never wraps; the header stays 32. (b)
- **F6 Chip anatomy — A.** × always shown (12, muted) on every chip; a click on the label opens Context ⌘I at that item; one `⌘I` cap at the end of the chip row. Not B (⌘I on the first chip, × on hover): the iPad has no hover. (b)

**Other drift the designer found**

- **E2 Names over ids.** The review chip reads "Review ‹head branch›", never a commit; base, head and worktree stay in the Context ⌘I detail. (c)
- **E3 Ages.** Short form as on every other list: 1m, 10m, 2h, 3d, not "1 min ago". (c)
- **E4 Focus.** The panel opens focused: the suggestion is the selected row (accent bar + fill, ↩ cap). With focus elsewhere it keeps a neutral fill and loses the cap. (a)
- **E5 File chip.** File icon + base name (`send.go`), path in the tooltip; no "@", no paperclip. (b)(c)
- **E7 Overflow.** Chips wrap to a second line inside the composer; one long chip truncates its label (middle for paths). Never past the card's edge. (b)
- **iPad rows.** Picker rows use the Companion status glyphs (one shape per status), not filled dots; the suggestion keeps its accent-tinted fill. (a)
- **E1 is not drift.** The designer read `wiki-1280-sidebar-right.png` as the tree moving right of the page; that shot was taken with Settings › Sidebar side = right. W1 still replaces only the margin column.

**Not chosen (round 2):** Q1 A (icon-only Context button in the header when there are no chips: the header changes shape and context has two homes); Q2 A (bottom banner for 6 s: shows where nobody looks and goes before it is read), a tooltip on the tree header, a badge on View › Sidebar; Q5 with the label (costs ~90 pt, the title keeps ~15 characters at 392 pt); F6 B.

## Not chosen

- **Picker B** (suggestion card with Continue ↩, quiet list, "Show 14 more"): needs a card node and two focus zones; a wrong guess costs a step.
- **Picker C** (grouped: this review, this worktree, other worktrees): the plugin must group, and Wiki has no nearness. The designer: take C's grouping later if 20+ open sessions turn out to be common.
- **H2** (the title opens the picker, context strip under the header), **H3** (breadcrumb "Sessions / title").
- **W2** (chat right of the margin: the page drops to 380 at 1280), **W3** (Outline | Chat tabs in the margin column: a Wiki-only container).
- **K2** (⌥⌘B for chat everywhere), **K3** (different keys per section).
- **P3** (iPad out of scope).

## Sample content

Session titles, ages, worktrees, the review comment and the chat text on the boards are the designer's; the section rail numbers on the boards (Wiki ⌘5) are not the app's.
