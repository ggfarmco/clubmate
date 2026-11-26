# Guided reading brief

Design pass on the guided code reading SDUI components (backlog #1294, #1295; research: `clubmate-notes/research/guided-code-reading.md` § Candidate SDUI components), 26 Nov 2026.

**Status: ruled by Claude Design, 26 Nov 2026** (round 1 and one follow-up; chat https://claude.ai/chat/864f0eb9-c8cd-468a-9ce9-4baff23c36cb). An agent draft came first, when the Chrome extension was not connected; the designer replaced it. Two points are left to the user (`design/features/guided-reading/rules.md` § Left to the user).

Canvas "Clubmate · guided reading": https://claude.ai/artifact/DwTbyRojLTBm1dTuj2j74w (page Round 1: the final set; Today: shots + question card fold boards; Language: Agents-Mate-1280, Components, C-Components, C-Question). Assets: cm.css `/_blob/cd5295310c5e9c7200233f268a9b7bfe`, c.css `/_blob/f71a336b82436dbd14e5736a721ce641` (gr.css `/_blob/185b3df6…` was only for the agent draft), shots `7cae9c97…` (1280), `685cd091…` (640).

- `prompt.md`: the brief and the follow-up for the Design chat.
- `today/`: test-app shots (main checkout's Debug build, fakeagent script with an elicitation) of today's question card in a guide-like session, Mac 1280 and 640, Mate light.
- `round1.json`: the round page config; `boards/round1.html`: the final set, then round 1 as first drawn, as one self-contained page. Build (`mkdir -p "$TMPDIR/boards-page/rounds"` first, the script does not make subfolders): `node scripts/design/boards-page/build.mjs design/features/guided-reading design/language/html/assets design/guided-reading-brief/round1.json design/guided-reading-brief/boards/round1.html`.
- The boards (`Final-*.dc.html`, `Rules.dc.html`, PNGs, `boards.html`), the first round (`rounds/`) and the ruling (`rules.md`) are in `design/features/guided-reading/`.
