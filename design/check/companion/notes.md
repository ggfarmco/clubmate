# Companion re-check — 1 Nov 2026, after the polish pass on 932c48f0

Re-capture after the four polish fixes (› on navigable rows, row density, no ✕ on the Needs-you banner, photo attach glyph) and the first iPad capture. Debug build (`make companion`), iOS 27.0 simulator, status bar time 9:41, fresh install per shot.

- iPhone: iPhone 18 Pro (402×874 pt, shots 1206×2622 px at 3×; boards are 393×852 at 2×).
- iPad: iPad Pro 13-inch (M5) (1032×1376 pt portrait, shot 2064×2752 px at 2×; the board is landscape 2388×1668). `-orientation landscape` is still ignored: the app supports iPad multitasking (no `UIRequiresFullScreen`), so `requestGeometryUpdate` is a no-op, and `simctl` has no rotate command. The shot is portrait; at 1032 pt it is still the wide three-column layout.

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
| C-iPad | iPad | dark | `-macs 3 -open agents/agt_0000000000000001 -orientation landscape` (portrait, see above) | 12 s |

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
- **C-Permission**: matches decision 1, P0 "Stop", P1 banner; the banner has no ✕ and the attach button is the photo glyph. Remaining: the Plan card's "Plan · 2 of 3" header is still under the banner. The fixture's plan lists the done step ("Extract cartTotal") which the phone board doesn't show, and the iPhone 18 Pro has ~18 pt more bottom safe area than the board; the transcript opens scrolled to the end, so the card's top is clipped.
- **C-Question**: matches (P1 "Started by", P2 bullets, "Answer Claude…"); the parent's permission banner shows (seed, the rule working).
- **C-Inbox**: matches; rows denser. Inbox notification rows have no › (the board draws none there). "Earlier" order as before (seed or an ordering question, see the previous note).
- **C-MacInfo**: matches; Forget this Mac now fits. The board's "Notifications · All requests ›" row has no feature behind it in the app (not added).
- **C-Backlog**: matches P1 / P2, › on task rows, board density.
- **C-Offline**: matches decision 3; dimmed rows keep their ›.
- **Light**: same as dark across Macs, Workspace, Permission, Inbox, MacInfo, Backlog.

## C-iPad (portrait vs landscape board)

Matches: three columns (Macs · shop · session), no › anywhere (selection instead), session header title + "Waiting for you · ● Mac Studio · shop", transcript (bubble, code block, plan card with done/current/next, tool group, running Explore card), docked request card, photo attach.

Differences:
1. Selected workspace row in the Macs column is a solid accent fill; "2 waiting · 1 running" on it is warning/secondary ink on green and unreadable. The board uses a dim green tint with accent-ink name and a count. Real gap.
2. Macs column: Needs you is a full section with rows truncated to "Should I a…" / "Mac Studi…" in the 260 pt column; the board collapses it to one "Needs you 3" row. Workspace rows are two-line with activity, the board's are one line with a count at the right ("shop 2", "infra 1", Inbox "3"). Mac headers truncate the presence ("on…", "offl…"); the board writes "MacBook Pro · online" inline.
3. Sessions column: large title "shop" + Mac line + segmented control; the board has an inline title and no segmented control, and a plain (not inset-grouped) list with a green-tinted selected row; ours highlights the selected row grey.
4. Session header: no Stop button (companion.md "iPad: header button"; the board draws ■ next to •••); subtitle lacks the mode ("Ask before edits").
5. Detail layout is the phone's stacked layout: the board puts Plan and actions cards side by side and the request card on two rows (title + command inline; Allow once · Always allow… · Reject in one row).
6. Composer: ours has the Send button and the "Sends after the current turn · Stop is in •••" caption; the board has neither (with a Stop header button the caption would change).
7. Seed: the other-Mac banner ("Move the state bucket… 1 more") and "Thought for 3 s" show; the board has neither.

## Remaining gaps

- Permission Plan header still clipped on iPhone (see C-Permission).
- iPad items 1–6 above; item 1 (unreadable selected row) is the one that looks broken.
- MacInfo Notifications row: needs a per-Mac notification setting first.

## Not reproduced

- Always-allow confirm sheet: needs a tap; no UI driver in a capture-only pass.
- iPad landscape (see above).
- Shots show only what fits on screen.
