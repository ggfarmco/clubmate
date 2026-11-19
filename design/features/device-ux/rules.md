# Device UX (Companion, iPhone + iPad) — rules (decided 19 Nov 2026)

Source of truth for the build. Claude Design round 2, canvas "Clubmate · Device UX" (https://claude.ai/artifact/XgeKPdK3CsRqRPkGUMDMMy, page r2). Boards: `R2-iPhone-sections-wiki`, `R2-iPhone-session-requests`, `R2-iPhone-navigation`, `R2-iPad-landscape`, `R2-iPad-portrait`, `R2-Build-rules` (`.dc.html` here, all six in `boards.html`; styles `rounds/r1.css` + `r2.css`). Brief, audit and screenshots of today: `design/device-space-brief/`. Rejected round-1 directions (B as a system, C) and the round-1 page: `rounds/`.

## Decisions

| Decision | Source |
|---|---|
| Direction A (bars fold, cards fold, iPad columns step down) + B's reading mode for wiki/doc pages and the iPad reading view | user, after round 1 |
| Shared navigation: cross-section links push on the current stack with a named back chip; wiki/doc links open in place; one stack per tab | round 1, kept |
| Sections are a native `TabView` (Liquid Glass): Agents · Backlog · Wiki · More + `Tab(role: .search)` | user («отдельная вкладка», Liquid Glass tabs) |
| Docs section removed; no Docs row anywhere | user («docs можно убрать») |
| iPhone Macs screen removed: workspace capsule menu + Inbox tray on every tab root | user accepted the designer's call |
| Search covers the current workspace only | user |
| iPad: `.tabViewStyle(.sidebarAdaptable)` | designer (the sidebar already holds Needs you, Inbox, workspaces) |
| Question card follows `design/features/question-card-fold/rules.md` (B · Peek) | earlier ruling, same day; this design aligns to it |

## Structure

- **iPhone**: `TabView` is the app root. Tabs Agents · Backlog · Wiki · More + Search. Each tab = one `NavigationStack`, kept per workspace and restored on return. `.tabBarMinimizeBehavior(.onScrollDown)`.
- **Wiki tab** only when the workspace has the wiki plugin (else three tabs). Root = the plugin's tree node (`wiki.sidebar`); pages = its document nodes. "Recently updated" is the first group when the plugin sends it.
- **iPad**: same `TabView`, `.sidebarAdaptable`. Sidebar showing → tabs are its rows, with Needs you, Inbox and workspaces below. Sidebar hidden → system tab bar at the top. Inside each tab a `NavigationSplitView` list + detail. Portrait: the sidebar never sits inline, so the tab bar is always at the top; Needs you/Inbox/workspaces are reached from the capsule and the tray.
- **Workspace capsule**: leading glass capsule (`Menu`) "● shop ⌄" on every tab root. Lists Macs with their state → workspaces with amber counts · Mac details · Pair a Mac. Replaces the iPhone Macs root and today's `WorkspaceMenu` folder button. Switching swaps all four stacks to that workspace's saved ones.
- **Mac details** (connection, usage, paired devices, Pair a Mac, unpair): More › This Mac (usage line on that row) and the capsule menu row.
- **Connection state**: per Mac in the capsule menu. Current Mac offline/unauthorized → every tab root shows C3's offline row under the title, the capsule dot goes hollow, answer buttons disable as in C3.
- **Inbox**: tray button, trailing, on every tab root, amber dot while anything waits; pushes the Inbox page on the current tab. "Waiting for you" (today's Needs you, across all Macs) first, "Earlier" below. Seen rows lose the dot; answered ones move to Earlier. iPad sidebar keeps Needs you and Inbox rows.
- **Search tab**: scope = current workspace. Groups in order Sessions · Tasks · Wiki pages · Docs (`docs.list`: docs this workspace's sessions opened), 3 each then "Show all n". A result pushes on the Search tab's own stack with its section line; Back returns to the results, query kept. Empty field: Recent searches (5), Recently opened (5). No results: "Nothing in shop for "x"". The field's own mic is dictation only.
- **Mic**: Voice is a bar button on tab roots and pages and the first composer button in sessions. Nothing floats over content (the floating `VoiceButton` goes).
- **Top bar trailing**: tab root = Inbox · Voice · the section's create action. Pages = up to two actions (e.g. Edit) · Voice · ••• (History inside). Sessions: mic in the composer.
- **Tab bar hidden** in a session, on anything pushed above a session, and in reading mode.

## iPhone measurements (402 × 874 pt)

| Element | Value |
|---|---|
| Navigation bar | 44 pt at y 59 (safe area 62) |
| Large title + subtitle | 34/41 + 15/20 pt; folds into the bar over the first 52 pt of scroll |
| Tab bar | capsule 62 pt high, 20 pt side inset, 22 pt from the bottom → 84 pt bottom chrome; Search circle 62 pt, 12 pt gap |
| Tab bar minimized | two 54-pt circles, 20 pt inset, 26 pt from the bottom |
| Tab badge | 18 pt amber, sessions waiting in this workspace (Agents only) |
| Back chip | glass capsule 44 pt, padding 8 / 14, max 200 pt, chevron 20 pt; label ≤ 18 characters then "…" |
| Amber count (session bar) | glass capsule 44 pt, dot 9 pt + count |
| Permission card | open 251 pt · folded row 56 pt, radius 28, amber 1.5 pt border |
| Question card | peek 286 pt (grabber 36 × 5, header 36, question, option titles 46 each) · full ≤ 60% of the screen (366 here, with descriptions) · bar 56 pt (same row as the folded permission card) |
| Other chip (composer under a question) | 32 pt, amber tint, "Other · for <header>…", placeholder "Type your own answer…" |
| Composer row | 44 pt; dock padding 10 / 16 / 34; 10 pt between card and composer; Voice · Attach · field · Send/Queue |
| Reading mode | status bar only; 2-pt progress line at y 58, tertiary grey |
| Text | Body 17/26, side margins 20, code 14/21 mono |

Resulting space: wiki page chrome 28% today → 21% at rest, 13% scrolled, 7% reading; transcript under a permission 35% → 47% open, 70% folded; under questions 15% → 41% (peek).

## iPad columns (pt)

| State | Landscape 1210 | Portrait 834 |
|---|---|---|
| All columns | sidebar 260 · list 360 · detail 590 (49%) | — (sidebar only over the content) |
| List + detail | list 360 · detail 850 (70%) | list 320 · detail 514 (62%) |
| Detail only | 1210 | 834; list 320 slides over (shadow, no dim); picking a row closes it |
| Reading view | 1210, no bars, text 680 centred | 834, no bars, text 680 centred |
| Bands | tab bar 48 pt at y 26 (band 1 = 24–74); column bars 44 pt at y 80 (band 2); column bars at y 26 when the sidebar shows | same |
| Default | Agents, Backlog, Wiki open as list + detail; each section remembers its last state | list + detail while nothing is open; detail only once something opens |

- The list column belongs to the tab: Agents list, Backlog list, Wiki tree, More list. A push never changes it; selection in it follows the detail.
- Wiki tree + page in portrait: tree 320 · page 514 (text 434).
- Document measure in regular width: 680 pt.
- Band 2 (column bars below the tab bar) needs checking on a device: if iPadOS puts the column buttons beside the tab bar, band 2 goes away. Build with the system placement and compare.

## What folds, when, how it moves

| Element | Folds when | Comes back when | Animation |
|---|---|---|---|
| Large title → bar | system, interactive over 0–52 pt of scroll | scroll back under 52 pt | system |
| Tab bar | system `.onScrollDown` | scroll up, or tap a circle | system |
| Reading mode (wiki page, doc page) | header folded and 120 pt more scrolled down, last move ≥ 40 pt downwards | scroll up ≥ 24 pt · tap on text that is not a link · offset < 52 pt | hide `.easeOut(duration: 0.25)` · show `.spring(response: 0.3, dampingFraction: 1)` |
| Permission card (the only card that folds on scroll) | live end > 120 pt below the visible bottom; never while the composer has focus | live end within 24 pt · tap the row (scrolls to the end) | `.spring(response: 0.35, dampingFraction: 0.9)`; contents cross-fade 0.15 s |
| Question card full → peek | transcript scrolled up (any amount) | grabber drag up | same spring |
| Question card → bar | only a tap on the header chevron, a grabber drag down or ⌥Q; never by scrolling; kept per question set | tap the bar, grabber drag up, ⌥Q → peek | same spring |
| New request while folded | — | the row pulses once (scale 1.03, 0.3 s); no auto-scroll | — |
| Question → next question | 200 ms after a pick | — | slide left, `.spring(response: 0.3, dampingFraction: 1)` |
| Answered card | on answer | — | collapses into its transcript line (spring 0.35 / 0.9); Next row fades in 0.2 s |
| iPad columns | ⌃⌘S / sidebar toggle | next press | system split-view animation |
| iPad reading view | ⌃⌘F | ⌃⌘F · Esc · edge swipe; restores the column state exactly | `.easeInOut(duration: 0.3)`, bars and columns together |
| Reduce Motion | all slides and springs → cross-fade 0.2 s; reading mode never auto-enters | | |

Reading mode is not used with VoiceOver or Switch Control.

## Session screen

- Tab bar hidden; docked card + composer own the bottom.
- **Permission card**: arrives open at the live end, card order as C3 (Allow once first, filled). Folded row never answers: it says what waits and offers Answer; tap → open at the live end.
- **Question card**: three heights full · peek · bar per the Question card fold ruling; opens at peek on phone and iPad; one question on screen, 46-pt option rows, a pick advances after 200 ms, the last pick fills Send. iPad peek keeps option descriptions on one line; full capped at 60% of the column.
- **Composer under a question** = that question's Other (chip, placeholder above). No "Queue a message" while a question is pending; the plugin refuses a plain prompt (question-card-fold rules, handoff (d)).
- **Answered**: the card becomes one muted line in the transcript. If more wait, a **Next row** takes its place: the oldest other waiting request in this workspace, then other workspaces; gone when nothing waits.
- **Amber count in the session bar** replaces the other-session request banner: tap pushes the oldest waiting session at any count; Inbox lists them all.
- The "Stop is in •••" footnote goes.

## Keys (iPad hardware keyboard; iPhone too)

| Keys | Action |
|---|---|
| ⌃⌘S | step columns: all → list + detail → detail → all (portrait: list + detail ↔ detail) |
| ⌃⌘F | reading view on / off (pages and transcripts) |
| Esc | in order: leave reading view → close the list or sidebar over the detail → today's meaning (closes the voice panel). Never stops the turn on iPad |
| ⌘[ | Back (chevron or chip) |
| ⌘1 ⌘2 ⌘3 ⌘4 | Agents · Backlog · Wiki · More |
| ⇧⌘F | Search tab, field focused (everywhere) |
| ⌘F | find in page on an open wiki or doc page (WebKit find bar, as today); nothing elsewhere |
| ⇧⌘I | Inbox |
| ⌘N | the current tab's create action: new session, task, page |
| ⌥1 ⌥2 ⌥3 | permission: Allow once · Always allow… · Reject (as today) |
| ⌥1 … ⌥4 | question: pick option 1–4 |
| ⌥Q | question card: fold to bar / back to peek |
| ⌘↩ | Send (answers, or Queue a message when no question is pending) |

New keys go into `docs/shortcuts.md` § Companion on iPad in the commit that binds them.

## Navigation: every jump

| Jump | Opens | Back control | Back lands |
|---|---|---|---|
| Wiki page → wiki page (link) | `openDoc` reaches the wiki plugin, which selects the page; push in the Wiki stack | chevron | previous page, scroll kept |
| Task → task (#4 → #7) | push in the Backlog stack | chevron | #4 |
| Session → subagent | push in the Agents stack | chevron | parent session |
| Subagent → parent ("Started by") | pop if the parent is right below, else push | chevron | where you were |
| Session → task #4 | push on the Agents stack; section line "Backlog · shop"; ••• › Show in Backlog opens it in the Backlog tab instead | chip = session title | the session, scroll and card state kept |
| Task → its session (Last owner, Start agent) | push on the Backlog stack; line "Agents · shop" | chip "#4" | #4 |
| Any page → wiki page (`openDoc` path inside the wiki) | push on the current stack; line "Wiki · shop"; ••• › Show in Wiki | chip = origin title | origin |
| Any page → file outside any section | push a doc page; line "File · path" | chip = origin title | origin |
| Tray → Inbox | push on the current tab | chevron | tab root |
| Inbox → session | push | chip "Inbox" | Inbox, scroll kept |
| Needs you (iPad sidebar) | list column shows Needs you; a row opens in the detail | none (a selection) | — |
| Amber count in the session bar | push the oldest waiting session | chip = current session title | the session you were in |
| Search result | push on the Search tab's stack, with its section line | chevron | results, query kept |
| Next row after answering | replaces the answered session on the stack | keeps the original back control | where the first session came from |
| Notification, app in front | push on the current stack | chip = current page title | current page |
| Notification from Lock Screen / background | switch to that workspace's Agents tab; stack = Agents › session | chevron | that workspace's Agents list; other tabs untouched |
| Workspace capsule | swap all four stacks to the workspace's saved ones | — | — |
| Tap the current tab again | pop to root; a second tap scrolls to top | — | — |

**Chip rule.** A chip appears only when Back crosses a section or comes from Inbox or a notification; inside a section it is the plain chevron. Label = the origin's title. Long-press either = the system stack menu. On iPad the same rules apply inside the detail column.

Taps: session → #4 → back 1 + 3 (scroll lost) → 1 + 1 (scroll kept); Inbox → session → back = 3 from any tab root; two questions 5 → 3; wiki page from launch 3 → 2.

## Removing Docs

- The Docs section (`DocsPlugin`) goes from the tabs; shortcuts are ⌘1–⌘4 as above.
- Its list (`docs.list`) becomes Search's Docs group, last.
- A doc link (`openDoc` with an absolute path + #anchor) opens in place as before, from a session, task or page: pushed on iPhone, in the detail column on iPad. The full-screen `DocumentSheet` cover goes.
- No Docs row, no file browser. Saved state or deep links pointing at Docs open the Agents tab.

## Changes per screen

| Screen | Today | After |
|---|---|---|
| Launch (iPhone) | Macs list → workspace → segmented sections | Agents tab of the last workspace; capsule to switch |
| Section lists | large title + Mac line + segmented control + chips; bottom "Search sessions" field + floating mic | large title folds into the bar; tab bar (minimizes); Inbox · Voice · create in the bar; chips scroll with the list |
| Wiki | More › Wiki, title twice, header outside the scroll (248 pt) | Wiki tab = tree; page header is a large title + one subtitle line in the scroll, actions in the bar; reading mode |
| Doc / wiki link | full-screen cover | push in place (iPhone) / detail column (iPad) |
| Session | request banner on top; card always open; tab-less | amber count in the bar; permission folds to 56 pt; question card peek/full/bar; Next row |
| Backlog task | detail with mic floating | pushed with a chip when reached from another section |
| More | Wiki · Processes · Cron · Triggers | Processes · Cron · Triggers · This Mac |
| iPad | 3 fixed columns, list never hides; no Back in the detail | sidebarAdaptable tabs; column step-down; reading view; tree in the list column; chips in the detail |

## Stock SwiftUI (iOS 26/27 Liquid Glass) vs ours

Deployment target is already iOS 27.0 (`client/companion.yml:13`, `client/CompanionKit/Package.swift:6`), so every API below is available without `#available`. Use the system glass materials; don't imitate them.

| Part | Stock API | Ours |
|---|---|---|
| Sections | `TabView` + `Tab` × 4, `Tab(role: .search)` | which tabs show (Wiki only with the wiki plugin), per-workspace stack restore |
| Tab bar shrinking on scroll | `.tabBarMinimizeBehavior(.onScrollDown)` | — |
| iPad sidebar / tab bar | `.tabViewStyle(.sidebarAdaptable)`, sidebar toggle | Needs you, Inbox, workspaces rows under the tabs; ⌃⌘S step-down, remembered per section |
| Columns | `NavigationSplitView` with `columnVisibility` incl. `.detailOnly` | the step order and defaults per orientation |
| Large title folding into the bar, glass bars | `navigationTitle` + large display mode, system glass bar, `.scrollEdgeEffectStyle(.soft, for: .top)` (already used in `SessionDetailView.swift:274`) | the SDUI `header` node moving into the scroll so it can fold |
| Search | `Tab(role: .search)` + `.searchable` | result groups, scope, recents |
| Back | `NavigationStack` back button, long-press stack menu | the back chip (glass capsule with the origin title) where Back crosses a section |
| Workspace capsule | `Menu` with `.buttonStyle(.glass)` | its content (Macs, states, counts, This Mac, Pair) |
| Reading mode / reading view | — | ours: hiding bars + tab bar on the thresholds above, progress line |
| Folded request row, question card heights, amber count, Next row | — | ours (glass/amber per C3 tokens) |
| `.tabViewBottomAccessory` | not used: the mic is a bar button, the waiting count sits in the session bar, and the tab bar is hidden in sessions | — |

**Device-only trap** (memory `companion-menu-over-interactive-glass`): a `Menu` inside a `GlassEffectContainer` whose rows open over a sibling with `.glassEffect(.regular.interactive())` never gets the tap on the iPhone (the simulator passes). It applies to the workspace capsule menu and the ••• menus over the glass tab bar and composer. Don't put `.interactive()` on glass that is not a button next to a `Menu`, and check menu taps with an XCUITest run on the phone.

## Mapping to the code causes (brief § Causes)

| Cause (brief) | Where | Change |
|---|---|---|
| (a) wiki header beside the page | `plugins/wiki/view.go:399-400` `VStack(Header, Document)`; `client/Companion/SDUI/Content.swift:234` `DocumentNode` (own WKWebView scroll), `:306` `HeaderNode`, `:374`, `:398` | header renders inside the document's scroll as a large title + subtitle that folds into the navigation bar; actions → bar buttons by role; a title equal to the navigation title is drawn once; 680-pt measure; reading mode |
| (a) extra "Wiki" bar | `client/Companion/SDUI/SectionScreen.swift:36-41` | the Wiki tab's root is the tree; no extra inline bar |
| (b) links open a popup | `Content.swift:241-244` `follow` → `openDoc`; `client/Companion/SDUI/SDUINodeView.swift:19` intercepts; `SectionScreen.swift:68-70`, `91` pass the closure; `client/Companion/App/Navigation.swift:112` `openDoc`; `client/Companion/App/RootView.swift:69` `.fullScreenCover` `DocumentSheet` | stop intercepting `openDoc` in a section: let it reach the plugin (`plugins/wiki/ui.go:480`, `openLink` `:610-631`), push the result; only a path outside any section pushes a doc page; drop the full-screen cover |
| (c) no way back | `Navigation.swift:78-85` `open` replaces `sidebar` + `detail`; callers `client/Companion/Plugins/Agents/SessionDetailView.swift:764-776`, `:262`, `client/Companion/Plugins/Inbox/InboxPlugin.swift:129`, `client/Companion/App/LocalNotifier.swift:27`, `client/Companion/App/RequestBannerView.swift:15`, `client/Companion/App/SidebarView.swift:110`, `client/Companion/Plugins/Backlog/BacklogPlugin.swift:612-617`, `:716-717`, `SectionScreen.swift:86`; segments clear `detail` in `client/Companion/App/PluginUI.swift:117-121` | `AppNavigation` holds one path per (workspace, tab); a jump pushes a typed destination on the current path with its origin (for the chip); "Show in <section>" switches tab; per the navigation table |
| (d) iPad columns | `RootView.swift:17-25` fixed 3-column split; `Navigation.swift:63`, `arrange()` `:132-145` only `.all` / `.doubleColumn`; `client/CompanionKit/Sources/CompanionKit/Layout.swift:27-32`; `SectionScreen.swift:23`, `:28` nested sidebar → main | `TabView(.sidebarAdaptable)` root with a per-tab `NavigationSplitView`; visibility stepped by ⌃⌘S and the toggle and remembered per section (no `arrange()` override); list column 360 / 320; a section's tree node goes to the list column |
| segmented sections | `PluginUI.swift:68` `Picker(.segmented)`, `:132` `WorkspaceMenu`, `:30` `CompanionPlugins.all` (with `DocsPlugin`) | `TabView` tabs; capsule menu; `DocsPlugin` out of the tabs, its store feeds Search |
| floating mic | `client/Companion/App/VoiceButton.swift:20-28`, `RootView.swift:27` bottom content margin | Voice bar button / composer button |

## Handoff tags (from the rules board)

- (a) companion SwiftUI: tab bar, workspace capsule, back chip, section line, folded request row, question card heights, Other composer, amber count, Answered + Next, composer order, header with up to two actions, iPad sidebarAdaptable / step-down / reading view, This Mac, offline row on tab roots, Search tab.
- (b) SDUI rendering: `header` node folds into the bar, actions as bar buttons, title dedupe; `document` node 680-pt measure and reading mode; `tree` node in the iPad list column with selection following the detail; `openDoc` reaches the plugin.
- (c) plugin payload: header actions carry a role (primary → bar button, secondary → •••); a section marks its tree as its navigation (for the iPad list column); questions as structured options with an optional suggested one. Link routing needs no plugin change.

## Language changes (not applied yet)

New or changed companion components and patterns, to add to the language boards (`C-Language`, `C-Components`) and `.claude/skills/clubmate-ui/references/companion.md` as a separate step:

- Sections as a Liquid Glass `TabView` (replaces the segmented control, the bottom search field and the floating mic); iPad `sidebarAdaptable`.
- Workspace capsule menu (replaces the Macs root on iPhone and the folder menu).
- Inbox tray button with amber dot on tab roots.
- Back chip and section line under a pushed page's title.
- Folded request row (56 pt); question card three heights (with question-card-fold).
- Amber count in the session bar (changes C2 "a request arriving … shows as a banner").
- Answered row + Next.
- Composer: Voice · Attach · field · Send; footnote removed; Other chip under a question.
- Header = back · title · up to two actions · ••• (amends C2 "back · title · one ••• menu").
- Reading mode (phone) and reading view (iPad); column step-down.
- More › This Mac; offline row on tab roots.

## Not done here

- The designer did not open the rendered boards; `boards.html` was checked only in a headless Chrome render.
- Dark: no layout differs; each board has a Theme tweak, not exported as separate boards.
- iPad band 2 placement to verify on a device (see iPad columns).
