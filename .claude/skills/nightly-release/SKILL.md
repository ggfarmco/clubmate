---
name: nightly-release
description: Use when running the nightly release (started by the "Nightly release" trigger after the nightly tidy merged something into main) or when asked to cut a release unattended — pushes main, then a detached script waits for green CI, tags the next patch version and waits for the release to publish.
---

# Nightly release

Nobody is watching. First call `agents_afk on=true` for yourself. Never ask the user; what needs them goes to the backlog tagged `needs-you` and `release`, plus a `notify_post` of kind `stuck`.

Pushing a `v*` tag publishes at once (the `release` environment has no reviewers), so tag only through these steps.

1. In the main checkout: `git fetch -q --tags origin`; `SHA=$(git rev-parse main)`. If a `v*` tag already points at `SHA`, there is nothing to release: say so in one line and stop.
2. Version: the newest tag (`git tag -l 'v*' --sort=-v:refname | head -1`) with the patch number plus one, written without the `v` (`0.0.30`, not `v0.0.30`): `ship.sh` adds it, so `v0.0.30` would tag `vv0.0.30`.
3. `git push origin main`. Rejected (not a fast-forward) → never force; file it and stop.
4. Ship detached, so it outlives this session:
   `python3 -c 'import os,sys; os.setsid(); os.execv("/bin/bash", ["bash"] + sys.argv[1:])' scripts/release/ship.sh <version> $SHA $CLUBMATE_WORKSPACE_ID >>~/Library/Logs/clubmate-ship.log 2>&1 </dev/null &`
   Once the ci run on `SHA` exists it tags and pushes the tag, so the release builds, signs and notarizes while CI runs; `release.yml` publishes only after that ci run passed (`scripts/release/wait-ci.sh`). It waits for the release run and notifies `done` or `stuck`; when the release run fails before publishing, it deletes the tag.
5. Reply in one line: version, sha, log path. Don't install the update on this Mac (it restarts the core and kills running agent sessions); Sparkle offers it when the user is back.

## Trigger

Lives in the triggers plugin, not in git; recreate it if it is gone:

```
triggers_add name="Nightly release" on=event.tidy.done match={"changed":true} command=agents.new owner=none
  args={"title":"Nightly release","notify":"needs-input","prompt":"Run the nightly release: invoke the nightly-release skill and follow it. You are AFK: turn AFK mode on for yourself first and never ask the user."}
```

The nightly tidy emits `event.tidy.done` with `changed: true` when at least one of its commits stays on `main`.
