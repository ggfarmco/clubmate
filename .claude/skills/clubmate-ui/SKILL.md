---
name: clubmate-ui
description: Use when adding or changing any SwiftUI/AppKit view in client/Clubmate or an SDUI node renderer — lists, popups, pickers, menus, control rows, labels, cards, focus or keyboard handling, UI state — or when the user sends a screenshot of something in the app that looks broken.
---

# Building Clubmate UI

Every UI complaint so far was a screenshot of code that compiled fine: a list grew past the window, a row pushed the window wider, text ran under a header, state vanished on a section switch. Build each element from the recipe, then look at it.

## Copy these, not the nearest neighbour

Neighbouring code may be the bug (the `/` command popup was an uncapped `VStack`).

| Element | Canonical code |
|---|---|
| Long list, popup, picker | list in `Windows/FuzzyPickerView.swift` |
| Row of controls | `controls` in `SDUI/ComposerView.swift` |
| Text field in an overlay | `PaletteTextField` |
| Key handling | `.keyMonitor(active:handle:)` |
| Card, button | `.agentCard()`, `CardButton` |
| Rarely changed options | one `Menu` with a short summary label (composer options) |

## Recipe per element

- **List or popup**: `LazyVStack` in `ScrollView`, `.frame(height: rowHeight * min(count, 8))`, `.id(item.id)` on rows, `scrollTo(id)` when the highlight moves, an empty-state text.
- **ScrollView inside a stack**: `.clipped()`.
- **Row of controls**: fits at 640pt. `ViewThatFits(in: .horizontal)` with one row, then a wrapped `FlowLayout`; container `.frame(minWidth: 0, maxWidth: .infinity)`; no fixed `minWidth` on fields.
- **Label**: `.lineLimit(1)`, `.truncationMode(.label)` (`.path` for a file path; both in `Theme/Theme.swift`), `.help(full)`; the identifying part gets `.layoutPriority(1)`.
- **Selected row**: `theme.foreground.opacity(0.06)` background plus a 2pt `theme.accent` leading bar.
- **State that must survive** re-renders and section switches (drafts, expansion, chosen tab): a ClubmateKit store keyed by a stable node id, not `@State`. In `Equatable` views every render input is in `==`; themed parts live in a child view with its own `@Environment(\.theme)`.
- **Keyboard**: every action reachable by keyboard; focusable things are real `Button`s; key changes update `docs/shortcuts.md`.

## Theme tokens, never literals

`@Environment(\.theme)`: colours `foreground muted border accent success warning danger`; fonts `ui uiSmall title glyph` (they scale with the interface font size, `.system(size:)` does not); sizes `rowHeight gutter indent`; `spacing.xs s m l capsule chip field`.

## Design defaults

Compact, Zed/Ghostty feel; width is the scarce resource. Collapsed by default, expand on click. Rarely changed settings behind a menu. Keyboard first, fuzzy search in every long list.

## Done means

1. `make app` prints `BUILD SUCCEEDED`. View code that was not compiled is not done.
2. Stress inputs: 80+ items, the longest realistic name or path, an empty list, interface font +4.
3. Look at it in an invisible test instance; it never takes focus or covers the user's windows, and other agents' instances don't interfere:
   - `scripts/test-app.sh start` (or `start <name> -- --sdui-gallery` for the gallery, nodes from `SDUI/GalleryFixture.json`); it opens this worktree as a workspace.
   - `scripts/test-app.sh cm -- ui snapshot --out /tmp/clubmate-test-<name>/snapshots --size 640x800`, then again with `--size 900x800`. Read the `summary` first (`0 problems` or flagged SDUI nodes: `outside-window`, `wider-than-parent`, `overlaps-sibling`, `truncated`), then the png. Popovers and settings: `cm -- ui present inbox|worktree|switcher|settings` before the snapshot; menus are listed as text in the `w<id>.json`.
   - Drive it with `cm -- ui event`, `cm -- command run`, or over MCP with `testapp_call`.
   - `scripts/test-app.sh stop` afterwards.
   Never use `screencapture`.
4. If you could not look, write "visually unverified" in the report and say which of the above was skipped.

Past bug classes with their commits: `references/bug-classes.md`.
