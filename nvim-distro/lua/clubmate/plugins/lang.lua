local pin = require("clubmate.pin")

local function select_obj(query)
  return function()
    require("nvim-treesitter-textobjects.select").select_textobject(query, "textobjects")
  end
end

return {
  pin("nvim-treesitter/nvim-treesitter", {
    lazy = false,
    config = function()
      require("nvim-treesitter").setup({ install_dir = vim.fn.stdpath("data") .. "/site" })
      vim.api.nvim_create_autocmd("FileType", {
        group = vim.api.nvim_create_augroup("TreesitterFeatures", { clear = true }),
        callback = function(args)
          if pcall(vim.treesitter.start, args.buf) then
            vim.bo[args.buf].indentexpr = "v:lua.require'nvim-treesitter'.indentexpr()"
          end
        end,
      })
    end,
  }),
  pin("nvim-treesitter/nvim-treesitter-textobjects", {
    lazy = false,
    config = function()
      require("nvim-treesitter-textobjects").setup({
        select = { lookahead = true },
      })
      vim.keymap.set({ "x", "o" }, "af", select_obj("@function.outer"), { desc = "Function outer" })
      vim.keymap.set({ "x", "o" }, "if", select_obj("@function.inner"), { desc = "Function inner" })
      vim.keymap.set({ "x", "o" }, "ac", select_obj("@class.outer"), { desc = "Class outer" })
      vim.keymap.set({ "x", "o" }, "ic", select_obj("@class.inner"), { desc = "Class inner" })
    end,
  }),
  pin("mason-org/mason.nvim", {
    config = function()
      require("mason").setup()
    end,
  }),
}
