# Guided reading brief

Design pass on the guided code reading SDUI components (backlog #1294, #1295; research: `clubmate-notes/research/guided-code-reading.md` § Candidate SDUI components), 26 Nov 2026.

**Status: DRAFT.** The Chrome extension was not connected, so Claude Design was not reached. The agent seeded the canvas and drew a draft round itself; the real round is still to hold.

Canvas "Clubmate · guided reading": https://claude.ai/artifact/DwTbyRojLTBm1dTuj2j74w (page Round 1: the 6 draft boards; Today: shots + question card fold boards; Language: Agents-Mate-1280, Components, C-Components, C-Question). Assets: cm.css `/_blob/cd5295310c5e9c7200233f268a9b7bfe`, c.css `/_blob/f71a336b82436dbd14e5736a721ce641`, gr.css `/_blob/185b3df60707c2be0aa85adc3e135ead`, shots `7cae9c97…` (1280), `685cd091…` (640).

- `prompt.md`: the brief and the follow-up for the Design chat.
- `today/`: test-app shots (main checkout's Debug build, fakeagent script with an elicitation) of today's question card in a guide-like session, Mac 1280 and 640, Mate light.
- `round1.json`: the round page config; `boards/round1.html`: the round as one self-contained page. Build: `node scripts/design/boards-page/build.mjs design/features/guided-reading design/language/html/assets design/guided-reading-brief/round1.json design/guided-reading-brief/boards/round1.html`.
- The boards themselves (`R1-*.dc.html`, `gr.css`, PNGs) and the draft ruling are in `design/features/guided-reading/`.
