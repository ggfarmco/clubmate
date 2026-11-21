# Root bar: native items — ruling

## Built (21 Nov 2026)

Only **New: `plus`** shipped, by the user's choice: it replaces `square.and.pencil` on every tab root (Agents was the only `square.and.pencil`; Backlog and Wiki were already `plus`). It overrides the `square.and.pencil` in the Top bar order row below.

The mic and part 2 were built and measured, then not shipped, because the perf gate needs the back move clearly better than main. iPhone 18 Pro sim, `nav.sh run … testBackButton testSwipeBackSlow testSwipeBackFast` + `nav.sh profile … testBackButton`, two rounds alternated with main; cells are dropped frames per move / worst frame:

| Move | main | B2 bottom accessory | B3 mic only | B3 + part 2 (all native) |
|---|---|---|---|---|
| Back button | 9.6 / 85 ms | 10.7 / 108 ms | 10.4 / 85 ms | 9.4 / 85 ms |
| Slow swipe back | 12.2 / 79 ms | 16.1 / 115 ms | 12.0 / 73 ms | 12.1 / 78 ms |
| Fast swipe back | 12.1 / 82 ms | 13.5 / 74 ms | 11.7 / 73 ms | 12.6 / 66 ms |
| Push | 22.3 / 240 ms | 26.6 / 284 ms | 22.6 / 231 ms | 23.1 / 231 ms |
| Main-thread ms per pop / push | 155–163 / 257–308 | 156 / 278–312 | 161 / 299 | 148 / 293 |

Noise between identical runs: about ±1 frame, ±15 ms per pop, ±40 ms per push. B2 made the back move worse even trimmed to one `Label`. All native equals main within noise. The B3 + part 2 patch and these notes: `clubmate-notes/superpowers/research/2026-11-21-companion-native-root-bar.{patch,md}`. Found while building: an inline `Picker` in the capsule menu drops the section headers and the row subtitles (use the Buttons fallback); `.glassProminent` draws no fill on a top-bar item on iOS 27; `tabViewBottomAccessory` does not hide with `toolbarVisibility(.hidden, for: .tabBar)` (gate it with `isEnabled`); an empty `navigationSubtitle` collapses with no gap.

Status 2026-11-21: **ruled by Claude Design** (round 1 + two follow-ups on the mic). It replaces the draft of the same day.

Canvas: https://claude.ai/artifact/XTurAnVyW6mnRwfLujcUkX ("Clubmate · Root bar", page Round 1 / r1). Chat: https://claude.ai/chat/7cb9a8c8-efef-41ea-935f-442d5fb76a33.
Boards (`boards/`, `.dc.html` + `.png`): `R1-Ruling` (handoff table, the source of truth), `R1-Q3b-Mic-bottom` (mic top vs bottom, B1–B4), `R1-Q1-Capsule`, `R1-Q2-Inbox`, `R1-Q3-Mic` and `R1-Q4-Order-iPad` (these two still draw the mic in the top bar = the B3 fallback; Q3b and R1-Ruling override them), `R1-Draft-native-bar` (our draft). Round CSS: `boards/r1-a.css`, `boards/r1-b.css`. User page: `round-1.html` (built from `round-1.json` with `scripts/design/boards-page/build.mjs`).

## Rulings

