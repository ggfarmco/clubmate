# Wiki — decided 17 Nov 2026

Direction B, "Margin" (Claude Design round 1, canvas "Clubmate · Wiki", https://claude.ai/artifact/2Kjq9JqXuTAwhw1Y2ZB4o7). Boards: `r1-B-History-1280-Mate`, `r1-B-Search-1280-Mate`, `r1-B-LongTitle-1280-Mate`, `r1-Handoff` (`.dc.html`), all in one page `boards.html`; B at 1720 and 640 is drawn next to A and C in `rounds/r1-ABC-*`. Brief: `design/wiki-brief/`.

The user's pain: "the wiki looks sad: huge empty margins on the sides, and in general it isn't neat". The page body was an 820 px column centred in the pane, the title showed twice, the sidebar listed every page twice (Recent + tree).

**Status:** landing 1 (rules 1, 3, 4, 8, rule 5 without New to you), landing 2a (rule 2, the margin column) and landing 2c (New to you, search results with snippets, opened hits marked and scrolled to: rules 5, 7) are built; History with diff (rule 6) is not yet, so the boards show more than the app until it lands (backlog #788, #790). Rule 4's meta on the title line (from a 640pt header), its today-only time and Reveal in Finder are built too; the full timestamp tooltip is not (the `header` has no tooltip for `meta`). How landing 1 looks: `built/`.

## The rules

1. **Layout.** The page body starts on the header's left inset; nothing is centred. Prose, lists, quotes and headings keep a reading measure of 88ch; code, tables and diagrams may grow to the content width and scroll inside their box beyond it.
2. **Margin column.** From a 960 px wide page view, a 200 px column on the right (40 px gap) holds: On this page (≥ 2 headings), Stale (the changed source path), Changes (last 3, "All n changes"), Linked from (pages whose Markdown links point here). Below 960 it folds into one row above the body.
3. **One title.** The header carries the title; the page's leading `# H1` is not drawn when it matches the title.
4. **Header** (Patterns detail header): kind icon · title (two lines max) · stale badge · meta right ("updated 14:04 by Claude", the date only before today); then the action bar: **Edit** (primary, E), **History** (⌘Y), "…" with Copy path, Reveal in Finder, Delete. New page moves to the sidebar header (+).
5. **Sidebar:** pane header "Wiki" with +; search field; "New to you" (≤ 5 pages someone else changed since you last opened them, hidden when empty) instead of Recent; "Pages" with a count; folder rows without folder icons, page count when folded; long titles tail-truncate with the full title in the tooltip; stale pages show the badge; agent notes are a folded "Agent notes" section at the end of the tree instead of the toggle.
6. **History** replaces the body, not the header: changes grouped by day (summary / who · time, +n −m), the selected change's diff beside them; ⎋ or Back to page returns; Restore this version writes a new commit with the old text (user only, with a confirm).
7. **Search:** a query replaces New to you and the tree with results (Pages, then Agent notes), two-line rows with a snippet; opening a result marks the matches on the page.
8. **Typography** (doc stylesheet, every doc): no rules under h1/h2; h1 1.23em, h2 1.15em, h3/h4 1em, weight 600; inline code without a border; `pre` on surface-1 with a border and a muted language caption; tables with a muted caption-size header row, row rules, no zebra; blockquote in text-2 with a 2 px rule. Sizes in em of the interface size, not the board's px.

## Deviations from the boards

- **"Edit", not "Edit in nvim".** Ruled by the user on 17 Nov: the wiki has no focus on nvim. The button opens the page in the workspace's editor (`editor.open`); no "nvim" in any wiki label or menu.
- **History is ⌘Y, not H.** A bare h is the sidebar tree's collapse key (`docs/shortcuts.md` § Trees); ⌘Y is History in Safari.
- **No ⌘B sidebar overlay below 800.** That is app shell, not the wiki; the sidebar stays docked as in every other section.
- **No match highlights in sidebar snippets** (handoff 29): it needs a new SDUI prop the design language doesn't name. Snippets ship without marks.
- **Search rows are one line** (handoff 28, 30): tree rows have no second line, so the snippet is the row's detail (from the first match on, since the sidebar leaves the detail about 16 characters) and the match count is dropped; an opened hit marks its matches and scrolls to the first, without the "1 of 4 · n · N" find strip. No "/" to focus search and no "⎋ clear" hint.
- **The unread mark is the row's icon** (an accent `circlebadge.fill` at icon size, in place of the page icon), not a small dot beside it; the detail "<who> · <time>" tail-truncates at 640.
- **"Pages" label only while New to you shows**, so the wiki's root pages don't read as part of it; the page count stays in the sidebar head.
- **No outline scroll-spy, no "Ask Claude to update", no "while working on …"** (handoff 7, 8, 26): later, backlog.
- **Sample content** on the boards (Claude's history entries, `[[SDUI protocol]]` links) is the designer's invention; "Linked from" uses real Markdown links between wiki pages.

## Landing order

1. **Look:** rules 1, 3, 4, 5 and 8; Recent is dropped and New to you comes with landing 2. No new plugin logic.
2. **Margin column** (rule 2), **History with diff and Restore** (rule 6), **New to you and search snippets** (rules 5, 7): one landing each.
