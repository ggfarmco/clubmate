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

A script runs only once you allow it. The first time a repo's script is about to run with given contents of `.clubmate/`, Clubmate shows the approval dialog "Run .clubmate/setup.sh?" with the repo and worktree (a path over 200 characters is cut to its last 199 after a leading `…`), the script's SHA-256 and size, and a scrollable box with every entry of `.clubmate/`: the script first, then each other file's content (a symlink as its target), each under a `──── .clubmate/<name>` line. Long lines wrap, so text after a run of spaces can't hide off to the side, and control and invisible formatting characters (bidi overrides, zero-width characters, line and paragraph separators, `\r`) are shown as `\u{…}`, so the shown text can't hide or reorder what runs. `─` is escaped the same way in contents and names (and line breaks, tabs and `→` in names), so a file can't fake a `────` line. If `.clubmate/` holds more than 64 KiB (file contents plus names), or the shown text would pass 65536 characters, the script is skipped without asking. Allowing it trusts that script with that exact `.clubmate/` folder in that repo: any change anywhere in `.clubmate/` (another script, the include list, a file `worktree-include` copied there, something `setup.sh` wrote there before teardown) asks again, so keep generated and untracked files out of `.clubmate/`. A `.clubmate` that is not a real folder (e.g. a symlink) skips the scripts. Files outside `.clubmate/` that the script calls (other scripts in the repo, package installs) are not covered: they are whatever the worktree's branch holds, so allow a script only in a repo whose branches you trust. Denying, no app to ask, or no answer within 60 s skips the script, reported in the operation's notice, and the worktree is still created or removed.
