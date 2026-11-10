# Agent graphs

An agent graph is a team of agent sessions wired together: each **node** is a role (coder, reviewer, tester …) played by one agent session, or a **step** Clubmate runs itself (a shell command or a Clubmate command, see [Steps](#steps)); each **edge** says "when this node ends a turn (or reports an outcome), prompt that node". The `graphs` plugin reads a graph from a JSON file, starts it with `graphs.run`, and shows each run in the Graphs section.

A graph adds no new engine. A run compiles into what already exists:

| Graph part | Built from |
|---|---|
| Node | One `agents.new` session, started idle |
| Edge | One `triggers.add` trigger: on `agents.turn` (or an emitted `event.*`) of the source session, call `agents.prompt` on the target session. An edge into or out of a step, or one with `maxFires`, is fired by the graphs plugin itself instead (see [Steps](#steps)) |
| Start | `agents.prompt` to each start node, after all triggers exist |
| Outcome | The node itself calls `triggers.emit`; the edge's trigger matches the outcome and the emitting session |
| Run view | `agents.sessions` status per node, rendered as SDUI |

So everything triggers do still holds: a busy target holds the prompt until its turn ends, 10 runs within a minute pause the trigger, destructive commands ask for approval. The Triggers section lists a run's edges like any other triggers.

## Graph file

Graphs live in `.clubmate/graphs/<name>.json` of the caller's worktree (an agent's own worktree, else the current worktree of each folder of the workspace; from another worktree the repo's main checkout is not looked in), or in `~/.clubmate/graphs/<name>.json` for [your own graphs](#your-own-graphs), which show in every workspace. `<name>` is lowercase letters, digits, `-` and `_`. Unknown fields are errors.

```json
{
  "title": "Review loop",
  "description": "A coder implements, a reviewer reviews, until the reviewer approves.",
  "maxTurns": 12,
  "nodes": [
    {"id": "coder", "role": "Coder", "start": true,
     "prompt": "Implement the task in this worktree with tests, and commit when they pass. Task: {{input}}"},
    {"id": "reviewer", "role": "Reviewer", "outcomes": ["approved", "changes"], "end": ["approved"],
     "prompt": "Review the latest commits of session {{node.coder}} in this worktree for bugs and missing tests. Do not edit code."}
  ],
  "edges": [
    {"from": "coder", "to": "reviewer", "on": "done"},
    {"from": "reviewer", "to": "coder", "on": "outcome", "outcome": "changes",
     "text": "The reviewer asks for changes: read its last reply with agents_transcript {\"session\":\"{{node.reviewer}}\"}, fix them and commit."}
  ]
}
```

The same file is `plugins/graphs/testdata/.clubmate/graphs/review-loop.json` (with one more `on: error` edge), which the tests load.

| Field | Meaning |
|---|---|
| `title`, `description` | Shown in the Graphs section and `graphs.list`. |
| `maxTurns` | Turn budget of a run, 1–200, default 20 (see [Stopping](#stopping)). |
| `nodes[].id` | Lowercase id, unique. At most 8 agent nodes, since an agent session runs at most 8 child sessions at once, and 16 nodes in all. |
| `nodes[].role` | Human name of the role; defaults to the id. |
| `nodes[].prompt` | The role brief. Required. |
| `nodes[].start` | Prompted when the run starts. At least one node. |
| `nodes[].branch` | Run the node's session in a new worktree on this branch (as `agents.new {branch}`); may use `{{run}}` and `{{graph}}`. |
| `nodes[].outcomes` | Words the node may report, e.g. `["approved","changes"]`; needed for `on: outcome` edges. |
| `nodes[].end` | Outcomes that finish the run, e.g. `["approved"]`; each one of the node's `outcomes`, and no `on: outcome` edge may fire on it. A step's `end` names one of its outcomes (a check) or `done` (an action). |
| `nodes[].afk` | Start the node's session in AFK mode (`agents.new {afk}`), for graphs that run unattended. |
| `nodes[].run` | Makes the node a step: a shell command, run with `/bin/sh -c` in the step's folder. One of `prompt`, `run` or `command` per node. No placeholders. |
| `nodes[].command`, `nodes[].args` | Makes the node a step that runs a Clubmate command with `args` (a JSON object; `{{run}}`, `{{graph}}`, `{{node.<id>}}` only), always as an agent call by the run's caller. |
| `nodes[].in` | Where a step runs: `main` (the repo's main checkout) or an agent node's id (that node's worktree); default the run's folder. |
| `nodes[].timeout` | A step's time limit in seconds, 1–7200, default 600. |
| `nodes[].exits` | A step's named outcomes by exit code, e.g. `{"3": "flaky"}`. |
| `maxSteps` | Step budget of a run, 1–500, default 50. |
| `edges[].maxFires` | Round cap of an edge, 1–50: past it the next firing waits for you (see [Round cap](#round-cap)). |
| `edges[].from`, `to` | Node ids. Self-loops are allowed. At most 32 edges. |
| `edges[].on` | `done` (turn ended; default), `asks` (turn ended with a question), `error` (turn failed), `outcome` (the node reported `outcome`). A `done` edge also fires on a turn that ends with a question, unless the node has an `asks` edge, which then takes those. |
| `edges[].outcome` | For `on: outcome`: one of the source node's `outcomes`. |
| `edges[].text` | The message to the target (an edge into a step takes none); defaults to "<source> finished its turn. Act on it. Its last reply (possibly cut; the whole transcript is in agents_transcript …): <reply>" (`{{payload.reply}}`). An `outcome` edge's default names the outcome and tells the target to read the reply with `agents_transcript`, since the outcome is reported before the reply is written. |

### Placeholders

| Placeholder | Filled by | Value |
|---|---|---|
| `{{input}}` | graphs | The run's `input`. |
| `{{run}}`, `{{graph}}` | graphs | Run id (`gr_…`), graph name. |
| `{{node.<id>}}` | graphs | That node's session id in this run. |
| `{{topic}}`, `{{seq}}`, `{{payload}}`, `{{payload.<field>}}` | triggers, when the edge fires | See [triggers](triggers.md#placeholders). Only in `edges[].text`. |
| `{{payload.reply}}` | triggers, when a `done`, `asks` or `error` edge fires | The source's last reply of the turn, cut at 4000 characters (`agents.turn`'s `reply`). Not in `outcome` edges: their event carries only `outcome`, `workspace` and `by`. |

`input` may not contain `{{`, so it can't smuggle trigger placeholders into an edge.

### What a node is told

The start nodes get `[Agent graph <name>, run <run>: you are node <id> (<role>)]`, their rendered prompt, the session ids of the other nodes, and, if they have `outcomes`, how to report one. Without `{{input}}` in the prompt, the input follows as `Task: …`.

A node that is not a start node gets that whole brief in front of every edge message, since an edge can't know whether it is the node's first message; a start node gets only the header line, until it is handed off (see [Handoff](#handoff)). This keeps every message self-contained, also after a handoff or a long context. Every brief also says that the reply goes to the next node, not to a person, so the node shouldn't end it with a question it doesn't need answered.

A node with `outcomes` is told to call the clubmate tool `triggers_emit {name: "graph.<run>.<node>", payload: "{\"outcome\":\"<word>\"}"}` before it ends its turn, and which of them end the run. The edge's trigger listens on `event.graph.<run>.<node>` and matches `outcome` and `by` (the emitting session), so no other session can report for it; `triggers.emit` itself refuses a `graph.<run>.<node>` name from any caller but the user and that agent node's current session (so no session can publish a step's outcome for observer triggers either). The plugin listens on `event.graph.*` itself for `end` outcomes, with the same `by` check.

## Steps

A step is a node Clubmate runs itself, without an agent, so its result is what the command returned, not what a model says it returned. A step can't be a start node; at most 8 agent nodes and 16 nodes in all.

**Check or action.** A step with an `on: outcome` edge is a **check**: its outcomes are `pass` (exit 0), the `exits` word of its exit code, or `fail` (any other exit), and `error`. Any other step is an **action**: `done` (exit 0) or `error` (any other exit). `error` also means it timed out, was killed by a signal (`killed by signal N`), was rejected or stopped, or Clubmate restarted while it ran. The outcome comes from the exit code alone: a `run` whose command the shell can't find exits 127, which is `fail` on a check (or its `exits` word) and `error · exit 127` on an action. On a step, `on: done` fires on any outcome but `error`; `on: asks` is invalid. A step's `error` with no `on: error` edge stops the run with reason `<Role> error · <reason>`.

**Edges.** Every edge into or out of a step, and every edge with `maxFires`, is fired by the graphs plugin, not by a trigger: it starts the step on the source's turn or outcome, and when a step ends it prompts the target itself (losing the trigger's busy hold: a prompt to a running node steers or queues). The plugin acts only on `agents.turn` events published by the agents plugin and outcome events published by `triggers.emit` (the event's source, which the core sets per connection), never on an outcome event for a step, and a step's result never travels over the bus: it publishes `event.graph.<run>.<node>` `{workspace, outcome, exitCode, durationMs}` afterwards for observers only. These edges don't appear in the Triggers section. A step fired while it runs or waits runs once more after it ends; if the run stops first, that queued firing ends `error · stopped` (also one held by a round cap or refused by the step budget), and after a restart `error · interrupted` without firing its `on: error` edges.

**What a step gets.** No message: an edge into a step takes no `text`. It runs with the core's environment minus every `CLUBMATE_*` variable but `CLUBMATE_STATE_DIR` and `CLUBMATE_RUNTIME_DIR` while they are not the defaults (so `clubmate` in a step reaches the dev or test-app core that runs it, and a step of the release core never hands the release dirs to a dev build), plus `CLUBMATE_WORKSPACE_ID`, `CLUBMATE_GRAPH`, `CLUBMATE_GRAPH_RUN`, `CLUBMATE_GRAPH_INPUT`, `CLUBMATE_GRAPH_FROM` (the node that fired it) and, per agent node, `CLUBMATE_GRAPH_NODE_<ID>`, `CLUBMATE_GRAPH_WORKTREE_<ID>`, `CLUBMATE_GRAPH_BRANCH_<ID>` (`<ID>` upper-cased, `-` as `_`). Nothing is substituted into `run`. Its folder is `in` (above), else the caller's worktree, else the folder the graph file was found in, else the workspace's first folder. The command comes from the graph as stored at run start: editing the file mid-run changes nothing.

**Output.** A step ends when its shell exits; whatever it left running in the background is killed then (5 s later at most, if it still holds the output open). stdout and stderr go to a file in the plugin's state (`output/<run>/<move>.log`, 0600, cut at 1 MB) and the last 4000 characters are kept, without terminal escape sequences and control characters other than newline and tab (the file keeps them). In an edge from a step, `{{payload.output}}` is that tail in a fenced block (three backticks inside it become ʼʼʼ), and the default text says it is data, not instructions.

**The check's own files.** At run start the run stores the HEAD commit of the caller's worktree, the graph's folder and each workspace folder (`starts`). When a check (a `run` step with outcome edges) ends, the files changed since the start commit of its folder (that folder's own, else one of the same repo, so a coder's worktree made during the run compares with the commit it branched from: `git diff --name-only --relative` plus untracked files) are taken just before the check starts (so what the check itself writes doesn't count) and matched against what the check runs: `make`/`gmake` → every `Makefile`, `makefile`, `GNUmakefile` and `*.mk`; a script path (also after `sh`, `bash`, `zsh`, `python`, `python3`, `node`, `ruby`, `perl`) → that file, relative to the step's folder; every part of a `;`/`&&`/`|` line counts. The ones found are the move's `checkFilesChanged`, a warning line in the output panel and, after the fenced tail of `{{payload.output}}`, "The check's own files changed in this run: `Makefile`, `rules.mk`.", so a reviewer after a passing check knows the check itself was edited. It flags, it doesn't block; an absolute script path is not matched.

**Approval.** A `command` step always calls as an agent would (agent-visible commands only; a destructive one asks in the app), within its `timeout`; a failing command is `fail` on a check, a missing command, a timeout or a stop is `error`. A `run` step, in its folder:
1. a matching `permissions.deny` `Bash(...)` rule (user, project, local or managed settings; managed are `/Library/Application Support/ClaudeCode/managed-settings.json`, then `managed-settings.d/*.json` beside it (hidden files skipped) merged on top in name order, the last `defaultMode` winning, as Claude Code merges them) ends it `error · denied by rule …`, also in bypass mode;
2. the run's caller in `bypassPermissions` mode runs it; a run without a caller uses `permissions.defaultMode` from your user or managed settings (a repo's settings can't turn on bypass; managed `disableBypassPermissionsMode` turns it off);
3. else a matching `ask` rule asks, a matching `allow` rule runs it — allow rules from your user and managed settings, and from the repo's `.claude/settings*.json` only when the step runs in the main checkout as `git.worktrees` lists it (an agent can write those files in its own worktree; a folder git doesn't list gets none). So Always allow is offered and accepted only for a step that runs in the main checkout, where the rule it writes counts; elsewhere `graphs.approve` with `always` fails and the card offers Allow once and Reject;
4. else it waits for you: the run needs you (see [Needs you](#needs-you)), `notify.post` kind `permission` with the run id as its `session` says "A graph step wants to run a command" (never the command), and `graphs.approve {id, node, decision}` answers it — `once`, `always` (also writes `Bash(<command>)` to the main checkout's `.claude/settings.local.json`; refused for commands with `*`, `(` or `)`, and when that file or its `.claude` folder is a symlink) or `reject` (`error · rejected`).

Allow rules match only simple commands (words of letters, digits and `_./:=@%+,-`, single-quoted words, one space between): anything with `;`, `&`, `|`, `$`, backticks, redirects, a `VAR=` prefix or other characters asks, unless you allow bare `Bash` and have no deny or ask rules. Deny and ask rules also match each part of a chained command, and a command that holds every word of a deny rule anywhere (`env rm`, `/bin/rm`, `sh -c 'rm …'`, `git -C . push`), or anything but plain words (an operator, a quote, a backslash, `$` or a backtick) while you have deny or ask rules, asks, also in bypass mode. Auto mode can't be reproduced outside Claude Code, so it counts as rules, then ask. Only you answer a step's approval; the agent that ran the graph isn't asked.

**Trust.** A project graph with steps, and a built-in one with steps (`review-gate`), runs only once you trusted its steps in that repo: `graphs.trust {graph, hash?}` stores a hash of every step's `run`, `command`, `args`, `in`, `timeout` and `exits`, of the routing (nodes' kind, `start` and `end`, every edge but its text) and of the limits (`maxTurns`, `maxSteps`, every agent node's `branch`, which steps get as `CLUBMATE_GRAPH_BRANCH_<ID>`, and the `afk` of an agent node a step's `in` names), scoped to the repo (its main checkout). Changing a step, rewiring the graph or changing a limit makes the file untrusted again (`graphs.list` sets `untrusted`, `hash` and `changed`: the step ids that differ from the last ones trusted in the same repo, plus `edges` and `limits`, and `trusted`: `{hash, steps, at}` as last trusted, for "was: …"); editing prompts, roles or edge texts doesn't. With `hash` (as `graphs.list` showed it), trust is refused if the file changed since. `graphs.run` of an untrusted graph fails `untrusted: …` for every caller. The limits joined the hash on 10 Nov 2026, so every graph with steps trusted before needs Trust once more (its `changed` lists `limits`). A built-in graph's or [your own](#your-own-graphs) graph's repo is the caller's worktree, else the workspace's first folder; such a graph without steps needs no trust. A project file that shadows a built-in or your own graph is a project file. In the Graphs section an untrusted graph's sidebar row reads `untrusted` in warning ink (the trust state is cached per graph file content, so a render looks it up only after the file or a trust changed), its page has an `untrusted` badge, Trust… (primary, ⌘↩) in place of Run, a warning card ("The file changed after you trusted it (Nov 1). Its steps run shell commands as you: review what changed, then Trust to run it again."; "review them" for a file never trusted), the Input field reading "Trust the file to run it", the definition row with "changed · trusted version 3f9a1c, now 8b02e7", the changed step cards tagged `changed`, and under the diagram "What changed since you trusted it" (each changed field as − old / + new). Trust… opens a sheet (the `sheet` node) over the page: "Trust ‹title›?", every step's role, kind and `changed`, its `$ command`, then "was: ‹old command›" and its timeout on a muted line, "A command outside the allow list still asks before it runs. Trust lasts until the file changes again.", then Cancel (the default, ⎋) and Trust (⌘↩), which calls `graphs.trust` with the hash the sheet showed (only while it is shown). The section refuses Trust, Continue and a step's approval sent as an agent's `ui.event` (`agent: true`), as `graphs.trust`, `graphs.continue` and `graphs.approve` refuse agents.

### Round cap

An edge with `maxFires: N` delivers its first N firings in a run; the next one waits for you: the run keeps it in `held` and needs you (see [Needs you](#needs-you)), `notify.post` kind `permission` with the run id as its `session` says "<From> → <To> reached its limit of N rounds", and `graphs.continue {id, edge}` delivers it and allows that edge one more round. Stopping the run drops what is held.

### Needs you
A running run needs you while a step waits for approval or an edge holds a firing past its round cap. The graphs plugin decides it (oldest first: the step's `waitingAt`, the held firing's time) and hands each workspace's list to the agents plugin (`agents.run-needs`, accepted only from the graphs plugin), which lists the run as its own entry in `agents.sessions` (see [agents § Needs you](agents.md#needs-you)) with the run's `number` and, for a step, its `role`: text "<Role> wants to run <command>" for a step, "<From> → <To> ran N of N rounds" for a cap. It is sent again whenever the agents plugin restarts (`agents.ready`).

## Your own graphs

A graph in `~/.clubmate/graphs/<name>.json` is yours: it shows and runs in every workspace and every worktree, with no copy in the repo. It is found after the project's graphs and before the built-in ones: a project's `.clubmate/graphs/<name>.json` of the same name wins over it, and it wins over a built-in graph of the same name. Otherwise it behaves like a built-in graph: its sessions and steps run in the caller's worktree (else the workspace's first folder), and a graph of yours with steps needs trust in each repo it runs in (see [Steps](#steps)). `graphs.list` lists it with `user: true` and its full `path`; a run of it has `user: true`. The Graphs section lists it with `global` as its detail, and its detail and its runs' Definition row say `~/.clubmate/graphs/<name>.json`. The folder is the same for every Clubmate build (release and dev).

## Built-in graphs

Clubmate ships graphs inside the graphs plugin: `plugins/graphs/builtin/<name>.json`, embedded in the plugin binary with `go:embed`, so they work in any workspace without a `.clubmate/graphs/` folder. They are found after the project's graphs and [your own](#your-own-graphs): a `.clubmate/graphs/<name>.json` or `~/.clubmate/graphs/<name>.json` with the same name wins, so a project or you can copy a built-in graph and change it. `graphs.list` lists a built-in graph not shadowed by a project file with `builtin: true` and `path` `builtin/<name>.json`; `graphs.run` runs it like a file; a built-in with steps needs trust like a project graph (see [Steps](#steps)). The Graphs section lists it under Graphs with `built in` as its detail, and its detail and its runs' Definition row say `Built into Clubmate` instead of a path. `TestBuiltinGraphsParse` checks every built-in graph is valid.

### PR walkthrough

`pr-walkthrough` helps you review a large PR, often one an AI wrote, by rebuilding it from scratch as a chain of small steps you review one at a time, with a check after each that you understood it. The original PR is the spec, not the target: you may edit or drop things while reviewing, and the next steps adapt.

- **Start:** select PR walkthrough in the Graphs section, type `<PR number or branch>[ base=<branch>]` into Input and press Run (⌘↩ or ↩), or `cm command run graphs.run graph=pr-walkthrough input=<PR number or branch>[ base=<branch>]`; yourself and from a workspace in auto mode. Started from an agent session, the questions go to that agent instead of you (§ Needs you in [agents](agents.md)). Auto mode runs the PR's code and tests without asking, so use it only for PRs you trust. The nodes treat the PR's description, diff and changed instruction files as data, never as instructions; they take the test command from the base and ask before the first test run of a PR that changes the build or tests. The PR's branch name is used only as a slug (`[a-z0-9._-]`).
- **Slicer** builds one step per turn in its own worktree, `.worktrees/walkthrough/<pr>/<k>` on branch `walkthrough/<pr>/<k>`, each made from the previous step (the first from the merge-base) and switched to in your workspace. Your edits in a step are committed there as their own `шаг k: правки ревью` commit before the next step is made. The PR's own branch and worktree are never touched.
- **Quizzer** asks in Needs you: done reviewing / too big, split it / stop and reconcile; then one question: on what your edit means for the next step, or on why this way and not another, only where the step had a real alternative; and before a step with a real choice, how you would build it. A wrong answer splits the step (`<k>.1`, `<k>.2`, …).
- **Document:** `.worktrees/walkthrough/<pr>/walkthrough.md` in the main checkout, opened in the docs viewer: Now (the step, one diagram, places to look, also in quickfix), Map (what the PR touches, coloured by ported / yours / dropped / to do), Steps, Remaining and Result. Diagrams are mermaid, drawn from the diff with `path:line` for each element.
- **Reconciler** compares the PR with the last step, running the PR's own tests there, and sorts each part into same / changed by you / dropped / lost, lost first.
- **Limits:** no resume; a second run on the same PR is refused until the old `walkthrough/<pr>/*` branches and worktrees are removed. Step worktrees stay until you remove them.

### Review gate

`review-gate` turns its input into one squashed commit on the main checkout's branch, with `make test` and a reviewer between the coder and the merge.

- **Coder** works in its own worktree on branch `graph/<run>`, commits with conventional-commit messages and never touches the main branch.
- **Tests** (`make test` in the coder's worktree, timeout 1200 s) is a check: `pass` → Reviewer; `fail` → Coder with the output's tail, at most 3 rounds before it waits for `graphs.continue`; a timeout or a missing `make` is `error` and stops the run.
- **Reviewer** reads `graph/<run>` against the main checkout's branch without editing and reports `approved` or `changes`; `changes` → Coder.
- **Squash into main** runs `scripts/graph/squash.sh` in the main checkout: it squashes `$CLUBMATE_GRAPH_BRANCH_CODER` into the branch checked out there as one commit and ends the run. The message is the branch's one commit, or the oldest subject with the other subjects as a list. It refuses (`error`, the reason in the output) when that variable is unset or its branch is missing, HEAD is detached or is that branch, the checkout has staged or unstaged changes (untracked files are fine), the branch has no commits the checkout lacks, the squash conflicts (it resets the checkout first) or changes nothing. It never pushes, rebases or forces.
- **Elsewhere:** the graph runs `make test` and `scripts/graph/squash.sh` from the repo, so it needs `graphs.trust graph=review-gate` once per repo (Trust… on its page), and a repo without them copies the graph to `.clubmate/graphs/review-gate.json` and changes those steps (and then trusts it). `graphs.run` of a built-in graph or one of yours checks first that every step whose command starts with a relative path (`scripts/graph/squash.sh`) finds it in the repo's main checkout, and fails at once naming the step and the missing file instead of failing at that step. Unattended, Tests and Squash need `Bash(make test)` and `Bash(scripts/graph/squash.sh)` allow rules, or bypass mode; the Tests step runs in the coder's worktree, where a repo's own allow rules don't count (§ Steps).

### Nightly

`nightly` is Clubmate's own nightly tidy and release. It is not built in but a project graph of the Clubmate repo (`.clubmate/graphs/nightly.json`), so it shows only there and runs only once you trusted its steps (`graphs.trust {"graph":"nightly"}`, again after any change to them); the "Nightly tidy" cron job runs it (`graphs.run {"graph":"nightly"}`, `owner: none`).

- **Tidy** (AFK session) follows the `nightly-tidy` skill and reports `changed` when at least one of its commits stays on `main`, else `nothing`, which ends the run.
- **Tests** (`make test`), **Companion UI tests** (`scripts/graph/companion-ui.sh`, timeout 3600 s), **Mac UI tests** (`scripts/graph/mac-ui.sh`, timeout 3600 s), **Push main** (`git push origin main`) and **Ship** (`scripts/graph/ship-next.sh`, timeout 7200 s) are actions in the main checkout, each on the previous one's `done`; a failing one stops the run with `<Role> error`, so a red phone or Mac suite keeps main unpushed and nothing ships. `make test` skips `dist/`, the release bundle's copy of the sources.
- `companion-ui.sh` runs `make companion-ui-test COMPANION_SIM=cm-nightly-ui` (a simulator of its own, erased and seeded each run). On a failure it prints each failing test with the first line of its failure and the `.xcresult` path at the end of its output (the step's tail and summary), and posts a `stuck` notification naming the failing tests, since a stopped run posts nothing by itself.
- `mac-ui.sh` builds the Debug app (`make app`) and runs `make mac-ui-test` (about 4 min); on a failure it posts a `stuck` notification naming the failing tests.
- `ship-next.sh` fetches tags, refuses when a `v*` tag already points at `HEAD`, takes the newest `vX.Y.Z` tag plus one patch and runs `scripts/release/ship.sh <version> <HEAD> $CLUBMATE_WORKSPACE_ID` in the foreground, so Ship lasts until the release publishes or fails. A restart mid-ship kills it (`error · interrupted`); `ship.sh` deletes a tag whose release failed, but a kill between tag push and publish leaves the tag; a SIGTERM, SIGHUP or SIGINT then posts a stuck notification saying where it stopped (a SIGKILL posts nothing).
- The cron run has no caller, so steps follow your `permissions.defaultMode`; in anything but bypass mode they need `Bash(make test)`, `Bash(scripts/graph/companion-ui.sh)`, `Bash(scripts/graph/mac-ui.sh)`, `Bash(git push origin main)` and `Bash(scripts/graph/ship-next.sh)` in the main checkout's `.claude/settings.local.json`, or they wait for you.
- The `nightly-release` skill stays for releases cut by hand.

### Design check

`design-check` is Clubmate's nightly design check, also a project graph of the Clubmate repo (`.clubmate/graphs/design-check.json`), trusted like `nightly`; the "Nightly design check" cron job runs it at 05:00 (`graphs.run {"graph":"design-check"}`, `owner: none`). Input `all` captures every board. Its agents follow the `nightly-design-check` skill, each its own section; its steps are scripts in `scripts/design/`, all in the main checkout. Everything a run writes is in `<main checkout>/.worktrees/design-check/<run>` (the run id lower-cased, `_` as `-`).

- **Scope** (AFK, start) runs `capture.sh --plan`, which writes the boards whose UI changed since the last run (from the `head` of the newest `clubmate-notes/design-check/*/manifest.json`; all of them on Sundays) to `<run>/boards`, and adds boards of views the path map missed.
- **Capture** (`capture.sh`, timeout 7200 s) is a check: in a detached worktree of `HEAD` it runs `make app`, starts a test instance with the fake agent, seeds it, snapshots each Mac board in its theme and the phone boards on a simulator of its own (`capture-phone.sh`; C-iPad is skipped), and writes `<run>/manifest.json`. `pass` → Judge; exit 3 is `nothing` (no board) → Report; `fail` or `error` → Report.
- **Judge** (AFK) judges the shots against the boards, files what no fixer may touch and reports `fixes` (briefs in `<run>/fixes.md`) → Fixer, or `nothing` → Report.
- **Fixer** (AFK) runs at most 3 fix children on `design/<date>-<area>` and lands the ones that pass with `scripts/design/land.sh <branch> <run>`, which squashes into the main checkout's branch as one commit and records it in `<run>/merged`; it refuses a non-empty index, an uncommitted file the branch changes, a detached HEAD and a conflicting squash. `landed` → Tests, `nothing` → Report.
- **Tests** (`tests.sh`, timeout 5400 s) is a check over what landed: `go test` with every `CLUBMATE_*` unset (without `dist/` and `.worktrees/`) and `third_party/jsonrpc2`, plus `make app`, the ClubmateProtocol / ClubmateKit (without the known red `testRestartUnderTrafficKeepsReceivingUpdates`) / CompanionKit swift tests and `make companion` when the landed commits touch those clients; log in `<run>/tests.log`. It runs in the capture tree checked out at the main checkout's `HEAD` (warm build, and other sessions' uncommitted work in the main checkout can't turn it red), else in the main checkout. `pass` → Report; `fail` or `error` → **Revert** (`revert.sh`), which reverts exactly the commits in `<run>/merged`, newest first (never other sessions' commits), and refuses a detached HEAD, a non-empty index or a commit not on the branch; both outcomes → Report.
- **Report** (AFK) leaves the Claude Design package, the report in `clubmate-notes/design-check/<date>.md` with the shots and manifest next to it, and one notification; on every path, also failures.
- **Clean up** (`cleanup.sh`, end) stops the run's test instance and simulator, removes the run dirs, then every `design/*` branch (all of them count as fix branches) that landed with `land.sh`, has no commits of its own or whose commits are all on the checked-out branch as the same patch (`git cherry`, e.g. squashed by hand), with its worktree; a worktree with uncommitted changes or a process in it stays. Scope's `error` goes here too, so every path ends with it.
- Every script has a dry run (`capture.sh --plan` / `--dry-run`, `tests.sh --dry-run`, `cleanup.sh --dry-run`) and takes the run from `CLUBMATE_GRAPH_RUN`, else `DESIGN_CHECK_RUN`, else `manual-<date>`.
- Unattended, the steps need `Bash(scripts/design/capture.sh)`, `Bash(scripts/design/tests.sh)`, `Bash(scripts/design/revert.sh)` and `Bash(scripts/design/cleanup.sh)` in the main checkout's `.claude/settings.local.json` (or bypass mode); the agents reach the design project with the `Artifact` tool from the same settings, so no node has a `branch`.

## Running

| Command | Does |
|---|---|
| `graphs.list` | Graph files, your own graphs (`user` set) and built-in graphs (`builtin` set; `error` set on invalid ones) and the workspace's runs. |
| `graphs.run {graph, input?}` | Starts a run and returns it with node sessions and edge trigger ids. |
| `graphs.show {id}` | One run, each node with its session title and live status, and from the graph its `end` (the outs that end the run) and, for an agent node, its `outcomes`. A run has a `number`, its place among the workspace's runs of the same graph (1, 2, …, kept when other runs are deleted, though a new run reuses the number of a deleted newest one; runs from before numbering get theirs by start time when the plugin loads), shown as "Review gate · #4" on the phone. An agent node also has `outcome` (the outcome its last finished turn reported, e.g. `approved`, the word to show instead of `status`; absent while its session runs, waits or failed, and when none was reported), `durationMs` (the time its finished turns took, from the move's start to the turn's end; absent for runs from before it was kept) and, while its session runs, `startedAt` of the current turn. |
| `graphs.stop {id}` | Marks the run stopped, cancels its running steps, then deletes its triggers; sessions stay open and running turns go on. A trigger it can't delete (the triggers plugin is down) fails the call naming it, with the run already stopped; stop again to retry. `graphs.delete` keeps the run listed until its triggers are gone. |
| `graphs.delete {id}` | Stops the run and removes it from the list. |
| `graphs.approve {id, node, decision}` | Not agent-visible. Answers a step waiting for approval (see [Steps](#steps)). |
| `graphs.continue {id, edge}` | Not agent-visible. Delivers an edge's held firing past its round cap. |
| `graphs.trust {graph, hash?}` | Not agent-visible. Trusts a graph's steps (a project file's, your own or a built-in's) as they are now, in this repo. |
| `graphs.trusted {path}` | Not agent-visible. `{graphs: [name]}`: the graphs in `.clubmate/graphs/` of a folder, in the workspace or not, whose steps are trusted as they are now or that have none. `git.repo-facts` reads it. |
| `graphs.show {id}` per step | A step node's `status` is `not started`, `running`, `waiting` or its outcome, `command` is the command or Clubmate command the graph gives it (also before it first runs), and `last` is its last move: `command`, `dir`, `allowedBy` (`rule Bash(…)`, `mode bypass`, `you`, `command`), `exitCode`, `outcome`, `reason`, `tail`, `durationMs`, `waitingAt` (when it began to wait for you), `startedAt` (when its process started), `output` (the full output file), `checkFilesChanged` (see The check's own files). Moves carry `summary` (the Haiku summary of a failed step or a finished agent turn) and an agent turn's `reply` (its last 600 characters). A step waiting for approval also has `pending`: `{command, dir, main, rule}`, `rule` being the `Bash(…)` rule Always allow would write to `main` (absent when the step doesn't run in the main checkout or the command can't be a rule). `graphs.show`, `graphs.stop`, `graphs.approve` and `graphs.continue` are `remote`: the phone answers a run from them. |
| `graphs.reveal {id}` | Not agent-visible. Selects the run in the Graphs section and focuses it; Open graph in Triggers calls it. |

`graphs.list`, `graphs.run`, `graphs.show`, `graphs.stop` and `graphs.delete` are agent-visible, and all are non-destructive. `graphs.run` runs in three steps, in this order because `triggers.add` checks its target session exists:

1. `agents.new` every node, idle (title `<graph title> · <role>`, `run`, `runTitle` and `runAfter` (the edges into the node: `{role, on, outcome}` of each, for the Code chat's "runs after Coder"), so the Agents sidebar lists the node sessions under one run row: the run title with `<Role> needs you`, `Running` or the last change as its detail, each node labelled by its role; selecting a node, also through the run's Open, unfolds the row).
2. `triggers.add` every edge.
3. `agents.prompt` every start node.

A failure in step 1 or 2 deletes the triggers made so far, closes the sessions and returns the error; one in step 3 stops the run.

### Who runs it

| Caller | Node sessions | Edge triggers | Cleanup |
|---|---|---|---|
| An agent session | The caller's children, in its worktree (or `branch`), with its mode, model and advisor | Owned by the caller, fire as the caller | The triggers go when the caller's session closes; the run is then marked stopped |
| User, CLI, cron job without owner | Top-level sessions | Ownerless, fire with no caller | Stay until `graphs.stop`, the turn budget or Remove |

The calls carry the caller (`agent`, `caller`) like a trigger does, so a graph can't do more than its caller could by hand. An agent may stop or remove only runs started by itself or its child sessions.

### Stopping

A run stops when:
- a node reports one of its `end` outcomes (reason `<node> reported <outcome>`); the turn it ends with is neither counted nor fires anything, since the run is already stopped;
- `graphs.stop`, or Stop in the section;
- its node sessions together finished `maxTurns` turns (counted from `agents.turn` that ends `done`, `error` or `closed`, so a wait for a permission or an answer doesn't count; the edge of the last turn may still fire, since the trigger and the count see the same event);
- the agent session that ran it closes;
- a start prompt failed.

A run's step budget (`maxSteps`) stops it too (reason `step budget of N reached`), and so does a step's `error` without an `on: error` edge. A step's output goes with its run: `graphs.delete` and a deleted workspace remove `output/<run>/`. Stopping deletes the run's triggers and ends its running steps (SIGTERM to the process group, SIGKILL 5 s later; `error · stopped`); a step waiting for approval ends the same way. After a restart, a step that was running or waiting ends `error · interrupted` and its leftover processes are killed: the plugin keeps the step's pid and process start time, and sends its process group SIGTERM, then SIGKILL 5 s later, only while a process with that pid and start time still runs. Cancel or close sessions with `agents.cancel` and `agents.close`, or in the Agents section. A stopped or deleted run's top-level node sessions that ended `done`, or that the run never reached (still `idle`), close themselves an hour after they went quiet, like any other done session (see [agents](agents.md)); a node that ended in `error` stays. Steps are processes, not sessions, so they leave nothing in Agents.

### Handoff

`agents.handoff` closes a node's session and continues it in a new one. The plugin sees the new session's `agents.changed` `created` event, finds a node whose session it was handed off from (`handoffFrom`, followed along a chain), moves the node to the new session and re-points every edge of the run with `triggers.update` (match and message: the briefs name the peers' sessions). The node is marked `handedOff`, and from then on gets its whole brief in front of every edge message, also as a start node. The handoff's own first turn counts and fires the node's edges like any other. Handing off the session that ran the graph still stops the run: its triggers are owned by that session.

Cycles are the point (coder ↔ reviewer), so loops are bounded by `maxTurns`, the triggers' 10-per-minute pause and the reviewer's `approved` outcome ending the run.

## Graphs section

The sidebar starts with How graphs work, then a tree of two foldable groups, each with its count; a group row is an `action` row, so selecting it does nothing and keeps the selection where it was. **Graphs** comes first: every graph `graphs.list` returns for the workspace's folders, by name, labelled with its title; the detail is `N nodes`, `built in` for a [built-in graph](#built-in-graphs), or `invalid` in danger ink for a file that does not parse. **Runs** follows (only when there are runs), newest first: title, start time and turns (or the stop reason), and an icon for the most urgent node state: waiting a warning `!`, error a danger ✕, running an accent ▶; a run that needs you (see [Needs you](#needs-you)) has the warning `!` and reads `approve` (a step waits) or `N of N` (a round cap) at the right. A running row's menu has Stop; a stopped one's has Remove. Graph files are read on each render of the section, not watched.

Selecting a graph shows its detail: a header with the title, the name as meta and Run (primary, ⌘↩); the description (three lines, Show all), an Input field (↩ runs too), the definition path (`Built into Clubmate` for a built-in one) and its turn budget; then its diagram, drawn like a run's (below) but structure only: each card has the role and its ways out, with no state glyph, state word or counts; every edge and port is solid and muted, the start and end markers as in a run, no History; the cards don't look clickable (no hover tint, no `↩ open` in the status bar) and clicking one or ↩ does nothing. Hovering an edge (or Tab / ⇧Tab to it) shows what it does in words (`when Coder is done → prompt Reviewer`, `when Coder fails …`, `when Coder asks you …`, `when Reviewer reports changes …`); a definition has no triggers yet, so its edges open nothing. Run calls `graphs.run` as the user (not as an agent), so the node sessions are top-level and their questions come to you; the button reads Starting… until the run is made (other graphs' Run is disabled meanwhile), then the new run is selected. A failed start keeps the input and shows the error under it. An invalid graph shows an `invalid` badge and its error instead of Input and Run.

How graphs work (selected while there are no graphs and no runs, or when you pick it) explains the section in place: a header with one line on what a graph is (agent sessions chained by their turns, every edge a trigger, a step's exit code routing the run) and Ask an agent to write one (⌘N, primary); a ghost Coder → Reviewer flow (`done` into Reviewer, whose outs read `approved → end` and `changes → Coder`); Parts of a graph, a table of agent node, check step, action step, hand-off (`on: done`), asks or fails (`on: asks`, `on: error`), outcome, round cap (`maxFires`) and end, each with what it does, its field, how many of your graph files use it (`Yours`: the workspace's project graphs and your own, not the built-in ones) and an Ask an agent link (↩ on the selected row); then Files, Trust, Budget, Needs you and Runs in one line each. Ask an agent (⌘N, `graphs.new`, also from the app menu anywhere in Graphs) starts an agent session titled `New graph` that asks you what roles and hand-offs you want, writes `.clubmate/graphs/<name>.json` (or `~/.clubmate/graphs/<name>.json` for every workspace), checks it with `graphs.list`, doesn't run it, and opens in Agents; from a part row its prompt names that part. The agent may work in a repo without this doc, so the prompt carries the file format in one paragraph.

The main view shows the selected run or graph, else the newest run, else the first valid graph, else How graphs work. A run's detail is a header with the title, `running · turn N of M` (or `stopped`), a `<Role> needs you` badge, Open <node> (⌘↩; while a step runs and no node session waits or failed, Open ‹Step› output, which selects the step's output panel and opens its output file), then Stop run (⌘.) while it runs or Remove (⌘⌫, in the header's ••• menu) once it is stopped; the input, start time, turns, stop reason and definition path (`Built into Clubmate` for a [built-in graph](#built-in-graphs)); then the run's diagram (the `graph` SDUI node). Nodes are cards laid out in layers from left to right (longest path from the start node, back edges left out): a state glyph, the role, the state word in its tone (`waiting for you`, `idle · 3 turns`, `closed · 4 turns`, `not started`, or the end outcome; a run from before move journals reads `idle` or `closed` with no count), and one row per way out (outcomes, then `done`/`asks`/`error`) with a port on the card's right edge and `×N` once taken. Edges run from a row's port to the target's input port: never taken dashed and faint, taken solid and muted, the last transition in accent; back edges route over the cards, edges that skip a layer under them. A start marker points into each start node and an end marker (double ring, filled once the run ended through it) follows each `end` outcome. The waiting node's card is warning-tinted with a pulsing glyph, a not-started one dashed. The diagram fits the pane width down to 80% and scrolls sideways below that; − / % / + / Fit at its top-right, `-` `+` `0`, pinch and ⌘/⌃+wheel zoom it (50–200%, kept per run until the app quits). Click a card or press ↩ on the focused one (j/k move focus) to open its session; a node whose session is not running or waiting and that has a finished turn shows that turn's panel first (see below) and opens the session on a second click or ↩. Each edge carries the trigger the run made for it: hovering it (or Tab / ⇧Tab to it) shows it in words as on a definition, and while the run runs clicking it (or ↩ on it) selects that trigger in Triggers (`openEdge`; the plugin sends the Triggers sidebar a `select` and focuses `triggers.main`); a stopped run's triggers are deleted, so its edges open nothing. Under the diagram a collapsed `History · N moves` row (`h`; not shown while the run has no moves, as runs from before move journals) opens one swimlane per node: each move a dot (the current one with its node's live glyph, one whose turn ended in an error a danger ✕, and one a stopped run left open that reported nothing a faint dashed circle, unless its session still runs or waits on that turn, which keeps the live glyph), joined to the move that caused it by a curve labelled with the outcome (`you` for a turn the user started), the move that ended the run leading on to the end marker (the plugin names it, `end`); a step's move is a bar from its start to the next move (open-ended while it runs) labelled with its outcome and duration (`pass · 2:10`, `fail · 1:04`) in the state's tone; once a node runs a second time the moves are bracketed in rounds (`round 1`, `round 2`), counted from that node's moves; a move lasts until its turn ends `done`, `error` or `closed` (the session was closed mid-turn), so a node waiting on a question or permission stays current; hovering a hop lights up its edge on the diagram, clicking a finished agent move shows its turn panel (a second click opens its session at that turn; a running or waiting one opens at once), and the lanes open scrolled to the latest move. Run and session ids are not shown; a card's tooltip is its session title.

Steps on the diagram (decision 9, `Graph-Steps` board): a step's card is square with a left rule and its command on a `$ …` strip (middle-cut, the full command on hover), and the small word at the right says `agent`, `check` or `action` (a step with an out-edge on pass, fail or a named outcome is a check). Its state word: `not started`, `running` with the elapsed time ticking on the client and a bar that fills over the timeout, `waiting for you` (amber card), `pass · 48 s` (teal), `fail · 1 min 04 s` (neutral ✕: a route, not an alarm), `error · timed out` (red card). An edge with `maxFires` reads `×n/N` on a run and `at most N` on the definition; while its next firing is held it turns amber and dashed with `×N/N · waits for you`. Under the diagram, before History (beside it when the pane is at least 800pt wide and History is open):

- a run waiting at a round cap: a warning card "‹From› → ‹To› reached its limit of N rounds. Continue allows one more round; Stop run ends the run.", the header badge `waiting for you · round cap` and Continue (primary, ⌘↩, `graphs.continue` of that edge) before Stop run;
- a step waiting for approval: the agent permission card ("Run a command?", "‹Role› · a step of ‹Graph›", the command, Allow once ⌥1 · Always allow… ⌥2 (shows the `Bash(…)` rule and "in the main checkout (‹path›)" first, the rule going to its `.claude/settings.local.json`; absent when the step doesn't run in the main checkout or the command can't be a rule) · Reject ⌥3, answered with `graphs.approve`), then "in ‹folder› · not in the allow list · Reject ends the step with error · rejected"; the header badge reads `waiting for you` and has no Open;
- the turn panel of the selected agent node's turn (a finished turn picked on the diagram or in History): the role, the outcome (`changed`, else `done`), `turn N` and how long it took, then a summary of what the agent did in that turn, Open ‹Role› (opens the session at that turn) and a muted note. The summary comes from Haiku (`claude -p --model claude-haiku-5-5`, no tools, no settings, in the temp dir), asked once when the turn ends with the turn's compact transcript (`agents.transcript fromTurn`, its last 32 KiB) and kept on the move (`summary`); until it lands the panel reads "Summarizing…", and without one (no `claude` on the PATH, a test instance, a failed call, a turn from before summaries) it shows the end of the agent's reply (`reply`, kept on the move, 600 characters, clamped to three lines with Show all) or "No summary of this turn.".
- the output panel of the selected step (a click on a step card selects it; by default the latest step that ran): the role, `round n` when it ran more than once, the outcome, exit code and duration (while it runs `running · 1 min 12 s`, the time since it started, again with each 2 s refresh), for a step that ended in anything but `done` or `pass` a Haiku summary of what broke (asked once when the step ends with the command, the exit code and the output file: its last 32 KiB plus the `FAIL`/`panic:`/`error:` lines before them; "Summarizing…" meanwhile, nothing when no summary came, and "summary by Haiku" in the note under the buttons), the check's own files changed in this run as a warning line when there are any, the last 12 lines of its output in a sunken mono box (`text` style `block`), Full output (opens the output file in Neovim; `o` on a focused step card does the same) and Copy (the tail), "last 12 of N lines" when cut, N counting every line the command printed (the step records it when it ends); "No output — the command printed nothing." with the command and folder in the box when it printed nothing. While the step runs the box follows its output file: the plugin reads the file's tail again every 2 s while the workspace shows Graphs and a run has a running step, "Nothing printed yet." until it prints, and "‹Role› is still running: the tail follows its output." under it. There is no read-only terminal pane yet (`terminal.new` takes no command), so Full output of a running step opens what it printed so far in Neovim.

## Limits of this slice

- **Fan-in.** An edge fires per source turn; there is no "wait for A and B". Two edges into one node give two prompts (the second held while the node is busy). See [Join nodes](#join-nodes) for the planned design.
- **Restarts.** Triggers see only events published while they run (see [triggers](triggers.md#firing)); a turn that ends while Clubmate is down fires nothing, and neither does a handoff made then.
- **Outcome edges carry no reply.** A node reports its outcome before it writes its final message, so `on: outcome` edges point the target at the transcript instead.
- **End state is explicit.** A run finishes on an `end` outcome; one whose last turn fired no edge and reported no `end` outcome stays `running` until `graphs.stop` or the turn budget. Detecting "no edge can fire any more" from outside the triggers is racy: when `agents.turn` arrives the edge's prompt may not have reached its target yet, and prompts held for a busy target are invisible to the plugin.
- **Runs from before handoff support** (no stored graph) don't follow handoffs.
- **Trust.** `.clubmate/graphs/` is inside `.clubmate/`, so in a repo whose worktree hooks you trusted, changing a graph asks for hook trust again (see [worktree hooks](worktree-hooks.md#trust)). Running a graph without steps needs no trust: it starts only what its caller could start. A graph with steps needs its steps trusted (see [Steps](#steps)).

## Security

What steps promise, and how it's tested (`plugins/graphs/*_test.go`, `TestStepSec_*` and `TestRedTeam_*`):

- A step's `pass` is its exit code: no agent, event or printed line can report it.
- Nothing outside the plugin starts a step: there is no command for it, and forged turns or outcomes fire nothing.
- What runs is what was trusted and allowed: the command is frozen at run start, nothing is substituted into it, allow rules match only simple commands and deny rules hold in bypass mode.
- A step is bounded: timeout, step budget, round caps, killed on stop and after a restart.
- Its output is data: fenced in prompts, kept on this Mac, never in a push.
- Every step leaves a move with what ran, where and who allowed it, including rejected ones.

Known gaps: a node can still edit what a check calls (a `Makefile`, a script), so `make test` can be made to pass — the step only flags it (The check's own files in [Steps](#steps)), `protect` globs that block it come later; a process of your user can reach `graphs.approve` through the CLI, like every agent-mode guard (see [the protocol](sdui-protocol.md)); a step waiting for you shows on its card, as a notification and in Needs you.

## Design notes

### Model

A graph is a directed graph of **roles**. A role is a prompt template plus optional outcome vocabulary; a run binds each role to one agent session. Edges are reactions to the two signals a session gives: its turn ended (`done`, `asks`, `error`), or it reported an outcome. Edge conditions are payload matches (the triggers filter), not expressions: routing on content happens inside the agents, which know the content, and they say so with an outcome word. This keeps the graph declarative and checkable before anything runs.

Compiling a run into sessions and triggers, instead of a graph engine, was chosen because both are already persisted, rate-limited, visible and caller-checked. The cost is the limits above; a dedicated engine (still on the bus) becomes worth it when fan-in, handoff edges or per-edge history are needed.

### Next steps

1. **Join nodes**, see below.
2. **Idle end:** finish a run without an `end` outcome once every node is idle and no trigger holds a prompt for one (needs the triggers' held state, and a quiet period).
3. **Run timeline:** per-run history of which edge fired when, merged from the edge triggers' history (`triggers.show`).
4. **Sub-graphs:** a node whose role is another graph.
5. **Graph from the composer:** `/graph review-loop <task>` in the Agents composer.

### Join nodes

Not built: "wait for A and B" can't be a trigger, since a trigger fires per event and has no AND across events.

- **Field:** `nodes[].join: true`. The node is prompted once every edge into it fired since its last prompt from the join, with one message holding each edge's text in edge order.
- **Validation:** a join node is not `start`, has at least two incoming edges, and its incoming edges come from different nodes.
- **Engine:** incoming edges of a join node are not triggers. The graphs plugin subscribes to `agents.turn` and `event.graph.*` itself, matches them as `edgeTrigger` would (status, `asks`, `by` and `outcome`), keeps the fired edges per run in the stored run, and calls `agents.prompt` itself when the set is complete, clearing it.
- **Lost against triggers:** the `busy` hold (a bare `agents.prompt` to a running node steers or queues instead), the 10-per-minute loop pause and the Triggers section listing; the turn budget still bounds a run. Restarts lose nothing stored, but events published while Clubmate is down still fire nothing.
- **Later:** `join: ["a", "b"]` naming a subset, and timeouts ("after 10 minutes, go with what arrived").

### Ideas taken from n8n

| n8n | Here |
|---|---|
| Trigger nodes start workflows (cron, webhook, event) | A run starts from `graphs.run`, which cron and triggers can call: cron `triggers.emit` → trigger → `graphs.run`. |
| AI Agent node with tools and memory | A node is a full agent session with Clubmate MCP tools and its own transcript. |
| Execution view: each node's input/output and status per run | Graphs section: per run, each node's session and live status; the transcript is one click away in the Agents section. |
| Pinned data and re-running from a node | Turn checkpoints (`refs/clubmate/cp/…`) and handoff; a later "re-run from node" prompts one node of a stopped run again. |
| Error workflow | `on: error` edges to a fixer or notifier node. |
| IF / Switch nodes | Outcome words plus `on: outcome` edges. |
| Sub-workflow node | Sub-graphs (next steps). |
| Wait node, merge node | Not yet: [join nodes](#join-nodes). |

Not taken: n8n's expression language over JSON items. Messages here are prompts, and routing on content is left to the agents.

### Native editor

A canvas in a Graphs main slot, drawn natively (SwiftUI `Canvas` plus hit-testing), editing the same JSON file so git stays the store:

- **Nodes** are cards: role, model/mode chips, outcome chips as output ports, a start marker. Double-click opens the prompt in nvim (`editor.open` on the JSON with the node's line) rather than an in-app text editor.
- **Edges** are drawn from an output port (`done`, `asks`, `error`, each outcome) to a node; the edge label shows `on` and the first words of `text`.
- **Run overlay:** the same canvas during a run, nodes tinted by session status, the last fired edge animated, a click on a node opens its session in the Agents section.
- **Keyboard first:** a fuzzy "add node" palette, Tab between nodes, Enter to edit, arrows to move; key changes go into `docs/shortcuts.md`.
- **SDUI:** needs a new `graph` node type (`nodes`, `edges`, positions, status per node) in `docs/sdui-protocol.md`; positions live in the file as `nodes[].at: [x, y]`, ignored by the runner.

Until then a graph is edited as JSON, with `graphs.list` reporting parse errors.
