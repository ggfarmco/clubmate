# Clubmate design language

Claude Design's answer to `design/brief/prompt.md`: the review, the design language and mockups of the key screens.

* `png/`: every board at 2×.
   * `Audit`, `Principles`, `Tokens`, `Components`, `AgentNodes`, `Patterns` and `Handoff` hold the review and the language.
   * The interface is always Mate: every window and every plugin panel, dark or light following macOS.
      * `*-1280-Mate` and `*-640-MateLight` are the key screens in Mate dark and Mate light.
      * `Agents-Mate-1280` is the Agents screen with the Mac's colour dot in the workspace pill.
      * The Agents tree (3 Nov, main cdc26f6a): `Agents-1280` and `Agents-Mate-1280` show two worktrees with Closed and Service at the root; `Agents-OneTree-1280-Mate` shows one worktree with no branch row, Closed rows carrying their worktree and Service open because it needs you; `Agents-Drawer-640-MateLight` is the same tree opened with ⌘B at 640.
      * `Graphs-1280-Mate` and `Graphs-640-MateLight` are the drawn graph run as built on 3 Nov (main 43f02ac2); `Graphs-Def-*` is the definition, with an edge shown as its trigger.
   * `Graph-Steps`: steps in graphs (nodes Clubmate runs itself): step vs agent card, states, approval, output, edge caps, trust, History bars.
      * `Graphs-Gate-1280-Mate` (Review gate: Tests failed once, Coder in round 2, output tail), `Graphs-Nightly-1280-Mate` (Ship running 40 min; empty output), `Graphs-GateDef-1280-Mate` (definition, untrusted file).
      * `Graphs-Approve-640-MateLight` (a step waiting for approval), `Graphs-Cap-640-MateLight` (round cap hit), `Graphs-NightlyDef-640-MateLight` (the Trust… sheet).
      * The phone side is `C-Step-Approve` and `C-Step-NeedsYou-Light` in `design/companion/`.
   * `Drawn-Views`: the vocabulary of the drawn views (cards, ports, edges, markers, lanes, run strip), what each screen draws and what stays a form or a list, the rules and the handoff.
      * `Cron-Runs-1280-Mate` (missed and failed, run strip), `Cron-Paused-640-MateLight`, `Cron-Empty-640-MateLight`.
      * `Triggers-Edge-1280-Mate` (a graph's trigger, drawn in its graph), `Triggers-Chain-640-MateLight` (a chain, a failed run), `Triggers-Empty-640-MateLight`.
      * `Backlog-Owned-1280-Mate` (owned by a graph run, owners as lanes), `Backlog-Start-640-MateLight` (Start agent; last owner failed), `Backlog-Empty-640-MateLight`.
   * `*-1280` (Kanagawa, dark) and `*-640` (Catppuccin Latte, light) are the earlier mockups. The Mate boards import them, so they stay as the source.
   * `T-Themes`: the Clubmate / Clubmate Light themes for Ghostty and nvim (palettes, 8 syntax roles, rules). The terminal and nvim use them by default, and a theme the user picks wins.
   * `T-Nvim-Map`: every role an nvim theme fills, with its highlight groups and Light Owl's resolved values as the example.
   * `T-Nvim-Clubmate`: the nvim side of Clubmate / Clubmate Light.
      * One value per T2 role, dark and light, with contrast on the background.
      * Mocks of code, completion, the picker, which-key, a diff, neo-tree and every lualine mode.
      * Decisions:
         * lime chrome accent;
         * types blue, not the warning amber;
         * diff fills at 15%, words at 28%;
         * the themes ship by name through `colors/clubmate.lua`.
   * `Mac-Settings-Remote`: the Settings → Remote page as built (native form; Remote is one plugin page in the Settings sidebar) with this Mac's name and colour, the title-bar option and paired devices.
   * `Mac-AlwaysAllow`: the Mac confirm for Always allow…. ⌥2 opens it, and ⌥2 again stores the rule.
   * `Mac-ChatGraphStep`: the Code chat on a graph step, off until the loop runs it.
   * `K1-Check-Mac`, `K2-Check-Companion`: the build check of 1 Nov 2026 (divergences per screen with impact, class a–d and fix; verdicts; decisions). The fix list that goes with them is `design/check/fix-list.md`.
   * `Main` and `Layers` present the icon.
* `html/`: the same boards as standalone pages, with fonts and styles in `assets/`.
   * `assets/cm.css` holds the theme roles and the Mac colour tokens (`--id-*`).
   * To try another palette, swap the `.th-*` class on a board's root (`th-mate` / `th-matelight` for the interface).
* `source/`: the raw canvas files (`.dc.html`, `canvas.json`) as they live in the Claude Design project, plus `cm.css` and `c.css` that the boards link as assets. `design/README.md` says how to pull them.

`Handoff` lists the work in order. Changes split into three classes:
* **(a)** theme tokens and renderers;
* **(b)** four SDUI additions: the `header` node; `button` variant/disabled/shortcut; `table` column truncate/align/priority; `empty` icon/title/action/hint;
* **(c)** plugin content.

The companion boards (`C-*`) live in `design/companion/`; Mac colours (`C-MacColour`) are specified there.

The icon files that came with the export are byte-identical to `design/app-icon`.
