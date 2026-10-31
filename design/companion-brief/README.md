# Companion design brief

Input for a Claude Design pass over the iPhone/iPad companion: upload `screens/*.png` with the Mac boards named in `prompt.md`, paste `prompt.md`.

## Re-capturing the screens

1. `make companion`; `xcrun simctl create cm-brief "iPhone 18 Pro"`, boot it, `xcrun simctl status_bar <udid> override --time 9:41`.
2. Per screen: `xcrun simctl ui <udid> appearance dark|light`, install `build/CompanionDerivedData/Build/Products/Debug-iphonesimulator/Companion.app`, launch with `-fixtures client/CompanionKit/Tests/CompanionKitTests/Fixtures -autoConfirmPairing YES -pair 'clubmate://pair?v=1&c=ABCDEF&h=100.101.1.2&p=47622&id=mac-mock&n=Mac%20Studio&e=4000000000' -open <sidebar|agents|agents/<session>|inbox>`, wait ~7 s, `xcrun simctl io <udid> screenshot <file>`.
3. Fixture runs skip the notification permission prompt (Debug builds). Erase the simulator if an old prompt is still queued.
