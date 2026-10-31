# nvim integration

Clubmate always starts its bundled Neovim with its own distribution: lazy.nvim, snacks (picker, input, notifications), which-key, blink.cmp, treesitter, gitsigns, neo-tree, lualine, LSP configs, dap/neotest and the `0xnorimori/diffview.nvim` fork with `nrev`. It never reads or writes `~/.config/nvim` or `~/.local/share/nvim`; the only access is an existence check for the import offer.

## Neovim binary

Clubmate runs the Neovim shipped at `Clubmate.app/Contents/Resources/nvim/bin/nvim` as `nvim -u <Resources>/nvim-distro/init.lua`. Without the bundled copy (for example when the core runs outside the app), it falls back to the `nvim` in `PATH`; with neither, the editor shows `nvim not found in PATH`. Clubmate removes any inherited `VIM` and `VIMRUNTIME` and sets `NVIM_APPNAME` to `clubmate` (release) or `clubmate-dev` (every other build), so `stdpath("config")`, `data`, `state` and `cache` are Clubmate's own (`~/.config/clubmate`, `~/.local/share/clubmate`, …). nvim also gets `COLORTERM=truecolor` and, when the app's bundled terminfo is present, `TERM=xterm-ghostty` with `TERMINFO` pointing at it: nvim knows Ghostty's truecolor, undercurl and cursor shapes without asking the terminal, and programs nvim starts find the entry. nvim usually starts before a terminal attaches; its remaining startup queries are answered when one does (see `ARCHITECTURE.md`, `clubmate-session`).

The app's bundled tools dir (`Contents/Resources/tools`, with `rg` and `nrev`) is appended to the end of `PATH` for nvim, terminals and agent sessions, so your own tools still win. The binary is chosen each time nvim starts; after an app update, restart Neovim (palette "Restart Neovim") so running editors pick up the new binary and distribution.

## The distribution

`Contents/Resources/nvim-distro/` is read-only and replaced by every app update:

| Path | What |
|---|---|
| `init.lua` | entry: lazy.nvim pointed at the bundled plugin dirs, then the user layer |
| `lua/clubmate/` | options, keymaps (leader `,`, see `docs/shortcuts.md`), autocmds, theme, LSP, health check, import offer; `plugins/*.lua` are the default lazy specs |
| `lua/clubmate/manifest.lua` | generated: every plugin's URL and commit, the parser list and the lock file's sha256 |
| `plugins/<name>/` | each plugin's unmodified source tree at its pinned commit, with generated `doc/tags`; `blink.cmp/target/release/libblink_cmp_fuzzy.dylib` is blink's prebuilt fuzzy library |
| `site/{parser,queries,parser-info}/` | treesitter parsers compiled at bundle time |
| `colors/`, `lsp/` | the `clubmate` and `lightowl` colorschemes, LSP server configs |
| `skill/SKILL.md` | the guide agents follow to change the user layer |

Nothing is cloned, downloaded or compiled when nvim starts, and nothing writes inside the app. lazy.nvim's checker and change detection are off and its module cache is disabled, so a long install path (a dev build in DerivedData) cannot break `vim.loader`.

## User layer

Your changes live in `stdpath("config")` (`~/.config/clubmate`, or `~/.config/clubmate-dev`) and survive app updates: `lua/user/options.lua` (before plugins), `lua/user/plugins/*.lua` (lazy specs merged over the defaults: `opts`, `enabled = false`, `keys`, extra plugins), `lua/user/keymaps.lua` and `lua/user/autocmds.lua` (after the defaults), `snippets/` for blink. Each file is optional; an error in one shows as a notification and the rest still loads. A root `init.lua` or `lua/plugins/` there (an old copied config) is ignored. Plugins you add are installed by lazy.nvim into `stdpath("data")/lazy`; that is the only download, and only for plugins you chose.

## Theme

