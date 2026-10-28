# SDUI protocol

Plugins render trees of nodes into slots; clients draw them and send events back. Every tree is validated by `internal/sdui` (max 5000 nodes, depth 32). Violations fail with `ErrInvalidTree`.

## Node

```json
{"type": "button", "id": "new", "props": {"label": "New"}, "children": []}
```

- `type`: one of the types below.
- `id`: required for interactive types, unique within a tree.
- `props`: only the listed props are accepted; unknown props are rejected. `*` marks required props.
- `children`: count limited per type.

Interactive types (`tabs list tree table button textField folderField toggle select form`) accept an optional `action` prop: a string of 1-64 chars matching `^[a-z][a-z0-9.-]*$`. It overrides the default action name sent in `ui.event`.

## Item shapes

`items` and `options` accept `null` as an empty list.

- item (`tabs`, `list`): `{id*, label*, detail, icon, badge}`, all strings, ids unique and non-empty.
- tree item (`tree`): item fields plus `muted` (bool), `hasChildren` (bool), `expanded` (bool), `children` (tree items).
- option (`select`): `{value*, label*}`.
- column (`table`): `{id*, title*, align, width}`: `align` is `left` (default) or `right`, `width` is a fixed width in points (`0` or absent = flexible, sharing the remaining space). Ids unique and non-empty.
- row (`table`): `{id*, depth, hasChildren, expanded, muted, cells*}`: `depth` is an integer 0-32 (indent 12pt per level, chevron when `hasChildren`), `muted` dims the row's text. Ids unique and non-empty. `cells` must have exactly one cell per column.
- cell (`table`): `{text, bar, tone, spark}`: `bar` is a number 0…1 drawn as a filled capsule behind the text, `tone` is `""`\|`ok`\|`warn`\|`danger` (colours the bar and sparkline, or the text when there is no bar), `spark` is a spark.
- spark (`table` cell): `{values*, max*}`: at most 900 numbers, `max` > 0. Values are clamped to 0…`max` and drawn as a 60×16pt line. The Go helpers `sdui.Sparkline`, `sdui.SparklineSized` and `sdui.NewSpark` clamp them before sending.
- sort (`table`): `{column*, desc}`: the active sort column (arrow in its header) and direction. `column` must be one of the table's column ids.

## Node types

| type | props | children | interactive |
|---|---|---|---|
| stack | direction* (`vertical`\|`horizontal`) | any | no |
| split | direction* (`vertical`\|`horizontal`) | 2-4 | no |
| scroll | none | exactly 1 | no |
| tabs | items* (item[]), selected* (string), action | none | yes |
| list | items* (item[]), selected (string), action | none | yes |
| tree | items* (tree item[]), selected (string), action | none | yes |
| table | columns* (column[], at most 32), rows* (row[], at most 1000), selected (string, row id), sort (sort), action | none | yes |
| sparkline | values* (number[], at most 900), max* (number > 0), height (number 8…200) | none | no |
| text | text* (string), style (`body`\|`muted`\|`title`\|`mono`) | none | no |
| badge | text* (string), tone (`neutral`\|`accent`\|`success`\|`warning`\|`danger`) | none | no |
| icon | name* (string) | none | no |
| diff | text* (string, unified diff) | none | no |
| markdown | text* (string) | none | no |
| button | label* (string), icon (string), confirm (string), action | none | yes |
| textField | value (string), placeholder (string), action | none | yes |
| folderField | label* (string), values (string[]), action | none | yes |
| toggle | value* (bool), action | none | yes |
| select | options* (option[]), value (string), action | none | yes |
| form | action | any | yes |
| surface | sessionId* (`ses_` id) | none | no |
| empty | text* (string) | none | no |

A window shows at most one `surface` node at a time in v1. The client keeps every terminal it has shown mounted until the session's attach exits or the window closes, so switching between sessions is instant. A second `surface` visible in the same window at once renders a placeholder.

`table` rows render lazily; wrap the table in a `scroll` to scroll them under a pinned header. Each row counts toward the 5000-node limit; cells are bounded by the row and column caps (at most 1000 × 32), and spark values (cell sparks and `sparkline` nodes) are capped at 100000 per tree. The client does not page or truncate: a plugin with more rows renders the first 1000 or fewer and adds a final muted `… N more` row itself. `selected` works like `tree`'s: a changed value from the plugin sets, moves or clears the highlight (and scrolls to it); clicking a row highlights it locally and sends `select`. `sparkline` draws `values` as a 60×16pt line (e.g. in a header); with `height` it fills the available width at that height in points (e.g. a detail chart).

