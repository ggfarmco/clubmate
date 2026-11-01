# Companion check — 1 Nov 2026, b91c54f2

Debug build (`make companion`), iOS 27.0 simulator, status bar 9:41. Every launch: `-fixtures client/CompanionKit/Tests/CompanionKitTests/Fixtures -autoConfirmPairing YES -pair 'clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000'` plus the args below. Fresh install per shot.

iPhone shots are iPhone 18 Pro, which is 402×874 pt here (1206×2622 px), not the boards' 393×852; the boards are 2×, the shots 3×.

| File | Device | Look | Extra args | Wait |
|---|---|---|---|---|
| C-Macs(-Light) | iPhone 18 Pro | dark / light | `-macs 3 -open sidebar` | 22 s |
| C-Workspace(-Light) | iPhone 18 Pro | dark / light | `-macs 3 -open agents` | 9 s |
| C-Permission(-Light) | iPhone 18 Pro | dark / light | `-macs 3 -open agents/agt_0000000000000001` | 9 s |
| C-Question(-Light) | iPhone 18 Pro | dark / light | `-macs 3 -open agents/agt_0000000000000002` | 9 s |
| C-Inbox(-Light) | iPhone 18 Pro | dark / light | `-macs 3 -open inbox` | 9 s |
| C-MacInfo(-Light) | iPhone 18 Pro | dark / light | `-macs 3 -open mac` | 9 s |
| x-Backlog | iPhone 18 Pro | dark | `-macs 3 -open backlog` | 9 s |
| x-Offline | iPhone 18 Pro | dark | `-offline 4 -open sidebar` (one Mac) | 28 s |

## Not reproduced

- iPad and Fold: left out on purpose, not tested yet.
- Always-allow confirm: needs a tap; no UI driver used for a capture-only pass.
- Shots are only what fits on screen: the MacBook Air (offline) section on C-Macs and Notifications / Forget this Mac on C-MacInfo are below the fold.
- Fixture content differs from the boards (workspace `clubmate` not `design-brief`, an extra Done section, a running Explore subagent, inbox order); not counted as differences below.

## Looks broken or off vs the boards

- Permission / Question: a Needs-you banner ("Move the state bucket… · 1 more") floats under the header and covers the transcript (on C-Permission it overlaps the Plan card). No banner on the boards.
- Workspace: the subagent row under "Refactor…" is folded into a "1 ›" chevron instead of the nested "Subagent · Explain the checkout flow / Asks: …" row; the "Needs you 2" count then shows one row. Board has no Agents | Backlog control (new in 3e7f3561).
- Question: no "Started by · Refactor the cart total…" parent card above the transcript; bullets are small grey dots, not the board's full-size text-coloured ones.
- Permission: card reads "Claude says: Run the shop tests" where the board says "go test isn't in this workspace's allow list."; extra "···" in the card header and a "Sends after the current turn · Stop is in ···" footer; send button is neutral grey (board: dim green).
- Macs: Needs-you order is question → terraform → go test (board: go test → question → terraform); shop subtitle adds "1 running".
- Offline (one Mac): the Needs-you section disappears while the Mac is offline although its rows say "2 waiting".
