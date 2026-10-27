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

Interactive types (`tabs list tree button textField folderField toggle select form`) accept an optional `action` prop: a string of 1-64 chars matching `^[a-z][a-z0-9.-]*$`. It overrides the default action name sent in `ui.event`.

## Item shapes

`items` and `options` accept `null` as an empty list.

- item (`tabs`, `list`): `{id*, label*, detail, icon, badge}`, all strings, ids unique and non-empty.
- tree item (`tree`): item fields plus `muted` (bool), `hasChildren` (bool), `expanded` (bool), `children` (tree items).
- option (`select`): `{value*, label*}`.

## Node types

| type | props | children | interactive |
|---|---|---|---|
| stack | direction* (`vertical`\|`horizontal`) | any | no |
| split | direction* (`vertical`\|`horizontal`) | 2-4 | no |
| scroll | none | exactly 1 | no |
| tabs | items* (item[]), selected* (string), action | none | yes |
| list | items* (item[]), selected (string), action | none | yes |
| tree | items* (tree item[]), selected (string), action | none | yes |
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

The `.status` slot is a compact status line for the section; clients show it in the status area while the section is active.

### Views

A plugin may claim a slot of another plugin's section with `contributes.views`:

```json
{"contributes": {"views": [{"slot": "alpha.sidebar"}]}}
```

The claiming plugin becomes the owner of that slot instead of the section owner: only it may `ui.render` the slot and `ui.event` for it is routed to it. Slot syntax is validated at manifest load. A view on a section that no plugin registers is ignored and logged. When two plugins claim one slot, the later manifest in discovery order loses its claim and the conflict is logged.

## Revisions

Every slot (per project) has a revision counter that starts at 1 and increases by 1 on each render. When a plugin stops or crashes, its slots are cleared: the counter increments and the tree becomes `empty` with empty text. The cleared slot is removed from the store: the `empty` view (rev + 1) is delivered only to live subscribers, and later `ui.get` or subscribe replays omit the slot. Clients render that view like any other update (an empty pane) and do not get the slot back from `ui.get` after a reconnect.

`project.delete` clears every slot of the deleted project the same way (the `empty` view at rev + 1 to live subscribers; `ui.get` for that project then returns `[]`). Before deleting, the core kills the project's processes, waits for each of its sessions to stop and removes them, publishing `session.removed` `{id}` for each; then it deletes the project and publishes `project.deleted` `{id}`. If a session does not stop within the kill grace, the delete fails and the project is kept. Clients close a project window when its project disappears from `project.list` after having been listed.

Revisions are in-memory and reset when the core restarts; clients must reset their rev map on reconnect.

Clients keep the highest `rev` per `(projectId, slot)` and ignore views with a lower or equal `rev`.

## RPC methods

| method | params | result |
|---|---|---|
| ui.sections | none | `[{id, title, icon, order, scope, plugin}]` |
| ui.get | `{projectId}` (`""` for app scope) | `[SlotView]` sorted by slot |
| ui.subscribe | `{projectId}` | `{subscription}` |
| ui.unsubscribe | `{subscription}` | `{}` |
| ui.event | `{projectId, slot, nodeId, action, value}` | plugin-defined JSON |
| ui.render | `{projectId, slot, tree}` (plugin callers only) | `{rev}` |
| ui.presence | none | `[projectId]`, projects that currently have UI subscribers |
| fs.watch | `{projectId}` | `{watch}` |
| fs.unwatch | `{watch}` | `{}` |
| command.list | none | `[{id, title, plugin, hidden, confirm}]` |
| command.run | `{id, projectId, args}` | plugin-defined JSON |
| ui.focus | `{projectId, section, slot}` | `{delivered}` |
| client.register | `{handles}` | `{}` |
| project.open | `{id}` | `{clients}` |
| process.kill | `{pids, sessionId, projectId, orphansOnly}` | `{killed: [pid]}` |

`SlotView` is `{projectId, slot, owner, rev, tree}`.

A command with `"hidden": true` in its manifest entry is still listed and runnable; clients leave it out of the command palette.

A command with a non-empty `"confirm"` string in its manifest entry is listed with that text (`""` when absent); the client asks for confirmation with it before running the command.

`process.kill` with `orphansOnly` kills only orphaned processes and needs `sessionId` or `projectId` (otherwise invalid params). With `projectId`, only that project's orphans are killed; with `sessionId`, only that session's. Without `orphansOnly`, `pids` must be tracked processes. Each target is signalled only if its start time still matches the tracked one, so a reused pid is skipped.

