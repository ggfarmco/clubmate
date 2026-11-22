# Home workspace brief

Input for a Claude Design round on Home, the personal workspace (22 Nov 2026). Spec: `clubmate-notes/superpowers/specs/2026-11-22-home-workspace-design.md` § Clients.

- `prompt.md`: the brief sent to the Design chat (sent as one paragraph).
- `screens/`: today's screens. Mac from the test app (Mate dark), iPhone 18 Pro and iPad Pro 11" from the simulator with fixtures (light).

## What the designer decides

1. The look of Home on the Mac (workspace switcher, Open Recent, Welcome) and on the iPhone and iPad (workspace capsule menu): row or tile, icon, how "always there, first, cannot be removed" shows. Name "Home", not "~".
2. One behaviour choice from the user: (a) Home always first in Open Recent and Welcome, outside the 10-item cap, Clear Recents keeps it, window model unchanged (S); or (b) Home always open in every Mac window as its first tile, cannot be closed (M/L).
3. The empty Home: Agents, Backlog and Wiki on first open. Today's "Agents aren't available in the home workspace" (`plugins/agents/spawn.go` `errHome`) and "Open a project workspace to see its wiki." (`plugins/wiki/view.go` `homeText`) go away.

## Today in code

- Mac switcher: `client/Clubmate/Windows/SwitcherGrid.swift` (tiles for the workspaces open in this window, plus "Open folder…").
- Welcome: `client/Clubmate/Windows/WelcomeView.swift`. Open Recent: `client/Clubmate/App/ClubmateApp.swift:145-175` (10 items, Clear Menu). Both use `Workspace.recents` (`client/ClubmateKit/Sources/ClubmateKit/SDUI.swift:42`), which hides Home.
- ⇧⌘N New Home Terminal: `client/Clubmate/App/WorkspaceActions.swift` `newHomeTerminal`.
- Phone and iPad: `client/Companion/App/WorkspaceCapsule.swift` (system `Menu`, a section per Mac). Rules: `design/features/device-ux/rules.md`.

## Round 1

Claude Design, 22 Nov 2026. Canvas "Clubmate · Home workspace": https://claude.ai/artifact/KgyEvkpPMwwVdiwymsy2W5, chat https://claude.ai/chat/23e4d8d6-ff9b-4357-9bb9-58223b53b0ce. Pages: Today (`Main`: every screenshot named), r1 (6 boards), Language (`Principles`, `Tokens`, `Components`, `Patterns`, `C-Language`, `C-Components`, `C-Workspace`). Canvas copies of cm.css / c.css: `/_blob/8eab21fc…` / `/_blob/5c8858c0…`.

- `round-1.json`: the source of the round page, `design/features/home-workspace/boards.html`. Boards, PNGs and `rules.md` are in `design/features/home-workspace/`.
- Ruling: (a). Home is first in Open Recent, on Welcome and under each Mac on the phone; a window can close it; window model unchanged. The `house` glyph, muted, is the only mark; new key ⇧⌘H Open Home.
- Empty Home: same empty nodes as a project, Home's words (see `rules.md` § Empty Home).
