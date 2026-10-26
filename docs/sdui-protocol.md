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

Interactive types (`tabs list tree button textField toggle select form`) accept an optional `action` prop: a string of 1-64 chars matching `^[a-z][a-z0-9.-]*$`. It overrides the default action name sent in `ui.event`.

## Item shapes

`items` and `options` accept `null` as an empty list.

- item (`tabs`, `list`): `{id*, label*, detail, icon, badge}`, all strings, ids unique and non-empty.
- tree item (`tree`): item fields plus `hasChildren` (bool), `expanded` (bool), `children` (tree items).
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
| markdown | text* (string) | none | no |
| button | label* (string), icon (string), action | none | yes |
| textField | value (string), placeholder (string), action | none | yes |
| toggle | value* (bool), action | none | yes |
| select | options* (option[]), value (string), action | none | yes |
| form | action | any | yes |
| surface | sessionId* (`ses_` id) | none | no |
| empty | text* (string) | none | no |

A window shows at most one `surface` node at a time in v1. The client keeps every terminal it has shown mounted until the session's attach exits or the window closes, so switching between sessions is instant. A second `surface` visible in the same window at once renders a placeholder.

## Slots

A slot is named `<section>.sidebar` or `<section>.main`. A section is registered by a plugin, which owns both of its slots. Only the owning plugin may call `ui.render` for them. Slots are rendered per project.

## Revisions

Every slot (per project) has a revision counter that starts at 1 and increases by 1 on each render. When a plugin stops or crashes, its slots are cleared: the counter increments and the tree becomes `empty` with empty text. The cleared slot is removed from the store: the `empty` view (rev + 1) is delivered only to live subscribers, and later `ui.get` or subscribe replays omit the slot. Clients render that view like any other update (an empty pane) and do not get the slot back from `ui.get` after a reconnect.

Revisions are in-memory and reset when the core restarts; clients must reset their rev map on reconnect.

Clients keep the highest `rev` per `(projectId, slot)` and ignore views with a lower or equal `rev`.

## RPC methods

| method | params | result |
|---|---|---|
| ui.sections | none | `[{id, title, icon, order, plugin}]` |
| ui.get | `{projectId}` | `[SlotView]` sorted by slot |
| ui.subscribe | `{projectId}` | `{subscription}` |
| ui.unsubscribe | `{subscription}` | `{}` |
| ui.event | `{projectId, slot, nodeId, action, value}` | plugin-defined JSON |
| ui.render | `{projectId, slot, tree}` (plugin callers only) | `{rev}` |
| command.list | none | `[{id, title, plugin}]` |
| command.run | `{id, projectId, args}` | plugin-defined JSON |

`SlotView` is `{projectId, slot, owner, rev, tree}`.

`command.run` fails with unknown command when `id` is not listed; it is routed to the plugin that registered the command.

`ui.event` requires a non-empty `action`, a valid slot and a registered section. It is routed to the owning plugin.

## Notifications

- `ui.updated` `{subscription, view}`: `view` is a `SlotView`. On subscribe, the current views of the project are replayed first.
- `ui.closed` `{subscription, error}`: the subscription ended with an error (for example it fell behind); resubscribe and replace local state.

## Core notifications

- `core.stopping` (no params): sent to every connected client (not to plugins) after `core.shutdown` has replied and right before the core stops. A client that sees the connection end after `core.stopping` treats the stop as intentional and does not restart the core; a connection that ends without it is an unexpected loss.

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
| form | submit | `props.action ?? "submit"` | object `{fieldNodeId: value}` |

`nodeId` is the `id` of the node that fired the event.

## Forms

A `form` collects the values of the `textField`, `toggle` and `select` nodes among its descendants (nested forms collect their own fields), keyed by the field node `id`. The value is the field's current text, bool or option value; fields that disappear from the tree are dropped.

- A `button` without an `action` prop inside a form submits the form: the form sends `props.action ?? "submit"` with the collected object as `value` and the form's `id` as `nodeId`.
- Return in a `textField` without an `action` prop submits the form the same way.
- Fields without an `action` prop inside a form are silent: they only update the collected values.
- A field or button with its own `action` prop sends its own event per the contract above (fields still update the collected values); it does not submit the form.
