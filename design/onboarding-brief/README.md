# Onboarding brief

Input for a Claude Design round on Clubmate's first run and the public GitHub repo (23 Nov 2026). Audience: people who already use Claude Code and want to run agents on a Mac.

- `prompt.md`: the brief sent to the Design chat (sent as one paragraph; backticks became quotes).
- `screens/`: today's screens. Mac from the test app (fresh state, `CLUBMATE_ONBOARDING=1`, Mate light by day); iPhone 18 Pro simulator, unpaired, dark and light.

| Shot | What |
|---|---|
| `mac-firstrun-window` + `mac-editor-sheet` | first start, no workspace: Welcome dimmed under "Choose your editor" |
| `mac-welcome-empty`, `mac-welcome-recents` | Welcome after the sheet, without and with recents |
| `mac-nvim-import-picker` | first project in Code: Neovim asks to import `~/.config/nvim` |
| `mac-project-agents` | Agents in a new project, empty |
| `mac-home-agents`, `mac-home-backlog`, `mac-home-wiki` | Home's empty sections |
| `iphone-first-dark`, `iphone-first-light` | Companion before pairing |

## What the designer decides

1. What Welcome offers instead of "New terminal" (⇧⌘N may stay as a key).
2. The first-run flow: environment check → editor → first agent → phone pairing; skippable or not; sheet or inline.
3. Hints for the first agent.
4. When and how to offer Companion pairing.
5. README structure, hero image, social preview 1280×640, GitHub About line and topics.

## Today in code

- Welcome: `client/Clubmate/Windows/WelcomeView.swift` (`WelcomeEntry`, actions at :54); handling `client/Clubmate/App/WorkspaceActions.swift:37`; menu "New Home Terminal" `client/Clubmate/App/ClubmateApp.swift:169`.
- Editor sheet: `client/Clubmate/App/Onboarding.swift`; core `internal/plugin/onboarding.go` (shown only on a fresh state without VS Code), `internal/rpc/methods_plugin.go`. Test mode shows it only with `CLUBMATE_ONBOARDING=1` (`cmd/clubmate-core/main.go:224`).
- First agent: `plugins/agents/install.go` installs `@agentclientprotocol/claude-agent-acp` with npm on the first session (Node 22+, "Node 22+ is required to run Claude"); no check runs before that.
- Companion: `client/Companion/App/RootView.swift` (unpaired "Pair a Mac"); Mac pairing only in Settings › Devices (`client/Clubmate/App/DevicesSettings.swift`).
- Repo: github.com/ggfarmco/clubmate is public, no README, no description, no topics. Releases: signed, notarized `Clubmate-<v>.dmg` + Sparkle `appcast.xml`.

## Rounds

Claude Design, 23 Nov 2026. Canvas "Clubmate · Onboarding": https://claude.ai/artifact/A9ZBWRzQyAWucnyaNdEQZt, chat https://claude.ai/chat/0276257a-1c4a-44b8-9a7e-c305a1d17761. Pages: Today (`Main`: every shot named), Language (`Principles`, `Tokens`, `Components`, `Patterns`, `C-Language`, `C-Components`, `C-Workspace`, `Mac-Settings-Remote`, `Agents-Mate-1280`), r1. Canvas copies of cm.css / c.css / Principles logo: `/_blob/46bb52f6…` / `/_blob/de1f5223…` / `/_blob/590f7a17…`.
- `round-1.json` … `round-4.json`: sources of `round-<n>.html` (built with `scripts/design/boards-page/build.mjs`; the boards' `/_blob/` images were replaced by `data:` copies before the build).
- r1 (6 boards): user took all A picks (Welcome one door, silent check, three hints, pairing after the first turn); Clone repository… later.
- r2 (4 boards): editor A (no editor until needed) and "Your workshop. Claude Code is the crew." The user picked editor B (Neovim on silently) and rejected the line: Claude Code is only one of the agents.
- r3 (4 boards): "Your garage. Agents welcome."; editor B detail; both editors off = hide. The user found the hero crooked: fixed (one 64 px margin). Part of it was `build.mjs` itself: its page class `.card` also styled the boards' `.card` elements (renamed to `.bp-card`).
- r4 (3 boards): workshop wording, real screenshots. The user picked line B "Your workshop, set up your way." and the export. Result: `design/features/onboarding/`.

## Real screenshots

`screens/real-home-*.jpg` come from a test build in Mate dark, 2560×1600, 23 Nov 2026:

1. A local, uncommitted check in `GhosttyApp.systemIsDark` that sets `NSApp.appearance = darkAqua` when the launch arg `-ShotDark YES` is set; `make app`; revert the source.
2. `scripts/test-app.sh start <name> -- -ShotDark YES` under `env -i`, with `CLUBMATE_AGENTS_COMMAND=<fakeagent>` and `FAKEAGENT_SCRIPT=<copy of design/check/mac/fakeagent-script.json>`; the copy gets `"fork": true` (asides) and two `prompt` steps that answer "Brief me" and "Why did the audit fail". The fake agent reads its script only at start.
3. Seed Home (`workspace` file = Home's id): three `cron.add` jobs (Morning brief 0 8 * * *, Dependency audit, Sunday money check) running `agents.new`, one `triggers.add`, four `backlog.add`, two `wiki.write`, `agents.new prompt="Rate-limit the checkout retries"` (waits on a permission), `plugins/agents/graphs/testdata/.clubmate/graphs/review-loop.json` copied into Home's `.clubmate/graphs/` and `agents.graph-run graph=review-loop input="fix the flaky switcher test"`, then `cron.run` of the Morning brief job.
4. `agents.aside session=<brief> anchor='[{"key":"message","value":"agent-1"},{"key":"quote","value":"…"}]'`, `agents.prompt` the aside, `cm workspace open <home>`, `ui.focus section=agents`, select the brief, `ui key cmd+]`, `ui snapshot --size 1280x800`; then `ui.focus` backlog, wiki, cron, triggers.