`button.confirm`: when set, the client asks the user to confirm with that text before sending the event.

Input fields are uncontrolled: `value`/`values` on `textField`, `folderField`, `toggle` and `select` are initial values. The client keeps what the user entered while the node keeps its identity. A changed `value` in a later render replaces it, but re-rendering an unchanged one does not. To reset a form (e.g. after a successful submit), the plugin renders it with a new node `id`. The client then discards the form's field state and starts again from the props. Actions are routed by `action`, not by `id`, so rotating ids (e.g. `create-<n>`) does not change event handling.

## Slots

A slot is named `<section>.sidebar`, `<section>.main` or `<section>.status`. A section is registered by a plugin, which owns all of its slots. Only the owning plugin may call `ui.render` for them. Slots are rendered per project, unless the section has `scope: "app"`.

### Scope

A manifest section has `scope`: `"project"` (default) or `"app"`; any other value fails manifest load. `ui.sections` returns it. Slots of an app-scope section are not tied to a project and are rendered, read, subscribed and sent events with `projectId: ""`:

- `ui.get` and `ui.subscribe` with `""` cover only app-scope slots; with a project id, only project-scope slots.
- `ui.render` and `ui.event` with `""` on a project-scope slot, or with a project id on an app-scope slot, fail with invalid params.
- `ui.subscribe` with `""` does not count toward presence and publishes no `ui.opened`/`ui.idle`.
- App-scope slots of a stopped or crashed plugin are cleared like project slots (the `empty` view is delivered to live subscribers).

### Auto new

