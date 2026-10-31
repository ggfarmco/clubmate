# Clubmate design language

Distilled from the boards in `design/language/` (open `png/<Board>.png` or `html/<Board>.html` when you need the picture). `u` = `theme.uiSize` (12pt by default). The boards are the target; `Theme.swift` does not have every token yet (see "Status" below).

## Principles (earlier wins)

1. **The palette is borrowed; the proportions are ours.** No hex in views. A colour is a role: fg, bg or a tone at a fixed ratio, computed once per theme. Looks wrong in one theme → fix the ratio, never the theme.
2. **One line per thing.** Rows are one `rowHeight`. Space separates groups, never items. A second line only where items are compared by more than their name (notifications, detail properties).
3. **Colour is state, never decoration.** accent = focus, selection, the one primary action. warning = needs you. danger = failed or destructive. success = finished, shown once, then muted. Running is normal: no tone.
4. **Cards are for things that ask or change.** Plan, diff, permission request, composer. Lists, properties, messages, settings stay flat; group with a section label, not a card.
5. **Every action is visible once and has a key.** A context-menu action is also in the pane's action bar or the palette, with its key printed. Focus is always visible.
6. **Names over ids; keep the end that differs.** Show what a person named or what the thing does; ids go behind "Copy id". The plugin writes a name when the user gave none ("Review loop: Coder done → Reviewer").

Brand mate green: only in the app icon, onboarding, docs and the Mate fallback theme. Never as accent, selection, success, title bar, rail or status-bar tint inside a themed window.

## Colour roles

| Role | Formula | Use |
|---|---|---|
| surface-0 | bg | editor, terminal, transcript, detail panes |
| surface-1 | mix(fg 4%, bg) | title bar, rail, sidebar, status bar |
| surface-2 | mix(fg 7%, bg) | cards, composer, buttons, pills |
| surface-3 | mix(fg 11%, bg) + shadow | palette, popovers, menus |
| text | fg | labels, content |
| text-2 | lowest fg ratio ≥ 4.5:1 on surface-1 | property values, secondary labels |
| muted | lowest fg ratio ≥ 3:1 (was fixed 55%) | row detail, timestamps, section labels, hints, keycaps |
| faint | fg 42% dark / 52% light | placeholders, disabled, line numbers; never needed information |
| border / border-strong | fg 12% / fg 22% | dividers, card outlines / fields, buttons, keycaps |
| hover / pressed | fg 6% / fg 11% overlay | pointer over a row; mouse down, open menu trigger |
| selected | accent 20% overlay + 2pt accent leading bar | selected row in the focused pane |
| selected-idle | fg 9% overlay, no bar | selection in a pane without focus |
| focus ring | accent 1.5pt inset (rows); 2pt gap + 1.5pt (controls) | keyboard cursor when it differs from the selection; focused control |
| tone-bg / tone-border | tone 15% / tone 45–50% | badges, permission fill, diff lines / permission outline, danger buttons |
| tone-ink | tone moved toward fg until ≥ 4.5:1 on bg | tone text and icons |
| on-accent | bg or white, whichever passes 4.5:1 on accent | text of the one primary button |

## Type, space, size

One family, the terminal font; hierarchy from weight and text level, not size.

| Token | Value |
|---|---|
| caption / body / strong / title / heading | u−1 / u / u 600 / u+1 600 / u+4 600 |
| count (badge numbers) | max(9, u−2) 600 |
| spacing xs s m l xl xxl section | u/6 u/3 u/2 2u/3 u 4u/3 2u (2 4 6 8 12 16 24) |
| radius s m l xl | 3 5 8 10 (keycap/badge, button/field, card, popover) |
| rowHeight / rowHeight2 | round(5u/3) = 20 / 3u = 36 (notifications) |
| control / header | 2u = 24 (buttons, fields, pills) / round(8u/3) = 32, measured, may grow |
| rail | 40pt, items 30pt |
| sidebar | 248 default, 200–320, collapses below 760pt window |