| Item | Ruling | SwiftUI API |
|---|---|---|
| Workspace capsule | Text label only (workspace name), system glass. The Mac dot goes (bar items draw monochrome). Chevron only if `Text("shop \(Image(systemName: "chevron.down"))")` measures native, else none. No `.buttonStyle(.glass)`. | `ToolbarItem(.topBarLeading) { Menu { … } label: { Text(ws.name) } }` |
| Capsule menu | Stock parts only: one `Section` per Mac with its state in the header; workspaces in an inline `Picker` (system checkmark), "n waiting" from `needsYou` as the row subtitle; offline Mac's rows disabled; Divider; Mac Details; Pair a Mac. The Mac identity colour stays on Mac Details only. | `Section(…) { Picker(…) { … }.pickerStyle(.inline) }` |
| Mac offline | Said once, in words, in the system subtitle under the large title on every tab root: "Mac Studio offline · since 18:20". Hollow dot goes. The in-list offline row stays only if it has an action. | `.navigationSubtitle(offlineLine ?? "")` |
| Inbox mark | `tray` → `tray.full.fill` when `needsYou > 0` or an Inbox row is unread. No badge (red = failure), no count in the bar, no amber on a plain glyph. Reserve: amber `.glassProminent` when `needsYou > 0`, only if testing shows people miss waits. | `Button("Inbox", systemImage: hasNews ? "tray.full.fill" : "tray")` + `.accessibilityValue` |
| Mic place + gesture | **Leaves the top bar (B2).** Tab roots: a voice bar in the tab view's bottom accessory — mic + "Talk to <workspace>" + caption; press and hold talks, release sends; a tap goes hands-free. Composer-less pushed pages (graph run, open queue): the same `VoiceAccessory` in a bottom safe-area inset as a full-width glass capsule; the floating 50 pt mic retires. Session composer mic unchanged. Not taken: B1 (search-role tab), B4 (floating mic on roots: needs a hand-made ~62 pt margin, collides with the minimized tab bar). | `TabView { … }.tabBarMinimizeBehavior(.onScrollDown).tabViewBottomAccessory { VoiceAccessory(ws:) }`; pushed: `.safeAreaInset(edge: .bottom) { VoiceAccessory(ws:).glassEffect(.regular, in: .capsule).padding(.horizontal, 16) }` |
| Listening | In the voice bar: Cancel · live Mate-green waveform · timer · green Send circle. Tab bar minimized (inline placement): waveform + Send only. Cancel plain, never red. Green in the bar means live, only that. | `@Environment(\.tabViewBottomAccessoryPlacement)`; one `DragGesture(minimumDistance: 0)`, < 0.25 s = tap, longer = hold |
| Mic offline | `.disabled` only; glyph stays `mic` (no `mic.slash`); the subtitle says why. | `.disabled(!mac.isOnline)` |
| Top bar order | Trailing: `[Inbox] · [New]`, two glass circles with a fixed spacer. New trailing-most. iPad identical over the list column; the sidebar toggle stays the system's. | `ToolbarItem(inbox)`, `ToolbarSpacer(.fixed)`, `ToolbarItem(Button("New", systemImage: "square.and.pencil"))` |

Colour roles: every resting item monochrome; Mate green only for listening; amber only in the reserve Inbox style; red never; caption ink for the offline subtitle. `needsYou` drives the mark, "n waiting" and the VoiceOver value; the client never recounts.

Must stay: capsule leading on every tab root, workspace name as its label; Inbox pushes the Inbox page; hold + tap voice in thumb reach on every root and composer-less page; iPhone and iPad the same; 44 pt targets; no custom view, overlay, gesture or custom-coloured shape in any **top-bar** item.
May change: chevron (only if native), menu order inside a section, offline wording, voice-bar caption and timer, spacer width.

## Perf gate

The whole root bar ≤ 10 ms per push **and** per pop over the no-items baseline (`scripts/companion-perf/nav.sh run <tag> 120 0 testBackButton testSwipeBackSlow`, `nav.sh profile <tag> testBackButton`), and the items move with the bar mid-swipe (RECORD=1, `frames.py`). Over budget, strip in this order: `accessibilityValue` → prominent style → chevron interpolation. The voice accessory is measured with the tab bar: over budget → trim it (idle = one `Label("Talk to <ws>", systemImage: "mic")` with the gesture, waveform view only while listening, inline drops the caption) → still over → **B3 fallback**: native top-bar mic, tap only, listening = `.glassProminent` accent `waveform` as `Menu(primaryAction:)` with Send · Cancel, group `[Inbox] · [Voice · New]`; hold-to-talk only in the composer.

## Check on device before merge

- An empty `navigationSubtitle` collapses with no gap.
- The bottom accessory hides with `toolbarVisibility(.hidden, for: .tabBar)`, renders on iPad (placement unknown), and fits the pop budget with the tab bar. #1121 (tab bar pops in at swipe end) also affects the voice bar.
- An inline `Picker` in the menu shows row subtitles (else Buttons with a checkmark on the current row).
- An amber/green tint shows on a bar item only with a prominent style.

## Changed versus the draft

- Mic: top-bar native button → bottom voice bar (`tabViewBottomAccessory`), hold-to-talk kept; floating mic on graph run / open queue retires into the same view. Draft line kept only as the B3 fallback.
- Inbox: `tray.full` → `tray.full.fill`; Inbox in its own glass.
- Mac offline: existing offline row → system subtitle on every root.
- Capsule menu: rebuilt from stock parts (Sections, inline Picker).
- Mic offline: `mic.slash` → same `mic`, disabled.
- Top bar group: Inbox · Voice · create → `[Inbox] · [New]`.
- Perf gate tightened to push and pop, plus moving with the bar mid-swipe.

`references/companion.md` § Workspace capsule / Inbox tray and `docs/` change with the build.
