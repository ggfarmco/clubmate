# Design

The repo is the source of truth for design; Claude Design canvases are where boards get drawn.

- `language/` and `companion/`: the design language and every board drawn up to 13 Nov 2026 (screens, Review, MR, Graph-Steps…), `source/` + `png/` + `html/`.
- `features/<slug>/`: one folder per feature decided since 16 Nov: the chosen boards (`.dc.html`), `png/`, `rules.md` (what was decided, keys, handoff), rejected directions under `rounds/`. Builders and the nightly check read these.
- `archive/canvas-2026-11-16/`: the shared canvas's layout (`canvas.json`) and the 70 boards that lived only there before it was cut down to the language.
- `*-brief/`, `check/`: what we sent a designer.

The shared Claude Design project "Clubmate design" (https://claude.ai/artifact/GzeKKbWXXXGoUBqGS3S2nN) holds only the language: `Main`, `Layers`, `Principles`, `Tokens`, `Components`, `AgentNodes`, `Patterns`, `Drawn-Views`, `C-Language`, `C-Components`, `T-Themes`, `T-Nvim-Map`, `T-Nvim-Clubmate`. Never draw a feature on it.

## Working with a designer

Talk to Claude Design through the Claude in Chrome extension; the canvas and its chat are one page, and every board on it is a live page, so the canvas must stay small.

1. **One canvas per feature.** Create a Design canvas "Clubmate · <feature>" (`Artifact publish type_url=<Design type> title=…`). Seed it with only what the feature touches: the language boards it needs (from `language/source/`, `companion/source/`) and real screenshots of today's screens from the test app (`scripts/test-app.sh`, the companion simulator), uploaded as assets. Not old screen mockups: the app is the truth for how a screen looks today.
2. **Rounds.** Each round is at most 6 boards on its own canvas page (`pages` in `canvas.json`); the canvas opens on the latest. Say so in the brief: "put this round on page `r<n>`, at most 6 boards, 1280 Mate dark + 640 Mate light + phone dark/light only where they differ".
3. **What the user sees.** One composite PNG per round in `design/<feature>-brief/boards/` (directions side by side, labelled A/B/C), opened with `docs_open`. Not the canvas.
4. **Chrome.** One canvas tab at a time; close it after each exchange with the designer.
5. **After the pick.** Export the chosen boards to `features/<slug>/` (`.dc.html` via `Artifact read`, PNG renders, `rules.md`), rejected ones to `features/<slug>/rounds/`. The feature canvas is done after that; nothing goes back to the shared canvas.
6. **Language changes.** A feature that adds or changes a component, token or pattern updates the language boards on the shared canvas and `.claude/skills/clubmate-ui/references/design-language.md` (or `companion.md`) as a separate, named step.

## Reaching the project from Claude Code

The `Artifact` tool reads and writes the project. The interactive `claude` has it; agent sessions in Clubmate (Agent SDK) get it only with `CLAUDE_CODE_ARTIFACT=1`, e.g. `"env": {"CLAUDE_CODE_ARTIFACT": "1"}` in `.claude/settings.local.json`. The flag is undocumented and read from the checkout's own settings, so a worktree needs its own copy.

`/design-sync` and `DesignSync` are something else: they upload a React component library into a design-system project and don't see this one.

## Pulling boards

1. `Artifact list scope=files url=<canvas>`: the boards are `project/*.dc.html` plus `project/canvas.json`; `index.html`, `SKILL.md` and `artifact-type/*` belong to the Design type.
2. `Artifact read paths=[project/…]` saves them to the scratchpad. `cm.css` and `c.css` aren't files: each board links them as `/_blob/<id>` assets; read those ids (`Artifact read path=<id>`). A new canvas gets them with `Artifact publish asset=true from_url=<shared project> asset_ids=[…]` and the boards' `/_blob/` links rewritten to the copies.
3. Language boards go to `language/source/` (`C-*` to `companion/source/`), feature boards to `features/<slug>/`. `cmp` against what's there shows which changed.

`png/` and `html/` are renders, not project files.

## Sending screenshots for a check

`Artifact publish url=<canvas> asset=true file_paths=[…]` uploads up to 25 PNGs per call into the canvas's assets (`list scope=assets` shows them). Read the canvas in the same session first: a publish to an artifact the session hasn't read is refused. An asset keeps no file name, only `/_blob/<id>`: in the Design chat name each id with its board ("compare `/_blob/<id>` with Mac-AlwaysAllow"), and the chat reads it like an attached image.

## Nightly check

The "Nightly design check" cron job runs the `design-check` graph (`.clubmate/graphs/design-check.json`, `docs/agent-graphs.md` § Design check), whose nodes follow the `nightly-design-check` skill: it captures the screens whose UI changed (every board on Sundays), judges them against the repo boards with Claude Design's craft rules, fixes small token and copy drift, files the rest with tag `design-check` and leaves a request for the Design chat in `clubmate-notes/design-check/<date>.md`.
