# Wiki brief

Claude Design pass on the Wiki section (17 Nov 2026). Canvas "Clubmate · Wiki": https://claude.ai/artifact/2Kjq9JqXuTAwhw1Y2ZB4o7 (page Today: screenshots + language boards; page r1: round 1).

- `prompt.md`: the brief sent to the Design chat.
- `boards/`: test-app screenshots of the wiki today (seeded test wiki, light theme), 640 / 1280 / 1720.
- `r1/`: round 1 boards (`.dc.html`) pulled from the canvas; `round-1.html` shows them in one page (`round-1.json`).

What the code does today: `client/ClubmateDocument/.../Web/document.css` `#doc { max-width: 820px; margin: 0 auto }` centres the page (also used by the docs popup, artifacts and the backlog description); `plugins/wiki/view.go` builds the sidebar (search, Recent, tree, agent-notes toggle) and the header (title, meta, Edit/History/New/Delete).
