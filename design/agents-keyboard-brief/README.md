# Agents keyboard brief

Input for a Claude Design pass on the keyboard model of the Agents section (Mac app, and the same model on the iPad Companion with a hardware keyboard). 25 Nov 2026.

The user's words: "мне не хватает хоткиев сейчас … хочу на клавиатуре удобно переключаться между списком и транскриптом, хочу удобно переключаться по разным сессиям". They are not sure what the keys should be, so the designer proposes 2–3 directions and the user picks one.

- Canvas: https://claude.ai/artifact/HYecUiQ5K7t89v7x4MFshr ("Clubmate · Agents keyboard"). Page `today`: the four screens below on `Main`, plus the language boards `Agents-Mate-1280` and `Components` (cm.css copy `/_blob/f7b38d71…`). Round 1 goes on page `r1`.
- `prompt.md`: the message for the Design chat.
- `screens/`: real test-app shots (Mate light, 1280×800, fake agent, 7 sessions):
  - `agents-sidebar-focused.jpg`: the sessions tree has focus (green bar on the selected row, status bar "j/k · ↩ open · gc field").
  - `agents-composer-focused.jpg`: the composer has focus (green ring), the tree row is selected but idle.
  - `agents-transcript-permission.jpg`: a transcript with a thought, a plan, folded tool calls, an answer and a permission card (⌥1 ⌥2 ⌥3).
  - `agents-aside-beside.jpg`: an aside beside its parent (⌘] / ⌘[ between them), the aside composer has focus.
- `boards/round1.html`: the round card for the user (A/B/C side by side, each with its key table), built from the `boards/r1-*.dc.html` copies of canvas page `r1` with `scripts/design/boards-page/build.mjs` and `round-1.json`.

## Round 1 (Claude Design, 25 Nov)

Shared by all directions: ⌃⌘1–9 open the N-th visible session (⌃⌘↓ order), ⌃⌘J the next session that needs you; every jump lands in the prompt.

- **A · Ring**: ⌃⇥ / ⌃⇧⇥ step through the panes (tree → transcript → composer → aside). Esc still stops the turn.
- **B · Modes**: Esc steps out (composer → transcript → tree → composer) and never stops; ⌘. stops the turn from any pane. The tree's j/k, h/l, g… work in the transcript; g1–g9, gl (last), gn (next that needs you). Status bar names the pane: TREE · READ · TYPE.
- **C · Switcher**: quick ⌃⇥ = last session, held ⌃ + ⇥ = most-recent-first list; ⌥⌘ + arrows move between panes; ⌃⌘1–9 open pinned sessions (p pins).
- **Designer's pick**: B, with C's ⌃⇥ for the last session. Details: `boards/r1-Handoff.dc.html`.

Open for the user: ship the Esc change with a "⌘. stops" hint (first 20 presses) or a setting "Esc stops the turn"; should session numbers skip asides and graph steps; C's pins and held switcher in a later round.

The r1 handoff put "open in nvim" on ⌘O, which is the app's Open Folder…; round 2 fixed it. The iPad Companion was drawn without a screenshot of it.

## Round 2 (Claude Design, 25 Nov): B, final

The user's answers to round 1: direction B with ⌃⇥ for the last session; the Esc change ships with the "⌘. stops" hint, no setting; session numbers skip asides and graph steps. Their nuance: Esc in full screen mode takes the window out of full screen.

