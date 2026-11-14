# Gates UI: brief

Backlog #567 · 14 Nov 2026 · engine #536 (`feat/land-review-gate`) · spec `clubmate-notes/superpowers/specs/2026-11-13-land-review-gate.md`

**Goal.** You set up land gates yourself and name the repo explicitly. When a gate exists, a landing into that repo's default branch, or a child's merge into its parent's branch, waits for a Fable verdict. A repo with no gate lands without a review.

**Your words:** "some UI like the one we built for triggers, so it can be configured and the repo can be named explicitly there".

## Pick a direction

| | A · own section | B · inside Triggers | C · Settings, per repo |
|---|---|---|---|
| Mockup | [gates-a-section.html](gates-a-section.html) | [gates-b-triggers.html](gates-b-triggers.html) | [gates-c-settings.html](gates-c-settings.html) |
| Where | new rail item "Gates" next to Triggers | "Land gates" group at the top of the Triggers tree | Settings → Agents → Land gates |
| Repo | picker over the workspace's folders in a sheet | menu of folders in the detail pane | every folder is a row; a switch turns its gate on |
| Add | ⌘N → sheet | ⌘N → New trigger / New gate | none: the first switch-on adds it |
| Pause / delete | P · ⌘⌫ in the detail | P · ⌘⌫ in the detail | the switch pauses; right-click → Delete gate… |
| History | table + findings in the detail | run strip + table, like a trigger's runs | right-click → History… sheet |
| Plus | most visible; room to grow (command or graph gates later) | no new section; sits with the other automation | no form at all; can't name the wrong repo |
| Minus | 9th rail item: past ⌘8, and with VS Code it gets no ⌘ key | Triggers draws gates owned by the agents plugin (a cross-plugin view); ⌘N in Triggers turns into a choice (a shortcuts question) | hidden in Settings; verdicts only show in Agents; one switch per repo can't grow to several gates per repo (command, graph, check) |
| Built from | `Tree`, `Header`, `Table`, `Sheet` + `Form`, `Select` | `Tree` group, `Header`, `Select`, `Runstrip`, `Table` | the existing `agents.settings` slot: a `SettingRow` with `Toggle` and `Select` per repo, `Sheet` + `Table` |
| New node? | none, but a new section (agents plugin or its own) | none | none (the board draws a table; built as rows) |

## The same in every direction

- **List row:** repo name (the identifying part), then the last verdict as toned detail text: approved, changes, needs you, or paused.
- **Form fields:** Repo, Kind (Fable review only for now; command, graph and check come later), Rounds (default 2). Docs-only is fixed: a change only in `.md` files lands without a review.
- **Edit:** only Rounds and Paused can change. Repo and Kind are fixed after Add (that's the API).
- **Only you** can add, change or delete a gate. Agents can read gates. This is why the form is the main path; Triggers has no add form, so ⌘N follows Cron.
- **History row:** when, verdict (approved / changes / reviewing / docs only / you: land), landing session, target branch, Open review. A selected row shows its findings as `file:line` plus deferred items → backlog.
- **Open review** opens the reviewer session. After a land it sits in Closed.

## In Agents (all directions): [gates-agents.html](gates-agents.html)

- **Tree:** the landing session row shows the gate state as its toned detail: `review…`, `changes` or `approved`. Only `needs you` is a badge. The agents plugin sends the state as a field; the Mac and the phone only render it.
- **Reviewer:** drawn as a row under its lander ("Review · Fable", round, age).
- **Transcript:** one card per round (verdict, findings, Open review). The round in progress is a strip above the composer.
- **Needs you after 2 rounds:** the existing `request.land` node as it renders today: title, repo name, detail (branch, the open finding), rule ("By its gate…"), hint, and the options "Land anyway ⌥1" and "Stop… ⌥2" (`reject_once`, at the far end, asks `confirm` first). It is built like `MR-Merge-640`, and the round cap follows `Graphs-Cap-640`. No new node, so the phone shows it with no client change. The findings stay in the round cards above it. Neither button is primary: landing over open findings is the risky choice.
- **New in Agents:** the round card and the "reviewing" strip have no node yet. Either `git_land`'s tool card shows the verdict (no new node), or a gate-round transcript item and a dock strip get added. To be decided at build time.

## Naming

"Gate" already names the graph step in `Graphs-Gate-*` (Review gate). The UI says **land gate** wherever both could show up (the Triggers group, Settings); the section title in A stays "Gates".

## Phone

No setup on the phone. The phone shows the needs-you card (`request.land`) and the chip once #536 sends it.

## Needs from #536 (the engine, being built now)

1. **Per-gate history:** verdict files are keyed `<old>-<tree>`, with no gate id, repo or lander. The list needs `gate`, `repo`, `lander` (session + title), `into`, `round` and `at` on each verdict, plus a query such as `agents.gate-verdicts {id, limit}`.
2. **The verdict outcomes to show:** `by: reviewer | docs | user` (the user's "land anyway") and `pending` (a reviewer is running).
3. **Last verdict per gate** in `agents.gates`, for the list row.
4. **The chip field** on `agents.sessions` rows (the gate state of the landing in progress), sent by the plugin per the needs-you rule.
5. **Reviewer's place in the tree:** the plan says both "a child of the lander" and "top-level, startedBy the lander". The mockups draw it under the lander. Please confirm `startedBy` is enough to nest it there.
6. **The docs-only skip** is automatic (`.md` paths) and not a gate option. If you want it switchable, it needs a field on the gate.
7. **One gate per (repo, kind)?** The picker marks a folder that "has a Fable gate". Is (repo, kind) unique, or can a repo have two Fable gates?
8. **Which gates a workspace shows:** gates are stored per repo, not per workspace. A and B list the gates for this workspace's folders; C lists every folder of every workspace. Say which one `agents.gates` filters by.
9. **Repo choices:** a picker needs the workspace's folders, each with its canonical primary checkout and default branch, and whether it already has a gate.

## Files

- `brief.md`: this file.
- `gates-a-section.html`, `gates-b-triggers.html`, `gates-c-settings.html`: one direction each, two boards per file.
- `gates-agents.html`: the Agents boards shared by all three.
- `src/*.html` + `build.py`: the sources. `python3 build.py` inlines `design/language/html/assets` (cm.css, c.css, fonts) into the self-contained pages.
