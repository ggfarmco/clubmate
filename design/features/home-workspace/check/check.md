# Home workspace — UI check, 22 Nov 2026

Build: `feat/home-workspace` at 6f09b87ff plus this branch. Mac: test app (Mate dark, 1280×800; Home in the instance dir, so the path reads `/private/tmp/clubmate-test-hc/home`, not `~/.clubmate/home`). Phone: Companion fixtures on an iPhone 18 Pro (dark) and an iPad Pro 11" M5 (landscape, light). Shots are 50 % of the device pixels.

| Screen | Board | Shot | Result |
|---|---|---|---|
| Title bar with the house glyph | R1-Switcher, R1-Empty | `mac-titlebar.png` | matches |
| Switcher ⌃⌘W, Home open | R1-Switcher | `mac-switcher.png` | matches (Home first, house, × "Close from this window"); the order after Home: filed #1175 |
| Switcher "Open…" → picker | rules § User's answers | `mac-switcher-open-picker.png` | matches |
| Switcher after Esc | rules § User's answers | `mac-switcher-after-esc.png` | matches |
| File › Open Recent | R1-Recent | not shot | the harness can't draw an open menu; code checked (`ClubmateApp.swift`: Home + house + ⇧⌘H, divider, recents, Clear Menu); ⇧⌘H routes to "menu Home" |
| Welcome | R1-Recent | `mac-welcome.png` | matches ("open" on the Home row in accent, as on the recents) |
| ⌘K "Open Home ⇧⌘H" | R1-Rules | `mac-palette-open-home.png` | matches |
| ⇧⌘H | R1-Switcher | `mac-titlebar.png` | opens Home in the current window |
| ⇧⌘N | R1-Recent | `mac-new-home-terminal.png` | Home with a terminal in `$HOME` |
| Empty Home · Agents | R1-Empty | `mac-home-empty-agents.png` (before: `…-before.png`) | fixed: centred, `sparkles` icon |
| Empty Home · Backlog | R1-Empty | `mac-home-empty-backlog.png` | matches |
| Empty Home · Wiki | R1-Empty | `mac-home-empty-wiki.png` | matches |
| Project Agents empty | rules § User's answers | `mac-project-empty-agents.png` (before: `…-before.png`) | fixed: centred, `sparkles` icon |
| iPhone capsule menu | R1-Phone | `iphone-capsule-menu.png`, `iphone-capsule-home.png`, `iphone-capsule-menu-home-picked.png` | matches (Home first under Mac Studio, house, ✓ when picked, "● Home ⌄"; no Home row for Macs that send none) |
| iPad capsule menu | R1-Phone | `ipad-capsule-menu.png`, `ipad-capsule-home.png`, `ipad-capsule-menu-home-picked.png` | matches |
| iPhone empty Home · Agents, Backlog | R1-EmptyPhone | `iphone-home-empty-agents.png`, `iphone-home-empty-backlog.png` | filed #1174 (the phone's own words) |
| iPhone empty Home · Wiki | R1-EmptyPhone | not shot | the fixture serves one static wiki tree; in #1174 |
