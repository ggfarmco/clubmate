# Clubmate: land review sessions in the Agents sidebar

Hi. A small question about how one kind of session shows in the Agents sidebar. Today's screens are on the **Today** page of this canvas: board `Main` has three real screenshots of the Mac app, next to the `Agents-Mate-1280`, `Agents-640-MateLight` and phone `C-Workspace` boards. The design language is on the **Language** page. The user's verdict on today's look: "looks extremely poor".

## 1. What exists

**Land gate.** In a repo with a land gate, an agent that wants to land its branch into `main` (or a child landing into its parent's branch) first needs a review. Clubmate starts a fresh reviewer session (Fable model, read-only) and gives it the diff. The reviewer ends with a verdict: `approved` (the lander lands) or `changes` with findings (the lander fixes and asks again; the same reviewer reviews the next round). After the gate's rounds of changes (default 2), the user decides with a Land anyway / Stop card on the lander. After the landing the reviewer closes itself.

**What the user does with a reviewer.** Almost nothing. They may open it to read the findings, and very rarely answer its own permission request. It is bookkeeping for the lander, not a task of theirs.

**How it shows today:**
- The reviewer is an ordinary top-level session titled `Land review · ‹branch› → ‹into›` (or `Land review · the staged change → main`). Its row reads `Land review ·…` because the title is cut before the branch, then `Input only · 4 min` (its notification setting is "only when it needs input", always the same for a reviewer).
- Since today the row sits right after its lander's row in the lander's worktree group (screenshot 2). Before, each review made a branch group of its own (screenshot 1).
- The lander's own row already says how its landing stands, as a toned detail: `review…`, `changes`, `approved`; a Land anyway / Stop request makes it need you.
- Closed reviewers go to Closed like any session. On a busy day they are most of it (screenshot 3).
- The Gates section (its own section in the rail) lists each landing with its verdict history, findings and an "Open review" link to the reviewer.
- The phone draws the same session list natively (`C-Workspace`): a reviewer is a top-level session row there too.
- The reviewer's session page header: `Land review · feat/gates-t2-gate-match → fe…`, then `Input only · 16 min · 23% context`.

## 2. Questions for you

1. **While a review runs.** Does the reviewer get a row at all? Options we see: (a) its own row, as today; (b) nested under its lander like a subagent, with its own glyph; (c) no row: the lander's row carries the review state and opens the review (a chip, a disclosure, the session page). Pick and say why.
2. **After the verdict, before the landing** (the reviewer is done but still open, waiting for the next round or the landing). What is left on screen?
3. **Closed reviewers.** Out of Closed entirely (reachable from the lander and from Gates), folded into one row ("12 land reviews"), or kept as they are?
4. **Words and glyph.** What does the row (or chip) say: the branch, the verdict, the round ("round 2 of 2")? Does "Input only" belong on it? Is there a glyph for "a review of" (e.g. the Gates shield)?
5. **Needs you.** In the rare case the reviewer asks for a permission, where does that show, given your answer to 1?
6. **Session page.** When someone opens a reviewer, how does its header say whose review it is and what it decided, and link back to the lander?
7. **Phone.** Same as the Mac, or less?

## 3. Constraints

- Keep the design language (principles, patterns, components; Mate green accent, colour roles not hex). Badges only for attention. Compact: the sidebar is often 280 pt wide.
- The sidebar is a server-driven tree the Go plugin builds: each row has a label, a detail with a tone, an icon with a tone, badges, nested children, faint/muted flags and a context menu. Clients only render it. For each part of your answer, say whether it is (a) a token or renderer change, (b) a new SDUI node or prop, (c) plugin content (words, grouping, order, nesting), or (d) new plugin logic. Prefer (c).
- Data we have per reviewer: lander id and title, source branch, target branch, round, last verdict and findings, status (running / done / waiting for permission / closed), when it started.

## 4. Deliverables

- 2 or 3 directions for questions 1–3, on canvas page **Round 1**, at most 6 boards: the sidebar at 1280 Mate dark and 640 Mate light; a phone board only where it differs. Show a realistic busy state: main group with 4 user sessions, 2 of them landing (one reviewing, one with `changes`), and a Closed group where reviews outnumber the user's sessions.
- Your recommendation, with answers to 4–7.
- A handoff list: every change tagged (a)/(b)/(c)/(d).
