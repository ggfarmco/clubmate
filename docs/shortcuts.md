# Keyboard shortcuts

The single list of every Clubmate shortcut. Any change that adds, removes or rebinds a key updates this file in the same commit.

## App and windows

| Keys | Action | Where defined |
|---|---|---|
| ⌘N | New (terminal or item in the current section; in Backlog it focuses the new task field) | `ClubmateApp.swift` |
| ⇧⌘N | New Home Terminal | `ClubmateApp.swift` |
| ⌘O | Open Folder… | `ClubmateApp.swift` |
| ⌘W | Close terminal session, or the window when no terminal is focused | `ClubmateApp.swift` (`CloseKeyOwner`) |
| ⇧⌘W | Close Session | `ClubmateApp.swift` |
| ⌘K | Command Palette | `ClubmateApp.swift` |
| ⌘B, ⌃⌘S | Toggle Sidebar; in Code, Toggle Chat: the agent chat panel on the right (per window) | `ClubmateApp.swift`, `WorkspaceLayout.swift` |
| ⌘= (⌘+), ⌘-, ⌘0 | Terminal font size up, down, reset | `ClubmateApp.swift`, `WorkspaceLayout.swift` |
| ⌘1…⌘9 | Switch to the Nth section in the rail, in your order (drag icons to reorder; right-click the rail to reset) | `ClubmateApp.swift` |
| ⌃⌘→, ⌃⌘← | Next, previous workspace | `ClubmateApp.swift` |
| ⌃⌘W | Workspace switcher grid | `ClubmateApp.swift` |
| ⇧⌘I | Notifications inbox (bell popover) | `ClubmateApp.swift` |

Inside terminals, Ghostty's own bindings apply, except ⌘W, which is unbound so the app gets it (`GhosttyApp.swift`).
⇧⌘I is an app menu key, so it shadows any ⇧⌘I mapping in terminals and nvim.

## Lists: palette, file picker, pickers, switcher

Pickers are the fuzzy lists for worktrees: the worktree switcher in the window title and Switch Worktree… in the palette. The file picker is the composer's attach list. Esc closes the title switcher too.

| Keys | Action |
|---|---|
| ↑ ↓ (arrows in the switcher grid) | Move the selection |
| ⌃J ⌃N / ⌃K ⌃P | Move the selection down / up (text fields) |
| h j k l | Move left / down / up / right (switcher grid) |
| ↩, ⌃L | Run, open, or choose |
| Esc | Close |

## Trees (sidebar focused)

| Keys | Action |
|---|---|
| j / ↓, k / ↑ | Move the cursor |
| l / → | Expand the item, or step into it |
| h / ← | Collapse the item, or go to its parent |
| ↩ | Select a leaf item, or expand / collapse an item with children |
| o | Same as ↩, but an item with children gets selected instead |
| gc | Focus the sidebar's text field, when it has one |
| Esc | Back to the main pane |
| ⌃L (sidebar on the left), ⌃H (sidebar on the right) | Back to the main pane |

`g` waits for the next key; a key that is not listed is swallowed. Defined in `ClubmateKit/TreeKeys.swift`.

## Agent composer (Agents section or Code chat panel, prompt focused)

| Keys | Action |
|---|---|
| ⌘↩ | Send the prompt; while a turn runs, add it to the running turn (agents that support steering) or queue it for the next turn |
| Esc | Stop the running turn |
| @ (at the start of a word) | Attach a file through the file picker |
| / (at the start of the prompt) | List the agent's commands, filtered by what follows |
| ↑ ↓, ↩ / ⇥ | Move in the command list, insert the command |

↩ alone inserts a newline. Defined in `ClubmateKit/Composer.swift` (`ComposerKey`).

## nvim (Clubmate distribution, leader `,`)

Defaults of the bundled nvim distribution (`nvim-distro/`). Keys in normal mode unless noted. The user layer (`~/.config/clubmate/lua/user/keymaps.lua`) can add or override any of them.

