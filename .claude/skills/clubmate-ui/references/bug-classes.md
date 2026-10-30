# UI bug classes fixed in Clubmate

| Symptom | Root cause | Fix | Commit |
|---|---|---|---|
| `/` command popup covers the window and slides under the title bar | ~80 rows in a plain `VStack`, no height cap | Capped `ScrollView` + `LazyVStack`, `scrollTo` on highlight | composer popup fix |
| Transcript draws under the session header | Scroll content bleeds outside its frame | `.clipped()` on the scroll container | c674d67 |
| Composer controls widen the window, rail disappears after restart | Rigid `HStack` of pickers with a large ideal width | `ViewThatFits` → `FlowLayout`, `minWidth: 0`; later options behind one menu | 5a1f5c3, 7a5cb01 |
| Review header doesn't fit a narrow sidebar | Fields and buttons side by side, chips in a horizontal scroll | Full-width fields, buttons on their own row, chips in `FlowLayout`, middle truncation | 4723261 |
| Attachment labels ignore the font size setting | `Equatable` parent compares only `node`, never re-renders on theme change | Themed part in a child view with its own `@Environment(\.theme)` | 4f8d4d4 |
| Composer draft lost on section switch | `@State` model dies with the view | `ComposerDrafts` store keyed by node id | 4cdef7b |
| Text typed during a send is wiped | Draft cleared without checking what was sent | Clear only if text still equals the sent text | 2596ff1 |
| Tool card collapses while streaming | Expansion keyed by a group id that changes | Key by stable card ids, prune on transcript change | 33b88bf |
| Expanded last item stays off-screen | No scroll after expanding | `scrollTo(edge: .bottom)` when the last item opens | b8865b0 |
| ⌘P list jumps, wrong row scrolled | `ForEach`/`scrollTo` keyed by index | Stable `id`, `scrollTo(item.id)` with bounds check | e0dc3f9 |
| Tree cursor hides rows under sticky headers | `scrollTo(id)` ignores pinned headers | `TreeCursor.scrollTarget(..., limit:)` | 237cff4 |
| Overlay shortcut closes the overlay instead of focusing it | Toggle ignores focus | Focus the field if the overlay is open | e0dc3f9 |
| Rail not reachable by keyboard | `onTapGesture` | Real `Button` with `.buttonStyle(.plain)` | 857a76c |
| Key monitor eats terminal keys or leaks | No first-responder check, no teardown | Shared `.keyMonitor(active:handle:)` | 1627793, 48176d7 |
| Leader key stays armed after blur | Focus loss not handled | Reset on `onChange(of: focused)` | 6601c9d |
| Enter approves a permission prompt | "Allow" is the default first responder | `refusesFirstResponder`, `initialFirstResponder = dismiss` | 77ddf8b |
| Placeholder overlaps typed text in a popover | NSTextField placeholder | Nil placeholder while text is non-empty | 4723261 |
| Selected palette row unreadable | `theme.selection` background | fg 0.06 background + 2pt accent bar | b6a2cf8 |
| Settings window wrong size | `.fixedSize` per tab | Fixed frame | 857a76c |
| Light/dark switch needs a relaunch | Appearance read once at init | Observe `effectiveAppearance` | 54d4840 |
| Sparkline fixed size | `.frame(width: 60, height: 16)` | Height from server, `maxWidth: .infinity` | 715a3e5 |
