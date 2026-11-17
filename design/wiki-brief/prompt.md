# Clubmate: a Wiki section that looks finished

Hi. This is a new question for the Clubmate boards on this canvas: redesign the Wiki section on the Mac. Draw on top of the language boards and the screenshots here.

- Real screenshots from the test app (light theme, interface font = terminal font), uploaded as assets: `mac-1720-page`, `mac-1280-page`, `mac-640-page`, `mac-1280-table-notes` (long title, table, agent notes on), `mac-1280-mermaid`, `mac-1280-history`.
- Closest drawn screens to borrow from: `Backlog-1280-Mate`, `Backlog-640-MateLight` (sidebar + detail), `Code-1280-Mate`; rules in `Principles`, `Patterns`, `Components`, `Tokens`.

## 1. The problem, in the user's words

"The wiki looks sad: huge empty margins on the sides, and in general it isn't neat."

## 2. What the screenshots show

The Wiki is project knowledge that agents write as they work (how things work, why they were decided, examples) and the user reads. Pages are Markdown, `.mmd` diagrams or `.html` pages, grouped in folders, each with history (one git commit per write) and a "stale" badge when the code it explains changed.

Page pane:

- The page body is a column capped at 820 px and centred in the pane. At 1720 that leaves about 380 px empty on each side, and the column's left edge doesn't line up with the header's.
- The title is shown twice: in the pane header, then again as the page's own `# H1` right under it.
- The header takes three lines before any content: title, "updated Nov 17 14:04 by you", then a row of buttons (Edit in nvim, History, New page, Delete). Delete sits alone at the far right.
- A long title wraps to two lines in the header and two more in the H1.
- History replaces the page with a bare list of commit summaries; the History button turns into "Back to page".

Sidebar:

- Search field, then "Recent" (every page again, each with "you"), then "Pages" (the folder tree), then a "Show agent notes" toggle pinned at the bottom. "Recent" and "Pages" are small muted words without spacing, so the sidebar reads as one long list where every page appears twice.
- Long titles are cut ("A flaky test no longer…").
- The agents' own notes show as an "Agent notes" folder (lock icon) at the end of the tree when the toggle is on.

## 3. Questions for you

1. **Reading layout across widths (640 → 1720+).** How should a page sit in a wide pane: a reading measure aligned left with the header, centred, wider content (tables, code, diagrams) breaking out of the measure, or something that uses the extra width (an outline of the page's headings, page info)? It must still work at 640.
2. **Header.** One place for the title, not two. How compact can it be (title + meta + actions on one or two lines)? Which action is primary, and where do Delete and History go?
3. **Sidebar.** Is Recent worth its place above the tree, and in what form (fewer rows, only pages changed by others, by time)? How should section labels, folder rows, long titles, the stale badge and the agent-notes switch look?
4. **History.** What should a page's history look like (who, when, what changed), and how does the user get back?
5. **Page typography.** H1/H2 rules, tables, code, block quotes, mermaid diagrams: whatever reads better in the terminal font.

## 4. Constraints

- Keep the boards' design language (`Principles`, `Patterns`, `Components`). Colour roles, not hex values. The Mac font is the terminal font, monospaced. Compact, keyboard-first, width is scarce.
- The sidebar and header are SDUI the wiki plugin builds (search field, list, tree, header with icon, meta, badges and buttons, toggle). The page body is a web view with one shared stylesheet. That stylesheet also draws the docs popup, agents' HTML artifacts and the backlog task description, so say for each typography change whether it is for the wiki only or for every doc.
- The phone has no Wiki section; this round is Mac only.
- For each part of your design, say whether it is (a) a token or renderer change, (b) a new SDUI node or prop, (c) plugin content (words, grouping, order, which nodes), (d) new plugin logic, or (e) the doc stylesheet. Prefer (a), (c) and (e).

## 5. Deliverables

Put this round on page `r1`, at most 6 boards: 1720 and 1280 Mate dark, 640 Mate light.

- 2–3 distinct directions answering questions 1–4, each shown on the same page (`Architecture overview`) at 1720 and 640, and your recommendation.
- For your pick: the History view, search results, and a long-title page with a table.
- A handoff list with every change tagged (a)–(e).
