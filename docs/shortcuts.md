# Keyboard shortcuts

The single list of every Clubmate shortcut. Any change that adds, removes or rebinds a key updates this file in the same commit.

## App and windows

| Keys | Action | Where defined |
|---|---|---|
| ⌘N | New (terminal or item in the current section; in Backlog it focuses the new task field) | `ClubmateApp.swift` |
| ⇧⌘N | New Home Terminal | `ClubmateApp.swift` |
| ⌘O | Open Folder… | `ClubmateApp.swift` |
| ⌘W | Close terminal session, or the window when no terminal is focused (the popup when it has focus, see below) | `ClubmateApp.swift` (`CloseKeyOwner`) |
| ⇧⌘W | Close Session | `ClubmateApp.swift` |
| ⌘K | Command Palette | `ClubmateApp.swift` |
| ⌘B, ⌃⌘S | Toggle Sidebar; in Code, Toggle Chat: the agent chat panel on the right (per window) | `ClubmateApp.swift`, `WorkspaceLayout.swift` |
| ⌘= (⌘+), ⌘-, ⌘0 | Terminal font size up, down, reset | `ClubmateApp.swift`, `WorkspaceLayout.swift` |
| ⌘1…⌘9 | Switch to the Nth section in the rail, in your order (drag icons to reorder; right-click the rail to reset) | `ClubmateApp.swift` |
| ⌃⌘→, ⌃⌘← | Next, previous workspace | `ClubmateApp.swift` |
| ⌃⌘W | Workspace switcher grid | `ClubmateApp.swift` |
| ⇧⌘I | Notifications inbox (bell popover) | `ClubmateApp.swift` |
| Esc | Close the popup a plugin shows over the workspace (e.g. a doc), in the panel or its popped-out window; focus goes back to where it was | `PopupPanel.swift`, `PopupWindow.swift` |
| ⌘W | Close the popup over the workspace while it has focus, instead of the terminal or window | `PopupPanel.swift` |
| ⇧⌘O | Pop the popup out into its own window; later popups of that workspace open there until it is closed | `PopupPanel.swift` |

Inside terminals, Ghostty's own bindings apply, except ⌘W, which is unbound so the app gets it (`GhosttyApp.swift`).
⇧⌘I is an app menu key, so it shadows any ⇧⌘I mapping in terminals and nvim.

## Plugin buttons

A plugin button with a `shortcut` prints it (`⌘R`) and presses on it while the button is on screen; a combo without ⌘, ⌃ or ⌥ does nothing while a text field has focus, app menu keys always win, and Esc, ↩ and ⇥ without ⌘, ⌃ or ⌥ cannot be plugin shortcuts. Defined in `SDUI/InputViews.swift` (`ButtonNodeView`), combos parsed by `ClubmateKit/KeyCombo.swift`. Plugin shortcuts in use:

| Keys | Action | Plugin |
|---|---|---|
| ⌘R | Run the cron job or trigger now | cron, triggers |
| P | Pause or resume the job or trigger | cron, triggers |
| ⌘⌫ | Delete the job, trigger or task, or remove the graph run (confirmed) | cron, triggers, backlog, graphs |
| ⌘↩ | Start an agent on the task; open the session of the graph node that needs you (else the running or start node) | backlog, graphs |
| ⌘. | Stop the graph run (confirmed) | graphs |
| G | Open the graph run that created the trigger | triggers |
| D | Mark the task done | backlog |
| ⌘S | Save the task now (the form also autosaves) | backlog |
| ⌘I | Show the session's context, or back to the transcript | agents |
| ⇧K | Kill the selected process and its children (confirmed) | processes |
| ⇧⌘R | Reveal the selected process's executable in Finder | processes |

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
| < / > | Previous / next chip of the sidebar's filter row, when it has one (Agents: All · Needs you · Running) |
| Esc | Back to the main pane |
| ⌃L (sidebar on the left), ⌃H (sidebar on the right) | Back to the main pane |

`g` waits for the next key; a key that is not listed is swallowed. Defined in `ClubmateKit/TreeKeys.swift`.

## Tables (plugin table focused)

A click on a row, or focus moving to the main pane, focuses the table; the selected row then shows the accent bar. Group headings are skipped.

| Keys | Action |
|---|---|
| j / ↓, k / ↑ | Move the selection |
| l / → | Expand the row, or step to its first child |
| h / ← | Collapse the row, or go to its parent row |

