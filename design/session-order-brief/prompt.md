# Clubmate: a session list that stays still

Hi. This is a new question for the Clubmate boards in this project. Draw on top of the language boards and the screenshots on this canvas. The screens involved:

- `Main` (page Today): the real screenshots. `mac-before` and `mac-after` are the user's Mac sidebar 20 seconds apart; `phone-dark` and `phone-light` are the phone's Agents list from the simulator.
- Mac Agents sidebar boards: `Agents-Mate-1280`, `Agents-640-MateLight`.
- Phone Agents list boards: `C-Workspace`, `C-Workspace-Light`, `C-Q1-NeedsYou-Light`.

## 1. The problem, in the user's words

"The order jumps whenever something updates in one of the groups. It's worst on the phone: I go to tap a session, and right before my tap a new one takes its place. At least keep the order roughly stable. It must be clearly better than now."

## 2. What makes it jump today

Mac sidebar (groups by worktree, see `mac-before` → `mac-after`):

- Rows are sorted newest-started first, so every new session pushes all rows of its worktree down by one.
- Worktree groups are ordered by their newest session. A new session in another worktree makes its whole group jump above the one you were reading. In the screenshots, `main` and `chore/awake-mac-shots` swap places.
- Many new sessions are not started by the user. Agents start child sessions, and every landing starts a "Land review" reviewer session automatically. So rows move while the user is only reading.
- A reviewer row also moves between the top level and under its lander as its review starts and ends.

Phone list (groups by state: Needs you, Open, Running, Done, then Closed and Service folded):

- Running and Done are sorted by last activity, which changes on every tool call. The Running list reorders every few seconds while agents work. This is the main pain.
- A row changes group when its state changes (Running → Done, Done → Needs you). That moves it however the groups are sorted.
- Needs you and Open are sorted longest-waiting first. The user is fine with that.

## 3. Questions for you

1. **Order inside a group.** Which key keeps rows still: start time (oldest or newest first), a position that sticks once a row appears, or something else? Where does a new session enter: top, bottom, or next to the session that started it?
2. **Order of groups.** On the Mac, the worktree groups need an order that does not jump when a session starts. Options include a fixed order with home (`main`) first and then the rest by when they were created, alphabetical, or user-pinned. What do you recommend?
3. **Tap safety on the phone.** Even a stable sort moves rows when a session is added, removed, or changes state. Should the list hold its layout while a finger is down or just after scrolling, and show "2 new · tap to show" instead? Should a state change only restyle the row in place (glyph, tint) instead of moving it to another group?
4. **State groups versus a stable list.** The phone groups by state and the Mac by worktree. Should the phone keep state groups, switch to worktree groups like the Mac, or use one stable list with state filters (the Mac has "All / Needs you / Open / Running" tabs)? The two clients should end up with the same meaning.
5. **Showing what changed without moving.** If rows no longer jump to the top on activity, how does the user see that something happened? Options: a short highlight, an "updated" dot, or the right-hand detail ("7 min", "Waiting for children") changing in place.

## 4. Constraints

- Keep the boards' design language (`Principles`, `Patterns`, `Components`; companion `C-*`). Colour roles, not hex values. The Mac font is the terminal font.
- Needs you and Open keep longest-waiting first. Closed stays a folded group, most recently closed first, 25 at a time.
- The order is decided once in the Go plugin and sent to both clients. The Mac sidebar is an SDUI tree the plugin builds. Today the phone sorts on its own; it will render the plugin's order instead. For each part of your design, say whether it is (a) a token or renderer change, (b) a new SDUI node or prop, (c) plugin content (words, grouping, order), or (d) new plugin logic (a new state or rule). Prefer (a) and (c).
- The Mac sidebar is a tree: worktree groups, sessions, and child sessions and graph runs under them. Keyboard selection and the "Needs you" filter must keep working.

## 5. Deliverables

Put this round on page `r1`, at most 6 boards: 1280 Mate dark and 640 Mate light for the Mac, and phone dark and light only where they differ.

- 2–3 distinct directions that answer questions 1–4. For each: the Mac sidebar and the phone list before and after the same event, such as a new reviewer session starting or a Running session finishing.
- Your recommendation, and your answer to question 5 in that direction.
- A handoff list with every change tagged (a), (b), (c) or (d).
