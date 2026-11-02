# Design

The boards live in the Claude Design project "Clubmate design": https://claude.ai/artifact/GzeKKbWXXXGoUBqGS3S2nN. `language/` and `companion/` are its copy in the repo; `brief/`, `companion-brief/` and `check/` are inputs we send it.

## Reaching the project from Claude Code

The `Artifact` tool reads and writes the project. The interactive `claude` has it; agent sessions in Clubmate (Agent SDK) get it only with `CLAUDE_CODE_ARTIFACT=1`, e.g. `"env": {"CLAUDE_CODE_ARTIFACT": "1"}` in `.claude/settings.local.json`. The flag is undocumented and read from the checkout's own settings, so a worktree needs its own copy.

`/design-sync` and `DesignSync` are something else: they upload a React component library into a design-system project and don't see this one.

## Pulling the boards

1. `Artifact list scope=files url=<project>`: the boards are `project/*.dc.html` plus `project/canvas.json`; `index.html`, `SKILL.md` and `artifact-type/*` belong to the Design type.
2. `Artifact read paths=[project/…]` saves them to the scratchpad. `cm.css` and `c.css` aren't files: each board links them as `/_blob/<id>` assets; read those ids (`Artifact read path=<id>`).
3. Copy into `source/`: `C-*`, `Mac-Settings-Remote` and `Agents-Mate-1280` go to `companion/source/`, the rest and `canvas.json` to `language/source/` (`Mac-Settings-Remote`, `Agents-Mate-1280`, `cm.css` and `c.css` are in both). `cmp` against what's there shows which boards changed.

`png/` and `html/` are renders, not project files: after a board changes, they still come from the project's export.

## Sending screenshots for a check

`Artifact publish url=<project> asset=true file_paths=[…]` uploads up to 25 PNGs per call into the project's assets (`list scope=assets` shows them). Read the project in the same session first: a publish to an artifact the session hasn't read is refused. Not tried yet: whether the Design chat picks up these assets for a check the way it does attached images.