A manifest section may set `autoNew: true` (default `false`); `ui.sections` returns it. When the user selects such a section and its main slot, once rendered, is an `empty` node (the section has nothing to show, e.g. no terminals; an exited terminal's view is not `empty`), the client runs the section's `<section>.new` command once for that selection (`CommandInvocation.shouldAutoNew`). Terminals sets it, so a terminal appears on entering the section.

The `.status` slot is a compact status line for the section; clients show it in the status area while the section is active.

### Views

A plugin may claim a slot of another plugin's section with `contributes.views`:

```json
{"contributes": {"views": [{"slot": "alpha.sidebar"}]}}
```

The claiming plugin becomes the owner of that slot instead of the section owner: only it may `ui.render` the slot and `ui.event` for it is routed to it. Slot syntax is validated at manifest load. A view on a section that no plugin registers is ignored and logged. When two plugins claim one slot, the later manifest in discovery order loses its claim and the conflict is logged.

## Revisions

Every slot (per project) has a revision counter that starts at 1 and increases by 1 on each render. When a plugin stops or crashes, its slots are cleared: the counter increments and the tree becomes `empty` with empty text. The cleared slot is removed from the store: the `empty` view (rev + 1) is delivered only to live subscribers, and later `ui.get` or subscribe replays omit the slot. Clients render that view like any other update (an empty pane) and do not get the slot back from `ui.get` after a reconnect.

`project.delete` clears every slot of the deleted project the same way (the `empty` view at rev + 1 to live subscribers; `ui.get` for that project then returns `[]`). Before deleting, the core kills the project's processes, waits for each of its sessions to stop and removes them, publishing `session.removed` `{id}` for each; then it deletes the project and publishes `project.deleted` `{id}`. If a session does not stop within the kill grace, the delete fails and the project is kept. Clients take a workspace out of its window when its project disappears from `project.list` after having been listed.

Revisions are in-memory and reset when the core restarts; clients must reset their rev map on reconnect.

Clients keep the highest `rev` per `(projectId, slot)` and ignore views with a lower or equal `rev`.

## RPC methods

| method | params | result |
|---|---|---|
| ui.sections | none | `[{id, title, icon, order, scope, autoNew, plugin}]` |
| ui.get | `{projectId}` (`""` for app scope) | `[SlotView]` sorted by slot |
| ui.subscribe | `{projectId}` | `{subscription}` |
| ui.unsubscribe | `{subscription}` | `{}` |
| ui.event | `{projectId, slot, nodeId, action, value}` | plugin-defined JSON |
| ui.render | `{projectId, slot, tree}` (plugin callers only) | `{rev}` |
| ui.presence | none | `[projectId]`, projects that currently have UI subscribers |
| ui.state.section | `{projectId}` | `{section}`, the section saved in the project's UI state (`""` when unset) |
| fs.watch | `{projectId}` | `{watch}` |
| fs.unwatch | `{watch}` | `{}` |
| command.list | none | `[{id, title, description, plugin, hidden, confirm, args?, result?, agent, destructive, provides?}]` |
| command.run | `{id, projectId, args}` | plugin-defined JSON |
| ui.focus | `{projectId, section, slot, edge}` | `{delivered}` |
| client.register | `{handles}` | `{}` |
| project.open | `{id}` | `{clients}` |
| project.rename | `{id, name}` | project |
| workspace.open | `{folders}` | `{id, created, clients}` |
| workspace.home | none | `{id, clients}` |
| workspace.touch | `{id}` | `{}` |
| process.tree | `{projectId, sessionId}` (both optional filters) | `[{process: {pid, ppid, pgid, comm, args, startTime, rssKiB, cpu}, sessionId, orphan, duplicate}]` |
| process.kill | `{pids, sessionId, projectId, orphansOnly}` | `{killed: [pid]}` |

`SlotView` is `{projectId, slot, owner, rev, tree}`.

A command with `"hidden": true` in its manifest entry is still listed and runnable; clients leave it out of the command palette.

A command with a non-empty `"confirm"` string in its manifest entry is listed with that text (`""` when absent); the client asks for confirmation with it before running the command.

A manifest command entry may also declare, all optional and listed as declared:
- `description` (`""` when absent): what the command does, for agents and `command describe`;
- `args` and `result`: schemas for the command's args and its result (absent: any args, nothing declared);
- `agent` (default `false`): the command is meant for agents;
- `destructive` (default `false`): the command destroys state (closes, kills, discards);
- `provides`: generic capability names the command implements.

A schema is a small JSON Schema subset: an object `{properties: {name: prop}, required: [name]}` (object only at the top level), where a prop is `{type, items, enum, description, default}`, `type` is `string`, `integer`, `number`, `boolean` or `array`, and an `array` has scalar `items`. `enum` values must match the type and every `required` name must be a property; anything else fails manifest load.

`command.run` validates `args` against the command's `args` schema before routing, so the plugin is never called with bad args. Absent or `null` args count as `{}`; properties not in the schema are allowed; a `null` property counts as absent. Errors are invalid params naming the field: `args: want object`, `args.path: required`, `args.line: want integer`, `args.side: want one of ["left","right"]`, `args.paths[1]: want string`. `integer` rejects fractions and exponents. A command without an `args` schema accepts any args.

`process.tree`'s `args` is the full argv (`[]` when it can't be read, e.g. for another user's process) and `startTime` is RFC 3339.

`process.kill` with `orphansOnly` kills only orphaned processes and needs `sessionId` or `projectId` (otherwise invalid params). With `projectId`, only that project's orphans are killed; with `sessionId`, only that session's. Without `orphansOnly`, `pids` must be tracked processes. Each target is signalled only if its start time still matches the tracked one, so a reused pid is skipped.

`project.open` fails with not found for an unknown project; otherwise it sends the `project.open` notification to every connection that registered `"project.open"` via `client.register`, except the caller, and returns how many were notified (`clients`). `clubmate workspace open --id <id>` calls it and, when `clients` is 0, runs `open -b com.clubmate.app --args --open-project <id>`.

Projects are shown to users as workspaces; `clubmate project …` is an alias of `clubmate workspace …`. A project is `{id, name, repos, createdAt, home, lastOpenedAt}`; `lastOpenedAt` is `null` until the first `workspace.touch`; subscribing to a project's UI does not set it. `project.list` orders by `lastOpenedAt` then `createdAt`, newest first.

`workspace.open` resolves each folder path (symlinks resolved, cleaned), drops duplicates and returns the project whose repos are exactly that set (order-insensitive, stored repos resolved the same way) with `created: false`; otherwise it creates a project with the resolved folders, named after the base name of the first one (`project.created`), with `created: true`. Folders must be absolute paths to existing directories (otherwise invalid params, as is an empty `folders`). `workspace.home` returns the single `home: true` project, named `~` and rooted at the user's home directory, creating it (`project.created`) on first use. Both then send the `project.open` notification exactly like `project.open` (not to the caller) and return how many were notified (`clients`); `clubmate workspace open <dir>…` and `clubmate workspace home` run the app like `open --id` when `clients` is 0. `workspace.touch` sets `lastOpenedAt` to now (not found for an unknown project); clients call it when they activate a workspace. `project.rename` fails with invalid params on a blank name and publishes `project.updated`.

`client.register` marks the calling connection for the listed handles; the only handle is `"project.open"` and anything else is invalid params. Registration lasts for the connection. Plugin callers get invalid params.

`ui.focus` is open to any caller. It is invalid params when the project does not exist, the section is not registered or is app-scope, or `slot` is not `""`, `<section>.sidebar` or `<section>.main`, or `edge` is not absent, `""`, `h`, `j`, `k` or `l`. It sends the `ui.focus` notification to every `ui.subscribe` subscription of that project (app-scope subscriptions never receive it) and returns their count as `delivered`. Plugins call it as `Host.Focus`, or as `Host.FocusEdge` to pass `edge`.

`ui.render` is invalid params for a project that has been deleted.

`command.run` fails with unknown command when `id` is not listed; it is routed to the plugin that registered the command.

`files.collapse-others` ("Collapse Others") takes no args. It keeps expanded only the repo roots and the ancestors of the selected file (the file last opened in or revealed from the editor), persists the expanded set and re-renders the tree. In the files tree a directory whose only visible entry is one directory is merged with it into one row labelled `a / b / c` whose id is the deepest path; expand and collapse act on that path. File rows have no icon.

`editor-nvim.restart` takes optional args `{force}`. Without `force: true` it first asks the running nvim for modified listed file buffers (`buftype` empty, `buflisted`; scratch buffers do not count) and fails with `unsaved buffers: <names>` (names relative to the project repos, `[No Name]` for unnamed buffers) if there are any; the session is left running. If nvim does not answer within the RPC deadline it is restarted anyway. editor-nvim starts nvim in the first project repo that exists, else in `$HOME`; if the session cannot be started its main slot shows the error and a Restart button.

`editor-nvim.diff` takes args `{path, base, target}`: `path` must be absolute (otherwise an error), `base` is `head`, `index` or `rev:<sha>` (lowercase hex), `target` is `index`, `worktree` or `rev:<sha>` (lowercase hex), and they differ (`rev:` sides compared by their 7-char short sha). The repo is the nearest ancestor of `path` containing `.git`; `path` itself need not exist. editor-nvim reads `HEAD:<rel>` / `:<rel>` / `<sha>:<rel>` with `git show`; a path missing from `HEAD` (or an unborn `HEAD`), from the index or from `<sha>` is empty content, any other git failure (including an unknown `<sha>`) is an error. It opens a new nvim tab with a side-by-side diff: the left window is a read-only scratch buffer named `clubmate://<base>/<rel>` (`clubmate://<first 7 chars of sha>/<rel>` for `rev:<sha>`), the right window is the file itself for `worktree`, a scratch buffer `clubmate://index/<rel>` for `index`, or a scratch buffer `clubmate://<first 7 chars of sha>/<rel>` for `rev:<sha>` (scratch buffers are read-only). Any other tab showing a `clubmate://*/<rel>` scratch buffer is closed. When the worktree file has `fileformat=dos`, trailing `\r` is stripped from the scratch lines.

`editor-nvim.review` takes args `{repo, base, head, mergeBase}` and returns `{mode}`. `head` defaults to `HEAD` when empty. `repo` must be absolute and `base` and `head` non-empty refs not starting with `-` and without whitespace (otherwise an error); `mergeBase` is accepted but unused. It starts nvim if needed. If nvim has `:DiffviewOpen` (`exists(':DiffviewOpen') == 2`), it closes the review tab it opened last time if that tab is still valid (switches to it and runs `DiffviewClose`), runs `DiffviewOpen -C<fnameescape(repo)> <base>...<head>` and returns `mode: "diffview"`, or `mode: "builtin"` (recording no tab) when the command opened no new tab (diffview reports its own errors softly); without `:DiffviewOpen` it changes nothing and returns `mode: "builtin"`. `editor-nvim.review-close` takes no args and closes that review tab the same way; it is a no-op when nvim is not running or the tab is gone.

`editor-nvim.navigation` (hidden) takes args `{enabled, side}` and returns `{skipped}`. `side` is the Code sidebar's side, `left` or `right` (anything else is an error). It stores `{enabled, side}` in plugin storage under `navigation` (global, not per project; the `projectId` only routes the call) and applies it to every connected nvim; on every (re)connect editor-nvim applies the stored value right after registering the `clubmate` module, and does nothing when nothing is stored. Enabled: with `package.loaded["smart-splits"]` present it wraps smart-splits' `at_edge` (`config.move.at_edge` in v3, `config.at_edge` in v2) so that `ctx.direction` pointing at the sidebar (`left` for side `left`, `right` for side `right`) only calls `edge(ctx.direction)`, and every other direction runs the original `at_edge` (a function, or `wrap`/`split` through `ctx.wrap()`/`ctx.split()`); it maps no keys; otherwise it maps normal-mode `<C-h>`, `<C-j>`, `<C-k>`, `<C-l>` to `require("clubmate").navigate(dir)` with `desc = "clubmate navigate"`, taking over a global mapping that is nvim's built-in default (`desc` `:help …-default`) or a plain non-`expr` window move in the key's own direction (rhs keycodes equal to `<C-w>d`, `<C-w><C-d>`, `<Cmd>wincmd d<CR>` or `:wincmd d<CR>`), and skipping keys that have any other global mapping; `skipped` lists those keys (sorted, across all nvims). Disabled: removes the global mappings with that `desc`, restores the mappings it took over (exactly, from `maparg()`; unless the key was remapped meanwhile) and unwraps `at_edge` if it is still the wrapper. Applying is idempotent. The app stores the answer in `AppSettings.navigationKeys`, sends it with `AppSettings.sidebarSide` once per launch after the first project connects, and again whenever either changes; a non-empty `skipped` is shown as a banner (`Ctrl+J left alone: you already map it`).

`terminals.busy` (hidden) takes args `{sessionId}` and returns `{busy, process}`. `busy` is true when the session's process tree (`process.tree`) has a live descendant of the shell (the session's `childPid`); `process` is the `comm` of the newest such descendant (latest start time), `""` when not busy. An unknown or exited session is not busy. The app's ⌘W "Close Terminal" closes the Terminals section's selected terminal (the `selected` of the `terminals` list in `terminals.sidebar`): it always runs `terminals.busy` first and, when busy, asks `“<process>” is running. Close the terminal?` before `terminals.close`; an exited one is not busy, so it closes without asking. In a project window outside the Terminals section ⌘W does nothing and never closes the window (the red button does); in a window without a workspace (the welcome screen, Settings) it is "Close Window" and closes it. ghostty's `super+w` is unbound inside clubmate, so ⌘W reaching a surface does nothing. The item sits after "Close Session" in the File menu; SwiftUI keeps the system "Close" item (`performClose:`) and may hand ⌘W back to it when it rebuilds the menu, so the app moves ⌘W from it to its own item whenever the menu changes (the system item stays, without a shortcut).

