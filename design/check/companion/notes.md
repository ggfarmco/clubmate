# Companion re-check — 1 Nov 2026, d45a8ff7

Re-capture after the design-check fixes. Debug build (`make companion`), iOS 27.0 simulator, iPhone 18 Pro (402×874 pt, shots 1206×2622 px at 3×; boards are 393×852 at 2×), status bar time 9:41. Fresh install per shot. iPad and Duo are out of scope.

Every launch: `-fixtures <absolute path>/client/CompanionKit/Tests/CompanionKitTests/Fixtures -autoConfirmPairing YES -pair 'clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000'` plus the args below. The fixture path must be absolute: the app's working directory in the simulator is `/`, so a relative path reads every fixture as empty ("No sessions", "Couldn't Load the Transcript").

| File | Look | Extra args | Wait |
|---|---|---|---|
| C-Macs(-Light) | dark / light | `-macs 3 -open sidebar` | 22 s |
| C-Workspace(-Light) | dark / light | `-macs 3 -open agents` | 9 s |
| C-Permission(-Light) | dark / light | `-macs 3 -open agents/agt_0000000000000001` | 9 s |
| C-Question(-Light) | dark / light | `-macs 3 -open agents/agt_0000000000000002` | 9 s |
| C-Inbox(-Light) | dark / light | `-macs 3 -open inbox` | 9 s |
| C-MacInfo(-Light) | dark / light | `-macs 3 -open mac` | 9 s |
| C-Backlog(-Light) | dark / light | `-macs 3 -open backlog` | 9 s |
| C-Offline | dark | `-offline 4 -open sidebar` (one Mac) | 28 s |

Seed data is not counted as a difference: workspace `clubmate` instead of `design-brief`, the extra running Explore subagent ("1 running" on shop), ages, the Explore title carrying its last tool ("· Read cart_test.go"), "3 actions" instead of 2, inbox rows. Needs-you order on Macs and Offline is oldest first by `needsYouSince` (question 25 min → terraform 4 min → go test 1 min), as decided in fix list §3 P2 "Order of Needs you"; the boards' order comes from their own seed ages.

## Per board

- **C-Macs**: matches (Needs you section, waiting in warning ink, Mac sections). MacBook Air is below the fold.
- **C-Workspace**: matches P0 "Companion workspace list" (subagent nested 24pt, no separator, not folded) and P1 "Running row" ("Editing reserve.go · 4 min"). Real gap: no › on session rows (see below).
- **C-Permission**: matches decision 1 (title, mono command, "Claude says:", Allow once / Always allow… / Reject, •••), P0 "Stop" (spinner + "Running · 10 s", no Stop button) and P1 banner (neutral surface, outline glyph, names the Mac, pinned above the transcript instead of over it). Differences: the banner has an extra ✕ Dismiss; the Plan card's "Plan · 2 of 3" header is not visible: the transcript opens scrolled to the end and the card's top is clipped under the pinned banner (presumably still rendered — `TranscriptViews.swift` draws it and the board's shorter rows fit it; unverified, no scroll in this pass); the attach button is a paperclip, the board's is a photo glyph.
- **C-Question**: matches P1 "Started by · ‹parent›" and P2 "Phone bullets". A Needs-you banner for the parent's permission shows above the transcript; the board has none, but the fixture has another pending request, so this is the banner rule working (seed). Placeholder "Answer Claude…" per P2 "Composer placeholder".
- **C-Inbox**: matches. "Earlier" lists Build finished (10 min, dimmed as read) before Stuck (1 h); the board puts Stuck first. Counted as seed, but if the board's order is meant (unread or by kind first), that is an ordering rule, not seed.
- **C-MacInfo**: matches content. Real gap: colour rows are ~74 pt tall against the board's 48 pt, so Notifications and Forget this Mac fall below the fold.
- **C-Backlog**: matches P1 "Backlog, phone" (inset grouped, counts at the right) and P2 "Priority" (neutral `!!` / `↓`). Real gap: no › on task rows.
- **C-Offline**: matches decision 3 (Needs you stays, dimmed, "Mac Studio is offline — answer when it's back", "as of" rows).
- **Light**: compared against the Workspace, Permission, Inbox and Backlog light boards; same results as dark, warning glyph fills match the boards' tone.

## Remaining real gaps (not in the fix list)

1. No › disclosure chevron on navigable list rows (Macs workspace/inbox rows, workspace sessions, backlog tasks, Needs-you rows); every board draws one. Present in the previous capture too and not raised in K2, so it may be an accepted divergence rather than a regression.
2. Row density: list rows are taller than the boards (two-line rows ~80 pt vs ~65 pt, MacInfo colour rows ~74 pt vs 48 pt), which pushes MacInfo's Notifications / Forget this Mac and the Permission Plan header out of view.
3. Needs-you banner has a ✕ Dismiss the C-Permission board doesn't show.
4. Composer attach button is a paperclip; the boards use a photo glyph.

## Not reproduced

- Always-allow confirm sheet: needs a tap; no UI driver in a capture-only pass.
- Shots show only what fits on screen.
