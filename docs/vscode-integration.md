# VS Code integration

VS Code is its own plugin, `editor-vscode`, with its own VS Code section in the rail, next to Neovim (`editor-nvim`). It is off by default: turn it on in Settings → VS Code. Any plugin can be turned off there, Neovim too. When both editors are on, Settings → General → Default editor picks which one `editor.open` (agents, `clubmate open`, tool cards) uses; the core resolves the capability, the editors never decide between themselves.

Both editors can run at once: VS Code's server and each worktree's Neovim are separate, and neither stops the other.

## What it runs

Clubmate bundles no VS Code. It uses the user's installed `code` CLI, looked up on `PATH`, then at `/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code`. Without it the VS Code section reads "VS Code not found — install it or turn VS Code off in Settings → VS Code".

- **Download, once per server version.** `code serve-web --host 127.0.0.1 --port 0 --connection-token-file <f> --accept-server-license-terms --cli-data-dir <cli> --server-data-dir <server>` runs only to fetch the VS Code server (about 600 MB unpacked). editor-vscode loads its page once, which starts the download, waits for `<cli>/serve-web/<commit>/bin/code-server` and stops it.
- **The server.** editor-vscode then runs the newest downloaded `bin/code-server` itself, as a plain child process (not a terminal session): `--host 127.0.0.1 --port <p> --connection-token-file <f> --accept-server-license-terms --disable-workspace-trust --server-data-dir <server>`. One server serves every workspace; each root is its own `?folder=`.
- **Port.** The first start picks a free loopback port and stores it (`vscode.port`); later starts reuse it, because the web view keeps the workbench's state (open editors, layout, hot-exit backups) per origin. Only when that port is taken does it pick and store a new one, which drops that state.
- **Token.** A fresh random token per start, in a 0600 file passed with `--connection-token-file`, never on the command line. The server listens on `127.0.0.1` only.
- **Lifetime.** It starts once the plugin runs and a workspace is open in a window, and stops when the plugin shuts down (VS Code turned off, the core quitting). Its pid is kept in `server.pid`: when the plugin was killed and left a server running, the next start stops that one (only if its command line names this server data dir) so the stored port stays free. If the server exits it is restarted once; a second exit within 30 s shows the error with Retry.
- **Settings.** Before each start, and on every theme change, editor-vscode merges into `<server>/data/Machine/settings.json` (other keys are kept): `workbench.colorTheme` Default Dark Modern or Default Light Modern from the app theme's `dark` flag (editor-vscode follows `theme.changed` itself and keeps the last flag in its kv as `theme.dark`), `chat.disableAIFeatures: true` and `workbench.secondarySideBar.defaultVisibility: "hidden"` (no Copilot chat; Clubmate's chat lives there), and `task.allowAutomaticTasks: "off"`: workspace trust is off (`--disable-workspace-trust`), so a repo's `tasks.json` must not run on folder open. A theme change also bumps the webview's `rev`, which reloads the workbench.

## Where data lives

Under the plugin state dir, `plugin-data/editor-vscode/vscode/`:

| Path | What |
|---|---|
| `cli/` | `--cli-data-dir`: downloaded servers under `serve-web/<commit>/` (each new server version is a new download; old ones stay until removed) |
| `server/` | `--server-data-dir`: `data/Machine/settings.json`, user data and extensions installed from the Extensions view |
| `token`, `server.pid` | the current connection token and server pid |

Nothing goes to `~/.vscode` or the desktop VS Code's settings and extensions.

Before the split this lived under `plugin-data/editor-nvim/vscode/`. On start, editor-vscode moves that dir to its own when it has none yet, so downloaded servers, extensions and settings are kept.

## Consent and download

The VS Code server is under Microsoft's VS Code Server License Terms. The first time the VS Code section shows it explains that ("VS Code needs Microsoft's VS Code Server: about 600 MB, downloaded once per version, under the VS Code Server License Terms."), links the [terms](https://aka.ms/vscode-server-license) and offers Accept & Download. Only that button sets the consent, stored as `{"accepted": true}` under `settings` in editor-vscode's kv, after a confirmation; `editor-vscode.settings` reads it (`{accepted}`) and nothing else sets it. The button has `confirm`, so the core refuses an agent's `ui.event` on it, and editor-vscode takes `vscode-accept` only from its main slot of a workspace it shows. Users who accepted before the split keep it: the core copies `vscodeAccepted` from editor-nvim's old `settings` key. An agent cannot accept for the user. Clubmate passes `--accept-server-license-terms` only after it. While the server downloads, every workspace showing VS Code reads "Downloading the VS Code server…"; a failure shows its message and Retry. The failure stays until Retry or a restart (`editor-vscode.restart`).

## In the VS Code section

The main slot `editor-vscode.main` is a `webview` node (key `vscode`) at `http://127.0.0.1:<port>/?tkn=<token>&folder=<root>`, for the current root: the selected worktree of the first folder that exists, else home, chosen by the same rules as Neovim's (shared in `plugins/internal/editorroot`). The Mac keeps the page alive while the section is hidden; a root change navigates it to the new folder.

- `editor-vscode.open` (`editor.open` when VS Code is the default editor) switches the worktree when the path is in another one, then loads the workbench with `&payload=[["openFile","vscode-remote://127.0.0.1:<port><path>:<line>:<col>"],["gotoLineMode","true"]]`. This reloads the workbench. Later re-renders of that root keep the same URL, so they don't reload it again; a root change drops it. It focuses the VS Code section when it switched the worktree or `focus: true` is passed (agents' tool cards do), like Neovim's open.
- The status slot `editor-vscode.status` reads `VS Code` and the branch. The agent chat panel (⌘B in Neovim) is not in the VS Code section.
- `editor-vscode.terminal` opens a terminal in the root; `editor-vscode.restart` restarts the server and clears a shown failure.
- Quickfix, Review changes / Full diff (diffview, nrev comments), the chat's attach selection and Reveal in Finder are Neovim's and open in the Neovim section. `editor-nvim.unsaved` and the worktree switch guard see only Neovim's buffers; VS Code's hot exit keeps its own unsaved edits.

## Keys

While VS Code has focus, ⌘W, ⌘N, ⌘O, ⌘P and ⇧⌘P go to it; every other app menu key stays with Clubmate, so VS Code's ⌘K chords, ⌘B sidebar and ⌘J panel are shadowed (⌃` opens its panel). See [shortcuts](shortcuts.md#vs-code-vs-code-section).

The phone never shows the VS Code section.
