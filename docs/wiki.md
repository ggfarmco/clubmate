# Wiki

The **Wiki** section is a per-project knowledge base: how subsystems work, why things were decided, examples and guides. Agents write most of it, for the user; the user reads it and edits now and then. Agents also keep a shadow wiki of their own notes (traps, recipes) under `.agents/`, hidden until **Show agent notes** is on.

## Storage

- Root: `$CLUBMATE_WIKI_DIR`, else `~/.clubmate/wiki`; the same folder for release and dev builds. Tests set `CLUBMATE_WIKI_DIR` (or `HOME`) to a temp dir; `scripts/test-app.sh` points it at the instance's `wiki/`.
- One folder per project, `<root>/<project>/`, its own git repo. `<project>` is the folder name of the repo's main checkout (`<name>-2`, `-3`, … when the name is taken). `<project>/.wiki.json` holds `{origin, checkout}` (owned by `plugins/internal/wikiroot`): a folder maps to a wiki by its `origin` URL first (normalised: no scheme, user, `.git` or trailing `/`; `git@host:path` = `host/path`; any case), then by its main-checkout path, so every worktree of a repo shares one wiki and a moved checkout keeps it. A folder that is not in a git repo gets a wiki keyed by its path the same way. The home workspace has none (`no wiki here`).
- Pages: `.md`, `.markdown`, `.mmd`, `.html`, `.htm` (`pkg/docext`), at most 4 MiB, no front matter; folders group them. A page path is relative to the project folder; `..`, absolute paths, a `.git` component, and a path that leaves the folder through a symlink are `bad page path`; another extension is `not a page`. Files are written and read through `os.Root`, git runs with literal pathspecs.
- Page metadata in `<project>/.pages.json` (owned by the plugin): per page `title` (first `#` heading, an `.html` page's `<title>`, else the file name), `sources` (repo paths the page explains), `blobs` (the git blob of each source in the writer's worktree at write time, `""` when missing), `writtenAt`, `by` (the agent session's title, else its id, or `you`).
- Single writer: every write and commit holds `<project>/.git/wiki.lock` (exclusive `flock`), project creation holds `<root>/.lock`, so the release and the dev core can write the same wiki. Each write commits at once: author `<by> <wiki@clubmate.local>`, message `<page>: <summary>` (`<page>: deleted`). Edits made outside the plugin (the user in nvim) are committed by a sweep under the same lock, one commit per page, as `you` with `<page>: edited` (or `deleted`): before every read, list and search, and from the section's 3 s poll.
- Links: relative Markdown links between pages; a link to code is its absolute path in the repo's main checkout. The docs viewer's scope includes the workspace's wikis (see [docs-viewer.md](docs-viewer.md) § Scope).

## Stale

Decided in the plugin and sent as `stale`; clients only draw it. A page is stale when, for any of its sources, the default branch's blob (`refs/heads/<default>`, else `origin/<default>`) differs from the recorded one **and** the default branch's last commit touching that path is newer than `writtenAt`. Agents write pages from a worktree before landing, so: before the branch lands main's last touch is older (not stale); after a squash the blob is the same (not stale); a later change on main makes it stale; a deleted source is stale. A page without `sources`, or a wiki of a non-git folder, is never stale. A user edit sets `writtenAt` to the sweep's time and keeps the sources and blobs.

## Commands

| id | args | result |
|---|---|---|
| `wiki.search` (agent) | `query`, `shadow?`, `limit?` (default 20, max 100) | `{hits: [{page, title, snippet}]}`: every word must match (any case); title 3 > heading 2 > body 1 per word, ties by path; `snippet` the first matching body line, 160 runes; an `.html` page matches on its visible text |
| `wiki.list` (agent) | `shadow?` | `{pages: [{page, title, updated, by, stale}]}` by path |
| `wiki.read` (agent) | `page` | `{page, title, content, sources, updated, by, stale, path}` |
| `wiki.write` (agent) | `page`, `content`, `summary`, `sources?` (strings; absolute paths inside the worktree are made relative) | `{page, path}`; creates the project wiki on the first write |
| `wiki.delete` (agent, destructive) | `page` | `{}` |
| `wiki.history` (hidden, not agent) | `page` | `{changes: [{at, by, summary}]}`, newest first, at most 50 |

The project of a command is the calling agent session's worktree, else the workspace's first git folder's selected worktree, else its first folder. Reads never create a wiki (no wiki yet → empty list, `not found`). Errors: `no wiki here`, `bad page path`, `not a page`, `too large`, `not found`. Every write and delete (and a sweep from the poll) publishes `wiki.changed {workspace, page}`.

## Section

- Sidebar: a live **Search wiki** field (`textField` `search`, action `search`; while it holds text the tree holds only the hits, best first, and Recent hides; "No pages match"), **Recent** (`list` `recent`: the 10 newest non-shadow pages with who wrote them), **Pages** (`tree` `pages`: folders first, then pages, ids `<project>/<page>`, folder rows `dir:<project>/<folder>` that fold; a workspace with several projects gets one top-level row per project, `project:<name>`; a stale page has a warning `stale` badge), and **Show agent notes** (`toggle` `agentNotes`, per workspace in memory) that adds `.agents/` as an "Agent notes" folder last.
- Main: the selected page, else the newest one. A `header` with the page's title, its icon, `updated Jan 2 15:04 by <who>` and the `stale` badge, then Edit in nvim (`editor.open {path, focus: true}`), History (swaps the page for the list of its changes; Back to page returns), New page, and Delete (confirmed: "Delete <page>? It stays in the wiki's git history."). The page is a `document` (`pkg/docrender`; an `.html` page as `kind: artifact`); a link to another page of the workspace's wikis selects it, any other doc opens in the docs viewer, a file in the editor; a link outside the workspace's folders and wikis is `not in workspace`. A `ui.event` with `agent: true` on New page, Edit in nvim or Delete is refused (`not for agents`): agents use the `wiki.*` commands, where `wiki.delete` asks the user.
- Empty wiki: a centred `empty` "No wiki pages yet" — "Agents write here as they work: how things work, why they were decided, examples. You can add a page too." — with **New page**, which writes `untitled.md` (`untitled-2.md`, …) with `# Untitled` and opens it in the editor. The home workspace reads "Open a project workspace to see its wiki."
- While a workspace shows the section, a 3 s poll stats each wiki; a change runs the sweep and re-renders, and every tick re-renders the section so `stale` follows main.
- No keys of its own yet; the phone shows no wiki yet.

## Agent guidance

The tool descriptions and the Clubmate MCP instructions tell agents: search the wiki before exploring a subsystem; after a feature or a decision, update its topic page (how it works, why) with `sources` instead of adding a page per session; traps, recipes and private notes go under `.agents/`; an example that must run is one self-contained `.html` page (same rules as `docs_open`); never write secrets (nothing enforces it).
