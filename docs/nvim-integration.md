# nvim integration

Clubmate drives your own nvim with your own config. It never writes to `~/.config/nvim` and creates no keymaps.

## Neovim binary

Clubmate runs the Neovim shipped at `Clubmate.app/Contents/Resources/nvim/bin/nvim`. Without the bundled copy (for example when the core runs outside the app), it falls back to the `nvim` in `PATH`; with neither, the editor shows `nvim not found in PATH`. Clubmate removes any inherited `VIM`, `VIMRUNTIME` and `NVIM_APPNAME` before it starts nvim, so a runtime meant for another nvim never shadows the bundled one and nvim always reads `~/.config/nvim`.

The app's bundled tools dir (`Contents/Resources/tools`, with `rg`) is appended to the end of nvim's `PATH`, so your own tools still win. The binary is chosen each time nvim starts; a running nvim keeps its binary until it restarts. After an app update that changes the bundled Neovim version, restart Neovim (palette "Restart Neovim") so running editors pick up the new binary.

## Review changes

In the Agents section, "Review changes" in a session's header and "Diff" on an edit card run `DiffviewOpen -C<worktree> <checkpoint> --untracked-scoped` in that worktree's nvim and show the Code section (`editor-nvim.diffview`). The left side is the checkpoint Clubmate took before the turn (the first turn's for Review changes), the right side the editable worktree file, uncommitted and untracked files included. Clubmate never installs or configures [diffview.nvim](https://github.com/sindrets/diffview.nvim); without it, nvim's error shows as an "open failed" badge.

## Large and binary files

Opening a file larger than 5 MiB, or one whose first 8000 bytes contain a NUL byte, uses light mode: `noautocmd noswapfile edit`, so no filetype detection, treesitter, LSP or swap file, plus `syntax=off` and `b:clubmate_light = true`. The status bar shows a "light" badge for such a buffer. Other files open normally. Opening is asynchronous, so a slow open never times out the command; if nvim then refuses the open (for example E37 with `nohidden` and a modified buffer), the status bar shows an "open failed" badge until the next buffer is entered.

## Opening at a line and column

`editor.open` takes an optional 1-based `line` and `col`. `col` is a byte column, as ripgrep reports it; it is clamped to the line length and ignored without `line`.

## Quickfix

`editor.quickfix {title, items:[{path, line, col?, text?}]}` replaces nvim's quickfix list with a new one (`setqflist({}, ' ', {title})`, so `:colder` brings back the previous one) and focuses the Code section. It takes 1 to 10 000 items; each `path` is absolute (at most 4096 bytes), `line` at least 1, `col` a 1-based byte column (0 or absent for none) and `text` at most 512 characters; line breaks and NUL are rejected, and so are unknown fields. nvim creates a buffer for every distinct file in the list, which is slow, so only items in the first 1000 distinct files are kept and the title then ends in " (first 1000 files)". The command returns once the request is validated. nvim then appends the items in batches of about 40 ms each, so it stays responsive. It opens the quickfix window with `copen` after the first batch and returns the cursor with `wincmd p`. A newer quickfix request stops an older one that is still filling. A failure shows as an "open failed: quickfix: ..." badge.
