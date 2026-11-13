# Architecture

Clubmate is a native macOS IDE built around your own Neovim, real terminals and coding agents. A Go **core** owns all state and processes. Features are **plugins** that render UI as data (SDUI). A thin **SwiftUI app** draws that UI and hosts terminal surfaces.

```
 Clubmate.app (SwiftUI)          cm / clubmate CLI         agents (MCP)
  ClubmateKit · Ghostty surfaces        │                      │
        │  JSON-RPC (unix socket)       │                      │ clubmate mcp
        └───────────────┬───────────────┴──────────────────────┘
                        ▼
                 clubmate-core (Go, one per user)
   rpc · command registry · bus (SQLite) · sdui store · workspaces
   sessions · process tracker · plugin supervisor · notify · mcp
        │ JSON-RPC over stdio                     │ spawns, attaches
        ▼                                         ▼
  plugins (one process each)               clubmate-session (one per PTY)
  editor-nvim · editor-vscode · git        terminals, nvim TUI; survives
  search · terminals · processes            app and core restarts
  agents · cron · backlog · triggers
  graphs · docs · voice · remote
```

## Processes
- **`clubmate-core`** (`cmd/clubmate-core`): a single daemon per user, guarded by a lock in the state dir. It listens on a unix socket in the runtime dir and outlives the app. The app starts it on demand (`CoreLauncher`), as does the CLI.
- **`clubmate-session`** (`cmd/clubmate-session`, `internal/sessionhost`): one PTY host per terminal session, in its own session (`setsid`). It keeps a scrollback ring, so a terminal or the editor's nvim survives an app or core restart and is reattached. While no client is attached, it answers a program's status query itself and keeps the terminal queries a full-screen program sends (DECRQM, the kitty keyboard query, DA1); the first terminal that attaches and reports its size gets them, so the program still learns what the terminal supports. A terminal that attaches to a full-screen program gets the input modes the program turned on (cursor keys, mouse, focus, bracketed paste, grapheme clusters, theme updates, kitty keyboard flags). When the program turns on in-band resize reports (mode 2048, as nvim 0.12 does in Ghostty, after which it ignores `SIGWINCH`), the host sends it `CSI 48;rows;cols;0;0 t` itself right away and on every resize, so a terminal attached later still resizes it. Before it starts the program it raises the soft open-file limit (`RLIMIT_NOFILE`) to 10240, capped by the hard limit; the bundled tools dir (`Contents/Resources/tools`: `rg`, `nrev`) is appended to `PATH` for nvim, terminals and agent sessions.
- **Plugins** (`plugins/*`): separate Go binaries started by the core's supervisor (`internal/plugin`). They speak JSON-RPC over stdin/stdout through `pkg/pluginsdk`, and are restarted with backoff when they crash, stop answering `plugin.ping` (3 missed pings, every 15 s) or let 3 commands or UI events in a row time out with no reply in between.
- **`clubmate` / `cm`** (`cmd/clubmate`, `internal/cli`): CLI over the same socket. It opens folders (`cm <folder>`), runs commands, reads the bus, posts notifications (`cm notify`), collects diagnostics and serves MCP (`clubmate mcp`).
- **Clubmate.app** (`client/`): `ClubmateKit` holds the logic and is tested with `make kit-test`. The `Clubmate` app target holds views, windows, Ghostty surfaces, Sparkle updates and UserNotifications. Two Swift packages are shared with the Companion: `ClubmateProtocol` (wire types: SDUI nodes, agent sessions, bus events) and `ClubmateDocument` (the docs viewer's web view, mermaid, zoom).
- **Companion** (`client/Companion`, `client/CompanionKit`, `client/companion.yml`): iPhone/iPad app that drives agents on paired Macs over Tailscale through the `remote` plugin. It shows plugins' SDUI slots and native screens (`Companion/Plugins`: Agents, Backlog, Docs, Inbox) built on the fields and commands those plugins send. `CompanionKit` (Foundation only) holds the logic and is tested with `make companion-kit-test`; `client/CompanionTests` holds the app target's unit tests; `make companion` builds the app for the simulator; `make companion-ui-test` runs the XCUITest scenarios (`client/CompanionUITests`) against the Debug fixture server on a dedicated, freshly erased simulator; `make companion-e2e` runs `client/CompanionE2ETests` against a real headless core over loopback (`scripts/companion-e2e`, see `docs/remote.md`).
- **`clubmate-push-relay`** (`cmd/clubmate-push-relay`, `deploy/push-relay`): a small server off the Mac that holds the APNs auth key. Each Mac's `remote` plugin sends its pushes there with its own token; the relay forwards them to APNs. Once a Mac registers its device tokens (`PUT /v1/devices`), it may push only to those. It also takes signed problem reports (`POST /v1/reports`), keeps them on disk for 30 days and serves them only to admin tokens. See `docs/push-relay.md`.

## Core (`internal/`)
| Package | Role |
|---|---|
| `rpc` | JSON-RPC server, built-in methods (primitives), command registry. Every command is flagged `agent` (visible to MCP/agents), `destructive` (needs in-app approval when an agent calls it), `hidden` (not in the palette), or `remote` (runnable by paired companion devices); an agent call to a command without `agent` gets unknown command. `scope: app` commands need no workspace. An agent caller (`agt_…`) must belong to the workspace it names, checked against `agents.sessions` (`caller_scope.go`). |
| `bus` | Persistent event bus on SQLite (`bus.db`). Topics, publish, subscribe with backfill, 64 KiB payload cap. Pruned on start and hourly (`retention.go`): 30 days or the newest 10 000 events, never below a stored cursor younger than 90 days (`docs/sdui-protocol.md` § Bus topics). Plugins and the app react to `workspace.updated`, `agents.changed`, `notify.posted`, … |
| `core` | The service behind the built-in methods: workspaces and folders, sessions, process trees and kills, process roots plugins register; publishes their bus events. |
| `workspace` | Workspaces (`clubmate.db`): folders, git repos, the selected worktree per folder, missing folders. |
| `session` | Session manager: creates, attaches, kills and removes `clubmate-session` hosts; owner plugin and workspace per session. |
| `proc` | Process tracker (reads processes through libproc, no `ps`): process trees per session and per root a plugin registered (agents), CPU/MEM history, orphan detection and kill. |
| `sdui` | Node catalog and validator (max 5000 nodes, depth 32), section and slot store, hosted sections. Invalid trees are rejected before they reach a client. |
| `plugin` | Manifest (`plugin.json`: sections, slots, commands, events), discovery, supervisor. |
| `kv` | Plugin storage (`plugins.db`). Each plugin also has a private state dir, `plugin-data/<id>`. |
| `cli` | The `clubmate`/`cm` commands: open, workspace, session, attach, process, command, agent, plugin, bus, notify, ui (sections, dump, event, snapshot, present, key), core, diagnostics, debug, mcp, version. |
| `sessionhost` | The PTY host inside `clubmate-session`: scrollback, terminal state kept for the next attach, bell, open-file limit. |
| `mcp` | `clubmate mcp`: exposes agent-visible commands as MCP tools, scoped to `$CLUBMATE_WORKSPACE_ID` (another `workspace` is refused; the core denies agent `session.read/send/kill` on a session of another workspace). |
| `notify` | `notify.post`: validates, rate-limits (per workspace and global) and publishes `notify.posted`. |
| `shellenv` | Resolves the login-shell environment once, so GUI-launched processes get your `PATH`. |
| `diagnostics` | `diagnostics.collect`: the zip behind Help → Collect Diagnostics. |
| `testutil` | Test harness: a core with the plugin supervisor, workspaces and test plugins (`plugins/echo`, `detach`). |
| `macui` | Mac UI tests (`make mac-ui-test`): each test drives its own test-mode instance of the Debug app on a scratch repo with `ui.key`/`ui.pointer` and asserts on snapshots and plugin commands; skipped without `CLUBMATE_TEST_APP`. See `docs/agents.md`. |
| `paths`, `wire`, `fswatch`, `schema`, `buildinfo` | State and runtime dirs, session wire format, file watching, JSON schemas, version info. |

The JSON-RPC transport is a fork of `github.com/sourcegraph/jsonrpc2` in `third_party/jsonrpc2` (a `replace` in `go.mod`, its own module, tested separately; changes in its `CLUBMATE.md`).

## Shared packages (`pkg/`)
| Package | Role |
|---|---|
| `pluginsdk` | The plugin side of the protocol: JSON-RPC over stdio, commands, events, slots, kv and `Host` calls into the core. |
| `sdui` | Node builders and deltas for plugin views. |
| `contract` | Types shared by core, CLI and plugins: workspaces, sessions, processes, events, errors. |
| `docext` | Which files the docs viewer opens (Markdown, mermaid, HTML artifacts) and their titles. |
| `docrender` | Markdown and mermaid to HTML (GFM, highlighted code, local images, links) for the docs viewer and backlog. |
| `apns` | APNs client (ES256 JWT), used by the push relay and `remote`. |
| `report` | Problem reports: Ed25519 request signing and verifying, bundle meta and entry checks, shared by `remote` and the push relay. |
| `id` | Prefixed random ids. |
| `pprofdump` | `debug.pprof`: CPU and other `runtime/pprof` profiles of the core and plugins. |

## Plugins (`plugins/`)
| Plugin | Does |
|---|---|
| `editor-nvim` | Runs the bundled nvim 0.12.6 with the Clubmate distribution (`NVIM_APPNAME=clubmate`/`clubmate-dev`, else the `nvim` in `PATH`) in a session, one per worktree. It drives nvim over RPC: open/quickfix/diffview/checktime/selection/unsaved, pushes the app's `theme.changed` into running nvims and offers the config import. Owns reviews: Agents' Review changes (`editor-nvim.diffview`) and the agent command `editor-nvim.review` open them through the bundled diffview fork's host API v4, whose review panel lives inside nvim; it publishes their progress on the `editor-nvim.review` bus topic and renders only the Send sheet natively. Hosts the Neovim section. |
| `editor-vscode` | Off by default (Settings → VS Code). Runs the VS Code server from the user's installed `code` CLI, after the user accepts its license terms, as one loopback child process for every workspace, and shows the current root's workbench as a `webview` in the VS Code section. See `docs/vscode-integration.md`. |
| `git` | Worktrees and branches behind the switcher in the window title, and a watch that moves folders off worktrees removed outside Clubmate. No section. |
| `search` | File index and fuzzy path search behind the agents composer's file picker. No section. |
| `terminals` | Terminal sessions. |
| `processes` | The Processes section, built on the core's process tracker; the core's own process tree (plugins and their children, not session hosts) it reads itself through `internal/proc`. |
| `agents` | ACP client that hosts Claude Code (`@agentclientprotocol/claude-agent-acp`, installed into its state dir). One process per (agent, workspace, worktree). Keeps a per-session JSONL log, takes a git checkpoint at the start and end of each turn (`refs/clubmate/cp/<session>/<turn>`, `<turn>.end`), exports Agent Trace records and session bundles, runs multi-repo tasks (an orchestrator with one child per repo: connect, Repos card and sheet, land, archive; `repos*.go`, `connect.go`, `archive*.go`, `cleanup*.go`), deletes closed session trees 30 days after their last activity (`retention.go`), and renders the Agents section. `api` holds its bus topics and ids for other plugins; `acp` the ACP client; `internal/claudectx` reads Claude Code's own session log for the context view; `internal/fakeagent` is the scripted agent tests and test-app use. |
| `backlog` | Per-workspace task list shared by the user and agents; renders the Backlog section. See `docs/backlog.md`. |
| `triggers` | Calls an agent-visible command when a bus event arrives; renders the Triggers section. See `docs/triggers.md`. |
| `docs` | Docs viewer: Markdown, mermaid and HTML artifacts rendered in a popup on the Mac and for the phone. See `docs/docs-viewer.md`. |
| `cron` | Calls agent-visible commands on a schedule (cron, every, one-shot), jobs per workspace, optionally owned by an agent session; renders the Cron section. See `docs/cron.md`. |
| `graphs` | Agent graphs: reads `.clubmate/graphs/*.json` and its built-in graphs (`builtin/`). A run starts one idle agent session per agent node and one trigger per edge between them; step nodes (a shell command or a Clubmate command, no agent) it runs itself, the exit code picking the edge, and it fires every edge into or out of a step (or with `maxFires`) itself. A run stops at its turn or step budget. A graph with steps runs only once trusted in that repo (`graphs.trust`, a hash of its steps, routing and limits); a shell step follows Claude Code's `Bash` rules (a repo's allow rules only in its main checkout, the only place Always allow is offered), else waits for `graphs.approve`. Renders the Graphs section. See `docs/agent-graphs.md`. |
| `voice` | Voice assistant and dictation: utterances (audio in, live partial text out), a Python speech recognizer (`asr/`: transcribe.cpp on ggml with Metal, Nemotron streaming by default) run through `uv` only while voice is on (off by default), context from where the user is, and one Voice agent session per workspace that answers agents as the user or runs commands, dangerous ones after the user confirms. Dictation skips the session: one Claude Haiku call (`claude -p`) tidies the text for the composer. No section. See `docs/voice.md`. |
| `remote` | Companion server: HTTP + SSE on the Mac's Tailscale addresses only, QR pairing, device tokens, proxy for `"remote": true` commands, APNs pushes through the push relay. No section. See `docs/remote.md`. |
| `internal/` | Shared helpers: `fuzzy`, `gitrepo`, `fileindex` (file lists for search and docs), `agentcall` (agent sessions and command targets), `automation` (what cron, triggers and graphs share: actions, runs, the registry), `editorroot` (worktree roots and HEAD watches shared by editor-nvim and editor-vscode), `claudecli` (runs `claude -p`: Haiku summaries, voice dictation cleanup), `parallel` (bounded fan-out), `shortcuttest` (test helper: a view's shortcuts are declared in `plugin.json`). |

## UI model (SDUI)
- **Sections:** plugins declare them in `plugin.json`, with an icon, order and slots (`<section>.sidebar`, `<section>.main`, plus extra slots a section declares). The rail shows sections in the user's order. A section may name another as its `host`, or set `rail: false` (Docs: only its `popup` slot shows); it then has no rail icon of its own.
- **Rendering:** a plugin renders a tree of nodes (`pkg/sdui` builders) into a slot. The core validates and stores it, then pushes it to every client watching that workspace.
- **Events:** the app sends user events back as `ui.event {slot, node, action, value}`. Typed text and other client-only state stay in ClubmateKit (e.g. composer drafts).
- **Terminals:** a terminal is a `surface` node. The app hosts a libghostty surface (`client/GhosttyKit.xcframework`) attached to the session's socket. Ghostty's config is Clubmate's own: `theme = dark:Clubmate,light:Clubmate Light`, then `<state>/ghostty.conf` (Settings → Terminals writes font family and size there; Import copies a system Ghostty config in once, colours commented out), then the app's forced keybinds; `~/.config/ghostty` and Ghostty's Application Support config are never read, like `~/.config/nvim`.
- **Reference:** the contract for every node, prop and event is in `docs/sdui-protocol.md`.

## Agents and integrations
- **Agents in terminals:** Claude Code in a Clubmate terminal can drive Clubmate through MCP (`clubmate mcp`), seeing only agent-visible commands. Destructive commands ask for approval in the app. See `docs/agents.md`.
- **Agents section:** ACP over stdio (`plugins/agents/acp`). The Agents section never writes `~/.claude*` or `.mcp.json`; only Connect Claude Code in Settings runs `claude mcp add --scope user` when the user clicks it. See `.claude/skills/agents-acp/SKILL.md`.
- **Notifications:** `cm notify`, `notify.post` and the app hub (banners, inbox, badges). See `docs/notifications.md`.
- **nvim:** the bundled Neovim with Clubmate's own pinned distribution plus a user layer; `~/.config/nvim` is never read or written. The app publishes its Ghostty theme as `theme.changed`; editor-nvim persists it to `theme.json` and applies it live. Reviews (Agents' Review changes, agents' `editor-nvim.review`) open in nvim through the diffview fork's host API v4, panel included; only the Send sheet is native. See `docs/nvim-integration.md`.
- **Problem reports:** `report.send` (MCP `report_send`, core, `internal/rpc/methods_report.go`) builds a diagnostics bundle, shows it in the app's report sheet (`ui.report`) and, on Send, hands the zip to `remote.report-upload`, which signs it with the install's Ed25519 key (`pkg/report`) and posts it to the relay; the dev Mac pulls reports with `remote.reports-fetch`. See `docs/reports.md`.
- **Browser:** agents browse the web in a WebKit (`WKWebView`) browser the app hosts in a hidden window. The `browser.*` commands (MCP `browser_*`) are built into the core (`internal/rpc/methods_browser.go`) and forward each call to the app (`browser.call`), so the app must be running; `browser.handoff` asks the user to solve a captcha or sign in. The core refuses local and private network addresses before and after each call (`internal/rpc/browser_guard.go`); the app keeps one `WKWebsiteDataStore` per workspace. See `docs/browser.md`.

## State on disk
- **State dir:** `~/Library/Application Support/clubmate`, or `clubmate-dev` for dev builds. Holds `clubmate.db`, `bus.db`, `plugins.db`, `plugin-data/<id>/`, `sessions/`, `diagnostics/`, `core.log`, `plugins/<id>.log` and `notifications.json`. `core.log` and `plugins/<id>.log` are rotated when they are opened (core start, plugin start) and over 10 MiB: the old file becomes `<name>.1`, replacing an older `.1`. While the core runs it checks them every minute and, past 10 MiB, copies the file to `<name>.1` and truncates it in place (the writers append, so their open fds keep working; lines written during the copy are lost).
- **Runtime dir:** `/tmp/clubmate-<uid>`, or `/tmp/clubmate-dev-<uid>`. Holds the core socket, session sockets and nvim sockets.
- **Overrides:** `CLUBMATE_STATE_DIR` and `CLUBMATE_RUNTIME_DIR`.

## Build, test, release
- **Build:**
  - `make build` builds the Go binaries.
  - `make app` builds the Debug "Clubmate Dev.app" (`com.clubmate.app.dev`). It builds GhosttyKit from source on first run (`scripts/build-ghostty.sh`).
  - `scripts/vendor-tools.sh` fetches the bundled ripgrep, Neovim and the tree-sitter CLI pinned in `scripts/tools.lock`.
  - `scripts/vendor-nvim-distro.sh` builds `Contents/Resources/nvim-distro`: the plugins pinned in `scripts/nvim-distro.lock` (checksummed tarballs), treesitter parsers compiled with the vendored tree-sitter CLI and clang, blink.cmp's prebuilt fuzzy dylib, and `tools/nrev` built from the diffview fork. `bundle-app.sh` signs every added Mach-O, then runs `TestBundledAppSmoke` against the built app and fails the build if nvim starts with any error, missing plugin or parser, network call or write into the bundle.
- **Test:**
  - `go test -race ./...`;
  - `make lint`;
  - `make kit-test`;
  - `make mac-ui-test` (`internal/macui` against the Debug app);
  - `make companion-e2e` (the Companion against a real core);
  - `make nvim-test` (vendors the distribution into `build/resources` and runs `nvim-distro/distro_test.go` against the bundled nvim);
  - end-to-end runs use the dev app with scratch `XDG_*` dirs; the records are in the `clubmate-notes` repo under `verification/`;
  - agents look at the app through `scripts/test-app.sh`: an invisible test-mode instance with its own core that draws its windows to png plus a flagged layout dump (`cm ui snapshot`, MCP `testapp.call`); see `docs/agents.md`.
- **Nightly:** the repo's `nightly` graph (`.clubmate/graphs/nightly.json`, run by the "Nightly tidy" cron job) runs the tidy, `make test`, the Companion and Mac UI tests (`scripts/graph/`), pushes main and ships the next patch; `design-check` is the nightly design check. See `docs/agent-graphs.md` § nightly.
- **Release:**
  - Pushing a `vX.Y.Z` tag runs the release workflow (`.github/workflows/release.yml`) in the `release` environment, which holds the signing secrets and allows only `v*` tags; it fails unless the tagged commit is on main.
  - It builds, signs with Developer ID (entitlements in `scripts/release/`), builds a DMG, notarizes it and makes a Sparkle appcast, then waits for the CI run on that commit to pass (`scripts/release/wait-ci.sh`) before it publishes the GitHub release.
  - The build number is the commit count, so updates always move forward.

## Conventions
- **Repo rules:** see `CLAUDE.md`. Every shortcut change updates `docs/shortcuts.md` in the same commit.
- **Go:** imports in 4 groups (stdlib, third-party, other module, this module).
- **Code:** no comments in code.
- **Tests:** minimal, one per behaviour.
- **Swift:** logic lives in ClubmateKit with tests; views stay thin.
