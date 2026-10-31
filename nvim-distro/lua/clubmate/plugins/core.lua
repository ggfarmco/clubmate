local pin = require("clubmate.pin")

return {
  pin("folke/lazy.nvim"),
  pin("rebelot/kanagawa.nvim", {
    lazy = false,
    priority = 1000,
    config = function()
      require("clubmate.theme").startup()
    end,
  }),
  pin("nvim-lua/plenary.nvim", { lazy = true }),
  pin("MunifTanjim/nui.nvim", { lazy = true }),
  pin("nvim-tree/nvim-web-devicons", { lazy = true }),
}
