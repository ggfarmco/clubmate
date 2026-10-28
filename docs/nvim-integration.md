# nvim integration

Clubmate drives your own nvim with your own config. It never writes to `~/.config/nvim` and never creates keymaps. When the Code section connects to nvim it registers a Lua module, `clubmate`, that your config can call to move focus from nvim to the Code sidebar (the file tree).

## The `clubmate` module

The module exists only inside nvim started by Clubmate, so always load it with `pcall(require, "clubmate")`. Clubmate updates the same module table whenever it reconnects to nvim, so a cached `require("clubmate")` keeps working.

- `navigate(dir)`: `wincmd dir` (`h`, `j`, `k` or `l`); if the cursor stays in the same window (the edge of the layout), it calls `edge(dir)`.
- `edge(dir)`: tells Clubmate that nvim reached its edge in `dir`. `dir` is `h`, `j`, `k`, `l` or `left`, `down`, `up`, `right`. Today only `h` does something: it focuses the Code sidebar.

## Plain keymap

```lua
vim.keymap.set("n", "<C-h>", function()
  local ok, cm = pcall(require, "clubmate")
  if ok then cm.navigate("h") else vim.cmd.wincmd("h") end
end)
```

Outside Clubmate this is a plain `<C-w>h`.

## smart-splits.nvim

Set the `at_edge` option to call the module:

```lua
require("smart-splits").setup({
  at_edge = function(ctx) local ok, cm = pcall(require, "clubmate"); if ok then cm.edge(ctx.direction) end end,
})
```

## Branch review

When you review a branch — the checked-out one or any other — Clubmate uses your own [diffview.nvim](https://github.com/sindrets/diffview.nvim) if `:DiffviewOpen` exists: it runs `DiffviewOpen -C<repo> <base>...<head>` (`<head>` is `HEAD` for the checked-out branch) in a new tab and closes the review tab it opened before, so there is only ever one. Clubmate never installs or configures diffview. Without it, the review is shown only in the Git section's Review group, whose per-file diffs open in nvim as a `clubmate://<short sha>/<path>` scratch buffer against the worktree file for the checked-out branch, or, for any other branch, against a second read-only `clubmate://<head short sha>/<path>` scratch buffer; nothing is checked out and the worktree is untouched.

## Back to nvim

In the file tree, Ctrl+l and Esc return focus to nvim.
