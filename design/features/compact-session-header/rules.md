# Compact session header — rules (26 Nov 2026)

Ruling by Claude Design, round 1, in the chat of the canvas "Clubmate · compact session header" (https://claude.ai/artifact/6SCBqSsbVqR7XQWvk5eLcg, page r1; chat https://claude.ai/chat/11469688-8064-4900-885a-b41b015abf79). The user has not picked against it yet. Brief: `design/compact-session-header-brief/`.

The user's pain: the session header has 3 rows and the window top bar is tall; on a small screen they take space from the transcript.

Boards (PNG next to each `.dc.html`; all on one page in `boards.html`, config `design/compact-session-header-brief/round-1.json`):

| Board | Shows |
|---|---|
| `r1-Pick-1280` | the whole window with the pick: T1 bar + A header, Mate dark |
| `r1-A-1280` | A at 1280 Mate dark: idle, heaviest (long Russian title, 3 badges, 5 actions, running, landing line), failed with its problem line, the ring's popover |
| `r1-A-640` | A at 640 Mate light: idle, heaviest, failed, ••• open |
| `r1-Bar-1280` | top bar: today / T1 lower (pick) / T2 merged |
| `r1-Bar-640` | T1 and T2 at 640 Mate light |
| `rounds/r1-B-1280` | B, inline meter + labelled overflow (not picked) |

## Ruling

1. **One row, 28 pt, at both widths** (20 pt buttons, 13 pt title). Direction A "meter + icons".
2. **Order, left to right:** status icon · title · caption "Claude · ‹branch› · ‹mode›" (muted) · badges · flexible gap · context ring · | · action icons · ••• · | · Stop. Stop is always last at the right and never moves. Badge order: red, amber, accent, neutral.
3. **Meta goes behind a context ring.** A 16 pt ring with the context % ("15%"); it turns amber when context is full (the "context full" badge stays). A click opens a popover with today's row 2 as a table: caption, running time, context bar, 5 h and 7 d windows, AFK, notify, gate review round, share, repos the session touched, and a "Context ⌘I" button. Its accessible label reads the whole meta.
4. **Action buttons are icon-only ghosts** with a tooltip that gives the name and the key. Context (⌘I) and Review changes (icon + ⌄, opens its bases) stay in the row at 1280; the rest (from ‹parent›, Earlier session, Reviewed, Open review, ‹n› asides ⌘], repos pill) show as icons while they fit, then go into a ••• menu that keeps their keys.
5. **What gives way first when the row is short:** (1) the caption (the branch is in the top bar, the mode in the composer; both are in the popover); (2) the title, down to 160 pt (120 at 640); (3) the badges, down to the most urgent one plus "+n" (hover lists the rest); (4) the actions, into •••; (5) the ring's %. Stop never folds.
6. **640:** the caption is gone; ring, Context and Review stay while the title keeps ≥120 pt; in the heaviest case Context stays and Review and the rest go into •••. Stop keeps its word, its key moves to the tooltip.
7. **Failed:** a red "failed" badge (the failure's own badge) sits where Stop would be, after the title; the problem line sits right under the header as one 28 pt red-tinted row: icon, exit code, the text (truncates), then Retry and Sign in… (never truncate).
8. **Landing line** stays as its own 24 pt muted row under the header.
9. **Top bar T1:** 28 pt (a plain macOS title bar) instead of ~38; pills 18 pt (text 11, icons 12, padding 7, radius 5) instead of 24. The traffic lights stay where macOS puts them. With A the chrome above the transcript goes from ~118 pt to 56 pt.
10. **Not merged (T2 rejected for now):** putting the session header into the window bar saves only ~24 pt more, ties every section's header to the bar, leaves only the gaps to drag the window, and at 640 the title gets ~170 pt.
11. **Keys unchanged:** ⌘. Stop, ⌘I Context, ⌘] next aside (`docs/shortcuts.md`).

## Deviations from the boards

- **Sidebar toggle at 640.** `r1-A-640` and `r1-Bar-640` draw a "Show sessions ⌘B" icon at the start of the header. ⌘B is Toggle Chat (`docs/shortcuts.md`); the sidebar is ⌃⌘S, and today there is no sidebar button in the header. Leave it out, or add it with ⌃⌘S as a separate decision.
- **Rail and section keys** on the boards' rail labels (Code ⌘1, Agents ⌘3, …) are drawing labels, not a rebind.
- **Review at 640.** The chat said "Context and Review always stay"; `r1-A-640` heaviest puts Review into •••. The board wins: at 640 only Context is sure to stay.

