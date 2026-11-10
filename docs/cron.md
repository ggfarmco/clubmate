# Cron

The `cron` plugin calls an agent-visible command on a schedule: a job is "call command X with args Y on schedule Z" in one workspace. Agents manage jobs through MCP (`cron.*` tools); you can do the same from the CLI. The Cron section shows the workspace's jobs and their runs; it is for watching and managing, and its How cron works page asks an agent to add one.

## Job

| Field | Meaning |
|---|---|
| `id` | `cj_<16 hex>`. |
| `name` | Short human name. |
| `workspace` | The workspace the job belongs to and fires in. Jobs are dropped with their workspace. |
| `schedule` | One of `cron` (+ `timezone`), `every`, `at` (see below). |
| `command`, `args` | The command to call and its arguments. The command must be agent-visible and neither a `cron.*` one nor a `triggers.*` one other than `triggers.emit`; `args` (JSON text holding an object) are checked against its schema when the job is added or updated, so a typo fails at once, not at 9:00. |
| `enabled` | `false` while paused. |
| `owner` | The agent session the job belongs to, if any (see [Owner](#owner)). |
| `createdBy` | The agent session that added it; `plugin:<id>` when a plugin added it; empty when added by the user or the CLI. |
| `missed` | `once` or `skip` (see [Missed runs](#missed-runs)). |
| `nextRun` | When it fires next. |
| `failures`, `lastError` | Errors in a row and the last error message. |
| `history` | The last 100 runs: `{at, status, reason?, result?}`, `status` `ok`, `error` or `skipped`; `result` is the command's raw result cut to 200 characters. Only `cron.show` returns it. |
| `paused` | When it was paused: `[{from, to?, reason?}]`, `to` empty while still paused, `reason` empty when you or an agent paused it, else the error that paused it. Spans that ended before the oldest kept run are dropped. |

## Schedules

Give exactly one of:

- `cron`: a 5-field expression (minute hour day month weekday), e.g. `0 9 * * 1-5`, in `timezone` (IANA, e.g. `Europe/Berlin`; default the system's local zone). A `TZ=` prefix is refused: use `timezone`.
- `every`: a Go duration, at least `1m`, e.g. `1m`, `15m`, `2h`; counted from when the job was added or resumed.
- `at`: an RFC 3339 time, e.g. `2026-10-31T09:00:00+01:00`; runs once.
- `in`: a Go duration from now, e.g. `30m`; stored as `at`, runs once.

A one-shot job (`at`, `in`) deletes itself when it runs; a skipped run (see below) keeps it and tries again on the next check.

Times are wall-clock times. `every` and `in` don't stop while the Mac sleeps: a run due during sleep is late when the Mac wakes, and `missed` decides whether it still runs. A `cron` expression fires once per wall-clock time it names in its zone, also on the nights the clocks change. A time the spring change skips fires when the clock jumps: in `Europe/Berlin`, `0 2 * * *` runs at 03:00 that night, and `*/30 * * * *` runs at 03:00 instead of 02:00 and 02:30. A time the autumn change repeats fires on its first pass only: `0 2 * * *` runs at 02:00 summer time, and a job that runs several times an hour skips the repeated hour.

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

A job without an owner (`nobody`) has no session to report to, so its failed runs (kind `stuck`, `Cron job failed: <error>`) and missed runs (kind `info`, `Cron job missed a run: …`) are posted to the inbox, titled with the job's name; ok runs only stay in its history. The failure that pauses the job posts only the paused notification.

## Missed runs

When the Mac slept or Clubmate was down, a job that is more than 1 minute late follows its `missed` policy: `once` runs it once when Clubmate is back (not once per missed slot), `skip` records a `missed` skip and waits for the next slot. The default is `skip` for a job with an owner (a reminder to a session that has moved on) and `once` otherwise (the morning digest still runs at 9:40 if the Mac woke at 9:40). A one-shot job missed by more than 1 minute under `missed: skip` is deleted without running; `cron.run` on a one-shot does not use it up.

## Owner

A job that calls `triggers.emit` names itself in the event (`cronJob` in the payload), and a session a job starts remembers the job (`cronJob` in `agents.sessions` and on its `agents.turn`), so a trigger's runs can say which job led to them (see [Triggers](triggers.md#triggers-section)).

A job added by an agent belongs to that agent session by default (`owner: "self"`) and is deleted when the session is closed or deleted. `owner: "none"` makes it outlive the session. Jobs added by the user, the CLI or a plugin have no owner. A job's `agents.new` also passes who made it (`jobBy`): a session a job with `owner: "none"` starts is yours, with the AFK default, only when you or a plugin made the job; one an agent made starts as that agent's (see [agents § Orchestration](agents.md), `startedBy`). While an enabled one-shot job prompts its own owner, that session shows as running even after its turn ended (see [agents § Status](agents.md#status)). In the Cron section, "Show Session" next to the owner selects that session in Agents.

An agent may update, pause, resume, run and delete only jobs owned or created by itself or by its child sessions, and any job a plugin added (`createdBy: plugin:<id>`); an agent call without an agent session may not change any. The Cron section acts for you, so it can change any job.

## Commands

All are agent-visible and none is destructive (the fired command carries its own approval).

| Command | Does |
|---|---|
| `cron.add {name, command, args?, cron?, timezone?, every?, at?, in?, owner?, missed?}` | Adds a job and returns it. |
| `cron.list` | The workspace's jobs, without history. |
| `cron.show {id}` | One job with its history. |
| `cron.update {id, …}` | Changes the given fields; any schedule field replaces the whole schedule. |
| `cron.pause {id, reason?}`, `cron.resume {id}` | Stops and restarts firing; a `reason` shows with the job. |
| `cron.jobs-in {worktrees: [{path, branch, repo?, first?}], first?}` | Hidden, for sheets that take worktrees away: the jobs that run there, `{jobs: [{id, name, enabled, in?}]}`, `in` the worktree that holds the job. A job runs there when its owner session does, when it is an `agents.new` job whose `repo` is one of them, or when its `branch` is a worktree's and its `repo` that worktree's `repo` (a job with no `repo`, owned or not, counts for a worktree marked `first`, the workspace's first git folder). `first` says the worktrees are those of the folder `agents.new` starts in when no `repo` names one, the workspace's first git folder, so every `agents.new` job with no `repo` and no owner counts too, with or without a `branch` and whether or not that worktree exists: it would otherwise move to another repo silently. Without `first` such a job counts for no folder, however its `branch` matches. |
| `cron.run {id}` | Runs it now without changing the schedule; returns the run. |
| `cron.delete {id}` | Deletes it. |

## Examples

An agent waiting on CI reminds itself in 10 minutes; the job goes away with its session. Children need no such job: Clubmate prompts the orchestrator when a child's turn ends.

```json
cron.add {
  "name": "Check CI",
  "command": "agents.prompt",
  "args": "{\"session\":\"<your session id>\",\"text\":\"Check the CI run\"}",
  "in": "10m"
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

Inside a Clubmate terminal `$CLUBMATE_WORKSPACE_ID` names the workspace:

```sh
cm command run cron.add --args '{"name":"Good morning","command":"agents.new","args":"{\"prompt\":\"/good-morning\"}","cron":"0 9 * * *"}'
cm command run cron.list
cm command run cron.show id=cj_1a2b3c4d
cm command run cron.run id=cj_1a2b3c4d
```

A job added from the CLI has no owner and defaults to `missed: once`.

## Cron section

The sidebar starts with How cron works, then lists the jobs by name with their next run at the right (`09:00` within 24 h, else `Nov 2 03:00`) or `paused`; a job whose last run failed has a danger ✕ icon. A row's context menu has Run now (Run once while paused), Pause or Resume, Edit, Duplicate and Delete. The main view shows the selected job: a header with `active` or `paused`, `failed` and `missed N runs` (warning) badges and the primary action first: Run now (⌘R) then Pause (P) while active, Resume (P) then Run once (⌘R) while paused; then Edit (E), Duplicate, Show session (when the owner is listed) and Delete (⌘⌫) last; then Schedule, Next run with the relative time (`Sun, Nov 1 · 03:00 · in 13 h`) or `none while paused` with who paused it and when (`paused by you, Oct 30`, or `paused, Oct 30 · <error>`), When missed (a picker: Run once when Clubmate is back or Skip it and wait for the next run; a change saves at once), Worktree (for `agents.new`), Owner (`nobody — failures go to the inbox` without one), then Does drawn: the schedule (`every 2m`) leads to a card for what the job does (`Start an agent` · `Claude · new session`, `Prompt a session` · `<session>`, `Emit an event` · `<name>`, any other command by its title from its plugin's manifest in sentence case, `List backlog tasks` · `plugin command`; the command id only in Show as JSON) with an `ok` and a `failed` port counting the kept runs, and from them to where results go: both to the owner session, or for a job without owner `ok` to Runs, below and `failed` to the Inbox. A job that prompts a session draws that session as a second card (its title, status and `prompted Nov 3`), which `ok` leads to (and `failed` too when it is the owner); clicking it opens the session. While paused the `ok` edge reads `paused`. The newest ok or failed run's port and edge are in accent, ports without runs empty, everything faint while paused. Under it, full width, the prompt in a card clamped to 3 lines (Show all) and Show as JSON for the raw command and args; after runs were skipped as missed, a warning banner `Missed Tue, Nov 3 · 16:26 — not run: Clubmate was closed or the Mac asleep` with Run it now at its right; then Runs. Everything under the header scrolls.

Runs is a foldable row (`Runs · 21`, `h` or a click folds the axis; each job keeps its own fold until the app quits) with the counts at its right (`● ok 19 · × failed 1 · ○ missed 1`), the runs on a time axis (teal ok, red ✕ failed, amber ring missed, faint dot skipped; a vertical line at now, a dashed ring at the next run while active, the paused spans hatched; hover for the time, status and result; click a run, or ←/→, to select it) and, under a folded `Show N runs` row (`Hide runs` while open; folded again when you pick another job, opened by picking a run on the axis), lists them newest first: When, Status (`ok` teal, `failed` red, `missed` amber, `skipped`) and Result in words, never `null` or a JSON blob: `agents.new` reads `started <session>`, `agents.prompt` `prompted <session> · its turn N started` (just `prompted <session>` when the prompt was queued or joined a running turn), `triggers.emit` `emitted “<name>”`, a result object with a `text`, `message` or `summary` field that text's first line, an empty result (`null`, `{}`) the action in words (`List backlog tasks · done`), any other result `ok`; a failure its error without the RPC code (`GitHub answered 403`; a deleted or closed target session reads `its session was deleted` or `its session is closed`), a skip why it did not run (`not run — Clubmate was closed or the Mac asleep; the policy skips it`). A failed run's Result ends in Show log (or ↩ on its row), which selects it and opens its JSON: a run keeps no log of its own, its stored record (with the error) is what there is. Consecutive runs with the same status and result fold into one row (`16:20 – 15:46 · ok ×18 · same result: main is green`, `Nov 3 – Nov 1` when they span days) that opens to its runs. Until you click a run, nothing is marked and Show the latest run as JSON shows the newest one (so opening a job does not jump to the table); click a run on the axis or in the table (or a folded row, for its newest run) and Show the selected run as JSON (under the table) shows it as stored, with the raw result; the axis and the table mark the same run. Duplicate adds a paused copy named `<name> copy` and selects it. How cron works (selected while there are no jobs, or when you pick it) explains the section in place: a header with one line on what a cron job is and how it relates to triggers (a job does something on a clock, triggers react to events; a job can start an agent, prompt a session, run a graph or emit an event a trigger listens to), and Ask an agent to add one (⌘N, primary); a ghost of Does (a schedule → what it does; where results go is the Owner fact); Can run on, a table of the schedules (on a calendar: `cron` in a time zone; on an interval: `every`, a minute or more; once: `at` or `in`), each with an example, how many of this workspace's jobs use it (`Yours`; `in` jobs are stored as `at`, so the two share a row) and an Ask an agent link (↩ on the selected row); then Can do, Missed runs, Owner, Worktree, Overlap and Failures in one line each. Ask an agent (⌘N, `cron.new`, also from the app menu anywhere in Cron) starts an agent session titled `New cron job` that asks you what the job should do and when, adds it with `owner: "none"` so it outlives that session, and opens it in Agents; from a schedule row its prompt names that schedule. There is no create form or template: agents add jobs.

Edit (E, or Edit in the row's menu) turns the main view into a form: Name, Schedule (one field: a 5-field cron expression, an interval such as `15m` or `every 15m`, or an RFC 3339 time for a one-off run), Time zone (used by cron expressions), Prompt for `agents.new` and `agents.prompt` jobs (and any job whose args have a `prompt` or `text`) or Arguments as JSON for other commands, Worktree (the branch of a new worktree, `agents.new` only; empty runs in the main checkout or the owner's worktree) and When missed. The command itself is not editable. Save (⌘S) sends the changed fields through `cron.update` (an unchanged schedule keeps its next run); an error stays under the form in danger ink with the typed values kept. Cancel (E) closes the form without saving; selecting another job closes it too. Session and graph run ids are shown as their titles.
