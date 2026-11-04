# Notifications

Anything running in Clubmate can raise a macOS notification: a script, a build, or an agent that has a question or waits for permission.

## `cm notify`

```sh
cm notify [--title T] [--kind K] [--workspace W] [--session S] [--quiet] <message | ->
```

`clubmate notify` is the same command. Development builds install the CLI as `cm-dev`.

- Inside a Clubmate terminal the workspace and session come from `$CLUBMATE_WORKSPACE_ID` and `$CLUBMATE_SESSION_ID`, so a plain `cm notify "Build finished"` points at the terminal it ran in. Inside an agent session of the Agents section `$CLUBMATE_AGENT_SESSION` names that session and wins over `$CLUBMATE_SESSION_ID`. `--workspace` and `--session` override them; the env session is used only when the workspace is the env workspace.
- Outside Clubmate, `--workspace` is required unless exactly one workspace is open in the app.
- `--kind` is one of `needs-input`, `permission`, `question`, `done`, `stuck`, `info` (default `info`). Any other kind is an error.
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
- Agents: a session waiting for permission is posted through the same path as kind `permission`, and a pending agent question (an elicitation: AskUserQuestion, an MCP form or sign-in link) as kind `question` ("Claude has a question"), once per request; on macOS both carry their kind icon (hand, question mark) as the notification's image. A `permission` or `question` post that names an agent session (`agt_…`) is dropped, because that path already covers it; `needs-input`, `done` and the other kinds are shown (skipped while that session is on screen, a click opens it).
- The agents plugin posts one `notify.posted` per pending request for the phone: kind `permission` or `question`, the session, its title as the title and a message that names only the kind of action ("Wants to run a command", "Wants to edit a file", "Has a question"), never the command, path or question text, because the push leaves the Mac through the relay and APNs; the phone shows the request itself when the session is opened. The Mac drops it by the rule above; the phone shows it and the workspace's push trigger [pushes](remote.md#real-push) it. It counts against the rate limit; a post over the limit is logged and dropped.
- The agents plugin also posts when a top-level session's turn ends, for the Mac and the phone alike: kind `needs-input` ("Asks you a question") when the turn is done and its last message asks a question (`asks`, see [Needs you](agents.md#needs-you)), `done` ("Finished", or "Done · 3 open" with [open items](agents.md#open-items) left) when it is done otherwise and none of its child sessions is running or waiting (an orchestrator's turns end each time a child reports; its `done` goes out with the first turn that ends after the last child finished), `stuck` ("Stopped with an error") when it stopped with an error (the turn failed, the agent could not start, or its process died mid-turn) and no automatic network retry is scheduled (a retry that gives up posts it then). The session, its title as the title, and only that generic message, never the reply or the error. Nothing is posted for a cancelled turn, a closed session, a turn that goes on with a queued prompt or a background task (it is posted when the task ends), a child session (its parent collects it), a service session (voice), nor `done`/`stuck` for a step of a graph run (the run goes on); a graph step that asks still posts `needs-input`. The plugin is the only source for agent sessions: the [hooks](#claude-code-hooks) skip them.
- Rate limit: permission and question posts go out first, and turn ends take at most 2 of the 5 posts per 10 s of a workspace; the turn ends beyond that wait for the window and go out as one post without a session, titled "N agent sessions", listing the titles by kind ("Asks you a question: A, B" / "Stopped with an error: C" / "Finished: D · 3 open, E") with the most urgent kind as its kind, leaving out sessions whose [policy](#per-session-policy) holds that kind back. A session that ends again while waiting is listed once, with its last end.
- A notification an agent posted through MCP says "via agent" under the title.
- Agent sessions have a notification policy; see [Per-session policy](#per-session-policy).
- Every notification is unseen and unhandled unless its session was on screen when it arrived (then it is listed as handled).
  - Seen: the switcher (⌃⌘W) shows each workspace's unseen count next to it and on its button for the other workspaces of a window, and the Dock icon shows the total. A workspace's notifications become seen when its window becomes key with the app active.
  - Handled: the bell counts what is unhandled plus the waiting agent permission requests (each request once). A notification becomes handled when you click it (in the inbox or the banner), with Mark All Read, or when its session is on screen (for one without a session, when its workspace is in front).

## Per-session policy

Each agent session of the Agents section has a notification policy: **All** (default) shows every notification, **Needs input** only `needs-input`, `permission` and `question` ones (questions and permission requests), **Muted** none.

- A notification the policy holds back gets no banner and no sound; it is listed in the inbox history as read, so it counts neither on the bell nor on the Dock icon and is not pinned. Pending agent permission requests stay pinned in "Waiting for you" and counted on the bell whatever the policy.
- The strictest policy among the session and its parent sessions applies, so muting an orchestrator mutes its children too.
- Set it from the session's context menu in the Agents sidebar (**Notify About Everything**, **Notify Only When It Needs Input**, **Mute Notifications**; the current one is left out), with `agents.notify session=… policy=all|needs-input|muted`, or with `notify` when starting a session with `agents.new`. An agent may set it for its own session and the sessions it started.
- `agents.sessions` lists each session's own `notify` (omitted for All) and the applied `notifyEffective`.
- The Agents sidebar shows a policy other than All at the start of the session's row detail: `Muted · …` or `Input only · …` (the applied one, so children of a muted session show `Muted` too).
- The phone companion applies the same policy: a held-back notification is added to its inbox as read and gets no local notification.
- `remote.push`, which the workspace's push trigger calls for a paired phone (see [real push](remote.md#real-push)), applies the same policy on the Mac: a held-back notification is not pushed and the trigger run's result says `held: session muted` or `held: session needs-input only`.
- Notifications about terminals and editor sessions are not affected.

## Inbox

The bell in the window's title bar, next to the workspace switcher, or ⇧⌘I opens the inbox. It works whether or not macOS notifications are allowed.

- **Waiting for you**, pinned at the top: agent permission requests and questions until they are answered (or the session is deleted, or the workspace is closed; their history entries and delivered macOS notifications go too), and unhandled `needs-input` notifications. A window becoming key does not unpin them.
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
            "command": "[ -n \"$CLUBMATE_AGENT_SESSION\" ] || jq -r '.message // empty' | cm notify --quiet --kind needs-input --title Claude -"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "[ -n \"$CLUBMATE_AGENT_SESSION\" ] || cm notify --quiet --kind done --title Claude Finished"
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
- Claude running in a Clubmate terminal inherits `CLUBMATE_WORKSPACE_ID` and `CLUBMATE_SESSION_ID`, so the notification is skipped while you look at that terminal and clicking it brings you back to it. Claude in the Agents section gets `CLUBMATE_AGENT_SESSION` (passed per session through the adapter's `_meta.claudeCode.options.env`); both hooks do nothing there, because the agents plugin already posts its permission requests, questions and turn ends ([above](#in-the-app)). Hooks copied before this guard post a second notification for agent sessions; copy them again. Claude running elsewhere needs exactly one open workspace, or add `--workspace <id>` to the commands; otherwise `--quiet` drops the notification.
