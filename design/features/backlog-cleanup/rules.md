# Backlog cleanup + task numbers — final (A)

Decided 16 Nov 2026 by the user: direction **A · tidy in place**; Note is written by both (agents via backlog_update, the user in the UI); the Backlog list stays 248 pt at 1280 (the "316 from C" offer declined); phone number search opens the pinned task on Return, not while typing; `#N` in agent messages is a plain accent link (done = secondary ink, not struck), not a chip.

Boards: `png/` (rendered locally with `source/cm.css`), sources `source/*.dc.html` from the Claude Design project "Clubmate design" (BL-*, TL-*, C-BL-Search, C-TL-*). Brief and the round with B/C: `design/backlog-cleanup-brief/`.

## Designer rules (BL-Rules, as text)

Backlog cleanup + task numbers 

- 

Clubmate · Backlog · 16 Nov · three directions for review 

## Backlog cleanup + task numbers — A tidy in place (recommended), B properties beside, C one meta line 
The screen reads as dirty for three reasons: three inputs stand before the first task, list rows don't share columns, and the detail pane says who owns the task and since when three times. A fixes all three with the nodes we have. B and C go further and cost more. A number in search pins its task, and a #N in an agent's message links to that task. 

## Recommendation 
- A, tidy in place. One search field (gc). New task moves to + and ⌘N. Tags fit on one line: All · the top two · “+45”. Groups become section labels, and rows sit on fixed columns: priority mark, title, #N. In the detail: the title with “#81 · saved”, then Start agent · Done · Edit, with a quiet Delete. Five property rows share one look: Status, Priority, Tags, Owner (“since …”), and Note. Then the body, then Links with + Link (L). The Owners lane shows only from 2 moves. 
- Number search, in A’s rows: a section label “Task #40” sits above the pinned row. The row shows its status icon in the priority column, because it sits outside its group. Its number is in fg. When nothing has that number, the row reads “No task #999 · last is #83” and can't be selected. 
- #N in agent messages: a plain accent link, the same as #12 and #40 in a description today. A done task's link is in secondary ink and never struck. Hover shows the status and title. On the phone, the same link opens the task on tap and previews it on long-press. 
- If you want more room for titles, take one thing from C: let the Backlog list be 316 wide when the window is ≥1280 . The detail has a max width, so the room is spare. 

## What looks dirty — your read, checked 
Mostly agreed. Where the cause differs from the symptom, the second column says so. The last three rows are ours. 
- In the build Our read A · tidy in place B · properties beside C · one meta line 
- Three inputs before the first task Agreed. New task and + Add are one action drawn twice; search is the field you decided to keep. Search alone. + and ⌘N open a New task field above Todo; ⏎ adds and stays open, ⎋ closes. One field: it finds as you type, and ⌘↩ adds the query as a task. As A, with an “All tags” select beside the search field. 
- Tag chips wrap to 4 rows Agreed. The seven shown are only the biggest tags; the long tail already hides behind “show 40 more”. One line: All 70 · agents · phone · “+45”, which opens a menu with a filter. Tags move behind a Tags button in the header. The active tag sits as a token in the field. An “All tags” select. 
- Titles cut at ~20 characters Three causes, not one: the 24 pt tree indent under groups, the #N column with its gap, and the !! column that only some rows reserve. No indent, the !! column kept on every row, #N in 4ch. About +5 characters. Number first in 3ch; !! moves to the end. A's rows, list 316 wide: about +14 characters. 
- !! shifts titles This is #12: rows without an icon don't keep the icon column. Fixed in the tree renderer. Moot: the number leads. As A. 
- “Todo · 36” looks like a row It is a tree parent drawn at row height, with a status circle. A section label: caption ink, count at the right, space above. Same. Same. 
- The note reads like a subtitle Agreed. It is content with no label. A property row labelled Note. A quoted block above the body. The first line of the body, labelled Note. 
- Delete at full weight Agreed. A rare act shouldn't sit in red at the end of every task. Ghost button in muted ink; under ⋯ at 640. Red only in its confirm. Under ⋯. Under ⋯. 
- Owners · 1 move A lane for one event is a timeline of nothing. Hidden below 2 moves; the Owner row says “since …”. History as a list in the rail. One line: “History · created by you · …”. 
- Owner and Created repeat the lane Agreed. Merged: “you · since Nov 16 18:46 · created it”. Merged. In the meta line. 
- Field rows mix selects, a text field and plain text Agreed. One quiet look: the value as text, with a border and chevron on hover and focus. Same, in the rail; tags as chips. Inline controls in the meta line. 
- “Saved ⌘S” mid-form It's a status, not an action. “#81 · saved” beside the title. “saved” in the action bar. In the meta line. 
- Links last; the link field always open Agreed. Links come right after the body. + Link (L) opens the field. Links sit in the rail. One line when there are 0–1 links. 
- ours · “Edit description” row A second way to edit, in the middle of the pane. Edit (E) in the action bar. Same. Same. 
- ours · 640 keeps the list inline Patterns: below 800 the list is an overlay on ⌘B and the detail gets a breadcrumb. All three follow Patterns. 
- ours · description links #12 and #40 in a description are already accent links. Agent messages reuse that same look. 

