# Side threads — rules (decided 16 Nov 2026)

Text of the SA-Rules board (Claude Design), extracted. Spec: clubmate-notes/superpowers/specs/2026-11-16-side-threads-design.md.


Side threads
Clubmate · Agents · side threads · backlog #601 · decided 16 Nov
Side threads — decided 16 Nov: beside by default, its own page on request
An aside is a read-only fork of a session, opened from an option of a pending question or from a passage. It is an ordinary session page that opens beside its parent, so the card never leaves the screen; it expands to its own page with the question pinned on top. Send back through one preview, keep it as a session, or close it.
Decided 16 Nov
The aside is an ordinary session page : its own page, with the parent's question pinned as a strip on top, so you can pick from there (from C).
By default it opens beside the parent : a split at 1280, stacked under the parent at 640 (from A). From there it expands to its full page and goes back beside: Full page / Beside ⌘⇧\ .
Phone: a sheet over the session by default; its expand button opens the ordinary pushed session screen with the question strip.
Close: ⌥⌘⌫ , the same key as Close Session, plus the × in the aside's header. It asks once, only when something is unsent. ⌘W keeps its meanings and does not close an aside.
Keep: the aside leaves its parent for the top level of the tree (its worktree group), with “forked from ‹parent›” in its header.
Send back: “close the aside after” is on by default.
Unchanged from A: a on an option, ⌘\ on a selection, ⌘[ / ⌘], ⌘⇧↩ Send back (Answer question / Draft in its composer · Summary / Last message / Quote), ⌘⇧K Keep, the states and Needs you.
The three directions explored
A · Beside
folded in: the default place
The aside opens beside its parent (1280), stacked under it (640), or as a sheet over the session (phone). The card never leaves the screen.
SA-A-Option-1280-Mate, SA-A-Selection-1280-Mate, SA-A-SendBack-1280-Mate, SA-A-Option-640-MateLight, SA-A-SendBack-640-MateLight, C-SA-A-Option, C-SA-A-Message-Light, C-SA-A-SendBack-Light
C · Go there
folded in: the full page
The aside is an ordinary session page with the parent's question pinned on top. Now the place it expands to, not where it opens.
SA-A-Full-1280-Mate, C-SA-A-Full (final) · SA-C-Option-1280-Mate, SA-C-Option-640-MateLight, C-SA-C-Option (as explored)
B · Over
not chosen
A panel anchored to the option or passage, over the transcript. It covers what you are deciding about and outgrows itself in a longer exchange.
SA-B-Option-1280-Mate, SA-B-Selection-640-MateLight, C-SA-B-Option-Light
Beside and full page
Beside (default) Full page
What it is the aside in the parent's page: split at 1280, stacked at 640 the aside's own session page, like any session
Question strip no: the parent's card is on screen, and a second copy would compete with it yes, on top: the question the aside came from, options as chips, the option it is about marked
Control · key ⤢ Full page ⌘⇧\ in the aside header ⤡ Beside ⌘⇧\ in the page header (same key toggles back)
Reached by a on an option, ⌘\ on a passage, ⌘] from the parent, clicking the anchor ⌘⇧\, or clicking the aside's row in the tree
⌘[ focus the parent, scroll to the anchor open the parent, the aside beside it again, focus on the parent
Picking in the parent's card in the strip: click a chip, or ⇧⇥ from the composer then 1–4 · o, as in the card; Send stays in the parent's card
Phone a sheet over the session the pushed session screen; ‹ and ⤡ both go back to the sheet over the parent
Questions 1–6 in the final direction
1 · Where it opens
Beside the parent by default: a split in the Agents main pane at 1280 (parent left, aside right, the edge drags), stacked under the parent at 640. Full page on request: the aside's own session page with the question strip (above). Phone: a sheet over the session, the card visible above it; swipe down to the card, its Next / Send work as before. ⤢ in the sheet's header pushes the ordinary session screen.
2 · From an option
One key on the focused option: a — Ask aside , next to the card's 1–4 and o. Only the row with focus or the pointer shows “Ask aside a”, so a 4 × 4 card stays as it is. The option's right-click menu has Ask aside too. A row that has an aside shows a small “aside” mark; a on it reopens that aside. Phone: long-press an option → Ask aside.
3 · From a selection
Mac: Ask aside ⌘\ is the first item of the prose text view's menu, above Copy and Look Up; ⌘\ with a selection does the same, with nothing selected it asks about the whole last answer. The passage keeps a tint, a dashed underline and an aside mark in the margin. Phone: long-press a message → Ask aside ; the whole message is the passage, and the quote inside the aside is selectable to narrow it.
4 · How it is marked
In the tree an aside hangs under its parent with the aside glyph and the label “Aside · ‹option›” or “Aside · “‹passage…›””; a child an agent started keeps the agent glyph and its own title. Its header says read-only and where it came from. In the parent, the option row shows “aside” and the passage keeps its mark; clicking either, or ⌘], focuses the aside beside. ⌘[ in the aside goes to the parent and scrolls to the anchor.
5 · Send back
A preview panel at the end of the aside (⌘⇧↩); nothing leaves without it. To: “Answer question 1” (an aside from an option): picks that option in the parent's card and adds an editable note; the card still waits for your Send. Or “Draft in its composer” (default for a passage). From: a Summary Claude writes on request (default), the Last message, or a Quote you pick. The text is editable; “close the aside after” is on . ⌘↩ sends, ⎋ cancels.
6 · Keep and close
In the aside's header: Send back ⌘⇧↩ (primary), Keep ⌘⇧K , Full page / Beside ⌘⇧\ , and × ⌥⌘⌫ at the far end. Keep: the aside leaves its parent for the top level of its worktree group, the aside glyph becomes the session glyph, “read-only” goes, the mode switches from Plan to the parent's mode, the header keeps “forked from ‹parent›”, and it stays where it is on screen. Close (⌥⌘⌫ or ×) deletes it; only when something is unsent it asks once: “Close without sending? · Send back… · Close”.
7 · Several asides
One aside per anchor : asking again about the same option or passage reopens it. In the tree, asides hang under their parent after the children an agent started, oldest first (session order, ruled 16 Nov: a new aside goes last); from three on they fold into one row “Asides · 3”. Beside shows one aside at a time; its header gets “‹ 2 of 3 ›” and ⌘] steps through them. The card marks each option that has one. On the phone the parent's title bar gets a pill “2 asides” that lists them in a sheet.
8 · Needs you
An open aside doesn't count, unless it asks you itself. The plugin owns the state and the label; Mac and phone draw them.
Aside state (plugin) Tree detail Ink Needs you
open — waiting for you to write open plain no
thinking thinking… accent no
asks — its own question or permission (rare in plan mode) asks you amber yes, once — the aside, not the parent
sent — something went back, still open sent ok no
kept — promoted a normal session row at the top level as any session as any session
Keys
Key Where Does Status
a a focused option in a question card Ask aside about this option (reopens its aside if one exists) new
⌘\ prose with a selection · no selection Ask aside about the passage · about the whole last answer new
⌘⇧\ in an aside Full page ↔ Beside new, decided 16 Nov
⌘[ · ⌘] parent ↔ aside to the parent, scrolled to the anchor · to the aside; ⌘] steps through several ⌘[ decided 16 Nov (Agents); ⌘] new
⌘⇧↩ in an aside open the Send back panel new
⌘↩ · ⎋ in the Send back panel send back · cancel as in sheets
⌘⇧K in an aside Keep as its own session new
⌥⌘⌫ in an aside close it, asking once only if something is unsent the existing Close Session key
1–4 · o the question strip, after ⇧⇥ from the composer pick, as in the card as in the card
⌘⌫ an aside row in the tree delete, with a confirm (follows the selection) decided 16 Nov (Agents)
⌘W — unchanged: does not close an aside unchanged
Handoff
a Agents main pane: the aside beside the parent — a split at 1280, stacked at 640, draggable edge, two transcripts — and the switch to the aside's own page and back (⌘⇧\, ⤢ / ⤡ in the headers). Beside or full is a view choice the client keeps per aside.
a Prose text view: “Ask aside ⌘\” as the first menu item; the anchor tint, dashed underline and margin glyph, clickable. Phone: “Ask aside” in the message long-press menu; a selectable quote inside the aside.
a Phone: the aside sheet (one detent, card visible above) with ⤢ and ×; ⤢ pushes the ordinary session screen with ⤡ in its bar; ‹ and ⤡ return to the sheet. The question strip there is drawn natively.
a Aside glyph, expand / collapse glyphs, the read-only pill. Keys: a, ⌘\, ⌘⇧\, ⌘], ⌘⇧↩, ⌘⇧K; ⌥⌘⌫ routed to Close in an aside.
b choice option prop aside {id, state}: the “aside” mark, the “Ask aside a” hint on focus, the a key.
b Question card answer prop note : a per-question note under the options, prefilled by Send back, editable (carried as the AskUserQuestion annotation note).
b Question card prop pinned : one question drawn as a strip — header chip, question, options as chips, the aside's option marked; picking writes to the parent's card. The plugin sends it only on the aside's full page.
b Send back panel from existing nodes (card, select, textField, toggle, button): no new node.
c Words: “Ask aside”, “Aside · ‹option›”, “read-only”, “Full page” / “Beside”, “‹parent› asks”, “Pick here: it answers question 1 in the parent's card…”, “Forked from the question · read everything the session knew”, “forked from ‹parent›”, “Send back”, “Answer question 1” / “Draft in its composer”, “Summary · Last message · Quote…”, “close the aside after” (on), “Keep”, “Close without sending?”.
c Order: asides after agent-started children under a parent, oldest first (session order, 16 Nov); fold into “Asides · N” from 3. A kept aside goes to the top level of its worktree group.
d Aside session kind: fork of the parent at an anchor — {question id, option index} or {message id, range}; plan mode; parentId ; one per anchor.
d Commands: askAside(anchor) , summarize (on request), sendBack{to: answer|draft, from: summary|last|quote, text, close = true} , keep (lifts plan mode, takes the parent's mode, reparents to the top level, keeps forkedFrom ), close (the Close Session command on an aside: delete, reports unsent first).
d Strip answers: a pick in the pinned strip sets the parent's pending answer for that question, the same write as the card; the card and the strip stay in sync.
d States and Needs you as in the table: an aside counts only when it asks itself; the parent's state is untouched by its asides.
d docs/shortcuts.md: add a (card), ⌘\, ⌘⇧\, ⌘], ⌘⇧↩, ⌘⇧K; ⌥⌘⌫ gains “closes an aside”; ⌘W unchanged.
Mac, decided 16 Nov after the build: the open items card has no option cursor, so a arms the card and the next 1–4 (or o) names the option to ask about; it doesn't pick it, and ⎋ or any other key disarms.
One check
⌘⇧\ is used on no other board, and ⌘] only on these; neither is in AG-Rules. Please check both against docs/shortcuts.md before the keys land.