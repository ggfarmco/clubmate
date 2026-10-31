vim.g.mapleader = ","
vim.g.maplocalleader = ","

local opt = vim.opt

opt.number = true
opt.relativenumber = true
opt.signcolumn = "yes"

opt.tabstop = 4
opt.shiftwidth = 4
opt.expandtab = false

opt.smartindent = true
opt.wrap = false

opt.ignorecase = true
opt.smartcase = true

opt.splitright = true
opt.splitbelow = true

opt.termguicolors = true
opt.cursorline = true

opt.scrolloff = 999
opt.sidescrolloff = 8

opt.foldmethod = "expr"
opt.foldexpr = "v:lua.vim.treesitter.foldexpr()"
opt.foldtext = ""
opt.foldlevel = 99
opt.foldlevelstart = 99
opt.foldnestmax = 4

opt.updatetime = 250
opt.timeoutlen = 500

opt.undofile = true

opt.clipboard = "unnamedplus"

opt.spell = false
opt.spelllang = { "en" }
opt.spelloptions = "camel"

opt.list = true
opt.listchars = {
	tab = "→ ",
	trail = "·",
	space = "·",
	nbsp = "␣",
	extends = "›",
	precedes = "‹",
}
