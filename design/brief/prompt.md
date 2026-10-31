# Clubmate — design review and design language brief

Paste this file into Claude Design together with `screens/*.png` and `clubmate-icon.svg`.

## 1. Your task

You are the product designer for Clubmate. Using the screenshots below:

1. **Audit** the current UI and UX: list concrete problems per screen, ranked by impact (readability, hierarchy, consistency, density, discoverability, broken layouts).
2. **Define the Clubmate design language**: a small, opinionated system that every plugin view can follow (principles, tokens, type scale, component specs, states).
3. **Redesign the key screens** with that language as hi-fi mockups at 1280×800 and 640×800: Agents (transcript + composer), Code with the chat panel, Backlog task detail, Cron/Triggers detail, Processes, Graphs run.
4. **Hand off**: for every change say whether it is (a) a theme token / renderer change in the native client, or (b) needs a new SDUI prop or node type, or (c) a plugin-side content change. Prefer (a), then (c); keep (b) rare and explicit.

Keep the information architecture (rail → sidebar → main) unless you show a strong reason. Do not turn it into a web app look; it is a native macOS app.

## 2. What Clubmate is

A native macOS IDE built around the user's own Neovim, real terminals and coding agents (Claude Code over ACP). A Go core owns all state; features are plugins that render UI as data (SDUI); a thin SwiftUI app draws it and hosts Ghostty terminal surfaces.

Users: developers who live in the terminal and run several Claude agents in parallel across git worktrees. They value speed, keyboard control and density; they dislike chrome, padding and mouse-only flows.

Brand: the name is Club-Mate (the yerba-mate soda) — the icon is a mate gourd with a bombilla on a green gradient (`#58b866 → #2f8a45 → #11482a`, see `clubmate-icon.svg`). The brand green appears nowhere in the app UI today.

## 3. Hard constraints

- **Theme comes from the user's Ghostty terminal theme.** Background, foreground and the 16-colour palette are whatever the user picked (any dark or light theme). The app derives `accent / success / warning / danger` from that palette. Your language must work for *any* palette: define colour **roles** and opacity steps, not fixed hex values. Show it on at least one dark and one light theme.
- **Font is the user's terminal font** (monospace by default) at an interface size `uiSize` (default 12pt, user-adjustable). Every size must be expressed relative to `uiSize`.
- **SDUI**: plugins send trees of a fixed node catalog; the client renders each node type one way everywhere. A component spec = one renderer, used by every plugin. Node types: `stack split scroll tabs list tree table sparkline text(body|muted|title|mono) badge(tone) icon diff markdown button textField folderField toggle select form surface empty transcript message(user|agent|thought) plan toolCard permissionCard composer`. Tones: `neutral accent success warning danger`. Tree/list items: `label, detail, icon, badge, tone, muted, menu`.
- **Width is scarce**: the window is often half a screen (640pt) next to another app; sidebars ~220–260pt.
- **Keyboard-first**: every action reachable by keys; vim-style tree navigation (j/k/h/l), fuzzy search in long lists, ⌘K palette. Focus and selection must be clearly visible.
- **Icons**: SF Symbols.
- Native popovers, menus, sheets and the Settings window use AppKit/SwiftUI system styling today.

## 4. Current tokens (`client/Clubmate/Theme/Theme.swift`)

| Token | Value |
|---|---|
| colours | `background`, `foreground`, `accent`, `success`, `warning`, `danger` from the terminal theme; `muted` = foreground 55%; `border` = foreground 10% |
| fonts | `ui` = uiSize; `uiSmall` = uiSize − 1; `title` = uiSize + 1 semibold; `glyph` = uiSize − 4 semibold |
| sizes | `rowHeight` = round(uiSize × 5/3) (20pt at 12); `gutter` 8; `indent` 12; rail 36pt wide |
| spacing | xs 2, s 4, m 6, l 8, overlay 48; capsule 3/10, chip 2/7, badge 1/6, field 0/6 insets |
| selected row | foreground 6% background + 2pt accent leading bar |
| fallback theme | Kanagawa: bg `#1F1F28`, fg `#DCD7BA`, accent `#7E9CD8`, success `#98BB6C`, warning `#E6C384`, danger `#E46876` |

Stated intent so far: "compact, Zed/Ghostty feel; collapsed by default, expand on click; rarely changed settings behind one menu; keyboard first."

## 5. Screenshots

All at 2× from an invisible test instance with seeded data (fake agent, sample backlog/cron/triggers). The window chrome (titlebar colour) differs between shots because of how the test capture draws inactive windows — judge it, but don't treat light-vs-dark titlebar as intentional.

