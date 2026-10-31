# Cron

The `cron` plugin calls an agent-visible command on a schedule: a job is "call command X with args Y on schedule Z" in one workspace. Agents manage jobs through MCP (`cron.*` tools); you can do the same from the CLI. The Cron section shows the workspace's jobs and their runs; it is for watching and managing, not for authoring.

## Job

| Field | Meaning |
|---|---|
| `id` | `cj_<8 hex>`. |
| `name` | Short human name. |
| `workspace` | The workspace the job belongs to and fires in. Jobs are dropped with their workspace. |
| `schedule` | One of `cron` (+ `timezone`), `every`, `at` (see below). |
| `command`, `args` | The command to call and its arguments. The command must be agent-visible and neither a `cron.*` one nor a `triggers.*` one other than `triggers.emit`; `args` (JSON text holding an object) are checked against its schema when the job is added or updated, so a typo fails at once, not at 9:00. |
| `enabled` | `false` while paused. |
| `owner` | The agent session the job belongs to, if any (see [Owner](#owner)). |
| `createdBy` | The agent session that added it; empty when added by the user or the CLI. |
| `missed` | `once` or `skip` (see [Missed runs](#missed-runs)). |
| `nextRun` | When it fires next. |
| `failures`, `lastError` | Errors in a row and the last error message. |
| `history` | The last 20 runs: `{at, status, reason?, result?}`, `status` `ok`, `error` or `skipped`; `result` is the command's result cut to 200 characters. Only `cron.show` returns it. |

## Schedules

Give exactly one of:

- `cron`: a 5-field expression (minute hour day month weekday), e.g. `0 9 * * 1-5`, in `timezone` (IANA, e.g. `Europe/Berlin`; default the system's local zone). A `TZ=` prefix is refused: use `timezone`.
- `every`: a Go duration, at least `1m`, e.g. `1m`, `15m`, `2h`; counted from when the job was added or resumed.
- `at`: an RFC 3339 time, e.g. `2026-10-31T09:00:00+01:00`; runs once.
- `in`: a Go duration from now, e.g. `30m`; stored as `at`, runs once.

A one-shot job (`at`, `in`) deletes itself when it runs; a skipped run (see below) keeps it and tries again on the next check.

## Firing

The plugin checks its jobs at least every 30 s and calls `command.run` with `agent: true`, so a job can only do what an agent may: a destructive command asks for your approval in the app when it fires. A job with an owner runs as that session, and only while the session is open. A job without an owner runs with no caller (an `agents.new` then starts a top-level session). A recurring job never runs twice at once.

A run is skipped, never counted as a failure, with one of these reasons:

| Reason | When |
|---|---|
| `busy` | An `agents.prompt` job whose target session is running or waiting for permission. |
| `no app` | The command needs an approval and no app is open to ask. |
| `missed` | The run is more than 1 minute late and the job has `missed: skip`. |
| `previous run still running` | The job's previous run has not returned yet. |
| `owner gone` | The job's owner session is closed or deleted; the job is about to be deleted with it. |
| `owner unknown` | The agents plugin can't be asked whether the job's owner session is open. |

Three errors in a row pause the job, keep the last error and post a `stuck` notification to the workspace. `cron.resume` clears the count and schedules the next run from now.

## Missed runs

When the Mac slept or Clubmate was down, a job that is more than 1 minute late follows its `missed` policy: `once` runs it once when Clubmate is back (not once per missed slot), `skip` records a `missed` skip and waits for the next slot. The default is `skip` for a job with an owner (a reminder to a session that has moved on) and `once` otherwise (the morning digest still runs at 9:40 if the Mac woke at 9:40). A one-shot job missed by more than 1 minute under `missed: skip` is deleted without running; `cron.run` on a one-shot does not use it up.

## Owner

A job added by an agent belongs to that agent session by default (`owner: "self"`) and is deleted when the session is closed or deleted. `owner: "none"` makes it outlive the session. Jobs added by the user or the CLI have no owner.

An agent may update, pause, resume, run and delete only jobs owned or created by itself or by its child sessions; an agent call without an agent session may not change any. The Cron section acts for you, so it can change any job.

## Commands

All are agent-visible and none is destructive (the fired command carries its own approval).

| Command | Does |
|---|---|
| `cron.add {name, command, args?, cron?, timezone?, every?, at?, in?, owner?, missed?}` | Adds a job and returns it. |
| `cron.list` | The workspace's jobs, without history. |
| `cron.show {id}` | One job with its history. |
| `cron.update {id, …}` | Changes the given fields; any schedule field replaces the whole schedule. |
| `cron.pause {id}`, `cron.resume {id}` | Stops and restarts firing. |
| `cron.run {id}` | Runs it now without changing the schedule; returns the run. |
| `cron.delete {id}` | Deletes it. |

## Examples

An orchestrator that started child sessions reminds itself every minute; the job goes away with its session:

```json
cron.add {
  "name": "Check children",
  "command": "agents.prompt",
  "args": "{\"session\":\"<your session id>\",\"text\":\"Check your children\"}",
  "every": "1m"
}
```

A morning digest in a new agent session every day at 09:00, kept after the session that added it is closed:

```json
cron.add {
  "name": "Good morning",
  "command": "agents.new",
  "args": "{\"prompt\":\"/good-morning\"}",
  "cron": "0 9 * * *",
  "owner": "none"
}
```

Emit an event every night and let [triggers](triggers.md) react to it:

```json
cron.add {
  "name": "Nightly",
  "command": "triggers.emit",
  "args": "{\"name\":\"nightly\"}",
  "cron": "0 2 * * *",
  "owner": "none"
}
```

## CLI

Inside a Clubmate terminal `$CLUBMATE_PROJECT_ID` names the workspace:

```sh
cm command run cron.add --args '{"name":"Good morning","command":"agents.new","args":"{\"prompt\":\"/good-morning\"}","cron":"0 9 * * *"}'
cm command run cron.list
cm command run cron.show id=cj_1a2b3c4d
cm command run cron.run id=cj_1a2b3c4d
```

A job added from the CLI has no owner and defaults to `missed: once`.

## Cron section

The sidebar lists the jobs by name with their schedule and next run (`every day 09:00 · next 09:00`), or `paused · <last error>`, and a `failed` badge when the last run failed. A row's context menu has Pause or Resume, Run Now and Delete. The main view shows the selected job: command and args, schedule, next run or paused state, missed policy, owner session and the run history, newest first.
