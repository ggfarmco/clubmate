# Phone: graph runs, Graphs and Gates today

Companion Debug build of `b3417dd7` (`make companion`) on an own iPhone 18 Pro simulator `cm-agents-merge` (iOS 27.0), status bar at 09:41, appearance via `simctl ui`. `../../capture/phone.sh` (`GATE=<gt_…> RUN=<gr_…>` from the Mac scene) takes every screen dark and light.

This Mac has no Tailscale, so the phone can't pair with the test core. Two sources instead:
- **Native screens** from the repo's fixtures (`client/CompanionKit/Tests/CompanionKitTests/Fixtures`, `-runs YES`): two runs that need you in Mac Studio's `clubmate` workspace.
- **SDUI screens** (More → Graphs, More → Gates) from a copy of the fixtures (`build/agents-merge-brief/phone-fixtures`) whose `sdui.json` got the `graphs` and `agents.gates` sections and the Mac scene's real `graphs.*` and `agents.gates.*` trees (`cm ui dump`). The phone draws them with its own SDUI renderer, as it would from a real Mac; the trees are static, so a tap changes nothing.

| File | Shows |
|---|---|
| `phone-agents-runs-{dark,light}` | Agents list: the two runs under Needs you ("Review gate · #1" past a round cap, "Review gate · #4" a step asking to run a command). A run has a row here only while it needs you (`kind: "run"` entries in `agents.sessions`); otherwise its node sessions are ordinary rows, not grouped by run (`SessionRows.swift`). The fixture has no node sessions, so that part isn't in the shot |
| `phone-run-detail-{dark,light}` | the native run screen (`RunDetailView`): node list with states and the step's approval card |
| `phone-more-{dark,light}` | More: Processes, Cron, Triggers, Graphs, Gates (every rail section the phone has no native screen for, `PhoneSections.more`) |
| `phone-graphs-list-{dark,light}` | More → Graphs: How graphs work, Graphs (definitions), Runs |
| `phone-graphs-run-{dark,light}` | More → Graphs → the waiting run: header, Open Coder, Stop run, props, "Drawn on the Mac" (no diagram on the phone) and the node rows |
| `phone-graphs-definition-{dark,light}` | More → Graphs → Review loop definition: Run, Description, Input, nodes |
| `phone-gates-list-{dark,light}` | More → Gates: the `shop` gate, needs you |
| `phone-gates-detail-{dark,light}` | More → Gates → `shop`: header, Pause, Edit (opens on the Mac: the phone draws no `sheet`), props, the verdict table as cards |

Not captured: the land gate's Land anyway / Stop card on the phone (no fixture has a lander with a request; the phone draws the same `request.land` node as the Mac), and the lander's gate state (`gate` in `agents.sessions` reaches the phone but isn't drawn yet).
