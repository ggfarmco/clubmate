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
| ⌘1…⌘9 | Switch section (Code, Git, Terminals, Processes, …) | `ClubmateApp.swift` |
| ⌃⌘→, ⌃⌘← | Next, previous workspace | `ClubmateApp.swift` |
| ⌃⌘W | Workspace switcher grid | `ClubmateApp.swift` |

Inside terminals, Ghostty's own bindings apply, except ⌘W, ⌘P and ⇧⌘F, which are unbound so the app gets them (`GhosttyApp.swift`).

## Lists: palette, Go to File, Find in Files, switcher

| Keys | Action |
|---|---|
| ↑ ↓ (arrows in the switcher grid) | Move the selection |
| ↩ | Run, open, or choose |
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

Defined in `ClubmateKit/TreeKeys.swift`.

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
