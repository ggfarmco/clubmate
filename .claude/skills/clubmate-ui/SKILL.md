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
- **Label**: `.lineLimit(1)`, `.truncationMode(.middle)` (`.head` for a directory), `.help(full)`; the identifying part gets `.layoutPriority(1)`.
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
3. Look at it in the real window narrowed to ~640pt and at ~900pt, or in the gallery (`open -n "build/DerivedData/Build/Products/Debug/Clubmate Dev.app" --args --sdui-gallery`, nodes from `SDUI/GalleryFixture.json`). Tell the user before launching: the test app takes keyboard focus. Capture only its window, never the full screen (the user's own sessions are on it): `screencapture -x -o -l "$(swift .claude/skills/clubmate-ui/scripts/window-id.swift 'Clubmate Dev')" /tmp/ui.png`, then Read the png. Quit the test app afterwards.
4. If you could not look, write "visually unverified" in the report and say which of the above was skipped.

Past bug classes with their commits: `references/bug-classes.md`.
