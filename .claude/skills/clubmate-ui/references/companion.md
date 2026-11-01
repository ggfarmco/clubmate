# Companion (iPhone, iPad, Fold)

Distilled from `design/companion/` (Claude Design, 2026-10-31). Boards: `C-Audit`, `C-Language`, `C-Components`, `C-Handoff`; screens `C-Macs`, `C-Workspace`, `C-Permission`, `C-Question`, `C-Inbox` (dark, `*-Light`), `C-iPad`, `C-Fold`. The Mac principles in `design-language.md` carry over; this file lists what is different on the phone.

## Colour

System surfaces and label levels (no Mate cream). Accent is Mate green: `#A6D85B` dark, `#357A27` light (white text passes 4.5:1). Accent = the one primary action, selection, unread dot. Success = teal (`#4FB3A0` / `#1F7F70`), diff additions only; done is muted grey. Warning amber = needs you (permission, question). Danger red = failed, stuck, Reject. Surfaces: background `systemBackground`/`systemGroupedBackground`, surface-1 `#1C1C1E`/`#FFFFFF` (groups, cards, glass base), surface-2 `#2C2C2E`/`#E9E9EE` (user bubble, plain buttons, chips).

## Metrics

- Touch: 44pt minimum hit area; capsule buttons 50pt (primary) / 44pt (secondary); ≥ 10pt between opposite choices.
- Type: Dynamic Type (Large Title 34, Title 3 20, Body 17, Subheadline 15, Footnote 13, Caption 12); SF Mono at body − 3 for commands, paths, code.
- Density: a row is a title (≤ 2 lines) plus one state line; no previews.
- Space and radius: 16pt margins, 22pt above a section; list groups radius 22, cards 20, code 16.
- Navigation: NavigationSplitView everywhere; header = back · title · one ••• menu; the header only navigates.
- Reach: everything that answers lives in the bottom 40% (docked request card + composer).
- Gestures: swipe only for harmless actions (mark read); never swipe to allow, reject or stop.
- Keys on iPad / Fold hardware keyboards: ⌥1 ⌥2 ⌥3 answer, ⌘⏎ send.
- Width breakpoints, not devices: compact < 600pt one stack; regular 600–1000pt two columns (sessions · session; Macs in a toolbar menu); wide > 1000pt three columns (Macs · sessions · session). Folding keeps the open session. A request arriving for another session shows as a banner at the top of the detail column, never a modal.

## Components

- **Session row**: 28pt status glyph, filled amber only when it needs you; title ≤ 2 lines; one state line that says what it wants ("Wants to run go test ./…", "Asks: …", "Running · 4 min", "Stopped · error", "Done · 1 h"); no pills, no ids; subagents nested under the parent, indented to the title, labelled "Subagent · AFK", folded by default behind a trailing "N ›" toggle on the parent (a parent with a waiting subagent says "N subagents waiting"); min height 52pt, the whole row is the hit area; Closed collapsed by default.
- **Status words** everywhere (rows, headers, notifications): Waiting for you · Asking you · Running · Stopped · Done · Closed. Header: state first, toned only when it needs you ("Asking you · subagent · AFK").
- **Transcript**: user = right bubble on surface-2 (not the accent); agent = flat markdown, code in a surface-1 mono block with horizontal scroll; thought = one collapsed caption row with duration; plan = one card, done = check + muted (no strikethrough), current = accent + semibold, next = secondary, a prose plan followed by a plan entry folds into the card; tool group = consecutive calls in one row, tap to expand, failures expand themselves; diff = three lines inline, "Full diff" opens a sheet. Content is inset below the glass header with a short edge fade.
- **Permission request**: docked above the composer while pending, then one muted transcript row ("Allowed once · go test ./... · 09:42"). Row 1: Allow once (filled accent, full width, 50pt). Row 2: Always allow… (plain) and Reject (plain, danger ink), 10pt apart. "Always allow…" opens a sheet with the rule it will store (e.g. `Bash(go test:*)`) and its scope; the confirm button is plain, Cancel is bold. reject_always is in the card's ••• ("Reject and don't ask again"), never a fourth button. The exact command in mono, the reason in one caption line.
- **Question**: docks like a permission (amber border), "Claude is asking", the question in Title 3, "Paused until you answer."; the composer placeholder becomes "Answer Claude…".
- **Composer**: [attach] [field] [send]. The right slot is always Send / Queue (disabled when empty), never Stop. While running: placeholder "Queue a message", caption "Sends after the current turn · Stop is in •••". Stop lives in ••• (iPad: header button) and asks once. Attachments as 56pt removable thumbnails above the field.
- **Macs screen**: a "Needs you" section on top across all Macs listing the actual requests; per-workspace rows say "2 waiting"; offline Macs dimmed with "offline since 18:20".
- **Inbox**: groups Waiting for you / Earlier; unread dot only on unseen actionable items; rows = kind glyph · title · time / body / workspace · session; permission rows show the command; done in muted grey.
- **Empty / offline**: empty states teach in one sentence and give one action ("No Macs yet — Open Clubmate on your Mac, then Settings › Devices › Pair a phone." + Scan pairing code). Offline Mac: data dims, "as of 18:20", answer buttons disabled with the reason; never act on a stale request.

## Data the companion needs (from the `remote` plugin)

Pending summary per session in the list payload (kind permission | question, short text, created-at); pending items across workspaces for "Needs you"; an explicit asking marker with the question text; the exact allow_always rule and scope; last-seen per Mac and a still-pending check before answering; a resolved event when a request is answered elsewhere.
