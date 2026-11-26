# Keyboard shortcuts

The single list of every Clubmate shortcut; the app's own keys and the ones plugins declare are also listed, searchable, in Settings → Shortcuts (`ClubmateKit/Shortcuts.swift`, a test checks each is in this file). Any change that adds, removes or rebinds a key updates this file in the same commit.

Plugin keys are declared in each plugin's `plugin.json` (`contributes.shortcuts`) and listed in Settings → Shortcuts; a test checks that every declared key appears here.

## App and windows

| Keys | Action | Where defined |
|---|---|---|
| ⌘N | New (terminal or item in the current section; in Backlog it opens the New task field above the list and focuses it, like the sidebar header's +, in Triggers and Cron it starts an agent session that asks you for a trigger or a cron job and adds it) | `ClubmateApp.swift` |
| ⇧⌘N | New Home Terminal | `ClubmateApp.swift` |
| ⌘O | Open Folder… | `ClubmateApp.swift` |
| ⇧⌘H | Open Home in this window (File › Open Recent, item 1; also the Home row on Welcome and Open Home in ⌘K; ↩ on Welcome no longer opens Home, it opens the row under the cursor, which starts on Open folder…) | `ClubmateApp.swift` |
| ⇧⌘D | Open Doc…: fuzzy picker over `docs.list` (agent-written docs first), ↩ opens the doc in the popup | `ClubmateApp.swift` |
| ⌘W | In Terminals, close the selected terminal, asking first while it runs a process; in a window with no workspace, close the window; in other sections of a workspace window, nothing (the popup and VS Code have their own rows) | `ClubmateApp.swift` (`CloseKeyOwner`) |
| ⌥⌘⌫ | Close Session: the terminal session shown in the main pane, asking first while it runs a process (as ⌘W); in Agents the selected agent session with its child sessions, as its row's Close Session (a closed one stays as it is), asking first while its turn or a child session's turn runs or waits (`agents.busy`); with focus anywhere in an aside beside its parent (its prompt, a message, a card), that aside (Close without sending? when something is unsent) | `ClubmateApp.swift`, `ClubmateKit/CommandInvocation.swift` (`AgentsSection`) |
| ⌘K | Command Palette | `ClubmateApp.swift` |
| ⌘B | Toggle Chat in Code and Wiki: the right column with the agent chat panel (per workspace; stacked under the editor or the page below 1100 pt). Opening it shows the session picker and focuses the suggestion; hiding it returns focus to the section's main pane. The first 3 times ⌘B opens the chat in Wiki on this Mac, a muted line under the picker header says "⌘B opens chat here now. The page tree moved to ⌃⌘S." with Got it; Got it or the first ⌃⌘S in Wiki ends it for good. The hide button at the end of the panel's header (tooltip "Hide chat ⌘B") does the same as ⌘B. In a section without a chat panel it does nothing | `ClubmateApp.swift`, `WorkspaceUIStore.swift` |
| ⌃⌘S | Toggle Sidebar (the tree) in every section that has one; it never touches the chat panel (in Wiki it ends the one-time ⌘B hint), and Code has no sidebar | `ClubmateApp.swift`, `WorkspaceUIStore.swift` |
| ⌘= (⌘+), ⌘-, ⌘0 | Font size up, down, reset where focus is: in a terminal or nvim (or with nothing focused) the terminal font for all terminals; in the agent chat (composer) the chat font, shared by every chat; in another pane (e.g. the Backlog sidebar) that plugin's font, both its sidebar and its main pane. The terminal font is Settings → Terminals → Font size, the chat font Settings → Agents → Chat font size and a plugin's font its own Settings page → Font size; a plugin follows Settings → General → Interface font size until zoomed or set there, ⌘0 puts it back (General → Plugin fonts resets them all); sizes are kept across restarts. While the popup shows a doc and has focus it zooms the doc instead, see below | `ClubmateApp.swift`, `WorkspaceLayout.swift`, `WorkspacePanes.swift` (`ClubmateKit/PaneFocus.swift`, `AppSettings.swift`) |
| ⌘1…⌘9 | Switch to the Nth section in the rail, in your order (drag icons to reorder; right-click the rail to reset). Graphs and Gates are not in the rail: they are docked at the bottom of the Agents sidebar (see § Trees), so ⌘8 and ⌘9 go to the sections after them | `ClubmateApp.swift` |
| ⌃⌘→, ⌃⌘← | Next, previous workspace | `ClubmateApp.swift` |
| ⌃⌘↓, ⌃⌘↑ | In Agents: next, previous session, the row below or above the selected one among the rows the sidebar shows (filter, search and folded groups apply), wrapping around; focus goes to its prompt (`agents.step`) | `ClubmateApp.swift`, `ClubmateKit/CommandInvocation.swift` (`AgentsSection`), `plugins/agents/sidebar.go` (`stepSession`) |
| ⌃⌘1…⌃⌘9 | In Agents: open the Nth numbered row of the sidebar: the first nine top-level sessions and graph runs it shows (filter, search and folded groups apply), never asides, graph steps, reviewer rows or inbox copies; a session's prompt gets the focus, a run opens its page (`agents.jump`). Hold ⌃ for ½ s in Agents to see the digits on the rows (Window › Go to Session) | `ClubmateApp.swift`, `ClubmateKit/CommandInvocation.swift` (`AgentsSection`), `plugins/agents/jump.go`, `Windows/Workspace/AgentsSessionKeys.swift` |
| ⌃⌘J, ⌃⇧⌘J | In Agents: next, previous session that needs you, oldest first (asides, graph steps and runs included), wrapping around; focus goes to its prompt; "nothing needs you" when none waits (`agents.needs`) | `ClubmateApp.swift`, `plugins/agents/jump.go` |
| ⌃⇥ | In Agents, with focus in any Agents pane (the prompt too): the last session, and back with a second ⌃⇥; "no previous session" when it is closed or gone (`agents.last`). Not a menu key: in terminals, nvim and text fields of other sections ⌃⇥ keeps its own meaning | `Windows/Workspace/AgentsSessionKeys.swift`, `plugins/agents/jump.go` |
| Esc, ⌃[ | In Agents (sessions tree, transcript, prompt, cards, aside): step out, prompt → transcript → tree → the prompt you left (an aside's transcript steps to its parent's transcript first; a session with no messages skips its transcript). Open things close first (a menu, the palette, a sheet, the / command list, the Always allow confirm, text in the tree's search field). Agents always takes Esc, also where there is nothing to step out of (the status bar then says why, e.g. "No session open · ↩ open · ⌘N new"), so Esc never reaches the window and never ends full screen there; ⌃⌘F, Globe F or the green button do. Other sections keep their Esc | `SDUI/Agents/AgentsFocus.swift`, `SDUI/KeyMonitor.swift` (a fallback monitor that runs after all others), `ClubmateKit/AgentsKeys.swift` |
| ⌥⌘←, ⌥⌘→, ⌥⌘↑ (⌃K), ⌥⌘↓ (⌃J) | In Agents, also from the prompt: go to the sessions tree; the main pane's prompt (from the main pane: the aside's prompt); the transcript of the column you are in; the prompt of the column you are in. ⌃K and ⌃J do the same as ⌥⌘↑ and ⌥⌘↓ from the tree, the transcript, the prompt and the tree's search field; an open list keeps them to move its selection (the prompt's / command list, the @ file picker, any picker field), and other text fields (a card's answer field) keep their own ⌃K. ⌃K in the prompt replaces Cocoa's delete to the end of the line there. Other sections, terminals and nvim keep ⌃J and ⌃K | `SDUI/Agents/AgentsFocus.swift`, `ClubmateKit/AgentsKeys.swift` |
| ⌃H, ⌃L | In Agents, also from the prompt: one column left, right (tree ← main pane → aside; with the sidebar on the right, the tree is the rightmost column). A column without messages takes the focus in its prompt. ⌃H in the prompt replaces Cocoa's delete backward there; from the sessions tree they land in the main pane's prompt, as before. Other text fields (a card's answer field, a picker field, the tree's search field) keep their own ⌃H and ⌃L. The keys are physical: they work in any keyboard layout (e.g. Russian). On the iPad, with a hardware keyboard: ⌃H goes to the list, ⌃L into the session | `SDUI/Agents/AgentsFocus.swift`, `ClubmateKit/AgentsKeys.swift`, `CompanionKit/PaneLadder.swift` |
| ⌘/ | In Agents, from any pane: show or hide the keys of the focused pane (a card at the bottom right, from the `AppShortcuts` groups of that pane: tree, transcript, prompt). Esc closes it first | `SDUI/Agents/AgentsFocus.swift`, `ClubmateKit/Shortcuts.swift` (`PaneKeysHelp`) |
| ⌘. | In Agents, from any pane (the prompt too): stop the running turn of the column with focus (the session header's Stop prints ⌘.; in an aside beside its parent, the aside's Stop when focus is in the aside, else the parent's). On a running graph run's page it stops the run (confirmed). With nothing to stop the status bar says "Nothing runs here"; ⌘. never reaches the window | `plugins/agents/view.go`, `plugins/agents/aside_view.go`, `SDUI/InputViews.swift` (`ButtonNodeView`), `ClubmateKit/AgentsKeys.swift` |
| ⌃⌘W | Workspace switcher grid | `ClubmateApp.swift` |
| ⇧⌘I | Notifications inbox (bell popover) | `ClubmateApp.swift` |
| ⌥⌘A | Keep Awake: with no choice active keeps the Mac awake until turned off, with any choice active turns it off (`awake.toggle`). Printed next to Until Turned Off or Turn Off in the status bar's Keep Awake menu and Settings → Keep Awake; ⌥-click on the gourd does the same. Disabled while the awake plugin doesn't answer | `ClubmateApp.swift`, `Windows/KeepAwakeItem.swift`, `ClubmateKit/KeepAwake.swift` |
| ⌘R | Retry: start the core again (Help › Retry Core). Only while the core didn't start (the window's main column and Welcome show the message with Retry) or stopped after its reconnects gave up (status bar "core stopped"); in those states the menu item exists, so it wins over a plugin's ⌘R (Run now, Open review), which cannot reach the core then. Otherwise the menu item is gone and ⌘R goes to the plugin | `ClubmateApp.swift`, `Windows/Workspace/StartupSkeleton.swift`, `Windows/StatusBar.swift`, `Windows/WelcomeView.swift` |
| ⌘L | Show log: open the core log (`core.log` in the state folder). Only while the core is slow to start (after 15 s, "Still starting core…") or didn't start | `Windows/Workspace/StartupSkeleton.swift`, `Windows/WelcomeView.swift` |
| ⌥Space | Voice: hold to talk, release to send; a tap (under 600 ms) starts listening and the next ⌥Space sends. Nothing is sent before that end. With key focus in an agent's message field (the composer) it dictates instead: the same hold or tap, then the tidied text goes into that field at the cursor and nothing is sent; the panel reads "Dictation". Taken before terminals, nvim and text fields, so it never types a character there. The mic button at the left of the status bar does the same with a click or a press-and-hold, and the mic in the composer's control row dictates into that composer. While voice is off (Settings → Voice) ⌥Space is not taken and reaches the focused view as usual | `Windows/VoiceButton.swift` (`VoiceHotkeys`), `ClubmateKit/Voice.swift` |
| ↩, Esc | In the voice panel's confirmation of a dangerous action: Yes, No. The panel takes key focus when the confirmation shows (↩ is ignored for its first 0.6 s and while held, so a Return typed for a terminal does not approve); once focus moves elsewhere, ↩ and Esc go there again | `Windows/VoicePanel.swift` |
| Esc, ⌥Space | In the voice panel during the 1 s before an utterance is sent ("Sending"): cancel it, nothing reaches the assistant; ⌥Space then starts nothing new. The panel takes key focus only for that second | `Windows/VoicePanel.swift`, `ClubmateKit/Voice.swift` |
| Esc | Close the popup a plugin shows over the workspace (e.g. a doc), in the panel or its popped-out window; focus goes back to where it was. While a doc's diagram is enlarged, Esc closes only the diagram; while the find bar is open, only the find bar | `PopupPanel.swift`, `PopupWindow.swift` (`ClubmateKit/PopupKeys.swift`) |
| Esc | Close an enlarged mermaid diagram from the transcript | `SDUI/MarkdownView.swift` |
| ⌘W | Close the popup over the workspace (or its popped-out window) while it has focus, instead of the terminal or window | `PopupPanel.swift`, `PopupWindow.swift` |
| ⌘= (⌘+), ⌘-, ⌘0 | While the popup or its popped-out window shows a doc or artifact and has focus: zoom the page in, out (×1.25, 50…300%), reset; the popup header shows the level (e.g. `125% ⌘0`, click resets) while it is not 100%. Trackpad pinch magnifies too | `PopupPanel.swift`, `PopupWindow.swift` (`ClubmateKit/PopupKeys.swift`) |
| ⌘F | While the popup or its popped-out window shows a doc or artifact and has focus, or a section's main pane shows a page (a wiki page, its History diff) and no popup is open over it: open the find bar over the page (again: focus it and select the query). Matches highlight as you type, case-insensitive; the bar counts them (`3/12`). The popup also has a Find button (magnifying glass) beside Pop out | `PopupPanel.swift`, `PopupWindow.swift`, `WorkspaceLayout.swift` (`ClubmateKit/PopupKeys.swift`), `DocumentNodeView.swift` |
| ↩ / ⇧↩, ⌘G / ⇧⌘G, ↓ / ↑ | While the find bar is open: next / previous match, wrapping (↩, ↓ and ↑ only in its field) | `PopupPanel.swift`, `WorkspaceLayout.swift` (`ClubmateKit/PopupKeys.swift`), `DocumentNodeView.swift` |
| Click, + (=), -, 0, Esc | In a Markdown doc (Mac popup, phone): click or tap a mermaid diagram to enlarge it over the page; + and - zoom, 0 fits, Esc closes; drag pans, wheel scrolls, ⌘/⌃+wheel or pinch zooms; the − + Fit ✕ buttons do the same | `ClubmateDocument/Web/document.js` |
| ⌘J | Switch Popup Focus: while a popup (e.g. a doc) is open, go from it back to where focus was and back again. The panel over the workspace hides on the way out, keeping its doc, Back stack and zoom, and shows again on the way in; a popped-out popup window stays open and only focus moves between it and the workspace window. `docs.open` while the panel is hidden shows it again. A menu key, so it also works from a terminal or nvim (shadows any ⌘J mapping there) | `ClubmateApp.swift`, `PopupPanel.swift`, `PopupWindow.swift` (`ClubmateKit/PopupKeys.swift`) |
| ⇧⌘O | Pop the popup out into its own window; later popups of that workspace open there until it is closed | `PopupPanel.swift` |

Inside terminals, Ghostty's own bindings apply, except ⌘W, which is unbound so the app gets it (`GhosttyApp.swift`). Selecting text copies it to the system clipboard and a middle click pastes the clipboard, as before Ghostty 1.4: Clubmate defaults to `copy-on-select = clipboard` and `middle-click-action = clipboard-paste` (Ghostty 1.4 turned copy-on-select off on macOS, and its `primary` copies nothing here because Clubmate has no selection clipboard); both keys in Clubmate's own `ghostty.conf` override them (`GhosttyConfigReader.swift`). Dropping files on a terminal or nvim pastes their paths, each quoted for the shell (`'my file.txt'`) and separated by spaces; dropped text pastes as it is. A drop is a paste: bracketed paste and Ghostty's paste protection apply, and nothing runs until you press ↩ (`TerminalSurfaceView.swift`, `ClubmateKit/TerminalDrop.swift`).
⇧⌘I, ⇧⌘D, ⌘J and ⌥⌘A are app menu keys, so they shadow any mapping of them in terminals (Ghostty's default ⇧⌘D split) and nvim.

## Problem report sheet

| Keys | Action |
|---|---|
| Esc | Cancel: nothing is sent |
| ⌃⇥ | Leave a text field for the next control (⇥ in a text field types a tab) |
| ⇥ ⇧⇥ | Once out of the text fields: move between the file checkboxes, Quick Look buttons, Cancel and Send (with Keyboard navigation on in System Settings; Space presses) |

Send has no key on purpose, so a keystroke meant for the window under the sheet never sends a report. Defined in `App/ReportSheet.swift` (`ReportSheet`).

## Plugin buttons

A plugin button with a `shortcut` prints it (`⌘R`) and presses on it while the button is on screen; a combo without ⌘, ⌃ or ⌥ does nothing while a text field has focus, app menu keys always win (a menu item that needs fn, like Edit › Emoji & Symbols on fn E, never takes the bare key), and Esc, ↩ and ⇥ without ⌘, ⌃ or ⌥ cannot be plugin shortcuts. Plugin shortcuts do nothing while a terminal or nvim has focus, except ⌘↩, which presses over them too; a focused agent message field (the composer) keeps ⌘↩ to send its message. A button in a docked form (`form.dock`, the agent's question card) ranks above those: its key beats other buttons and an empty composer, loses to a composer with text, and does nothing while a terminal or nvim has focus. Defined in `SDUI/InputViews.swift` (`ButtonNodeView`) and `SDUI/KeyMonitor.swift`, combos parsed by `ClubmateKit/KeyCombo.swift`. In a plugin's sheet (the `sheet` node) Esc, ↩ and Enter press its cancel button (↩ and Enter not while a text field has focus), which prints ⎋; the sheet's other buttons keep their shortcuts and the page's buttons under it wait (`SDUI/SheetNodeView.swift`). Plugin shortcuts in use:

| Keys | Action | Plugin |
|---|---|---|
| ⌘R | Run the cron job or trigger now | cron, triggers |
| P | Pause or resume the job or trigger | cron, triggers |
| ⌘⌫ | Delete the job, trigger or task, or Remove run… on an ended run's page in Agents (confirmed) | cron, triggers, backlog, agents |
| ⌘[ | Back from a run, graph or gate page in the Agents main pane to the session you came from; in an aside, back to its parent (see [Asides](#asides-agents-section)) | agents |
| ⌘↩ | Open the Start agent popover on the task while you own it (↩ in it starts, ⎋ cancels), else open the session or graph run that owns it; on a graph run held at a round cap Continue it, else open the session of the graph node that needs you, else a running step's output (Open ‹Step› output), else the running or start node's session; run the graph of a definition page with its input (↩ in the Input field too); on an untrusted graph Trust… (then Trust in its sheet, where Esc cancels) | backlog, agents |
| ⌘↩ | Send review: opens the Send sheet for the review tab current in Neovim, also while Neovim has focus (status bar button "Send review") | editor-nvim |
| ⌘↩ | Remove the folder, on the Remove folder sheet (⎋ cancels); Close, once the folder left and a worktree stayed | agents |
| ⌘↩ | Archive the task, on the Archive sheet (⎋ cancels); Close, once something stayed | agents |
| ⌘↩ | Remove what is ticked, on the Clean up worktrees sheet (⎋ cancels); Close, once something stayed | agents |
| ⌘↩ | Add the ticked repos, on the Add repos sheet (⎋ cancels) | agents |
| ⌘↩ | Create gate on the New gate sheet, Save on the Edit gate sheet (⎋ cancels) | agents |
| ⌘↩ | Trust and run on a gate page whose graph changed (the trust card; Mac only) | agents |
| ⌘N | New gate: opens the New gate sheet (on How gates work in Agents; also + on the docked Gates row and New gate… in ⌘K) | agents |
| P | Pause or resume the gate open in Agents | agents |
| E | Edit the gate open in Agents: which landings it holds (Into) or, for a Done gate, which sessions (Sessions) and what they wrote (Wrote), the graph that decides, its rounds and whether it is paused | agents |
| ⌘⌫ | Delete the gate open in Agents (confirmed; its verdict history goes too) | agents |
| ⌥1 / ⌥2 / ⌥3 | A graph step waiting for approval: Allow once / Always allow… (asks first; only when the step's command can be a rule) / Reject | agents |
| ⌥R | Open review: the land review of this landing. On a lander's page (second header line while its landing is gated) the latest round's reviewer; on a land gate's page the reviewer of the live card or of the selected verdict | agents |
| ⌘[ | Back from a reviewer's page to where it was opened from (the lander, or the land gate's page) | agents |
| ⌥↑ / ⌥↓ | A repo child of a task: the repo before / after it in landing order that has a session, no wrap-around (buttons in the crumb); in a text field they stay caret movement | agents |
| ⌘. | Stop the graph run open in Agents (confirmed) | agents |
| ⌘N | Ask an agent to write a graph (on How graphs work in Agents; also + on the docked Graphs row and New graph… in ⌘K), ↩ on a part row asks for a graph that uses that part | agents |
| G | Open the graph run that created the trigger | triggers |
| ⌘N | Ask an agent to add a trigger (How triggers work; the app menu's ⌘N does the same anywhere in Triggers), ↩ on an event row asks for a trigger on that event | triggers |
| D | Mark the task done | backlog |
| W | Close the task as Won't do: opens the "Why it won't be done" field under the task form, ↩ in it closes the task with that reason | backlog |
| E | Edit the task's description; again (Done editing) shows it as a page | backlog |
| S / P | Open the task's Status / Priority menu (a `select` with `shortcut`; ↑↓ ↩ pick, ⎋ closes); no Status menu while the task is In progress | backlog |
| ⌘N | Ask an agent to add a cron job (How cron works; the app menu's ⌘N does the same anywhere in Cron), ↩ on a schedule row asks for a job on that schedule | cron |
| E | Edit the cron job; in the form, cancel the edit | cron |
| ⌘S | Save the cron job's edit form | cron |
| E | Edit the wiki page open in Wiki, in the workspace's editor | wiki |
| ⌘Y | Show the wiki page's history; again (Back to page) shows the page | wiki |
| ↩ | Edit the task title (title focused); while editing ↩ saves it, Esc cancels | backlog |
| @ | In the empty Attachments field: pick a file to attach | backlog |
| ⌘I | Show the session's context, or back to the transcript; in the chat panel (no header button there) the key cap at the end of the composer's chip row, or the dashed "Context ⌘I" chip when there are no chips, while the panel shows a Claude session and no text field outside the panel has focus | agents, `SDUI/ComposerView.swift` |
| ⌘R | Open the session's review in the Code section (while it opened or received an open review; the header's Open review, printed also on that review's card in the transcript) | agents |
| ⇧K | Kill the selected process and its children (confirmed) | processes |
| ⇧⌘R | Reveal the selected process's executable in Finder | processes |

## Lists: palette, file picker, pickers, switcher

Pickers are the fuzzy lists for worktrees: the worktree switcher in the window title and Switch Worktree… in the palette. The file picker is the composer's attach list, also opened by @ in Backlog's empty Attachments field. Esc closes the title switcher too.

| Keys | Action |
|---|---|
| ↑ ↓ (arrows in the switcher grid) | Move the selection |
| ⌃J ⌃N / ⌃K ⌃P | Move the selection down / up (text fields) |
| h j k l | Move left / down / up / right (switcher grid) |
| ↑ ↓, j k, ⌃J ⌃N / ⌃K ⌃P, ↩ | Welcome screen (a window with no workspace; not while the switcher is open): move the cursor over Open folder…, Home and the recent workspaces, ↩ opens the one under it. The workspace picker opened from the switcher's + tile uses the same up and down steps; its ← → and h l no longer move between tiles |
| ⌫ | Close the highlighted workspace (switcher grid), like the × at the top right of the highlighted or hovered tile; it stays in Open Recent |
| ↩, ⌃L | Run, open, or choose |
| Esc | Close |

The title picker adds these to the keys above:

| Keys | Action |
|---|---|
| → | On a folder or task row: open it |
| ← | With an empty filter: back to all folders |
| ↩ | On a "show N" or "show N more" row: show the whole section (a click does the same). On the last row, ↓ onto "N more stale" shows them |
| ⌘↩ | Folder actions of the highlighted row's folder (Open its session, Reveal in Finder, Add repos from ‹folder›… on a folder that isn't a repo and holds some, Clean up worktrees…, Remove from ‹workspace›…); ↑ ↓ ↩ inside, Esc back |
| Esc | Inside a folder or task: back to all folders, else close |

## Trees (sidebar focused)

| Keys | Action |
|---|---|
| j / ↓, k / ↑ | Move the cursor, skipping unfolded section labels (a folded one is a stop, so ↩ or l unfolds it); j past the last tree row steps onto the first docked row (Agents: Service), k from there back into the tree |
| l / → | Expand the item, then step to its first child; on a docked row, unfold it upward |
| h / ← | Collapse the item, then go to its parent; on a docked row, fold it |
| ↩ | On a row that opens a page (a session, a run, a graph, a gate): select it and focus the main pane, without folding; on a row with children that opens a page (`mirrorOf`, e.g. a wiki folder with its own page): select it and unfold it, never fold (l / h and → / ← fold it); a row with `open` and children unfolds too, except a session (Agents), which opens and never folds or unfolds its child sessions; else select a leaf item, or expand / collapse an item with children (a docked row unfolds or folds) |
| o | Same as ↩, but an item with children gets selected instead |
| ⌘⌫ | Remove the row under the cursor, always asking first: in Agents a session (Delete Session), an ended run (Remove run…) or a gate (Delete gate…); the row's menu entry with `key: cmd+delete`. On the iPad the same key in a plugin tree (Graphs, Gates) asks the same way |
| gc | Focus the sidebar's first text field, when it has one (Agents and Backlog: the search field, also while Backlog's New task field is open) |
| g1…g9 | Agents: open the Nth numbered session or graph run, as ⌃⌘1…⌃⌘9; while g waits, the numbered rows show their digit |
| gl | Agents: the last session, and back, as ⌃⇥ |
| gn, gN | Agents: next, previous session that needs you, as ⌃⌘J, ⌃⇧⌘J |
| gt, gi | Agents: stay in the tree, go to the prompt |
| ⌃J, ⌃K | Agents: go to the prompt, the transcript, as ⌥⌘↓, ⌥⌘↑ (see § App and windows); other trees move the cursor as j, k |
| ? | Agents: show or hide the keys of the focused pane, as ⌘/ |
| < / > | Previous / next chip of the sidebar's filter row, when it has one (Agents: All · Needs you · Open · Running) |
| Esc, ⌃[ | Fold the open docked row (also from a row inside it); elsewhere back to the main pane (in Agents: the prompt you last typed in, see § App and windows) |
| ⌃L (sidebar on the left), ⌃H (sidebar on the right) | Back to the main pane |

`g` waits for the next key; a key that is not listed is swallowed. The Agents chords (g1…g9, gl, gn, gN, gt, gi) work only in the Agents sidebar; the tree sends them to the plugin as `jump`, `last` and `needs` events. In other trees a key with ⇧, ⌘, ⌃ or ⌥ after g ends the wait and goes on to the app. Defined in `ClubmateKit/TreeKeys.swift` (`TreeChord`).

## Sidebar lists (Terminals, Processes; list focused)

A click on a row, or focus moving to the sidebar, focuses the list.

| Keys | Action |
|---|---|
| j / ↓, k / ↑ | Move the selection |
| ↩ | Open: focus the main pane on the selected item |
| Esc, ⌃L (sidebar on the left), ⌃H (sidebar on the right) | Back to the main pane |

Other keys pass through. Defined in `SDUI/CollectionViews.swift` (`ListNodeView`), keys from `ClubmateKit/TreeKeys.swift`.

## Tables (plugin table focused)

A click on a row, or focus moving to the main pane, focuses the table; the selected row then shows the accent bar. Group headings are skipped.

| Keys | Action |
|---|---|
| j / ↓, k / ↑ | Move the selection |
| l / → | Expand the row, or step to its first child |
| h / ← | Collapse the row, or go to its parent row |
| ↩ | Follow the selected row's link (Cron's Show log, a trigger's Open session; a click on the link does the same) |

Keys with ⌘, ⌃, ⌥ or ⇧ pass through (⇧K stays the plugin's Kill). Defined in `SDUI/TableView.swift`, rows in `ClubmateKit/TableKeys.swift`.

## Graph diagram (Agents main pane, a run or graph page)

Focus moving to the Agents main pane on a run or graph page focuses the diagram of the run or graph; the focused card gets an accent ring (the node that needs you first, else the first card).

| Keys | Action |
|---|---|
| j / k | Focus the next / previous card: layer by layer from the start, top to bottom inside a layer |
| ↩ | Open the focused card's session (a click on a card does the same); on an agent card whose session is not running or waiting, the first ↩ shows its last finished turn's panel (summary, Open ‹Role›) and the second opens the session; on a graph's definition page cards open nothing. With an edge selected (Tab): select that edge's trigger in Triggers (a click on the edge does the same; a definition's edges open nothing) |
| Tab / ⇧Tab | Select the next / previous edge, left to right, and show what it does in words (hovering an edge does the same); j/k go back to the cards |
| - / + (=) / 0 | Zoom the diagram out / in (×1.25, 50…200%) / back to fit the pane width (never below 80%); the − + Fit buttons at its top-right do the same |
| o | Open the focused step card's full output in Neovim (Full output in its output panel does the same; the board's ⌘O is Open Folder…, so Full output has no ⌘ key) |
| h | Open or close History under the diagram (only when the run has moves) |
| Pinch, ⌘/⌃+wheel | Zoom the diagram |

Keys with ⌘, ⌃ or ⌥ pass through. The zoom level and History stay per run (and the zoom per graph) until the app quits. Defined in `SDUI/GraphNodeView.swift` (`ClubmateKit/GraphViewState.swift`).

The boards' "Open in Triggers ⌘7" is not bound: ⌘7 stays the Triggers section; ↩ on the selected edge opens it.

## Drawn views (a `flow`, `lanes` or `runstrip` focused)

Cron's Does, a trigger's chain and empty lists are `flow`s; owners and runs as moves are `lanes`; Cron's runs are a `runstrip`.

When a pane holds several of them (or a table or graph), focus moving to the pane goes to the first one, top to bottom (a ghost-only `flow` takes none); a click on another one gives it the keys. No key moves between them.

| Keys | Where | Action |
|---|---|---|
| j / k | flow | Focus the next / previous card (the focused card gets an accent ring) |
| ↩ | flow | Open the focused card, or the selected edge's target (a click does the same) |
| Tab / ⇧Tab | flow | Select the next / previous edge and show its tooltip |
| ← / → | lanes | Select the previous / next move that opens something (accent ring) |
| ↩ | lanes | Open the selected move (a click on its dot does the same) |
| h | lanes with a title | Open or close the lanes |
| ← / → | runstrip | Select the previous / next run (the newest when none is selected; a click on a run does the same) |
| h | runstrip with a title | Open or close the axis (the Runs · N row with the counts stays; a click on it does the same) |

Keys with ⌘, ⌃ or ⌥ pass through. Defined in `SDUI/GraphNodeView.swift` (`FlowNodeView`, `FlowKeys`), `SDUI/LanesView.swift` and `SDUI/RunstripNodeView.swift`.

## Agent composer (Agents section or Code chat panel, prompt focused)

Focus moving to the Agents main pane (switching to the section, Esc or ⌃L/⌃H from the sessions tree, ⌘N's new session, ⌃⌘↓/⌃⌘↑) lands in the prompt when the session shows one; ⌘N and ⌃⌘↓/⌃⌘↑ wait for that session's prompt, not the one shown before (the rules: [SDUI protocol](sdui-protocol.md) § Focus). Switching back to a section returns focus to where it was when you left it (the prompt, the sessions tree, the terminal), unless that sidebar has been hidden since.

| Keys | Action |
|---|---|
| ↩ (or ⌘↩) | Send the prompt; while a turn runs, add it to the running turn (agents that support steering) or queue it for the next turn. While the command list is open ↩ inserts the command instead |
| ⇧↩ / ⌥↩ | Insert a newline |
| Esc, ⌃[ | Agents: step out to the transcript (to the tree when the session has no messages); the turn keeps running. The first 20 times on this Mac that Esc leaves the prompt of a running turn, the status bar says "⎋ left the prompt · ⌘. stops" for 4 s. With the command list open, Esc closes it first. Code and Wiki chat panels: stop the running turn |
| ⌘. | Agents: stop the running turn (see § App and windows) |
| @ (at the start of a word) | Attach a file through the file picker |
| ⌘V | With an image on the clipboard (a screenshot copied with ⌃⇧⌘4, an image copied in a browser) or image files copied in Finder: attach them as images (`Pasted image.png`) instead of pasting; text on the clipboard pastes as usual. A pasted or dropped path to an image file is taken out of the text and attached too |
| / (at the start of the prompt) | List the agent's commands, filtered by what follows |
| ↑ ↓, ⌃J ⌃N / ⌃K ⌃P, ↩ / ⇥ | Move in the command list, insert the command |
| ⌃K | Agents, no command list open: leave the prompt for the transcript, as ⌥⌘↑ (the prompt keeps the focus when the session has no messages; see § App and windows) |
| ⌃H, ⌃L | Agents: one column left, right, straight from the prompt (main prompt ⌃H → sessions tree; see § App and windows) |
| ⇥ | In an empty field that shows a suggested prompt ('Try "…" · ⇥ to use', the first session on a Mac, [First run hints](agents.md#first-run-hints)): put the prompt in the field |

While an input method is composing (marked text), ↩ and Esc go to it. Defined in `ClubmateKit/Composer.swift` (`ComposerKey`, `takePastedImages`) and `SDUI/ComposerPaste.swift`.

## Agent transcript (Agents section or Code chat panel)

In Agents the transcript moves like the tree: the focused block has a 2 pt accent bar at its left edge. Blocks are the prompt, a thought, the plan, an actions group (each action while the group is unfolded), the answer and a card (in-transcript cards and the pending request docked above the prompt); notes and turn lines are not blocks. Focus arrives on the newest block; a click on a block focuses it. While the bar is on an older block, new output does not move the transcript and the status bar says "↓ N new · G" (only blocks below count, not rows Show earlier loads above; the bar on the pending card follows new output like the newest block). The cursor rules are shared with the iPad (ClubmateProtocol `TranscriptCursor`); the Mac's blocks come from ClubmateKit `TranscriptBlocks`.

| Keys | Action | Where defined |
|---|---|---|
| ⌘D | Full diff: open the turn of the focused edit card in nvim's diffview, else of the newest edit card in the transcript (printed on that card's Full diff) | `SDUI/Agents/TranscriptView.swift` |
| click, Esc from the prompt, ⌥⌘↑, ⌃K | Agents: focus the transcript (status bar READ). A click on message text keeps the text selection |
| ⌃J, ⌥⌘↓ | Agents, transcript focused: go to the prompt of its column, the draft kept, as i | `SDUI/Agents/AgentsFocus.swift`, `ClubmateKit/AgentsKeys.swift` |
| j / ↓, k / ↑ | Agents, transcript focused: next, previous block | `SDUI/Agents/TranscriptView.swift`, `ClubmateKit/AgentsKeys.swift` (`TranscriptKey`), ClubmateProtocol `TranscriptCursor.swift` |
| l / →, h / ← | Agents, transcript focused: unfold the block, then step into an unfolded actions group; fold the block, from an action in a group fold the group | same |
| ↩ | Agents, transcript focused: fold or unfold the block; on the pending card docked above the prompt, give the card the keys; on Show earlier, show earlier rows | same |
| gg, G | Agents, transcript focused: first block (when the transcript shows only its newest rows, Show earlier runs too), newest block, the transcript follows new output again | same |
| ⌘C | Agents, transcript focused, no text selected: copy the focused block as Markdown (an action with its diff or output in a fence, a group as a list); the status bar says "Copied as Markdown". A text selection copies as usual | `SDUI/Agents/TranscriptView.swift`, `ClubmateKit/TranscriptBlocks.swift` |
| gf | Agents, transcript focused, on a Read or Edit block (an action with Open in nvim): open its file in nvim. On other blocks the status bar says "No file on this block". The which-key line after g offers f only on such a block. ⌘O stays Open Folder… | `SDUI/Agents/TranscriptView.swift`, `ClubmateKit/AgentsKeys.swift` (`TranscriptChord`) |
| g1…g9, gl, gn, gN | Agents, transcript focused: as in the tree (Nth numbered session, last session, next and previous that needs you) | `ClubmateKit/TreeKeys.swift` (`TreeChord`), `SDUI/Agents/AgentsFocus.swift` |
| gt, gi, gc | Agents, transcript focused: go to the tree, the prompt, the sidebar's search field | same |
| ? | Agents, transcript focused: show or hide the keys of the focused pane, as ⌘/ | `SDUI/Agents/TranscriptView.swift` |
| i, a | Agents, transcript focused: go to the prompt, the draft kept; physical keys, so they work in any keyboard layout | `SDUI/Agents/TranscriptView.swift`, `ClubmateKit/AgentsKeys.swift` (`TranscriptKey`) |
| Space, ⇧Space | Agents, transcript focused: page down, up | `SDUI/Agents/TranscriptView.swift` |
| ⌃H, ⌃L | Agents, transcript focused: one column left, right, as from the prompt (see § App and windows) | `SDUI/Agents/AgentsFocus.swift`, `ClubmateKit/AgentsKeys.swift` |
| Esc, ⌃[ | Agents, transcript focused: after g, end the wait; else step out to the tree (from an aside's transcript, to its parent's transcript first) | `SDUI/Agents/AgentsFocus.swift` |
| ⌘\ | Ask aside: about the passage selected in an agent message (also the first item of the message's right-click menu), else about the focused answer (the bar on an agent message), else about the whole last answer (also when the bar is on any other block); it opens beside the session (Agents section, a session that can have asides) | `SDUI/Agents/TranscriptView.swift`, `SDUI/MarkdownView.swift` |

After g the status bar shows the which-key line ("g → f file · g first · 1–9 session · l last · n needs you · t tree · i reply"); the next key ends the wait, a key that is not listed is swallowed, a key with ⌘, ⌃ or ⌥ goes on to the app. Open in nvim on an edit card prints gf only while its block has the bar; a click works without a key.

## Agent Browser window (during a handoff)

| Keys | Action | Where defined |
|---|---|---|
| ↩ | Done: hand the page back to the agent (only when the page does not have focus and the find bar is closed; closing the window does the same) | `BrowserWindow.swift` (`ClubmateKit/PopupKeys.swift` `browserKey`) |
| ⌘F | Open the find bar over the page (again: focus it and select the query); the same bar as in the docs viewer | `BrowserWindow.swift` (`ClubmateKit/PopupKeys.swift` `browserKey`), `ClubmateDocument/WebPageFinder.swift` |
| ↩ / ⇧↩, ⌘G / ⇧⌘G, Esc | While the find bar is open: next / previous match, wrapping; Esc closes the bar and gives focus back to the page. ↩, ⇧↩ and Esc only in the bar's field, so the page keeps them | `BrowserWindow.swift` (`ClubmateKit/PopupKeys.swift` `browserKey`), `DocumentNodeView.swift` |

## Permission requests (Agents section or Code chat panel)

A pending request is docked above the composer.

| Keys | Action |
|---|---|
| ⌥1 ⌥2 ⌥3 | Allow once · Always allow… · Reject: the key each option carries (`shortcut`, printed on its button); works while the prompt has a draft and leaves it untouched. ⌥2 (or a click) opens the Always allow confirm instead of answering. Always reject sits in the card's ••• menu without a key. A request whose options carry no keys keeps ⌥N in button order, "always" options without a key |
| ⌥2 again / ⎋ or ↩ | In the Always allow confirm: store the rule and allow / Cancel (the default); nothing is sent before ⌥2 |
| 1 2 3 | The same without ⌥ (bare 2 opens and confirms Always allow too), only while the session's prompt is focused and empty, the session's transcript is focused (not while g waits), or the request card has focus (click it, or ↩ on its block in the transcript); typed into a prompt with a draft, the digit goes into the draft |
| y / n | Allow once / Reject once, under the same condition as bare digits |

Outside the Always allow confirm, Esc does not answer the request: in Agents it steps out of the prompt and ⌘. stops the turn; in the Code and Wiki chat panels Esc stops the turn. Defined in `ClubmateKit/PermissionKeys.swift` and `SDUI/Agents/PermissionCardView.swift`.

## Repo requests (Agents section or Code chat panel)

A connect request (`Connect a repo?`), a Land request (`Land ‹repo›?`) or a Done gate's held-turn card is docked above the composer. The card has the keys as soon as it shows, unless you are typing, and until you click the composer; ⌥1–⌥3 work either way.

| Keys | Action |
|---|---|
| ⌥1 ⌥2 ⌥3 | Connect card: Connect the ticked repos · other folder for the focused repo · Don't connect |
| Space | Connect card: un-tick or tick the focused repo; un-ticked repos are skipped |
| ↑ ↓ | Connect card: move between repos; moving past the third opens the folded ones |
| ⌥1 ⌥2 ⌥3 | Land card: Review… (opens the repo's diff, never merges) · Merge · Leave it unlanded… (⌥3 again confirms, Esc or ↩ cancels) |
| 1 2 3, y n | Connect card: the same without ⌥ (y Connect, n Don't connect), under the rule of § Permission requests: the card has the keys, the prompt is empty, or the transcript is focused |
| 1 2 3, y n | Land card: the same without ⌥ (y the `allow_once` answer: Review…, or Merge on a gate's rounds card; n the `reject_once` answer: Leave it unlanded…, which asks to confirm), only while the card itself has the keys (it takes them when it shows, a click, or ↩ on its block in the transcript); never from an empty prompt or the transcript, so a stray digit does not merge |
| ⌥1 ⌥2 ⌥3 | Land review card ("‹graph› still asks for changes after N rounds"): Land anyway · One more round · Stop |
| ⌥1 ⌥2 ⌥3 | Held turn card ("‹graph› still asks for changes after N rounds" / "‹graph› ended without a verdict"): Accept as done · One more round (Check again on "Couldn't check this turn against its gates") · Stop |
| ⌥1 ⌥2 ⌥3 | Held tool call card ("‹graph› still asks for changes after N rounds" / "N gates still ask for changes"): Run anyway · One more round · Stop |

## Agent questions (Agents section or Code chat panel)

A pending question (AskUserQuestion or an MCP form) is docked above the composer like a permission request.

| Keys | Action |
|---|---|
| ⌥1 ⌥2 ⌥3 ⌥4 ⌥5 ⌥6 ⌥7 ⌥8 ⌥9 | Answer with that option (printed on each option row) when the question has a single list of options; the answer is sent at once, with whatever is typed in Other as a note. The key after the last option picks "Other…": it opens the field and sends nothing |
| ↩ | In a text field of the question (e.g. "Other"): send the form |
| ⌘↩ | Send the form (printed on Send), also from an empty composer, when something is answered ([Needs an answer](agents.md#needs-an-answer)); until then it does nothing. A composer with text sends its message instead. Not on a card a pick already answers (one question, no Other), and not while a terminal or Neovim has focus |

⎋ stays Stop the running turn and answers nothing; Skip has no key. With 2+ questions the rows have no keys: click them, or Tab to a row and press Space. Defined by the `choice` option `shortcut` props and Send's `shortcut` in `plugins/agents/elicit_view.go`, ranked in `SDUI/KeyMonitor.swift` (`KeyClaim`).

## Quiz questions (Agents section, guided code reading)

A quiz question ([agents § Quiz questions](agents.md#quiz-questions)) is docked above the composer and takes the keys while it has focus (it takes focus when it comes, unless the composer has text). Same keys on the iPad with a keyboard.

| Keys | Action |
|---|---|
| ↑ ↓ | Move the cursor line in the excerpt |
| ⇧↑ ⇧↓ | Make the pick a range from the cursor (also a drag over the lines) |
| ↩ | Pick the cursor line or range (also a click); a pick never sends |
| 1 2 3 | Rate guess · think so · sure. On a line, range or choice question the rating sends after a 2 s hold; on a text or value question it only rates |
| a b c d | Pick that option of a choice question |
| ⌘Z | During the 2 s hold: take the answer back to the pick (the card's Undo) |
| ⌘↩ | In the composer: send the typed words as the answer (needs a rating; with none the focus goes to the chips) |
| ⌥G | Open in nvim: the excerpt's file at the cursor line. ⌘O stays Open Folder…, and gf needs the transcript's focus, while letters typed on the card go to the composer |
| ⌥Q | Fold the card to its bar and back |

Other letters typed while the card has focus go to the composer, which is the question's Other. Skip has no key. Defined by the `request.quiz` node in `plugins/agents/quiz_view.go`, handled in `SDUI/Agents/QuizCardView.swift` (iPad: `client/Companion/Plugins/Agents/QuizViews.swift`).

## Guide steps and concept map (Agents section, guided code reading)

The step controls sit under the latest step of a guide session ([agents § Guide steps](agents.md#guide-steps)) and hide while a question is open; a key works only while its control shows and is enabled. Same keys on the iPad with a keyboard.

| Keys | Action |
|---|---|
| ⌥T | I know this · test me |
| ⌥S | Show me |
| ⌥D | Go deeper: open the step's aside (⌘[ goes back to the session, as in every aside) |
| ⌥M | Smaller steps |
| ⌘↩ | Continue, only on a step without a check and only while the composer is empty; with text ⌘↩ sends the message |
| ⇧⌘M | Show or hide the Tour pane (the concept map, [agents § Concept map](agents.md#concept-map)) |
| ↑ ↓ | Move the selected concept in the map's tree (pane or popover) |
| ↩ | Show the selected concept's offers (Ask again, Back to step N) |

Defined by the `guide.steps` and `guide.strip` nodes in `plugins/agents/guide.go` and `view.go`, handled in `SDUI/Agents/GuideViews.swift` (iPhone and iPad: `client/Companion/Plugins/Agents/GuideViews.swift`).

## Repos card (Agents section)

Only while the card has focus (a click in it, its header pill), never in the composer.

| Keys | Action |
|---|---|
| ↑ ↓, J K | Move between repos |
| ↩ | On a failed repo: Ask it to fix. On any other: open or fold the row |

Defined by the `keys` scope in `plugins/agents/repos_view.go`, handled in `plugins/agents/repos.go`.

## Asides (Agents section)

An aside opens beside its parent (a split in a wide window, under it in a narrow one) or on its own page. Its keys are the `shortcut`s of its header buttons, so they work while the aside is on screen.

| Keys | Action |
|---|---|
| ⇧⌘\ | Full page ↔ Beside: the aside's own page with the parent's question pinned on top, or back beside the parent |
| ⌘[ | From the aside to its parent, scrolled to the option or passage it is about; from the full page the aside goes back beside it |
| ⌘] | From a parent to its aside beside it; again steps through its asides, newest first (the header's Aside / Asides · N) |
| ⇧⌘↩ | Send back…: the panel at the end of the aside |
| ⌘↩ / ⎋ | In the Send back panel: send / cancel (while the panel has focus; ⎋ in its text field goes back to the panel) |
| ⇧⌘K | Keep: the aside becomes an ordinary session |
| ⌥⌘⌫ | Close the aside: with focus anywhere in the aside beside its parent, or on its full page (as the selected session); the same as its header's × |

⌘W doesn't close an aside. Defined by the header buttons in `plugins/agents/aside_view.go` and the `keys` scope in `plugins/agents/aside_send.go`.

## Open items (Agents section, card or Open queue)

⌥O works anywhere in Agents; the rest only while the card or the queue has focus (a click in it, Open in the sidebar), never in the composer or a text field.

| Keys | Action |
|---|---|
| ⌥O | Open filter: every open item of the workspace, one queue |
| ↑ ↓, J K | Move between items; the offers are walked one by one |
| 1 2 3 4 | Pick that option of the focused decision, or tick that offer |
| X | Tick the focused offer |
| O | Other…: opens the focused decision's field; Esc or ↩ in it goes back to the card |
| A, then 1 2 3 4 or O | Ask aside about that option of the focused decision (or its Other), without picking it: A arms the card (the status bar shows "1–4 ask aside about · ⎋ cancel"), the number opens the aside beside the session or reopens it; ⎋ or any other key disarms. Nothing while the session can't have asides |
| ⌘↩ | Send answers; in the queue every answered session's, one turn each |
| L | Later: keep them open and fold the card |
| B | To backlog: one task per open item of the session (not on a night session's card, whose offers never go to the backlog) |
| ⌫ | No to the focused item |
| ⇧⌫ | No to all of the session's open items |
| ↩ | In the queue: open the item's session |

Defined by the `keys` scope in `plugins/agents/open_view.go`, handled in `plugins/agents/open_actions.go`. ⌥O is the `shortcut` on the sidebar's Open chip.

## Companion on iPad / Fold (hardware keyboard)

| Keys | Action |
|---|---|
| ⌥1 ⌥2 ⌥3 | The pending permission card's keys from the agents plugin (`shortcut` in its `card`): ⌥1 Allow once, ⌥2 opens the Always allow confirm sheet (nothing is stored until its Always allow button), ⌥3 Reject; Always reject in ••• has no key |
| ⌘↩ | Send the composer's message (Session menu) |
| ⌥1 ⌥2 ⌥3 | On a graph run's screen, a step waiting for approval: ⌥1 Allow once, ⌥2 opens the Always allow confirm sheet (only when the step's command can be a rule), ⌥3 Reject |
| ⌘↩ | On a graph run's screen held at a round cap: Continue (Session menu) |
| ⌘F | On an open wiki or doc page: the system find bar (WebKit's find interaction; also Find in Page in the page's •••) |
| ⌥↩, Esc | In the voice panel's confirmation of a dangerous action (any screen): Yes, No. ⌥↩ is ignored for the first 0.6 s, like a tap on Yes |
| Esc | In the voice panel while an utterance waits its 1 s before going to the assistant (any screen): Cancel |
| Esc | On a wiki or doc page in reading mode: leave reading mode (the bars come back) |
| ⇧⌘F | Focus the current tab's search field: Agents, Backlog or Wiki (on the iPhone layout it pops to the tab root first); off on More (Edit menu, Search) |
| ↩ | In the Backlog list's search field (iPhone too), with a number that pins a task: open that task on the Backlog stack; typing never opens it |
| ⌃⌘S | iPad: show or hide the current tab's list column (list + detail ↔ detail only), the same as the sidebar button at the edge of the bar; where the detail would keep less than 640 pt (11-inch portrait) the list slides over the open page. Each tab keeps its state; from the reading view it leaves the reading view (View menu, Show List / Hide List) |
| ⌘B | iPad, on a wiki page: show or hide the chat panel (the Chat button in the page toolbar; View menu, Show Chat / Hide Chat). A 400 pt panel beside the page in a window wider than 1000 pt, which hides the list column until it closes; a sheet otherwise. ⌃⌘S while the panel is beside the page closes it. In the picker (focus in the panel) ↩ takes the suggested row, ↑ ↓ j k move, `/` focuses the filter and N starts a new session; All sessions has no ⌘[ on the iPad, ⌘[ stays Back in the detail column |
| ⌃⌘F | iPad: reading view on or off on a wiki page, a doc page or a session: no bars, no columns, prose min(780, width − 56) pt centred (a session: 680 pt), code and tables up to 1060 pt, the wiki margin folded; leaving restores the columns (View menu) |
| Esc, ⌃[ | iPad, in order: leave the reading view → close the list over the open page (11-inch portrait; when the list was the focused pane, the composer then gets focus) → with an Agents session open, step out: composer → transcript → session list → the composer you left. The focused pane shows a 2 pt accent bar at its leading edge (transcript, list). Empty stops are skipped: a session with no messages steps from the composer to the list; a closed session has no composer stop; the iPhone layout has no list stop. A sheet or dialog over the session, and the voice panel's Cancel or No, take the key first. Never stops a turn (View menu: Close, or Step Out while the list is the focused pane; the ⌃[ item is "… (Alternate)"). Esc in the composer while it holds marked text (an input method) goes to the input method. The list stop ends on a tab switch, a push or pop, and in the iPhone layout |
| ⌘. | Stop the running turn, from the list, the transcript or the composer: opens the same Stop confirmation as the Stop button. Only while a turn runs (Session menu, Stop) |
| ⌥⌘← ⌥⌘→ ⌥⌘↑ ⌥⌘↓ | Go straight to a pane, also from the composer: the session list (shows a hidden list), the open session's composer, the transcript (only when it has messages), the composer (Session menu, Go to List / Session / Transcript / Composer) |
| ⌃K, ⌃J | The same as ⌥⌘↑, ⌥⌘↓: the transcript, the composer, also from the composer, where ⌃K no longer deletes to the end of the line (Session menu, Go to Transcript / Composer (Alternate)) |
| ⌃H, ⌃L | The same as ⌥⌘←, ⌥⌘→: the list, the session, also from the composer, where ⌃H no longer deletes backward (Session menu, Go to List / Session (Alternate)) |
| i, a | Transcript focused (2 pt accent bar at its leading edge): focus the composer, the draft is kept |
| Space, ⇧Space | Transcript focused: page down, page up |
| j k ↓ ↑, l → h ←, ↩ | Transcript focused: next, previous block (2 pt accent bar at the block's leading edge; focus arrives on the newest block), unfold, fold, fold or unfold. The same rules as the Mac (CompanionKit `TranscriptCursorRows`, ClubmateProtocol `TranscriptCursor`); on the iPad layout an actions group lists its first 3 actions as blocks, and l / h show all or 3 |
| gg, G | Transcript focused: first block (Show earlier runs too), newest block. While the bar is on an older block, new rows do not move the bar and a note "↓ N new · G" shows above the composer for 4 s (rows loaded above by Show earlier are not counted) |
| ⌘C | Transcript focused: copy the focused block as Markdown |
| 1 2 3, y n | Transcript focused: answer the pending permission card, as ⌥1 ⌥2 ⌥3 (y Allow once, n Reject once) |
| g1…g9, gl, gn, gN, gt, gi | Transcript focused: Nth numbered session, last session, next and previous that needs you, the session list, the composer; the which-key line shows above the composer while g waits |
| ?, ⌘/ | Transcript focused (⌘/ while a session is open, Session menu, Transcript Keys, also in the hold-⌘ list): a sheet with the transcript keys |
| Esc (note) | When Esc leaves the composer of a running turn, a note "⎋ left the prompt · ⌘. stops" shows above the composer for 4 s, the first 20 times on this device (count in the app's preferences, `agents.stopHintShown`) |
| ⌘[ | Back (the chevron or the back chip); on the iPad only inside the detail column (Go menu) |
| ⌘1 ⌘2 ⌘3 ⌘4 | Tabs Agents · Backlog · Wiki · More (⌘3 only when the workspace has a wiki); on the iPad the tabs are buttons at the bottom of the list column, and the keys also work while the list is hidden (Go menu) |
| ⇧⌘I | Inbox, pushed on the current tab (Go menu) |
| ⌃⌘↓, ⌃⌘↑ | Next, previous session among the rows the Agents list shows (asides and steps included, inbox copies skipped), wrapping around (Go menu) |
| ⌃⌘1…⌃⌘9 | The Nth row of the Agents list that `agents.sessions` marks `numbered` (top-level sessions and graph runs), in the order shown, inbox copies skipped (Go menu) |
| ⌃⌘J, ⌃⇧⌘J | Next, previous session that needs you, in the plugin's `needsRank` order, oldest first (Go menu) |
| ⌃⇥ | The last session opened in this window, and back; a closed one drops out (Go menu) |
| ⌘N | The current tab's create action: New Session on Agents, New Task on Backlog; nothing on Wiki and More (File menu) |

Listed in the iPad menu bar under Session. Defined in `client/Companion/Plugins/Agents/SessionKeys.swift`, key mapping in CompanionKit `PermissionRequest.keyed`, the run card's in `RunRequestCard` (`client/Companion/Plugins/Agents/RunDetailView.swift`); the pane ladder, the ⌥⌘ arrow targets, the ⌃H ⌃J ⌃K ⌃L map (`controlKeys`) and the hint count in CompanionKit `PaneLadder.swift`; ⌘., the ⌥⌘ arrows and ⌃H ⌃J ⌃K ⌃L in `SessionCommands` (`SessionKeys.swift`); Esc, ⌃[, ⌃H, ⌃J, ⌃K and ⌃L in the composer in `ComposerTextView.swift`; the transcript focus, its Esc, ⌃[, i, a, Space and the hint in `client/Companion/Plugins/Agents/SessionDetailView.swift`; the list's focus bar in `AgentsPlugin.swift` (`SessionListView`); the voice keys in `client/Companion/App/VoicePanel.swift`; reading mode's Esc in `client/Companion/App/ReadingModeModifier.swift`; ⇧⌘F (`TabSearchCommands`) and the per-tab field (`TabSearch`) in `client/Companion/App/TabSearch.swift`; the Backlog search field's ↩ in `client/Companion/Plugins/Backlog/BacklogPlugin.swift` (`TaskListView`); ⌃⌘S, ⌃⌘F, the iPad Esc and ⌃[ (`AppNavigation.stepOut`), ⌘[, ⌘1–⌘4, ⇧⌘I, ⌘N and the session keys (⌃⌘↓ ⌃⌘↑ ⌃⌘1…⌃⌘9 ⌃⌘J ⌃⇧⌘J ⌃⇥, with CompanionKit `SessionJumps`) in `client/Companion/App/AppKeys.swift` (View, Go and File menus).

## Review (Code section, a review tab current in Neovim)

The review's file panel is the diffview fork's, inside nvim; its keys are in the nvim table below. The app adds:

| Keys | Action | Where defined |
|---|---|---|
| ⌘↩ | Send review: opens the Send sheet for the review in Neovim's current tab (the status bar button "Send review"; also while Neovim has focus, since ⌘↩ never reaches it; only while the review has open comments; with an agent's question in the chat panel only while Neovim has focus, elsewhere ⌘↩ goes to the question) | `plugins/editor-nvim/view.go`, `plugins/editor-nvim/review_send.go` |
| ⌘↩ | In the Send sheet: send the open comments to the chosen session | `plugins/editor-nvim/review_send.go`, `SDUI/FormSheet.swift` |
| ⎋ | In the Send sheet: cancel | `SDUI/FormSheet.swift` |

## Chat panel session picker (Code or Wiki, the picker has focus)

The picker keys act only while the picker has focus, so they never reach Neovim, a terminal or the composer. Defined by the agents plugin (`plugins/agents/chat_panel.go`); the client draws the list and its filter.

| Keys | Action | Where defined |
|---|---|---|
| ↩ | Take the focused row (the suggestion when the picker opens); in the filter field, the first match | `plugins/agents/chat_panel.go`, `SDUI/CollectionViews.swift` |
| ↑ ↓, J K | Move between the rows | `SDUI/CollectionViews.swift` |
| N | New session (in Code with a review current: New session for this review) | `plugins/agents/chat_panel.go` |
| / | Focus the filter field; ⎋ clears it | `SDUI/CollectionViews.swift` |
| ⌘[ | In the chat after the pick: All sessions, back to the picker | `plugins/agents/chat_panel.go` |

## Code chat panel starters (session with no turns yet)

Starters have no keys: ⌥1–⌥3 answer permission requests only. A click sends the starter prompt; "Explain" and "Write a test" attach the file open in Neovim. Defined by the agents plugin (`plugins/agents/view.go`).

## VS Code (VS Code section)

While a VS Code page (a `webview` node) has focus, these keys go to it instead of the app menu. Every other app menu key stays with Clubmate, as over nvim and terminals; keys the menu doesn't use reach VS Code.

| Keys | Action | Where defined |
|---|---|---|
| ⌘W, ⌘N, ⌘O, ⌘P, ⇧⌘P | Go to VS Code (close editor, new file, open, quick open, command palette) | `WebViewHost.swift` (`CodeWebView`) |
| ⌘K | Stays the Command Palette, so VS Code's ⌘K chords don't work | `ClubmateApp.swift` |
| ⌘B | Stays Toggle Chat (VS Code has no chat panel, so it does nothing), so VS Code's sidebar toggle is shadowed | `ClubmateApp.swift` |
| ⌘J | Stays Switch Popup Focus; use ⌃` for VS Code's panel | `ClubmateApp.swift` |

## nvim (Clubmate distribution, leader `,`)

Defaults of the bundled nvim distribution (`nvim-distro/`). Keys in normal mode unless noted. The user layer (`~/.config/clubmate/lua/user/keymaps.lua`) can add or override any of them.

| Group | Keys | Action | Where defined |
|---|---|---|---|
| Find | ,ff / ,fg / ,fb / ,fh | Files, live grep, buffers, help tags (snacks picker) | `lua/clubmate/keymaps.lua` |
| Find | ,fr / ,fs / ,fi / ,fd | LSP references, document symbols, implementations, buffer diagnostics | `lua/clubmate/keymaps.lua` |
| Find | ,fy | Registers | `lua/clubmate/keymaps.lua` |
| Find | ,, | Resume the last picker | `lua/clubmate/keymaps.lua` |
| Code | gd / gD | Go to definition, declaration | `lua/clubmate/keymaps.lua` |
| Code | gr | Rename symbol | `lua/clubmate/keymaps.lua` |
| Code | gl | Show the diagnostic under the cursor | `lua/clubmate/keymaps.lua` |
| Code | [d / ]d | Previous, next diagnostic (opens its float) | `lua/clubmate/keymaps.lua` |
| Code | ,cp | Copy the file's relative path | `lua/clubmate/keymaps.lua` |
| Code | ,cb / ,cc | `go build ./...`, `go test -c ./...` | `lua/clubmate/keymaps.lua` |
| Code | ,ci | Toggle inlay hints | `lua/clubmate/keymaps.lua` |
| Code | ,gt | Jump between a Go file and its `_test.go` | `lua/clubmate/keymaps.lua` |
| Code | af / if, ac / ic (visual, operator) | Function, class text objects (outer / inner) | `lua/clubmate/plugins/lang.lua` |
| Code | ⇥ / ⇧⇥, ⌃N / ⌃P, ↩, ⌃E, ⌃Space (insert) | Completion: next / previous, show + next / previous, accept, cancel, show | `lua/clubmate/plugins/editor.lua` |
| Git | ]h / [h | Next, previous hunk | `lua/clubmate/plugins/git.lua` |
| Git | ,gb / ,gB | Blame line, full blame | `lua/clubmate/plugins/git.lua` |
| Git | ,gp / ,gr | Preview hunk, reset hunk | `lua/clubmate/plugins/git.lua` |
| Git | ,gs (normal, visual) | Stage hunk, stage the selected lines | `lua/clubmate/plugins/git.lua` |
| Git | ,lg / ,lf | LazyGit, LazyGit history of the file (needs `lazygit` on PATH) | `lua/clubmate/keymaps.lua` |
| Diff | ,dv | Diffview of the working tree | `lua/clubmate/plugins/git.lua` |
| Diff | ,dm / ,dM | Diff against the merge base with `main`, with a picked branch (recently committed first) | `lua/clubmate/plugins/git.lua` |
| Diff | ,dh / ,dq | File history, close Diffview | `lua/clubmate/plugins/git.lua` |
| Diff | q / e (Diffview) | Close, edit the file; `s` stage, `r` mark reviewed in the file panel | `lua/clubmate/plugins/git.lua` |
| Diff | zc / zt / zg / zr (Diffview file panel) | Show or hide code files, test files, generated files, reviewed files (`h` still folds a folder) | diffview.nvim `config.lua` |
| Diff | t (Diffview file panel) | Show folders nested (where each file sits) or one folder per parent directory (the default); holds for the nvim session | diffview.nvim `config.lua` |
| Diff | ]f / [f (Diffview diff panes and file panel) | Open the next, previous file to review (skips hidden files) | diffview.nvim `config.lua` |
| Diff | ⇥ / ⇧⇥, [F / ]F (Diffview diff panes and file panel) | Open the diff of the next / previous file, the first / last file | diffview.nvim `config.lua` |
| Diff | gf / ⌃w ⌃f / ⌃w gf (Diffview diff panes and file panel) | Open the file in the previous tab, in a split, in a new tab | diffview.nvim `config.lua` |
| Diff | ,e / g⌃x / g? (Diffview) | Focus the file panel, cycle the layout, show the key help (`q` or Esc closes it) | diffview.nvim `config.lua` |
| Diff | j / ↓, k / ↑, ↩ / o / l (Diffview file panel) | Move the cursor; open the entry's diff | diffview.nvim `config.lua` |
| Diff | - / S / U / X (Diffview file panel) | Stage or unstage the entry (as `s`; `-` is not the file tree toggle here), stage all, unstage all, restore the entry to the left side | diffview.nvim `config.lua` |
| Diff | i / f / R / L (Diffview file panel) | Switch between list and tree, flatten single-child folders, refresh the list, open the commit log | diffview.nvim `config.lua` |
| Diff | zo / za / zR / zM, ⌃b / ⌃f (Diffview file panel) | Open, toggle a folder, open all, close all folders; scroll the diff up / down | diffview.nvim `config.lua` |
| Diff | [x / ]x, ,co / ,ct / ,cb / ,ca, dx (Diffview diff panes, a file with conflicts) | Previous / next conflict; choose ours / theirs / base / all, delete the conflict; ,cO ,cT ,cB ,cA dX do it for the whole file (file panel too). In a Diffview diff pane `,cb` is this, not `go build` | diffview.nvim `config.lua` |
| Diff | g! / y / L / X (Diffview file history panel) | Log options, copy the commit hash, show the commit, restore the file from the entry | diffview.nvim `config.lua` |
| Diff | ,b (Diffview) | Show or hide the review panel; in the narrow (inline) layout it opens as a drawer floating over the diff's left edge | diffview.nvim `config.lua` |
| Diff | Esc (review drawer) | Close the drawer | diffview.nvim `scene/views/diff/file_panel.lua` |
| Diff | gS (Diffview review panel) | Send review: the app opens the Send sheet (as ⌘↩ in the app) | diffview.nvim `config.lua` |
| Diff | click, ↩ (review panel header and footer) | A click on a file or folder row opens or folds it; on a filter checkbox, Send, Copy or a comment it does what the item says (a comment: jump to it); ↩ in the header and footer windows does the same as a click. In the narrow strip in the tabline a click on ‹ › opens the previous / next file, on ☰ the drawer | diffview.nvim `scene/views/diff/file_panel.lua` |
| Diff | ⌃w j / ⌃w k (review panel file tree) | From the file tree ⌃w j enters the footer and ⌃w k the header; focus stays there (cursorline on) so ↩ works on comment rows and checkboxes; ⌃w k / ⌃w j go back to the tree and do nothing at the panel's edges; ⌃w w still cycles past them | diffview.nvim `scene/views/diff/file_panel.lua` |
| Review | ,rc (normal, visual) / ,rl | Add a review comment (straight to its text, no type to pick: it is saved as a note), list comments | diffview.nvim `nrev` |
| Review | ]a / [a | Next, previous review comment | diffview.nvim `nrev` |
| Review | ,rr | Reply to the comment under the cursor (a reply to a resolved comment reopens it and moves its resolution note into the thread); the agent answers with `nrev reply` | diffview.nvim `nrev` |
| Review | ,rx / ,rd / ,re | Resolve or reopen, delete, export comments to the clipboard | diffview.nvim `nrev` |
| Debug | ,db / ,dB | Toggle breakpoint, conditional breakpoint | `lua/clubmate/plugins/debug.lua` |
| Debug | ,dc / ,do / ,di / ,dO | Continue, step over, into, out | `lua/clubmate/plugins/debug.lua` |
| Debug | ,dr / ,dl / ,dx | REPL, run last, terminate | `lua/clubmate/plugins/debug.lua` |
| Debug | ,dt / ,du | Debug the Go test under the cursor, toggle the DAP UI | `lua/clubmate/plugins/debug.lua` |
| Test | ,tr / ,tf / ,ta | Run the nearest test, the file's tests, all tests | `lua/clubmate/plugins/test.lua` |
| Test | ,td / ,tl / ,tL | Debug nearest, run last, debug last | `lua/clubmate/plugins/test.lua` |
| Test | ,ts / ,to / ,tp / ,tc | Summary, output, output panel, clear the panel | `lua/clubmate/plugins/test.lua` |
| Test | ,tw | Attach to the running test | `lua/clubmate/plugins/test.lua` |
| Test | [t / ]t | Previous, next failed test | `lua/clubmate/plugins/test.lua` |
| Spell | ,ss | Toggle spell checking | `lua/clubmate/keymaps.lua` |
| Spell | ,sn / ,sp | Next, previous misspelling | `lua/clubmate/keymaps.lua` |
| Spell | ,sa / ,sf | Add the word to the dictionary, fix it | `lua/clubmate/keymaps.lua` |
| Buffer | ,bd | Close the buffer | `lua/clubmate/keymaps.lua` |
| Buffer | ,ns | New scratch markdown buffer | `lua/clubmate/keymaps.lua` |
| Buffer | - / + | Toggle the file tree, the symbols tree (neo-tree, right side) | `lua/clubmate/plugins/editor.lua` |
| Buffer | Esc | Clear the search highlight | `lua/clubmate/keymaps.lua` |
| Buffer | ,q | Close the quickfix list | `lua/clubmate/keymaps.lua` |
| Buffer | v / s (quickfix) | Open the entry in a vertical, horizontal split | `lua/clubmate/autocmds.lua` |
| Docs | ,mp (or `:ClubmateDocOpen`) | Open the current Markdown/mermaid/HTML file in the docs viewer | `lua/clubmate/keymaps.lua` |
| Folding | Space | Toggle the fold under the cursor | `lua/clubmate/keymaps.lua` |
| Folding | zV | Close all folds, or open all when none is open | `lua/clubmate/keymaps.lua` |
| Windows | ⌃H / ⌃J / ⌃K / ⌃L | Move to the left, lower, upper, right window | `lua/clubmate/keymaps.lua` |
| Windows | < / > (visual) | Indent and keep the selection | `lua/clubmate/keymaps.lua` |
| Windows | J / K (visual) | Move the selection down, up | `lua/clubmate/keymaps.lua` |

Paths are relative to `nvim-distro/`.

## Ergonomics notes

- The status bar names the focused Agents pane at its left: TREE, READ (the transcript) or TYPE (the prompt), and shows that pane's keys at its right; a note ("⎋ left the prompt · ⌘. stops", "No session open · ↩ open · ⌘N new") replaces the keys for 4 s.
- Esc means two things: in Agents it steps out of a pane and ⌘. stops the turn; in the Code and Wiki chat panels Esc still stops the turn. Consider the Agents model there too.

- ⌘W does different things by focus and section: the popup, VS Code, the selected terminal in Terminals, the window without a workspace, nothing in other sections.
- ↩ and o in a tree differ only on items with children (↩ toggles, o selects). Consider merging them.
- nvim: `gr` (rename) waits for `timeoutlen` before firing, because nvim 0.12's built-in `grn`/`grr`/`gri`/`gra` start with it.
