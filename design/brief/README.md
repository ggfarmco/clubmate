# Design brief

Input for a Claude Design review: upload `screens/*.png` and `clubmate-icon.svg`, paste `prompt.md`.

## Re-capturing the screens

1. `make app`; build `plugins/agents/internal/fakeagent` and write a `FAKEAGENT_SCRIPT` (thought, plan, read/search/edit tool calls with a diff, a reply, then a permission request).
2. `CLUBMATE_AGENTS_COMMAND=<fakeagent> FAKEAGENT_SCRIPT=<json> scripts/test-app.sh start <name>`.
3. Seed: `backlog.add`, `cron.add`, `triggers.add` (`cm -- command run <id> --args '<json>'`), two `terminals.new`, `cm notify`, `cm agent spawn`, a graph in `.clubmate/graphs/` started with `graphs.run`.
4. Per section `cm -- command run ui.focus section=<id>`, then `cm -- ui snapshot --size 1280x800` (Agents also `640x800`); overlays with `cm -- ui present palette|worktree|switcher|settings`; the expanded edit card with `ui present card:<session>/tool/<id>`.
5. The approval sheet from a second instance started with `-- -NSRequiresAquaSystemAppearance YES` and `ui present approval`.
