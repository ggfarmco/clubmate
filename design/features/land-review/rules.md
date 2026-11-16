# Land review rows — decided 16 Nov (direction C on Agents A)

User picked C in round 1 and accepted round 2 (C redrawn on the agents-merge A layout). Boards: `R2-C-*.dc.html`, all of them in `boards.html`; round 1 in `rounds/`. Brief and canvas: `design/land-review-brief/`, https://claude.ai/artifact/6omjxpYoyY5duD1FLKE5dH.

**Ruled 16 Nov over the gates GT-A boards:** the Agents tree and the lander's words follow C ("held" and the gate run hanging under the lander are dropped). From the gates work (plan clubmate-notes/superpowers/plans/2026-11-16-gates-land-graph.md): a landing's review can be a graph run with several node sessions, all top-level, startedBy the lander, each with `gateFor`; hide and route by `gateFor`, any of them can be the "asks you" child, and `reviewerId` is `landReview.Reviewer` (the verdict node). Later the asking card gains "One more round" ⌥2 (Stop ⌥3), `review 2/2…` counts rounds plus extra rounds, and the dock row "Land gates" becomes "Gates".

## Rules

- **Tree:** reviewers have no row, not in worktree groups, not in Closed, not in All / Open / Running counts. Closed reads 87, not 283. A row goes to Closed only if it had a row while open.
- **Lander detail:** `review…` (`review 2/2…` from round 2) in accent · `3 changes` (N = findings) in text-2 · `approved` in success ink, fading to muted once seen · `asking` (Land anyway / Stop) in amber, counts once in Needs you.
- **Asking reviewer:** while it waits on its own permission, a child row under its lander: amber shield, `Round n review`, `asks you`; the lander unfolds; counts once in Needs you, rail dot; the dock's Land gates row reads `1 asking` in plain ink. Gone once answered.
- **Lander page:** second header line: shield · `Landing into ‹into›` · `round 1 of 2` · verdict badge · `Open review ⌥R`.
- **Gate page:** verdict rows with `Open review`; a live card for the landing under review (latest line, Open review ⌥R, Open landing ↩). Dock gate row detail `N landing`, plain. The only way to reach older rounds and closed reviewers.
- **Reviewer page:** crumb `Agents / main / ‹lander› / Round n review` (from the lander) or `Agents / Land gates / ‹repo› / Round n review` (from the gate page); ⌘[ back. Header `Round n review` of ‹lander›, verdict badge (`reviewing` · `3 changes` · `approved`), `asks you` badge while it asks. Meta `branch → into · Fable 5.2 · read-only · time`. No `Input only`. Request collapsed to one `Review request` row; transcript ends with `Sent to ‹lander›`.
- **Keys:** ⌥R = the review of this landing (lander page: latest round; gate page: selected verdict's round). ↩ on a verdict row opens the landing. `Open review` in ⌘K. Everything else as AG-Rules.
- **Phone:** lander subtitle `In land review`, `Review asked for 3 changes`; the asking reviewer nests under its lander in Needs you; Closed, Service, Land gates, Graphs end the list, Closed has no reviews; on the gate screen a verdict opens the reviewer's native session screen.
- A graph node named "Reviewer" is a run node and stays under its run: the plugin tells them apart by the gate field, not the name.

## AG-Rules changes

- § 4 "Reviewer sessions move under their lander, named 'Round 2 review', with the verdict as detail … The lander's own detail stays the gate state (review… / asking)." → "Reviewer sessions have no row. The lander's detail carries the landing: review… · review 2/2… · 3 changes · approved · asking. Only while a reviewer waits on its own permission does it show, under its lander, as 'Round n review' with an amber shield and 'asks you'; the row goes when the request is answered. A reviewer opens from the lander's page (Open review ⌥R) and from its verdict on the gate page."
- Handoff a "Reviewer row under its lander." → "… only while it asks for a permission."
- Handoff d "Reviewer: parentId = lander; gate = {round, of, verdict, findings} replaces the notify detail." → "Reviewer: no tree node, except as a child of its lander (parentId = lander) while its status is 'waiting for permission'. The lander carries landing = {round, of, verdict, findings, reviewerId}. Reviewers are left out of the tree, Closed and the chip counts."
- § 6 Links "Open review … the reviewer session (now under its lander)" → "the reviewer's page in the main pane, crumb Agents / Land gates / ‹repo› / Round n review (it has no tree row)".

## Handoff

- (a) ⌥R Open review in docs/shortcuts.md + ⌘K.
- (a/b) Lander session header second line (b if the header is plugin-built).
- (a) Reviewer page crumb and ⌘[ like the run page, "of ‹lander›".
- (a) Renderer fix: toned row details lose to the muted `.row > .d` colour (also on A's boards).
- (b) Tree row prop `open` also takes a reviewer session that has no row.
- (c) No reviewer nodes in tree / Closed / counts; asking child row; lander detail words and inks; reviewer title `Round n review`, no notify level; gate row `N landing`; phone subtitles.
- (d) `landing = {round, of, verdict, findings, reviewerId}` on the lander; reviewer child only while waiting for permission; link target `agents/review/‹id›`; fade `approved` after seen.