Measured in the test app before the round (which handler takes `ui key esc`; the invisible instance can't enter real full screen, so "reaches the window" is the case that ends it):

| Focus | Esc today |
|---|---|
| Tree | a key monitor takes it, focus goes to the composer |
| Composer, turn running or waiting | a monitor takes it and stops the turn (a pending permission is cancelled) |
| Composer, idle | the text view takes it, nothing happens |
| Composer, then another row picked by click | focus is lost, Esc reaches the window: leak (d) |
| After a click on transcript text or a card | the hosting view has focus, no Agents view takes Esc, it reaches the window: leak (e) |

Ruling (`boards/round2.html`, boards `boards/r2-*.dc.html`, final handoff `r2-Handoff.dc.html`):

- Every Agents pane (tree, transcript, composer, cards, aside, 640 drawer) takes every Esc, also where there is nothing to step out of; the status bar then says why ("No session open · ↩ open · ⌘N new"). A row click gives the tree key focus; a click in the transcript focuses that block. Full screen ends only with ⌃⌘F, Globe F or the green button. Other sections keep today's Esc.
- A way out that is not Esc: ⌥⌘← ↑ ↓ → go straight to a pane, also from the composer. ⌃[ is an Esc alias.
- ⌘O stays Open Folder…; "open in nvim" on a focused file block is gf. The diff card prints gf only while its block has the bar.
- The "⌘. stops" hint shows for 4 s, the first 20 times on a device that Esc leaves a running turn's prompt.

### Final key table (B)

Panes

| Key | Action | Where | iPad | vs today |
|---|---|---|---|---|
| Esc, ⌃[ | step out: composer → transcript → tree → composer; open things close first; always consumed, never ends full screen | all of Agents | Esc or ⌃[ | changed: today Esc stops the turn |
| ⌥⌘← ⌥⌘→ | tree / main pane; from the main pane → goes to the aside | all of Agents, composer too | same | new |
| ⌥⌘↑ ⌥⌘↓ | transcript / composer of the column you are in | main pane, aside | same | new |
| i a | transcript → composer, draft kept | transcript | same | new |
| ⌃H ⌃L | column left / right: tree ← main → aside | tree, transcript | same | unchanged in the tree |
| ↩ | open the row, focus the composer | tree | same | unchanged |
| ⌘. | stop the running turn; the Stop button prints it | anywhere | ⌘. (never read as Esc) | new |

Transcript and cards

| Key | Action | Where | iPad | vs today |
|---|---|---|---|---|
| j k ↓ ↑ | step through blocks: prompt, thought, plan, actions, answer, card | transcript | same | new |
| l → h ← | unfold / fold | transcript | same | new |
| ↩ | toggle fold; on a card, focus its first button | transcript | same | new |
| Space ⇧Space | page down / up | transcript | same | new |
| gg G | first / newest block (G follows new output again) | transcript | same | new |
| ⌘C | copy the focused block as Markdown | transcript | same | new |
| ⌘\ | aside about the selection, else the focused block, else the last answer | main pane | same | changed target |
| ⌘D | full diff of the focused edit, else the newest | main pane | same | changed target |
| gf | open the focused block's file in nvim | transcript, file block | only if the Mac can open it | new |
| 1 2 3 y n | answer a focused card, or any card with the prompt empty | transcript, composer | same | changed |
| ⌥1 … ⌥9 | answer the pending card | anywhere | ⌥1 … ⌥3 | unchanged |

Sessions and help

| Key | Action | Where | iPad | vs today |
|---|---|---|---|---|
| ⌃⌘1 … ⌃⌘9 | N-th session: top-level tasks and graph runs only, never asides or graph steps | anywhere | same | new |
| g1 … g9 | the same, bare | tree, transcript | same | new |
| ⌃⇥ / gl | last session ↔ this one | anywhere / tree, transcript | same | new |
| ⌃⌘J ⇧⌃⌘J / gn gN | next / previous session that needs you, oldest first | anywhere / tree, transcript | same | new |
| gt gi gc | tree · composer · search field | tree, transcript | same | new (gc exists) |
| ⌃⌘↓ ⌃⌘↑ | next / previous visible row, asides and steps included | anywhere | same (new on iPad) | unchanged |
| ⌘] ⌘[ | parent → aside / aside → parent | anywhere | ⌘] new, ⌘[ is Back | unchanged |
| ⌥↑ ⌥↓ | repo child of a multi-repo task, only when the prompt is one line or not focused | anywhere | same | changed |
| ? ⌘/ | keys of the focused pane | tree, transcript / anywhere | hold ⌘, ⌘/ | new |
| hold ⌃ ½ s | digits on the numbered rows until released | Agents | same | new |

Check before a build:

- ⌃⇥: the board scopes it to the whole app. As a menu key it would also take ⌃⇥ from terminals and nvim, and AppKit's own ⌃⇥ (leave a text field, used in the problem report sheet). Keep it to the Agents section.
- ⌘. already stops the graph run open in Agents (with a confirm). The meaning is the same, but say which one wins on a run page.
- ⌥⌘ + arrows against iPadOS window tiling (Globe ⌃ arrows) need a check on an iPad; the designer did not confirm it.

## Problems

1. **Pane focus.** Tree → main pane: Esc, ↩, ⌃L (sidebar left) / ⌃H (sidebar right). Main pane → tree: no key; Esc in the composer stops the running turn. The transcript has no keyboard navigation (no scroll, no step through messages); focus always lands in the composer.
2. **Switching sessions.** ⌃⌘↓/⌃⌘↑ next/previous visible row (wraps, focus to the prompt); ⌘[ / ⌘] parent ↔ aside; ⌥↑/⌥↓ repo children of a task; in the tree j/k, < / > filter chips, gc search field. No jump to the N-th session, no "last session" toggle, no "next session that needs you".
3. **⇧⌘1…6.** The user first asked for ⇧⌘1…6 inside Agents. ⇧⌘3/4/5 are macOS screenshots (the app never gets them; the user uses ⌃⇧⌘4), ⇧⌘6 is the Touch Bar shot. ⌥⌘1…9 and ⌃⌘1…9 are free in the app and in macOS. ⌘1…⌘9 switch rail sections. ⌥1…⌥9 answer cards (permission, question, land, graph step).

Source of truth for every key: `docs/shortcuts.md`. A board key that clashes with it is a question for the user, not a rebind (`clubmate-ui` skill).
