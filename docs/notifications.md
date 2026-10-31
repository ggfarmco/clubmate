# Notifications

Anything running in Clubmate can raise a macOS notification: a script, a build, or an agent that has a question or waits for permission.

## `cm notify`

```sh
cm notify [--title T] [--kind K] [--workspace W] [--session S] [--quiet] <message | ->
```

`clubmate notify` is the same command. Development builds install the CLI as `cm-dev`.

- Inside a Clubmate terminal the workspace and session come from `$CLUBMATE_PROJECT_ID` and `$CLUBMATE_SESSION_ID`, so a plain `cm notify "Build finished"` points at the terminal it ran in. Inside an agent session of the Agents section `$CLUBMATE_AGENT_SESSION` names that session and wins over `$CLUBMATE_SESSION_ID`. `--workspace` and `--session` override them; the env session is used only when the workspace is the env workspace.
- Outside Clubmate, `--workspace` is required unless exactly one workspace is open in the app.
- `--kind` is one of `needs-input`, `permission`, `done`, `stuck`, `info` (default `info`). Any other kind is an error.
- The message is cut to 1000 characters and the title to 120 (cut, not rejected). Several words are joined with spaces. `-` reads the message from stdin, so hooks can pipe into it.
- It prints the notification id and exits 0 once the core accepted it. It never waits for the app; if the app isn't running the notification is dropped.
- At most 5 notifications per 10 seconds per workspace and 20 in all; extra ones fail with `too many notifications`.
- `--quiet` is for hooks: it prints nothing and exits 0 when Clubmate isn't running, no workspace resolves, the message is empty or the rate limit hits. Other errors (an unknown kind) still fail.

Agents reach the same thing through MCP as the `notify_post` tool (`notify.post`, non-destructive): `message`, optional `title`, `kind` and `session`.

## In the app

- A notification is shown unless you are already looking at its target: the app is active, its workspace is the active one in the key window, and the session is on screen (a terminal or editor surface in the selected section, or the session selected in the sidebar, e.g. an agent session). Without a session the workspace being in front is enough.
- The title is the workspace name and the notification's title (the kind when it has none); the body is the message.
- Clicking it brings up the workspace's window and the session: a terminal is selected in Terminals and focused, an agent session opens in Agents.
- macOS asks for notification permission the first time one is shown.
- Agents: a session waiting for permission is posted through the same path as kind `permission`, once per request. A `needs-input` or `permission` post that names an agent session (`agt_…`, e.g. from Claude's hooks inside the Agents section) is dropped, because that path already covers it; `done` and the other kinds are shown (skipped while that session is on screen, a click opens it).
- A notification an agent posted through MCP says "via agent" under the title.
- Agent sessions have a notification policy; see [Per-session policy](#per-session-policy).
- Every notification is unseen and unhandled unless its session was on screen when it arrived (then it is listed as handled).
  - Seen: the switcher (⌃⌘W) shows each workspace's unseen count next to it and on its button for the other workspaces of a window, and the Dock icon shows the total. A workspace's notifications become seen when its window becomes key with the app active.
  - Handled: the bell counts what is unhandled plus the waiting agent permission requests (each request once). A notification becomes handled when you click it (in the inbox or the banner), with Mark All Read, or when its session is on screen (for one without a session, when its workspace is in front).

## Per-session policy

Each agent session of the Agents section has a notification policy: **All** (default) shows every notification, **Needs input** only `needs-input` and `permission` ones (questions and permission requests), **Muted** none.

- A notification the policy holds back gets no banner and no sound; it is listed in the inbox history as read, so it counts neither on the bell nor on the Dock icon and is not pinned. Pending agent permission requests stay pinned in "Waiting for you" and counted on the bell whatever the policy.
- The strictest policy among the session and its parent sessions applies, so muting an orchestrator mutes its children too.
- Set it from the session's context menu in the Agents sidebar (**Notify About Everything**, **Notify Only When It Needs Input**, **Mute Notifications**; the current one is left out), with `agents.notify session=… policy=all|needs-input|muted`, or with `notify` when starting a session with `agents.new`. An agent may set it for its own session and the sessions it started.
- `agents.sessions` lists each session's own `notify` (omitted for All) and the applied `notifyEffective`.
- The Agents sidebar shows a policy other than All at the start of the session's row detail: `Muted · …` or `Input only · …` (the applied one, so children of a muted session show `Muted` too).
- The phone companion applies the same policy: a held-back notification is added to its inbox as read and gets no local notification.
- Notifications about terminals and editor sessions are not affected.

## Inbox

The bell in the window's title bar, next to the workspace switcher, or ⇧⌘I opens the inbox. It works whether or not macOS notifications are allowed.

- **Waiting for you**, pinned at the top: agent permission requests until they are answered (or the session is deleted, or the workspace is closed), and unhandled `needs-input` notifications. A window becoming key does not unpin them.
- **History**, newest first, across all workspaces: kind icon, title, two lines of the message, the source (workspace · session · "via agent"), how long ago (kept current while the inbox is open), and a dot when unhandled. A menu filters by kind. Notifications skipped because you were looking at the session are listed as read.
- Clicking a row brings up its workspace and session and marks it handled (a pinned permission marks its history entry); opening the inbox marks nothing.
- The history keeps the last 200 notifications from the last 7 days, with their read state, in `notifications.json` in Clubmate's state directory. It remembers the last bus event it saw, so after a restart it picks up notifications posted while the app was closed (listed unhandled, without a banner; so is anything older than 30 s). If that event is no longer on the bus (a new bus database), it starts from new events; if the core cannot be asked, it keeps the remembered event and resumes from it. Writes of the file are ordered, so a save still in flight at quit cannot overwrite the final one. Entries it cannot read are skipped, not the whole file.
- When macOS notifications are off (denied in System Settings, or the permission request failed, as it does for ad-hoc signed development builds; `make app` signs with the first valid "Apple Development" identity in the keychain, `make app DEV_SIGN_IDENTITY=` builds ad-hoc), the inbox shows "macOS notifications are off" with a button to System Settings → Notifications. Clubmate asks for permission at most once per launch.

## Claude Code hooks

Claude Code can call `cm notify` from its hooks. **Settings → Agents → Copy notification hooks** copies this snippet (with `cm-dev` in development builds); merge it into the `hooks` of `~/.claude/settings.json` yourself. Clubmate never edits Claude's config.

```json
{
  "hooks": {
    "Notification": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "jq -r '.message // empty' | cm notify --quiet --kind needs-input --title Claude -"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "cm notify --quiet --kind done --title Claude Finished"
          }
        ]
      }
    ]
  }
}
```

- `Notification` fires when Claude asks for permission or waits for your input; its hook gets JSON on stdin and `jq` pulls out the message (nothing when there is none).
- `--quiet` keeps the hooks best-effort: Claude never sees an error when Clubmate is closed or busy.
- `Stop` fires when Claude finishes a turn.
- The hooks need the `cm` command on `PATH` (**Clubmate → Install “cm” Command…**) and `jq`.
- Claude running in a Clubmate terminal inherits `CLUBMATE_PROJECT_ID` and `CLUBMATE_SESSION_ID`, so the notification is skipped while you look at that terminal and clicking it brings you back to it. Claude in the Agents section gets `CLUBMATE_AGENT_SESSION` (passed per session through the adapter's `_meta.claudeCode.options.env`), so its `Stop` hook points at that agent session. Claude running elsewhere needs exactly one open workspace, or add `--workspace <id>` to the commands; otherwise `--quiet` drops the notification.
