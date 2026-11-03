# Backlog

The **Backlog** section is a per-workspace task list shared by you and your agents.

## What it shows

- Sidebar: a "New task" field on top (Return adds a `todo` task); once any task that isn't done has a tag, a row of tag chips (`tabs` `tags`, action `filter`: `all` "All" with the task count, then `tag:<tag>` per tag of the tasks that aren't done with their count, most used first, ties A–Z; scrolls sideways when it doesn't fit, keeping the selected chip in view) that keeps only the tasks with that tag until the plugin restarts or the tag is gone; then tasks grouped as In progress, Todo, Idea, Not now, Done. Rows read `#12 Title`; in-progress rows show the owner, others the first line of the note; high and low priority get a badge.
- Main: the task as an autosaving form. Its `header` is the title, edited in place (click it or ↩ on it; ↩ or leaving saves, Esc cancels; a bad title shows its error under it), the status glyph, `#12` with the owner, the last owner and the note as the muted meta, then the actions. The primary one follows the owner (the plugin decides it, the client only draws it):
  - you own it (Todo or Idea, or In progress by you, or by a closed session): **Start agent** (⌘↩), labelled with what it passes, e.g. `Start agent · description, 1 link, 1 file`; In progress by you adds Release, by a closed session Take back;
  - an open agent session owns it: **Open ‹session›** (⌘↩) selects it in Agents; a session that is a node of a graph run reads **Open ‹run title›** and selects the run in Graphs (`graphs.reveal`); Take back next to it;
  - an external agent (an `owner` name) owns it: Take back only.

  Done (D) and Delete (⌘⌫, confirmed) follow. Status, priority, tags (one field, comma- or space-separated; a bad tag shows its error under it) and the description follow, with a "✓ Saved" button (⌘S saves at once).
