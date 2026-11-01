# Docs viewer

The `docs` plugin (`plugins/docs`) shows a Markdown or mermaid file rendered, or an HTML page (an artifact, see § Artifacts) running with JS, in a popup over the workspace on the Mac, and gives the phone the same HTML over remote. It renders with `pkg/docrender` (GFM, footnotes, highlighted code, mermaid fences as `pre.mermaid`, local images inlined) into a `document` node; the clients only draw it.

## Commands

| Command | Args | Agent | Remote | Result |
|---|---|---|---|---|
| `docs.open` | `{path, session?}` (`path` may end in `#anchor`) | yes (MCP `docs_open`) | no | `{path}`; shows the doc in the workspace's popup and focuses it |
| `docs.read` | `{path}` | no | yes | `{path, title, html, rev, kind}` |
| `docs.close` | none | no | no | renders the popup `empty` |

- A relative `path` in `docs.open` is resolved against the worktree of `session` (default the calling agent session, from `agents.sessions` field `worktree`), else the selected worktree of the workspace's first folder. In `docs.read` it is resolved against that folder.
- `path` in a result is absolute with symlinks resolved; `title` is the first `# heading` (an artifact's `<title>`), else the file name without extension; `kind` is `markdown` or `artifact`.
- Errors are exactly `not in workspace`, `not a document`, `too large` and `not found`.

## From nvim

`,mp` or `:ClubmateDocOpen` in the bundled nvim sends the event `docOpen` with the buffer's path to editor-nvim, which runs `docs.open {path}` in its workspace. Only a buffer whose name ends in `.md`, `.markdown`, `.mmd`, `.html` or `.htm` (any case) sends it; any other or an unnamed buffer shows `Not a Markdown, mermaid or HTML file` in nvim. The buffer is not saved first: the viewer shows the file on disk. A failed `docs.open` shows as `open failed` in the editor status, like a failed open.

## Scope

A path is allowed when, symlinks resolved, it is inside one of the workspace's folders (its path or selected worktree), the same folder in any other worktree of its repo (`git worktree list`), or the agents plugin's uploads dir (`<state>/plugin-data/agents/uploads`). Anything else is `not in workspace`, including a symlink inside a repo that points out of it. Only `.md`, `.markdown`, `.mmd`, `.html` and `.htm` open (`not a document`, also for a directory; the rule is `pkg/docext`); a file over 4 MiB is `too large`. Git-ignored files are allowed. Links and images inside a doc go through the same check: an out-of-scope link shows as plain text, an image as its alt text.

## Links

- an in-scope `.md`/`.markdown`/`.mmd`/`.html`/`.htm` → `clubmate-doc:<abs path>[#anchor]`; clicking it sends `openDoc` on `docs.popup`
- another in-scope file → `clubmate-file:<abs path>`; clicking it sends `openFile`, which runs `editor.open {path}`
- `http(s)` is kept and opens outside the app; `#anchor` scrolls in place; anything else is plain text

Images: `png jpg jpeg gif webp svg` whose first bytes match the type, at most 2 MiB each and 8 MiB per doc; above that the alt text and `image too large`.

## Popup

The plugin declares section `docs` with `rail: false` and slot `popup`, so it has no rail entry and only `docs.popup` shows.

- The tree is a vertical `stack` of a `header` (the file name, since the doc's own `# heading` is the first line of the page; a ghost icon-only `button` id `back`, action `back`, icon `chevron.left`, only while the back stack has more than one doc) and a `document` id `doc` (`path`, `rev`, `size: fill`, `anchor` when the doc was opened at one, `kind: artifact` for an artifact).
- Esc and ⌘W close the popup while it has focus; ⌘W does not reach the terminal or window under it.
- `docs.open` replaces the back stack with the doc and calls `ui.focus` on `docs.popup`. From an agent it also publishes `docs.opened`.
- A `#anchor` is split off a path only when the part before it is a doc (`a.md#intro`; `notes#1.md` stays a file name). Each back stack entry keeps its anchor: the doc loads at that heading, or at the top when no element has that id; a live reload keeps the scroll position.
- Events on `docs.popup`: `openDoc` (value the path with an optional `#anchor`) pushes the doc, or for the doc already on top only moves to the anchor, `back` pops, `close` clears the stack and renders `empty`; `openFile` opens the file in the editor.
- When the open file is deleted or renamed away, the popup keeps its header and shows `File no longer exists` with the path as hint; it shows the doc again when the file comes back.

## Artifacts

An artifact is one self-contained `.html`/`.htm` file, for interactive reports and UI mockups; the `docs_open` description tells agents to write them this way. It opens through the same commands, popup, phone sheet and Back stack as a doc, and links between docs and artifacts work both ways.

- The plugin does not render it: the `document` node has `kind: artifact` and `html` is the file as is (bytes as a UTF-8 string). Same scope and 4 MiB limit as a doc; a `#anchor` is split off the same way.
- The client serves it from its own `clubmate-artifact://<random>/` origin, one per load, with a non-persistent data store, no script message handlers (the page cannot reach the app) and this CSP header, which a `<meta>` in the page cannot loosen: `default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval' blob: <cdn>; style-src 'unsafe-inline' <cdn>; font-src data: <cdn>; img-src data: blob: <cdn>; connect-src 'none'; frame-src 'none'; form-action 'none'; base-uri 'none'`, where `<cdn>` is `https://cdnjs.cloudflare.com https://cdn.jsdelivr.net https://unpkg.com https://esm.sh`.
- Navigation: a relative link resolves against the artifact's path and sends `openDoc` (the plugin checks scope); a clicked `http(s)` link opens outside the app; everything else, including a script setting `location`, is cancelled.
- Theme: the app's `--bg --text --muted --accent --border --code-bg --success --warning --danger --font --code-font` are set on `:root` at document start.
- A change to the file bumps `rev` like a doc and reloads the page; page state is lost.
- Not supported: sibling files (its own css/js/images next to it), `size: fit`, editing.

## Live reload

While a workspace has a doc open in the popup or read by `docs.read` (the last 16 read paths), the plugin holds an `fs.watch` on it. A change to a watched path re-renders at most every 200 ms: the first change starts the window, later ones within it are folded into one render. Each re-render bumps the path's `rev`, renders the popup when the path is on top and publishes `docs.changed`. The watch is dropped when the popup closes and no read path is left. `fs.watch` covers each folder's path and selected worktree only, so a doc under uploads or in another (not selected) worktree outside the folder opens but does not reload.

## Bus

| Topic | Payload | When |
|---|---|---|
| `docs.changed` | `{workspace, path, rev}` | a watched path re-rendered |
| `docs.opened` | `{workspace, session, path}` | `docs.open` from an agent |

Both are forwarded to paired phones by `remote` (see [remote](remote.md)).

## Entry points

- an agent calls `docs_open`
- Open on a transcript card or tool row whose entry has `doc` (Mac and phone, see [agents](agents.md#docs-in-the-transcript))
- on the phone, the "Opened <file name>" banner shown for `docs.opened` on that session's screen

## Phone

The Companion opens a doc as a full-screen sheet (`AppNavigation.openDoc(computer:workspace:path:)`, presented from `RootView`) that reads it with `docs.read` and draws the HTML with the shared `ClubmateDocument` web view in the system theme. `DocsStore` keeps its own back stack: a `clubmate-doc:` link reads and pushes (keeping its `#anchor`, or only moving to the anchor when it points into the page on top), Back pops (only shown with two or more pages), `http(s)` opens in Safari, `clubmate-file:` does nothing. A `docs.changed` for the page on top with a higher `rev` reads it again and swaps the body in place; one for a page under it is read again when Back shows that page. A failed read keeps the stack and shows the plugin's error (`not in workspace`, …) under the page, or in place of it when nothing is open yet.
