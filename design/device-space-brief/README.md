# Device space and navigation brief (Companion, iPhone + iPad)

Input for a Claude Design round on how the Companion uses screen space and moves between screens (19 Nov 2026). `prompt.md` is the brief for the Design chat (one paragraph); `screens/` are today's screens from the simulator.

The user, verbatim: «на компаньонах совсем не уважается пространство экрана, в той же вики хедер не убирается и не прячется а он занимает хорошую четверть экрана и таких примеров по всему приложению куча, на айпаде сайд панели которые не сворачиваются тоже проблемы, переходы по ссылкам в вики почему-то открывают страницу в попапе а не переходят на страницу в вике, переход по клику на ссылку в бэклог потом непонятно как вернуться в сессию», then «надо подумать над UX».

## Problems in one look

1. **Fixed chrome on reading screens.** The wiki page keeps 248 of 874 pt (28%) above the text on the iPhone and never hides it. While a permission or question waits, a session shows 15–35% transcript.
2. **No way back.** Every jump to another section replaces the whole navigation state (`AppNavigation.open`), so Back leads to that section's list, not to where you came from. On the iPad there is no Back at all.
3. **Links leave the place.** A link inside a wiki page opens a full-screen doc popup instead of the wiki page. The Mac's wiki handles the same link in place.
4. **The iPad copies the phone.** The list column can't be hidden, so the detail column gets 47% of the width in landscape and 54% in portrait. The wiki's own page tree is pushed inside the detail column, while the list column holds More's four rows. Doc popups cover all three columns with unbounded line length.

## Screens and measurements

iPhone 18 Pro, 402 × 874 pt (status bar 62 pt). iPad Pro 11" (M5), 1210 × 834 pt landscape and 834 × 1210 pt portrait. Fixture data (`client/CompanionKit/Tests/CompanionKitTests/Fixtures`, plus a wiki section dumped from the real `plugins/wiki` view code), light theme. Frames come from the XCUITest accessibility tree. "Top" is everything above the first content row, status bar included. "Bottom" is docks, composer, search bar and home indicator.

### iPhone

| Screen | File | Top | Bottom | Content | Hides on scroll? |
|---|---|---|---|---|---|
| Agents list | `iphone-agents-light.png` | 286 pt (33%): large title 106 + Mac line + Agents/Backlog/Docs/More segments + filter chips | 84 (10%): search bar + mic | 504 (58%) | yes: down to 116 (13%), see `iphone-agents-scrolled-light.png` |
| Backlog list | `iphone-backlog-light.png` | 273 (31%) | 84 | 59% | yes: 116, `iphone-backlog-scrolled-light.png` |
| Docs list | `iphone-docs-light.png` | 273 (31%) | 84 | 59% | yes (same list) |
| More | `iphone-more-light.png` | 233 (27%) | mic floats | 4 rows (~180 pt), the rest is empty | n/a |
| Inbox | `iphone-inbox-light.png` | 236 (27%) | mic floats | 73% | large title (not measured scrolled) |
| Wiki list (More → Wiki) | `iphone-wiki-list-light.png` | 292 (33%): large "Wiki" title, then the plugin's own "Wiki · 7 pages +" row, then search | mic floats | 67% | no: the plugin's head and search sit outside the tree's scroll (`plugins/wiki/view.go:124`) |
| **Wiki page** | `iphone-wiki-page-light.png` | **248 (28%)**: inline "Wiki" bar 54, then the SDUI header 132 (title + •••, "updated … by", Edit/History) | mic floats over the text | 592 (68%) | **no**, `iphone-wiki-page-scrolled-light.png` |
| Session, permission waiting | `iphone-session-permission-light.png` | 178 (20%): inline title + another session's request banner 56 | **389 (45%)**: permission card 281 + composer + caption | **307 (35%)** | no (docked) |
| Session, two questions | `iphone-session-question-light.png` | 178 (20%) | **564 (65%)**: paged question card + composer | **132 (15%)** | no |
| Session, subagent asking | `iphone-session-subagent-asking-light.png` | 178 (20%) | 258 (30%) | 50% | no |
| Run detail | `iphone-run-light.png` | 178 + step strip | 447 (51%): step approval card | ~29% | no |
| Backlog task | `iphone-backlog-task-light.png` | 116 (13%) | mic floats over rows | 87% | inline |
| Doc popup | `iphone-doc-popup-light.png` | 116 (13%), full-screen cover with Done | none | 87% | inline |
| Cron (More section) | `iphone-cron-light.png` | 168 (19%) | mic | 81% | large title |
| Mac details | `iphone-mac-details-light.png` | 196 (22%) | mic | 78% | large title |
| Macs | `iphone-macs-light.png` | 168 | mic | 81% | large title |
| Sheets | `iphone-usage-sheet-light.png`, `iphone-connection-sheet-light.png` | system sheet | | | |

