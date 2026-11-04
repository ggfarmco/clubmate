# Clubmate — decisions left in prose: design brief

Paste this into the Claude Design project that holds the Clubmate boards (`design/language`, `design/companion`). The screenshots are already in the project's assets; read each one like an attached image:

| Screen | Mac 1280 (dark) | Mac 800 (light) | Phone dark | Phone light |
|---|---|---|---|---|
| Question pending | `/_blob/6b44dd8313ff6024b51bacaeb67058c1` | `/_blob/f8a5206e9434589ceed843ce64d4b8ff` | `/_blob/280b4ca6002e660be71011f15e392983` | `/_blob/d30eaafea7c1504ce010ba9c36a97044` |
| Question, scrolled to the 2nd | — | — | `/_blob/e6e20b93ea15af2dfb5d7aed76c18a9a` | `/_blob/faa1d17f3a7cf5f41d91dbead5f99d92` |
| Report with a buried decision | `/_blob/759bfe4f7a6143774bd92c3651db7675` | `/_blob/0b2850992b82ca4af721a8dce16ebb6d` | `/_blob/0115c1c048cbc33e3218ceae2dfd9aea` | `/_blob/070ba48e34d3c08008188c2c8c2ae055` |
| Needs you list | `/_blob/7f346c56c04fd2a35f844d61080d20e5` | `/_blob/24f1b2070b51bd9faa961daf2f94b859` | `/_blob/3b74ccff281c441dc15be33992ff2d17` | `/_blob/f14babad4b70df81d19ed210d00c16da` |

The same files are in `screens/mac/` and `screens/phone/` in the repo; each folder's `notes.md` has the capture setup.

## 1. The problem

Agents often hand a decision to the user without asking it. The decision sits in a sentence of the final report, and the user skims past it. A phrase search over the last two weeks of the user's sessions found about 200 such turn endings (shares are rough):

| Kind | Share | Real example |
|---|---|---|
| A real decision hidden in the report | ~25% | "Fixing it means changing that rule for all lists, or adding a new SDUI prop, so it's your call (#185)." · "#132: a session waiting for your review sits under Done. Should it be Done, Needs you, or its own group?" · "Question for you: should it be fixed, or the recipe updated?" |
| An offer at the end | ~40% | "If you want, I can also add a ⌘/ shortcuts screen." · "If you want a grouped text instead of the raw list, I'll replace the release notes." |
| Leftovers ("Left for you", "Not done") | ~30% | "Left for you to file in the backlog (I added no tasks myself): minors 2, 4, 5…" |

Leftovers already go to the backlog by a project rule. **This brief covers the first two kinds.**

The user reads short, structured output and skips long reports. A decision that is only text is a decision they never see.

## 2. Where the user leans

- The user leans toward this: at the end of a turn, every decision and offer still open becomes **a question card**, not prose. Decisions are questions with options. Offers are a multi-select ("which of these should I do?").
- This should cover decisions that don't block the task and anything still open when the task is done.
- The user isn't sure how it should look and asked for design first, so a card is a starting point, not a requirement. A queue of open decisions across all sessions is an equally valid direction.
- AFK endings are in scope. Today the AFK instruction ends with "list which questions are left for the user" in prose, which is the same problem.

## 3. What exists today (see the screenshots)

- **Question card** (`screens/*/question-pending-*`, `question-card-*`): the agent's AskUserQuestion. It holds 1–4 questions with 2–4 options each, single or multi-select, plus a free-text "Other". On the Mac, the plugin renders it in the transcript as an SDUI card with Send and Skip. On the phone, `ElicitationCard` renders it natively. Once answered, it folds to one muted line: "Answered · option A · B", "Skipped" or "Cancelled".
- **While a card is pending**, the session's status is `waiting`. It counts in **Needs you** in the Mac sidebar filter, the status bar ("N need you") and the phone's Needs you list. It also sends one push or Mac notification: "Claude has a question".
- **Report with a buried decision** (`report-buried-decision-*`): how it looks today. The decision is one sentence among many.
- **Child sessions**: a child's question goes to the session that started it, not to the user. The parent answers it, or asks the user itself.
- **AFK mode**: the agent asks nothing and lists its questions at the end of the report.

