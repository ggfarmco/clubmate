# VS Code integration

VS Code is its own plugin, `editor-vscode`, with its own VS Code section in the rail, next to Neovim (`editor-nvim`). It is off by default: turn it on in Settings → VS Code. Any plugin can be turned off there, Neovim too. When both editors are on, Settings → General → Default editor picks which one `editor.open` (agents, `clubmate open`, tool cards) uses; the core resolves the capability, the editors never decide between themselves.

Both editors can run at once: VS Code's server and each worktree's Neovim are separate, and neither stops the other.

## First launch

A fresh install asks once, on the Mac, which editor to use: a sheet over the first main window, "Choose your editor", Neovim (selected) or VS Code. Neovim changes nothing. VS Code turns `editor-vscode` on and makes it the `editor.open` default, then the sheet shows "Set up VS Code" with editor-vscode's settings slot: the licence and Accept & Download (still a user click with its confirmation; an agent can't press it), the download, and Import from VS Code once the server is there, or "VS Code isn't installed on this Mac" with no import. Done closes it; a download or import goes on. Keys: ↑/↓ choose, ↩ Continue, Esc keeps Neovim; ↩ or Esc closes the VS Code step.

The core decides who sees it, once, and keeps the answer in its kv (`core` / `onboarding`, `pending` or `done`): `pending` when the state dir has no workspace at all at core start (every launched install has at least the home workspace) and `editor-vscode` is not already on; else `done`. Choosing marks it `done`; quitting before choosing shows it again next launch. `onboarding.state` → `{show}` and `onboarding.finish {editor: "editor-nvim" | "editor-vscode"}` (not for plugin callers) are all the client uses; it never decides itself. The phone has no onboarding. Test instances (`CLUBMATE_TEST_MODE=1`) decide `done` unless `CLUBMATE_ONBOARDING=1` (`CLUBMATE_ONBOARDING=1 scripts/test-app.sh start`); `cm -- ui present onboarding` / `onboarding-vscode` open either step there.

## What it runs

Clubmate bundles no VS Code. It uses the user's installed `code` CLI, looked up on `PATH`, then at `/Applications/Visual Studio Code.app/Contents/Resources/app/bin/code`. Without it the VS Code section reads "VS Code not found — install it or turn VS Code off in Settings → VS Code".

- **Download, once per server version.** `code serve-web --host 127.0.0.1 --port 0 --connection-token-file <f> --accept-server-license-terms --cli-data-dir <cli> --server-data-dir <server>` runs only to fetch the VS Code server (about 600 MB unpacked). editor-vscode loads its page once, which starts the download, waits for `<cli>/serve-web/<commit>/bin/code-server` and stops it.
- **The server.** editor-vscode then runs the newest downloaded `bin/code-server` itself, as a plain child process (not a terminal session): `--host 127.0.0.1 --port <p> --connection-token-file <f> --accept-server-license-terms --server-data-dir <server>`. One server serves every workspace; each root is its own `?folder=`.
- **Port.** The first start picks a free loopback port and stores it (`vscode.port`); later starts reuse it, because the web view keeps the workbench's state (open editors, layout, hot-exit backups) per origin. Only when that port is taken does it pick and store a new one, which drops that state.
- **Token.** A fresh random token per start, in a 0600 file passed with `--connection-token-file`, never on the command line. The server listens on `127.0.0.1` only.
- **Lifetime.** It starts once the plugin runs and a workspace is open in a window, and stops when the plugin shuts down (VS Code turned off, the core quitting). Its pid is kept in `server.pid`: when the plugin was killed and left a server running, the next start stops that one (only if its command line names this server data dir) so the stored port stays free. If the server exits it is restarted once; a second exit within 30 s shows the error with Retry.
- **Settings.** Before each start, and on every theme change, editor-vscode merges into `<server>/data/Machine/settings.json` (other keys are kept): `workbench.colorTheme` Default Dark Modern or Default Light Modern from the app theme's `dark` flag (editor-vscode follows `theme.changed` itself and keeps the last flag in its kv as `theme.dark`), `chat.disableAIFeatures: true` and `workbench.secondarySideBar.defaultVisibility: "hidden"` (no Copilot chat; Clubmate's chat lives there), and `task.allowAutomaticTasks: "off"`, so a repo's `tasks.json` never runs on folder open, trusted or not. A theme change also bumps the webview's `rev`, which reloads the workbench.

- **Workspace trust.** Workspace trust stays on: the first time a folder opens, VS Code asks whether to trust it, and an untrusted folder opens in Restricted Mode (its `.vscode/settings.json` and `launch.json` settings meant for trusted folders, debugging and extensions that need trust stay off). That matters for PR branches pr-walkthrough checks out under `.worktrees/walkthrough/*`. Clubmate pre-trusts nothing: `--disable-workspace-trust` is server-wide, and VS Code keeps the trusted-folder list in the workbench's own storage in the web view, with no setting or flag to seed it per root.