The app publishes its Ghostty theme (name, dark flag, background, foreground, 16-colour palette) on the bus as `theme.changed` at launch and on every change, including light/dark switches. editor-nvim writes it to `plugin-data/editor-nvim/theme.json`, passes that file to new nvims as `CLUBMATE_THEME_FILE`, and calls `require("clubmate.theme").apply(t)` in every running one. It remembers the last event it handled, so a theme published while the core or plugin restarts is still picked up. Known themes (Kanagawa Wave/Dragon/Lotus, Light Owl) take their syntax from the bundled colorscheme; any other theme gets a generated scheme from the palette. Chrome groups (floats, picker, which-key, blink, gitsigns, diffview, neo-tree, lualine, notifications) follow the app's tokens. If you pick another colorscheme in your layer, theme changes leave it alone.

## LSP and mason

The distribution ships `lsp/*.lua` for gopls, rust_analyzer, ts_ls, tailwindcss, sqls and solidity_ls_nomicfoundation and enables them; a server found in `PATH` or in mason's dir just works. Servers are never installed automatically. The first time you open a file whose server is missing, a notification names the `:MasonInstall …` command to run.

## Health check

`:checkhealth clubmate` lists the distribution root and lock, every bundled plugin and parser, the fuzzy matcher, the theme, your user layer files, missing LSP servers with their `:MasonInstall` lines, and the bundled tools.

## Import my nvim config

Palette "Import my nvim config" (offered once when `~/.config/nvim` exists) starts an agent session that moves your own settings into the user layer; see `docs/agents.md`.

## Bumping a plugin

1. Edit its line in `scripts/nvim-distro.lock` (`plugin <name> <commit> <sha256> <url>`; the sha256 is of `<url>/archive/<commit>.tar.gz`). A parser bump follows nvim-treesitter's `parsers.lua`; blink's `blink-lib` line must name the same commit as the `blink.cmp` plugin line.
2. Refresh `scripts/licenses/nvim-distro/<name>/` (licence files and `COMMIT`).
3. `make nvim-test`: vendors into `build/resources` and runs the distro tests against the bundled nvim.
4. `scripts/licenses.sh` to regenerate `THIRD_PARTY_LICENSES.md` (it also lists the Rust crates in blink's fuzzy library from blink's `Cargo.lock`); `go test ./nvim-distro/` fails while the licences name an older commit.
5. `make app` runs the same smoke tests against the built `Clubmate.app` and fails the build on any error.

## Review changes

In the Agents section, "Review changes" in a session's header and "Diff" on an edit card run `DiffviewOpen -C<worktree> <checkpoint> --untracked-scoped` in that worktree's nvim and show the Code section (`editor-nvim.diffview`). The left side is the checkpoint Clubmate took before the turn (the first turn's for Review changes), the right side the editable worktree file, uncommitted and untracked files included. The diffview is the bundled `0xnorimori/diffview.nvim` fork; its `nrev` review comments (`,rc`, `,rl`, …) are what the `review` skill reads.

## Large and binary files

Opening a file larger than 5 MiB, or one whose first 8000 bytes contain a NUL byte, uses light mode: `noautocmd noswapfile edit`, so no filetype detection, treesitter, LSP or swap file, plus `syntax=off` and `b:clubmate_light = true`. The status bar shows a "light" badge for such a buffer. Other files open normally. Opening is asynchronous, so a slow open never times out the command; if nvim then refuses the open (for example E37 with `nohidden` and a modified buffer), the status bar shows an "open failed" badge until the next buffer is entered.

## Opening at a line and column

`editor.open` takes an optional 1-based `line` and `col`. `col` is a byte column, as ripgrep reports it; it is clamped to the line length and ignored without `line`.

## Quickfix

`editor.quickfix {title, items:[{path, line, col?, text?}]}` replaces nvim's quickfix list with a new one (`setqflist({}, ' ', {title})`, so `:colder` brings back the previous one) and focuses the Code section. It takes 1 to 10 000 items; each `path` is absolute (at most 4096 bytes), `line` at least 1, `col` a 1-based byte column (0 or absent for none) and `text` at most 512 characters; line breaks and NUL are rejected, and so are unknown fields. nvim creates a buffer for every distinct file in the list, which is slow, so only items in the first 1000 distinct files are kept and the title then ends in " (first 1000 files)". The command returns once the request is validated. nvim then appends the items in batches of about 40 ms each, so it stays responsive. It opens the quickfix window with `copen` after the first batch and returns the cursor with `wincmd p`. A newer quickfix request stops an older one that is still filling. A failure shows as an "open failed: quickfix: ..." badge.
