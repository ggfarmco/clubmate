# Notifications

Anything running in Clubmate can raise a macOS notification: a script, a build, or an agent that has a question or waits for permission.

## `cm notify`

```sh
cm notify [--title T] [--kind K] [--workspace W] [--session S] <message | ->
```

`clubmate notify` is the same command. Development builds install the CLI as `cm-dev`.

- Inside a Clubmate terminal the workspace and session come from `$CLUBMATE_PROJECT_ID` and `$CLUBMATE_SESSION_ID`, so a plain `cm notify "Build finished"` points at the terminal it ran in. `--workspace` and `--session` override them; the env session is used only when the workspace is the env workspace.
- Outside Clubmate, `--workspace` is required unless exactly one workspace is open in the app.
- `--kind` is one of `needs-input`, `permission`, `done`, `stuck`, `info` (default `info`). Any other kind is an error.
- The message is cut to 1000 characters and the title to 120 (cut, not rejected). Several words are joined with spaces. `-` reads the message from stdin, so hooks can pipe into it.
- It prints the notification id and exits 0 once the core accepted it. It never waits for the app; if the app isn't running the notification is dropped.
- At most 5 notifications per 10 seconds per workspace; extra ones fail with `too many notifications`.

Agents reach the same thing through MCP as the `notify_post` tool (`notify.post`, non-destructive): `message`, optional `title`, `kind` and `session`.

## In the app

- A notification is shown unless you are already looking at its target: the app is active, its workspace is the active one in the key window, and the session is on screen (a terminal or editor surface in the selected section, or the session selected in the sidebar, e.g. an agent session). Without a session the workspace being in front is enough.
- The title is the workspace name and the notification's title (the kind when it has none); the body is the message.
- Clicking it brings up the workspace's window and the session: a terminal is selected in Terminals and focused, an agent session opens in Agents.
- macOS asks for notification permission the first time one is shown.
- Agents: a session waiting for permission is posted through the same path as kind `permission`, once per request.
- Unread: each workspace counts the notifications posted while it was not in front. The count shows next to the workspace in the switcher (⌃⌘W), on the switcher button for the other workspaces of a window, and in total on the Dock icon. It clears when the workspace's window becomes key with the app active.

## Claude Code hooks

Claude Code can call `cm notify` from its hooks. **Settings → Agents → Copy notification hooks** copies this snippet (with `cm-dev` in development builds); merge it into the `hooks` of `~/.claude/settings.json` yourself. Clubmate never edits Claude's config.

```json
{
  "hooks": {
    "Notification": [
      {
        "hooks": [
          {
            "command": "jq -r .message | cm notify --kind needs-input --title Claude -",
            "type": "command"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "command": "cm notify --kind done --title Claude Finished",
            "type": "command"
          }
        ]
      }
    ]
  }
}
```

- `Notification` fires when Claude asks for permission or waits for your input; its hook gets JSON on stdin and `jq` pulls out the message.
- `Stop` fires when Claude finishes a turn.
- The hooks need the `cm` command on `PATH` (**Clubmate → Install “cm” Command…**) and `jq`.
- Claude running in a Clubmate terminal inherits `CLUBMATE_PROJECT_ID` and `CLUBMATE_SESSION_ID`, so the notification is skipped while you look at that terminal and clicking it brings you back to it. Claude running elsewhere needs exactly one open workspace, or add `--workspace <id>` to the commands.
