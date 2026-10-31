# Architecture

Clubmate is a native macOS IDE built around your own Neovim, real terminals and coding agents. A Go **core** owns all state and processes. Features are **plugins** that render UI as data (SDUI). A thin **SwiftUI app** draws that UI and hosts terminal surfaces.

```
 Clubmate.app (SwiftUI)          cm / clubmate CLI         agents (MCP)
  ClubmateKit · Ghostty surfaces        │                      │
        │  JSON-RPC (unix socket)       │                      │ clubmate mcp
        └───────────────┬───────────────┴──────────────────────┘
                        ▼
                 clubmate-core (Go, one per user)
   rpc · command registry · bus (SQLite) · sdui store · projects
   sessions · process tracker · plugin supervisor · notify · mcp
        │ JSON-RPC over stdio                     │ spawns, attaches
        ▼                                         ▼
  plugins (one process each)               clubmate-session (one per PTY)
  editor-nvim · git · search                terminals, nvim TUI; survives
  terminals · processes · agents · cron     app and core restarts
```

## Processes
- **`clubmate-core`** (`cmd/clubmate-core`): a single daemon per user, guarded by a lock in the state dir. It listens on a unix socket in the runtime dir and outlives the app. The app starts it on demand (`CoreLauncher`), as does the CLI.
- **`clubmate-session`** (`cmd/clubmate-session`, `internal/sessionhost`): one PTY host per terminal session, in its own session (`setsid`). It keeps a scrollback ring, so a terminal or the editor's nvim survives an app or core restart and is reattached. While no client is attached, it answers a program's status query itself. Before it starts the program it raises the soft open-file limit (`RLIMIT_NOFILE`) to 10240, capped by the hard limit; the bundled tools dir (`Contents/Resources/tools`: `rg`, `nrev`) is appended to `PATH` for nvim, terminals and agent sessions.
- **Plugins** (`plugins/*`): separate Go binaries started by the core's supervisor (`internal/plugin`). They speak JSON-RPC over stdin/stdout through `pkg/pluginsdk`, and are restarted with backoff when they crash.
- **`clubmate` / `cm`** (`cmd/clubmate`, `internal/cli`): CLI over the same socket. It opens folders (`cm <folder>`), runs commands, reads the bus, posts notifications (`cm notify`), collects diagnostics and serves MCP (`clubmate mcp`).
- **Clubmate.app** (`client/`): `ClubmateKit` holds the logic and is tested with `make kit-test`. The `Clubmate` app target holds views, windows, Ghostty surfaces, Sparkle updates and UserNotifications.
- **Companion** (`client/Companion`, `client/CompanionKit`, `client/companion.yml`): iPhone/iPad app that drives agents on paired Macs over Tailscale through the `remote` plugin. `CompanionKit` (Foundation only) holds the logic and is tested with `make companion-kit-test`; `make companion` builds the app for the simulator.

## Core (`internal/`)
| Package | Role |
|---|---|
| `rpc` | JSON-RPC server, built-in methods (primitives), command registry. Every command is flagged `agent` (visible to MCP/agents), `destructive` (needs in-app approval when an agent calls it), `hidden` (not in the palette), `remote` (runnable by paired companion devices) or `userOnly`; `scope: app` commands need no workspace. |
| `bus` | Persistent event bus on SQLite (`bus.db`). Topics, publish, subscribe with backfill, 64 KiB payload cap. Plugins and the app react to `project.updated`, `agents.changed`, `notify.posted`, … |
| `project` | Workspaces (`clubmate.db`): folders, git repos, the selected worktree per folder, missing folders. |
| `session` | Session manager: creates, attaches, kills and removes `clubmate-session` hosts; owner plugin and project per session. |
| `proc` | Process tracker: process trees per session and per root a plugin registered (agents), CPU/MEM history, orphan detection and kill. |
| `sdui` | Node catalog and validator (max 5000 nodes, depth 32), section and slot store, hosted sections. Invalid trees are rejected before they reach a client. |
| `plugin` | Manifest (`plugin.json`: sections, slots, commands, events), discovery, supervisor. |
| `kv` | Plugin storage (`plugins.db`). Each plugin also has a private state dir, `plugin-data/<id>`. |
| `mcp` | `clubmate mcp`: exposes agent-visible commands as MCP tools, scoped to `$CLUBMATE_PROJECT_ID`. |
| `notify` | `notify.post`: validates, rate-limits (per workspace and global) and publishes `notify.posted`. |
| `shellenv` | Resolves the login-shell environment once, so GUI-launched processes get your `PATH`. |
| `diagnostics` | `diagnostics.collect`: the zip behind Help → Collect Diagnostics. |
| `paths`, `id`, `wire`, `fswatch`, `schema`, `buildinfo` | State and runtime dirs, ids, session wire format, file watching, JSON schemas, version info. |

