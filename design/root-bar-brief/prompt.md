# Clubmate: native root bar items on the Companion

Canvas: https://claude.ai/artifact/XTurAnVyW6mnRwfLujcUkX ("Clubmate · Root bar"). Page "Today" has real simulator screenshots (iPhone 18 Pro Agents + Backlog, iPad 11" Agents, dark and light) and two reference boards from the Device UX canvas. Put round 1 on page `r1`, at most 6 boards.

## 1. What exists

Every tab root (Agents, Backlog, Wiki, More) has this top bar:

- **Workspace capsule** top left: "● shop ⌄". A stock `Menu` with `.buttonStyle(.glass)` and a custom label: a 9 pt dot in the Mac's colour (hollow while the Mac is offline), the workspace name, a caption-ink chevron. The menu lists Macs with their state, their workspaces with "n waiting", Mac Details, Pair a Mac.
- **Glass group** top right: **inbox tray** (`tray` glyph, 9 pt amber dot top right while anything waits or an Inbox row is unread; pushes the Inbox page), **mic** (`mic`; tap starts listening and tap again sends; press and hold talks and release sends; filled accent circle while listening), **compose** (plain native bar button, section's create action).
- On iPad the same items sit over the list column; the sidebar toggle is beside them.

## 2. The problem

Capsule, inbox and mic are custom SwiftUI views hosted in the bar. Measured on the simulator, together they cost ~43 ms of main thread per push and ~48–58 ms per pop. One hosted item costs ~24 ms even in a bare SwiftUI app; a plain native `Button`/`Menu` item with a `Label` or `Image` costs ~0. During a slow back swipe from a session the root items show only when the swipe ends, with a glass fade (blurred and empty mid-swipe); users see it as a hitch. The Claude iOS app is the reference for a smooth back swipe: its bar items move with the bar.

Target: root bar cost per pop ≤ ~10 ms. So all items must be native: `ToolbarItem` / `ToolbarItemGroup` with `Button` or `Menu` and a `Label`/`Image` (SF Symbol, system tint), system Liquid Glass grouping, system badge (`.badge(n)` shows a count; a dot only by the system's rules). No custom overlay, no custom-coloured shape in the label, no custom gesture.

## 3. Questions

1. **Workspace capsule.** What does a native `Menu` label look like? Options we see: text only ("shop ⌄" with the system chevron indicator), a tinted SF Symbol (e.g. `circle.fill` in the Mac's colour, if the system keeps the tint in the bar) plus the name, the name as the inline title with a title menu (`toolbarTitleMenu`). Where does "Mac offline" show if the hollow dot goes?
2. **Inbox mark.** The amber dot is not native. Options: system badge with the waiting count, a symbol swap (`tray` → `tray.full` / `tray.and.arrow.down.fill`), a system tint of the glyph (amber), or no mark (the Agents tab and "Needs you" chip already show waiting). Which one, and does the count match "Needs you"?
3. **Mic.** A native bar button gets tap only: tap to start, tap to send. Press and hold is lost in the bar. Options: (a) the bar mic becomes tap-to-toggle and press-and-hold stays only on the composer mic inside a session; (b) the mic leaves the root bar (e.g. into the tab bar as a search-style accessory, or a bottom accessory) and stays a custom view where hosting is cheap; (c) keep it custom and accept its cost. Rule on it, with the listening state (today: filled accent circle).
4. **Order and grouping** of inbox · mic · compose in the glass group, iPhone and iPad.

## 4. Constraints

- Keep the design language (`C-*` boards, Mate green accent, amber = needs you, caption ink for secondary). Colour roles, not hex values.
- What the user sees as "waiting" comes from the plugin as a field (`needsYou`); clients render it, never re-derive it.
- iPhone and iPad behave the same.
- Touch targets ≥ 44 pt.

## 5. Deliverables

- One board per question 1–3 (or one combined), with 2 directions where it is open, iPhone dark + light, plus iPad where it differs.
- A rulings board: what may change, what must stay, a short handoff list (SwiftUI API per item).
