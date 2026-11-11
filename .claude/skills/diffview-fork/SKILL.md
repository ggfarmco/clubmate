---
name: diffview-fork
description: Use when a task touches diffview in Clubmate's nvim — the fork 0xnorimori/diffview.nvim (sibling checkout ../diffview.nvim), the review panel in the Code section, nrev review comments, the host API require("diffview.clubmate") that plugins/editor-nvim drives, ,dv ,dm ,dM keys, or bumping the diffview pin in scripts/nvim-distro.lock — or when asked what diffview can do.
---

# diffview fork

What the bundled diffview can do, where each part lives, and how a fork change reaches Clubmate. Read this instead of re-reading the fork.

## Upstream (sindrets/diffview.nvim)

| Feature | How |
|---|---|
| All changed files in one tab | `:DiffviewOpen [rev] [-- paths]` (`,dv`): working tree, `--cached`, any rev, `a..b`, `a...b` = vs merge-base; `--imply-local` puts the disk file on the right (LSP works); `-uno`, `-- :!path`, `-C<dir>` |
| File history | `:DiffviewFileHistory [paths]` (`,dh` = current file): most `git log` flags (`--range --follow --author --grep --since --no-merges --first-parent --left/right-only -L`), `-g --range=stash` for stashes; panel `g!` options, `y` copy hash, `L` commit details |
| Close / panel | `:DiffviewClose` (`,dq`, `q`), `:DiffviewToggleFiles`, `:DiffviewFocusFiles`, `:DiffviewRefresh` (`R`), `:DiffviewLog` |
| File panel | `<tab>/<s-tab>` next/prev file, `[F/]F` first/last, `s`/`-` stage, `S/U` stage/unstage all, `X` restore to left side (undoable), `i` list↔tree, `f` flatten single-child dirs, `zo h za zR zM` folds, `gf` `<C-w>gf` `e` open file, `g?` help |
| Layouts | `g<C-x>` cycles diff2 horizontal/vertical |
| Merge tool | conflicts get their own section, 3-way (also 4-way, 1-window); `[x/]x`; `<leader>co/ct/cb/ca`, `dx`; capitals = whole file; `1do 2do 3do` |
| Config | hooks `view_opened/closed/enter/leave`, `diff_buf_read`, `diff_buf_win_enter`, `files_staged`; keymaps per context (`false` disables); `default_args`; Mercurial adapter |

## Fork additions

| Feature | Keys / API | Code |
|---|---|---|
| Reviewed marks, keyed by file content | `r` (distro), `]f/[f` next unreviewed | `lua/diffview/review/marks.lua` |
| Review panel = the file panel: header (subtitle, base/changes, progress, `+/−`, comment count, checkboxes), footer (Send, Copy, comments) | `zc zt zg zr` filters, `gS`/⌘↩ send, `,b` show/hide (drawer in inline), `⌃w j/k` header/footer, mouse clicks | `review/panel.lua`, `review/classify.lua`, `scene/views/diff/render.lua` |
| Folder grouping | one folder per parent dir (`group_dirs`), `t` nested tree | `ui/models/file_tree/file_tree.lua` |
| Inline layout (narrow) | `set_layout("inline")`, removed lines drawn inline | `scene/layouts/diff_1_inline.lua` |
| New/deleted file filler | `╱` hatching + centred message | `review/fill.lua` |
| nrev comments (SQLite `review.db`) | `,rc` add, `,rl` list, `]a/[a`, `,rx` resolve, `,rd` delete, `,re` copy all | `lua/nrev/init.lua`, Go CLI `nrev/` |
| Live refresh | watches `review.db` and the worktree | `scene/views/diff/diff_view.lua`, `lua/nrev/diffview.lua` |

`nrev` CLI: `add|list|resolve|reopen|delete|reviewed|prune`, flags `--db --file --rev --side --type --text --note --status --json --md`. Agents add comments with `nrev add`.

## Host API (`require("diffview.clubmate")`, `version = 4`)

`open(opts)` `info` `entries` `comments` `select(path)` `set_reviewed(path, want)` `toggle_reviewed` `next_unreviewed(dir)` `set_excludes(globs)` `set_meta(tab, {subtitle, base, changes, recipient, sent})` `filters` `set_filter(name, shown)` `set_panel(visible)` `set_layout(layout)` `close` `reviews()` `subscribe(chan)`; the tab is an optional last argument (first for `set_meta`); calls return errors instead of echoing. Caller: `plugins/editor-nvim/review_lua.go`; a fork older than v4 is refused. Behaviour of the Code-section review: `docs/nvim-integration.md` § Review changes.

## Distro wiring

`nvim-distro/lua/clubmate/plugins/git.lua`: keys `,dv ,dm ,dM ,dh ,dq`, `q`/`e`/`s`/`r` overrides, panel on the right, `listing_style = "tree"`, generated-filter extras, winbar labels, `require("nrev").setup`. Every key is in `docs/shortcuts.md`.

## Landing a fork change

1. Fork worktree on a branch (`git -C ../diffview.nvim worktree add ../diffview.nvim-<slug> -b <slug>`); spec in `lua/diffview/tests/functional/*_spec.lua`; `make test` (builds `bin/nrev`, specs run one file at a time; unset `CLUBMATE_*` first). Busted traps: wrap a call that may return nothing in parentheses inside `assert.falsy((…))`; `.` in Lua patterns is one byte, write glyphs literally.
2. Squash into the fork's `main`, `git push origin main` (the pin fetches the GitHub archive).
3. In a Clubmate worktree: `scripts/nvim-distro.lock` line `plugin diffview.nvim <rev> <sha256> <url>` with sha = `curl -sfL <url>/archive/<rev>.tar.gz | shasum -a 256`; same rev in `scripts/licenses/nvim-distro/diffview.nvim/COMMIT` and the `### diffview.nvim <rev>` heading of `THIRD_PARTY_LICENSES.md`.
4. `make nvim-test` (vendors into `build/resources`, runs `nvim-distro` tests). A text look at the panel: headless `build/resources/nvim/bin/nvim -u build/resources/nvim-distro/init.lua` with `env -i HOME=<tmp>` and a luafile that prints the panel buffer.
5. One squashed Clubmate commit naming `old → new` pin; update `docs/shortcuts.md`, `docs/nvim-integration.md` and this skill in it.
