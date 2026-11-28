---
name: nightly-release
description: Use when asked to cut a release by hand or from an agent session — pushes main, then a detached script waits for green CI, tags the next patch version and waits for the release to publish. The nightly release no longer uses this skill; it is the Push main and Ship steps of the Clubmate repo's `nightly` graph.
---

# Nightly release

The nightly release runs as steps of the Clubmate repo's `nightly` graph instead (`.clubmate/graphs/nightly.json`, [docs](../../../docs/agent-graphs.md#nightly)): after Tidy reports `changed`, `make test`, `scripts/graph/companion-ui.sh` (the Companion UI tests) and `scripts/graph/mac-ui.sh` (the Mac UI tests), each only after the one before it passed, and then `git push origin main` run in the main checkout, then `scripts/graph/ship-next.sh` picks the newest green commit and the version as in steps 1–2, refuses when a `v*` tag already points at it, and runs `ship.sh` in the foreground, so the step lasts until publish or failure. This skill is for releases cut by hand.

Nobody may be watching. First call `agents_afk on=true` for yourself. Never ask the user; what needs them goes to the backlog tagged `needs-you` and `release`, plus a `notify_post` of kind `stuck`.

Pushing a `v*` tag publishes at once (the `release` environment has no reviewers), so tag only through these steps.

1. In the main checkout: `git push origin main` (rejected, not a fast-forward → never force; file it and stop), `git fetch -q --tags origin`, then `SHA=$(scripts/release/green-sha.sh origin/main)`: the newest commit whose `ci` push run passed. Never release a commit CI hasn't passed; a red or running CI on newer commits doesn't hold the release, it ships from the last green one. If a `v*` tag already points at `SHA`, there is nothing new to release: say so in one line and stop. Run `scripts/release/flaky-backlog.sh $SHA $CLUBMATE_WORKSPACE_ID` to file that run's flaky tests.
2. Version: the newest tag (`git tag -l 'v*' --sort=-v:refname | head -1`) with the patch number plus one, written without the `v` (`0.0.30`, not `v0.0.30`): `ship.sh` adds it, so `v0.0.30` would tag `vv0.0.30`.
3. Ship detached, so it outlives this session:
   `python3 -c 'import os,sys; os.setsid(); os.execv("/bin/bash", ["bash"] + sys.argv[1:])' scripts/release/ship.sh <version> $SHA $CLUBMATE_WORKSPACE_ID >>~/Library/Logs/clubmate-ship.log 2>&1 </dev/null &`
   Once the ci run on `SHA` has started (a newer push to main replaces a waiting run, never a running one; a replaced run makes it ship the newest green commit instead and file that commit's flaky tests) it tags and pushes the tag, so the release builds, signs and notarizes while CI runs; `release.yml` publishes only after that ci run passed (`scripts/release/wait-ci.sh`). It waits for the release run and notifies `done` or `stuck`; when the release run fails before publishing, it deletes the tag.
4. Reply in one line: version, sha, log path. Don't install the update on this Mac (it restarts the core and kills running agent sessions); Sparkle offers it when the user is back.

A release whose CI still went red is spent: don't rerun its jobs or reship that commit under a new version. Find the failing test's race, fix it, and the next green commit ships.

## No trigger

The "Nightly release" trigger on `event.tidy.done` is gone: with the `nightly` graph it would release twice. Don't recreate it.
