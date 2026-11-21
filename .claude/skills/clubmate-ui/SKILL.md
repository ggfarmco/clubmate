---
name: clubmate-ui
description: Use when adding or changing any SwiftUI/AppKit view in client/Clubmate or client/Companion (iPhone, iPad), an SDUI node renderer, or the SDUI tree a plugin renders (plugins/*/view.go, pkg/sdui) — lists, popups, pickers, menus, control rows, labels, cards, detail panes, empty states, badges, colours, theme tokens, focus or keyboard handling, UI state — or when the user sends a screenshot of something in the app that looks broken or asks for a design or UX change.
---

# Building Clubmate UI

Every UI complaint so far was a screenshot of code that compiled fine: a list grew past the window, a row pushed the window wider, text ran under a header, state vanished on a section switch. Build each element from the recipe, then look at it.

## Design language: read it first

Clubmate has a design language: `references/design-language.md` (principles, colour roles, type and spacing, one spec per node type, patterns, the SDUI additions it needs). The boards it was distilled from are in `design/language/` (`png/` or `html/`); before you change a screen, open its mockup there (`Agents-1280`, `Agents-640`, `Code-*`, `Backlog-*`, `Cron-1280`, `Triggers-640`, `Graphs-*`, `Processes-*`) and build toward it; a feature decided since 16 Nov has its boards and `rules.md` in `design/features/<slug>/`. Design work with Claude Design follows `design/README.md` § Working with a designer (one canvas per feature, never the shared one). Where this file and the design language disagree, the design language wins.

The iPhone/iPad companion (`client/Companion`) follows the same principles with phone metrics, the Mate green accent and its own component specs: `references/companion.md` (boards in `design/companion/`). Look at a companion change in the simulator with fixtures (`design/companion-brief/README.md`), on a simulator of your own from `scripts/sim.sh up cm-<task>`; run `scripts/sim.sh down cm-<task>` when you are done.

Decisions that overrule the boards; keep them when a new export is synced: failed tool calls stay folded on the Mac and the phone (`docs/agents.md` § Transcript folding); the Closed group's last row reads "Show 25 more · N left" and shows the next 25, not "N more · search to find one" (`docs/sdui-protocol.md`); the worktree picker's "N more · type to find one" (`MR-PickerFolder-*`) is a "show N more" row that opens the section, like every overflow row (`references/design-language.md` § Patterns); the open items card shows No to all in its footer on the Mac and the phone, not in ••• (`docs/agents.md` § Open items); the Repos card opens by itself during a Land request on the Mac and the phone, though `MR-Merge-*` draws it folded (`docs/agents.md` § Transcript folding). The phone's question card shows the agent's whole message (paged: in Footnote, not cut to one line) and the composer steps aside while a card field has the keyboard (`references/companion.md` § Question). The phone's secondary ink is `captionInk`, not the boards' system secondary label (`references/companion.md` § Colour). On the phone a page header carries up to two actions (amends C2 "back · title · one ••• menu"), and the amber count in the session bar replaces C2's request banner (`references/companion.md` § Device UX language). Search in the Backlog and Agents sidebars is an always-visible live field, not the 🔍 in the sidebar header that `Backlog-1280` draws.

It is a target; the code is partway there. Scope each change:

| Change | Do |
|---|---|
| A plugin view or one screen | Apply the principles and patterns (detail view, empty states, truncation, names over ids, badges only for attention, one primary action, keys printed) with today's tokens. |
| A token or a node renderer (selection, surfaces, text levels, button look) | Its own change in `Theme.swift` and the renderer, for every view at once, with the formula from the token table. Never in one view only, never as a side effect of a feature. |
| A new SDUI prop or node | Only the ones the language names (`header`, `button` variant/disabled/shortcut/loading, field `error`, table column truncate/align/priority, `empty` icon/title/action/hint), each its own change with `pkg/sdui`, `docs/sdui-protocol.md`, the renderer and the gallery fixture. Anything else: build it from existing nodes, or ask. |
| A key the boards propose | `docs/shortcuts.md` is the source of truth; a board key that clashes with it is a question for the user, not a rebind. |

## Copy these, not the nearest neighbour

Neighbouring code may be the bug (the `/` command popup was an uncapped `VStack`).

| Element | Canonical code |
|---|---|
| Long list, popup, picker | list in `Windows/FuzzyPickerView.swift` |
| Row of controls | `controls` in `SDUI/ComposerView.swift` |
| Text field in an overlay | `PaletteTextField` |
| Key handling | `.keyMonitor(active:handle:)` |
| Card, button | `.agentCard()`, `CardButton`; SDUI buttons take `variant`/`shortcut` |
| Text field, select chrome | `.fieldChrome(focused:)` |
| Rarely changed options | one `Menu` with a short summary label (composer options) |

## Recipe per element

- **List or popup**: `LazyVStack` in `ScrollView`, `.frame(height: rowHeight * min(count, 8))`, `.id(item.id)` on rows, `scrollTo(id)` when the highlight moves, an empty-state text.
- **ScrollView inside a stack**: `.clipped()`.
- **Pane gutter**: an SDUI renderer never pads itself by `theme.gutter` on the outside. `SDUINodeView` insets every node by one gutter, except the types `SDUIGutter.pads` (`SDUI/SDUIView.swift`) lists: containers, nodes with edge-to-edge rows or rules (list, tree, table, header), and ones that lay out their own insets (empty, graph, composer, transcript cards, button). A new node type gets the gutter for free; one that must draw to the edge is added to that list, never undone with a negative padding. `.environment(\.sduiGutter, false)` drops it for a subtree that places its children itself (the header filter field).
- **Row of controls**: fits at 640pt. `ViewThatFits(in: .horizontal)` with one row, then a wrapped `FlowLayout`; container `.frame(minWidth: 0, maxWidth: .infinity)`; no fixed `minWidth` on fields.
- **Label**: `.lineLimit(1)`, `.truncationMode(.label)` (`.path` for a file path; both in `Theme/Theme.swift`), `.help(full)`; the identifying part gets `.layoutPriority(1)`.
- **Selected row**: `theme.selected` plus a 2pt `theme.accent` leading bar in the focused pane, `theme.selectedIdle` without a bar elsewhere (`ItemRow(active:)`); hover `theme.hover`; a keyboard cursor apart from the selection gets a `theme.focusRing` stroke.
- **Whether something shows** (the floating mic, a banner, a dock) is derived from navigation or model state, never from `onAppear`/`onDisappear` pairs or a slot views write into: on iOS 26/27 a pushed screen can get `onDisappear` right after `onAppear` and never appear again.
- **State that must survive** re-renders and section switches (drafts, expansion, chosen tab): a ClubmateKit store keyed by a stable node id, not `@State`. In `Equatable` views every render input is in `==`; themed parts live in a child view with its own `@Environment(\.theme)`.
- **Text body in an `NSAlert`**: an `NSTextView` accessory with `isSelectable = false`; a selectable one becomes first responder and AppKit scrolls it to the middle of the document on open.
- **Keyboard**: every action reachable by keyboard; focusable things are real `Button`s; key changes update `docs/shortcuts.md`.

## Theme tokens, never literals

`@Environment(\.theme)`, computed once per theme (`Theme.init` from `RGB`, contrast math in ClubmateKit `ThemeContrast`):
- colours: `background foreground accent`; `success warning danger` are already inks (≥ 4.5:1 on bg), `accentInk` for accent text and icons, `tone(_:)` returns inks; surfaces `surface1` (rail, sidebar, status bar) `surface2` (cards, composer, buttons) `surface3` (popovers, palette); text `text2 muted faint`; lines `border borderStrong`; states `hover pressed selected selectedIdle`, `focusRing` width; `toneBg(_:) toneBorder(_:)`; `onAccent` for text on an accent fill.
- fonts `ui uiSmall strong title heading count glyph` (they scale with the interface font size, `.system(size:)` does not).
- sizes `rowHeight rowHeight2 control header rail gutter indent`; `spacing.xs s m l xl xxl section overlay capsule chip badge field`; `radius.s m l xl`.

## Design defaults

Compact, Zed/Ghostty feel; width is the scarce resource. Collapsed by default, expand on click. Rarely changed settings behind a menu. Keyboard first, fuzzy search in every long list. The full rules are the six principles in `references/design-language.md`.

## Done means

1. `make app` prints `BUILD SUCCEEDED`. View code that was not compiled is not done.
2. Stress inputs: 80+ items, the longest realistic name or path, an empty list, interface font +4.
3. Look at it in an invisible test instance; it never takes focus or covers the user's windows, and other agents' instances don't interfere:
   - Start, snapshot, drive, keys, pointer, automated checks and tool cards: `references/visual-check.md`. `scripts/test-app.sh stop` afterwards.
   Never use `screencapture`.
4. Put the snapshot next to the screen's mockup and the node specs in `references/design-language.md`; the report names every place it still differs and why (e.g. "action keys wait for the `button` shortcut prop").
5. A phone UI bug fix lands with an XCUITest in `client/CompanionUITests` that fails before the fix (repeat the navigation that broke it, e.g. push the screen 3×).
6. If you could not look, write "visually unverified" in the report and say which of the above was skipped.

Past bug classes with their commits: `references/bug-classes.md`.