| File | Shows | What I see |
|---|---|---|
| `01-agents-transcript.png` | Agents section: sessions tree, transcript with thinking, plan card, grouped tool actions with an expanded edit diff, agent markdown reply, permission card, composer | Plan and diff read well. Sidebar titles truncate at ~16 chars; "Needs you" repeats sessions also listed below; worktree row shows a useless `/Users/…`. Permission buttons are tiny chips. Composer footer mixes model, mode menu, options, attach, hint text, Stop and a disabled-looking Queue. |
| `02-agents-narrow-640.png` | Same at 640pt | **Bug**: header title wraps and overlaps "Thinking"; header meta wraps into two lines; permission card buttons cut off by the composer. |
| `03-code-editor.png` | Code section: Neovim in a terminal surface | The editor is the user's nvim (not designable); only the app chrome around it is. Status bar at the bottom duplicates nvim's own statusline. |
| `04-code-chat-panel.png` | Code with the agent chat panel on the right | Session picker and title repeat the same name; title wraps; empty transcript is just blank space. |
| `05-terminals.png` | Terminals list + Ghostty terminal | "running" detail repeated on every row adds noise. |
| `06-backlog-task.png` | Backlog tree (Todo / Idea / Not now) + task editor | Editor is raw: title field is a plain box, "Save" is plain text, Status/Priority selects are low contrast, actions (Start, Done, Delete) look like text. Sidebar titles truncated hard while priority badges take width. |
| `07-processes.png` | Process table with history sparklines and totals chart | Table is decent. COMMAND truncates the tail (the useful part); big empty gap above the charts; toolbar mixes a destructive button, a toggle and stats. |
| `08-cron.png` | Cron jobs + job detail | Detail is a plain text dump with raw JSON args; no hierarchy, no visible actions (pause/run live in context menus). |
| `09-triggers.png` | Triggers + trigger detail | Same plain-text dump; graph-generated triggers have cryptic names (`review-loop gr_a4841f…`). |
| `10-graphs-run.png` | Graph run: nodes, edges, statuses | Meta pushed to the far right edge, far from the label; internal ids (`gr_…`) shown to the user. |
| `11-graphs-empty.png` | Empty state | Instruction text floats mid-sidebar; main says only "Select a graph run". |
| `12-command-palette.png` | ⌘K palette | Good base; right column mixes command ids and shortcuts. |
| `13-worktree-picker.png` | Worktree/branch picker from the window title | OK; selected vs current (✓) could be clearer. |
| `14-workspace-switcher.png` | ⌃⌘W workspace grid | Thumbnail cards; fine. |
| `15-settings.png` | Settings window | Stock macOS form in system font — a different visual language from the main window. |
| `16-approval-sheet.png` | Approval sheet when an agent wants to run a script (stress fixture) | Stock NSAlert; "Allow" is a red pill, "Deny" grey. |

Not captured (the capture can't draw popover material): the **notifications inbox** popover from the bell in the title bar — a list grouped "Waiting for you" / "History", each row: kind icon (permission ✋, needs-input 💬, stuck ⚠, done ✓) · title · body · `workspace · source` · relative time · unread dot; header with an "All ⌄" filter and "Mark All Read".

Cross-screen notes:
- Two fonts fight: title bar pills, Settings and alerts use SF; everything else uses the monospace terminal font.
- Title bar pills (workspace ▸ worktree, bell with red count, window count) are white system capsules on a dark themed window.
- Details panes (Cron, Triggers, Backlog) have no shared "detail view" pattern: no header, no property list, no action bar.
- Status bar (`● connected  Agents`) carries little information.
- Empty states are one muted line.

## 6. What to deliver

1. **Audit**: a ranked table (screen · problem · why it matters · fix).
2. **Principles**: 4–6 short rules that decide future arguments (e.g. on density, colour use, when to use a card).
3. **Tokens**: colour roles mapped onto the existing tokens (surfaces/elevation levels as opacity steps over `background`/`foreground`, text levels, borders, focus ring, selection, hover, tone backgrounds); type scale relative to `uiSize`; spacing and radius scale; row heights. Note any new token and why.
4. **Components**: a spec per SDUI node type that appears in the shots (tree/list row, table, badge, button variants, textField/select/toggle, form, empty, message, plan, toolCard, permissionCard, composer, diff, markdown) plus the app chrome (rail, title bar pills, status bar, palette, popovers). Each with states: default, hover, selected, focused (keyboard), disabled, loading, error.
5. **Patterns**: a shared detail view (header · properties · actions), empty states, truncation rules (tail vs head vs middle for paths), how a 640pt window degrades.
6. **Mockups** of the six key screens at 1280×800 and 640×800, one dark and one light theme.
7. **Brand**: whether and where the mate green belongs inside the app (it must not fight the user's theme).
8. **Open questions** for me.
