local root = vim.fs.dirname(vim.fs.normalize(debug.getinfo(1, "S").source:sub(2)))
vim.opt.rtp:prepend({ root, root .. "/site", root .. "/plugins/lazy.nvim" })

local user = require("clubmate.user")

require("clubmate.options")
require("clubmate.ui").setup()
user.load("user.options")

local spec = { { import = "clubmate.plugins" } }
if user.has_plugins() then
  spec[#spec + 1] = { import = "user.plugins" }
end

require("clubmate.readonly").setup(root)

require("lazy").setup({
  spec = spec,
  root = vim.fn.stdpath("data") .. "/lazy",
  lockfile = vim.fn.stdpath("config") .. "/lazy-lock.json",
  install = { missing = true, colorscheme = { "clubmate", "habamax" } },
  checker = { enabled = false },
  change_detection = { enabled = false },
  rocks = { enabled = false },
  readme = { enabled = false },
  ui = { border = "rounded", backdrop = 60 },
  performance = {
    cache = { enabled = false },
    rtp = { paths = { root, root .. "/site" } },
  },
})

vim.opt.rtp:prepend({ root, root .. "/site" })

require("clubmate.lsp").setup()

require("clubmate.keymaps")
require("clubmate.autocmds")
user.load("user.keymaps")
user.load("user.autocmds")

vim.api.nvim_create_autocmd("User", {
  pattern = "VeryLazy",
  once = true,
  callback = function()
    vim.g.clubmate_ready = true
  end,
})