## Components (one renderer per node type)

- **list/tree row**: rowHeight, padding l, gap m, icon 14; label tail-truncates; at most one muted detail, never truncated; badge only for attention; indent u per level, chevron inside the indent.
- **table**: muted caption header, sorted column in fg with ⌄; numbers right-aligned, tabular figures; per-column truncate (tail, middle for paths, head); groups use a section label; the selected row gets an action strip.
- **badge**: height 16, radius 8, caption; tone-bg + tone-ink; neutral = hover overlay + text-2; lowercase, ≤ 2 words, one per row; never for the normal state ("running", "active"); a count is warning-filled when it means "needs you".
- **button**: control height (small 20, icon-only square). primary = accent fill + on-accent, one per pane; secondary = surface-2 + border-strong; ghost = text-2, fill on hover (toolbars, headers); danger = danger outline, filled only inside a confirm. Key printed as a keycap inside; icon-only buttons put it in the tooltip. Loading keeps the width and shows the progress verb.
- **textField/select/toggle/folderField**: control height, sunken bg + border-strong, radius m; focus = accent border + 3pt accent halo; error = danger border + one tone-ink caption below. select opens a native menu. toggle 26×16 at the row's end. folderField head-truncates.
- **form**: two-column property grid, muted key column 120 (88 below 800pt). Read-only values are plain text, editable ones are fields. Autosave; a "Saved" caption instead of a Save button.
- **empty**: icon 16 · one sentence · one action with its key · optional code hint, aligned to the content column. Never "Select an X": if something exists, show the most recent item.
- **message**: user = surface-1 block with accent `›`, radius l (the only tinted message); agent = flat markdown, no avatar or name; thought = one collapsed muted row with its duration. Reading width ≤ 820pt.
- **plan**: card, caption "Plan · n of m"; done = muted + check (no strikethrough); current = bold + accent icon; pending = text-2.
- **toolCard**: consecutive calls group into one row ("3 actions · read 1 · search 1 · edit 1"); success is silent, only running (spinner) and failed (danger row, output inline) get marks.
- **diff**: card header = middle-truncated path + Open / Full diff with keys; faint gutter numbers; ± lines at tone 13%.
- **permissionCard**: warning border, 7% warning tint, shield icon, one-line reason; the command in a sunken mono block; Allow (primary) · Always allow · Reject (danger outline). Keys ⌥1 ⌥2 ⌥3 in button order, always, without touching the draft; bare 1/2/3 or y/n only when the composer is empty or the card has focus; ⎋ stops the turn and never rejects. Pending: docked above the composer. Answered: one muted row.
- **composer**: card, min 2 lines, grows to 40% of the pane. Footer: [agent · mode ⌄] [options] [attach] · · · [hint] [primary]. Primary is Send, then Queue while the agent is busy; disabled only when empty. Stop lives in the pane header (⎋). The mode menu names the behaviour: Ask before edits · Plan · Accept edits, never a bare "Default".
- **chrome**: title pills surface-2 + border, radius m, terminal font; workspace / branch ⌄ is one pill. Rail active = fg + hover fill + 2pt accent bar; attention = 6pt warning dot. Status bar 22pt caption: core · agents · waiting … focused-pane keys · ⌘K. Palette: surface-3, rows grouped by plugin, right column keycaps only, command id muted on the selected row, sentence case. Notifications: two-line rows, tone glyph, unread accent dot, groups "Waiting for you" / "Earlier".

## Patterns