`project.open` fails with not found for an unknown project; otherwise it sends the `project.open` notification to every connection that registered `"project.open"` via `client.register`, except the caller, and returns how many were notified (`clients`). `clubmate project open <id>` calls it and, when `clients` is 0, runs `open -b com.clubmate.app --args --open-project <id>`.

`client.register` marks the calling connection for the listed handles; the only handle is `"project.open"` and anything else is invalid params. Registration lasts for the connection. Plugin callers get invalid params.

`ui.focus` is open to any caller. It is invalid params when the project does not exist, the section is not registered or is app-scope, or `slot` is not `""`, `<section>.sidebar` or `<section>.main`. It sends the `ui.focus` notification to every `ui.subscribe` subscription of that project (app-scope subscriptions never receive it) and returns their count as `delivered`. Plugins call it as `Host.Focus`.

`ui.render` is invalid params for a project that has been deleted.

`command.run` fails with unknown command when `id` is not listed; it is routed to the plugin that registered the command.

`editor-nvim.restart` takes optional args `{force}`. Without `force: true` it first asks the running nvim for modified buffers and fails with `unsaved buffers: <names>` (names relative to the project repos, `[No Name]` for unnamed buffers) if there are any; the session is left running. If nvim does not answer within the RPC deadline it is restarted anyway. editor-nvim starts nvim in the first project repo that exists, else in `$HOME`; if the session cannot be started its main slot shows the error and a Restart button.

`ui.event` requires a non-empty `action`, a valid slot and a registered section. It is routed to the owning plugin.

## Notifications

- `ui.updated` `{subscription, view}`: `view` is a `SlotView`. On subscribe, the current views of the project are replayed first.
- `ui.focus` `{subscription, section, slot}`: the client should show `section`, and `slot` when non-empty, for that subscription's project.
- `ui.closed` `{subscription, error}`: the subscription ended with an error (for example it fell behind); resubscribe and replace local state.

## File watching

`fs.watch` starts one FSEvents stream over the project's repos as they are at call time (repos added later are not covered; call `fs.watch` again). It ends on `fs.unwatch` or when the connection closes. Repos that cannot be resolved (missing or deleted) are skipped and the rest are watched; a project with no resolvable repos gets a watch that never emits. Roots are symlink-resolved, so paths are real paths (`/private/var/...`, not `/var/...`).

- `fs.changed` `{watch, projectId, paths, overflow}`: sent to the watching connection with a 200ms coalescing window. `paths` is deduplicated. Under any `.git/` only `HEAD`, `index` and `refs/heads/**` are reported. A window with more than 1000 paths, or one in which FSEvents reports dropped or coalesced events (MustScanSubDirs, KernelDropped, UserDropped), is sent as `overflow: true` with `paths: []`; the receiver rescans.

Plugins receive it as `Plugin.FSChanged`, and use `Host.WatchFS` / `Host.UnwatchFS`.

## Bus topics

- `project.updated` `{project}`: published when a repo is added to a project (`project.addRepo`). Existing `fs.watch` streams do not follow; re-watch on this event.
- `editor.buffer` `{projectId, path}`: published by `editor-nvim` when nvim enters a file buffer (BufEnter). `path` is absolute, as nvim names the buffer.
- `editor.saved` `{projectId, path}`: published by `editor-nvim` after nvim writes a file buffer (BufWritePost).
- `processes.killed` `{projectId, pids}`: published by `processes` when auto-kill terminates orphans of a project; never published with empty `pids`.

## Core notifications

- `project.open` `{id}`: sent to every connection that registered `"project.open"` (not to plugins, not to the caller of `project.open`); the client shows that project.
- `core.stopping` (no params): sent to every connected client (not to plugins) after `core.shutdown` has replied and right before the core stops. A client that sees the connection end after `core.stopping` treats the stop as intentional and does not restart the core; a connection that ends without it is an unexpected loss.

## Presence

The core publishes `ui.opened` `{projectId}` on the durable bus when a project's UI subscriber count goes from 0 to 1, and `ui.idle` `{projectId}` when it goes from 1 to 0.

## Plugin runtime dir

The core creates `<runtime>/p/<pluginId>` (mode 0700) and passes its path to the plugin in `CLUBMATE_PLUGIN_RUNTIME_DIR` (`pluginsdk.RuntimeDir()`).

## ui.event contract

| node | trigger | `action` | `value` |
|---|---|---|---|
| button | click | `props.action ?? id` | `null` |
| list / tree | select item | `props.action ?? "select"` | item id |
| tree | expand / collapse | `"expand"` / `"collapse"` | item id |
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