The floating mic (44 pt, bottom left) covers text on every reading screen (wiki page, backlog task, inbox).

### iPad

| Screen | File | Columns (pt) | Detail share | Notes |
|---|---|---|---|---|
| Session, landscape | `ipad-land-session-permission-light.png` | Macs 260 · list 380 · detail 569 | **47%** of the width | "Hide Sidebar" hides only the Macs column: detail 829 (69%), `ipad-land-flow-d1-sidebar-hidden-light.png`. Nothing hides the list. |
| Session, portrait | `ipad-port-session-permission-light.png` | list 380 · detail 453 | **54%** | "Show Sidebar" lays Macs over the list (`ipad-port-flow-d1-sidebar-overlays-light.png`). Nothing hides the list. |
| Wiki page, landscape | `ipad-land-wiki-page-light.png` | Macs 260 · More (4 rows) 380 · page 569 | text area 569 × 596 = **34% of the screen** | header 218 pt above the text (26% of the height); with the sidebar hidden 829 × 596, `ipad-land-flow-d2-wiki-sidebar-hidden-light.png` |
| Wiki page, portrait | `ipad-port-wiki-page-light.png` | More 380 · page 453 | 54% | the wiki's page tree is pushed inside the 453-pt column (`ipad-port-wiki-list-light.png`) while the 380-pt column lists Wiki · Processes · Cron · Triggers |
| Doc popup | `ipad-land-doc-popup-light.png`, `ipad-port-doc-popup-light.png` | full-screen cover over all columns | 100% | landscape line length ≈ 1180 pt (~150 characters) |
| Backlog task | `ipad-land-backlog-task-light.png` | 260 · 380 · 569 | 47% | body capped at 720 (`BacklogPlugin.swift:593`) |
| Others | `ipad-land-agents-light.png`, `ipad-land-run-light.png`, `ipad-land-session-question-light.png`, `ipad-land-docs-light.png`, `ipad-land-inbox-light.png`, `ipad-land-cron-light.png`, `ipad-port-agents-light.png`, `ipad-port-macs-light.png`, `ipad-port-inbox-light.png` | | | `ipad-port-macs-light.png`: the detail column is an empty "Clubmate" placeholder, 55% of the screen |

## Flows (screenshot sequences)

**(a) The wiki header never hides.** `iphone-wiki-page-light.png` → `iphone-wiki-page-scrolled-light.png`: after two swipes the text has moved and the 132-pt header with title, meta, Edit and History is still there.

**(b) A wiki link opens a popup.** `iphone-wiki-page-light.png` (tap "payments overview") → `iphone-wiki-link-popup-light.png`: a full-screen cover titled "Payments overview" with Find and Done. It has no wiki header, history or Edit, and is not selected in the wiki tree. Done returns to the old page. iPad: `ipad-land-wiki-link-popup-light.png` (covers all three columns).

