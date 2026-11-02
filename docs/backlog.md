# Backlog

The **Backlog** section is a per-workspace task list shared by you and your agents.

## What it shows

- Sidebar: a "New task" field on top (Return adds a `todo` task); once any task has a tag, a row of tag chips (`tabs` `tags`, action `filter`: `all` "All" with the task count, then `tag:<tag>` per tag with its count, most used first, ties A–Z; scrolls sideways when it doesn't fit, keeping the selected chip in view) that keeps only the tasks with that tag until the plugin restarts or the tag is gone; then tasks grouped as In progress, Todo, Idea, Not now, Done. Rows read `#12 Title`; in-progress rows show the owner, others the first line of the note; high and low priority get a badge.
- Main: the task as an autosaving form. Its `header` is the title, edited in place (click it or ↩ on it; ↩ or leaving saves, Esc cancels; a bad title shows its error under it), the status glyph, `#12` with the owner, the last owner and the note as the muted meta, and Start agent (⌘↩), Release, Done (D) and Delete (⌘⌫, confirmed); an owner that is an agent session adds "Show Session", which selects it in Agents. Status, priority, tags (one field, comma- or space-separated; a bad tag shows its error under it) and the description follow, with a "✓ Saved" button (⌘S saves at once).
- A non-empty description shows rendered under the header (a `document` node, `size: fit`, rendered by `pkg/docrender`: tables, highlighted code, mermaid diagrams; only `http(s)` links, which open outside the app) and leaves the form; "Edit description" swaps it for the multi-line field and "Done editing" back. The flag is per workspace and holds only for the selected task. An empty description is the field. A change from anywhere (an agent, the phone) re-renders it on `backlog.changed`.
- Companion (iPhone/iPad): a Backlog item under each Mac with the same groups (Not now and Done folded), New Task (title, description, priority, status), and a task screen with Start, Release, Done, Move to and Edit. The task screen draws the description from `descriptionHtml` (`backlog.get`) with the shared document web view; a Mac that sends none gets it as plain Markdown. It acts as you (owner `user`) and refreshes on `backlog.changed`.
- ⌘N inside the section, or "New backlog task" in the palette, focuses the new-task field. Delete is UI-only (row menu, confirmed).

Statuses: `idea`, `todo`, `in_progress`, `done`, `not_now`. Priorities: `high`, `normal`, `low`. Tags: up to 10 per task, each one word of letters, digits, `-`, `_`, `.` or `/` and up to 32 characters, stored lowercase without a leading `#`, duplicates dropped; `backlog_update` with `tags` replaces them (`[]` clears). The phone shows no tags yet. Order is priority, then oldest first (done: newest first).

## Agent workflow

Agents use the MCP tools `backlog_list`, `backlog_get`, `backlog_add`, `backlog_update`, `backlog_claim`, `backlog_release`, `backlog_done`.

1. `backlog_list` with `status: todo`, take the first task (`backlog_get` for the full text).
2. `backlog_claim` it. If another owner holds it, the error says who; pick the next task.
3. Work. Record progress with `backlog_update` and a `note`.
4. Finish with `backlog_done` and a note naming the commit, or `backlog_release` (with a note) if you give up.

`backlog_list` defaults to `status: open` (idea + todo + in progress); `tag` keeps only tasks with that tag. `backlog.get` from the app or the phone also returns `descriptionHtml`, the description rendered; agents don't get it.

A task an agent finds goes in with `backlog_add` as `idea` by default; an agent may add or move a task to `todo` (e.g. when the user asks it to). Nobody sets `in_progress` through update (claim does that). To park a task use `not_now`.

## Owners

- Hosted agent session (`agt_…`): the owner is that session; any `owner` argument is ignored.
- External agent (for example Claude Code connected with `clubmate mcp` in a terminal): pass `owner`, a name unique to you such as your session id; `user`, `agent` and names starting with `agt_` are rejected. Its claims never go stale.
- You (UI, palette, `cm command run` without `--agent`): the owner is `user`.

Release is allowed to the owner, an ancestor agent session of the owner (an orchestrator closing its children's tasks) and the user. Done is allowed to them too, and to anyone when the task is unclaimed or its claim is stale. A stale claim is taken over with claim. A task claimed by someone else can only get a new title, description, priority or note from other agents, not a new status.

## Stale claims

Nothing changes status on its own. A claim by an `agt_…` session is stale when that session is closed or no longer listed. The UI shows it as "<title> (closed)" so you can mark the task done or move it back. Claiming a task with a stale claim takes it over, and the previous owner is kept as `lastClaimedBy`. If the agents plugin can't be asked, claims are treated as live.

## Examples

```sh
cm command run backlog.list --agent status=todo
cm command run backlog.get --agent id=12
cm command run backlog.claim --agent id=12 owner=my-session
cm command run backlog.update --agent id=12 note="parser done, tests next"
cm command run backlog.done --agent id=12 owner=my-session note="abc1234 fix parser"
cm command run backlog.add --agent title="Flaky bus test" status=idea
cm command run backlog.update --agent id=12 tags=voice,phone
cm command run backlog.list --agent tag=voice
```

Prefer the MCP tools inside an agent session: `cm command run` without `--agent` counts as the user.