When nvim reaches its left or right edge through the `clubmate` Lua module (see `docs/nvim-integration.md`), editor-nvim calls `ui.focus` with slot `editor-nvim.sidebar` and `edge` `h` or `l`.

`ui.event` requires a non-empty `action`, a valid slot and a registered section. It is routed to the owning plugin.

## Notifications

- `ui.updated` `{subscription, view}`: `view` is a `SlotView`. On subscribe, the current views of the project are replayed first.
- `ui.focus` `{subscription, section, slot, edge}`: the client should show `section`, and `slot` when non-empty, for that subscription's project. `edge` is present only when the caller passed a non-empty one: the direction nvim navigation left in. The client ignores a focus whose `edge` does not lead to its sidebar (`h` with the sidebar on the left, `l` with it on the right; `j` and `k` never do). A focus to a hidden sidebar shows it first.
- `ui.closed` `{subscription, error}`: the subscription ended with an error (for example it fell behind); resubscribe and replace local state.

## File watching

`fs.watch` starts one FSEvents stream over the project's repos as they are at call time (repos added later are not covered; call `fs.watch` again). It ends on `fs.unwatch` or when the connection closes. Repos that cannot be resolved (missing or deleted) are skipped and the rest are watched; a project with no resolvable repos gets a watch that never emits. When a repo is a linked git worktree (`.git` is a file with `gitdir: <path>`), the watch also covers that worktree's gitdir and the main repository's common dir (from `<gitdir>/commondir`), so commits, checkouts and ref updates made in the worktree emit paths under `<main>/.git/` (only `HEAD`, `index`, `refs/heads/**` and `worktrees/<name>/{HEAD,index}` are reported from inside `.git`). Roots are symlink-resolved, so paths are real paths (`/private/var/...`, not `/var/...`).

