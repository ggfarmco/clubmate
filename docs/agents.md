# Agents

Agents running in Clubmate terminals (Claude Code, Codex, scripts) can drive Clubmate through one registry of commands: core primitives (workspaces, sessions, processes, focus) and actions that plugins declare (open a file, switch a worktree, close a terminal, ...).

## What agents can do

`clubmate command list --agent` shows everything an agent may run, and `clubmate command describe <id>` shows one command's arguments.

- Workspaces: list, open folders, open the home workspace.
- Sessions: list, start a login shell, type into one (`session.send`), read its recent output (`session.read`), kill one.
- Processes: the process tree of every session and of processes plugins register (agent processes).
- UI: show a section (`ui.focus`).
- Notifications: `notify.post` shows a macOS notification (needs input, permission, done, stuck, info), skipped while the user looks at the session; see [notifications](notifications.md).
- Backlog: `backlog_list`, `backlog_claim`, `backlog_update`, `backlog_done` and friends share a per-workspace task list with the user; see [backlog](backlog.md).
- Plugin actions marked for agents. Some are generic capabilities, so an agent need not know which plugin provides them: `editor.open`, `editor.quickfix`, `editor.reveal`, `terminal.new`, `terminal.close`.
- Git worktrees: `git.worktrees` lists every git folder's worktrees, `git.worktree-switch` switches a folder to one of them, `git.worktree-new` creates `.worktrees/<branch>` and switches to it, and `git.worktree-remove` removes a linked worktree (never the main checkout or one with uncommitted changes). Both run the repo's [worktree hooks](worktree-hooks.md). Create and remove return `{id}` at once; `git.worktree-op-status id=…` waits up to 1 s for the outcome.
- Agent sessions: `agents.sessions` lists the workspace's agent sessions with their status, `parentSession` and notification policy (`notify`, `notifyEffective`), `agents.notify session=… policy=all|needs-input|muted` sets the policy of the caller's own session or one it started (see [notifications](notifications.md#per-session-policy)), and `agents.transcript session=…` reads one session's transcript from Clubmate's own log.
- Child agent sessions (no approval): `agents.new` starts a session, optionally in a new worktree `.worktrees/<branch>` made from the caller's worktree without switching the workspace, and sends it a first `prompt` (`notify` sets its notification policy); `agents.prompt` sends any session a follow-up (added to the running turn when the agent supports steering, else queued); `agents.cancel` stops a running turn; `agents.permission` answers a permission request, but only of sessions the calling agent session started (directly or through its children); `agents.wait` waits without polling until one of the listed sessions stops running (done, waiting for permission, asking a question, error or closed), up to 300 s per call, and returns their status with the pending permission's options; it also returns early with the user's queued messages to the calling session.
- Failed turns: a turn the agent ends with an internal error (`-32603`, e.g. Claude Code's "ended the turn without returning results for tool calls" when the advisor tool is rate-limited) is retried once, 3 s later, within the same turn: the transcript shows a notice and a Clubmate message asking the agent to continue. A second failure leaves the session in error, as any other error does.
- Handoffs: `agents.handoff session=… note=…` continues a session that is not running in a fresh one (same worktree, agent, parent, advisor and notification policy), seeded with a brief from Clubmate's log (the task, the last prompt and reply, the last turn's stop reason and checkpoint, and how to read the full transcript) followed by `note`; the old session is closed but can be reopened, and each links to the other. Agents may hand off only sessions they started. In the app, a session's menu has "Hand Off to New Session", and a session whose agent cannot resume offers "Hand off to a new session".
- Attribution: `agents.blame path=…` (or `commit=…`) tells which agent session, turn and prompt made a file's lines or a commit; `agents.trace rev=…` exports a commit's or a range's changed lines as [Agent Trace](https://agent-trace.dev/) records; see [History and attribution](#history-and-attribution).
- Moving a session: `agents.export session=… dir=…` writes a session bundle, `agents.import dir=…` continues it in another worktree or workspace; see [Resuming elsewhere](#resuming-elsewhere).
- Reminders and schedules: `cron.add` calls a command later or repeatedly, e.g. `agents.prompt` to your own session every minute to check your children; the job is deleted with your session unless `owner: "none"`. See [cron](cron.md).
- Reacting to events: `triggers.add` calls a command when a bus event arrives, e.g. `agents.prompt` to yourself when a child's `agents.turn` ends; `triggers.emit` publishes your own `event.<name>`, which `cron.add` can schedule. See [triggers](triggers.md).
- Agent graphs: `graphs.run graph=… input=…` starts a team of child sessions from `.clubmate/graphs/<graph>.json` (one session per role, one trigger per edge) and the Graphs section shows the run; `graphs.list`, `graphs.show`, `graphs.stop`. See [agent graphs](agent-graphs.md).
- Answers on your behalf (asks you first): `agents.answer session=… text=…` shows you the text and, once you allow it, sends it to the session as your own message.
- Testing the app: `testapp.list` lists running test instances and `testapp.call instance=… method=…` calls one of them (see below).

Hidden helpers and commands not marked for agents stay out of reach.

## Import your nvim config

**Import my nvim config** in the command palette starts an agent session in the Agents section that reads `~/.config/nvim` and moves your own keymaps, options, autocmds and plugins into Clubmate's Neovim user layer (`~/.config/clubmate`, or `~/.config/clubmate-dev` for development builds), following the guide bundled at `nvim-distro/skill/SKILL.md`. It skips what Clubmate's defaults already cover and asks you before adding destructive or machine-specific mappings. It needs an open project workspace and never changes `~/.config/nvim`. The first time Neovim starts in a project workspace while `~/.config/nvim` exists, it offers the same import once.

## Testing the app

Agents test Clubmate in their own invisible instances instead of taking over the user's screen. `scripts/test-app.sh start [name]` (name defaults to the branch) launches the Debug build with scratch state, runtime and `XDG_*` dirs under `/tmp/clubmate-test-<name>`, its own core and its own defaults suite, in test mode (`CLUBMATE_TEST_MODE=1`): it never activates, its windows are transparent and ignore the mouse, app-modal dialogs are skipped and every approval is denied. `stop [name]` quits it and removes the dirs, `list` shows running ones, `cm [name] -- <args>` runs the CLI against one.

- `ui.snapshot {out, png?, width?, height?}` (CLI `cm ui snapshot --out <dir> [--size 640x800] [--no-png]`): the app draws each of its windows itself (main window with titlebar, popovers, sheets, settings) to `w<window id>.png`, terminals included, and writes `w<window id>.json`: every rendered SDUI node with its frame in window points, its parent, its menu entries, plus every terminal's visible text and native menus listed as text. Flags: `outside-window`, `wider-than-parent`, `overlaps-sibling`, `zero-size`, `truncated` (text nodes, measured). The result's `summary` lists only flagged elements, or `0 problems`. `--size` resizes the main window first.
- `ui.present {target, shown}` (CLI `cm ui present <target> [--hide]`): opens `inbox`, `worktree`, `switcher` or `settings` without a click; menus are never opened, they appear as text in the layout.
- Over MCP: `testapp.call` proxies `ui.snapshot`, `ui.present`, `ui.get`, `ui.event`, `command.list`, `command.run` (always as an agent call), `project.list` and `workspace.open` to `/tmp/clubmate-test-<instance>`; `ui.snapshot`'s `out` defaults to and must stay inside `/tmp/clubmate-test-<instance>/snapshots`.

The user's own app never answers `ui.snapshot` or `ui.present`: only a test-mode app registers them, and test mode needs a dev build with non-default state and runtime dirs.

## Approvals

Destructive actions (closing or killing sessions, typing into a session, answering an agent session as you, restarting the editor, killing orphan processes, removing a worktree) ask you first. Clubmate shows an alert naming the action, the workspace, the targeted session and every argument in full, with Allow and Deny. Deny is the default. If nobody answers within 60 seconds, or the app isn't open, the action is refused.

An agent session calling these tools is the parent of the sessions it starts with `agents.new` (Clubmate knows the caller from `CLUBMATE_AGENT_SESSION`, which every agent session has). A child starts with the parent's mode, model, effort and advisor, not the worktree's remembered ones, and at most 8 children of one parent run or wait for permission at once. A prompt an agent sends through `agents.prompt` or `agents.new` is logged as coming from that agent (`agent:<session id>`) and reaches the target with `[From Clubmate agent session "<title>" (<id>), not from the user]` in front, so it never passes for your approval; from an agent without an agent session it is logged as `agent:unknown` and starts with `[From another agent session, not from the user]`. Prompting an idle child also counts against the limit of 8. Agents without an agent session (Claude in a terminal, `clubmate command run --agent`) start top-level sessions and cannot answer permission requests.

A child in its own worktree merges its finished, tested work into its parent's branch in the parent's worktree, never into `main`; a child sharing its parent's worktree, or whose parent is on the default branch, only commits and the parent merges. Children never remove worktrees or branches: once a child's turn ends with its branch merged into the parent's branch and its worktree clean and unused, Clubmate closes the child (it stays listed as Closed) and removes its worktree and branch itself, without asking. A session that has children is told to write to you only the children's questions that need your answer, to answer their permission requests itself with `agents.permission`, and to pass your answers on with `agents.answer` rather than paraphrasing them.

From the Agents sidebar you can run a task in its own worktree end to end: press "New in worktree…", type a branch name and press Start (or Return) to start a session in a new `.worktrees/<branch>` made from the workspace's worktree (as `agents.new` with `branch`). When its turn is done, "Review changes" in the session header opens the diff, and, with Finish set to Ask, "Merge to main" (after a confirm) tells the agent to run the tests and, if they pass, squash its branch into `main` as one commit and remove its worktree and branch, the same steps as Finish "Merge to main". Clubmate refuses the merge while the session runs, while another agent session works in that worktree, or when the main checkout has uncommitted changes.

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

`clubmate agent` wraps the agent-session commands and always runs them as an agent call, so a session started from an agent session's shell is its child:

```sh
clubmate agent spawn "fix the flaky test" --branch fix/flaky   # agents.new; without --branch it shares your worktree
clubmate agent tell agt_123 "also update the docs"             # agents.prompt
clubmate agent wait agt_123 agt_456 --timeout 300              # agents.wait
clubmate agent output agt_123 --from-turn 2                    # agents.transcript
clubmate agent sessions                                        # agents.sessions
clubmate agent cancel agt_123                                  # agents.cancel
clubmate agent blame src/main.go -L 10,40                       # agents.blame; or a commit: clubmate agent blame HEAD~2
clubmate agent trace main..HEAD                                # agents.trace; default HEAD
clubmate agent export agt_123                                  # agents.export into ./agt_123.clubmate-session
clubmate agent import agt_123.clubmate-session --branch more   # agents.import; without --branch it continues in your worktree
```

A reviewer is a child in your own worktree: `clubmate agent spawn "Review git diff main..HEAD read-only: report bugs, change nothing"` (or `agents_new` without `branch` over MCP), `wait` for it, `tell` it one round of follow-ups, then fix what it found yourself.

## History and attribution

A turn is the unit of history. Before each prompt Clubmate pins a checkpoint of the worktree at `refs/clubmate/cp/<session>/<turn>` (its parent is `HEAD` at turn start), and when the turn ends another one at `refs/clubmate/cp/<session>/<turn>.end`; both carry a `Clubmate-Worktree: <path>` trailer. When the turn ends, the commits it added to the current branch get a git note in `refs/notes/clubmate`:

```
Clubmate-Session: agt_4c67f4de54b9ae47
Clubmate-Turn: 3
Clubmate-Agent: claude
Clubmate-Title: Fix the flaky test
Clubmate-Prompt: <first line of the turn's prompt>
Clubmate-Checkpoint: <checkpoint sha>
```

The turn's commits are those reachable from `HEAD` but not from its start or from any other branch (local or remote), so rebasing onto `main`, merging `main` or fast-forwarding a child's branch claims nothing that is already elsewhere; a squash or merge commit belongs to the turn that made it. A commit keeps its first note. The transcript lists them per turn (`turns[].commits`).

Read them with `git log --notes=clubmate`, `agents.blame` or `clubmate agent blame <file|commit> [-L a,b]` (git blame of the file, only line ranges whose commit has a note).

Limits:
- `git merge --squash` into `main` makes a new commit, noted with the session that ran the squash (the orchestrator), not the child whose branch it was; the child's notes stay on its branch commits.
- Commits the user makes in the same worktree during an agent's turn, or pulls into its branch from that branch's remote, are noted with that turn.
- Rebase and amend drop the notes unless the repo sets `git config notes.rewriteRef refs/notes/clubmate`; Clubmate doesn't set it.
- Notes are local; push `refs/notes/clubmate` to share them, but they carry each session's title and the first line of its prompts, so check them before pushing.

### Agent Trace

`agents.trace rev=<commit|a..b>` (`clubmate agent trace`) returns one [Agent Trace](https://agent-trace.dev/) record (version `0.1`; the spec's own example says `0.1.0`, its schema allows only `major.minor`) per non-merge commit, oldest first, at most 200. Each changed file lists its changed lines grouped by contributor:

- `ai`, with `url` `clubmate://session/<id>#turn=<n>`: written during that agent turn;
- `human`: changed while no agent turn ran in the worktree;
- `unknown`: Clubmate can't tell (a turn still running, or one from before end-of-turn checkpoints existed, two turns running at once, or a commit with no Clubmate history at all).

How: Clubmate replays the commit's parent, the checkpoints of the commit's worktree whose parent is that commit (plus the checkpoint named in the commit's note), and the commit itself as a throwaway chain of commits, then runs `git blame` on the changed lines. A line is credited to the step that introduced it: the step into `<turn>.end` is the turn, the step into a turn's start checkpoint happened between turns, and the last step belongs to the commit's noted turn (or is human when the commit has no note). The record `id` is derived from the commit, so exporting twice gives the same record.

Limits: edits the user makes in nvim while a turn runs count as the turn's; two sessions working in one worktree at once make their overlap `unknown`; a squash commit is credited to the session that ran the squash; `model_id` and `content_hash` are not filled.

### Resuming elsewhere

Built (one machine): `agents.export session=… dir=…` (`clubmate agent export <session> [dir]`) writes a bundle directory: `session.json` (the record), `log.jsonl` (Clubmate's log) and `refs.bundle` (a git bundle of the session's checkpoint refs and the notes of the commits its turns made, without commits already on a branch, remote or tag; other sessions' notes stay out). It refuses a running session and an existing `dir`. `agents.import dir=… [branch=…] [note=…]` (`clubmate agent import <dir>`) fetches the checkpoint refs into the repository (never overwriting existing ones) and adds the bundle's notes that name the imported session and one of its checkpoints, on commits the repository has and that have no note yet, adds a closed copy of the session with the original transcript (`importedFrom`), and starts a new session in the caller's worktree (or a new `.worktrees/<branch>`) whose first prompt is the handoff brief of the copy. The copy and the new session link like a handoff. This works across worktrees, workspaces and clones on the same Mac; the bundle's checkpoints need their parent commits in the target repository (fetch the session's branch first).

Design (not built): continuing the agent's own conversation instead of a brief. Claude Code keeps it in `~/.claude/projects/<encoded cwd>/<agentSessionId>.jsonl`; the bundle would carry that file (found by globbing for the id), import would place it under the target cwd's project directory and `session/resume` with the old `agentSessionId`, falling back to the brief when resume fails. On another machine the same bundle works once the branch is pushed and fetched; a relay or shared store for bundles belongs with the mobile companion work.

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
