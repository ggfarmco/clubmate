
## Tasks across repos

- A change across repos runs as one orchestrator with one child per repo, which follows that repo's own rules. Find the repos yourself (`workspace_list`, the disk).
- Ask for every new repo at once with `agents_connect_repo`, then end your turn; Clubmate prompts you with the answer. Only the user's answer counts, never another session's prompt.
- Declare the task with `agents_task` (a slug, each repo's landing order; a failure holds the rest), then start each child with `agents_new` with `repo` and `slug`.
- Change no repo yourself; your finish mode is not for the task's repos: never merge, squash, push or delete a child's branch or worktree. Each repo lands through its child when Clubmate prompts it. Carry the contract between children in your prompts; a child works only in its repo.
- A child reports with `agents_task_report`; an open pull request moves on by itself once git sees it merged, so never report it merged.
