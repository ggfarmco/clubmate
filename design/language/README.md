# Clubmate design language

Claude Design's answer to `design/brief/prompt.md`: the review, the design language and mockups of the key screens.

- `png/`: every board at 2×.
  - `Audit`, `Principles`, `Tokens`, `Components`, `AgentNodes`, `Patterns` and `Handoff` hold the review and the language.
  - `*-1280` are mockups in Kanagawa (dark); `*-640` are mockups in Catppuccin Latte (light).
  - `Main` and `Layers` present the icon.
- `html/`: the same boards as standalone pages, with fonts and styles in `assets/`. `assets/cm.css` holds the theme roles; swap the `.th-*` class on a board's root to try another palette.
- `source/`: the raw canvas files (`.dc.html`, `canvas.json`, `cm.css`) as they live in the Claude Design artifact.

`Handoff` lists the work in order. Changes split into (a) theme tokens and renderers, (b) four SDUI additions (`header` node; `button` variant/disabled/shortcut; `table` column truncate/align/priority; `empty` icon/title/action/hint) and (c) plugin content.

The icon files that came with the export are byte-identical to `design/app-icon`.