- Links and Attachments sit under the form, each a muted `Links · N` / `Attachments · N` row, a `list` of the entries and an add field. A link row reads `#8 Record missed runs` with `blocked by · done` and the other task's status glyph; clicking it selects that task, its menu has Remove link. The add field takes `blocks #3`, `blocked by #5`, `relates #7` (or `relates to`, `blocked_by`; a bare `#7` relates) and shows a bad one as its error. An attachment row shows its title or ref with `worktree`, `image`, `artifact` or `link` and Remove attachment in its menu; the add field takes a worktree path or a URL (a claude.ai `/artifact/` URL or an `.html` path is an artifact, an image extension an image, other URLs a link, other paths a file). Files can't be dropped or pasted yet.
- Take back makes you the owner and keeps the task In progress; the session (or agent) that had it becomes the last owner.
- A non-empty description shows rendered under the header (a `document` node, `size: fit`, rendered by `pkg/docrender`: tables, highlighted code, mermaid diagrams; `http(s)` links open outside the app; `#n` of an existing task, outside code and links, is a task link `clubmate-doc:/task/<n>` with the task's title as tooltip, and clicking it selects that task) and leaves the form; "Edit description" swaps it for the multi-line field and "Done editing" back. The flag is per workspace and holds only for the selected task. An empty description is the field. A change from anywhere (an agent, the phone) re-renders it on `backlog.changed`.
- Without tasks the main pane is an `empty` "Nothing in the backlog" with one sentence on what tasks and Start agent are, and a New task field with Add under it.
- Companion (iPhone/iPad): a Backlog item under each Mac with the same groups (Not now and Done folded), New Task (title, description, priority, status), and a task screen with Start, Release, Done, Move to and Edit. The task screen draws the description from `descriptionHtml` (`backlog.get`, rendered without task links) with the shared document web view; a Mac that sends none gets it as plain Markdown. It acts as you (owner `user`) and refreshes on `backlog.changed`. It shows no links, attachments or owners yet, and its Start claims the task for you instead of starting a session.
- ⌘N inside the section, or "New backlog task" in the palette, focuses the new-task field. Delete is UI-only (row menu, confirmed).

Statuses: `idea`, `todo`, `in_progress`, `done`, `not_now`. Priorities: `high`, `normal`, `low`. Tags: up to 10 per task, each one word of letters, digits, `-`, `_`, `.` or `/` and up to 32 characters, stored lowercase without a leading `#`, duplicates dropped; `backlog_update` with `tags` replaces them (`[]` clears). The phone shows no tags yet. Order is priority, then oldest first (done: newest first).

Links: `links` `[{kind, task}]` with kind `blocked_by` (this task waits for the other), `blocks` or `relates`, at most 20, one per other task, never to itself or to a task that doesn't exist. Links stay symmetric: setting `A blocks B` gives B `blocked_by A`, `relates` gives `relates` back, removing one removes the other, and deleting a task removes the links to it. Attachments: `attachments` `[{kind, ref, title?}]` with kind `file` (a path relative to the worktree, no `..`), `image` or `artifact` (a worktree path or an `http(s)` URL) or `url` (`http(s)`), at most 20. `backlog_update` with `links` or `attachments` replaces the list (`[]` clears).

## Start agent

Start agent (⌘↩) calls `agents.new` with the task title as the session title and a first prompt of `Backlog task #n: <title>`, the description, the links (`- blocked by #8 Record missed runs (done)`), the attachments (`- file design/check/fix-list.md`) and a closing line telling the session it owns the task and finishes with `backlog_done` or `backlog_release`. The new session (top level, in the workspace's first git folder) then owns the task: In progress, claimed by it, owner move `started`. It works on Todo and Idea, and on In progress while you or a closed session own it.

## Agent workflow

Agents use the MCP tools `backlog_list`, `backlog_get`, `backlog_add`, `backlog_update`, `backlog_claim`, `backlog_release`, `backlog_done`.

1. `backlog_list` with `status: todo`, take the first task (`backlog_get` for the full text).
2. `backlog_claim` it. If another owner holds it, the error says who; pick the next task.
3. Work. Record progress with `backlog_update` and a `note`.
4. Finish with `backlog_done` and a note naming the commit, or `backlog_release` (with a note) if you give up.

`backlog_list` defaults to `status: open` (idea + todo + in progress); `tag` keeps only tasks with that tag. `backlog.get` from the app or the phone also returns `descriptionHtml`, the description rendered; agents don't get it.

A task an agent finds goes in with `backlog_add` as `idea` by default; an agent may add or move a task to `todo` (e.g. when the user asks it to). Nobody sets `in_progress` through update (claim does that). To park a task use `not_now`.

## Owners

Every task keeps its owner history, `owners` in `backlog_get`: moves `{owner, title?, run?, runTitle?, at, why}`, oldest first, at most 100 (the first kept). A new task starts with you (`created`); `started` (Start agent), `claimed` (backlog_claim, also a stale take-over) move it to the claimer; `released`, `done`, `taken back` and `stopped` move it back to you. A move to the owner the task already has is not recorded. A session owner stores its title and, when it is a graph run's node, the run's id and title. Tasks from before the history have none stored and read as you since creation, then the last owner and the current one (no migration). `backlog_list` leaves `owners` out.

- Hosted agent session (`agt_…`): the owner is that session; any `owner` argument is ignored.
- External agent (for example Claude Code connected with `clubmate mcp` in a terminal): pass `owner`, a name unique to you such as your session id; `user`, `agent` and names starting with `agt_` are rejected. Its claims never go stale.
- You (UI, palette, `cm command run` without `--agent`): the owner is `user`.

Release is allowed to the owner, an ancestor agent session of the owner (an orchestrator closing its children's tasks) and the user. Done is allowed to them too, and to anyone when the task is unclaimed or its claim is stale. A stale claim is taken over with claim. A task claimed by someone else can only get a new title, description, priority or note from other agents, not a new status.

## Stale claims

When an agent session that owns an In progress task is closed or deleted (`agents.changed`), the task goes back to the status it was claimed from and to you (owner move `stopped`, the session as `lastClaimedBy`). Nothing else changes status on its own. A claim by an `agt_…` session is stale when that session is closed or no longer listed (for instance when the backlog plugin missed the close). The UI shows it as "<title> (closed)" so you can mark the task done or move it back. Claiming a task with a stale claim takes it over, and the previous owner is kept as `lastClaimedBy`. If the agents plugin can't be asked, claims are treated as live.

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
cm command run backlog.update --agent id=12 'links=[{"kind":"blocked_by","task":8}]'
cm command run backlog.update --agent id=12 'attachments=[{"kind":"file","ref":"design/check/fix-list.md"}]'
```

An array of objects goes to `cm command run` as one JSON array.

Prefer the MCP tools inside an agent session: `cm command run` without `--agent` counts as the user.