- **Detail view** (Cron, Triggers, Backlog, graph runs, any new plugin detail): header (kind icon · heading title, ≤ 2 lines then tail · state badges; normal state neutral) → action bar (primary, secondary, ghost; destructive last, pushed right; every action shows its key; below 800pt primary + two, rest under "…") → properties as a form grid (human value first, machine value muted: `Every day at 03:00  0 3 * * *`) → sections with caption labels (quoted user text in a surface-1 block; JSON only behind "Show as JSON") → history list with an in-place empty state that offers the action.
- **Truncation**: titles tail; file paths middle (first segment + last two); commands middle, keep the executable and args; folder fields head; branches middle, keep the prefix; ids and hashes not shown; numbers and times never truncate (the column gives way). Full value in the tooltip and on ⌘C.
- **Narrow windows**: ≥ 1100 rail · sidebar 248 · main · side panel 392. 800–1100: side panel becomes a bottom split or closes, sidebar 220. < 800: sidebar is an overlay on ⌘B, a one-line breadcrumb, header meta on its own line, property keys 88, action overflow, tables drop HISTORY, TIME, MEM in that order, title bar drops the window-count pill. At any width nothing overlaps: headers are measured; a pending permission docks above the composer.
- **Focus model**: exactly one pane has focus; its selection is `selected`, other panes show `selected-idle`. ⎋ goes up one level (detail → list → rail). The primary button is focused by default when a card asks something.

## SDUI additions the language needs (not in the protocol yet)

`header {icon, title, badges[], actions[], meta}` · `button {variant: primary|secondary|ghost|danger, disabled, shortcut}` · table column `{truncate, align, priority}` · `empty {icon, title, action, hint}`. Until they exist, build the closest thing from existing nodes and keep plugin content (names, words, grouping) right.

## Status

Every token in the tables above exists in `Theme.swift` (Swift names: `surface1 text2 borderStrong selectedIdle toneBg accentInk onAccent rowHeight2 …`), the renderers use them, and the fallback theme is Mate dark/light. Diff lines use `toneBg` (15%), not the board's 13%. A `title` text is one line (tail, tooltip) and every other text node reports its full height, so a header never runs under the transcript; only the oldest pending permission is docked above the composer, options ordered Allow · Always allow · Reject, ⌥N on one-time options only ("always" ones need a click), answered ones are one muted row; modes read Ask before edits · Plan · Accept edits. Bare 1/2/3 and y/n on an empty composer are not done yet. `button` has `variant`/`disabled`/`shortcut` (`sdui.WithVariant`, `WithDisabled`, `WithShortcut`; the key works and is printed) and `empty` has `title`/`icon`/`hint` and one action button (`sdui.EmptyState`), drawn top-left; text fields and selects use the sunken field chrome with a focus halo (`fieldChrome`). Not done: loading labels and error captions (no props for them). The `header` node and the form property grid exist; Cron, Triggers and Backlog use the detail pattern (helpers in `plugins/internal/automation/detail.go`; arguments other than the prompt sit behind a "Show as JSON" ghost button, open for one selection at a time, kept in the plugin kv), graph edge triggers are named "Review loop: Coder done → Reviewer". Not done: table column truncate/align/priority, autosave in Backlog, the header's overflow menu below 800pt (actions wrap instead). Chrome: the title pills (`TitlePill`: surface-2 + border, radius m, the terminal font) are workspace / ⑂ branch ⌄ as one pill, the bell and the window count; the bell count (`CountBadge`) is warning-filled while something waits for you (`hub.pinned`), neutral otherwise. The rail puts a 6pt warning dot on Agents while a top-level session waits or asks; the status bar reads `● core · N agents · N need you` (agents = running or waiting sessions, need you as the Agents sidebar counts it, warning ink), then the plugin's `<section>.status` slot and `⌘K`; connected is muted, connecting warning, failed danger with Retry. The Agents sidebar labels worktree groups with the branch, and only waiting and failed sessions have a tone. Not done there: graph sessions under their graph run (`agents.new` has no link to the run), the filter row instead of the `Needs you` group (a `tabs` item has no tone for the warning count, and three tabs with counts don't fit a 200–248pt sidebar), per-pane keys in the status bar (the app does not know the focused pane's keys) and middle truncation of branch labels in tree rows (the tree renderer tail-truncates every label). All five `Handoff` steps have landed; the "Not done" notes above are what is left.
