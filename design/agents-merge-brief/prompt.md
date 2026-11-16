# Clubmate: Graphs and Gates move into Agents

Hi. This is a new question for the Clubmate boards in this project (`design/language`, `design/companion`). The user has decided to merge two rail sections into Agents but isn't sure how they should look there. Please give us directions as boards.

The screenshots of today's app are already in this project's assets; read each `/_blob/<id>` like an attached image. "640" is the Mac window at its 800 pt minimum (the narrowest it gets), as in earlier checks.

| Screen | Mac 1280 dark | Mac 1280 light | Mac 640 dark | Mac 640 light |
|---|---|---|---|---|
| Agents: tree with two graph runs and four gated landings; a lander waiting on Land anyway / Stop | `/_blob/8d229cb382c1a4de80524ab4f6545540` | `/_blob/a05e1d737e439ebbd7c846852dd572a2` | `/_blob/d0e00ba10c347fa1a8f60ac1a2ff1e87` | `/_blob/01375364049be5716c17c72d25c2df7b` |
| Agents: the run row clicked (no run page opens) | `/_blob/4deca2de869a2c875ffdc7b2e5e1803a` | `/_blob/58bd4186907725523519532c702d9207` | — | — |
| Graphs: a run waiting for you | `/_blob/8475bbe55a0be951a2dbf9149bcc5038` | `/_blob/d31174d5ccd724079ae18e286da65469` | `/_blob/aa4342a7380a7203bf04f1d517d5e18c` | `/_blob/704d700645b031691c1733ab1ed37f95` |
| Graphs: a definition | `/_blob/969dd550d1f080709f07d08c6bb259d5` | `/_blob/1783164893172f075d6f8e5c782633cd` | `/_blob/3ff44445060f4f68227af3ea7fb8e3ce` | `/_blob/7cfdb468edd0238e382f5f36f94b1018` |
| Gates: list + detail, a round of changes selected | `/_blob/bcc74d5534ed8909bb0327e988313b85` | `/_blob/fe1327f6fa2ae8179e8716bdbc5608c8` | `/_blob/1564ab73d9983b6d73dd3ebca75ffa8d` | `/_blob/a7aed95934feedc8e928fb81f59d8e7e` |
| Triggers (stays its own section; for comparison) | `/_blob/6123cbdb591914c5c955d30d62100b2c` | `/_blob/6c7bd6593614069e29355d1798c92af1` | `/_blob/72ba13623eca1b7bd17ff02d6dd8e3a2` | `/_blob/3cd55180e07071d969c40768ebcc2c49` |

| Phone screen | Dark | Light |
|---|---|---|
| Agents list: runs that need you | `/_blob/bf4c391971b96dfad3cc9178bbeca737` | `/_blob/6fdfd2ea3554eed2f3a9c3b4d0c47d88` |
| Native run screen | `/_blob/d313575814de6990662ac965d000f696` | `/_blob/3f248c0b4138c3dbd44d1bc0b7a04cde` |
| More (Graphs and Gates are listed here) | `/_blob/bee7433148ef24dc8c1727a37fe56c2c` | `/_blob/5f38c491df54436d8d1ac32a6c31c94d` |
| More → Graphs | `/_blob/dbe9b8d7b795c6d74d2098cde6b14edf` | `/_blob/d0ead74b1a839f62450b3798319952c2` |
| More → Graphs → a run | `/_blob/e1982922fcabc4d936d3afd262028b4e` | `/_blob/27c9210a4fa1036e559c0a98978e6a08` |
| More → Graphs → a definition | `/_blob/ec03883ea7db6033e365a262b056869a` | `/_blob/a5495f9ccf06301ee62105a983f763b2` |
| More → Gates | `/_blob/46391ef3240fec26f425c70374e9e9f1` | `/_blob/cb30dc75817bd393ba478baa88f8c38c` |
| More → Gates → a gate | `/_blob/18eaa4dc9996d96f9155f0c67ab39f9c` | `/_blob/516e98a440acb1f4022ae3986d17c96e` |

## 1. What is decided

- The **Graphs** rail item (order 37) and the **Gates** rail item (`agents.gates`, order 38) go away. Both live inside the **Agents** section.
- **Cron** and **Triggers** stay separate sections.
- A graph run is already drawn in the Agents tree: one run row (graph glyph, run title, detail such as "Coder needs you" or an age) with its node sessions under it.

## 2. What exists today (see the screenshots)