## The three directions 

## A · Tidy in place recommended 
Same single column and the same nodes, with repeats removed. Properties come before the body because they're short and they're what you change. 
Cost: renderer 4 (a) · 2 small (b) · the rest is plugin content (c) · search logic (d) 
BL-A-1280-Mate · BL-A-640-MateLight (search “40”) · C-BL-Search 

## B · Properties beside 
Body on the left, a 300 pt rail on the right: properties, links, and history as a list. One find-or-add field; rows lead with the number. Below 800 the rail stacks above the body. 
Cost: A's (a) plus a fixed-width column in the stack (b), a token field for tags (b), and find-or-add (d) 
BL-B-1280-Mate · BL-B-640-MateLight (search “#999”) 

## C · One meta line 
Properties become one line of inline controls under the title. The body takes the full width. Links and History fold to one line each when they hold 0–1 items. The list is 316 wide at ≥1280. 
Cost: an inline select variant (b), a sidebar width per section (b), one-line sections (c) 
BL-C-1280-Mate · BL-C-640-MateLight (search “#78”, a done task) 

## Find a task by its number 
- A query is number-only when it is digits with an optional leading #, after trimming spaces: 601 , #601 , #601 . Anything else ( #601 flaky , v2 , 6o1 ) is a text query, as today. 
- The task with exactly that number is pinned first under a section label “Task #601”, selected, and opened on the right. Focus stays in the field so you can keep typing. ⏎ moves focus to the detail; ⎋ clears the query and returns to the task you had open. 
- Every keystroke re-pins: “6” pins #6, “60” pins #60, “601” pins #601. It never matches a prefix. 
- The pinned row ignores the tag filter and the groups' collapsed state, because you asked for that number. It is removed from the text matches below, which stay grouped by status. 
- When nothing has that number, the slot reads “No task #999 · last is #83”. It can't be selected, the detail keeps the task you had, and the field never offers to add a number as a task. 
- Phone: the same section label and row in the search results. It opens on Return rather than while you type, because the phone has no pane beside the list to open it in. 

 found · todo 

 Task #40 
 Open items Mac: mouse paths #40 
 Todo 1 
 !! Land gate rollout: release, add the… #81 
 found · done 

 Task #78 

 roots compare.py on both Macs #78 
 Text 
 No text matches 
 found · owned by an agent 

 Task #601 

 Side threads: open beside the parent #601 
 owned by Claude · running 
 not found 

 Task #999 
 No task #999 last is #83 
 Text 
 No text matches 

- Query Pinned slot Detail pane Below 
- 40 · #40 Task #40 · ○ Open items Mac… · #40 opens #40 text matches, grouped by status (#81, #78) 
- #78 (a done task) ✓ in muted ink, title in secondary ink opens #78 with Reopen the Done group stays closed 
- #601 (owned by an agent) the owner's status icon (▶ running in accent) opens #601 — 
- #999 · #0 No task #999 · last is #83 (not selectable) keeps the task you had text matches or “No text matches” 
- # nothing pinned unchanged text search for “#” 
- #40 flaky nothing pinned (a text query) unchanged text matches 

## #N in agent messages 

## 1 · Link or chip 
 A plain accent link. Agents write three numbers in one sentence. Chips break the line rhythm there (compare TL-A and TL-B), and in a native text view they need attachments, while a link is just an attribute on a range. The status goes in the hover, which shows the status icon, #N, title, status, priority and tags, plus “click opens it in Backlog”. It appears after 400 ms. 

## 2 · A done task 
 Secondary ink, still a link, never struck. A strike makes digits hard to read, and the number is what you scan for. Idea and Not now look like Todo; the hover tells them apart. 

## 3 · Phone 
The same accent link at body weight 500. The text view grows each link's hit area to 44 pt tall without changing the line height. A tap pushes the task screen, with ‹ back to the session. A long-press previews the task (status, title, the first lines, tags) with “Open task” and “Copy “#612””. That preview is the phone's hover. 

## 4 · Where it goes 
On the Mac, a click opens the Backlog section with that task selected, and ⌘click opens it in a new window. On the phone, a tap opens the task screen. The task is looked up when the message is drawn, so a link turns quiet when its task is done later, and goes back to plain text if the task is deleted. 
- Text in the agent's prose Becomes 
- Filed #612 · see #590 item 3 · #618 and #619 links (#590 in secondary ink, because it's done) 
- PR #3 · issue #3 · pull #3 · owner/repo#3 plain: the word or repo before it names something else 
- #612 in code, inside a URL, or in a heading anchor plain 
- #9999 where no task has that number plain 
- #601 in your own message, in tool output or in thinking plain: only the agent's prose links 

## Handoff — A, number search, links 
a token/renderer · b new SDUI node or prop · c plugin content · d new plugin logic 
- a Tree renderer: every row keeps the icon column, blank or not (#12). Parents in a grouped tree draw as section labels (caption ink, count at the right, 8 pt above). No indent under a section label. 
- a Form renderer in detail panes: select, textField and text values share one quiet look, with a border and chevron only on hover and focus. 
- a Action bar: a button marked quiet draws as a ghost in muted ink. Delete uses it. 
- a Transcript text view, Mac and phone: draw task ranges as accent links (done in secondary ink), with the hover card on the Mac, a 44 pt hit area and long-press preview on the phone. 
- b Tag tabs: overflow: menu . Chips that don't fit fold into “+N”, which opens a menu with a filter field. 
- b Link action openTask {n} . The Mac routes it to the Backlog section (⌘click: a new window); the phone pushes the task screen. 
- c Sidebar: drop the always-open New task field and the + Add row. The header + and ⌘N show the New task field above Todo. Search placeholder: “Search, or a #number”. 
- c Rows: priority mark first (blank when none), then the title, then #N as the row's detail. No status circle on group parents. 
- c Detail: header “#81 · saved”; actions Start agent · Done · Edit (E) · Delete (quiet; under ⋯ below 800). Properties: Status, Priority, Tags, Owner (“you · since Nov 16 18:46 · created it”), Note. Then the body; then Links with + Link (L), which opens the link field. Drop the Created and Saved rows and the Edit description row. The Owners lane only from 2 moves. 
- c Words: “Task #40”, “No task #999 · last is #83”, “No text matches”, “70 open”. 
- d Search: detect a number-only query → pinned section + select + open; skip the tag filter for it; remove it from the text matches; ⎋ returns to the previous task. 
- d Agents plugin: mark task ranges in the agent's prose (rules above), resolve them against the workspace backlog when drawn, and send them to the Mac and the companion. 

## Open questions 
- Who writes the Note: you, the agent, or both? If agents write it, the label should name the writer: “Note · Claude · 14 Nov”. 
- Should the Backlog list stay 248 wide everywhere, or take 316 at ≥1280 (from C)? 
- Phone: should the pinned task open on Return (drawn) or at once, like on the Mac? 
Task titles cut off in the screenshots are filled in for the drawing. Task bodies for #40 and #78 are placeholders. The #612–#619 numbers come from the brief.