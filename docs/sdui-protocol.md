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

- item (`tabs`, `list`): `{id*, label*, detail, icon, badge}`, all strings, ids unique and non-empty.
- tree item (`tree`): item fields plus `hasChildren` (bool), `expanded` (bool), `children` (tree items).
- option (`select`): `{value*, label*}`.

## Node types

| type | props | children | interactive |
|---|---|---|---|
| stack | direction* (`vertical`\|`horizontal`) | any | no |
| split | direction* (`vertical`\|`horizontal`) | 2-4 | no |
| scroll | none | exactly 1 | no |
| tabs | items* (item[]), selected (string), action | none | yes |
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

## Slots

A slot is named `<section>.sidebar` or `<section>.main`. A section is registered by a plugin, which owns both of its slots. Only the owning plugin may call `ui.render` for them. Slots are rendered per project.

## Revisions

Every slot (per project) has a revision counter that starts at 1 and increases by 1 on each render. When a plugin stops or crashes, its slots are cleared: the counter increments and the tree becomes `empty` with empty text. Clients keep the highest `rev` per `(projectId, slot)` and ignore views with a lower or equal `rev`.

## RPC methods

| method | params | result |
|---|---|---|
| ui.sections | none | `[{id, title, icon, order, plugin}]` |
| ui.get | `{projectId}` | `[SlotView]` sorted by slot |
| ui.subscribe | `{projectId}` | `{subscription}` |
| ui.unsubscribe | `{subscription}` | `{}` |
| ui.event | `{projectId, slot, nodeId, action, value}` | plugin-defined JSON |
| ui.render | `{projectId, slot, tree}` (plugin callers only) | `{rev}` |

`SlotView` is `{projectId, slot, owner, rev, tree}`.

`ui.event` requires a non-empty `action`, a valid slot and a registered section. It is routed to the owning plugin.

## Notifications

- `ui.updated` `{subscription, view}`: `view` is a `SlotView`. On subscribe, the current views of the project are replayed first.
- `ui.closed` `{subscription, error}`: the subscription ended with an error (for example it fell behind); resubscribe and replace local state.

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