| Group | Keys | Action | Where defined |
|---|---|---|---|
| Find | ,ff / ,fg / ,fb / ,fh | Files, live grep, buffers, help tags (snacks picker) | `lua/clubmate/keymaps.lua` |
| Find | ,fr / ,fs / ,fi / ,fd | LSP references, document symbols, implementations, buffer diagnostics | `lua/clubmate/keymaps.lua` |
| Find | ,fy | Registers | `lua/clubmate/keymaps.lua` |
| Find | ,, | Resume the last picker | `lua/clubmate/keymaps.lua` |
| Code | gd / gD | Go to definition, declaration | `lua/clubmate/keymaps.lua` |
| Code | gr | Rename symbol | `lua/clubmate/keymaps.lua` |
| Code | gl | Show the diagnostic under the cursor | `lua/clubmate/keymaps.lua` |
| Code | [d / ]d | Previous, next diagnostic (opens its float) | `lua/clubmate/keymaps.lua` |
| Code | ,cp | Copy the file's relative path | `lua/clubmate/keymaps.lua` |
| Code | ,cb / ,cc | `go build ./...`, `go test -c ./...` | `lua/clubmate/keymaps.lua` |
| Code | ,ci | Toggle inlay hints | `lua/clubmate/keymaps.lua` |
| Code | ,gt | Jump between a Go file and its `_test.go` | `lua/clubmate/keymaps.lua` |
| Code | af / if, ac / ic (visual, operator) | Function, class text objects (outer / inner) | `lua/clubmate/plugins/lang.lua` |
| Code | ⇥ / ⇧⇥, ⌃N / ⌃P, ↩, ⌃E, ⌃Space (insert) | Completion: next / previous, show + next / previous, accept, cancel, show | `lua/clubmate/plugins/editor.lua` |
| Git | ]h / [h | Next, previous hunk | `lua/clubmate/plugins/git.lua` |
| Git | ,gb / ,gB | Blame line, full blame | `lua/clubmate/plugins/git.lua` |
| Git | ,gp / ,gr | Preview hunk, reset hunk | `lua/clubmate/plugins/git.lua` |
| Git | ,gs (normal, visual) | Stage hunk, stage the selected lines | `lua/clubmate/plugins/git.lua` |
| Git | ,lg / ,lf | LazyGit, LazyGit history of the file (needs `lazygit` on PATH) | `lua/clubmate/keymaps.lua` |
| Diff | ,dv | Diffview of the working tree | `lua/clubmate/plugins/git.lua` |
| Diff | ,dm / ,dM | Diff against the merge base with `main`, with a picked branch | `lua/clubmate/plugins/git.lua` |
| Diff | ,dh / ,dq | File history, close Diffview | `lua/clubmate/plugins/git.lua` |
| Diff | q / e (Diffview) | Close, edit the file; `s` stage, `r` mark reviewed in the file panel | `lua/clubmate/plugins/git.lua` |
| Review | ,rc (normal, visual) / ,rl | Add a review comment, list comments | diffview.nvim `nrev` |
| Review | ]a / [a | Next, previous review comment | diffview.nvim `nrev` |
| Review | ,rx / ,rd / ,re | Resolve or reopen, delete, export comments to the clipboard | diffview.nvim `nrev` |
| Debug | ,db / ,dB | Toggle breakpoint, conditional breakpoint | `lua/clubmate/plugins/debug.lua` |
| Debug | ,dc / ,do / ,di / ,dO | Continue, step over, into, out | `lua/clubmate/plugins/debug.lua` |
| Debug | ,dr / ,dl / ,dx | REPL, run last, terminate | `lua/clubmate/plugins/debug.lua` |
| Debug | ,dt / ,du | Debug the Go test under the cursor, toggle the DAP UI | `lua/clubmate/plugins/debug.lua` |
| Test | ,tr / ,tf / ,ta | Run the nearest test, the file's tests, all tests | `lua/clubmate/plugins/test.lua` |
| Test | ,td / ,tl / ,tL | Debug nearest, run last, debug last | `lua/clubmate/plugins/test.lua` |
| Test | ,ts / ,to / ,tp / ,tc | Summary, output, output panel, clear the panel | `lua/clubmate/plugins/test.lua` |
| Test | ,tw | Attach to the running test | `lua/clubmate/plugins/test.lua` |
| Test | [t / ]t | Previous, next failed test | `lua/clubmate/plugins/test.lua` |
| Spell | ,ss | Toggle spell checking | `lua/clubmate/keymaps.lua` |
| Spell | ,sn / ,sp | Next, previous misspelling | `lua/clubmate/keymaps.lua` |
| Spell | ,sa / ,sf | Add the word to the dictionary, fix it | `lua/clubmate/keymaps.lua` |
| Buffer | ,bd | Close the buffer | `lua/clubmate/keymaps.lua` |
| Buffer | ,ns | New scratch markdown buffer | `lua/clubmate/keymaps.lua` |
| Buffer | - / + | Toggle the file tree, the symbols tree (neo-tree, right side) | `lua/clubmate/plugins/editor.lua` |
| Buffer | Esc | Clear the search highlight | `lua/clubmate/keymaps.lua` |
| Buffer | ,q | Close the quickfix list | `lua/clubmate/keymaps.lua` |
| Buffer | v / s (quickfix) | Open the entry in a vertical, horizontal split | `lua/clubmate/autocmds.lua` |
| Folding | Space | Toggle the fold under the cursor | `lua/clubmate/keymaps.lua` |
| Folding | zV | Close all folds, or open all when none is open | `lua/clubmate/keymaps.lua` |
| Windows | ⌃H / ⌃J / ⌃K / ⌃L | Move to the left, lower, upper, right window | `lua/clubmate/keymaps.lua` |
| Windows | < / > (visual) | Indent and keep the selection | `lua/clubmate/keymaps.lua` |
| Windows | J / K (visual) | Move the selection down, up | `lua/clubmate/keymaps.lua` |

Paths are relative to `nvim-distro/`.

## Ergonomics notes

- ⌘W does two different things depending on focus: terminal or window.
- Toggle Sidebar has two bindings (⌘B and ⌃⌘S). Consider dropping one.
- ↩ and o in a tree differ only on items with children (↩ toggles, o selects). Consider merging them.
- nvim: `gr` (rename) waits for `timeoutlen` before firing, because nvim 0.12's built-in `grn`/`grr`/`gri`/`gra` start with it.