## Plugins (`plugins/`)
| Plugin | Does |
|---|---|
| `editor-nvim` | Runs the bundled nvim 0.12.6 with the Clubmate distribution (`NVIM_APPNAME=clubmate`/`clubmate-dev`, else the `nvim` in `PATH`) in a session, one per worktree. It drives nvim over RPC: open/quickfix/diffview/checktime/selection/unsaved, pushes the app's `theme.changed` into running nvims and offers the config import. Hosts the Code section. |
| `git` | Worktrees and branches behind the switcher in the window title, and a watch that moves folders off worktrees removed outside Clubmate. No section. |
| `search` | File index and fuzzy path search behind the agents composer's file picker. No section. |
| `terminals` | Terminal sessions. |
| `processes` | The Processes section, built on the core's process tracker. |
| `agents` | ACP client that hosts Claude Code (`@agentclientprotocol/claude-agent-acp`, installed into its state dir). One process per (agent, workspace, worktree). Keeps a per-session JSONL log, takes a git checkpoint at the start and end of each turn (`refs/clubmate/cp/<session>/<turn>`, `<turn>.end`), exports Agent Trace records and session bundles, and renders the Agents section. |
| `cron` | Calls agent-visible commands on a schedule (cron, every, one-shot), jobs per workspace, optionally owned by an agent session; renders the Cron section. See `docs/cron.md`. |
| `graphs` | Agent graphs: reads `.clubmate/graphs/*.json`, starts a run as one idle agent session per node and one trigger per edge, stops it at its turn budget; renders the Graphs section. See `docs/agent-graphs.md`. |
| `remote` | Companion server: HTTP + SSE on the Mac's Tailscale addresses only, QR pairing, device tokens, proxy for `"remote": true` commands. No section. See `docs/remote.md`. |
| `internal/` | Shared helpers: `fuzzy`, `gitrepo`. |

## UI model (SDUI)
- **Sections:** plugins declare them in `plugin.json`, with an icon, order and slots (`<section>.sidebar`, `<section>.main`, plus extra slots a section declares). The rail shows sections in the user's order. A section may name another as its `host`; it then has no rail icon of its own.
- **Rendering:** a plugin renders a tree of nodes (`pkg/sdui` builders) into a slot. The core validates and stores it, then pushes it to every client watching that workspace.
- **Events:** the app sends user events back as `ui.event {slot, node, action, value}`. Typed text and other client-only state stay in ClubmateKit (e.g. composer drafts).
- **Terminals:** a terminal is a `surface` node. The app hosts a libghostty surface (`client/GhosttyKit.xcframework`) attached to the session's socket.
- **Reference:** the contract for every node, prop and event is in `docs/sdui-protocol.md`.

## Agents and integrations
- **Agents in terminals:** Claude Code in a Clubmate terminal can drive Clubmate through MCP (`clubmate mcp`), seeing only agent-visible commands. Destructive commands ask for approval in the app. See `docs/agents.md`.
- **Agents section:** ACP over stdio (`plugins/agents/acp`). Clubmate never writes `~/.claude*` or `.mcp.json`. See `.claude/skills/agents-acp/SKILL.md`.
- **Notifications:** `cm notify`, `notify.post` and the app hub (banners, inbox, badges). See `docs/notifications.md`.
- **nvim:** the bundled Neovim with Clubmate's own pinned distribution plus a user layer; `~/.config/nvim` is never read or written. The app publishes its Ghostty theme as `theme.changed`; editor-nvim persists it to `theme.json` and applies it live. Agents' Review changes opens diffview. See `docs/nvim-integration.md`.

## State on disk
- **State dir:** `~/Library/Application Support/clubmate`, or `clubmate-dev` for dev builds. Holds `clubmate.db`, `bus.db`, `plugins.db`, `plugin-data/<id>/`, `sessions/`, `diagnostics/`, `core.log`, `plugins/<id>.log` and `notifications.json`.
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
  - `make nvim-test` (vendors the distribution into `build/resources` and runs `nvim-distro/distro_test.go` against the bundled nvim);
  - end-to-end runs use the dev app with scratch `XDG_*` dirs; the records are in the `clubmate-notes` repo under `verification/`;
  - agents look at the app through `scripts/test-app.sh`: an invisible test-mode instance with its own core that draws its windows to png plus a flagged layout dump (`cm ui snapshot`, MCP `testapp.call`); see `docs/agents.md`.
- **Release:**
  - Pushing a `vX.Y.Z` tag runs the release workflow, which waits for approval of the `release` environment.
  - It signs with Developer ID (entitlements in `scripts/release/`), notarizes, builds a DMG and a Sparkle appcast, and publishes a GitHub release.
  - The build number is the commit count, so updates always move forward.

## Conventions
- **Repo rules:** see `CLAUDE.md`. Every shortcut change updates `docs/shortcuts.md` in the same commit.
- **Go:** imports in 4 groups (stdlib, third-party, other module, this module).
- **Code:** no comments in code.
- **Tests:** minimal, one per behaviour.
- **Swift:** logic lives in ClubmateKit with tests; views stay thin.