- `fs.changed` `{watch, projectId, paths, overflow}`: sent to the watching connection with a 200ms coalescing window. `paths` is deduplicated. Under any `.git/`, and under a linked worktree's gitdir and the main repository's common dir, only `HEAD`, `index`, `refs/heads/**` and `worktrees/<name>/{HEAD,index}` are reported. A window with more than 1000 paths, or one in which FSEvents reports dropped or coalesced events (MustScanSubDirs, KernelDropped, UserDropped), is sent as `overflow: true` with `paths: []`; the receiver rescans.

Plugins receive it as `Plugin.FSChanged`, and use `Host.WatchFS` / `Host.UnwatchFS`.

## Bus topics

- `project.updated` `{project}`: published when a repo is added to a project (`project.addRepo`) or it is renamed (`project.rename`). Existing `fs.watch` streams do not follow; re-watch on this event.
- `editor.buffer` `{projectId, path}`: published by `editor-nvim` when nvim enters a file buffer (BufEnter). `path` is absolute, as nvim names the buffer.
- `editor.saved` `{projectId, path}`: published by `editor-nvim` after nvim writes a file buffer (BufWritePost).
- `processes.killed` `{projectId, pids}`: published by `processes` when auto-kill terminates orphans of a project; never published with empty `pids`.

