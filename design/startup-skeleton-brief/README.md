# Startup skeleton brief

Input for a Claude Design round on what a Mac window shows while the app starts, before the Go core is up (22 Nov 2026). The user chose the direction: a skeleton of the real window, no splash, text only for a slow start and a failure.

- `prompt.md`: the brief sent to the Design chat (sent as one paragraph).
- `screens/`: today's screens from the test app (Mate dark, 1280×800, one 640×800). The start-up state itself is drawn on the canvas's `Today` board from the code: the test app's snapshot goes through the core, so it cannot capture the window before the core is up.

## Today in code

- Welcome window: `client/Clubmate/Windows/WelcomeView.swift:14-30` (`app.phase`: `.connecting` → `EmptyNodeView(text: "Starting core…")`, `.failed` → message + Retry).
- Workspace window: `client/Clubmate/Windows/Workspace/WorkspaceView.swift:95-112` (`content`, `placeholder`: "Starting core…", "Connecting…").
- Phases: `ConnectionSupervisor.Phase` (`client/ClubmateKit/Sources/ClubmateKit/ConnectionSupervisor.swift`): `connecting`, `ready`, `reconnecting(attempt:)`, `failed(String)`. Status bar text: `client/Clubmate/Windows/StatusBar.swift`.
- Known before the core: `WindowLayout` (windows and their workspaces, UserDefaults), `AppSettings.sectionOrder` (`client/ClubmateKit/Sources/ClubmateKit/SectionOrder.swift`), sidebar side, font sizes. From the core only: `WorkspaceUIStore.sections` (`client/ClubmateKit/Sources/ClubmateKit/WorkspaceUIStore.swift:99`), the selected section, sidebar hidden, chat column shown and width, workspace names.

## Round 1

Claude Design, 22 Nov 2026. Canvas "Clubmate · Startup skeleton": https://claude.ai/artifact/PqiJvgogi3py4ZHcwDAGaB, chat https://claude.ai/chat/30857035-230c-4019-84bf-7a453b61766d. Pages: Today (`Main`), r1, Language (`Principles`, `Tokens`, `Components`, `Patterns`, `Agents-Mate-1280`, `Code-1280-Mate`, `Backlog-1280-Mate`). Canvas copy of cm.css: `/_blob/e40cfba5…`.

- `round-1.json`: the source of `round-1.html` (all three directions). The boards are in `design/features/startup-skeleton/` (B and C in `rounds/`). `R1-Timeline-A` imports `Agents-Mate-1280`, which the boards-page shim does not resolve: run `python3 -I design/startup-skeleton-brief/inline-imports.py design/features/startup-skeleton design/language/source build/skel-boards`, then `node scripts/design/boards-page/build.mjs build/skel-boards design/language/html/assets design/startup-skeleton-brief/round-1.json design/startup-skeleton-brief/round-1.html`.
- Ruling: A · Remembered layout, approved by the user with the 5 s / 15 s timings and Retry ⌘R. Rules and handoff: `design/features/startup-skeleton/rules.md`.
