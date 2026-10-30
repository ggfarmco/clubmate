# Keyboard shortcuts

The single list of every Clubmate shortcut. Any change that adds, removes or rebinds a key updates this file in the same commit.

## App and windows

| Keys | Action | Where defined |
|---|---|---|
| ⌘N | New (terminal or item in the current section) | `ClubmateApp.swift` |
| ⇧⌘N | New Home Terminal | `ClubmateApp.swift` |
| ⌘O | Open Folder… | `ClubmateApp.swift` |
| ⌘W | Close terminal session, or the window when no terminal is focused | `ClubmateApp.swift` (`CloseKeyOwner`) |
| ⇧⌘W | Close Session | `ClubmateApp.swift` |
| ⌘K | Command Palette | `ClubmateApp.swift` |
| ⌘P | Go to File… | `ClubmateApp.swift` |
| ⇧⌘F | Find in Files… | `ClubmateApp.swift` |
| ⌘B, ⌃⌘S | Toggle Sidebar | `ClubmateApp.swift`, `WorkspaceWindow.swift` |
| ⌘= (⌘+), ⌘-, ⌘0 | Terminal font size up, down, reset | `ClubmateApp.swift`, `WorkspaceWindow.swift` |
| ⌘1…⌘9 | Switch to the Nth section in the rail, in your order (drag icons to reorder; right-click the rail to reset). A section hosted as a sidebar tab (Git, in the Code sidebar) has no rail icon or number: reach it with its tab or "Go to Git" in ⌘K | `ClubmateApp.swift` |
| ⌃⌘→, ⌃⌘← | Next, previous workspace | `ClubmateApp.swift` |
| ⌃⌘W | Workspace switcher grid | `ClubmateApp.swift` |
| ⇧⌘I | Notifications inbox (bell popover) | `ClubmateApp.swift` |

Inside terminals, Ghostty's own bindings apply, except ⌘W, ⌘P and ⇧⌘F, which are unbound so the app gets them (`GhosttyApp.swift`).
⇧⌘I is an app menu key, so it shadows any ⇧⌘I mapping in terminals and nvim.

## Lists: palette, Go to File, Find in Files, pickers, switcher

Pickers are the fuzzy lists for worktrees and review refs: the worktree switcher in the window title (its list, and its Review…), Switch Worktree… and Review… in the palette, and the review header's base and head fields. Esc closes the title switcher too.

| Keys | Action |
|---|---|
| ↑ ↓ (arrows in the switcher grid) | Move the selection |
| ⌃J ⌃N / ⌃K ⌃P | Move the selection down / up (text fields) |
| h j k l | Move left / down / up / right (switcher grid) |
| ↩, ⌃L | Run, open, or choose |
| ⌥↩ | Find in Files: send all results to nvim's quickfix list |
| Esc | Close |

## Files tree (sidebar focused)

| Keys | Action | Notes |
|---|---|---|
| j / ↓, k / ↑ | Move the cursor | |
| l / → | Expand the folder, or step into it | |
| h / ← | Collapse the folder, or go to its parent | |
| ↩ | Open the file, or expand / collapse the folder | |
| o | Same as ↩, but a folder also gets the selection highlight | |
| - | Collapse all other folders | |
| Esc | Back to the editor | |
| ⌃L (sidebar on the left), ⌃H (sidebar on the right) | Back to the editor | |
| . | Focus the tree on the folder under the cursor (on a file: its folder) | neo-tree `set_root` |
| ⌫ | Move the focus up one level (from the top level: show the whole workspace) | neo-tree `navigate_up` |
| < / > | Previous / next sidebar tab (Code: Files, Git, Review), wrapping | neo-tree `prev_source` / `next_source` |

Defined in `ClubmateKit/TreeKeys.swift`.

## Git list (Code sidebar, Git tab focused)

The files tree keys above apply. In addition:

| Keys | Action | Notes |
|---|---|---|
| ↩, o | Open the file's diff in nvim; focus stays in the list | |
| ga | Stage the file or untracked folder under the cursor | neo-tree git_status `ga` |
| gu | Unstage the staged file under the cursor | neo-tree git_status `gu` |
| gr | Discard the unstaged changes of the file under the cursor (asks first) | neo-tree git_status `gr` |
| gc | Focus the commit message field | neo-tree git_status `gc` |

`g` waits for the next key; a key that is not listed is swallowed. A chord only acts when the file's context menu offers it. Defined in `ClubmateKit/TreeKeys.swift` (`TreeChord`).

## Review list (Code sidebar, Review tab focused)

The files tree keys above apply; `-` collapses every folder except the ancestors of the file under the cursor (else of the open file). In addition:

| Keys | Action |
|---|---|
| ↩ | Open the file's diff |
| r | Toggle reviewed on the file under the cursor |
| n / N | Open the next / previous unreviewed file (any file without the fork module) |

Defined in `ClubmateKit/Review.swift` (`ReviewKeys`).

## Agent composer (Agents section, prompt focused)

| Keys | Action |
|---|---|
| ⌘↩ | Send the prompt; while a turn runs, add it to the running turn (agents that support steering) or queue it for the next turn |
| Esc | Stop the running turn |
| @ (at the start of a word) | Attach a file through the Go to File picker |
| / (at the start of the prompt) | List the agent's commands, filtered by what follows |
| ↑ ↓, ↩ / ⇥ | Move in the command list, insert the command |

↩ alone inserts a newline. Defined in `ClubmateKit/Composer.swift` (`ComposerKey`).

## nvim navigation overlay (opt-in in Settings)

| Keys | Action |
|---|---|
| ⌃H ⌃J ⌃K ⌃L | Move between nvim windows; at the edge, jump to the sidebar |
| ⌃Р ⌃О ⌃Л ⌃Д | The same keys on the Russian layout |

Installed by editor-nvim (`navLua` in `plugins/editor-nvim/nvim.go`). Your own mappings on these keys are kept unless they are plain window moves, and smart-splits is wrapped instead of overridden.

## Ergonomics notes

- ⌘W does two different things depending on focus: terminal or window.
- Toggle Sidebar has two bindings (⌘B and ⌃⌘S). Consider dropping one.
- ⌃H/⌃L mean "back to the editor" in the tree but "move window / to the sidebar" in nvim. The pair is symmetric on purpose.
- ↩ and o in the tree differ only in whether a folder gets the selection highlight (↩ matches neo-tree). Consider merging them.
