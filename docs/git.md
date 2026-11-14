# Git

The git plugin owns worktrees, branches and landings for the workspace's git folders. Worktree hooks (`.clubmate/setup.sh`, `teardown.sh`) are in [worktree hooks](worktree-hooks.md).

## git.land

`git.land {path, message, into?, mode?}` lands a change as one commit. It is the landing path agents are told to use (finish texts in [agents](agents.md)); it is an MCP tool (`git_land`).

- `mode: branch` (default) squashes the branch checked out at `path` into `into` (default the repo's default branch), in the worktree that has `into` checked out: `git merge --squash` and `git commit -m <message>`, so the repo's own commit hooks run. Before that it computes the result with `git merge-tree --write-tree` and fails on a conflict, naming the files; it fails when the target worktree has staged changes or the target branch moved meanwhile, and puts the index back (`git reset --merge`) when the squash or the commit fails. Then it removes the worktree at `path` (never the main checkout, never one on the default branch, and not while a terminal runs in it) with plain `git worktree remove`, no removal handlers, so the session that landed stays open, and force-deletes the branch; a removal git refuses (uncommitted files) leaves a `note`. A branch that adds nothing (already landed) lands nothing and is cleaned up the same way.
- `mode: index` commits what is staged in `path`, which must have `into` checked out: for sessions working on the default branch itself (cron jobs, nightly runs, graph steps).
- In a repo with a [land gate](agents.md#land-review-gate) a call from an agent needs an approved verdict for exactly this landing (the target's tip and the resulting tree): with none it starts the review and fails with `review pending: <session>`, with `changes` it fails with the findings, and past the gate's rounds it waits for the user. A call from the user (the app, `cm` without `--agent`) is never gated.

Returns `{commit, into, source, removed, note}`.

## The agent git hook

Agent sessions in a gated repo run git with an injected `core.hooksPath` that proxies every hook to the repo's own and refuses moving the protected branches with plain git; sessions in other repos get nothing. See [agents § Land review gate](agents.md#land-review-gate).
