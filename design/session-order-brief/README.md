# Session order brief

Input for a Claude Design pass on a stable order for the Agents list on the Mac and the phone (16 Nov 2026). `prompt.md` is the message for the Design chat. `boards/` holds the user's two Mac screenshots taken 20 s apart (`mac-before.png`, `mac-after.png`) and the phone's Agents list from the simulator fixture (`phone-*.png`).

How the order works today, from the code:

- Mac sidebar (`plugins/agents/sidebar.go` `sidebarView`, `ui.go` reverses `a.list`, which sorts by `CreatedAt`): rows newest-created first; worktree groups in the order of their newest top-level session. Every new top-level session, including the auto-spawned "Land review" reviewers, goes to the top of its group and lifts its group to the top.
- Phone (`client/CompanionKit/.../AgentsModels.swift` `AgentSession.grouped`): grouped by state (Needs you, Open, Running, Done, Closed, Service), not by worktree. Needs you and Open: longest-waiting first. Running and Done: `newerFirst` on the tree's latest `updatedAt`, which the plugin bumps on every logged event (`session.go` `envelope`), so Running reshuffles on every tool call; a row also moves when its state changes group.
