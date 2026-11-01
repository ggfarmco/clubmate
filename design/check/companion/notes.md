# Companion re-check — 1 Nov 2026, after the polish pass on 932c48f0

**iPad pass (1 Nov 2026, later):** C-iPad re-captured in landscape after the seven iPad fixes; C-Permission(-Light) re-captured for the plan fold. Everything else below is from the earlier pass.

Re-capture after the four polish fixes (› on navigable rows, row density, no ✕ on the Needs-you banner, photo attach glyph) and the first iPad capture. Debug build (`make companion`), iOS 27.0 simulator, status bar time 9:41, fresh install per shot.

- iPhone: iPhone 18 Pro (402×874 pt, shots 1206×2622 px at 3×; boards are 393×852 at 2×).
- iPad: iPad Pro 13-inch (M5), landscape 1376×1032 pt (shot 2752×2064 px at 2×; the board is 2388×1668). `-orientation landscape` is ignored (multitasking app, `requestGeometryUpdate` is a no-op, `simctl` has no rotate), but `XCUIDevice.shared.orientation = .landscapeLeft` in a UI test rotates the simulator. Recipe: a throwaway test in `CompanionUITests` launches with the args below, rotates, sleeps 12 s and touches a marker file under `/tmp`; a host loop waits for the marker and runs `xcrun simctl io <udid> screenshot` (the XCTest screenshot attachment of a rotated iPad comes out clipped and sideways).

Every launch: `-fixtures <absolute path>/client/CompanionKit/Tests/CompanionKitTests/Fixtures -autoConfirmPairing YES -pair 'clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000'` plus the args below. The fixture path must be absolute: the app's working directory in the simulator is `/`, so a relative path reads every fixture as empty ("No sessions", "Couldn't Load the Transcript").

| File | Device | Look | Extra args | Wait |
|---|---|---|---|---|
| C-Macs(-Light) | iPhone | dark / light | `-macs 3 -open sidebar` | 22 s |
| C-Workspace(-Light) | iPhone | dark / light | `-macs 3 -open agents` | 9 s |
| C-Permission(-Light) | iPhone | dark / light | `-macs 3 -open agents/agt_0000000000000001` | 9 s |
| C-Question(-Light) | iPhone | dark / light | `-macs 3 -open agents/agt_0000000000000002` | 9 s |
| C-Inbox(-Light) | iPhone | dark / light | `-macs 3 -open inbox` | 9 s |
| C-MacInfo(-Light) | iPhone | dark / light | `-macs 3 -open mac` | 9 s |
| C-Backlog(-Light) | iPhone | dark / light | `-macs 3 -open backlog` | 9 s |
| C-Offline | iPhone | dark | `-offline 4 -open sidebar` (one Mac) | 28 s |
| C-iPad | iPad | dark | `-macs 3 -open agents/agt_0000000000000001`, rotated in a UI test (see above) | 12 s after rotating |

Seed data is not counted as a difference: workspace `clubmate` instead of `design-brief`, the extra running Explore subagent ("1 running" on shop), ages, the Explore title carrying its last tool ("· Read cart_test.go"), "3 actions" instead of 2, inbox rows, the Docs segment next to Agents / Backlog (the docs plugin postdates the boards). Needs-you order on Macs and Offline is oldest first by `needsYouSince` (fix list §3 P2 "Order of Needs you").

## Row metrics (measured, pt)

| Row | Board | Before | Now |
|---|---|---|---|
| Two-line (Needs you, workspace, session, backlog task) | 66 | ~80 | 65 |
| One-line (Inbox on Macs) | 50 | ~66 | 52 (iOS 27's minimum list row) |
| MacInfo colour | 48 | ~74 | 48 |
| MacInfo Name | 52 | 52 | 49 |
| Transcript card gap | 10 | 16 | 10 |

## Per board

- **C-Macs**: matches, now with › on Needs-you, workspace and Inbox rows and board density; the MacBook Pro section and the MacBook Air header fit above the fold.
- **C-Workspace**: matches (› top-aligned on the first title line like the board; a parent with a folded-subagent toggle "N ›" gets no second ›).
- **C-Permission**: matches decision 1, P0 "Stop", P1 banner; the banner has no ✕ and the attach button is the photo glyph. The plan card now folds the done step behind "Show done" like the board, so the "Plan · 1 of 3" header sits just under the banner (the count stays done-of-total like the Mac card and the iPad board; the phone board's "2 of 3" would make the two clients disagree). The Mac plan card folds the same way (`PlanFold` in ClubmateProtocol, `docs/agents.md` § Transcript folding); looked at in a Kanagawa test instance with the fake agent's Rate-limit session: "Plan · 2 of 4", Show done, current and pending steps only; only the card's top padding is still under it (the iPhone 18 Pro has ~18 pt more bottom safe area than the board, and the transcript opens scrolled to the end).
- **C-Question**: matches (P1 "Started by", P2 bullets, "Answer Claude…"); the parent's permission banner shows (seed, the rule working).
- **C-Inbox**: matches; rows denser. Inbox notification rows have no › (the board draws none there). "Earlier" order as before (seed or an ordering question, see the previous note).
- **C-MacInfo**: matches; Forget this Mac now fits. The board's "Notifications · All requests ›" row has no feature behind it in the app (not added).
- **C-Backlog**: matches P1 / P2, › on task rows, board density.
- **C-Offline**: matches decision 3; dimmed rows keep their ›.
- **Light**: same as dark across Macs, Workspace, Permission, Inbox, MacInfo, Backlog.

## C-iPad (landscape, same orientation as the board)

Matches: three columns (Macs · shop · session); Macs column with one "Needs you 3" row, Mac headers "Mac Studio · online" / "MacBook Air · offline", one-line workspace rows with the amber waiting count ("shop 2", "infra 1"), Inbox "2" / "1"; the selected row is the dim green tint with label ink and a green folder glyph (light: same tint, label stays dark, checked); session header with ■ Stop next to ••• (asks once, the popover anchors on the button); Plan and the 3-actions card side by side; the request card's Allow once · Always allow… · Reject on one row; caption "Sends after the current turn".

Differences:
1. A 2 pt green rectangle around the selected Macs row: the iPadOS keyboard focus halo of the list cell. It shows only with a hardware keyboard attached (the simulator always has one); `focusEffectDisabled`, `focusable(false)` and a row tint don't reach the UIKit cell halo. With the focus elsewhere (after tapping ■) the row is just the tint.
2. Sessions column: large title "shop" + Mac line + segmented control; the board has an inline title, no segmented control and a plain list with a green-tinted selected row; ours is inset grouped with a grey selected row. Not in this pass.
3. Session header subtitle lacks the mode ("Ask before edits"); the composer keeps its Send button (the board has none). Not in this pass.
4. The request card keeps title and command on two lines (the board puts the command inline after "Run a command?") and keeps the "Claude says" caption.
5. The board's actions card lists each call with "Show diff"; ours is the folded tool-group row ("3 actions · read cart.go · edit … +2 −1"), and the two cards keep their own heights.
6. Seed: the other-Mac banner, "Thought for 3 s", "Context compacted" and the running Explore card show; the board has none of them.

## Remaining gaps

- iPad differences 2–5 above (sessions column chrome, header mode, composer Send, request card command inline, actions card shape).
- MacInfo Notifications row: needs a per-Mac notification setting first.

## Not reproduced

- Always-allow confirm sheet: needs a tap; no UI driver in a capture-only pass.
- Shots show only what fits on screen.
