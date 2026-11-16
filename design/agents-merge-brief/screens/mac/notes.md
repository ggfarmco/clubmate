# Mac: Agents, Graphs, Gates and Triggers today

Captured 2026-11-16 from `b3417dd7` (Debug build, `make app`), one test instance on a throwaway repo `shop` (`../../capture/seed.sh`), `ui snapshot` at 2× scale. Nothing in the product changed.

## Setup

- `capture/seed.sh` builds the fake agent, makes the `shop` repo under `build/agents-merge-brief/shop` with a copy of `scripts/test-app.sh` (so the instance opens `shop`, not the clubmate repo) and `plugins/graphs/testdata/.clubmate/graphs/review-loop.json`, starts the instance and seeds it:
  - a land gate on `shop` (2 rounds), 2 cron jobs, 2 triggers;
  - sessions Bump (done), Migrate (error), Port cron (running), Rate-limit (waiting on a `go test` permission), from `capture/fakeagent-script.json` (the `design/check/mac` script plus fake land reviews that sleep while the verdict is posted);
  - four landers on branches, each with a commit, landed through raw `command.run` `git.land` with `caller` = the lander (`capture/rpc.py`): stock-expiry approved and landed; docs/folding approved with a deferred item and landed (it touches `docs/agents.md`, which docs-only refuses, so it was reviewed); cart-total `changes` twice, then the Land anyway / Stop request (needs you); inbox-groups under review. Verdicts are real `agents.land-verdict` calls with `caller` = the reviewer;
  - two Review loop runs: "add a CSV export of orders" (Coder and Reviewer done; stopped with `graphs.stop`) and "fix the flaky switcher test" (Coder waits on a permission, Reviewer not started).
- Light: the main build with `-NSRequiresAquaSystemAppearance YES`. Dark: by day the app follows the Mac's light appearance whatever the launch args, so the dark set comes from a throwaway copy of the same commit (`git archive`, under `build/agents-merge-brief/darksrc`) with a `ShotDark` launch-arg check in `GhosttyApp.systemIsDark`, started with `-ShotDark YES` on its own fresh seed. Same scene, so ids and times differ slightly between light and dark.
- The interface is Mate (light or dark with the Mac); no Ghostty theme was set.
- `*-640-*` were taken with `--size 640x800`, which the window clamps to its 800 pt minimum (1600×1600 px), like `design/check/mac`.

## Files

| File | Shows |
|---|---|
| `agents-lander-{1280,640}-{dark,light}` | Agents tree with both run rows (the waiting run expanded with Coder/Reviewer, the stopped run with its two done nodes), the four landers grouped by worktree with their reviewers; the cart-total lander selected: its two round-of-changes prompts and the docked Land anyway ⌥1 / Stop ⌥2 card |
| `agents-run-1280-{dark,light}` | the waiting run row clicked: it gets the cursor ring, the main pane keeps the last session (a run has no page in Agents; ↩ expands or collapses it, and this one stays open because a node needs you) |
| `graphs-run-{1280,640}-{dark,light}` | Graphs: How graphs work, the definitions group (PR walkthrough built in, Review gate untrusted, Review loop 2 nodes) and Runs (2); the waiting run's page: header, Open Coder ⌘↩, Stop run ⌘., Input/Started/Turns/Definition, the diagram, History |
| `graphs-definition-{1280,640}-{dark,light}` | the Review loop definition page: Run ⌘↩, Description, Input field, Definition, Turns, diagram |
| `gates-{1280,640}-{dark,light}` | Gates: the `shop` gate (needs you); detail with Pause P, Edit E, Delete ⌘⌫, Repo/Guards/Kind/Rounds/Docs only, the verdict table (reviewing, needs you, changes ×2, approved ×2) with round 1's changes selected and its findings card |
| `triggers-{1280,640}-{dark,light}` | Triggers for comparison: the graph edge "Review loop: Coder done → Reviewer" (from graph Review loop · 3), Run now ⌘R, Pause P, Open graph G |

Reading the screens: a reviewer row reads "Land revi… · Input only…": "Input only" is its notify level (`needs-input`), shown as the row detail, not a state. The lander rows' "review" is the plugin's `review` state (worktree differs from its first checkpoint), not the gate verdict; the gate shows as `review…` (under review) or `asking` (needs you).
