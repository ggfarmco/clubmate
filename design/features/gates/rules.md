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

Gates → repos → gates. Row detail by priority: `trust?` amber (graph changed; counts once in Needs you) › `N asking` (the card is on the lander) › `N landing` plain › `paused` muted › last verdict and age. The Gates row shows the highest of these across all gates. Up to 6 rows, then it scrolls.

## Sheet (`GT-Sheet-*`)

Repo · Holds (Landing; "An agent's landing waits for the graph.") · Into (presets, Branches like… with a glob field; a line under the base) · Decided by (the graph, Built in / Yours / In ‹repo›; its node chain under the field) · Rounds 1–5 ("rounds of changes, then you decide"). The footer says the gate in one sentence. ⌘↩ creates or saves, ⎋ cancels. Edit changes Into, Decided by, Rounds and Paused.

## Gate page (`GT-A-Gate-1280-Mate`, `GT-A-Trust-640-MateLight`)

- Header: Landing glyph, the gate's name, active/paused, last verdict; Pause P, Edit E, Delete ⌘⌫.
- Facts: Repo, Holds, Match, Graph (numbered node list: title, agent or step, model or command, the verdict node "approves or asks for changes"; then "ends approved · changes", the pinned version and Open graph), Rounds, Docs only.
- History: C's verdict rows (When, Verdict, Landing, Into, Open review) and C's live card for the landing under review; an approved row carries its summary under it in caption ink. A selected changes row opens its findings, deferred items last.
- Graph changed (`state: trust`): an amber trust card on top — "‹graph› changed: trust the new version?", pinned and current version, what changed per node (− old, + new), **Trust and run** ⌘↩, Open graph, Pause the gate P. Mac only: the phone's Gates screen shows the card without Trust and run.
- `trust` never hides a landing that needs you or a paused gate: the header keeps both badges.

## Rounds used up (`GT-A-Rounds-640-MateLight`, `C-GT-A-Card`)

One card on the lander, counted once in Needs you: Land anyway ⌥1, One more round ⌥2, Stop ⌥3 (built in Task 6).

## Phone (`C-GT-A-Gates-Light`)

Gates screen grouped by repo, gate pages from the shared SDUI. Pause works on the phone; New, Edit and Trust stay on the Mac.

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

- Sheet (`GT-Sheet-*`): Holds Landing · Done; Done shows "An agent's turn that ends done waits for the graph.", Sessions as three checkboxes (Top-level, Children, AFK; default Top-level + AFK) and Wrote (Plans and specs, Any file, Nothing required, Files like… with a comma-separated glob field). No Into, no Docs only. The 7-day count under the match is not built.
- Dock and page: Done gates sit after the repo's landing gates, by name, with the speech-bubble glyph; the dock shows the match only and "N held" (accent). History columns When · Verdict · Held.
- Agents (overrides `GT-A-Held-*`): per the 17 Nov ruling a Done run has no rows in the tree, except a node waiting on its own permission (`Round n review · asks you`). The row reads `held` (accent); the header badge and the phone line name the match only ("held · plans and specs", "Held · plans and specs · Plan pipeline…"), not "Done · …". The held block lists rounds with Open review, not Open run (gate runs have no page yet).
- Card: the Mac keeps the shared `request.land` card (its branch glyph and the session title row with a folder glyph still differ from `GT-A-Rounds-640-MateLight`); its status-bar keys come from the card's options. The phone draws `C-GT-A-Card` without the second-line question ("Accept the plan as done?"), which the plugin doesn't send.

## Rulings (Claude Design, 17 Nov, canvas "Clubmate · Gates")

- A held turn's header badge and phone line name only the match: "held · plans and specs", "Held · plans and specs · Plan pipeline…", never "held · Done · …"; the match already tells the point.
- A Done gate's dock detail is always "N held" (accent), also "1 held": every held Done turn is under review, so "reviewing" adds nothing. GT-Rules now reads "N held" for that step of the priority.
