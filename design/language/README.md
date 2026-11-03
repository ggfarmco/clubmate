# Clubmate design language

Claude Design's answer to `design/brief/prompt.md`: the review, the design language and mockups of the key screens.

- `png/`: every board at 2×.
  - `Audit`, `Principles`, `Tokens`, `Components`, `AgentNodes`, `Patterns` and `Handoff` hold the review and the language.
  - `*-1280` are mockups in Kanagawa (dark); `*-640` are mockups in Catppuccin Latte (light); `Agents-Mate-1280` is the Agents screen in the Mate theme with the Mac's colour dot in the workspace pill.
  - `Mac-Settings-Remote`: the Settings → Remote page as built (native form; Remote is one plugin page in the Settings sidebar) with this Mac's name and colour, the title-bar option and paired devices.
  - `Agents-OneTree-1280-Mate` and `Agents-Drawer-640-MateLight` (source only, under "Agents sidebar, 3 Nov"): the sidebar with every open session in the window's worktree (no branch row; Closed open with worktree names and "N more", Service opened by a waiting voice session), at 1280 and as the ⌘B drawer at 640.
  - `Mac-AlwaysAllow`: the Mac confirm for Always allow… (⌥2 opens it, ⌥2 again stores the rule). `Mac-ChatGraphStep`: the Code chat on a graph step, off until the loop runs it.
  - `K1-Check-Mac`, `K2-Check-Companion`: the build check of 1 Nov 2026 (divergences per screen with impact, class a–d and fix; verdicts; decisions). The fix list that goes with them is `design/check/fix-list.md`.
  - `Main` and `Layers` present the icon.
- Only in `source/` so far (no export yet): the interface is always Mate now. `*-1280-Mate` and `*-640-MateLight` import the Kanagawa/Latte mockups with the Mate theme. `T-Themes` holds the Clubmate / Clubmate Light themes for Ghostty and nvim (palettes, 8 syntax roles, rules). `T-Nvim-Map` lists every role an nvim theme fills, with its highlight groups and Light Owl's resolved values as the example. `T-Nvim-Clubmate` fills those roles for Clubmate / Clubmate Light (shipped as `nvim-distro/lua/clubmate/mate.lua` and `client/ghostty-themes/`).
- `html/`: the same boards as standalone pages, with fonts and styles in `assets/`. `assets/cm.css` holds the theme roles and the Mac colour tokens (`--id-*`); swap the `.th-*` class on a board's root to try another palette.
- `source/`: the raw canvas files (`.dc.html`, `canvas.json`) as they live in the Claude Design project, plus `cm.css` and `c.css` that the boards link as assets. `design/README.md` says how to pull them.

`Handoff` lists the work in order. Changes split into (a) theme tokens and renderers, (b) four SDUI additions (`header` node; `button` variant/disabled/shortcut; `table` column truncate/align/priority; `empty` icon/title/action/hint) and (c) plugin content.

The companion boards (`C-*`) live in `design/companion/`; Mac colours (`C-MacColour`) are specified there.

The icon files that came with the export are byte-identical to `design/app-icon`.
