# Model of the Clubmate core

A model of the Go core (`cmd/clubmate-core`, `internal/`) in the roots language: what it owns, each request
with who may ask and every refusal, what happens by itself, and notes where the model stops. It follows the code.

## Check and read

```
ROOTS=../roots/target/release/roots-next      # the roots repository, beside this one
$ROOTS check model/core                       # errors and warnings; no error is expected
$ROOTS say model/core --out build/roots-model/say   # sheets in plain words, one per module
```

In Neovim the language server is `roots-next lsp`; `model/roots.project` marks the root of `use` paths.

## Files

| File | Holds |
|---|---|
| `foundation/` | the media Go, JSON and SQLite, their crossings, and Text, Flag, Time, Document |
| `architecture/core_host.roots2` | the words of the model: record, method, policy, call, table, memory, file, log, hub |
| `core/system.roots2` | the caller, the actors, the core process |
| `core/types.roots2` | shared ids and values |
| `core/plugins.roots2` | plugin processes: start, crash, backoff, giving up, ping, stuck calls, restart, on and off |
| `core/commands.roots2` | `command.run`: who may run a command, the agent rules, approval |
| `core/clients.roots2` | connections, `client.register`, who is asked to approve, `core.shutdown` |
| `core/bus.roots2` | the event log, `bus.publish`, cursors, subscriptions |
| `core/storage.roots2` | each plugin's keys in `plugins.db` |
| `core/workspaces.roots2` | workspaces, folders, worktrees and their events |
| `core/sessions.roots2` | terminal sessions and their host processes: create, kill, send, read, remove, and how an end is announced |
| `core/ui.roots2` | slots and their trees, subscriptions and presence, `ui.focus`, `ui.event`, `ui.approve`, the window state clients keep |
| `core/notify.roots2` | `notify.post`: what is checked, the two budgets, `notify.posted` |
| `core/left_out.roots2` | what exists and is not modelled: process trees, and the method families that were not read |

## Warnings that stay

`check` prints four "cannot be checked" warnings. Each is a call into a request that holds an `open:` note:
`Clients.show` (twice: the count of clients told), `Clients.approve` (one approval at a time, a caller that
leaves) and `Plugins.call` (per-command timeouts, a plugin that is still starting). `--release` fails on them
and on every `open:`, as it should until those are decided or the language can say them.
A callee is warned about once, at its first call: `Ui.approve` and `Ui.event` reach the same two and add no line.
A `// @ path:line` at the end of a line names the code that line restates.
