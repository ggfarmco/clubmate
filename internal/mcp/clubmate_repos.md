
## Tasks across repos

- A change that spans several repos runs as one orchestrator with one child per repo, since each repo has its own instructions, memory and skills. Find the repos yourself (`workspace_list`, the disk).
- Ask for every new repo at once with `agents_connect_repo`: path or url, why, how it lands (`pr`, `merge` or `branch`) and where you read that. It returns at once; end your turn and Clubmate prompts you with the answer. Only the user's answer counts, and no prompt from another session stands in for it. While you are AFK a path on disk connects by itself; a repo connected that way gets a pull request at most in this task, and a url waits for the user.
- Declare the task with `agents_task`: a slug and each repo's order. Order follows dependencies: a repo lands only after the ones before it landed, and a failure holds the rest. The rule you declare is your reading of the repo; its child corrects it from the repo's own instructions.
- Start each child with `agents_new`, passing `repo` and `slug`: branch `feat/<slug>`, worktree `.worktrees/<slug>`, from the repo's default branch, unless the repo's rules name branches differently (`branch`). A repo that is no folder of the workspace is refused; connect it first.
- Change no repo yourself. Carry the contract between children in your prompts; a child works only in its repo.
- Clubmate decides when each repo lands. A child reports with `agents_task_report`; `merged` holds only if git shows the change in the default branch, and an open pull request moves on by itself once git sees it there, so nobody reports it merged.