The captures also show three problems with today's card and status:
- A report with a buried decision reads as done ✓ on the Mac and "Review" on the phone. It is not in Needs you, because its last message doesn't end with `?`.
- With two or more questions, the Mac shows a single-choice question as a collapsed "—" popup, so its options and their descriptions are hidden. Multi-select options lose their descriptions, and every question gets an extra "Other" field.
- On the phone, the card's fields scroll inside a 300 pt cap, so at first only the second question's title shows.

## 4. Questions for you

1. **Blocking vs end-of-turn.** Today a question card means "the agent is stuck until you answer". An end-of-turn card means "the work is done; these are open". Should they look different, in the transcript, the sidebar row and on the phone? If yes, how?
2. **Decisions and offers in one card.** How do you mix "pick one of A/B" (a decision) with "tick what you want me to do next" (offers)? Show at least one card with 2 decisions + 3 offers.
3. **The report and the card.** The report stays as text above the card. Should the report point to the card, and should the card quote the report's context (e.g. the #185 detail)? How much context does each option need to be answerable without reading the report?
4. **Not answered.** If the user ignores the card, does the session stay in Needs you indefinitely? Should it be a softer state ("done, 3 open questions")? What do the sidebar row, the status bar count and the phone list show? The rule will live in the Go plugin (`needsYou`); propose the states and their labels.
5. **Many sessions.** Five sessions each finish with open questions. Is there a place to answer them in one pass (one queue across sessions), or does each one stay on its own session? Mac and phone.
6. **Skip and later.** What does Skip mean for an end-of-turn card: "no to all", "ask me later", "put in backlog"? Which of these should exist?
7. **Limits.** One card holds at most 4 questions. What if the agent has 7 open items?

## 5. Constraints

- **An AskUserQuestion keeps the turn open.** The session stays `waiting` until the user answers or skips; it is never `done` while a card is pending. So "the work is done, these are open" can't be built on today's card alone. There are two ways to build it:
  - (A) Reuse AskUserQuestion as the agent's last act. It blocks the session, shows in Needs you, and needs no new mechanism.
  - (B) Add a new "open decisions" record that the agent posts and the plugin owns, like a backlog task. The turn ends `done`, and the user answers later from the session or from a queue; the answer goes back to the session, or to the backlog when the session is gone.
  
  Your directions may use either, but name which one each uses.
- Keep the boards' design language (`Principles`, `Patterns`, `Components`; companion `C-*`). The theme comes from the user's Ghostty palette: colour roles, not hex values. Font is the terminal font at `uiSize`.
- The Mac card is an SDUI tree that the Go plugin builds from existing nodes (card, text, button, toggle, stack). The phone draws it natively. For each part of your design, say whether it is (a) a token/renderer change, (b) a new SDUI node or prop, (c) plugin content (words, grouping, order), or (d) new plugin logic: a new state, a new agent tool, or a rule change such as what counts toward Needs you. Prefer (a) and (c); say plainly what needs (d).
- Mac and phone show the same meaning. The plugin decides what a state means (needs you, waiting, counts); clients only render it.
- The card's data shape is fixed by Claude Code's AskUserQuestion: questions, each with a header chip (≤12 chars), options (label + description, optional preview), multi-select flag, free-text Other. Anything beyond that is ours to add around it.
- Keyboard first on the Mac: say which keys answer and send the card.

## 6. Deliverables

- 2–3 distinct directions for questions 1–5, each based on (A) or (B). For each: the Mac transcript at 1280 Mate dark and 640 Mate light, the Agents sidebar row, and the phone session screen with the Needs you list.
- Your recommendation, plus the answers to 6–7 in the chosen direction.
- A handoff list: every change tagged (a)/(b)/(c)/(d).

The screenshots were captured in Kanagawa Wave (dark, 1280) and Catppuccin Latte (light, 800 wide, the window's minimum), not Mate, and the phone on an iPhone 18 Pro simulator.
