# Device UX (Companion, iPhone + iPad) — rules (decided 19 Nov 2026)

Source of truth for the build. Claude Design round 2, canvas "Clubmate · Device UX" (https://claude.ai/artifact/XgeKPdK3CsRqRPkGUMDMMy, page r2). Boards: `R2-iPhone-sections-wiki`, `R2-iPhone-session-requests`, `R2-iPhone-navigation`, `R2-iPad-landscape`, `R2-iPad-portrait`, `R2-Build-rules` (`.dc.html` here, all six in `boards.html`; styles `rounds/r1.css` + `r2.css`). Brief, audit and screenshots of today: `design/device-space-brief/`. Rejected round-1 directions (B as a system, C) and the round-1 page: `rounds/`. Round 3 (20 Nov, after the first visual check V1) changes some rules below: see § Round 3 at the end; where it differs, Round 3 wins. Round 4 (20 Nov, iPad only, after the user's notes) replaces the iPad layout: see § Round 4 at the end; on the iPad it wins over R2 and R3.

## Decisions

| Decision | Source |
|---|---|
| Direction A (bars fold, cards fold, iPad columns step down) + B's reading mode for wiki/doc pages and the iPad reading view | user, after round 1 |
| Shared navigation: cross-section links push on the current stack with a named back chip; wiki/doc links open in place; one stack per tab | round 1, kept |
| Sections are a native `TabView` (Liquid Glass): Agents · Backlog · Wiki · More (Search tab dropped 2026-11-20) | user («отдельная вкладка», Liquid Glass tabs) |
| Docs section removed; no Docs row anywhere | user («docs можно убрать») |
| iPhone Macs screen removed: workspace capsule menu + Inbox tray on every tab root | user accepted the designer's call |
| Search covers the current workspace only | user |
| iPad: `.tabViewStyle(.sidebarAdaptable)` | designer (the sidebar already holds Needs you, Inbox, workspaces) |
| Question card follows `design/features/question-card-fold/rules.md` (B · Peek) | earlier ruling, same day; this design aligns to it |

## Structure

- **iPhone**: `TabView` is the app root. Tabs Agents · Backlog · Wiki · More. Each tab = one `NavigationStack`, kept per workspace and restored on return. `.tabBarMinimizeBehavior(.onScrollDown)`.
- **Wiki tab** only when the workspace has the wiki plugin (else three tabs). Root = the plugin's tree node (`wiki.sidebar`); pages = its document nodes. "Recently updated" is the first group when the plugin sends it.
- **iPad**: same `TabView`, `.sidebarAdaptable`. Sidebar showing → tabs are its rows, with Needs you, Inbox and workspaces below. Sidebar hidden → system tab bar at the top. Inside each tab a `NavigationSplitView` list + detail. Portrait: the sidebar never sits inline, so the tab bar is always at the top; Needs you/Inbox/workspaces are reached from the capsule and the tray.
- **Workspace capsule**: leading glass capsule (`Menu`) "● shop ⌄" on every tab root. Lists Macs with their state → workspaces with amber counts · Mac details · Pair a Mac. Replaces the iPhone Macs root and today's `WorkspaceMenu` folder button. Switching swaps all four stacks to that workspace's saved ones.
- **Mac details** (connection, usage, paired devices, Pair a Mac, unpair): More › This Mac (usage line on that row) and the capsule menu row.
- **Connection state**: per Mac in the capsule menu. Current Mac offline/unauthorized → every tab root shows C3's offline row under the title, the capsule dot goes hollow, answer buttons disable as in C3.
- **Inbox**: tray button, trailing, on every tab root, amber dot while anything waits; pushes the Inbox page on the current tab. "Waiting for you" (today's Needs you, across all Macs) first, "Earlier" below. Seen rows lose the dot; answered ones move to Earlier. iPad sidebar keeps Needs you and Inbox rows.
- **Search in context** (replaced the Search tab 2026-11-20; the user: «серч плохо сделан, мне не нравится. надо чтобы серч был привязан к контексту где мы находимся, отдельный tab search плохо»): Agents, Backlog and Wiki each search their own list with the Backlog's stock `.searchable` (under the large title, hides on scroll, filters in place); More has none; pages keep ⌘F; ⇧⌘F focuses the current tab's field. The Search tab rules below are history.
- ~~**Search tab**~~: scope = current workspace. Groups in order Sessions · Tasks · Wiki pages · Docs (`docs.list`: docs this workspace's sessions opened), 3 each then "Show all n". A result pushes on the Search tab's own stack with its section line; Back returns to the results, query kept. Empty field: Recent searches (5), Recently opened (5). No results: "Nothing in shop for "x"". The field's own mic is dictation only.
- **Mic**: Voice is a bar button on tab roots and pages and the first composer button in sessions. Nothing floats over content (the floating `VoiceButton` goes).
- **Top bar trailing**: tab root = Inbox · Voice · the section's create action. Pages = up to two actions (e.g. Edit) · Voice · ••• (History inside). Sessions: mic in the composer.
- **Tab bar hidden** in a session, on anything pushed above a session, and in reading mode.

## iPhone measurements (402 × 874 pt)

| Element | Value |
|---|---|
| Navigation bar | 44 pt at y 59 (safe area 62) |
| Large title + subtitle | 34/41 + system subtitle (pages only; tab roots have none); folds into the bar over the first 52 pt of scroll |
| Tab bar | capsule 62 pt high, 20 pt side inset, 22 pt from the bottom → 84 pt bottom chrome; Search circle 62 pt, 12 pt gap |
| Tab bar minimized | two 54-pt circles, 20 pt inset, 26 pt from the bottom |
| Tab badge | none (round 3) |
| Back chip | glass capsule 44 pt, padding 8 / 14, ≈ 150 pt, chevron 20 pt; label ≤ 12 characters including "…" |
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
| Reading view | 1210, no bars, text 780 centred (round 5) | 834, no bars, text 778 (round 5) |
| Bands | tab bar 48 pt at y 26 (band 1 = 24–74); column bars 44 pt at y 80 (band 2); column bars at y 26 when the sidebar shows | same |
| Default | Agents, Backlog, Wiki open as list + detail; each section remembers its last state | list + detail while nothing is open; detail only once something opens |

- The list column belongs to the tab: Agents list, Backlog list, Wiki tree, More list. A push never changes it; selection in it follows the detail.
- Wiki tree + page in portrait: tree 320 · page 514 (text 434).
- Document measure in regular width: min(780, detail − 56) pt, with the wiki margin column from a 1116-pt detail (round 5; was 680).
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
| ⇧⌘F | the current tab's search field focused (Agents, Backlog, Wiki) |
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
| Sections | `TabView` + `Tab` × 4 | which tabs show (Wiki only with the wiki plugin), per-workspace stack restore |
| Tab bar shrinking on scroll | `.tabBarMinimizeBehavior(.onScrollDown)` | — |
| iPad sidebar / tab bar | `.tabViewStyle(.sidebarAdaptable)`, sidebar toggle | Needs you, Inbox, workspaces rows under the tabs; ⌃⌘S step-down, remembered per section |
| Columns | `NavigationSplitView` with `columnVisibility` incl. `.detailOnly` | the step order and defaults per orientation |
| Large title folding into the bar, glass bars | `navigationTitle` + large display mode, system glass bar, `.scrollEdgeEffectStyle(.soft, for: .top)` (already used in `SessionDetailView.swift:274`) | the SDUI `header` node moving into the scroll so it can fold |
| Search | `.searchable` on each tab's list | what each tab asks the Mac |
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

## Round 3 (after V1)

Claude Design, 20 Nov, same canvas, page r3, same chat. Input: the V1 shots (`clubmate-notes/checks/2026-11-20-device-ux-v1/shots/`, iPhone 18 Pro and iPad Pro 11" portrait, light). Boards: `R3-Rulings`, `R3-iPhone-changes`, `R3-iPad-changes` (`.dc.html` here, first in `boards.html`). Stock first holds: every ruling uses the system `TabView`, `Menu`, navigation bar and empty state.

| # | Question | Ruling | Stock way | Goes to |
|---|---|---|---|---|
| 1 | Tab root title ("shop" twice, "shop ⌄ shop" scrolled) | The section name ("Agents", "Backlog", "Wiki", "More") is the large title, with no subtitle, on iPhone tab roots and in the iPad list column. Workspace and Mac show only in the capsule; the Mac state shows only when the Mac is not online (C3 offline row). Scrolled bar: "● shop ⌄ · Agents". | `.navigationTitle(section)`, no `.navigationSubtitle` on roots | fix after V1; iPad list column in T13 |
| 2 | Wiki subtitle 11 pt, text 31 pt below it | Keep the system subtitle at its own size. The text starts 14 pt below it (the document node's top padding, not the bar) → 188 pt (21.5%) at rest. "15/20 pt" in § iPhone measurements becomes "system". | `.navigationSubtitle` | fix after V1 |
| 3 | Reading mode: text under the status bar | Text must not show under the status bar. While reading mode hides the bars, the top scroll edge uses the hard style; the 2-pt progress line sits on its lower edge. Board W6 stays. | `.scrollEdgeEffectStyle(.hard, for: .top)` (on the WKWebView's scroll view: UIKit `topEdgeEffect.style = .hard`) | fix after V1 |
| 4 | Back chip fills 200 pt; no room for the title | Label ≤ 12 characters including "…", system 17 pt (≈ 150 pt with the glass). Voice stays on every page (L6 is right; r2 navigation frame 2 was missing it). The title "#7" and the section line "Backlog · shop" then fit. | toolbar `Button` with a label; cut the string, not the font | fix after V1 |
| 5 | Capsule menu: no trailing counts in the system Menu | Accept the build: "n waiting" as the row subtitle, a Mac Details row per Mac, Pair a Mac last. r2 W2 is superseded. | `Menu` with a `Section` per Mac | accepted, no change |
| 6 | More order | Accept the build's order: plugin rows first, This Mac last. Add the usage line as This Mac's subtitle ("Online · usage 42% of the limit"). | list row with a subtitle | fix after V1 (usage line) |
| 7 | Tab badge: system badge is red, cannot be tinted | No tab badge on either device: red means failed in the companion language. The amber Inbox dot and the amber count in the session bar carry "needs you". | no `.badge` on `Tab` | fix after V1 (remove the badge) |
| 8 | Inbox: large or inline title | Accept the inline title: Inbox is a pushed page like #7 and a session, and inline saves 52 pt. | inline display mode | accepted, no change |
| 9 | iPad Wiki detail when nothing is open | Wiki never shows an empty detail: it opens the last page read, the first time the wiki home (the tree's root page); the tree selection follows. Agents and Backlog never open something by themselves: stock empty state with one action — Agents "Open the oldest waiting" when something waits, else "New session"; Backlog "New task". | restored split-view selection; `ContentUnavailableView` | T13 (wiki home: plugin marks its home page, handoff (c)) |
| 10 | iPad portrait: column bar below the tab bar (54 pt at y 106) | Keep band 2 below the tab bar as built: no stock placement puts items beside the iPadOS tab bar. Remove our own sidebar toggle from band 2; the tab bar's system sidebar button is the only one (⌃⌘S stays). This replaces the band-1 layout on r2 L2–L6 and P1–P6. | `sidebarAdaptable`'s own sidebar button | T13 |

Rules that change:

- § iPhone measurements: large title "34/41 + 15/20 pt" → "34/41 + system subtitle (pages only; tab roots have none)"; tab badge → none; back chip label ≤ 12 characters, ≈ 150 pt.
- § iPad columns, Bands: band 2 is always under the tab bar (54 pt at y 106); the "verify on a device" note is closed.
- § Structure, Mac details: More › This Mac is the last row, with the usage line as its subtitle.
- § Language changes: no tab badge; the header line under a tab root's title goes.

Seen by the designer, not asked: in the capsule-menu and inbox shots the tab bar shows Agents · Backlog · More without Wiki while the workspace is still shop. The tab set may change only when the workspace changes; this looks like the wiki plugin's state reloading (a bug, not a ruling; backlog #1012).

Not done in round 3 (backlog #1013): two note lines on `R3-Rulings` ("Seen in the shots") and `R3-iPad-changes` ("Rulings shown") still touch the right edge of their board in the headless render; the frames are not cut.

## Round 4 (iPad, after the user's notes)

Claude Design, 20 Nov, same canvas, page r4, same chat. Input: the user's notes and screenshots from their own 13-inch iPad on build `614712fb0` (`clubmate-notes/checks/2026-11-20-ipad-notes/`, notes N1–N7 and mark A), and simulator shots of the same build on iPad Pro 11" (M5), landscape and portrait, light and dark. Boards: `R4-Rulings`, `R4-iPad-left-column`, `R4-iPad-landscape`, `R4-iPad-portrait`, `R4-iPad-13inch` (`.dc.html` here, first in `boards.html`). The user's notes win over earlier rulings. The iPhone does not change: the new layout applies only when `idiom == .pad` and the horizontal size class is regular; iPhone (also Pro Max landscape) and iPad compact width (Slide Over) keep the iPhone layout. The reference for every iPad screen is the iPhone Agents and Backlog tabs (memory: Backlog is the reference design).

### Rulings on the user's notes

| # | User's note | Ruling | Stock way (iPadOS 27) | No stock API · cost |
|---|---|---|---|---|
| N1 | "что сверху?" — empty strip above the capsule and Inbox | No band: the list column's bar is the first thing under the status bar (y 24). Hiding the tab bar (N2) removes it. | — | — |
| N2 | "табы хочу там как на телефоне"; the top tab bar "надо чтобы можно было свернуть"; a Mac shows as a tab | The sections are icon tabs at the bottom of the list column, as on the iPhone. The top tab bar is hidden, not folded. Workspaces leave the tab set; they are only in the capsule menu. | `TabView(selection:)` kept for the per-tab state; on iPad regular `.tabViewStyle(.tabBarOnly)` + `.toolbarVisibility(.hidden, for: .tabBar)`; the tabs are a `ToolbarItemGroup(placement: .bottomBar)` in each list root, icon + label buttons bound to the selection, `ToolbarSpacer(.fixed)` before Search; the workspace `TabSection` goes | iPadOS has no bottom tab bar, so these are toolbar buttons. Costs: no tab customization; ⌘1–⌘4 added by hand (`.keyboardShortcut`); VoiceOver needs `.isSelected` and a "tab" hint; the tabs leave with the list when it collapses (⌘1–⌘4 still switch) |
| N3 | Wiki: "Откуда и зачем такие отступы?" | Title, meta line and body share one 680-pt measure, centred in the detail; no large-title band; tree rows are one line, cut at the end. | detail `.navigationBarTitleDisplayMode(.inline)`; tree rows `.lineLimit(1)`; list `.searchable` | the title and meta move into the document (handoff (b)): the system large title cannot follow a centred measure |
| N4 | "слишком много места этот хедер занимает"; code line cut | A scrolled page has one 50-pt bar (inline title + actions): 74 pt (9%) above the text, was ≈ 150. Scrolling on enters reading mode. Code blocks scroll sideways inside their box with a right fade. iPad only. | inline title shows once the document's H1 passes under the bar; `.toolbarVisibility(.hidden, for: .navigationBar)` in reading mode | the H1-under-bar trigger is ours (web view scroll offset); code scroll is document CSS (b) |
| N5 | Backlog: "Цвета? Почему отличается от всего остального?" | One list style for every list column = the iPhone Agents list: white grouped cards on the system grouped background, system selection tinted Mate green, 360 pt wide on every tab. | `.listStyle(.insetGrouped)`, `List(selection:)`, `.tint`, `.navigationSplitViewColumnWidth(360)` | — |
| N6 | "Надо чтобы эта сайд панелька сворачивалась" — "По кнопке" | A visible button collapses the list column: two states, list + detail ↔ detail only. The button is at the leading edge of the bar in both states; ⌃⌘S does the same. The reading view sits on top and restores the state it came from. | `NavigationSplitView(columnVisibility:)` and its own sidebar toggle: keep it, do not remove it | — |
| N7 | Title cut to "A…"; rows show through under the top area | No title in the iPad list column: the selected tab names the section. The list is the stack's root scroll view, so the bar gets the system scroll edge and rows never show through. | `.toolbar(removing: .title)`; List as the root scroll view | — |
| A | "31 ›" on a session row — "фигня лишняя" (not confirmed by the user) | Yes, remove on iPad: rows of a selection list get no count and no chevron. The iPhone keeps its chevrons (push navigation). | `List(selection:)` with tags, not `NavigationLink` rows | — |

### Audit of build 614712fb0 against the iPad boards

| # | Finding | Breaks | Fixed by |
|---|---|---|---|
| 1 | Empty band ≈ 60 pt above the list column's bar (behind the floating tab bar) | R3 ruling 10, R2 L2 band 2 | N1/N2 |
| 2 | Workspaces show as tabs: "MacBook Pro (3)", "Mac Studio" (the sidebar-only `TabSection` leaks into the tab bar) | R2 L2 tab set | N2 |
| 3 | No visible control collapses the list column | R2 L1–L3 toggle | N6 |
| 4 | ⌃⌘S: first press does nothing; order list + detail → detail → list + detail; all columns only from the tab bar's button | R2 L1–L3 step order | N6 (two states; #1016 works on the step today) |
| 5 | Reading view from all columns keeps the TabView sidebar; the transcript stays narrow | R2 L4 | N6 |
| 6 | Portrait reading view leaves the sidebar open over a dimmed page | R2 P4 | N6 |
| 7 | Agents list column has no "Agents" title at rest | R3 ruling 1 (iPad part, now replaced) | N7 |
| 8 | Folded title cut to "A…"; rows scroll under the bar, "3 open" overlaps the section header | R2 L1 | N7 |
| 9 | Backlog column darker grey, grey grouped rows, pale green selection, ≈ 415 pt (others ≈ 363) | R2 widths, R3 P-c | N5 |
| 10 | Session rows show a count and a chevron | R2 L1 (state line only) | A |
| 11 | Wiki title at the column margin (x ≈ 340) and body centred (x ≈ 425); tall empty band above the title | R2 L5 | N3 |
| 12 | Wiki tree rows wrap to two lines or are cut | R2 L5 | N3 |
| 13 | Scrolled wiki page keeps tab bar + folded title + "updated … by …" (≈ 150 pt) | R2 L5 / W5 | N4 |
| 14 | Code line cut at the right edge, no scroll hint | R2 L5 | N4 |
| 15 | Wiki body blank at rest in landscape until scrolled (simulator, light run; the dark run drew it: intermittent) | R2 L5 | bug, no ruling |
| 16 | Session shows the old request banner row (`RequestBannerHost`) | R2 rules: amber count in the bar | not built yet (R2) |
| 17 | Question uses the old card ("Paused until you answer"), not peek / full / bar with the Other composer | R2 S3–S5, § Session screen | not built yet (R2) |
| 18 | Usage line ("7d 64%") under the capsule in the list column | R2 § Structure, Mac details | not built yet (R2) |
| 19 | "Sends after the current turn" footnote under the composer | R2 § Session screen | not built yet (R2) |
| 20 | Agents / Backlog empty states match R3 ruling 9 | — | accepted |
| 21 | Backlog list column bar sits 64 pt lower than on Agents (bar at y 150 vs 86 in landscape, light and dark): one more empty band (seen after the designer's audit, not shown to the designer) | R2 L1–L3 | N1, N5 |

### iPad measurements (replace § iPad columns)

| | 11" 1210 × 834 / 834 × 1210 | 13" 1376 × 1032 / 1032 × 1376 |
|---|---|---|
| List column | 360 on every tab | 360 on every tab |
| Landscape | list 360 + detail 850 (70%) · collapsed: detail 1210 | list 360 + detail 1016 (74%) · collapsed: detail 1376 |
| Portrait | detail 834; the list (360) slides over it (`.prominentDetail`), picking a row or the toggle or Esc closes it | list 360 + detail 672 side by side |
| Rule | the list sits beside the detail only when the detail keeps ≥ 640 pt | same |
| Bars | status 24 · column bar 50 · bottom icon tabs 56 pt, 20 pt from the bottom | same |
| Text measure | round 5: prose min(780, detail − 56), margin column from detail 1116 (was 680 centred) | same |
| Wiki scrolled chrome | 74 pt (9%) | 74 pt (7%) |

Keys change: ⌃⌘S toggles list + detail ↔ detail only (two states, both orientations); ⌘1–⌘4 are bound by hand on iPad since the tabs are toolbar buttons. `docs/shortcuts.md` § Companion on iPad changes in the commit that rebinds ⌃⌘S.

### What round 4 replaces

- `R2-iPad-landscape`, `R2-iPad-portrait`: the `sidebarAdaptable` top tab bar, bands 1 and 2, the 260-pt sidebar with Needs you, Inbox and workspaces, the three column states, list 360 / 320.
- § Decisions "iPad: `.tabViewStyle(.sidebarAdaptable)`"; § Structure, iPad bullet; § iPad columns; § Keys ⌃⌘S three steps; the iPad rows of § Stock SwiftUI.
- R3 ruling 10. R3 ruling 1 for the iPad list column (the iPhone part stands). R3 ruling 9 stands.
- On the iPad, Needs you is reached through Inbox ("Waiting for you") and workspaces through the capsule menu, as on the iPhone.
- Findings 16–19 are R2 rules the build has not done yet, not new rulings.

### Open

- For the user: the bottom tabs are drawn with 10-pt labels like the iPhone tab bar; the user said "icons", so the labels can go if the user prefers.
- For the builder (spike first): the designer believes a `.bottomBar` toolbar shows at the bottom of a split view's list column (as in Notes and Reminders) and that the system sidebar button sits at the leading edge of that column's bar. Confirm both on the simulator and on a 13-inch before building the rest.
- `R4-iPad-left-column` has an empty lower part in the headless render (board taller than its frames); nothing is cut.

## Round 5 (iPad text measure, 21 Nov)

Claude Design, 21 Nov, same canvas, page r5, same chat. Input: the user's question "Откуда на айпаде на вики по бокам столько отступов" (asked before as N3) and simulator shots of build `73511a0b5` on iPad Pro 11" and 13" (M5), landscape, wiki page Checkout flow, list shown and hidden. Today the text is 680 pt (+ 20-pt inset) centred: side margins ~65 (11", list shown), ~168 (13", list shown), ~245 (11", list hidden), ~330 (13", list hidden). Boards: `R5-Rulings`, `R5-iPad-11-list-hidden`, `R5-iPad-13-list-hidden`, `R5-iPad-11-list-shown` (`.dc.html` here, first in `boards.html`; the round as one page: `rounds/round-5.html`).

Ruling: option C + D. The prose takes the Mac wiki's measure (88ch ≈ 780 pt at the 17-pt body); when the detail holds the prose and the Mac's margin column side by side, the column fills the right. A (680 always) keeps 245–330 pt of empty space on each side; B adds a breakpoint the Mac has not got; E gives ≈ 130-character lines; D alone keeps two measures for the same page on the Mac and the iPad.

### Rules

- **Prose** = min(780, detail − 56) pt; side gutter at least 28 pt. Title and meta sit on the prose's left edge, in the same column; headings follow the prose. Images: at most the prose width.
- **Margin column** when the detail is ≥ 1116 pt (28 + 780 + 40 + 240 + 28): 240 pt wide, 40-pt gap; prose + column = 1060 pt, centred as one block. The prose never shrinks to make room. The column's top aligns with the title; it scrolls with the page, "On this page" sticks 16 pt under the bar. Sections and data as on the Mac (`docs/wiki.md` § Margin column): On this page, In this folder, Stale, Changes, Linked from.
- **Below 1116**: no column; the one-row fold "On this page · n changes · stale · n links" under the meta, as on the Mac below 960; a tap opens it in place.
- **Code and tables** start at the prose's left edge. Without the column they may run wider, up to min(1060, detail − left margin − 28); with the column they stop at the prose's right edge. Past that, code scrolls inside its box (no wrap) with a 56-pt right fade and a scroll indicator; table cells wrap, a table still too wide scrolls.
- **Reading view** (⌃⌘F): no bars, no column, prose min(780, detail − 56) centred on the screen, code and tables up to 1060, fold row kept.
- **iPhone and iPad compact width**: no change (full width, 20-pt margins).

| Detail (pt) | Where | Prose | Margins L / R | Margin column | Fold row | Code / tables max |
|---|---|---|---|---|---|---|
| 514 | 11" portrait, list beside | 458 | 28 / 28 | — | yes | 458 |
| 672 | 13" portrait, list shown | 616 | 28 / 28 | — | yes | 616 |
| 834 | 11" portrait, list hidden | 778 | 28 / 28 | — | yes | 778 |
| 850 | 11" landscape, list shown | 780 | 35 / 35 | — | yes | 787 |
| 1016 | 13" landscape, list shown | 780 | 118 / 118 | — | yes | 870 |
| 1032 | 13" portrait, list hidden | 780 | 126 / 126 | — | yes | 878 |
| 1210 | 11" landscape, list hidden | 780 | 75 / 75 | 240, gap 40 | — | 780 |
| 1376 | 13" landscape, list hidden | 780 | 158 / 158 | 240, gap 40 | — | 780 |
| 1210 · 1376 | reading view | 780 | 215 / 298 | — | yes | 967 · 1050 |

### What round 5 replaces

- R4 N3 "Title, meta line and body share one 680-pt measure, centred in the detail" (the rest of N3 stands: inline bar title, one-line tree rows, `.searchable`).
- § iPad measurements, row "Text measure"; § iPad columns, row "Reading view" and the bullet "Document measure in regular width: 680 pt".
- The 680-pt measure in handoff (a) and (b) (`document` node).

### For the builder

- Today: `PadLayout.measure = 680` (`client/Companion/App/PadLayout.swift`), passed as `theme.column` in `client/Companion/SDUI/Content.swift` (`pageTheme`) and `client/Companion/Plugins/Docs/DocPageView.swift` (`theme`); `html.column #doc` in `client/ClubmateDocument/Sources/ClubmateDocument/Web/document.css` caps `#doc` at column + 2 × inset and hides `.wiki-margin` (it shows only under `html:not(.column)` from 960 px, 200 px wide).
- The wiki document already carries `<aside class="wiki-margin">` and `<details class="wiki-fold">` for every client (`plugins/wiki/margin.go`), so the column and the fold are document CSS work (b), no plugin change. The iPad column is 240 pt, the Mac's stays 200 px.
- Doc pages (docs viewer) have no margin column: they take the prose rule only.
- The Mac and the iPad then share one prose measure (88ch ≈ 780 pt).
