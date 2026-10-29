# nvim integration

Clubmate drives your own nvim with your own config. It never writes to `~/.config/nvim` and creates no keymaps unless you turn on navigation keys. When the Code section connects to nvim it registers a Lua module, `clubmate`, that your config can call to move focus from nvim to the Code sidebar (the file tree).

## The `clubmate` module

The module exists only inside nvim started by Clubmate, so always load it with `pcall(require, "clubmate")`. Clubmate updates the same module table whenever it reconnects to nvim, so a cached `require("clubmate")` keeps working.

- `navigate(dir)`: `wincmd dir` (`h`, `j`, `k` or `l`); if the cursor stays in the same window (the edge of the layout), it calls `edge(dir)`.
- `edge(dir)`: tells Clubmate that nvim reached its edge in `dir`. `dir` is `h`, `j`, `k`, `l` or `left`, `down`, `up`, `right`. `h` and `l` focus the Code sidebar when it is on that side (Left for `h`, Right for `l`); `j` and `k` do nothing.

## Navigation keys

The first time you open the Code section Clubmate asks whether to use Ctrl+H/J/K/L to move between nvim and the sidebar; "Toggle nvim Navigation Keys" in the command palette (⌘K) flips the answer later. The setting is applied over RPC to every nvim Clubmate drives, again on every reconnect, and lives only in that nvim session:

- Without smart-splits.nvim, Clubmate maps normal-mode `<C-h>`, `<C-j>`, `<C-k>` and `<C-l>` to `require("clubmate").navigate(dir)` with `desc = "clubmate navigate"`. Their Russian ЙЦУКЕН equivalents `<C-р>`, `<C-о>`, `<C-л>` and `<C-д>` get the same maps, since with that layout active nvim receives the Cyrillic key. A key already mapped to a plain window move in the same direction (`<C-w>h`, `<C-w><C-h>`, `<Cmd>wincmd h<CR>` or `:wincmd h<CR>`, and so on for `j`, `k`, `l`), or to nvim's built-in default (`<C-l>`), is taken over and restored exactly when the keys are turned off. Any other mapping of yours is left alone, and Clubmate shows a banner naming the keys it skipped.
- With smart-splits.nvim loaded (`package.loaded["smart-splits"]`), Clubmate maps nothing and wraps its `at_edge` option (`move.at_edge` in v3, `at_edge` in v2) for the direction that points at the Code sidebar (`left` when the sidebar is on the left, `right` when it is on the right): there it only calls `edge(dir)`, so the cursor neither wraps nor splits. Every other direction keeps your own behavior (`wrap`, `split`, `stop` or your function). Clubmate re-applies the wrap when you move the sidebar to the other side. A smart-splits that is lazy-loaded after Clubmate connects is not wrapped until the setting is applied again (toggle it, or restart nvim). Calling smart-splits' `setup()` again drops the wrap.
- Turning the keys off removes exactly the keymaps with that `desc` and puts back the original `at_edge`.

If you set this up yourself (below), leave navigation keys off.

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

## Large and binary files

Opening a file larger than 5 MiB, or one whose first 8000 bytes contain a NUL byte, uses light mode: `noautocmd noswapfile edit`, so no filetype detection, treesitter, LSP or swap file, plus `syntax=off` and `b:clubmate_light = true`. The status bar shows a "light" badge for such a buffer. Other files open normally. Opening is asynchronous, so a slow open never times out the command; if nvim then refuses the open (for example E37 with `nohidden` and a modified buffer), the status bar shows an "open failed" badge until the next buffer is entered.

## Opening at a line and column

`editor.open` takes an optional 1-based `line` and `col`. `col` is a byte column, as ripgrep reports it; it is clamped to the line length and ignored without `line`.

## Quickfix

`editor.quickfix {title, items:[{path, line, col?, text?}]}` replaces nvim's quickfix list (`setqflist({}, ' ', {title, items})`, so `:colder` brings back the previous one), runs `copen`, returns the cursor to the previous window with `wincmd p` and focuses the Code section. It takes 1 to 10 000 items; each `path` is absolute, `line` at least 1, `col` a 1-based byte column (0 or absent for none) and `text` at most 512 characters. Unknown fields are rejected. Like opening, it runs asynchronously; a failure shows as an "open failed: quickfix: ..." badge. nvim creates a buffer for every distinct file in the list, so a list spanning thousands of files can keep nvim busy for several seconds.

## Back to nvim

In the file tree, Ctrl+l and Esc return focus to nvim.