Keys with ⌘, ⌃, ⌥ or ⇧ pass through (⇧K stays the plugin's Kill). Defined in `SDUI/TableView.swift`, rows in `ClubmateKit/TableKeys.swift`.

## Agent composer (Agents section or Code chat panel, prompt focused)

| Keys | Action |
|---|---|
| ⌘↩ | Send the prompt; while a turn runs, add it to the running turn (agents that support steering) or queue it for the next turn |
| Esc | Stop the running turn |
| @ (at the start of a word) | Attach a file through the file picker |
| / (at the start of the prompt) | List the agent's commands, filtered by what follows |
| ↑ ↓, ↩ / ⇥ | Move in the command list, insert the command |

↩ alone inserts a newline. Defined in `ClubmateKit/Composer.swift` (`ComposerKey`).

## Agent transcript (Agents section or Code chat panel)

| Keys | Action | Where defined |
|---|---|---|
| ⌘D | Full diff: open the turn of the newest edit card in the transcript in nvim's diffview (printed on that card's Full diff) | `SDUI/Agents/TranscriptView.swift` |

Open in nvim on an edit card has no key: the board's ⌘O is the app's Open Folder….

## Agent Browser window (during a handoff)

| Keys | Action | Where defined |
|---|---|---|
| ↩ | Done: hand the page back to the agent (only when the page does not have focus; closing the window does the same) | `BrowserWindow.swift` |

## Permission requests (Agents section or Code chat panel)

A pending request is docked above the composer.

| Keys | Action |
|---|---|
| ⌥1 ⌥2 ⌥3 | Allow once · Always allow… · Reject: the key each option carries (`shortcut`, printed on its button); works while the prompt has a draft and leaves it untouched. ⌥2 (or a click) opens the Always allow confirm instead of answering. Always reject sits in the card's ••• menu without a key. A request whose options carry no keys keeps ⌥N in button order, "always" options without a key |
| ⌥2 again / ⎋ or ↩ | In the Always allow confirm: store the rule and allow / Cancel (the default); nothing is sent before ⌥2 |
| 1 2 3 | The same without ⌥ (bare 2 opens and confirms Always allow too), only while the session's prompt is focused and empty or the request card has focus (click it); typed into a prompt with a draft, the digit goes into the draft |
| y / n | Allow once / Reject once, under the same condition as bare digits |

Outside the Always allow confirm, Esc still stops the turn and does not answer the request. Defined in `ClubmateKit/PermissionKeys.swift` and `SDUI/Agents/PermissionCardView.swift`.

## Agent questions (Agents section or Code chat panel)

A pending question (AskUserQuestion or an MCP form) is docked above the composer like a permission request.

| Keys | Action |
|---|---|
| ⌥1 … ⌥9 | Answer with that option (printed on each option button) when the question has a single list of options; the answer is sent at once |
| ↩ | In a text field of the question (e.g. "Other"): send the form |

Defined by the `button` `shortcut` props in `plugins/agents/elicit_view.go`.

## Companion on iPad / Fold (hardware keyboard, a session open)

| Keys | Action |
|---|---|
| ⌥1 ⌥2 ⌥3 | The pending permission card's keys from the agents plugin (`shortcut` in its `card`): ⌥1 Allow once, ⌥2 opens the Always allow confirm sheet (nothing is stored until its Always allow button), ⌥3 Reject; Always reject in ••• has no key |
| ⌘↩ | Send the composer's message (Session menu) |

Listed in the iPad menu bar under Session. Defined in `client/Companion/Plugins/Agents/SessionKeys.swift`, key mapping in CompanionKit `PermissionRequest.keyed`.

## Code chat panel starters (session with no turns yet)

Starters have no keys: ⌥1–⌥3 answer permission requests only. A click sends the starter prompt; "Explain" and "Write a test" attach the file open in Neovim. Defined by the agents plugin (`plugins/agents/view.go`).

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
| Docs | ,mp (or `:ClubmateDocOpen`) | Open the current Markdown/mermaid/HTML file in the docs viewer | `lua/clubmate/keymaps.lua` |
| Folding | Space | Toggle the fold under the cursor | `lua/clubmate/keymaps.lua` |
| Folding | zV | Close all folds, or open all when none is open | `lua/clubmate/keymaps.lua` |
| Windows | ⌃H / ⌃J / ⌃K / ⌃L | Move to the left, lower, upper, right window | `lua/clubmate/keymaps.lua` |
| Windows | < / > (visual) | Indent and keep the selection | `lua/clubmate/keymaps.lua` |
| Windows | J / K (visual) | Move the selection down, up | `lua/clubmate/keymaps.lua` |

Paths are relative to `nvim-distro/`.

## Ergonomics notes

- ⌘W does three different things depending on focus: popup, terminal or window.
- Toggle Sidebar has two bindings (⌘B and ⌃⌘S). Consider dropping one.
- ↩ and o in a tree differ only on items with children (↩ toggles, o selects). Consider merging them.
- nvim: `gr` (rename) waits for `timeoutlen` before firing, because nvim 0.12's built-in `grn`/`grr`/`gri`/`gra` start with it.
