# Agent graphs

An agent graph is a team of agent sessions wired together: each **node** is a role (coder, reviewer, tester …) played by one agent session, each **edge** says "when this node ends a turn (or reports an outcome), prompt that node". The `graphs` plugin reads a graph from a JSON file, starts it with `graphs.run`, and shows each run in the Graphs section.

A graph adds no new engine. A run compiles into what already exists:

| Graph part | Built from |
|---|---|
| Node | One `agents.new` session, started idle |
| Edge | One `triggers.add` trigger: on `agents.turn` (or an emitted `event.*`) of the source session, call `agents.prompt` on the target session |
| Start | `agents.prompt` to each start node, after all triggers exist |
| Outcome | The node itself calls `triggers.emit`; the edge's trigger matches the outcome and the emitting session |
| Run view | `agents.sessions` status per node, rendered as SDUI |

So everything triggers do still holds: a busy target holds the prompt until its turn ends, 10 runs within a minute pause the trigger, destructive commands ask for approval. The Triggers section lists a run's edges like any other triggers.

## Graph file

Graphs live in `.clubmate/graphs/<name>.json` of the caller's worktree (an agent's own worktree, else each folder of the workspace). `<name>` is lowercase letters, digits, `-` and `_`. Unknown fields are errors.

```json
{
  "title": "Review loop",
  "description": "A coder implements, a reviewer reviews, until the reviewer approves.",
  "maxTurns": 12,
  "nodes": [
    {"id": "coder", "role": "Coder", "start": true,
     "prompt": "Implement the task in this worktree with tests, and commit when they pass. Task: {{input}}"},
    {"id": "reviewer", "role": "Reviewer", "outcomes": ["approved", "changes"],
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
| `nodes[].id` | Lowercase id, unique. At most 8 nodes: an agent session runs at most 8 child sessions at once. |
| `nodes[].role` | Human name of the role; defaults to the id. |
| `nodes[].prompt` | The role brief. Required. |
| `nodes[].start` | Prompted when the run starts. At least one node. |
| `nodes[].branch` | Run the node's session in a new worktree on this branch (as `agents.new {branch}`); may use `{{run}}` and `{{graph}}`. |
| `nodes[].outcomes` | Words the node may report, e.g. `["approved","changes"]`; needed for `on: outcome` edges. |
| `edges[].from`, `to` | Node ids. Self-loops are allowed. At most 32 edges. |
| `edges[].on` | `done` (turn ended, not asking; default), `asks` (turn ended with a question), `error` (turn failed), `outcome` (the node reported `outcome`). |
| `edges[].outcome` | For `on: outcome`: one of the source node's `outcomes`. |
| `edges[].text` | The message to the target; defaults to "<source> finished its turn. Read its last reply with agents_transcript … and act on it." |

### Placeholders

| Placeholder | Filled by | Value |
|---|---|---|
| `{{input}}` | graphs | The run's `input`. |
| `{{run}}`, `{{graph}}` | graphs | Run id (`gr_…`), graph name. |
| `{{node.<id>}}` | graphs | That node's session id in this run. |
| `{{topic}}`, `{{seq}}`, `{{payload}}`, `{{payload.<field>}}` | triggers, when the edge fires | See [triggers](triggers.md#placeholders). Only in `edges[].text`. |

`input` may not contain `{{`, so it can't smuggle trigger placeholders into an edge.

### What a node is told

The start nodes get `[Agent graph <name>, run <run>: you are node <id> (<role>)]`, their rendered prompt, the session ids of the other nodes, and, if they have `outcomes`, how to report one. Without `{{input}}` in the prompt, the input follows as `Task: …`.

A node that is not a start node gets that whole brief in front of every edge message, since an edge can't know whether it is the node's first message; a start node gets only the header line. This keeps every message self-contained, also after a handoff or a long context.

A node with `outcomes` is told to call the clubmate tool `triggers_emit {name: "graph.<run>.<node>", payload: "{\"outcome\":\"<word>\"}"}` before it ends its turn. The edge's trigger listens on `event.graph.<run>.<node>` and matches `outcome` and `by` (the emitting session), so no other session can report for it.

## Running

| Command | Does |
|---|---|
| `graphs.list` | Graph files (`error` set on invalid ones) and the workspace's runs. |
| `graphs.run {graph, input?}` | Starts a run and returns it with node sessions and edge trigger ids. |
| `graphs.show {id}` | One run, each node with its session title and live status. |
| `graphs.stop {id}` | Deletes the run's triggers; sessions stay open and running turns go on. |
| `graphs.delete {id}` | Stops the run and removes it from the list. |

All are agent-visible and non-destructive. `graphs.run` runs in three steps, in this order because `triggers.add` checks its target session exists:

1. `agents.new` every node, idle (title `<graph title> · <role>`).
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
- `graphs.stop`, or Stop in the section;
- its node sessions together finished `maxTurns` turns (counted from `agents.turn`; the edge of the last turn may still fire, since the trigger and the count see the same event);
- the agent session that ran it closes;
- a start prompt failed.

Stopping deletes only triggers. Cancel or close sessions with `agents.cancel` and `agents.close`, or in the Agents section.

Cycles are the point (coder ↔ reviewer), so loops are bounded by `maxTurns`, the triggers' 10-per-minute pause and the reviewer's `approved` outcome having no edge.

## Graphs section

The sidebar lists the runs, newest first: title, start time and turns (or the stop reason), and a badge with the most urgent node state (`waiting`, `running`, `error`). A row's menu has Stop and Remove. The main view shows the run: title and id, state and turns, the input, and a tree of nodes. A node row shows role, id, `start`, its session title and a status badge coloured by state; its children are its outgoing edges (target node, `on done` / `on outcome changes`), muted once the run is stopped.

## Limits of this slice

- **Fan-in.** An edge fires per source turn; there is no "wait for A and B". Two edges into one node give two prompts (the second held while the node is busy).
- **Handoff breaks edges.** `agents.handoff` moves a session to a new id, and the run's triggers still name the old one. No handoff edges yet.
- **Restarts.** Triggers see only events published while they run (see [triggers](triggers.md#firing)); a turn that ends while Clubmate is down fires nothing.
- **Payload.** `agents.turn` carries status, not the reply, so edges tell the target to read the source's transcript instead of pasting it.
- **Trust.** `.clubmate/graphs/` is inside `.clubmate/`, so in a repo whose worktree hooks you trusted, changing a graph asks for hook trust again (see [worktree hooks](worktree-hooks.md#trust)). Running a graph needs no trust: it starts only what its caller could start.

## Design notes

### Model

A graph is a directed graph of **roles**. A role is a prompt template plus optional outcome vocabulary; a run binds each role to one agent session. Edges are reactions to the two signals a session gives: its turn ended (`done`, `asks`, `error`), or it reported an outcome. Edge conditions are payload matches (the triggers filter), not expressions: routing on content happens inside the agents, which know the content, and they say so with an outcome word. This keeps the graph declarative and checkable before anything runs.

Compiling a run into sessions and triggers, instead of a graph engine, was chosen because both are already persisted, rate-limited, visible and caller-checked. The cost is the limits above; a dedicated engine (still on the bus) becomes worth it when fan-in, handoff edges or per-edge history are needed.

### Next steps

1. **Join nodes:** a node with `join: [a, b]` prompted once all of them reported since its last turn. Needs state per run, so the graphs plugin listens itself rather than through triggers.
2. **Handoff-aware runs:** follow `handoffTo` and re-point the triggers of a handed-off node.
3. **Reply in the edge:** an `agents.turn` field with the last message's first lines (under the 64 KiB bus cap) as `{{payload.reply}}`.
4. **Run timeline:** per-run history of which edge fired when, merged from the edge triggers' history (`triggers.show`).
5. **Sub-graphs:** a node whose role is another graph.
6. **Graph from the composer:** `/graph review-loop <task>` in the Agents composer.

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
| Wait node, merge node | Not yet: joins (next steps). |

Not taken: n8n's expression language over JSON items. Messages here are prompts, and routing on content is left to the agents.

### Native editor

A canvas in a Graphs main slot, drawn natively (SwiftUI `Canvas` plus hit-testing), editing the same JSON file so git stays the store:

- **Nodes** are cards: role, model/mode chips, outcome chips as output ports, a start marker. Double-click opens the prompt in nvim (`editor.open` on the JSON with the node's line) rather than an in-app text editor.
- **Edges** are drawn from an output port (`done`, `asks`, `error`, each outcome) to a node; the edge label shows `on` and the first words of `text`.
- **Run overlay:** the same canvas during a run, nodes tinted by session status, the last fired edge animated, a click on a node opens its session in the Agents section.
- **Keyboard first:** a fuzzy "add node" palette, Tab between nodes, Enter to edit, arrows to move; key changes go into `docs/shortcuts.md`.
- **SDUI:** needs a new `graph` node type (`nodes`, `edges`, positions, status per node) in `docs/sdui-protocol.md`; positions live in the file as `nodes[].at: [x, y]`, ignored by the runner.

Until then a graph is edited as JSON, with `graphs.list` reporting parse errors.
