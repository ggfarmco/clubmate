# Keep awake

The `awake` plugin keeps the Mac from sleeping, like Amphetamine or `caffeinate`. It is the one place that decides whether the Mac stays awake: the Mac app, the phone and agents only ask it and show what it says. The core and the plugin outlive the app, so the Mac stays awake with the app closed, while the phone drives agents.

## Holds

The Mac stays awake while at least one hold is on:

| Kind | Who | Ends |
|---|---|---|
| `manual` | You: the status bar menu, Settings → Keep Awake, the phone, `cm command run awake.set`. | When you turn it off, or at its `until` (1 hour, 4 hours, or never). |
| `agents` | The rule "while agents work" (off by default; turn it on with While Agents Work in the status bar menu, in Settings → Keep Awake or with `awake.settings-set`): any agent session in any workspace is `active` in `agents.sessions` (running or waiting for you, not a service session; the same sessions as the Mac status bar's "N agents"). | When no session is active. |
| `agent` | An agent: `awake.hold` (MCP `awake_hold`), e.g. for a long build. | `awake.release`, its `minutes` (default 60, at most 720), the process `pid` it named exiting, or its session closing or being deleted. |

The plugin re-reads `agents.sessions` of every workspace when it starts, when the agents plugin restarts (`agents.ready`) and on `agents.changed` for a workspace (that workspace only), and every minute while the rule is on, so a missed or out-of-order event is right again within a minute. Deadlines are checked against the wall clock every 5 seconds, so a timer that stalled across a sleep ends on the next check.

## How it holds

While any hold is on the plugin itself holds IOKit power assertions named "Clubmate keep awake" (`IOPMAssertionCreateWithName`, the API under `caffeinate`; no child process), and releases them when none is. macOS drops a process's assertions when it exits, so a crashed or killed plugin never leaves the Mac awake; the supervisor's restart reloads the stored holds and asserts again. A change of settings releases them and asserts the new set. `pmset -g assertions` lists them under the plugin's pid.

- `PreventSystemSleep` keeps the Mac awake on power (macOS ignores it on battery).
- `PreventUserIdleSystemSleep` also keeps it awake on battery. A `manual` or `agent` hold always adds it; the `agents` rule only with "On battery too" on (off by default), so an agent question left overnight does not drain a laptop.
- `PreventUserIdleDisplaySleep` keeps the screen on too, with "Keep the screen on" (off by default).

A closed lid still sleeps a laptop on battery, whatever the holds; on power it stays awake with the lid closed only with an external display, keyboard and mouse (clamshell mode). The detail line says so.

## State

`awake.state` (scope app, `remote`, agent-visible) returns the state; every change publishes the same object on the bus topic `awake.changed`, which `remote` forwards to the phone:

```json
{
  "on": true,
  "text": "Awake · 2 agents",
  "detail": "Kept awake by 2 agents working, on power only; on battery it sleeps as usual. A closed lid still sleeps the Mac on battery.",
  "manual": false,
  "until": 0,
  "holds": [{"id": "agents", "kind": "agents", "label": "2 agents working"}],
  "agents": true,
  "battery": false,
  "display": false
}
```

| Field | Meaning |
|---|---|
| `on` | The plugin holds its power assertions now. |
| `text` | What a client shows while `on` ("Awake", "Awake until 18:30", "Awake · 2 agents", "Awake · <reason>"); empty when off. The first of manual, agent holds, agents rule names it; "+N" counts the others. |
| `detail` | One sentence for a tooltip or a footnote: what holds it, whether on battery too, the screen, the lid. "The Mac sleeps as usual." when off. |
| `manual` | Your own hold is on: clients offer Turn off instead of Keep awake. |
| `until` | Unix seconds when your hold ends; `0` when it is off or has no end. |
| `holds` | `[{id, kind, label, until?, pid?, session?, workspace?}]`: `manual` (id `manual`), `agents` (id `agents`), `agent` (id `ah_…`). |
| `agents`, `battery`, `display` | The settings: the "while agents work" rule is on, it holds on battery too, the screen stays on. |

Clients show `text` and `detail` as they are and never work out the state themselves.

## Commands

| Command | Who | Does |
|---|---|---|
| `awake.state` | app, phone, agents | The state above. |
| `awake.set {on, minutes?}` | app, phone, palette | Turns your hold on (for `minutes`, or until turned off when `0` or absent) or off. Returns the state. |
| `awake.hold {reason, minutes?, pid?}` | agents | Adds an agent hold in the caller's workspace; returns `{hold, until}` (`hold` `ah_…`). `reason` is shown ("Awake · <reason>"), at most 80 characters. |
| `awake.release {hold}` | agents | Ends an agent hold. |
| `awake.settings-set {agents?, battery?, display?}` | app, phone | Changes the settings; returns the state. |

The palette has "Keep Mac awake" (`awake.on`: your hold, until turned off) and "Let Mac sleep" (`awake.off`: ends your hold; agent holds and the agents rule stay). Settings → Keep Awake (`awake.settings`, rendered by the plugin) has your hold as a toggle with `detail` under it, and the three settings.

## Where it shows

- **Mac:** the status bar shows `text` after the agent counts while `on` (a filled cup, `text` cut at the tail when the bar is narrow, `detail` as the tooltip); while off, only a faint outline cup, so the menu stays reachable. Clicking it opens a menu: Keep Awake for 1 Hour, for 4 Hours, Until Turned Off, Turn Off (only while `manual`), While Agents Work (a checkmark: turns the `agents` rule on or off, `awake.settings-set`), and Keep Awake Settings…, which opens Settings on the plugin's page. Nothing shows while the core is down or the plugin doesn't answer.
- **Phone:** Mac details (tap the Mac's name) opens with a Keep Awake group, shown only when the Mac offers `awake.state`: `text` while `on` (`detail` under the group), `detail` alone while off; Keep Awake… with for 1 hour, for 4 hours and until turned off, and Turn Off while `manual`. It follows `awake.changed` and shows only while the Mac is online, so a Mac that went to sleep never reads "Awake".

## Storage

Plugin storage keys: `settings`, `manual` (`{on, until}`) and `holds` (agent holds), so a plugin restart keeps them; expired holds, exited pids and closed sessions are dropped on load.
