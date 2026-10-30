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
| ⌘B, ⌃⌘S | Toggle Sidebar (no-op in Code, which has no sidebar) | `ClubmateApp.swift`, `WorkspaceWindow.swift` |
| ⌘= (⌘+), ⌘-, ⌘0 | Terminal font size up, down, reset | `ClubmateApp.swift`, `WorkspaceWindow.swift` |
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

## Agent composer (Agents section, prompt focused)

| Keys | Action |
|---|---|
| ⌘↩ | Send the prompt; while a turn runs, add it to the running turn (agents that support steering) or queue it for the next turn |
| Esc | Stop the running turn |
| @ (at the start of a word) | Attach a file through the file picker |
| / (at the start of the prompt) | List the agent's commands, filtered by what follows |
| ↑ ↓, ↩ / ⇥ | Move in the command list, insert the command |

↩ alone inserts a newline. Defined in `ClubmateKit/Composer.swift` (`ComposerKey`).

## Ergonomics notes

- ⌘W does two different things depending on focus: terminal or window.
- Toggle Sidebar has two bindings (⌘B and ⌃⌘S). Consider dropping one.
- ↩ and o in a tree differ only on items with children (↩ toggles, o selects). Consider merging them.
