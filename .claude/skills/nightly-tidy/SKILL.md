---
name: nightly-tidy
description: Use when running the nightly tidy (started by the "Nightly tidy" cron job) or when asked for a code health pass over Clubmate — performance, code quality, security audit, ARCHITECTURE.md conformance, docs freshness, CLAUDE.md completeness — fixing what is safe and reporting the rest.
---

# Nightly tidy

Nobody is watching. The user should wake to a few small squashed commits on `main`, one short report and backlog items, never a red `main` or a big diff. When in doubt, report instead of editing. Decide yourself, ask the advisor on non-trivial calls, never ask the user.

`NOTES` is `clubmate-notes` next to the main checkout (`<main checkout>/../clubmate-notes`); use absolute paths, the `docs/superpowers` symlink breaks in worktrees.

## Scope of one run

Every night:
1. The commits since the last run (date of the newest `$NOTES/tidy/*.md`; none → 24 h), read through every lens below.
2. Whole repo: `go vet`, `staticcheck`, `govulncheck ./...`.
3. Benchmarks against the previous night.

Plus one deep dive over the whole repo, by weekday: Mon security, Tue performance, Wed ARCHITECTURE.md, Thu docs, Fri CLAUDE.md and skills, Sat code quality, Sun none.

## Lenses

- **Performance**: hot paths (rpc wire, bus, SDUI renders, processes tick, agents render, transcript), allocations in loops, N+1 SQLite queries, unbounded goroutines or channels, polling where an event exists, work on the main thread in the Swift clients. Benchmarks: `go test -run '^$' -bench . -benchmem -count 6 ./internal/rpc ./internal/proc ./plugins/processes ./plugins/agents > $NOTES/tidy/bench/<date>.txt`, compared with the newest earlier file via `go run golang.org/x/perf/cmd/benchstat@latest old new`. A regression over 10 % with p < 0.05: find the commit, fix or file it.
- **Code quality**: `go-style` skill, repo and user CLAUDE.md (no code comments, 4 import blocks, minimal non-redundant tests), dropped errors, dead code, duplicated helpers, functions or files grown too big, tests that re-prove what others prove.
- **Security**: socket and state file permissions, path traversal in file, docs and browser handlers and WKWebView scheme handlers, shell injection around `exec.Command`, secrets or tokens in logs, the `remote` plugin and push relay (auth, pairing, what is reachable over Tailscale), MCP tools missing their destructive flag, `govulncheck` hits. A fix lands only with a test that fails without it; anything exploitable is a high-priority backlog item and named first in the notification.
- **ARCHITECTURE.md**: every process, package and plugin it lists exists and does what it says; new ones are missing from it; the rules it states hold in code (core owns state, plugins speak JSON-RPC, UI is SDUI, clients render fields the plugin sends and never re-derive a rule).
- **Docs**: `docs/*.md` against code — commands, args, MCP tool names, Makefile targets, paths, `docs/shortcuts.md` against the real key bindings, `docs/agents.md` sections CLAUDE.md points to. Code is the truth unless the code is clearly the bug.
- **CLAUDE.md and skills**: rules that recent commits or `fix:` messages keep re-learning but no CLAUDE.md or skill says, stale rules, broken file references. Propose only: the exact text goes in the report, never edit CLAUDE.md.

## Run

1. **Triage, read-only.** Run the checks yourself, collect findings, drop duplicates of open backlog items (`backlog_list`).
2. **Fix in children.** You edit nothing in the main checkout. Start at most 4 children with `agents_new`, `branch: tidy/<YYYY-MM-DD>-<area>`, one area each. The prompt stands alone: the findings, the fix rules, the test commands, "run tests in the foreground, rebase on current main, stop at one conventional commit and reply with its sha".
3. **Fix rules** (put them in every child prompt):
   - Go code and docs only. Swift, views and plugin-rendered UI need `make app` and a look → backlog.
   - Mechanical, verifiable fixes. No protocol, wire, MCP schema or public API changes; no dependency bumps except a module `govulncheck` flags.
   - At most 10 files and 300 changed lines; over that, commit nothing and report.
   - Every behaviour fix comes with a test; docs fixes need none.
   - Never edit CLAUDE.md.
4. **Tests**: `env $(env | grep -o '^CLUBMATE_[A-Z_]*' | sed 's/^/-u /') go test $(go list ./... | grep -v -e /dist/ -e /.worktrees/)` and `cd third_party/jsonrpc2 && go test ./...`; plus `go vet` and `staticcheck` on the touched packages.
5. **Merge.** For each child: its branch tip equals the sha it reported; right before, in the main checkout, the index is empty (`git diff --cached --quiet`) and no uncommitted file is one the branch changes (other sessions' work in progress lives there; otherwise stop merging, leave the branches, say so in the report). Squash and commit in one guarded command: `git merge --squash <branch> && git commit -m "<type>(<scope>): …"`. After the last squash run the tests on `main`; red → `git revert` the tidy commits of this run and file why. Then remove each merged worktree and `git branch -D` it; leave a worktree with uncommitted changes or a running process.
6. **File the rest**: `backlog_add` as `idea`, title starting `tidy:`, one item per finding with file:line and the suggested fix.
7. **Report** `$NOTES/tidy/<date>.md`: per lens — fixed (sha), filed (backlog id), CLAUDE.md proposals, benchmark deltas; at most a screen. Commit it in clubmate-notes. `docs_present` it, then `notify_post` one line: `Tidy: N fixed, M filed[, K security]`.

## Cron job

Lives in the cron plugin, not in git; recreate it if it is gone:

```
cron_add name="Nightly tidy" command=agents.new cron="30 3 * * *" owner=none missed=once
  args={"title":"Nightly tidy","notify":"needs-input","prompt":"Run the nightly tidy: invoke the nightly-tidy skill and follow it."}
```

`missed: once` runs it after a night the Mac slept, possibly while the user works; step 5's checks are what keep that safe.
