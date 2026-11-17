# Keep awake

The `awake` plugin keeps the Mac from sleeping, like Amphetamine. It is the one place that decides whether the Mac stays awake and every word about it: the Mac app, the phone and agents only ask it and show what it says. The core and the plugin outlive the app, so the Mac stays awake with the app closed, while the phone drives agents. The design is `design/features/keep-awake/` (Claude Design, round 2).

## Your choice and agents' holds

**Your choice** is one switch with three choices; at most one is active, none means Off:

| Mode | Choice | Holds the Mac while | On battery |
|---|---|---|---|
| `hours` | For N hours: 1, 2, 4, 8 or 24 | the time hasn't passed | yes |
| `agents` | While agents work | any agent session in any workspace is `active` in `agents.sessions` (running or waiting for you, not a service session; the sessions of the Mac status bar's "N agents") | only with "On battery too" (laptops) |
| `on` | Until turned off | until you choose Turn Off | yes |

Picking a choice replaces the current one; picking another number of hours restarts the clock. When the hours run out the mode goes to none (Off). Turn Off ends your choice only. **Armed** is `agents` chosen while it holds nothing: no agent is active, or one is and the Mac is on battery with "On battery too" off.

**An agent's hold** is separate: an agent asks with `awake.hold` (MCP `awake_hold`, e.g. "release build"); it ends with `awake.release`, its `minutes` (default 60, at most 720), the process `pid` it named exiting, or its session closing or being deleted. Turn Off never ends it.

The plugin re-reads `agents.sessions` of every workspace when it starts, when the agents plugin restarts (`agents.ready`), on `agents.changed` for a workspace (that workspace only) and every minute while `agents` is chosen, so a missed event is right again within a minute. Deadlines and the power source are checked every 5 seconds against the wall clock, so a timer that stalled across a sleep ends on the next check.

## How it holds

While anything holds the Mac the plugin itself holds IOKit power assertions named "Clubmate keep awake" (`PreventUserIdleSystemSleep` and `PreventSystemSleep`; `PreventUserIdleDisplaySleep` too with "Keep the screen on"), and releases them when nothing does. macOS drops a process's assertions when it exits, so a crashed or killed plugin never leaves the Mac awake (the lid flag outlives it; its guard is in § Lid closed); the supervisor's restart reloads your choice and the agents' holds and asserts again. `pmset -g assertions` lists them under the plugin's pid. The plugin reads the power source itself (IOKit `IOPSCopyPowerSourcesInfo`): whether the Mac has a battery (`laptop`), whether it runs on it (`onBattery`) and the battery's charge, for the lid's 10 % cutoff.

A closed lid still sleeps a laptop on battery, whatever holds it; on power it stays awake closed only with an external display, keyboard and mouse. `detail` says so only on a laptop on battery.

### Lid closed

"Keep awake with the lid closed" (`lid`, off by default, laptops only) keeps a laptop awake with its lid closed, on battery too, the way Amphetamine does it without root: while the plugin holds its assertions on a laptop with `lid` on, it disables clamshell sleep (`IOPMrootDomain` user client, `IOConnectCallScalarMethod` selector 12 `kPMSetClamshellSleepState` with 1), and writes 0 when it stops holding, `lid` turns off, or a laptop on battery drops below 10 %; a plugin that shuts down leaves the flag to its guard (below), so a restart or an update with the lid closed does not sleep the Mac. It sends 1 again on every 5-second check and on every power-source change (the `com.apple.system.powersources.source` notification), because powerd may reset the flag when the source changes. Writing 0 with the lid closed sleeps the Mac a few seconds later.

The flag lives in the kernel and outlives the process, so a crash would leave the Mac unable to sleep on lid close. Before its first 1 the plugin starts a guard, the `awake` binary re-run as `awake lid-guard <plugin pid>` in its own session. When the plugin's end of its pipe closes (shutdown, crash or SIGKILL) the guard waits up to 2 s for that pid to be gone, then gives the next plugin up to 60 s to take the lid over: as soon as it finds the lock taken it exits without writing; if nobody takes it in that time it writes 0. So a restart or an update hands the 1 over without a gap, and a Clubmate that stops for good frees the lid within a minute. One process owns the flag at a time: the plugin takes an exclusive `flock` on `$TMPDIR/clubmate-awake-lid.lock` before writing 1 and lets it go after writing 0; a guard, or another Clubmate's plugin, writes 0 only when it can take the lock, so an old guard never clears a newer plugin's 1, and a second Clubmate waits its turn instead of fighting over the flag. The storage key `lid` remembers that this plugin wrote 1 and has not written 0 since (a shutdown keeps it set, the guard may still be holding the 1 for it); a plugin that starts with it set and nothing to hold writes 0. A starting plugin touches the flag only after it has read the power source and every workspace's sessions, so a restart that still holds goes straight to 1 without a 0 in between (a 0 with the lid closed sleeps the Mac within seconds). A plugin that never wrote 1 never touches the flag, so a test or a second instance with `lid` off leaves the real Mac alone. The supervisor kills a plugin's process group; the guard runs in its own session, so it outlives that kill and hands the lid over or frees it.

## State

`awake.state` (scope app, `remote`, agent-visible) returns the state; every change publishes the same object on the bus topic `awake.changed`, which `remote` forwards to the phone. Clients show every string as it is and never work out the state themselves.

```json
{
  "on": true,
  "armed": false,
  "text": "Awake until 18:00 +1",
  "detail": "Kept awake by you until 18:00 and by an agent's release build.",
  "mode": "hours",
  "hours": 4,
  "until": 1794870000,
  "value": "Until 18:00",
  "ends": "until 18:00",
  "holds": [
    {"kind": "you", "title": "You", "trailing": "for 4 hours · until 18:00"},
    {"kind": "agent", "id": "ah_…", "title": "release build", "trailing": "Ship 2.4 release · ≤ 20:40", "session": "agt_…", "workspace": "prj_…"}
  ],
  "labels": {
    "section": "Keep Awake",
    "picker": "Keep awake",
    "hours": {"title": "For Hours", "active": "For 4 Hours", "options": [{"hours": 1, "title": "1 Hour", "short": "1 h"}, {"hours": 2, "title": "2 Hours", "short": "2 h"}, {"hours": 4, "title": "4 Hours", "short": "4 h"}, {"hours": 8, "title": "8 Hours", "short": "8 h"}, {"hours": 24, "title": "24 Hours", "short": "24 h"}]},
    "agents": {"title": "While Agents Work", "subtitle": "Running or waiting for you"},
    "on": {"title": "Until Turned Off"},
    "off": {"title": "Turn Off"}
  },
  "laptop": true,
  "onBattery": false,
  "battery": false,
  "display": false,
  "lid": false
}
```

| Field | Meaning |
|---|---|
| `on` | The plugin holds its assertions now. |
| `armed` | `agents` is chosen and holds nothing yet (status bar: muted outline gourd). |
| `text` | Shown only while `on`; empty otherwise. Your choice leads, then an agent's hold, then the agents choice; "+N" counts agents' own holds besides the lead. "Awake until 18:00", "Awake", "Awake · 2 agents", "Awake · release build", "Awake until 18:00 +1", "Awake · 2 agents +1". Clock times in the system's format, never a countdown. |
| `detail` | One sentence, always: "The Mac sleeps as usual." / "The Mac sleeps as usual until an agent is running or waiting for you." (armed) / "The Mac sleeps as usual: on battery, agents wait for power." (armed on battery) / "Kept awake by you until 18:00." / "Kept awake by you until you turn it off." / "Kept awake while 2 agents work, on power." / "Kept awake by an agent's release build until it ends, 20:40 at the latest." / "Kept awake by you until 18:00 and by an agent's release build."; on a laptop while awake one lid sentence follows: " It stays awake with the lid closed." while `lid` holds it, " Below 10 % battery a closed lid sleeps the Mac again." with `lid` on at low battery, " Could not keep it awake with the lid closed: <error>." when the kernel call or the guard fails, else " A closed lid still sleeps the Mac on battery." on battery. |
| `mode` | `hours`, `agents`, `on`, or empty (Off). |
| `hours`, `until` | The chosen hours and when they end (unix seconds); 0 otherwise. |
| `value` | Your choice in words, for the phone's picker and Settings: "Off", "Until 18:00", "While agents work", "Until turned off". |
| `ends` | "until 18:00" while `hours` is chosen, else empty: beside the hours in Settings and in the phone's Macs list. |
| `holds` | Lines under the status (menu, Settings Now block, phone): `you` (your choice), `rule` ("While agents work" · "2 sessions" / "no agent working" / "waits for power · 1 waiting"), `agent` (its label · its session's title · "≤ 20:40"; opens its `session`). |
| `labels` | Every word of the switch, so the Mac menu, Settings and the phone can't drift apart. `hours.active` is the item's title while `hours` is chosen; `hours.options[].short` ("4 h") is the phone's palette row; `picker` the phone picker's label. |
| `laptop`, `onBattery` | The Mac has a battery; it runs on it now. "On battery too", the Lid group and the lid footnote show only on a laptop. |
| `battery`, `display`, `lid` | Settings: the agents choice holds on battery too; the screen stays on; a laptop stays awake with the lid closed. |

## Commands

| Command | Who | Does |
|---|---|---|
| `awake.state` | app, phone, agents | The state above. |
| `awake.set {mode, hours?}` | app, phone | `mode` `hours` (with `hours` 1, 2, 4, 8 or 24), `agents`, `on` or `off`. Returns the state. |
| `awake.toggle` | app (⌥-click on the gourd, ⌥⌘A) | Nothing chosen: `on`; anything chosen: `off`. Returns the state. |
| `awake.on`, `awake.agents`, `awake.off` | palette | "Keep Mac Awake Until Turned Off", "Keep Mac Awake While Agents Work", "Turn Off Keep Awake". |
| `awake.hold {reason, minutes?, pid?}` | agents | Adds an agent hold in the caller's workspace; returns `{hold, until}`. `reason` at most 80 characters. |
| `awake.release {hold}` | agents | Ends an agent hold. |
| `awake.settings-set {battery?, display?, lid?}` | app, phone | Changes the settings; returns the state. |

## Where it shows

- **Mac status bar** (`design/features/keep-awake/1-status-bar`): the item sits in the right group, just left of the section's status line (hints) and ⌘K, and grows leftwards, so nothing else in the bar moves. The glyph is a mate gourd (round 3, `KA3-Glyph`), drawn by `GourdGlyph` and monochrome on the Mac. Off: a faint outline gourd with its bombilla; armed: the outline gourd at muted; awake: the filled gourd with a leaf at the mouth plus `text` at the bar's muted level. No tint. Below 800 pt only the gourd; the text moves to the tooltip and the menu's first line. Tooltip `detail`. Click or right-click opens the menu; ⌥-click runs `awake.toggle`.
- **Menu** (`2-menu`): the first line is `text` (or "The Mac sleeps as usual."), then one quiet line per hold (title left, trailing right). Then the section header (`labels.section`): For Hours ▸ (submenu of `labels.hours.options`, ✓ on the active number; the item reads `labels.hours.active` while active), While Agents Work, Until Turned Off, ✓ on the active choice; Turn Off (with ⌥⌘A) only while a choice is active, ⌥⌘A on Until Turned Off otherwise. The first line carries the gourd as a template image, so macOS tints it. Then Keep Awake Settings…
- **Settings → Keep Awake** (`3-settings`, rendered by the plugin; its icon in the Settings sidebar and header is `clubmate.gourd`): a read-only **Now** block (the gourd, `clubmate.gourd.fill` while awake, and the status line, `detail`, the hold lines); **Keep awake**, one radio group: Off · For [hours select] (with "until 18:00" beside it while active) · While agents work (with "On battery too" indented under it, laptops only) · Until turned off; **Display**: Keep the screen on; **Lid**, laptops only: Keep awake with the lid closed ("Off: closing the lid sleeps the Mac."), then the lid footnote: "A closed lid still sleeps a laptop on battery. On power it stays awake closed only with an external display, keyboard and mouse." with `lid` off, "While it is kept awake, a closed Mac keeps running, on battery too until 10 % is left. It gets hot in a closed bag." with it on.
  Its `ui.event` actions on slot `awake.settings` (workspace ""): `choice` with `"off"`, `"hours"` (the last number used, 1 on first use), `"agents"` or `"on"`; `hours` with 1, 2, 4, 8 or 24; `battery`, `display` and `lid` with a bool.
- **Phone** (`4-phone`): Mac details, first group, while the Mac is online or remembered: the status row (the gourd at 16 pt in a 28 pt circle, accent while awake), agents' holds nested under it (tap opens the session), one **Keep awake** picker whose value is `value`; its menu: a row of hour buttons (1 h · 2 h · 4 h · 8 h · 24 h), While Agents Work (with its subtitle), Until Turned Off, Turn Off while a choice is active; footer `detail` while awake. Offline: the group stays, dimmed, with the last known state ("Asleep or offline"). The Macs list shows a small filled gourd (13 pt) and the end time on a Mac that is awake.

## Storage

Plugin storage keys: `settings` (`{battery, display, lid}`), `choice` (`{mode, hours, until}`), `holds` (agent holds) and `lid` (this plugin wrote 1 to the clamshell flag and has not written 0 since), so a plugin restart keeps them; an expired choice, expired holds, exited pids and closed sessions are dropped on load.
