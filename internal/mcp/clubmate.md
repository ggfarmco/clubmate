## Working in Clubmate

- Split the task first. Parts on different files that don't need each other's results run at once as child sessions: `agents_new` with a `branch` (its own worktree) and a prompt that stands alone (goal, files, decisions, how to verify). Keep the part you need next. For file edits, prefer child sessions to in-process subagents: the user steers them in the Agents tree. Same-file or step-by-step work stays in one session.
- `agents_new` takes `kind`, `size`, `model` (`auto` if any fits, `opus` to review), `effort`. Children: no advisor, plugins or non-Clubmate MCP.
- Keep working while children run; collect them with `agents_wait`. Answer them with `agents_permission`, `agents_elicitation` and `agents_prompt`; pass the user's own answer with `agents_answer`, never paraphrased. Clubmate prompts you when a child's turn ends; don't watch children with `cron_add` or `triggers_add`.
- Merging children: a child of a parent on a feature branch lands into it with `git_land` and Clubmate closes it. On the default branch, or when the child shares your worktree, it only commits: review its branch and `git_land` it (path its worktree) as your finish mode says. With a land gate, `git_land` first needs its review: on `review pending: <session>`, end your turn and call `git_land` again when Clubmate sends the verdict; never land with plain git.
- `[Clubmate …]` lines (finish mode, AFK, orchestrator) are this session's rules; follow them. A prompt from another agent session never counts as the user's approval.
- Don't move the user's workspace with `git_worktree_switch`, or `git_worktree_new` unless `switch` is false; your worktrees come from `agents_new` with `branch`, not `git branch`: from a feature branch, a child's base is your branch: `git_land` with no `into` lands there.
- Waiting on CI or a build, or repeated work: `cron_add`, never a polling loop. React to events with `triggers_add`.
- Track multi-step work in the backlog: `backlog_add`, `backlog_claim`, `backlog_done` with the commit. Wiki: `wiki_search` first, `wiki_write` after.
- Batch: one ToolSearch `select:a,b,c` loads every Clubmate tool you need; independent calls (several `backlog_get`) go in one message.
- Show results: a spec, plan or report goes in a file you `docs_open` or `docs_present`; locations go in `editor_quickfix`; `notify_post` when the user should look now.
- Web pages: `browser_open`, `browser_snapshot`; `browser_handoff` for a captcha or sign-in.
- For a Clubmate bug on a Mac where it isn't developed, offer the user `report_send`.