**(c) From a session to a backlog task, and back.** iPhone: `iphone-flow-c1-session-task-link-light.png` (tap "#4") → `iphone-flow-c2-backlog-task-light.png` → Back → `iphone-flow-c3-back-lands-in-backlog-light.png` (the Backlog list, not the session) → Back → `iphone-flow-c4-second-back-macs-light.png` (Macs). To get back: Agents segment, then find the session row again (3 taps, scroll position lost). iPad: `ipad-land-flow-c1-session-task-link-light.png` → `ipad-land-flow-c2-backlog-task-no-back-light.png`: the middle column turned into Backlog and the detail has no Back. To return: folder menu → Agents → the row.

**(d) iPad columns that won't collapse.** `ipad-land-session-permission-light.png` → "Hide Sidebar" → `ipad-land-flow-d1-sidebar-hidden-light.png` (two columns; no control left to hide the list). Portrait: `ipad-port-session-permission-light.png` → "Show Sidebar" → `ipad-port-flow-d1-sidebar-overlays-light.png`.

## Causes (code)

**(a) The wiki header doesn't hide.**
- The plugin stacks the header next to the page instead of inside it: `plugins/wiki/view.go:399-400` builds `VStack(Header(...), Document(... Size: fill))`.
- The phone draws `document` as a WKWebView that fills what's left and scrolls on its own (`client/Companion/SDUI/Content.swift:234`, `DocumentNode`). The header (`HeaderNode`, `Content.swift:306`) sits outside that scroll view, so nothing can collapse it.
- The header stacks everything: a 2-line Title 2 title (`Content.swift:398`), then meta, then the buttons in their own `FlowLayout` row (`Content.swift:374`).
- `SectionScreen` adds an inline "Wiki" bar on top of it (`client/Companion/SDUI/SectionScreen.swift:36-41`).

**(b) A wiki link opens a popup.**
- `DocumentNode.follow` fires `openDoc` (`Content.swift:241-244`). `SDUIContext.fire` intercepts every `openDoc` whenever the screen has an `openDoc` closure (`client/Companion/SDUI/SDUINodeView.swift:19`), and `SectionScreen` always passes one (`SectionScreen.swift:68-70`, `91`).
- That closure calls `AppNavigation.openDoc` (`client/Companion/App/Navigation.swift:112`), which `RootView` presents as `.fullScreenCover` (`client/Companion/App/RootView.swift:69`, `DocumentSheet`).
- The wiki plugin never gets the event. Its own handler would select the linked page in the wiki (`plugins/wiki/ui.go:480`, `openLink` `ui.go:610-631`), and falls back to `docs.open` only for files outside the wiki.

**(c) No way back after a backlog link.**
- The session's task opener calls `navigation.open(computer:plugin: Backlog, detail:)` (`client/Companion/Plugins/Agents/SessionDetailView.swift:764-776`).
- `AppNavigation.open` (`Navigation.swift:78-85`) replaces `sidebar` (the section) and `detail`. Nothing records where you came from.
- On the iPhone the split view collapses to sidebar › list › detail, so Back walks up the new section. On the iPad the section replaces the middle column, and the detail column has no Back.
- The same replace happens for: Inbox row → session (`client/Companion/Plugins/Inbox/InboxPlugin.swift:129`), notification → session (`client/Companion/App/LocalNotifier.swift:27`), the other-session request banner (`client/Companion/App/RequestBannerView.swift:15`), Needs you row (`client/Companion/App/SidebarView.swift:110`), backlog task → its agent session (`client/Companion/Plugins/Backlog/BacklogPlugin.swift:716-717`), an SDUI action that opens a session (`SectionScreen.swift:86`), and the section segments, which also clear `detail` (`client/Companion/App/PluginUI.swift:117-121`).

**(d) iPad columns that won't collapse.**
- `RootView` is a three-column `NavigationSplitView` with fixed widths: sidebar 260, list 320–440 (ideal 380) (`RootView.swift:17-25`).
- `AppNavigation` only ever sets `visibility` to `.all` or `.doubleColumn` (`Navigation.swift:63`, `arrange()` `132-145`). `.detailOnly` is never offered.
- `arrange()` runs again on every layout or section change, so any other state would not stick.
- Regular width (portrait) always forces two columns when a section is chosen (`client/CompanionKit/Sources/CompanionKit/Layout.swift:27-32`).
- The system "Hide/Show Sidebar" button toggles only the Macs column.
- More's sections push their own sidebar → main inside the detail column's `NavigationStack` (`SectionScreen.swift:23`, `28`), so on the iPad the wiki tree never gets a column of its own.

