# Triggers

The `triggers` plugin calls an agent-visible command when a bus event arrives: a trigger is "on event X (with payload Y), call command Z" in one workspace. It is the event-driven sibling of [cron](cron.md); the two compose through `triggers.emit`, which cron can schedule. [Agent graphs](agent-graphs.md) are built from triggers: each edge of a run is one. Agents manage triggers through MCP (`triggers.*` tools); you can do the same from the CLI. The Triggers section shows the workspace's triggers and their runs; it is for watching and managing, and its How triggers work page asks an agent to add one.

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
| `limit` | Runs a minute before the loop guard pauses it, 1–60; unset (or `0`) is 10 (see [Loops and limits](#loops-and-limits)). |
| `parallel` | `true` lets a run start while the previous one is still running; unset is `false`, and the event is skipped (see [Firing](#firing)). |
| `owner` | The agent session the trigger belongs to, if any (see [Owner](#owner)). |
| `createdBy` | The agent session that added it; `plugin:<id>` when a plugin added it on its own (e.g. `plugin:remote`); empty when added by the user or the CLI, and for a graph's edge the run's starter (a session, or empty for you). |
| `run`, `runTitle` | The graph run that made the trigger for one of its edges and that run's title; empty for other triggers. Only the Graphs plugin can set them; `triggers.add` drops them from any other caller, so an agent can't pass its trigger off as a graph's. The Triggers section groups and labels a run's triggers by them. |
| `failures`, `lastError` | Errors in a row and the last error message. |
| `history` | The last 100 runs: `{at, topic, seq, status, reason?, result?, chain?}`, `topic` and `seq` of the event, `status` `ok`, `error` or `skipped`; `result` is the command's raw result cut to 200 characters; `chain` is what led to the event (see [Chain](#chain)). Only `triggers.show` returns it. |
| `paused` | When it was paused, as for a [cron job](cron.md#job). |

## Filter

An event fires a trigger when all of these hold:

1. Its topic matches `on`.
2. It names the trigger's workspace in `workspace`, `workspaceId` or `session.workspaceId` (the first one present). An event naming no workspace (e.g. `agents.ready`, `session.killed`) fires no trigger.
3. Every key of `match`, a dotted path into the payload (e.g. `session`, `info.name`), is present and equals its value. An array value matches any of its elements: `{"status":["done","error"]}`.
4. It comes from its topic's owner (see [Sources](#sources)).

`match` is JSON text holding an object; `""` or `{}` matches every event on the topic.

## Sources

The core stamps every bus event with its `source`: `plugin:<id>`, `core`, `agent` (`notify.post` from an agent) or `cli` (anything else on the socket, `clubmate bus publish` included). The payload says nothing trustworthy, so a trigger fires only on an event from the owner of its topic, the topic's first segment:

| Topic | Fires only from |
|---|---|
| `event.*` | `plugin:triggers` (`triggers.emit`) |
| `event.graph.*` | also `plugin:graphs` (a graph step's outcome) |
| `workspace.*`, `session.*`, `ui.*` | `core` |
| `notify.*` | `agent`, a plugin (`notify.post`) or `cli` (`cm notify`) |
| `<id>.*`, e.g. `agents.turn`, `cron.ready`, `git.changed` | `plugin:<id>` |

So an agent's `clubmate bus publish agents.turn …` or `event.graph.<run>.<node> {"outcome":…}` fires nothing. `notify.*` is the one topic that fires from `cli` too, so a `cm notify` from a Claude Code hook in a terminal fires the same triggers as the `notify_post` MCP tool: any local process can already post a real notification through `cm notify`, so a forged `notify.posted` can do no more than a real one. An event from a source that does not own its topic is only logged to the plugin's stderr (`ignore <topic> from <source>: only its owner fires triggers`), not recorded in any trigger's history, and the plugin ignores it entirely, so a forged `agents.turn` can't release a held event either. To fire a trigger by hand use `triggers.emit` (`cm command run triggers.emit …`, see [CLI](#cli)) or Run now. Every process of your user can still reach the core socket and call `triggers.emit` itself (see [the protocol](sdui-protocol.md)); what this stops is an event passing for another plugin's.

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
| `previous run still running` | The trigger's previous run has not returned yet; the event is dropped, not queued. A `parallel` trigger never skips for this: each event starts its own run. |
| `busy` | An `agents.prompt` trigger whose target session is running or waiting for permission. The trigger holds the event (only the latest one, in memory, so not across a restart) and runs again with it when the target's turn ends (`agents.turn`); a run that finds the target busy again keeps holding it without a new history entry. |
| `no app` | The command needs an approval and no app is open to ask. |
| `owner gone` | The trigger's owner session is closed or deleted; the trigger is about to be deleted with it. |
| `owner unknown` | The agents plugin can't be asked whether the owner session is open. |
| `emitted by another agent session` | See [Emitted events](#emitted-events). |

## Emitted events

`triggers.emit` refuses a name under `graph.` from an agent session or a session's plain `cm` unless the name is `graph.<run>.<node>` of an agent node whose current session is the caller (`invalid_params: emit …: only the session of agent node … reports its outcome`; it asks `graphs.show` for the node), so no session can report another node's outcome or a step's. It sets the payload's `workspace` to the calling workspace and `by` to the calling agent session (`agent` for an agent call without one; no `by` for the user or the CLI), and `cronJob` to the cron job that emits it (only the cron plugin may pass `cronJob`; anyone else gets `invalid_params: only the cron plugin may set cronJob`, and a `by` or `cronJob` in `payload` is dropped). Only `triggers.emit` (and a graph step, for `event.graph.*`) publishes an `event.*` that fires triggers (see [Sources](#sources)), so `by` and `cronJob` are the plugin's own. An owned trigger runs as its owner, and some commands trust the caller (`agents.permission` and `agents.cancel` act only on the caller's descendants), so an owned trigger whose `args` use placeholders skips an `event.*` emitted by an agent session other than its owner or the owner's descendants. Placeholders otherwise copy event data verbatim: fill caller-checked fields such as `session` only from events you trust.

## Loops and limits

A trigger's command can publish the very event it listens to (`notify.post` on `notify.posted`, `agents.prompt` on `agents.turn` of the same session). Three guards stop that:

- `triggers.*` and `cron.*` commands cannot be targets, so a trigger can't emit events or create triggers or jobs. Cron, in turn, may schedule `triggers.emit` but no other `triggers.*` command.
- A trigger never runs twice at once unless it is `parallel` (see `previous run still running` above).
- A trigger that starts `limit` runs (10 unless set, at most 60) within 1 minute is paused at the next matching event, with the error `fired 10 times in 1m0s; paused to stop a loop` (its own `limit` in place of 10), and a `stuck` notification is posted to the workspace. A trigger that has to keep up with bursts, such as one per notification, sets a higher `limit` and `parallel`.

Three errors in a row also pause it, keep the last error and post a `stuck` notification. `triggers.resume` clears the count.

A trigger without an owner, other than a graph's (the graph run reports its own steps), posts each failed run to the inbox (kind `stuck`, `Trigger failed: <error>`, titled with its name); ok runs only stay in its history. The failure that pauses it posts only the paused notification.

## Chain

Each run keeps what led to its event as `chain`: `session` (and its `title`) for an `agents.*` event or an `event.*` emitted by an agent session, `turn` and `status` of an `agents.turn`, `session` (and its `title`) and `kind` of a `notify.*` event, and what started that session or emitted the event: `cronJob` (with `cronName`, from `cron.show`) or `trigger` (with `triggerName`), taken from the event and else from the session in `agents.sessions`. A Run now has `{user: true}`; an event that names none of these has no chain.

## Owner

A trigger added by an agent belongs to that agent session by default (`owner: "self"`) and is deleted when the session is closed or deleted. `owner: "none"` makes it outlive the session. Triggers added by the user, the CLI or a plugin have no owner. As with cron, a session a trigger with `owner: "none"` starts gets your AFK default only when you or a plugin made the trigger (`jobBy`).

An agent may update, pause, resume and delete only triggers owned or created by itself or by its child sessions, and any trigger a plugin added (`createdBy: plugin:<id>`), so you can change such a rule by asking any session; an agent call without an agent session may not change any. The Triggers section acts for you, so it can change any trigger.

## Commands

All are agent-visible and none is destructive (the fired command carries its own approval).

| Command | Does |
|---|---|
| `triggers.add {name, on, match?, command, args?, limit?, parallel?, owner?}` | Adds a trigger and returns it. `limit` outside 0–60 is refused with `limit: 1–60 runs a minute`. |
| `triggers.list` | The workspace's triggers, without history. |
| `triggers.show {id}` | One trigger with its history. |
| `triggers.update {id, name?, on?, match?, command?, args?, limit?, parallel?}` | Changes the given fields; `match: ""` drops the filter, `limit: 0` goes back to 10. |
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

The sidebar starts with How triggers work, then lists the triggers by name with what they wait for in words (`an agent turn finishes · status done · session Review loop · Coder`), or `paused · <last error>`; a trigger whose last run failed has a danger ✕ icon. Triggers a graph run created (`run`, `runTitle` set by `graphs.run`) are grouped under `from graph <title> · N`, labelled without the `<title>: ` prefix; selecting one (also through an edge in a flow or in Graphs) unfolds its group. A row's context menu has Run now (Run once while paused), Pause or Resume and Delete; for a graph's trigger Delete warns that the run stops at that step. The main view shows the selected trigger: a header with `active` or `paused` and `failed`, Run now (⌘R, fires it at once with an empty event, so its `{{…}}` placeholders are left empty) and Pause (P), Run now primary while active and Resume (P, first) primary with Run once while paused, neither for a graph's trigger, then Delete (⌘⌫) for the user's and agents' triggers, or `created by graph <title>` and Open graph (G, selects the run in Graphs) for a graph's, whose Delete (⌘⌫) is in the header's ••• menu with the warning that the run stops at that step; then When in words, Limits (`up to 60 runs a minute · runs overlap`, each part only when `limit` or `parallel` is set; no row without either), Owner (`graph <title>` for a graph's, `added by <plugin>` for a plugin's), Does in words with the prompt in a card clamped to 3 lines (Show all) and Show as JSON, and Runs as in a [cron job](cron.md#cron-section) (counts, results in words, identical runs folded, nothing marked and Show the latest run as JSON until you pick a run), with Open session on a failed run's Result (the session it prompted or started, else the one whose event fired it; only while that session is listed), and with an Event column that names the event and its chain in words (`Coder's turn 2 ended · done`, `“tidy.done” emitted by Nightly tidy · from cron Nightly tidy`, `Coder · done notification`, `run now by you`). An `agents.prompt` run reads `prompted <session> · its turn N started` (or `prompted <session>` when queued), naming the session its `{{payload.session}}` resolved to: each run keeps the target it prompted as `target`. Session and graph run ids, and a cron job in When, are shown as their names.

Under the header the trigger is drawn as an edge between what fires it and what it does:

- A graph's trigger (`run` set): `In graph <title> — this trigger is one of its edges` over the run's triggers drawn as one flow: a card per role (the sessions the run's triggers listen to and prompt, labelled without the `<title> · ` prefix), one out per way a role hands on (`done`, `error`, `asks`, an outcome) with the number of ok runs, and an edge per trigger, this one accent (`this trigger · fired ×N`), the others faint. From the run's shape (`graphs.show`, read once per run) it adds a start ring before each start node, step nodes as cards and the edges no trigger carries (into or out of a step) as dashed never-taken edges, and the outs that end the run (an `approved`) as never-taken edges into an end double ring. The shape is read once, so whether the run ended through that out is shown in Graphs only.
- A trigger on an emitted event (`event.*`) or on one session's turns: where its event came from last time (the newest run with a chain): `last time · cron <name> started the session that emitted it` over a clock marker for the cron job (or a card for the trigger that started the session), a card for the session (its state and when, one out for the event, `emits tidy.done` or `turn ends · done`, with how many runs it fired), the edge `this trigger`, and a card for what it does (the prompted or started session, or the command) in the last run's colour (`failed · <error>` in red). Without a chain yet it draws what it waits for → what it does.
- Any other trigger (any session's turns, notifications, another plugin's events) fires on events from any session, so it never draws the session that fired it last: it draws what it waits for → what it does, and its Runs lanes start at the event, not at a cron job or trigger behind the last one. The event card names the event once, from the trigger, and its out says what happens (`turn ends · done`, `emits tidy.done`, `posts done` for notifications of one kind and `posted` for several, a `notifications` card with a bell; the event's words from How triggers work under a card named by its group for a listed topic, e.g. `the branch or worktree changes` under `Git`, and `arrives` under a card named by the topic for any other).

Runs are drawn as lanes (`Runs · N`, foldable with `h`; the counts row is left out then): one lane for the cause (the graph role it listens to, the cron job or trigger behind its events, or the event) and one for the effect, each run a dot on the cause lane joined to a dot on the effect lane (`done → prompt`, red when the run failed; a skipped run has no effect dot); the newest hop is accent. The words table stays under them. Clicking a card opens what it names (a session in Agents, a trigger here), an edge of a graph's trigger selects that trigger, and a lanes dot selects its run in the table. How triggers work (selected while there are no triggers, or when you pick it) explains the section in place: a header with one line on what a trigger is and how it relates to cron and graphs, and Ask an agent to add one (⌘N, primary); a ghost When → Does flow; Can wait for, a table of the events a trigger can listen to here (agent turns, agent session changes, notifications, emitted events, graph step outcomes, git, backlog, presented docs, nvim reviews), each with what it can be filtered by, how many of this workspace's triggers listen to it (`Yours`) and an Ask an agent link (↩ on the selected row); then Can do, Only from now, Owner, Loops and Overlap in one line each. The event words are the ones When uses. Ask an agent (⌘N, `triggers.new`, also from the app menu anywhere in Triggers) starts an agent session titled `New trigger` that asks you what should happen, adds the trigger with `owner: "none"` so it outlives that session, and opens it in Agents; from an event row its prompt names that event. There are no templates and no New trigger form: agents and graph runs add triggers.
