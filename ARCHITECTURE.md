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
  editor-nvim · files · git · search        terminals, nvim TUI; survives
  terminals · processes · agents            app and core restarts
```

## Processes
- **`clubmate-core`** (`cmd/clubmate-core`): a single daemon per user, guarded by a lock in the state dir. It listens on a unix socket in the runtime dir and outlives the app. The app starts it on demand (`CoreLauncher`), as does the CLI.
- **`clubmate-session`** (`cmd/clubmate-session`, `internal/sessionhost`): one PTY host per terminal session, in its own session (`setsid`). It keeps a scrollback ring, so a terminal or the editor's nvim survives an app or core restart and is reattached. While no client is attached, it answers a program's status query itself.
- **Plugins** (`plugins/*`): separate Go binaries started by the core's supervisor (`internal/plugin`). They speak JSON-RPC over stdin/stdout through `pkg/pluginsdk`, and are restarted with backoff when they crash.
- **`clubmate` / `cm`** (`cmd/clubmate`, `internal/cli`): CLI over the same socket. It opens folders (`cm <folder>`), runs commands, reads the bus, posts notifications (`cm notify`), collects diagnostics and serves MCP (`clubmate mcp`).
- **Clubmate.app** (`client/`): `ClubmateKit` holds the logic and is tested with `make kit-test`. The `Clubmate` app target holds views, windows, Ghostty surfaces, Sparkle updates and UserNotifications.
- **Companion** (`client/Companion`, `client/CompanionKit`, `client/companion.yml`): iPhone/iPad app that drives agents on paired Macs over Tailscale through the `remote` plugin. `CompanionKit` (Foundation only) holds the logic and is tested with `make companion-kit-test`; `make companion` builds the app for the simulator.

## Core (`internal/`)
| Package | Role |
|---|---|
| `rpc` | JSON-RPC server, built-in methods (primitives), command registry. Every command is flagged `agent` (visible to MCP/agents), `destructive` (needs in-app approval when an agent calls it), `hidden` (not in the palette), `remote` (runnable by paired companion devices) or `userOnly`; `scope: app` commands need no workspace. |
| `bus` | Persistent event bus on SQLite (`bus.db`). Topics, publish, subscribe with backfill, 64 KiB payload cap. Plugins and the app react to `project.updated`, `review.changed`, `agents.changed`, `notify.posted`, … |
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
| `editor-nvim` | Runs your nvim (bundled 0.12.6 by default, system nvim selectable) in a session, one per worktree. It drives nvim over RPC: open/diff/checktime/selection/unsaved, plus a review bridge to the diffview fork (`diffview.clubmate`). Hosts the Code section. |
| `files` | File tree with neo-tree keys and focus/hoist. |
| `git` | Status, commit and stage, worktrees and branches (switcher), and review (base ◂ head, filters, reviewed marks, checkpoint bases `commit:<sha>`). Shown as the Git tab of the Code section (`host: editor-nvim`). |
| `search` | Find in files with the bundled ripgrep. |
| `terminals` | Terminal sessions. |
| `processes` | The Processes section, built on the core's process tracker. |
| `agents` | ACP client that hosts Claude Code (`@agentclientprotocol/claude-agent-acp`, installed into its state dir). One process per (agent, workspace, worktree). Keeps a per-session JSONL log, takes a git checkpoint per turn (`refs/clubmate/cp/<session>/<turn>`), and renders the Agents section. |
| `remote` | Companion server: HTTP + SSE on the Mac's Tailscale addresses only, QR pairing, device tokens, proxy for `"remote": true` commands. No section. See `docs/remote.md`. |
| `internal/` | Shared helpers: `fuzzy`, `gitrepo`. |

## UI model (SDUI)
- **Sections:** plugins declare them in `plugin.json`, with an icon, order and slots (`<section>.sidebar`, `<section>.main`, plus extra slots like `git.review`). The rail shows sections in the user's order. A hosted section appears as a tab of another section.
- **Rendering:** a plugin renders a tree of nodes (`pkg/sdui` builders) into a slot. The core validates and stores it, then pushes it to every client watching that workspace.
- **Events:** the app sends user events back as `ui.event {slot, node, action, value}`. Typed text and other client-only state stay in ClubmateKit (e.g. composer drafts, sidebar tabs).
- **Terminals:** a terminal is a `surface` node. The app hosts a libghostty surface (`client/GhosttyKit.xcframework`) attached to the session's socket.
- **Reference:** the contract for every node, prop and event is in `docs/sdui-protocol.md`.

## Agents and integrations
- **Agents in terminals:** Claude Code in a Clubmate terminal can drive Clubmate through MCP (`clubmate mcp`), seeing only agent-visible commands. Destructive commands ask for approval in the app. See `docs/agents.md`.
- **Agents section:** ACP over stdio (`plugins/agents/acp`). Clubmate never writes `~/.claude*` or `.mcp.json`. See `.claude/skills/agents-acp/SKILL.md`.
- **Notifications:** `cm notify`, `notify.post` and the app hub (banners, inbox, badges). See `docs/notifications.md`.
- **nvim:** the `clubmate` Lua module and navigation keys. Your config is never written. See `docs/nvim-integration.md`.

## State on disk
- **State dir:** `~/Library/Application Support/clubmate`, or `clubmate-dev` for dev builds. Holds `clubmate.db`, `bus.db`, `plugins.db`, `plugin-data/<id>/`, `sessions/`, `diagnostics/`, `core.log`, `plugins/<id>.log` and `notifications.json`.
- **Runtime dir:** `/tmp/clubmate-<uid>`, or `/tmp/clubmate-dev-<uid>`. Holds the core socket, session sockets and nvim sockets.
- **Overrides:** `CLUBMATE_STATE_DIR` and `CLUBMATE_RUNTIME_DIR`.

## Build, test, release
- **Build:**
  - `make build` builds the Go binaries.
  - `make app` builds the Debug "Clubmate Dev.app" (`com.clubmate.app.dev`). It builds GhosttyKit from source on first run (`scripts/build-ghostty.sh`).
  - `scripts/vendor-tools.sh` fetches the bundled ripgrep and Neovim pinned in `scripts/tools.lock`.
- **Test:**
  - `go test -race ./...`;
  - `make lint`;
  - `make kit-test`;
  - end-to-end runs use the dev app with scratch `XDG_*` dirs; the records are in `docs/verification/`.
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
