# Backlog

The **Backlog** section is a per-workspace task list shared by you and your agents.

## What it shows

- Sidebar: a "New task" field on top (Return adds a `todo` task), then tasks grouped as In progress, Todo, Idea, Not now, Done. Rows read `#12 Title`; in-progress rows show the owner, others the first line of the note; high and low priority get a badge.
- Main: the selected task as a form (title, multi-line description, status, priority) with Start, Release and Done buttons, plus the owner and the last note.
- ⌘N inside the section, or "New Backlog Task" in the palette, focuses the new-task field. Delete is UI-only (row menu, confirmed).

Statuses: `idea`, `todo`, `in_progress`, `done`, `not_now`. Priorities: `high`, `normal`, `low`. Order is priority, then oldest first (done: newest first).

## Agent workflow

Agents use the MCP tools `backlog_list`, `backlog_get`, `backlog_add`, `backlog_update`, `backlog_claim`, `backlog_release`, `backlog_done`.

1. `backlog_list` with `status: todo`, take the first task (`backlog_get` for the full text).
2. `backlog_claim` it. If another owner holds it, the error says who; pick the next task.
3. Work. Record progress with `backlog_update` and a `note`.
4. Finish with `backlog_done` and a note naming the commit, or `backlog_release` (with a note) if you give up.

`backlog_list` defaults to `status: open` (idea + todo + in progress).

Only the user puts tasks into `todo`. A task an agent finds goes in with `backlog_add` as `idea`, and the user promotes it. Agents may not add or set `todo`, and cannot set `in_progress` through update (claim does that). To park a task use `not_now`.

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
```

Prefer the MCP tools inside an agent session: `cm command run` without `--agent` counts as the user.
