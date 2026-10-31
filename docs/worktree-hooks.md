# Worktree hooks

A repo can prepare every new worktree and clean up before one is removed. This applies to worktrees made by New Worktree, `git.worktree-new` and `agents.new` with a branch, and to removal through Remove Worktree or `git.worktree-remove`. Every file is read from the worktree itself, so a branch uses its own version.

| File | When |
|---|---|
| `.clubmate/worktree-include` | After `git worktree add`: copies the listed files into the new worktree |
| `.clubmate/setup.sh` | After the copy |
| `.clubmate/teardown.sh` | Before `git worktree remove`, after the worktree's Neovim and agent sessions stop |

## Include list

One path per line, relative to the repo root; empty lines and `#` comments are skipped. Each file is copied from the worktree the new one starts from (`from`, else the current worktree), e.g. `.env` that git does not track. A file already in the new worktree is kept. No globs and no folders; absolute paths, `..` and symlinks are refused and reported in the operation's notice, missing files are skipped. Copying needs no trust.

## Scripts

- Must be regular files (a symlinked script is skipped). Run as `/bin/bash -c <the approved content>` with the worktree root as the working folder, stdin closed and the environment of the Clubmate core without `CLUBMATE_*` variables.
- They share the operation's 10-minute limit; on timeout the script's whole process group is killed.
- `setup.sh` failing (non-zero exit or timeout) removes the new worktree with `git worktree remove --force`, keeps the branch, and fails the operation with the end of the script's output.
- `teardown.sh` failing is reported in the operation's notice; the worktree is removed anyway (unless git refuses, e.g. for untracked files the script left).

## Trust

A script runs only once you allow it. The first time a repo's script with given content is about to run, Clubmate shows the approval dialog (Run .clubmate/setup.sh, with the repo, worktree, the script's SHA-256, its size and its first 200 characters with `↵` for line breaks). Allowing it trusts that exact content in that repo; any change to the script asks again. Trust covers the script's own text only: files it calls (other scripts, package installs) are whatever the worktree's branch holds, so allow a script only in a repo whose branches you trust. Denying, no app to ask, or no answer within 60 s skips the script, reported in the operation's notice, and the worktree is still created or removed.
