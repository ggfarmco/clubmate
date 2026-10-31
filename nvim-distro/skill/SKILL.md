---
name: clubmate-nvim
description: Use when changing Clubmate's Neovim — keys, options, plugins, LSP, theme — or importing a user's nvim config into Clubmate
---

# Clubmate Neovim

## Overview

Clubmate starts its bundled nvim with its own distribution (lazy.nvim, snacks, which-key, blink, …). The defaults ship read-only inside the app; everything the user changes lives in a **user layer** that app updates never touch. You only ever write the user layer.

## Where things are

This file is `<RES>/nvim-distro/skill/SKILL.md`; `<RES>` is the app's `Contents/Resources` (or `build/resources` in a dev checkout).

| Path | What |
|---|---|
| `<RES>/nvim/bin/nvim` | bundled nvim 0.12.6 |
| `<RES>/nvim-distro/` | the distro, **read-only**: `init.lua`, `lua/clubmate/{options,keymaps,autocmds}.lua`, default specs in `lua/clubmate/plugins/*.lua`, vendored `plugins/<name>/`, parsers in `site/`, `colors/`, `lsp/` |
| `<RES>/tools/` | `rg`, `nrev` |
| `~/.config/<APP>/` | the user layer (`stdpath("config")`, honours `$XDG_CONFIG_HOME`) |
| `~/.local/share/<APP>/lazy/` | plugins the user added |

`<APP>` is `NVIM_APPNAME`: `clubmate` for `Clubmate.app`, `clubmate-dev` for `Clubmate Dev.app` and dev builds. Inside an nvim terminal `$NVIM_APPNAME` is already set.

## The user layer

| File | Loaded |
|---|---|
| `lua/user/options.lua` | after the default options (leader is `,`), before plugins |
| `lua/user/plugins/*.lua` | lazy.nvim specs, imported after the defaults |
| `lua/user/keymaps.lua`, `lua/user/autocmds.lua` | after the default keymaps and autocmds |
| `snippets/` | blink.cmp snippets |
| `lazy-lock.json` | lazy's lockfile for added plugins |

Each file is optional. An error in one is shown as a notification and the rest still loads. A root `init.lua` or `lua/plugins/` here is **ignored** (left over from an old copied config) — don't put anything there.

## Changing a default plugin

Read its default spec in `<RES>/nvim-distro/lua/clubmate/plugins/` first, then add a spec with the same `owner/repo` in `lua/user/plugins/<name>.lua`. lazy.nvim merges it:

```lua
return {
  { "folke/which-key.nvim", opts = { delay = 100 } },
  { "folke/snacks.nvim", opts = function(_, opts) opts.picker.layout = { preset = "ivy" } end },
  { "Aasim-A/scrollEOF.nvim", enabled = false },
  { "0xnorimori/diffview.nvim", keys = { { "<leader>dv", false }, { "<leader>dd", "<cmd>DiffviewOpen<cr>", desc = "Diffview" } } },
}
```

- `opts` only works when the default spec uses `opts`. If it has its own `config = function() … end`, your `opts` is ignored — give a full `config` instead.
- `{ "<lhs>", false }` in `keys` drops a default lazy key.
- Never set `dir` and never call `require("clubmate.pin")` — those are for bundled plugins.

## Keys, options, autocmds

Plain Lua in `lua/user/{keymaps,options,autocmds}.lua`: `vim.keymap.set` overrides a default key, `vim.keymap.del("n", "<leader>q")` removes one. Keys a plugin sets inside its `config` (neo-tree's `-`/`+`, gitsigns' buffer keys) are applied when it loads and win over `keymaps.lua`; change those in that plugin's spec. Every default key is listed in `<RES>/nvim-distro/lua/clubmate/keymaps.lua` and the plugin specs.

## Adding a plugin

`lua/user/plugins/<name>.lua` returning `{ "owner/repo", opts = {} }`. lazy installs it into `~/.local/share/<APP>/lazy/` on the next start, which needs the network once; the defaults never do.

## LSP

Configs ship for gopls, rust_analyzer, ts_ls, tailwindcss, sqls and solidity_ls_nomicfoundation; servers are not bundled. A missing one shows a one-time notice and a `:checkhealth clubmate` warning with the line to run: `:MasonInstall <package>` (or put the server on `PATH`). Change or add servers in the user layer, e.g. `lua/user/autocmds.lua`:

```lua
vim.lsp.config("gopls", { settings = { gopls = { gofumpt = true } } })
vim.lsp.enable("pyright")
```

## Theme

The `clubmate` colorscheme follows the app's theme live. Leave it alone to keep that. To use another one, run `vim.cmd.colorscheme("<name>")` from the user layer (a plugin spec's `config`, or a `User VeryLazy` autocmd); app theme changes then leave it untouched. `colorscheme clubmate` switches back.

## Verify

After every change run (set `RES` and `APP` as above; `-i NONE` keeps the user's shada untouched):

```sh
RES=/Applications/Clubmate.app/Contents/Resources APP=clubmate
NVIM_APPNAME=$APP "$RES/nvim/bin/nvim" --headless -i NONE -u "$RES/nvim-distro/init.lua" \
  -c 'Lazy! load all' -c 'silent checkhealth clubmate lazy' -c 'silent w! /tmp/clubmate-health.txt' \
  -c 'lua local e = vim.tbl_values(require("clubmate.user").errors); if vim.v.errmsg ~= "" then e[#e + 1] = vim.v.errmsg end; for _, n in ipairs(require("lazy.core.config").spec.notifs) do if n.level >= vim.log.levels.ERROR then e[#e + 1] = n.msg end end; for _, n in ipairs(package.loaded["snacks.notifier"] and Snacks.notifier.get_history() or {}) do if n.level == "error" then e[#e + 1] = n.msg end end; if #e > 0 then io.stderr:write(table.concat(e, "\n") .. "\n"); vim.cmd("cquit 1") end' \
  -c 'qa!' >/dev/null && echo OK
```

Exit 1 prints the startup errors (user files, specs, plugin configs). Then read `/tmp/clubmate-health.txt` and fix every `ERROR`; `WARNING`s for missing LSP servers are expected until they are installed. To apply the change to running editors, use "Restart Neovim" from the palette (or the `editor_nvim_restart` tool); it refuses while buffers are unsaved — tell the user rather than forcing it.

## Never

- Edit anything under `<RES>` — it is read-only, signed and replaced by every update.
- Write `~/.config/nvim`, `~/.local/share/nvim` or `~/.claude*`. Read `~/.config/nvim` only when importing it, with the user's permission.

## Importing `~/.config/nvim`

Read it, then write only `~/.config/<APP>/lua/user/…`. Move the user's own options, keymaps, autocmds and extra plugins; skip what the defaults already cover (compare with `<RES>/nvim-distro/lua/clubmate/`). Destructive one-off mappings and machine-specific paths go in only if the user confirms. Finish with the verify command and report what moved and what was skipped.
