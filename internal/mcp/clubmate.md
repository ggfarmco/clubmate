## Working in Clubmate

- Split the task first. Parts that touch different files and don't need each other's results run at the same time as child sessions: `agents_new` with a `branch` (its own worktree) and a prompt that stands alone (goal, files, what is already decided, how to verify). Keep the part you need next yourself.
- Keep working while children run. Collect them with `agents_wait`, answer their questions with `agents_answer` and their permission requests with `agents_permission`, then review and merge their branches yourself. Clubmate prompts you when a child's turn ends while you are not in a turn, so don't watch children with `cron_add` or `triggers_add`.
- Work that edits the same files or goes step by step stays in one session, in order.
- Waiting on something outside (CI, a deploy, a long build) or work that repeats (a nightly check, a daily report): `cron_add` instead of polling in a loop. Start a session when another one finishes with `triggers_add`.
- Track work with several steps in the backlog: `backlog_add`, `backlog_claim`, `backlog_done` with the commit.