**Agents.** The tree groups sessions by worktree (`main`, `feat/inbox-groups`, …). Under `main` there are two Review loop runs, each a row with Coder and Reviewer under it. In this scene the run itself needs nothing: its Coder session waits on its own permission, so the run row's detail reads "Coder need…" in plain ink and the Coder row reads "waiting". When the run itself needs you (a step approval or a round cap), its row reads the request ("Tests wants to run make test") in warning ink and counts once under Needs you. That case wasn't captured on the Mac; the phone's run rows show it. A run row has no page of its own. Clicking it only moves the cursor, and ↩ expands or collapses it. This one stays open, because a run with a node that needs you never folds. The main pane keeps the last session (`agents-run-*`). The run's page (the diagram, Input/Turns/Definition, History, Open Coder ⌘↩, Stop run ⌘.) exists only in Graphs.

**Gated landings in Agents.** A lander's row shows its gate state as toned detail: `review…` while a reviewer runs, `asking` while the Land anyway / Stop request waits (needs you). The reviewer ("Land review · fix/cart-total → main") is a top-level session started by the lander. It sits in the same worktree group, and its row detail shows its notify level "Input only…", which reads like a state but isn't one. The request is docked as a card with the last findings: Land anyway ⌥1, Stop ⌥2. Don't confuse `review` with `review…`. Plain `review` (eye glyph) on a neighbouring row is a different state: the session changed files and waits for you to press Reviewed. It has nothing to do with the gate. The rounds of findings reach the lander as plain `[Clubmate]` prompts in its transcript.

**Graphs.** The sidebar has How graphs work, then Graphs (definitions: "PR walkthrough · built in", "Review gate · untrusted", "Review loop · 2 nodes") and Runs ("Nov 16 12:33 · 0/12 turns", with a warning or stopped glyph). The run page and the definition page (Run ⌘↩ with an Input field, Description, Definition, Turns, diagram) are separate pages. ⌘N asks an agent to write a new graph.

**Gates.** The sidebar has How gates work, then one row per gated repo ("shop · needs you"; otherwise "approved · 12m", "changes · 3h", "reviewing", "paused"). The detail has:
- a header with "active", the needs you badge, "last: changes 3 min ago · 1 reviewing", and Pause P, Edit E, Delete ⌘⌫;
- Repo, Guards, Kind, Rounds and Docs only;
- "Verdicts · last 7 days · approved 2 · changes 2";
- a table: the landings under review or waiting first, then the history (When, Verdict, Landing with its round and finding count, Open review / Open landing, Into);
- a findings card for the selected verdict.

New gate (⌘N) and Edit open a sheet over any section.

**Runs outlive their sessions.** Sessions are deleted after 30 days, or archived (`plugins/agents/retention.go:12`), while a run lives until it is deleted. Today the Graphs sidebar still lists such runs. After the merge the Agents tree needs a run row built from the run itself: the plugin sends it as a field and the clients only render it. The row shows even when the run has no sessions left.

**Things that open "the run in Graphs" today** and will need a new target:
- the bell's pinned run items and the run's macOS notification;
- Triggers' Open graph (G) on a graph edge;
- the Code chat panel's "Open in Graphs" for a graph step;
- the phone's run row, which opens its native run screen.

**Phone.**
- A run gets a row in the phone's Agents list only while it needs you ("Review gate · #4", a `kind: "run"` entry). Tapping it opens a native run screen with the node list and the step's approval card. Otherwise its node sessions are listed as ordinary sessions ("Review loop · Coder"); the phone doesn't group them by run (`SessionRows.swift`).
- The phone has no native Graphs or Gates screen. More lists every rail section without a native screen (`client/CompanionKit/Sources/CompanionKit/PhoneSections.swift:8`), so More → Graphs and More → Gates draw the same SDUI as the Mac (no diagram: "Drawn on the Mac").
- The phone draws no `sheet`, so New gate and Edit happen on the Mac.
- The lander's gate state reaches the phone but isn't drawn yet. The Land anyway / Stop card is drawn.
- Once the two rail items are gone, a phone that hasn't been updated loses those two More pages. Its native run screen keeps working through command aliases.

**Keys.**
- ⌘1…⌘9 switch to the Nth rail section in the user's order (`docs/shortcuts.md`). The default rail is Code, Terminals, Agents, Backlog, Processes, Cron, Triggers, Graphs (⌘8) and Gates (⌘9, only while the rail holds nine sections or fewer; a second editor pushes it past). After the merge the default rail has seven items.
- Inside one section these keys would collide:
  - **⌘N:** new session in Agents, New gate sheet in Gates, "ask an agent to write a graph" in Graphs.
  - **⌘⌫:** remove a run in Graphs, delete a gate in Gates.
  - **P / E:** pause or edit a gate.
  - **⌘.:** stop a run.
  - **⌥1–⌥3:** a graph step's approval, which means the same as a session's permission keys.
