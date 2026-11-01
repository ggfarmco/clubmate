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
- Git worktrees: `git.worktrees` lists every git folder's worktrees, `git.worktree-switch` switches a folder to one of them, `git.worktree-new` creates `.worktrees/<branch>` and switches to it, and `git.worktree-remove` removes a linked worktree (never the main checkout or one with uncommitted changes). Both run the repo's [worktree hooks](worktree-hooks.md). Create and remove return `{id}` at once; `git.worktree-op-status id=…` waits up to 1 s for the outcome (the app instead gets the `git.outcome` event).
- Agent sessions: `agents.sessions` lists the workspace's agent sessions with their status, `parentSession`, `childStatus` (a done or idle orchestrator whose child sessions still run or wait), `needsYou` and `needsYouSince` (see [Needs you](#needs-you)), `activity` (`{title, at}` while it runs: the title of the tool call it is in or last started, "Editing reserve.go", and when that call started; the phone's running row says "Editing reserve.go · 4 min" from it; a session whose transcript was dropped from memory has none until it is read again) and notification policy (`notify`, `notifyEffective`), `agents.notify session=… policy=all|needs-input|muted` sets the policy of the caller's own session or one it started (see [notifications](notifications.md#per-session-policy)), and `agents.transcript session=…` reads one session's transcript from Clubmate's own log (`last=true` keeps only the last turn and leaves out the modes, config options and commands catalogs; `compact=true` drops the images in tool calls' content and the commands catalog; tool calls never carry `rawOutput` or `_meta`, which stay only in the log; `since=<cursor>` returns only what changed after an earlier result's `cursor`).
- Child agent sessions (no approval): `agents.new` starts a session, optionally in a new worktree `.worktrees/<branch>` made from the caller's worktree without switching the workspace, and sends it a first `prompt` (`notify` sets its notification policy); `agents.prompt` sends any session a follow-up (added to the running turn when the agent supports steering, else queued); `agents.cancel` stops a running turn; `agents.permission` answers a permission request and `agents.elicitation` a question (AskUserQuestion, an MCP form or sign-in link), but only of sessions the calling agent session started (directly or through its children); `agents.wait` waits without polling until one of the listed sessions stops running (done, waiting for permission, asking a question, error or closed), up to 300 s per call, and returns their status with the pending permission's options; it also returns early with the user's queued messages to the calling session.
- Failed turns: a turn the agent ends with an internal error (`-32603`, e.g. Claude Code's "ended the turn without returning results for tool calls" when the advisor tool is rate-limited) is retried once, 3 s later, within the same turn: the transcript shows a notice and a Clubmate message asking the agent to continue. A second failure leaves the session in error, as any other error does. Network errors ("No response from API", "Connection refused", `ECONNREFUSED`) are not retried within the turn, since Claude Code has already waited and retried; see the network error paragraph under orchestration.
- Handoffs: `agents.handoff session=… note=…` continues a session that is not running in a fresh one (same worktree, agent, parent, advisor and notification policy), seeded with a brief from Clubmate's log (the task, the last prompt and reply, the last turn's stop reason and checkpoint, and how to read the full transcript) followed by `note`; the old session is closed but can be reopened, and each links to the other. Agents may hand off only sessions they started. In the app, a session's menu has "Hand Off to New Session", and a session whose agent cannot resume offers "Hand off to a new session".
- Attribution: `agents.blame path=…` (or `commit=…`) tells which agent session, turn and prompt made a file's lines or a commit; `agents.trace rev=…` exports a commit's or a range's changed lines as [Agent Trace](https://agent-trace.dev/) records; see [History and attribution](#history-and-attribution).
- Moving a session: `agents.export session=… dir=…` writes a session bundle, `agents.import dir=…` continues it in another worktree or workspace; see [Resuming elsewhere](#resuming-elsewhere).
- Reminders and schedules: `cron.add` calls a command later or repeatedly, e.g. `agents.prompt` to your own session tomorrow morning; the job is deleted with your session unless `owner: "none"`. Watching your children needs no cron job: Clubmate wakes you when their turns end (see below). See [cron](cron.md).
- Reacting to events: `triggers.add` calls a command when a bus event arrives, e.g. `agents.prompt` to a reviewer session when another session's `agents.turn` ends; `triggers.emit` publishes your own `event.<name>`, which `cron.add` can schedule. See [triggers](triggers.md).
- Agent graphs: `graphs.run graph=… input=…` starts a team of child sessions from `.clubmate/graphs/<graph>.json` (one session per role, one trigger per edge) and the Graphs section shows the run; `graphs.list`, `graphs.show`, `graphs.stop`. See [agent graphs](agent-graphs.md).
- Answers on your behalf (asks you first): `agents.answer session=… text=…` shows you the text and, once you allow it, sends it to the session as your own message.
- Web research: `browser.open`, `browser.snapshot`, `browser.click` and the other `browser.*` tools drive a hidden WebKit browser in the app, one set of tabs per agent session, and `browser.handoff` asks the user to solve a captcha or sign in; see [agent browser](browser.md).
- Showing a doc: after writing a spec, plan or report the user should read, `docs.open {path}` (MCP `docs_open`) shows it rendered in a popup with mermaid diagrams drawn; see [docs viewer](docs-viewer.md).
- Testing the app: `testapp.list` lists running test instances and `testapp.call instance=… method=…` calls one of them (see below).

Hidden helpers and commands not marked for agents stay out of reach.

## Import your nvim config

**Import my nvim config** in the command palette starts an agent session in the Agents section that reads `~/.config/nvim` and moves your own keymaps, options, autocmds and plugins into Clubmate's Neovim user layer (`~/.config/clubmate`, or `~/.config/clubmate-dev` for development builds), following the guide bundled at `nvim-distro/skill/SKILL.md`. It skips what Clubmate's defaults already cover and asks you before adding destructive or machine-specific mappings. It needs an open folder workspace and never changes `~/.config/nvim`. The first time Neovim starts in a workspace workspace while `~/.config/nvim` exists, it offers the same import once.

## Testing the app

Agents test Clubmate in their own invisible instances instead of taking over the user's screen. `scripts/test-app.sh start [name]` (name defaults to the branch) launches the Debug build with scratch state, runtime and `XDG_*` dirs under `/tmp/clubmate-test-<name>`, its own core and its own defaults suite, in test mode (`CLUBMATE_TEST_MODE=1`): it never activates, its windows are transparent and ignore the mouse, app-modal dialogs are skipped and every approval is denied. `stop [name]` quits it and removes the dirs, `list` shows running ones, `cm [name] -- <args>` runs the CLI against one, with that instance's workspace as `CLUBMATE_WORKSPACE_ID`. `start [name] -- --sdui-gallery` shows the SDUI gallery with a core but no workspace.

- `ui.snapshot {out, png?, width?, height?}` (CLI `cm ui snapshot --out <dir> [--size 640x800] [--no-png]`): the app draws each of its windows itself (main window with titlebar, popovers, sheets, settings) to `w<window id>.png`, terminals included, and writes `w<window id>.json`: every rendered SDUI node with its frame in window points, its parent, its menu entries, plus every terminal's visible text and native menus listed as text. Flags: `outside-window`, `wider-than-parent`, `overlaps-sibling`, `zero-size`, `truncated` (text nodes, measured). The result's `summary` lists only flagged elements, or `0 problems`. `--size` resizes the main window first.
- `ui.present {target, shown}` (CLI `cm ui present <target> [--hide]`): opens `inbox`, `worktree`, `switcher`, `settings` or the `approval` fixture sheet without a click; menus are never opened, they appear as text in the layout. `card:<node id>` expands a transcript tool card (`--hide` collapses it), and with it the summary row of its group; card ids (`<session>/tool/<tool call id>`) are listed by `cm ui dump` and not checked, so a wrong id changes nothing.
- `ui.key {keys, focus?}` (CLI `cm ui key [--focus <node id>] <combo>...`): `focus` first makes the view under that SDUI node (an id from the snapshot layout; with duplicate ids any match, so prefer a unique one) first responder, e.g. a text field. Each combo then goes to the active key monitors (sidebar trees, the composer), else, when it has ⌘, to the window's key equivalents (view shortcuts; never the app menu), else as a key down and up to the first responder of the workspace window; the result lists the route each combo took (`monitor`, `shortcut`, `responder <class>`). A combo is `[cmd+][ctrl+][opt+][shift+]<key>`, `<key>` a US-layout letter, digit or punctuation, `return`, `tab`, `space`, `delete`, `esc`, `up`, `down`, `left` or `right`; `cm ui key --focus description o n e opt+return t w o` types two lines. Keys are refused while a sheet or alert is open or a terminal has focus, so they can't answer an approval or run shell commands. The test app never becomes active, so it has no key window: menu shortcuts (⌘N New, ⌘W) are not sent and Tab does not move focus. Run their commands instead (`cm command run backlog.new` focuses the new task field), and focus a sidebar with `cm command run ui.focus section=<section> slot=<section>.sidebar`.
- Over MCP: `testapp.call` proxies `ui.snapshot`, `ui.present`, `ui.key`, `ui.get`, `ui.event`, `command.list`, `command.run` (always as an agent call), `workspace.list` and `workspace.open` to `/tmp/clubmate-test-<instance>`; `ui.snapshot`'s `out` defaults to and must stay inside `/tmp/clubmate-test-<instance>/snapshots`.

The user's own app never answers `ui.snapshot`, `ui.present` or `ui.key`: only a test-mode app registers them, and test mode needs a dev build with non-default state and runtime dirs.

## Approvals

Destructive actions (closing or killing sessions, typing into a session, answering an agent session as you, restarting the editor, killing orphan processes, removing a worktree) ask you first. Clubmate shows an alert naming the action, the workspace, the targeted session and every argument in full, with Allow and Deny. Deny is the default. If nobody answers within 60 seconds, or the app isn't open, the action is refused.

An agent session calling these tools is the parent of the sessions it starts with `agents.new` (Clubmate knows the caller from `CLUBMATE_AGENT_SESSION`, which every agent session has). A child starts with the parent's mode, model, effort and advisor, not the worktree's remembered ones, and at most 8 children of one parent run or wait for permission at once. A prompt an agent sends through `agents.prompt` or `agents.new` is logged as coming from that agent (`agent:<session id>`) and reaches the target with `[From Clubmate agent session "<title>" (<id>), not from the user]` in front, so it never passes for your approval; from an agent without an agent session it is logged as `agent:unknown` and starts with `[From another agent session, not from the user]`. Prompting an idle child also counts against the limit of 8. Agents without an agent session (Claude in a terminal, `clubmate command run --agent`) start top-level sessions and cannot answer permission requests.

A child in its own worktree merges its finished, tested work into its parent's branch in the parent's worktree, never into `main`; a child sharing its parent's worktree, or whose parent is on the default branch, only commits and the parent merges. Children never remove worktrees or branches: once a child's turn ends with its branch merged into the parent's branch and its worktree clean and unused, Clubmate closes the child (it stays listed as Closed) and removes its worktree and branch itself, without asking. A session that has children is told to write to you only the children's questions that need your answer, to answer their permission requests itself with `agents.permission`, and to pass your answers on with `agents.answer` rather than paraphrasing them.

Clubmate wakes an orchestrator when a child's turn ends: once a child is done, ends in an error or waits for permission, and the parent is not in a turn of its own, the parent gets `[Clubmate] Child "<title>" (<id>) is <status>.` (the title's first line, quoted), one line per child in one message when several finish together (within a second, or while the parent's turn was still running). A child the parent already saw through `agents.wait` is not reported again, so an orchestrator that waits on its children is not woken twice. A suspended parent is resumed for the message like for any prompt. Pending wake-ups live in memory and are lost if the plugin restarts.

When Clubmate restarts (an update, a core handover) while a turn is running, the turn is cut off. On start, the agents plugin sends `[Clubmate] Your turn was cut off by a restart; continue from where you stopped.` to every cut-off session that is not closed, including one that was waiting for a permission or an answer (it asks again), so every session picks up its work without you.

When a turn ends with a network error ("No response from API", "Connection refused", `ECONNREFUSED`) in a session that is in AFK mode or belongs to a live orchestration, Clubmate sends it `[Clubmate] Your turn ended with a network error (<reason>). Continue the task from where you stopped.` 1 min later, then after 2, 4 and 8 min and every 10 min after that, up to 8 times; the transcript shows a "Network error; retrying in …" notice with the attempt. A turn that ends without an error resets the count. While a retry is pending, the parent is not woken for the child's error; once the retries run out, the session stays in error with a "Network error; stopped retrying" notice and the parent is woken as for any error. Any other session stays in error; send it a message to continue. Pending retries live in memory and are lost if the plugin restarts.

AFK mode is for when you step away. Turn it on with the AFK item in the composer options menu of a top-level session, or tell the agent you are away ("я афк") and it turns it on itself with `agents.afk on=true`; "я вернулся" turns it off. While it is on, each prompt of that session and of all its child sessions tells the agent to work without asking you, make and note its own decisions, hand large work to child sessions and subagents, and finish with one short report of commits, decisions and open questions. The instruction is not shown in the transcript. `agents.afk-default on=true|false` sets whether new top-level sessions start in AFK mode.

From the Agents sidebar you can run a task in its own worktree end to end: press "New in worktree…", type a branch name and press Start (or Return) to start a session in a new `.worktrees/<branch>` made from the workspace's worktree (as `agents.new` with `branch`). When its turn is done, "Review changes" in the session header opens the diff, and, with Finish set to Ask, "Merge to main" (after a confirm) tells the agent to run the tests and, if they pass, squash its branch into `main` as one commit and remove its worktree and branch, the same steps as Finish "Merge to main". Clubmate refuses the merge while the session runs, while another agent session works in that worktree, or when the main checkout has uncommitted changes.

"Context" (⌘I) in a Claude session's header shows what the session sees instead of its transcript, to find out why it did not do something you expected: the CLAUDE.md files it loaded (user, project, memory and nested ones, with their current text), the skills, MCP servers (and the ones that need a sign-in or failed), hook context, subagent types, tools and deferred tools it was offered, each with how often it used it; unused ones are muted. It reads the session's own Claude log (`$CLAUDE_CONFIG_DIR` or `~/.claude`, `projects/*/<agent session id>.jsonl`) and follows it as it grows; it shows the current state, not its history. "Transcript" (⌘I) goes back.

The status bar shows how much of the Claude subscription is used: `5h 42% ↻19:03 · 7d 81% ↻Wed`, the 5-hour and weekly limits with their reset times, in the warning colour once Claude warns about a limit and in the danger colour once it is reached. The numbers come from the rate-limit reports Claude attaches to agent replies, so they appear after the first reply of an agent session, are kept across restarts, and miss usage from Claude outside Clubmate until the next reply; a limit disappears once its reset time passes. The tooltip names when they were last updated; `agents.usage` returns them.

Agent mode is a cooperative guard for well-behaved agents, not a security boundary: any program running as your user can reach Clubmate directly.

## Status

One map for both clients: the glyph (SF Symbol), the word and the tone of each state a session shows. The agents plugin decides the state once: `status` in `agents.sessions` and in the session header (`header.status`), with the parent states `childrenRunning`/`childrenWaiting` (`childStatus` in `agents.sessions`) and `asks` (a top-level `done` session with `needsYou`). The Mac (sidebar, header, Graphs) and the phone copy this table and never pick a glyph or tone on their own.

| State | Glyph | Word | Tone |
|---|---|---|---|
| `running` | `play.circle` | elapsed · what it does ("4 min · Editing reserve.go"), else "Running" | accent |
| `waiting` (permission or question pending) | `exclamationmark.circle` | "waiting" ("asking" for a question) | warning |
| `asks` (done, its last message asks you) | `exclamationmark.circle` | "asking" | warning |
| `done` | `checkmark.circle` | age ("2 h ago") | none (secondary ink) |
| `idle` (no turn yet) | `circle` | "idle" | none |
| `error` (the turn failed) | `xmark.circle` | "Stopped · <first line of the error>", "Stopped · error" without one | danger |
| `closed` | `archivebox` | "Closed" | none |
| `childrenRunning` (done or idle, a child runs) | `play.circle` | "Waiting for child sessions" | accent |
| `childrenWaiting` (done or idle, a child waits) | `exclamationmark.circle` | "Child session waiting" | warning |

Waiting is amber (warning) everywhere: sidebar, Graphs, header badge ("waiting for you"), phone. A failed turn is `error`, never `done`. An untyped internal error is retried once in the same turn ("The turn failed; retrying once"); only when the retry fails too does the session end in `error`, so a turn that recovers on the retry is `done`.

## Needs you

A session needs you when a permission or a question is pending (status `waiting`), or when a top-level session's turn is done and its last message asks a question (`asks`). A child session's `asks` is for the session that started it, not for you, so it never needs you on its own; its pending permissions and questions do. A session that stopped with an error does not need you; it shows as Stopped. The ending `?` is a fallback: a top-level Claude session is told, through its system prompt (`_meta.systemPrompt.append` on `session/new` and `session/resume`), to ask questions that need your answer with AskUserQuestion, which arrives as a pending question (`elicitation/create`) and needs no guessing. Child sessions are not told this; their questions go to the session that started them. The agents plugin computes this once and sends it as `needsYou` in `agents.sessions`. The Mac sidebar's "Needs you" filter (a top-level session that needs you, or whose child waits for permission), the Mac status bar's "N need you" and the phone (its "Needs you" list, waiting counts, session groups and glyphs) show exactly these sessions; neither client derives it on its own. The phone falls back to the same rule only for a Mac whose Clubmate does not send `needsYou` yet.

Oldest first. A session that needs you also carries `needsYouSince` in `agents.sessions`: when its pending request was made, or when its turn ended with the question. Every list of sessions that need you is ordered by it, oldest first, ties by session id: the Mac sidebar's "Needs you" filter (a top-level session counts with the oldest request in its subtree, and its worktree group with its oldest session), the bell's pinned items and the phone's "Needs you" list across workspaces and Macs.

A session that needs you is never folded. In the Mac sidebar the worktree group, the graph run and every parent above it are expanded while it needs you, whatever the user folded. On the phone a subagent or child that asks is a nested row under its parent, one step in (24pt) with no separator between them, never hidden in a folded group.

## Permission card

A pending permission request shows the same content on the Mac and the phone; the agents plugin builds it once: the Mac gets it as the `permissionCard` node ([SDUI](sdui-protocol.md#node-types)), the phone as `card` `{text, title, command?, caption?, options}` on each permission entry of `agents.transcript` (left out for agent callers), with the same option fields:

- Title by tool kind: "Run a command?" for a command; other kinds keep the sentence "Claude wants to edit notify.go".
- The command itself (`command`), in a monospaced box.
- The request's description as the caption, always attributed: "Claude says: Run the notify tests".
- Options in this order: Allow once ⌥1 · Always allow… ⌥2 · Reject ⌥3; "Always reject" (Claude's `reject_always`) sits in the card's ••• menu without a key. These labels replace Claude's own ("Yes", "Yes, and don't ask again for go test commands", "No") only when the request has the usual one option per kind; anything else (plan-mode exits, several allow options) keeps Claude's labels and gets ⌥1–3 in order.
- Always allow… asks first and stores nothing until confirmed: the option carries the `rule` Claude would store and its `scope` ("in shop on this Mac", rules go to the project's local Claude settings on this Mac). The Mac shows them in a popover anchored to the button (Cancel ⎋ default, ⌥2 again confirms) and the phone in its confirm sheet; only the confirm sends the option. Claude does not send the rule over ACP, only its label, so the plugin rebuilds it from that label for commands (`Bash(go test:*)`, one rule per command prefix Claude names); when the label names no command (paths, "similar" commands, other tools) there is no `rule` and the confirm shows the option's label instead.

## Names in the UI

What the agents plugin renders uses names, not ids or command names: a session is its title, a graph run its graph's title; prompts Clubmate sends an agent ("[Clubmate] Child "Fix tests" (agt_…) is done.", the handoff summary) keep the ids for the agent but show without them in the transcript. A detached worktree is labelled "detached · 03a9ff5f". Themed UI is in sentence case ("New in worktree…"); Title Case only in context menus.

## Transcript folding

Tool calls are folded on the Mac and the phone: consecutive calls are one actions row, and a call's output opens only when you click or tap it. A failed call is folded too: on the Mac its card says Failed and the actions row "N failed", on the phone its title is in the danger colour and its card or group gets a danger border. A user message longer than 40 lines or 4000 characters shows its first 12 lines with "Show all N lines"; opened, it scrolls inside a capped box, since one row that tall leaves the transcript scrolled past its end, blank. Nothing in the transcript opens itself. The design boards say failures expand; that is overruled. A closed ```` ```mermaid ```` fence in an agent message is drawn as a diagram (one hidden web view per app renders it to an image, cached by source, theme and width); it keeps its aspect and is at most 480 pt tall on the Mac and 420 pt on the phone, shows its source as a code block while it renders, and stays a code block when mermaid cannot parse it, when it would be over 16384 px on a side or 24 million pixels even at 1×, or while the fence is still open. Clicking it on the Mac opens a sheet with the diagram fitted to the window (pinch, zoom buttons, Esc closes); tapping it on the phone opens it full screen (pinch, double tap to zoom). A change to this rule lands in both clients in the same commit.

## Docs in the transcript

A tool call is a doc when its status is `completed` and either its kind is `edit` (Write, Edit, MultiEdit) and one of its `locations` or `diff` paths ends in `.md`, `.markdown`, `.mmd`, `.html` or `.htm` (case-insensitive, `pkg/docext`), or it has a `resource_link` whose `file://` URI does. The agents plugin decides this once and sends the first such path, absolute against the session's worktree, as the entry's `doc` field in `agents.transcript`; a failed, cancelled or still running call, a delete or move, and a call on other files have none. The Mac card gets an Open action (before Open in nvim) and the phone's tool row an Open button; both show the doc in the [docs viewer](docs-viewer.md) — the Mac through `openDoc` on `agents.main`, which runs `docs.open {path, session}`, the phone with `docs.read`. Like the rest of a call, Open is inside the folded actions row until you open it. Neither client checks the path itself.

`agents.docs {session?}` (not an agent tool, remote) lists those docs as `{docs: [{path, session, at}]}`: one per path at its latest `at` (when that call began), newest first, from that session and its subagents' calls, or without `session` from every session of the workspace. It reads each session's transcript, so a dropped one is read back first. `docs.list` builds the docs lists on it (see [docs viewer](docs-viewer.md#docs-list)).

When an agent opens a doc with `docs_open`, the phone shows "Opened <file name>" with an Open button above the composer of that session's screen, for 8 s or until tapped; on any other screen it shows nothing.

## CLI

Inside a Clubmate terminal, `clubmate` is on `PATH` and `$CLUBMATE_WORKSPACE_ID` names the current workspace.

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

Its `initialize` instructions end with "Working in Clubmate" (`internal/mcp/clubmate.md`, embedded in the binary): split a task, run independent parts as child sessions with `agents_new` and a branch, collect them with `agents_wait`, and use `cron_add` and `triggers_add` instead of polling. Every MCP client that connects gets it, in any project.

- Tools are named after the command or capability with `.` and `-` replaced by `_` (`editor_open`, `workspace_list`, `terminal_close`). A plugin command that provides a capability appears under the capability's name only.
- Every tool takes an optional `workspace` id. It defaults to `$CLUBMATE_WORKSPACE_ID`, then the most recently opened workspace.
- If Clubmate isn't running, the tools still list the core primitives, and calls fail with "Clubmate isn't running". The server never starts Clubmate itself.

To connect Claude Code, use **Settings → Agents → Connect Claude Code**. It runs:

```sh
claude mcp add --scope user clubmate -- "/Applications/Clubmate.app/Contents/Resources/bin/clubmate" mcp
```

Development builds register the server as `clubmate-dev` instead, so both can be connected at once. The button is shown only when `claude` is on your login shell's `PATH`; Clubmate looks once per launch, so restart Clubmate after installing `claude`. It says "Connected" only when the registered server runs this copy of Clubmate; if it runs another one, it shows that command and offers "Reconnect" (remove, then add; if the remove fails, nothing is added). Clubmate never edits Claude's config files itself.
