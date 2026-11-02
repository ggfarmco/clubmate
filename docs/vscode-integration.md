# VS Code integration

Settings → Code → Editor chooses what the Code section shows: Neovim (default) or VS Code. The choice is app-wide and takes effect at once in every open workspace; it is stored in editor-nvim's kv (`editor-nvim.settings`, `editor-nvim.settings-set`, published as `editor-nvim.settings-changed`).

- Neovim → VS Code is refused while any Neovim has unsaved buffers (the page names the files); otherwise every Neovim stops.
- VS Code → Neovim stops the VS Code server. VS Code's hot exit keeps unsaved edits for the next time.

## What it runs

Clubmate bundles no VS Code. It uses the user's installed `code` CLI, looked up on `PATH`, then at `/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code`. Without it the Code section reads "VS Code not found — install it or switch the editor to Neovim in Settings → Code".

- **Download, once per server version.** `code serve-web --host 127.0.0.1 --port 0 --connection-token-file <f> --accept-server-license-terms --cli-data-dir <cli> --server-data-dir <server>` runs only to fetch the VS Code server (about 600 MB unpacked). editor-nvim loads its page once, which starts the download, waits for `<cli>/serve-web/<commit>/bin/code-server` and stops it.
- **The server.** editor-nvim then runs the newest downloaded `bin/code-server` itself, as a plain child process (not a terminal session): `--host 127.0.0.1 --port <p> --connection-token-file <f> --accept-server-license-terms --disable-workspace-trust --server-data-dir <server>`. One server serves every workspace; each root is its own `?folder=`.
- **Port.** The first start picks a free loopback port and stores it (`vscode.port`); later starts reuse it, because the web view keeps the workbench's state (open editors, layout, hot-exit backups) per origin. Only when that port is taken does it pick and store a new one, which drops that state.
- **Token.** A fresh random token per start, in a 0600 file passed with `--connection-token-file`, never on the command line. The server listens on `127.0.0.1` only.
- **Lifetime.** It starts the first time a workspace shows VS Code, and stops when the editor goes back to Neovim or the plugin shuts down. Its pid is kept in `server.pid`: when the plugin was killed and left a server running, the next start stops that one (only if its command line names this server data dir) so the stored port stays free. If the server exits it is restarted once; a second exit within 30 s shows the error with Retry.
- **Settings.** Before each start, and on every theme change, editor-nvim merges into `<server>/data/Machine/settings.json` (other keys are kept): `workbench.colorTheme` Default Dark Modern or Default Light Modern from the app theme's `dark` flag, `chat.disableAIFeatures: true` and `workbench.secondarySideBar.defaultVisibility: "hidden"` (no Copilot chat; Clubmate's chat lives there), and `task.allowAutomaticTasks: "off"`: workspace trust is off (`--disable-workspace-trust`), so a repo's `tasks.json` must not run on folder open. A theme change also bumps the webview's `rev`, which reloads the workbench.

## Where data lives

Under the plugin state dir, `plugin-data/editor-nvim/vscode/`:

| Path | What |
|---|---|
| `cli/` | `--cli-data-dir`: downloaded servers under `serve-web/<commit>/` (each new server version is a new download; old ones stay until removed) |
| `server/` | `--server-data-dir`: `data/Machine/settings.json`, user data and extensions installed from the Extensions view |
| `token`, `server.pid` | the current connection token and server pid |

Nothing goes to `~/.vscode` or the desktop VS Code's settings and extensions.

## Consent and download

The VS Code server is under Microsoft's VS Code Server License Terms. The first time the Code section shows VS Code it explains that ("VS Code needs Microsoft's VS Code Server: about 600 MB, downloaded once per version, under the VS Code Server License Terms."), links the [terms](https://aka.ms/vscode-server-license) and offers Accept & Download. Only that button sets `vscodeAccepted`, after a confirmation. The button has `confirm`, so the core refuses an agent's `ui.event` on it, and editor-nvim takes `vscode-accept` only from the main slot of a workspace that shows the consent; `editor-nvim.settings-set` cannot set it. An agent cannot accept for the user. Clubmate passes `--accept-server-license-terms` only after it. While the server downloads, every workspace showing VS Code reads "Downloading the VS Code server…"; a failure shows its message and Retry. The failure stays until Retry, a restart or a change of the editor setting.

## In the Code section

The main slot is a `webview` node (key `vscode`) at `http://127.0.0.1:<port>/?tkn=<token>&folder=<root>`, for the current root: the selected worktree of the first folder, the same root Neovim uses. The Mac keeps the page alive while the section is hidden; a root change navigates it to the new folder.

- `editor.open` (agents, `clubmate open`, tool cards) switches the worktree when the path is in another one, then loads the workbench with `&payload=[["openFile","vscode-remote://127.0.0.1:<port><path>:<line>:<col>"],["gotoLineMode","true"]]` and focuses it. This reloads the workbench. Later re-renders of that root keep the same URL, so they don't reload it again; a root change drops it.
- The status slot reads `VS Code`, the branch and `⌘B chat`.

## Keys

While VS Code has focus, ⌘W, ⌘N, ⌘O, ⌘P and ⇧⌘P go to it; every other app menu key stays with Clubmate, so VS Code's ⌘K chords, ⌘B sidebar and ⌘J panel are shadowed (⌃` opens its panel). See [shortcuts](shortcuts.md#vs-code-code-section).

## What still opens Neovim

- `editor.quickfix` and "Review changes" / "Full diff" (`editor-nvim.diffview`, nrev review comments) start that root's Neovim and show it in the Code section, with Back to VS Code in the status bar. That button, quitting that Neovim, a root change or the next `editor.open` shows VS Code again. The Neovim keeps running in the background until its root goes away.
- `editor-nvim.unsaved` and the worktree switch guard see only those Neovims' buffers; VS Code's hot exit keeps its own unsaved edits.
- `editor-nvim.selection` (the chat's attach selection) and `editor.reveal` fail with "Not available with VS Code yet" while VS Code is shown. `editor-nvim.terminal` opens in the root. `editor-nvim.restart` restarts the VS Code server.

The phone never shows the Code section, so it shows no VS Code either.
