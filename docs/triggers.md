# Triggers

The `triggers` plugin calls an agent-visible command when a bus event arrives: a trigger is "on event X (with payload Y), call command Z" in one workspace. It is the event-driven sibling of [cron](cron.md); the two compose through `triggers.emit`, which cron can schedule. Agents manage triggers through MCP (`triggers.*` tools); you can do the same from the CLI. The Triggers section shows the workspace's triggers and their runs; it is for watching and managing, not for authoring.

## Trigger

| Field | Meaning |
|---|---|
| `id` | `tr_<8 hex>`. |
| `name` | Short human name. |
| `workspace` | The workspace the trigger belongs to and fires in. Triggers are dropped with their workspace. |
| `on` | The bus topic to listen to, e.g. `agents.turn`, or a prefix pattern, e.g. `event.*` (matches `event.a`, `event.a.b`). `*` alone is refused. |
| `match` | Optional payload filter (see [Filter](#filter)). |
| `command`, `args` | The command to call and its arguments. The command must be agent-visible and not a `triggers.*` or `cron.*` one; `args` (JSON text holding an object) are checked against its schema when the trigger is added or updated. String values may hold placeholders (see [Placeholders](#placeholders)). |
| `enabled` | `false` while paused. |
| `owner` | The agent session the trigger belongs to, if any (see [Owner](#owner)). |
| `createdBy` | The agent session that added it; empty when added by the user or the CLI. |
| `failures`, `lastError` | Errors in a row and the last error message. |
| `history` | The last 20 runs: `{at, topic, seq, status, reason?, result?}`, `topic` and `seq` of the event, `status` `ok`, `error` or `skipped`; `result` is the command's result cut to 200 characters. Only `triggers.show` returns it. |

## Filter

An event fires a trigger when all of these hold:

1. Its topic matches `on`.
2. It names the trigger's workspace in `workspace`, `projectId`, `project.id` or `session.projectId` (the first one present). An event naming no workspace (e.g. `agents.ready`, `session.killed`) fires no trigger.
3. Every key of `match`, a dotted path into the payload (e.g. `session`, `project.name`), is present and equals its value. An array value matches any of its elements: `{"status":["done","error"]}`.

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

An orchestrator hears when a child session ends its turn; the trigger goes away with the orchestrator's session:

```json
triggers.add {
  "name": "Child finished",
  "on": "agents.turn",
  "match": "{\"session\":\"<child id>\",\"status\":[\"done\",\"error\"]}",
  "command": "agents.prompt",
  "args": "{\"session\":\"<your session id>\",\"text\":\"Child {{payload.session}} is {{payload.status}}\"}"
}
```

Cron and triggers together: cron emits `event.nightly` at 02:00, and any number of triggers react to it:

```json
cron.add {"name": "Nightly", "command": "triggers.emit", "args": "{\"name\":\"nightly\"}", "cron": "0 2 * * *", "owner": "none"}
triggers.add {"name": "Nightly sweep", "on": "event.nightly", "command": "agents.new", "args": "{\"prompt\":\"Sweep the backlog\"}", "owner": "none"}
```

## CLI

Inside a Clubmate terminal `$CLUBMATE_PROJECT_ID` names the workspace:

```sh
cm command run triggers.add --args '{"name":"Ping","on":"event.ping","command":"notify.post","args":"{\"message\":\"pinged: {{payload.why}}\"}"}'
cm command run triggers.emit --args '{"name":"ping","payload":"{\"why\":\"test\"}"}'
cm command run triggers.show id=tr_1a2b3c4d
```

## Triggers section

The sidebar lists the triggers by name with `on <topic> <match>`, or `paused · <last error>`, and a `failed` badge when the last run failed. A row's context menu has Pause or Resume and Delete. The main view shows the selected trigger: topic and filter, command and args, state, owner session and the run history, newest first.