- The Graphs diagram has its own keys (j/k cards, Tab edges, h History, o full output, − + 0 zoom).

## 3. Questions for you

1. **Where Graphs and Gates live inside Agents.** Options include:
   - tabs or a segmented control at the top of the Agents sidebar (there is a `tabs` node);
   - groups in the one tree (a "Graphs" group, a "Gates" group);
   - filter chips next to All · Needs you · Open · Running;
   - something else.

   Width is the scarce resource: the sidebar is about 250 pt wide and the window can be 800 pt.
2. **Runs and definitions.** How do a graph run row and the graph definitions relate? Options: a definitions list beside runs in the tree, definitions only behind a "New run" action, or something else. What does a click or ↩ on a run row open? Is it the run page in the Agents main pane, and what happens to Open Coder, Stop run, the diagram and History there? Where do you start a run (Run with an Input)?
3. **Finished, stopped and emptied runs.** How does a run row look when the run has finished or stopped, when some of its sessions are closed, and when none of its sessions are left (deleted after 30 days or archived)? Where do old runs go: under Done or Closed with the sessions, into the Graphs part, or somewhere else? How do these two look side by side: a run that needs you itself (a step approval, a round cap), and a run whose node session waits on its own permission?
4. **Gates.** Where do the gate list, a gate's settings (Pause, Edit, Delete, New gate) and its verdict history live? Verdicts already show on the lander row. Should the history also be reachable from the lander or the reviewer? Where should reviewer sessions sit in the tree (today they are top-level rows beside their lander)? Should a reviewer's row show its round and verdict instead of the notify level?
5. **Keys.** What do ⌘1–⌘9 and the default rail look like with seven items? How do ⌘N, ⌘⌫, P, E and ⌘. resolve when Agents, Graphs and Gates share one section? Say which key does what where, keyboard first.
6. **Links.** Where do the bell's run items, the run notification, Triggers' Open graph (G) and the Code chat's "Open in Graphs" go now?
7. **Phone.** How does the new phone reach graph definitions, runs that don't need you, and gates, now that More won't list them? Should run rows show in the phone's Agents list at all times, like on the Mac? What should a phone that hasn't been updated show, if anything?

## 4. Constraints

- **SDUI only, existing nodes first.** Plugin views are SDUI trees built from the catalog: `header`, `tree`, `list`, `table`, `tabs`, `form`, `select`, `textField`, `toggle`, `button`, `card`, `badge`, `empty`, `graph`, `flow`, `lanes`, `runstrip`, `sheet`, `split`, `stack`, `scroll`, `surface`, `text`, `markdown`, `request.land`, and others. A new node or prop is a cost: name it when you need one.
- **The plugin sends fields; clients only render them.** What a run row or a gate means to the user (needs you, waiting, done, counts, grouping, where an old run goes) is decided once in the Go plugin and sent as a field, like `needsYou` and `gate` in `agents.sessions`. The Mac and the phone draw that field and never re-derive the rule. Propose the states and their labels, and the plugin will own them.
- **Mac and phone mean the same thing.** The phone draws the shared SDUI except where it has a native screen (Agents, the run screen).
- Keep the boards' design language (`Principles`, `Patterns`, `Components`; companion `C-*`) and the existing `Agents-*`, `Graphs-*`, `Triggers-640` and `Graphs-Gate-*` boards as the starting point. "Review gate" is also the name of a graph, so the UI says **land gate** wherever both can show.
- The Mac window is at least 800 pt wide. The 640 boards draw the narrow layout.
- Every key change is written in `docs/shortcuts.md`. A board key that clashes with it is a question for the user, not a rebind.
- Out of scope: Cron and Triggers stay where they are, and gate kinds other than Fable review.

## 5. What we'd like back

- **2–3 distinct directions** for questions 1–4. For each one, give:
  - the Agents section at **Mac 1280 dark** and **Mac 640 light**: the tree with a running run, a finished run and an emptied run, a gated landing, a run page and a gate page;
  - the **phone** screens: the Agents list, how you reach graphs and gates, a run screen and a gate screen.
- **Your recommendation**, plus the answers to 5–7 in the chosen direction.
- **A handoff list:** each change tagged (a) token or renderer, (b) new SDUI node or prop, (c) plugin content (words, grouping, order), or (d) new plugin logic or field.

Please make them as boards in this project's board format (one artboard per screen, next to the existing `Agents-*` and `Graphs-*` boards), so we can compare them with the screenshots above.