## Where data lives

Under the plugin state dir, `plugin-data/editor-vscode/vscode/`:

| Path | What |
|---|---|
| `cli/` | `--cli-data-dir`: downloaded servers under `serve-web/<commit>/` (each new server version is a new download; old ones stay until removed) |
| `server/` | `--server-data-dir`: `data/Machine/settings.json`, user data and extensions installed from the Extensions view |
| `token`, `server.pid` | the current connection token and server pid |

Nothing goes to `~/.vscode` or the desktop VS Code's settings and extensions.

Before the split this lived under `plugin-data/editor-nvim/vscode/`. On start, editor-vscode moves that dir to its own when it has none yet, so downloaded servers, extensions and settings are kept.

## Extensions from your VS Code

The VS Code in Clubmate starts with no extensions; it never reads `~/.vscode/extensions`. Settings → VS Code → Extensions → **Import from VS Code** (or `editor-vscode.import-extensions`) lists the desktop VS Code's extensions (`code --list-extensions`) and installs every one the server doesn't have yet with `code-server --install-extension <id> --server-data-dir server/`, one by one, from the Marketplace, so each gets the build that fits the server's VS Code version. The row shows "Installing N of M…" while it runs and then "Installed N · K failed: <ids>" (or "All your VS Code extensions are installed."); the VS Code section reloads when something was installed. The server must be downloaded first. Extensions added to the desktop VS Code later come over with the next import; removing one there doesn't remove it here.

## Consent and download

The VS Code server is under Microsoft's VS Code Server License Terms. The first time the VS Code section shows it explains that ("VS Code needs Microsoft's VS Code Server: about 600 MB, downloaded once per version, under the VS Code Server License Terms."), links the [terms](https://aka.ms/vscode-server-license) and offers Accept & Download. Only that button sets the consent, stored as `{"accepted": true}` under `settings` in editor-vscode's kv, after a confirmation; `editor-vscode.settings` reads it (`{accepted}`) and nothing else sets it. The button has `confirm`, so the core refuses an agent's `ui.event` on it, and editor-vscode takes `vscode-accept` only from its main slot of a workspace it shows or from its app-scoped settings slot (`editor-vscode.settings`, workspace `""`: Settings → VS Code and the first-launch sheet). Users who accepted before the split keep it: the core copies `vscodeAccepted` from editor-nvim's old `settings` key. An agent cannot accept for the user. Clubmate passes `--accept-server-license-terms` only after it. Accepting starts the download at once, even when no workspace shows VS Code (the first-launch sheet sits over the Welcome screen). Settings → VS Code → VS Code server reads Not downloaded (Accept & Download, or Download once accepted), Downloading the VS Code server…, Downloaded., or the failure with Retry; without a desktop VS Code it reads "VS Code isn't installed on this Mac…" and there is no Extensions row. While the server downloads, every workspace showing VS Code reads "Downloading the VS Code server…"; a failure shows its message and Retry. The failure stays until Retry or a restart (`editor-vscode.restart`).

## In the VS Code section

The main slot `editor-vscode.main` is a `webview` node (key `vscode`) at `http://127.0.0.1:<port>/?tkn=<token>&folder=<root>`, for the current root: the selected worktree of the first folder that exists, else home, chosen by the same rules as Neovim's (shared in `plugins/internal/editorroot`). The Mac keeps the page alive while the section is hidden; a root change navigates it to the new folder.

- `editor-vscode.open` (`editor.open` when VS Code is the default editor) switches the worktree when the path is in another one, then loads the workbench with `&payload=[["openFile","vscode-remote://127.0.0.1:<port><path>:<line>:<col>"],["gotoLineMode","true"]]`. This reloads the workbench. Later re-renders of that root keep the same URL, so they don't reload it again; a root change drops it. It focuses the VS Code section when it switched the worktree or `focus: true` is passed (agents' tool cards do), like Neovim's open.
- The status slot `editor-vscode.status` reads `VS Code` and the branch. The agent chat panel (⌘B in Neovim) is not in the VS Code section.
- `editor-vscode.terminal` opens a terminal in the root; `editor-vscode.restart` restarts the server and clears a shown failure.
- Quickfix, Review changes / Full diff (diffview, nrev comments), the chat's attach selection and Reveal in Finder are Neovim's and open in the Neovim section. `editor-nvim.unsaved` and the worktree switch guard see only Neovim's buffers; VS Code's hot exit keeps its own unsaved edits.

## Keys

While VS Code has focus, ⌘W, ⌘N, ⌘O, ⌘P and ⇧⌘P go to it; every other app menu key stays with Clubmate, so VS Code's ⌘K chords, ⌘B sidebar and ⌘J panel are shadowed (⌃` opens its panel). See [shortcuts](shortcuts.md#vs-code-vs-code-section).

The phone never shows the VS Code section.
