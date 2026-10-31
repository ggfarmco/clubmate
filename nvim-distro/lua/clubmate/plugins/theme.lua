local pin = require("clubmate.pin")

return {
  pin("nvim-lualine/lualine.nvim", {
    opts = function(_, o)
      o.options = o.options or {}
      o.options.theme = require("clubmate.theme").lualine
    end,
  }),
}