- **Top bar 32 pt, not 28.** Built as the app's own bar under a hidden title bar; its height is twice the distance from the window top to the traffic lights' centre, which is 32 pt on macOS 27 (a plain title bar there). A 28 pt bar would put the pills 2 pt above the lights' centre line. The user kept 32 pt (26 Nov 2026).
- **The aside keeps its two-row header.** The ruling covers a session's own header only; the aside's header (Send back, Full page, Keep, ×) stays as it was, so its actions do not fold into ••• in the narrow beside pane. The plugin marks the compact header with `compact: true`.

## Where it lands (agent's note, not the designer's)

- Plugin (`plugins/agents/view.go` `header`): the meta moves from a text into a structured field for the popover (rows with label and value, context fill, full flag), so the client does not parse "15% context"; the buttons get a priority (always / fold) so the renderer knows what goes into •••. `docs/sdui-protocol.md` (header node), gallery fixture.
- Mac renderer (`client/Clubmate/SDUI/ContentViews.swift` `HeaderNodeView`): one row, the ring + popover, icon-only buttons with tooltips, the fold order above, `ViewThatFits` per step.
- Top bar (`client/Clubmate/Windows/Workspace/WorkspaceToolbar.swift`, `TitlePill.swift`): 28 pt bar, 18 pt pills.
- Companion: not in scope (it has its own session bar); the header rule changes nothing the phone shows.

## Round 2 · top bar height and full screen (27 Nov 2026)

Ruling by Claude Design, round 2, same canvas and chat (page r2). Brief: `design/compact-session-header-brief/prompt.md` § Round 2. It replaces item 9 and the "Top bar 32 pt, not 28" deviation above. The user picked N, the native toolbar (27 Nov 2026).

The user's words (Russian, translated): "Honestly this is ugly, let the designer advise. 1) We must return the default width of this header, because on new Macs it will look crooked. 2) In other apps it simply slides to the side when the buttons come out; why don't we do that?"

Boards (PNG next to each `.dc.html`; all on one page in `boards-r2.html`, config `design/compact-session-header-brief/round-2.json`):

| Board | Shows |
|---|---|
| `r2-Height-1280` | Q1 in a window: N native toolbar (pick, 40 pt), M measured title bar (32 here, 28 on another Mac), ✗ fixed 28 (pills 2 pt above the lights' centre), a 3× zoom of each |
| `r2-FS-Notch-1280` | Q2 on a Mac with a notch: menu bar hidden, half way, shown |
| `r2-FS-NoNotch-1280` | Q2 on a Mac without a notch: the same three frames, the bar rides down |
| `r2-Motion` | the reveal timeline and the N / M table |
| `rounds/r2-FS-Alts-1280` | rejected: fixed gap (today), stay with the lights strip over the pills; the user's two shots |

### Ruling

1. **"Default width" = the bar's thickness (height).** A fixed or guessed height does not line up with the traffic lights on other Macs: that is the "crooked" look. The fixed gap is the user's point 2.
2. **Height, pick N: back to the native `NSToolbar` `.unifiedCompact`.** AppKit sets the height (40 pt on this Mac, and the correct value on other Macs, macOS versions and text sizes), centres the lights, insets the items and does full screen. It costs 8 pt against today; the one-row session header pays for it. The N boards draw 22 pt pills (text 11.5, padding 9, radius 6), not 18.
3. **Height, M (only if the custom bar stays):** height = 2 × the lights' centre, measured at run time. Never a constant 28 or 32.
4. **Left inset.** In a window: the lights' right edge + 12, measured (AppKit gives it with N); never a constant 69. In full screen with the lights hidden: the bar's 12 pt padding, no gap.
5. **Full screen, pick "ride along".** No fixed gap. The left items slide right while the lights show and slide back when they hide. The right pills never move. The lights strip is never drawn over the bar.
6. **Animation: one clock, the menu bar's.** N: AppKit does it all, no own animation. M: on every frame take the progress from the full-screen title bar window's frame (p = shown height ÷ menu bar height) and set inset = lerp(12, lights' maxX + 12, p). Only when there is no frame to follow: 0.25 s ease-in-out, the same curve back on hide. Reduce Motion: no slide, positions jump with the system's cross-fade. Entering or leaving full screen: the inset snaps to the window value at the end of the system transition.
7. **While the menu bar shows.** Mac without a notch: the bar rides down with the menu bar (y 0 → 24) and covers the top 24 pt of the content with a shadow; the content does not reflow. Mac with a notch: the menu bar uses the black band above the bar (the screen's safe-area top), so the bar does not move; only the inset slides (12 → 81) and the lights fade in.
8. **Keys unchanged** (`docs/shortcuts.md`).

### Not verified by the designer (check on a real Mac)

- With N, full-screen toolbar items move right by themselves when the lights show. If they do not, the M column of `r2-Motion` is the fallback.
- On a notched Mac, the black band above the bar is `NSScreen.safeAreaInsets.top`.
