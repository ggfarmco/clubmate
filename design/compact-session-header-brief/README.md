# Compact session header brief

Design pass on a one-row session header in the Mac Agents view and a lower window top bar, 26 Nov 2026.

The user's words (Russian, translated): "Make the session transcript header more compact, about the height of the title. On small screens it takes useful space. And the top header could also be a third lower."

Canvas "Clubmate · compact session header": https://claude.ai/artifact/6SCBqSsbVqR7XQWvk5eLcg (pages Today: the user's real shot; Language: Agents-Mate-1280, Agents-640, Agents-640-MateLight, Components; r1: the round). Chat https://claude.ai/chat/11469688-8064-4900-885a-b41b015abf79. Assets: cm.css `/_blob/77baef3ce02f146bccd35190e260a9d1`, shot `/_blob/051b722200106a40a87321a6cf33e333`.

- `prompt.md`: the brief as sent.
- `today/mac-1280.jpg`: the user's screenshot of today's header (3 rows) and top bar.
- `round-1.json`: the round page config. Build: `node scripts/design/boards-page/build.mjs design/features/compact-session-header design/language/html/assets design/compact-session-header-brief/round-1.json design/features/compact-session-header/boards.html`.
- `round-2.json`: round 2 (top bar height and full screen, 27 Nov 2026). Build: `node scripts/design/boards-page/build.mjs <copy of the r2 boards with the shots' /_blob/ links replaced by data: URIs> design/language/html/assets design/compact-session-header-brief/round-2.json design/features/compact-session-header/boards-r2.html`. Shots: `today/user-fullscreen-menubar.png` (`/_blob/7950d882f9676a5f0f35f697aab80324`), `today/user-after-exit.png` (`/_blob/94220997de14523e3828aae07788fc7d`).
- The ruling and the boards are in `design/features/compact-session-header/`.