## Navigation map

How you move today, and whether there is an obvious way back to where you were.

| From → to | How | Back to origin? |
|---|---|---|
| Macs → workspace (Agents list) | push (compact) / column (iPad) | yes, ‹ Macs |
| Agents ↔ Backlog ↔ Docs ↔ More | segmented control (phone, `PluginUI.swift:68`), folder menu (iPad, `PluginUI.swift:132`) | no: replaces the section and clears the open item (`PluginUI.swift:117-121`); each section forgets its item and scroll |
| List row → session / task / doc / section item | push (phone), detail column (iPad) | yes |
| More → Wiki / Cron / Processes / Triggers → item | `SectionScreen` nested stack: plugin sidebar, then main pushed | yes (two Backs) |
| Session → doc link, doc card, "Open file" | full-screen cover (`RootView.swift:69`); links inside push in the cover's own stack with ‹ Back (`DocumentSheet.swift:51`, `103`) | yes: Done |
| Wiki page → link to another page | same full-screen cover (cause b) | Done, but the target never opens in the wiki |
| Wiki page → `#4` task link | the same `openDoc` path with `/task/4` (from code; not captured) | likely a "Can't Open Document" popup |
| Session → `#4` task link | section switch to Backlog (cause c) | **no** (phone: Back → Backlog list → Macs; iPad: none) |
| Backlog task → `#7` link in the description | replaces the detail (`BacklogPlugin.swift:612-617`) | no: Back → Backlog list, not the first task |
| Backlog task → its session (Start agent, owner row) | section switch to Agents (`BacklogPlugin.swift:716`) | no: Back → Agents list |
| Inbox row → session | section switch to Agents (`InboxPlugin.swift:129`) | no: Back → Agents list, not Inbox |
| Notification → session | `LocalNotifier.swift:27` | no previous place; Back → Agents list |
| Request banner (another session asks) → that session | replaces the detail (`RequestBannerView.swift:15`) | no: the session you were in is gone from the stack |
| Needs you (Macs screen) → session | `SidebarView.swift:110` | Back → Agents list, not Needs you |
| Subagent ↔ parent ("Started by") | replaces the detail (`SessionDetailView.swift:262`) | Back → list (from code) |
| Aside | sheet at 62% over the session, ⤢ pushes it | yes (sheet / ‹) |
| Settings, Session Info, Always allow, full diff, usage, connection, report, task editor, Start agent, Won't do | sheets | yes: Done / × |
| Image, mermaid diagram | full-screen cover | yes |

## Core jobs (iPhone, one Mac; the app opens on the Agents list)

| Job | Taps | Where context is lost or space is short |
|---|---|---|
| Check a session that needs you | 1 (Needs you row) or 1 from a notification | Back goes to the Agents list, never to where you were (Inbox, another session, Needs you) |
| Answer a permission | 2 (row, Allow once) | the card + composer take 45% of the height, transcript 35%; another session's banner takes 56 pt on top |
| Answer two questions | 5 (row, pick, Next, pick, Send) | transcript 15% while answering |
| Read a wiki page | 3 (More segment, Wiki, page) | 28% fixed header; links open a popup, not the wiki; a wiki page linked from a session opens as the same popup |
| Look up a backlog task from a session and come back | 1 there; 3 back (Back, Agents segment, the row) | the session and its scroll position are gone from the stack; on iPad there's no Back at all |
| Start a session | 2 (New Session, Send) + typing | fine |

## Where the iPad copies the phone, or wastes space

