# Gates — decided 16 Nov (direction A), landing point

A gate is point × match × graph, several per repo; the graph decides. The user picked A (`GT-Rules`): gates live under their repo in the dock row **Gates**, each named by its match with the point's glyph; the gate page lists the graph's steps. B (`rounds/GT-B-*`) is not chosen. Spec: `clubmate-notes/superpowers/specs/2026-11-16-gates-points-graphs-design.md`; plan: `clubmate-notes/superpowers/plans/2026-11-16-gates-land-graph.md`. All boards in `boards.html`.

Slice 1 built the **Landing** point; slice 2 (17 Nov) adds **Done** (see "Done slice" below). Tool call and the 7-day dry run under the match are drawn on the boards but come in later slices.

## Overrides of the boards

- **Landing words follow design C** (`design/features/land-review/rules.md`, ruled by the user 16 Nov): the lander reads `review…` / `review 2/2…` / `N changes` / `approved` / `asking`, never "held"; the Agents tree has no gate-run rows, except a node waiting on its own permission, under its lander as `Round n review` · `asks you`. `GT-A-Held-*`, `C-GT-A-Held-Light` and `C-GT-A-Agents` show "held" and a run under the lander: not built for landings.
- **Dock row detail for a landing gate** reads C's `N landing` (plain), not `reviewing` / `N held`.
- **Open run** on history rows waits for gate runs to have a page (gate runs are not listed with the workspace's runs); rows keep C's Open review.
- **Trust and run** re-pins the graph; a landing refused while the gate waited for trust runs when its agent calls `git_land` again (nothing records the refused landings yet, so the card says no count).
- **Holds** is a Landing · Done chip row (`tabs`) in New gate, plain text in Edit; Tool call waits for its slice.
- **Decided by** is a menu (graph and where it comes from); graphs that can't decide a landing are listed under it as "can't decide a landing" instead of greyed menu items, since the SDUI select has no disabled or grouped items.

## Names

| Thing | UI name |
|---|---|
| Dock row | **Gates** (was "Land gates"); + on the row and ⌘K "New gate…" |
| Point `land` | **Landing**, glyph merge |
| A gate | "Landing into ‹match›": "Landing into main", "Landing into the base", "Landing into release/*", "Landing into any branch"; under its repo in the dock only "into ‹match›" shows, with the Landing glyph |
| Sheet fields | Repo · **Holds** · **Into** · **Decided by** · **Rounds** |

## Match (Landing)

Into: the default branch (its name, e.g. `main`) · the worktree's base · any branch · Branches like… (a glob such as `release/*`). The base applies only to worktrees made from a branch. A change only in `.md` files lands without a review (Docs only). The most specific gate decides: exact branch › pattern › base › default › any; one run per landing. The sheet refuses an exact duplicate (same repo and into).

## Dock

Gates → repos → gates. Row detail by priority: `trust?` amber (graph changed; counts once in Needs you) › `N asking` (the card is on the lander) › `N landing` plain › `paused` muted › last verdict and age. The Gates row shows the highest of these across all gates, after its label, with the gate count staying in the count column (`design/features/dock-state`, 18 Nov). Up to 6 rows, then it scrolls.

## Sheet (`GT-Sheet-*`)

Repo · Holds (Landing; "An agent's landing waits for the graph.") · Into (presets, Branches like… with a glob field; a line under the base) · Decided by (the graph, Built in / Yours / In ‹repo›; its node chain under the field) · Rounds 1–5 ("rounds of changes, then you decide"). The footer says the gate in one sentence. ⌘↩ creates or saves, ⎋ cancels. Edit changes Into, Decided by, Rounds and Paused.

## Gate page (`GT-A-Gate-1280-Mate`, `GT-A-Trust-640-MateLight`)

- Header: Landing glyph, the gate's name, active/paused, last verdict; Pause P, Edit E, Delete ⌘⌫.
- Facts: Repo, Holds, Match, Graph (numbered node list: title, agent or step, model or command, the verdict node "approves or asks for changes"; then "ends approved · changes", the pinned version and Open graph), Rounds, Docs only.
- History: C's verdict rows (When, Verdict, Landing, Into, Open review) and C's live card for the landing under review; an approved row carries its summary under it in caption ink. A selected changes row opens its findings, deferred items last.
- Graph changed (`state: trust`): an amber trust card on top — "‹graph› changed: trust the new version?", "Pinned ‹a› · now ‹b› · N of M steps changed", each changed step with its kind and its − / + lines (a `diffLines` node: every − and + line shows, runs of unchanged lines fold into "N unchanged lines"), the unchanged steps in one line "‹steps›: unchanged", **Trust and run** ⌘↩, Open graph, Pause the gate P. The phone draws the same card without Open graph, and its Trust and run asks once (r-trust, below).
- `trust` never hides a landing that needs you or a paused gate: the header keeps both badges.

## Rounds used up (`GT-A-Rounds-640-MateLight`, `C-GT-A-Card`)

One card on the lander, counted once in Needs you: Land anyway ⌥1, One more round ⌥2, Stop ⌥3 (built in Task 6).

## Phone (`C-GT-A-Gates-Light`)

Gates screen grouped by repo, gate pages from the shared SDUI. Pause and Trust and run work on the phone; New and Edit stay on the Mac. A gate waiting for trust is the screen's top row: the amber needs-you glyph, its name and "Graph changed · N waits".

## Keys

| Key | Where | Does |
|---|---|---|
| + · ⌘K New gate… | Gates in the dock | open the New gate sheet |
| ⌘↩ · ⎋ | the sheet | create or save · cancel |
| P · E · ⌘⌫ | a gate page | pause · edit · delete (confirms) |
| ⌘↩ | a gate page with a trust card | Trust and run |
| ⌥1 · ⌥2 · ⌥3 | the rounds-used card | Land anyway · One more round · Stop |

## Handoff (landing slice)

- (a) Landing glyph (merge) on gate rows and the page header.
- (b) None: the sheet is built from existing nodes (select, text field, toggle).
- (c) Words, grouping by repo, the dock row's detail priority, the gate page's facts, node list and history with summaries.
- (d) Gate model (Tasks 2–6): match, most specific wins, pin and trust, summary into the commit body and the history.

## Done slice (17 Nov, plan `clubmate-notes/superpowers/plans/2026-11-17-gates-done-point.md`)

- Sheet (`GT-Sheet-*`): Holds Landing · Done; Done shows "An agent's turn that ends done waits for the graph.", Sessions as Top-level · Children checkboxes with "Only AFK sessions" as a separate checkbox below them (default Top-level + Only AFK) and the note "Holds done turns from the sessions you pick. Only AFK leaves sessions you're running yourself alone." (ruled by Claude Design 18 Nov, #843) and Wrote (Plans and specs, Any file, Nothing required, Files like… with a comma-separated glob field). No Into, no Docs only. The 7-day count under the match is not built.
- Dock and page: Done gates sit after the repo's landing gates, by name, with the speech-bubble glyph; the dock shows the match only and "N held" (accent). History columns When · Verdict · Held.
- Agents (overrides `GT-A-Held-*`): per the 17 Nov ruling a Done run has no rows in the tree, except a node waiting on its own permission (`Round n review · asks you`). The row reads `held` (accent); the header badge and the phone line name the match only ("held · plans and specs", "Held · plans and specs · Plan pipeline…"), not "Done · …". The held block lists rounds with Open review, not Open run (gate runs have no page yet).
- Card: the Mac draws the shared `request.land` card as a gate card (`gate`: the warning glyph, the caption on its own line, no folder row, as `GT-A-Rounds-640-MateLight`); its status-bar keys use each option's `short` word ("⌥1 accept · ⌥2 one more round · ⌥3 stop"; the multi-repo Land card "⌥3 leave" as `MR-Merge-640`). Both draw the plugin's question line (see the 18 Nov rulings). On the phone a held turn's transcript ends with the round line instead of "Working…".

## Rulings (Claude Design, 17 Nov, canvas "Clubmate · Gates")

- A held turn's header badge and phone line name only the match: "held · plans and specs", "Held · plans and specs · Plan pipeline…", never "held · Done · …"; the match already tells the point.
- A Done gate's dock detail is always "N held" (accent), also "1 held": every held Done turn is under review, so "reviewing" adds nothing. GT-Rules now reads "N held" for that step of the priority.

## Rulings (Claude Design, 18 Nov, canvas "Clubmate · Gates"; GT-Rules updated)

- **Only AFK sessions.** The Done sheet's Sessions picks Top-level · Children (toggles); a separate checkbox "Only AFK sessions" below them narrows them. The note under the field: "Holds done turns from the sessions you pick. Only AFK leaves sessions you're running yourself alone."
- **Tool-call message order** (Tool call slice). Changes on a held tool call: the gate's message ("From the gate · ‹gate› · round N · changes" with the findings) is queued first, then the call gets a plain refusal, so both reach Claude in the same turn and it never ends its turn on the bare refusal. The held block becomes "Refused by the gate · round N · changes" with the gate's message right under it, no turn break (GT-A-Refused-640-MateLight). Check in a test run that the queued message rides with the tool result.
- **Pause keeps holds.** Pausing a gate stops new holds only: what it already holds stays held until its review ends (approved runs it, changes refuse it), and a hold waiting for trust keeps waiting until the gate is trusted (on the Mac, the phone or the iPad) or the session stops. Pause on the phone never releases what a gate holds.
- **Question line on every rounds card** (#840 open item). The plugin sends "‹verb› ‹what› ‹as done / without another review›?" and the Mac and the phone draw it under the title: Done · plans and specs "Accept the plan as done?"; Done · Any file and Files like… "Accept the changes as done?"; Done · Nothing required "Accept the turn as done?"; Landing "Land ‹branch› without another review?" (see below).
- **Round line under a held turn on the phone.** In place of "Working…": the round line in caption ink, no spinner, updated in place, opening the gate run on tap, e.g. "Plan pipeline · round 1 changes · round 2 reviewing" (`heldRound`, sent by the plugin).
- **Holds chips are a segmented control** in form rows: the track in the hover colour, the selected item in the pressed colour on top, as GT-Sheet draws (`tabs` `segmented`). `tabs` elsewhere stay chips.
- **Rounds card title and question** (18 Nov, second round). Every rounds card: the title says what happened, the question says what ⌥1 does. The Landing card's title is like the Done card's, "‹graph› still asks for changes after N rounds", its question "Land ‹branch› without another review?"; the inbox line follows the title, and the card's notification reads "‹title› · ‹question›" on both rounds cards.
- **Done sheet sentence and Match fact.** The sheet's closing sentence reads "Holds ‹whose› done turn in ‹repo› when it wrote ‹what›; ‹graph› decides." and the gate page's Match fact "‹sessions› · wrote ‹what›". Top-level: "a top-level session's" / "top-level sessions"; Children: "a child session's" / "child sessions"; both: "any session's" / "all sessions"; with Only AFK: "a top-level AFK session's" / "top-level AFK sessions", "a child AFK session's" / "child AFK sessions", "any AFK session's" / "AFK sessions". Neither picked: no sentence, Create disabled, the note "Pick top-level, children or both." E.g. "Holds a top-level session's done turn in shop when it wrote a plan or a spec; Plan pipeline decides."

## Rulings (Claude Design, 18 Nov, canvas "Clubmate · Gates"): a turn whose gates can't be read

The user ruled 18 Nov (#849): a turn whose gates or repo can't be read at its end fails closed, with a way out. It stays held under the first Done gate of its repo for its kind (a repo with no such gate ends done), the plugin checks again after 5 s, 30 s and 2 min, and only then the card opens.

- While the checks run: the Mac row and header badge read `held · checking again` in the held row's accent (plain, not an attention badge), the phone line `Held · checking the gates again…`; neither names a match or a graph. It moves to Needs you only when the card opens after the third failure.
- Card: title "Couldn't check this turn against its gates"; question line "Accept as done without a review?" (sent as `question`, drawn under the title like every rounds card's); rule "When the turn ended, Clubmate couldn't read ‹the gates / ‹repo›'s repo›: ‹reason›. It tried 3 times. Check again tries now. Stop leaves the session idle and the turn not done." naming the actual cause when known, never explaining ⌥1; caption "held since 12:41 · tried 12:41, 12:42, 12:44".
- ⌥2 reads "Check again", not One more round: no review ran, it counts against no rounds, and once a check succeeds the turn goes through its gate as round 1.

## Rulings (Claude Design, 18 Nov, round r-trust; the user picked direction A)

Boards: `TR-Rules`, `C-TR-A-Gate-Light`, `C-TR-A-Confirm-Dark` (in `boards.html`); the rejected review sheet (B) is `rounds/C-TR-B-Sheet-Light`. Two gates on one call (`TR-Two-Gates`, also in `boards.html`) belongs to the Tool call slice.

- **Trust and run on the phone.** The Mac's trust card sits on top of the phone's gate page, as built: a one-line title, the versions line "Pinned ‹a› · now ‹b› · N of M steps changed"; each changed step's name "· ‹kind› · ‹what› changed" above its − / + lines in mono, no step number or box (added and removed steps say so and show their whole content); the unchanged steps fold into "‹steps›: unchanged"; the waiting line; **Trust and run** (primary, first) and **Pause the gate** (no confirm, leaves what's held held), wrapping; no Open graph.
- **Every changed line shows** (second ruling, 18 Nov). A − or + line never folds, however long the diff; only runs of unchanged context inside a step fold ("12 unchanged lines"), and the page scrolls.
- **The waiting line**, per point: "Until you trust it, the gate runs nothing. 1 done turn waits: ‹title›" / "1 landing waits: ‹branch› into ‹base›"; more than one "2 done turns wait: ‹first title› +1". The gate page header reads "graph changed ‹age› · N waiting for trust" on the Mac, the phone's "Graph changed · ‹repo›".
- **One confirm.** Trust and run on the phone asks once in a system alert: "Trust this version of ‹graph›?" / "What the gate holds is reviewed by it." / Cancel · Trust and run. Then the card goes and the held thing's round line reads "round 1 reviewing". The Mac's ⌘↩ keeps trusting at once.
- **Where you find it.** The Gates dock row ("N to trust", amber) and the Gates screen's top row (the gate's name and "Graph changed · N waits") show any gate whose graph changed. Only while the gate holds something does it also get a Needs you row, counted once however many things wait and listed only in Needs you (not again under a held session's worktree; graph run entries keep theirs), with one needs-input notification that goes when the gate is trusted or nothing waits; the held sessions stay under Running with "waits for trust" and don't count again. Each opens the gate page.
- **iPad.** The same card in the detail column.
- **SDUI.** A `diffLines` node (mono − / + lines, tinted; `fold` folds only runs of unchanged lines into "N unchanged lines") and a button `confirm` with `confirmMessage` (title, message; Cancel and the button's label).
- **Graph put back.** When the graph's file again matches the pin (put back), the gate is no longer waiting for trust: what waited resumes its review and the Needs you row goes, as after Trust (the held turn checks again every 30 s).
- **Every hashed change shows** (second ruling, 18 Nov). An added step says where it went ("‹step› · ‹kind› · added as step N"); a change in the steps' order reads "Order changed:" then the new order, the moved steps in full ink, with " · ‹id›" after a title two steps share.

## Rulings (Claude Design, 18 Nov, the trust entry; TR-Rules, GT-Rules, C-GT-A-Gates-Light redrawn)

- **Mac Needs you row.** One line like every tree row: the point's glyph in amber, the gate's match ("plans and specs"), then an amber "to trust" badge (the dock's attention form). At narrow widths the label truncates, the badge never. The waiting count is in the tooltip ("Graph changed · 1 waits", tree item `help`) and on the gate page, not the row.
- **Needs you only.** The trust entry is listed only in Needs you, not again under the held session's worktree group (the held session already reads "waits for trust"). Graph run entries keep their duplicate.
- **Phone row.** "Graph changed · 1 waits" with "Graph changed" in amber (no "trust it", no branch), the amber needs-you glyph (circle with "!"), not the shield, which means held with nothing to answer. The Gates screen's top row reads the same words with the same glyph.
- **Order changed.** A reordered graph reads "Order changed:" then the new order, the moved steps in full ink, the others in caption ink ("Simplify · **Lint** · Fable review · Summary").
- **Mac notification.** Source "‹repo› · Gates", title the gate's name, body "Graph changed · 1 waits".
- **One counting rule.** Only holds whose graph changed count, in the Needs you entry, the Gates top row, the gate header and the card's waiting line; a gate whose graph can't be read counts nothing and its top row reads "Graph can't be read".
- **As built, differs from the boards.** The "Order changed:" line is a `markdown` node: the moved steps are bold and the others in body ink, not caption ink (no node mixes inks in one line). The Gates screen's top row draws its whole detail in warning ink, not "· 1 waits" in caption ink (a tree item's detail has one tone).
