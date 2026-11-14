---
name: nightly-tidy
description: Use when running the nightly tidy (the Tidy node of the Clubmate repo's `nightly` graph, which the "Nightly tidy" cron job runs) or when asked for a code health pass over Clubmate — performance, code quality, security audit, ARCHITECTURE.md conformance, docs freshness, CLAUDE.md completeness — fixing what is safe and reporting the rest.
---

# Nightly tidy

Nobody is watching. The user should wake to a few small squashed commits on `main`, one short report and backlog items, never a red `main` or a big diff. When in doubt, report instead of editing.

This is an AFK session: first call `agents_afk on=true` for yourself, so you and every child work under `[Clubmate AFK mode]`. Never ask the user and never wait for them (no AskUserQuestion, no elicitation). Decide yourself and ask the advisor (Fable) on every non-trivial call; what still needs the user after that — a product or design choice, their hardware, accounts or keys, a risky change — goes to the backlog tagged `needs-you`, and the run moves on.

`NOTES` is `clubmate-notes` next to the main checkout (`<main checkout>/../clubmate-notes`); use absolute paths, the `docs/superpowers` symlink breaks in worktrees.

## Scope of one run

Every night:
1. The commits since the last run (date of the newest `$NOTES/tidy/*.md`; none → 24 h), read through every lens below.
2. Whole repo: `go vet`, `staticcheck`, `govulncheck ./...`.
3. Benchmarks against the previous night.
4. Disk footprint against the previous night, and what one test run leaves behind (Leaks below).

Plus one deep dive over the whole repo, by weekday: Mon security, Tue performance, Wed ARCHITECTURE.md, Thu docs, Fri CLAUDE.md, skills and the MCP guide, Sat code quality, Sun none.

## Lenses

- **Performance**: hot paths (rpc wire, bus, SDUI renders, processes tick, agents render, transcript), allocations in loops, N+1 SQLite queries, unbounded goroutines or channels, polling where an event exists, work on the main thread in the Swift clients. Benchmarks: `go test -run '^$' -bench . -benchmem -count 6 ./internal/rpc ./internal/proc ./plugins/processes ./plugins/agents > $NOTES/tidy/bench/<date>.txt`, compared with the newest earlier file via `go run golang.org/x/perf/cmd/benchstat@latest old new`. A regression over 10 % with p < 0.05: find the commit, fix or file it.
- **Leaks** (disk and process): temp dirs and files nothing removes (`os.MkdirTemp`/`CreateTemp` without `RemoveAll` or `t.TempDir`, build outputs per test process, scratch dirs per run), state that only grows (`bus.db`, `plugins.db`, `plugin-data/agents/sessions`, logs, caches with no cap or prune), leftover processes and sockets after tests. Two checks with `.claude/skills/nightly-tidy/footprint.sh`:
  - Night over night: `footprint.sh snapshot > $NOTES/tidy/disk/<date>.txt`, then `footprint.sh compare <newest earlier file> <today>` lists every temp prefix, state dir or cache that grew over 1 GB or 200 entries, and a drop in free disk over 1 GB.
  - One test run: `footprint.sh snapshot tmp` before and after the step 4 test run, `compare before after 10240 20`; a prefix that grew is the tests' leak — `grep -rn '"<prefix>'` finds the creator.
  A leak in code is fixed like any finding (a test that the dir is gone or reused); growth with no code cause (user data, simulators, `.worktrees`) goes in the report with sizes. Never delete anything during the run; the report names what is safe to remove.
- **Code quality**: `go-style` skill, repo and user CLAUDE.md (no code comments, 4 import blocks, minimal non-redundant tests), dropped errors, dead code, duplicated helpers, functions or files grown too big, tests that re-prove what others prove.
- **Security**: socket and state file permissions, path traversal in file, docs and browser handlers and WKWebView scheme handlers, shell injection around `exec.Command`, secrets or tokens in logs, the `remote` plugin and push relay (auth, pairing, what is reachable over Tailscale), MCP tools missing their destructive flag, `govulncheck` hits. A fix lands only with a test that fails without it; anything exploitable is a high-priority backlog item and named first in the notification.
- **ARCHITECTURE.md**: every process, package and plugin it lists exists and does what it says; new ones are missing from it; the rules it states hold in code (core owns state, plugins speak JSON-RPC, UI is SDUI, clients render fields the plugin sends and never re-derive a rule).
- **Docs**: `docs/*.md` against code — commands, args, MCP tool names, Makefile targets, paths, `docs/shortcuts.md` against the real key bindings, `docs/agents.md` sections CLAUDE.md points to. Code is the truth unless the code is clearly the bug.
- **CLAUDE.md and skills**: rules that recent commits or `fix:` messages keep re-learning but no CLAUDE.md or skill says, stale rules, broken file references. Propose only: the exact text goes in the report, never edit CLAUDE.md.
- **MCP guide for agents**: what every agent gets from Clubmate's MCP server — `internal/mcp/clubmate.md` (the "Working in Clubmate" instructions) and the agent-visible commands' `description`s and arg descriptions in `plugins/*/plugin.json`. Every agent-visible command is reachable from the guide or a sibling tool's description when it belongs to a workflow (new plugins, new tools nobody mentions); descriptions match the behaviour, return shape, destructive flag and limits in code; no guide line names a tool, arg or behaviour that is gone; the docs in `docs/*.md` and the descriptions agree. Fixing a description or a guide line is a docs fix; a new rule in `clubmate.md` is a proposal in the report, like CLAUDE.md.

## Run

1. **Triage, read-only.** The main checkout holds other sessions' uncommitted work, so check `main` in a throwaway tree: `git worktree add --detach <scratchpad>/tidy HEAD`, run all checks, benchmarks and the footprint there (`mkdir -p $NOTES/tidy/bench $NOTES/tidy/disk` first), read the night's commits with `git log`/`git show`. Collect findings, drop duplicates of open backlog items (`backlog_list`).
2. **Fix in children.** You edit nothing in the main checkout. Start at most 4 children with `agents_new`, `branch: tidy/<YYYY-MM-DD>-<area>`, one area each. The prompt stands alone: the findings, the fix rules, the test commands, "AFK: never ask the user, a finding you can't fix without them goes back to me in your reply; run tests in the foreground, rebase on current main, stop at one conventional commit and reply with its sha".
3. **Fix rules** (put them in every child prompt):
   - Go code and docs only; docs include `internal/mcp/clubmate.md` and the `description` texts in `plugin.json` (not arg names, types or flags). Swift, views and plugin-rendered UI need `make app` and a look → backlog.
   - Mechanical, verifiable fixes. No protocol, wire, MCP schema or public API changes; no dependency bumps except a module `govulncheck` flags.
   - At most 10 files and 300 changed lines; over that, commit nothing and report.
   - Every behaviour fix comes with a test; docs fixes need none.
   - Never edit CLAUDE.md.
4. **Tests**: `env $(env | grep -o '^CLUBMATE_[A-Z_]*' | sed 's/^/-u /') go test $(go list ./... | grep -v -e /dist/ -e /.worktrees/)` and `cd third_party/jsonrpc2 && go test ./...`; plus `go vet` and `staticcheck` on the touched packages.
5. **Merge.** For each child: its branch tip equals the sha it reported; right before, in the main checkout, the index is empty (`git diff --cached --quiet`) and no uncommitted file is one the branch changes (other sessions' work in progress lives there; otherwise stop merging, leave the branches, say so in the report). Land it with the `git_land` tool (path the child's worktree, message `<type>(<scope>): …`), which squashes, commits and removes the child's worktree and branch; in a repo with a land gate it first waits for the gate's review (end your turn on `review pending`, Clubmate prompts you). After the last landing run the tests on `main`; red → `git revert --no-commit` the tidy commits of this run, land the revert with `git_land` mode index, and file why.
6. **File the rest**: `backlog_add` as `idea` with tag `tidy`, one item per finding with file:line and the suggested fix; add tag `needs-you` when the user has to decide or act (say what exactly in the description), `security` for security findings.
7. **Report** `$NOTES/tidy/<date>.md`: per lens — fixed (sha), filed (backlog id), CLAUDE.md proposals, benchmark deltas, footprint growth and free disk; at most a screen. Commit it in clubmate-notes. `docs_present` it, then `notify_post` one line: `Tidy: N fixed, M filed[, K need you][, S security]`.
8. **Hand over**: `changed` means at least one tidy commit of this run stays on `main`.
   - Started as a graph node (your brief starts `[Agent graph nightly, run …]`): report `changed` or `nothing` with the `triggers_emit` call your brief gives, and emit no `tidy.done`. `changed` makes the graph run `make test` in the main checkout, the Companion UI tests (`scripts/graph/companion-ui.sh`), `git push origin main` and `scripts/graph/ship-next.sh` (next patch version, foreground `ship.sh`); `nothing` ends the run.
   - Started any other way: `triggers_emit name=tidy.done payload={"changed":<bool>,"commits":[<shas>]}`; nothing listens to it any more, so a release then is manual (`nightly-release` skill).
9. **Clean up last** (an approval may stall here, the report is already out): remove the triage tree, each merged worktree and `git branch -D` its branch; leave a worktree with uncommitted changes or a running process.

## Cron job

Lives in the cron plugin, not in git; recreate it if it is gone:

```
cron_add name="Nightly tidy" command=graphs.run cron="30 3 * * *" owner=none missed=once
  args={"graph":"nightly"}
```

The repo's `nightly` graph (`.clubmate/graphs/nightly.json`, trusted with `graphs.trust`; [docs](../../../docs/agent-graphs.md#nightly)) starts this skill as its AFK Tidy node. Its steps run in the main checkout with no caller, so they need `Bash(make test)`, `Bash(scripts/graph/companion-ui.sh)`, `Bash(scripts/graph/mac-ui.sh)`, `Bash(git push origin main)` and `Bash(scripts/graph/ship-next.sh)` in the main checkout's `.claude/settings.local.json`; without them each step waits for an approval nobody gives. A core restart while Ship runs kills `ship.sh` (the step ends `error · interrupted`); a kill between the tag push and the publish leaves the tag, so check `gh release list` against `git tag` after one.

`missed: once` runs it after a night the Mac slept, possibly while the user works; step 5's checks are what keep that safe.