- The list column (380 pt) can never hide. The detail is 47% of the width in landscape (69% with the Macs column hidden) and 54% in portrait. There is no reading mode.
- More keeps four section rows in a 380-pt column while the wiki's own tree is pushed inside the detail column, phone-style. On the iPad the tree should take that column.
- The wiki page header is the phone's three-row stack at 569–829 pt wide.
- A doc or wiki-link popup is a full-screen cover over all three columns with ~150-character lines in landscape, instead of opening in the detail column.
- A cross-section jump swaps the middle column wholesale. The list you came from is gone, and there is no Back in the detail.
- Portrait with no session open: the detail column is a "Clubmate" placeholder over 55% of the screen (`ipad-port-macs-light.png`).

## What is fine

- Agents, Backlog and Docs lists: the large title, Mac line, segments and chips scroll away (top chrome 33% → 13%).
- Backlog task, doc popup, session: inline bars of 116 pt including the status bar.
- Inside the doc popup, doc-to-doc links push with ‹ Back.
- Sheets close the same way everywhere (Done / ×).
- On the iPad, "Hide Sidebar" works for the Macs column and is remembered (`sidebarHidden` in `UserDefaults`).
- The session transcript and the cards keep an 820-pt reading measure on wide screens.

## How these were taken

- `make companion`, then a throwaway XCUITest (not committed) on own simulators `cm-dsb-iphone` (iPhone 18 Pro) and `cm-dsb-ipad` (iPad Pro 11" M5). It launched with `-fixtures <copy> -autoConfirmPairing YES -pair … -open <target>` as in `design/companion-brief/README.md`, and saved `XCUIScreen` shots and accessibility trees.
- The fixture copy added a `wiki` section whose `wiki.sidebar` / `wiki.main` trees came from `sidebarView` / `mainView` in `plugins/wiki` (through a throwaway test). It also added an agent message with task link `#4` to `agt_0000000000000001`.
- Dark theme: the iOS 27 simulator stayed light under both `XCUIDevice.appearance` and `simctl ui appearance dark`. Every shot is light; space and navigation don't depend on the theme.
- iPad shots are rotated to landscape and downscaled: iPhone to 603 px wide, iPad to 1200 (landscape) or 1000 (portrait).
- Leftovers (the wiki `#4` link on the phone, dark shots): backlog #938.

## Round 1

Claude Design, 19 Nov 2026. Canvas "Clubmate · Device UX": https://claude.ai/artifact/XgeKPdK3CsRqRPkGUMDMMy, chat https://claude.ai/chat/a77b7db2-9cd7-4fa6-b1a9-86906669d093. Pages: Today (`Main`: every screenshot captioned with its file name, plus `C-Macs`, `C-Workspace`, `C-Permission`, `C-Question`, `C-iPad`), r1, Language (`C-Language`, `C-Components`). Canvas copies of cm.css / c.css: `/_blob/335c10eb…` / `/_blob/b817086b…`; the round's own stylesheet `/_blob/c2936f2b…` is `boards/r1/r1.css`.

- `round-1.html` (built from `round-1.json`): the round in one page. Boards in `boards/r1/` (`.dc.html` + `canvas.json` as pulled).
- A · Fold into the bar (designer's pick): system bars fold on scroll, request cards fold to a 56-pt row, iPad columns step down with the sidebar button.
- B · Get out of the way: chrome, banner and mic leave on scroll, one tap brings them back; amber pill for a waiting request; iPad reading view on ⌃⌘F.
- C · Everything at the bottom: one bottom bar with back, title and actions; Macs column becomes a rail on iPad.
- Shared navigation: cross-section links push on the current stack with a named back chip; wiki and doc links open in place (the phone stops catching `openDoc`, the wiki plugin already routes the path); one stack per segment.
- Recommendation: A, plus B's reading mode for wiki pages and docs and the iPad reading view.
- Answered in the chat from the code: today's 2 permission taps are open + Allow once (no confirm step); plugins send no typed link targets, so link routing needs no plugin change.
- Open for the pick: Wiki as its own iPhone segment (2 taps to a page; drawn on A1 only) or under More (3 taps).
