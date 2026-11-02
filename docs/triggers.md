# Triggers

The `triggers` plugin calls an agent-visible command when a bus event arrives: a trigger is "on event X (with payload Y), call command Z" in one workspace. It is the event-driven sibling of [cron](cron.md); the two compose through `triggers.emit`, which cron can schedule. [Agent graphs](agent-graphs.md) are built from triggers: each edge of a run is one. Agents manage triggers through MCP (`triggers.*` tools); you can do the same from the CLI. The Triggers section shows the workspace's triggers and their runs; it is for watching and managing, not for authoring.

## Trigger

| Field | Meaning |
|---|---|
| `id` | `tr_<16 hex>`. |
| `name` | Short human name. |
| `workspace` | The workspace the trigger belongs to and fires in. Triggers are dropped with their workspace. |
| `on` | The bus topic to listen to, e.g. `agents.turn`, or a prefix pattern, e.g. `event.*` (matches `event.a`, `event.a.b`), or a comma-separated list of those, e.g. `agents.turn,event.*`. `*` alone is refused. |
| `match` | Optional payload filter (see [Filter](#filter)). |
| `command`, `args` | The command to call and its arguments. The command must be agent-visible and not a `triggers.*` or `cron.*` one; `args` (JSON text holding an object) are checked against its schema when the trigger is added or updated. String values may hold placeholders (see [Placeholders](#placeholders)). |
| `enabled` | `false` while paused. |
| `owner` | The agent session the trigger belongs to, if any (see [Owner](#owner)). |
| `createdBy` | The agent session that added it; empty when added by the user or the CLI. |
| `run`, `runTitle` | The graph run that made the trigger for one of its edges and that run's title; empty for other triggers. Only the Graphs plugin can set them; `triggers.add` drops them from any other caller, so an agent can't pass its trigger off as a graph's. The Triggers section groups and labels a run's triggers by them. |
| `failures`, `lastError` | Errors in a row and the last error message. |
| `history` | The last 20 runs: `{at, topic, seq, status, reason?, result?}`, `topic` and `seq` of the event, `status` `ok`, `error` or `skipped`; `result` is the command's result cut to 200 characters. Only `triggers.show` returns it. |

## Filter

An event fires a trigger when all of these hold:

1. Its topic matches `on`.
2. It names the trigger's workspace in `workspace`, `workspaceId` or `session.workspaceId` (the first one present). An event naming no workspace (e.g. `agents.ready`, `session.killed`) fires no trigger.
3. Every key of `match`, a dotted path into the payload (e.g. `session`, `info.name`), is present and equals its value. An array value matches any of its elements: `{"status":["done","error"]}`.

`match` is JSON text holding an object; `""` or `{}` matches every event on the topic.

## Placeholders

String values in `args` may hold, filled from the event when the trigger fires:

| Placeholder | Value |
|---|---|
| `{{topic}}` | The event topic. |
| `{{seq}}` | The event's bus sequence number. |
| `{{payload}}` | The whole payload as JSON. |
| `{{payload.<path>}}` | A payload field: a string as is, anything else as JSON. |

A placeholder whose field is not in the event makes the run an `error`.

## Firing

The plugin subscribes to the bus when it starts and sees only events published after that: events from while Clubmate was down are not replayed. A matching event calls `command.run` with `agent: true`, so a trigger can only do what an agent may: a destructive command asks for your approval in the app. A trigger with an owner runs as that session, and only while the session is open; one without an owner runs with no caller.

A run is skipped, never counted as a failure, with one of these reasons:

| Reason | When |
|---|---|
| `previous run still running` | The trigger's previous run has not returned yet; the event is dropped, not queued. |
| `busy` | An `agents.prompt` trigger whose target session is running or waiting for permission. The trigger holds the event (only the latest one, in memory, so not across a restart) and runs again with it when the target's turn ends (`agents.turn`); a run that finds the target busy again keeps holding it without a new history entry. |
| `no app` | The command needs an approval and no app is open to ask. |
| `owner gone` | The trigger's owner session is closed or deleted; the trigger is about to be deleted with it. |
| `owner unknown` | The agents plugin can't be asked whether the owner session is open. |
| `emitted by another agent session` | See [Emitted events](#emitted-events). |

## Emitted events

`triggers.emit` sets the payload's `workspace` to the calling workspace and `by` to the calling agent session (`agent` for an agent call without one; no `by` for the user, the CLI or a cron job without owner). An owned trigger runs as its owner, and some commands trust the caller (`agents.permission` and `agents.cancel` act only on the caller's descendants), so an owned trigger whose `args` use placeholders skips an `event.*` emitted by an agent session other than its owner or the owner's descendants. Placeholders otherwise copy event data verbatim: fill caller-checked fields such as `session` only from events you trust.

## Loops and limits

A trigger's command can publish the very event it listens to (`notify.post` on `notify.posted`, `agents.prompt` on `agents.turn` of the same session). Three guards stop that:

- `triggers.*` and `cron.*` commands cannot be targets, so a trigger can't emit events or create triggers or jobs. Cron, in turn, may schedule `triggers.emit` but no other `triggers.*` command.
- A trigger never runs twice at once (see `previous run still running` above).
- A trigger that starts 10 runs within 1 minute is paused at the 11th matching event, with the error `fired 10 times in 1m0s; paused to stop a loop`, and a `stuck` notification is posted to the workspace.

Three errors in a row also pause it, keep the last error and post a `stuck` notification. `triggers.resume` clears the count.

## Owner

A trigger added by an agent belongs to that agent session by default (`owner: "self"`) and is deleted when the session is closed or deleted. `owner: "none"` makes it outlive the session. Triggers added by the user or the CLI have no owner.

An agent may update, pause, resume and delete only triggers owned or created by itself or by its child sessions; an agent call without an agent session may not change any. The Triggers section acts for you, so it can change any trigger.

## Commands

All are agent-visible and none is destructive (the fired command carries its own approval).

| Command | Does |
|---|---|
| `triggers.add {name, on, match?, command, args?, owner?}` | Adds a trigger and returns it. |
| `triggers.list` | The workspace's triggers, without history. |
| `triggers.show {id}` | One trigger with its history. |
| `triggers.update {id, name?, on?, match?, command?, args?}` | Changes the given fields; `match: ""` drops the filter. |
| `triggers.pause {id}`, `triggers.resume {id}` | Stops and restarts firing. |
| `triggers.delete {id}` | Deletes it. |
| `triggers.emit {name, payload?}` | Publishes `event.<name>` with `payload` (JSON text holding an object), its `workspace` set to this workspace and `by` to the caller (see [Emitted events](#emitted-events)); returns `{topic, seq}`. |

## Examples

A reviewer session looks at a coder session's work each time the coder ends a turn; the trigger goes away with the session that added it. An orchestrator needs no trigger for its own children: Clubmate prompts it when a child's turn ends.

```json
triggers.add {
  "name": "Review coder",
  "on": "agents.turn",
  "match": "{\"session\":\"<coder id>\",\"status\":\"done\"}",
  "command": "agents.prompt",
  "args": "{\"session\":\"<reviewer id>\",\"text\":\"Review what {{payload.session}} just did\"}"
}
```

A release after every nightly run: the cron job starts the session in AFK mode, and the trigger fires once that session (with all its children) is done, whichever session id it got that night. A parent ends many turns while it waits for its children; only the last one is `finished`.

```json
cron.add {"name": "Nightly tidy", "command": "agents.new", "args": "{\"prompt\":\"Tidy\",\"afk\":true}", "cron": "0 2 * * *", "owner": "none"}
triggers.add {"name": "Nightly release", "on": "agents.turn", "match": "{\"cronJob\":\"cj_…\",\"finished\":true}", "command": "agents.new", "args": "{\"prompt\":\"Release if {{payload.session}} changed main\",\"afk\":true}", "owner": "none"}
```

Cron and triggers together: cron emits `event.nightly` at 02:00, and any number of triggers react to it:

```json
cron.add {"name": "Nightly", "command": "triggers.emit", "args": "{\"name\":\"nightly\"}", "cron": "0 2 * * *", "owner": "none"}
triggers.add {"name": "Nightly sweep", "on": "event.nightly", "command": "agents.new", "args": "{\"prompt\":\"Sweep the backlog\"}", "owner": "none"}
```

## CLI

Inside a Clubmate terminal `$CLUBMATE_WORKSPACE_ID` names the workspace:

```sh
cm command run triggers.add --args '{"name":"Ping","on":"event.ping","command":"notify.post","args":"{\"message\":\"pinged: {{payload.why}}\"}"}'
cm command run triggers.emit --args '{"name":"ping","payload":"{\"why\":\"test\"}"}'
cm command run triggers.show id=tr_1a2b3c4d
```

## Triggers section

The sidebar lists the triggers by name with what they wait for in words (`an agent turn finishes · status done · session Review loop · Coder`), or `paused · <last error>`; a trigger whose last run failed has a danger ✕ icon. Triggers a graph run created (`run`, `runTitle` set by `graphs.run`) are grouped under `from graph <title> · N`, labelled without the `<title>: ` prefix. A row's context menu has Run now, Pause or Resume and Delete; for a graph's trigger Delete warns that the run stops at that step. The main view shows the selected trigger: a header with `active` or `paused` and `failed`, Run now (⌘R, fires it at once with an empty event, so its `{{…}}` placeholders are left empty) and Pause (P), then Delete (⌘⌫) for the user's and agents' triggers, or `created by graph <title>` and Open graph (G, selects the run in Graphs) for a graph's, whose Delete (⌘⌫) is in the header's ••• menu with the warning that the run stops at that step; then When in words, Owner (`graph <title>` for a graph's), Does in words with the prompt in a card clamped to 3 lines (Show all) and Show as JSON, and the run history, newest first. Session and graph run ids are shown as their titles.
