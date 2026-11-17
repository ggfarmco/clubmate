# Wiki — decided 17 Nov 2026

Direction B, "Margin" (Claude Design round 1, canvas "Clubmate · Wiki", https://claude.ai/artifact/2Kjq9JqXuTAwhw1Y2ZB4o7). Boards: `r1-B-History-1280-Mate`, `r1-B-Search-1280-Mate`, `r1-B-LongTitle-1280-Mate`, `r1-Handoff` (`.dc.html`), all in one page `boards.html`; B at 1720 and 640 is drawn next to A and C in `rounds/r1-ABC-*`. Brief: `design/wiki-brief/`.

The user's pain: "the wiki looks sad: huge empty margins on the sides, and in general it isn't neat". The page body was an 820 px column centred in the pane, the title showed twice, the sidebar listed every page twice (Recent + tree).

**Status:** landing 1 (rules 1, 3, 4, 8, rule 5 without New to you), landing 2a (rule 2, the margin column), landing 2b (rule 6, History with diff and Restore, without Show page at this version) and landing 2c (New to you, search results with snippets, opened hits marked and scrolled to: rules 5, 7) are built. Rule 4's meta on the title line (from a 640pt header), its today-only time and Reveal in Finder are built too; the full timestamp tooltip is not (the `header` has no tooltip for `meta`). Folder index pages: see § Round 2. Left: backlog #803, #805, #808. How it looks: `built/` (`margin-*.jpg` for the margin column, `mac-1280-history.jpg` for History).

## The rules

1. **Layout.** The page body starts on the header's left inset; nothing is centred. Prose, lists, quotes and headings keep a reading measure of 88ch; code, tables and diagrams may grow to the content width and scroll inside their box beyond it.
2. **Margin column.** From a 960 px wide page view, a 200 px column on the right (40 px gap) holds: On this page (≥ 2 headings), Stale (the changed source path), Changes (last 3, "All n changes"), Linked from (pages whose Markdown links point here). Below 960 it folds into one row above the body.
3. **One title.** The header carries the title; the page's leading `# H1` is not drawn when it matches the title.
4. **Header** (Patterns detail header): kind icon · title (two lines max) · stale badge · meta right ("updated 14:04 by Claude", the date only before today); then the action bar: **Edit** (primary, E), **History** (⌘Y), "…" with Copy path, Reveal in Finder, Delete. New page moves to the sidebar header (+).
5. **Sidebar:** pane header "Wiki" with +; search field; "New to you" (≤ 5 pages someone else changed since you last opened them, hidden when empty) instead of Recent; "Pages" with a count; folder rows without folder icons, page count when folded; long titles tail-truncate with the full title in the tooltip; stale pages show the badge; agent notes are a folded "Agent notes" section at the end of the tree instead of the toggle.
6. **History** replaces the body, not the header: changes grouped by day (summary / who · time, +n −m), the selected change's diff beside them; Back to page (⌘Y) returns; Restore this version writes a new commit with the old text (user only, with a confirm).
7. **Search:** a query replaces New to you and the tree with results (Pages, then Agent notes), two-line rows with a snippet; opening a result marks the matches on the page.
8. **Typography** (doc stylesheet, every doc): no rules under h1/h2; h1 1.23em, h2 1.15em, h3/h4 1em, weight 600; inline code without a border; `pre` on surface-1 with a border and a muted language caption; tables with a muted caption-size header row, row rules, no zebra; blockquote in text-2 with a 2 px rule. Sizes in em of the interface size, not the board's px.

## Deviations from the boards

- **"Edit", not "Edit in nvim".** Ruled by the user on 17 Nov: the wiki has no focus on nvim. The button opens the page in the workspace's editor (`editor.open`); no "nvim" in any wiki label or menu.
- **History is ⌘Y, not H.** A bare h is the sidebar tree's collapse key (`docs/shortcuts.md` § Trees); ⌘Y is History in Safari.
- **History: ⌘Y only, no ⎋, no key for Restore.** Ruled on 17 Nov: a bare esc is reserved for the app (SDUI catalog), and a bare R collides with the tree's keys; Back to page is ⌘Y, Restore this version is pressed (confirmed), and Edit (E) stays in the History bar.
- **History list: equal panes, one-line rows.** Ruled on 17 Nov: the list and the diff split the pane equally and each change is one row with who · time as its detail, until `split` gets a width prop and the tree a two-line row (backlog #805).
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

## Round 2 · folder index pages — decided 17 Nov 2026

Direction C, "Heading + first row" (Claude Design round 2), plus "In this folder" in the margin column. Boards: `r2-Handoff.dc.html` (rules 1–17), `r2-C-iPad.dc.html`, `r2-C-iPhone.dc.html`; A/B/C side by side in `rounds/r2-Tree-*`. Brief: `design/wiki-brief/prompt-r2.md`, `round-2.json`, `round-2.html`.

A folder's own page is `<folder>/index.md`, else `<folder>/README.md` (Markdown, any depth, never under `.agents/` or at the wiki root).

**Rules as built (handoff numbers):**

1. Section labels stay headings: not selectable, a click folds them, j/k skip them while unfolded (see Deviations). The selectable section label of the first build is gone.
2. A top-level folder with a page (a section label in a single-project wiki) keeps its name; its page is the first row under it, with the page's title and the folder-page icon.
3. Any other folder with a page is one row: chevron in the indent, folder-page icon, the page's title, tail-truncated, its stale badge; its other pages are its children and its page is not listed again.
4. A folder holding only its page is a page row with the folder-page icon and no chevron, among the pages it sits with (top-level ones among the wiki's root pages).
5. Plain folders are as before.
6. The folder-page icon is the SF Symbol `list.bullet.rectangle.portrait` (a page with list lines); pages keep `doc.text`.
7–9. Every tree (SDUI renderer): a row with children and an open action (`mirrorOf` or `open`) opens on a click or ↩ and unfolds while folded, a click never folds it; its chevron is a 20 × 20 target in the indent with a hover ring that only folds; a plain folder row (`action`, children, no open action) folds on a click of the whole row, as on the Companion; l/→ unfolds, then steps to the first child; h/← folds, then goes to the parent; j/k skip unfolded section labels. Tables share the chevron cell.
10. An index page's header uses the folder-page icon.
11. On an index page the margin column starts with **In this folder · n**: subfolders, then the other pages, each with its age; the narrow fold row lists "n in this folder" first.
15. Companion: a tree row with children and an open action opens its page on a tap and gets a trailing 44 pt disclosure button that folds it; an `action` row with children and no open action folds on a tap of the whole row; taps and menus send the row's target (`mirrorOf`), like the Mac.

**Answers to the designer's questions:** a multi-project wiki applies the rule per project (its folders are rows under the project row, so a folder with a page is one row there, rule 3); with both index.md and README.md, README.md is an ordinary page; a root index.md stays an ordinary root page for now.

**Deviations:**

- **j/k skip only unfolded section labels**, not every section label (rule 9 says "j/k skip section labels"): amended in land review 2 because a folded section (Backlog's Done, the wiki's Agent notes, Graphs' Runs) became unreachable from the keyboard; a folded label is a stop and ↩ or l unfolds it.

- **Tooltip is the title only**, not title + path (rule 3): tree items have no tooltip prop; the row shows the title and its `.help` is the label.
- **Plain folder names are not in text-2** (rule 5): a section-label folder keeps the muted caption, a folder row under a project row the foreground ink; a text-2 label needs a tree-item prop the design language doesn't name.
- **Companion top-level folders are not section headers** (rule 15): the phone tree has no section headers; a top-level folder is a row that folds from the whole row, its page the first row under it.
- **iPhone drill-down waits** (rule 16): the Companion renders a `tree` as one indented list on the iPhone and the iPad alike, with no pushed list per folder; pushing a folder's list needs a drill-down tree renderer first.
- **Count while folded**: a section label counts every page under it (its page row included), a folder row the pages under it besides its own.

**Not in this release:** 12 (caption breadcrumb and the header `crumb` prop), 13 (Page in this folder ⌘⇧N), 14 (Add a page for this folder), 17 (Companion page breadcrumb and "In this folder" inset group), a root index.md as the wiki's home.

**Status:** rules 1–11 and 15 built on `feat/wiki-folder-index`. Go tests for the tree, the header icon and the margin; ClubmateKit test for j/k; ClubmateProtocol test for the tap rule; mac-ui `TestWikiFolderRowOpensItsIndexPage` (click opens and unfolds, chevron folds, ↩, j skips the label, cursor from outside the tree); XCUITest `testTreeRowWithAPageOpensItAndTheChevronFolds` on the phone gallery.
