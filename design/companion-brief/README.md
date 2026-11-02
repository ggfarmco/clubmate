# Companion design brief

Input for a Claude Design pass over the iPhone/iPad companion: upload `screens/*.png` with the Mac boards named in `prompt.md`, paste `prompt.md`.

## Re-capturing the screens

1. `make companion`; `xcrun simctl create cm-brief "iPhone 18 Pro"`, boot it, `xcrun simctl status_bar <udid> override --time 9:41`.
2. Per screen: `xcrun simctl ui <udid> appearance dark|light`, install `build/CompanionDerivedData/Build/Products/Debug-iphonesimulator/Companion.app`, launch with `-fixtures client/CompanionKit/Tests/CompanionKitTests/Fixtures -autoConfirmPairing YES -pair 'clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000' -open <sidebar|agents|agents/<session>|inbox>`, wait ~7 s, `xcrun simctl io <udid> screenshot <file>`.
3. Three Macs like the C5 boards (Mac Studio blue, MacBook Pro magenta, MacBook Air graphite that goes offline after 3 s): add `-macs 3`. `-open mac` opens the first Mac's details (colour picker).
   `-open more` shows the More segment, `-open more/<cron|processes|triggers>` a plugin section from `Fixtures/sdui.json`; `-open agents/<session> -docOpened YES` publishes `docs.opened` for the first session ~3 s after launch (the "Opened …" banner on its screen); add `-noUIBridge YES` to act like a Mac without the UI bridge (`hello.ui` false: no More).
   `-open agents -search <query>` types the query into the Agents search field (the fixture's `agents.search` matches titles and fixture transcript text).
   `-failureAction signin|continue` changes the failed fixture session's (`agt_0000000000000005`) action from Retry to the Mac-only Sign in line or Hand off.
   `-doc <path>` opens the docs sheet on the first workspace; the fixture serves `/Users/me/src/shop/docs/checkout-retries.md`, `payments.md`, the diagram `checkout-flow.mmd` and the artifact `conversion.html` (a Chart.js bar chart from jsdelivr with a button that hides a series; from `Fixtures/docs-read.json`), any other path fails with `not in workspace`, and each doc changes once 4 s after its first read.
4. An offline Mac: `-offline <seconds>` makes the fixture Mac unreachable that many seconds after launch (the event stream drops, requests fail); `-offline 0` on a relaunch without `-pair` shows a paired Mac that is offline from the start with its saved last-seen time. Wait ~20 s for the reconnect backoff before the screenshot.
5. Fixture runs skip the notification permission prompt (Debug builds). Erase the simulator if an old prompt is still queued.