## Core notifications

- `project.open` `{id}`: sent to every connection that registered `"project.open"` (not to plugins, not to the caller of `project.open`, `workspace.open` or `workspace.home`); the client shows that project.
- `core.stopping` (no params): sent to every connected client (not to plugins) after `core.shutdown` has replied and right before the core stops. A client that sees the connection end after `core.stopping` treats the stop as intentional and does not restart the core; a connection that ends without it is an unexpected loss.

## Presence

The core publishes `ui.opened` `{projectId}` on the durable bus when a project's UI subscriber count goes from 0 to 1, and `ui.idle` `{projectId}` when it goes from 1 to 0.

## Visible section

Clients keep per-project UI state with `ui.state.get` / `ui.state.set` `{projectId, state}` (client callers only). Its `section` field is the section the project's window shows: the app has one window per project and writes `section` when it opens the project and on every switch. When a `ui.state.set` changes `section`, the core publishes `ui.section` `{projectId, section}` on the durable bus. Plugins read the current value with `ui.state.section` (`pluginsdk.Host.SelectedSection`).

`processes` uses it to skip work nobody sees: its 1 Hz refresh keeps recording history for every present project but renders only projects whose section is `processes` or unknown (`""`). On `ui.section` to `processes` it renders at once.

## Plugin runtime dir

The core creates `<runtime>/p/<pluginId>` (mode 0700) and passes its path to the plugin in `CLUBMATE_PLUGIN_RUNTIME_DIR` (`pluginsdk.RuntimeDir()`).

## ui.event contract

| node | trigger | `action` | `value` |
|---|---|---|---|
| button | click | `props.action ?? id` | `null` |
| list / tree | select item | `props.action ?? "select"` | item id |
| tree | expand / collapse | `"expand"` / `"collapse"` | item id |
| table | select row | `props.action ?? "select"` | row id |
| table | expand / collapse | `"expand"` / `"collapse"` | row id |
| table | header click | `"sort"` | object `{column, desc}`: the clicked column id; `desc` flips when it is already the sort column, otherwise starts `true` for right-aligned columns and `false` for others |
| tabs | select tab | `props.action ?? "select"` | tab id |
| textField | submit | `props.action ?? "submit"` | text |
| toggle | change | `props.action ?? "toggle"` | bool |
| select | change | `props.action ?? "select"` | option value |
| folderField | pick folders | `props.action ?? "pick"` | string[] of absolute paths |
| form | submit | `props.action ?? "submit"` | object `{fieldNodeId: value}` |

`nodeId` is the `id` of the node that fired the event.

## Forms

A `form` collects the values of the `textField`, `folderField`, `toggle` and `select` nodes among its descendants (nested forms collect their own fields), keyed by the field node `id`. The value is the field's current text, bool, option value or (`folderField`) string[] of chosen paths; fields that disappear from the tree are dropped.

- A `button` without an `action` prop inside a form submits the form: the form sends `props.action ?? "submit"` with the collected object as `value` and the form's `id` as `nodeId`.
- Return in a `textField` without an `action` prop submits the form the same way.
- Fields without an `action` prop inside a form are silent: they only update the collected values.
- A field or button with its own `action` prop sends its own event per the contract above (fields still update the collected values); it does not submit the form.
