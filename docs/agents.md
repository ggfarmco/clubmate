# Agents

Agents running in Clubmate terminals (Claude Code, Codex, scripts) can drive Clubmate through one registry of commands: core primitives (workspaces, sessions, processes, focus) and actions that plugins declare (open a file, stage changes, close a terminal, ...).

## What agents can do

`clubmate command list --agent` shows everything an agent may run, and `clubmate command describe <id>` shows one command's arguments.

- Workspaces: list, open folders, open the home workspace.
- Sessions: list, start a login shell, type into one (`session.send`), read its recent output (`session.read`), kill one.
- Processes: the process tree of every session.
- UI: show a section (`ui.focus`).
- Notifications: `notify.post` shows a macOS notification (needs input, permission, done, stuck, info), skipped while the user looks at the session; see [notifications](notifications.md).
- Plugin actions marked for agents. Some are generic capabilities, so an agent need not know which plugin provides them: `editor.open`, `editor.quickfix`, `editor.reveal`, `terminal.new`, `terminal.close`, `files.reveal`, `git.stage-all`, `git.review`.
- Git worktrees: `git.worktrees` lists every git folder's worktrees, `git.worktree-switch` switches a folder to one of them, `git.worktree-new` creates `.worktrees/<branch>` and switches to it, and `git.worktree-remove` removes a linked worktree (never the main checkout or one with uncommitted changes). Create and remove return `{id}` at once; `git.worktree-op-status id=…` waits up to 1 s for the outcome.
- Agent sessions (read-only): `agents.sessions` lists the workspace's agent sessions with their status, and `agents.transcript session=…` reads one session's transcript from Clubmate's own log.
- Search (read-only, never in the home workspace): `search.files` fuzzy-finds workspace files by path (a trailing `:N` adds a line), and `search.grep` searches file contents (`regex`, `case`, `word`, `glob`, up to 500 matches) and returns within 4 seconds.

Hidden helpers and commands not marked for agents stay out of reach.

## Approvals

Destructive actions (closing or killing sessions, typing into a session, restarting the editor, killing orphan processes, removing a worktree) ask you first. Clubmate shows an alert naming the action, the workspace, the targeted session and every argument in full, with Allow and Deny. Deny is the default. If nobody answers within 60 seconds, or the app isn't open, the action is refused.

Agent mode is a cooperative guard for well-behaved agents, not a security boundary: any program running as your user can reach Clubmate directly.

## CLI

Inside a Clubmate terminal, `clubmate` is on `PATH` and `$CLUBMATE_PROJECT_ID` names the current workspace.

```sh
clubmate open src/main.go:42                       # open in the editor
clubmate command run terminal.new                  # any command or capability
clubmate command run session.read id=ses_123 bytes=4000
clubmate command run terminals.close --agent sessionId=ses_123
clubmate session send ses_123 "make test" --enter
clubmate session read ses_123
```

`key=value` arguments are typed by the command's schema (arrays are comma-separated); `--args '{"…": …}'` takes JSON instead. `--agent` runs the command as an agent would, with the same limits and approvals. The CLI never turns it on by itself.

## MCP (Claude Code and other MCP clients)

`clubmate mcp` is a stdio MCP server over the same registry. Every call it makes is an agent call.

- Tools are named after the command or capability with `.` and `-` replaced by `_` (`editor_open`, `workspace_list`, `terminal_close`). A plugin command that provides a capability appears under the capability's name only.
- Every tool takes an optional `workspace` id. It defaults to `$CLUBMATE_PROJECT_ID`, then the most recently opened workspace.
- If Clubmate isn't running, the tools still list the core primitives, and calls fail with "Clubmate isn't running". The server never starts Clubmate itself.

To connect Claude Code, use **Settings → Agents → Connect Claude Code**. It runs:

```sh
claude mcp add --scope user clubmate -- "/Applications/Clubmate.app/Contents/Resources/bin/clubmate" mcp
```

Development builds register the server as `clubmate-dev` instead, so both can be connected at once. The button is shown only when `claude` is on your login shell's `PATH`; Clubmate looks once per launch, so restart Clubmate after installing `claude`. It says "Connected" only when the registered server runs this copy of Clubmate; if it runs another one, it shows that command and offers "Reconnect" (remove, then add; if the remove fails, nothing is added). Clubmate never edits Claude's config files itself.
