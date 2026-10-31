local pin = require("clubmate.pin")

local function bundled_fuzzy(callback)
  callback = vim.schedule_wrap(callback)
  if pcall(require, "blink.cmp.fuzzy.rust") then
    return callback(nil, "rust")
  end
  vim.notify("blink.cmp: bundled fuzzy library not found, using the Lua matcher", vim.log.levels.WARN)
  callback(nil, "lua")
end

return {
  pin("saghen/blink.cmp", {
    dependencies = { pin("rafamadriz/friendly-snippets") },
    opts = {
      keymap = {
        preset = "default",
        ["<Tab>"] = { "select_next", "fallback" },
        ["<S-Tab>"] = { "select_prev", "fallback" },
        ["<C-n>"] = { "show", "select_next", "fallback" },
        ["<C-p>"] = { "select_prev", "fallback" },
        ["<CR>"] = { "accept", "fallback" },
        ["<C-e>"] = { "cancel", "fallback" },
        ["<C-Space>"] = { "show" },
      },
      completion = {
        list = {
          selection = { preselect = false, auto_insert = true },
        },
        menu = {
          draw = {
            columns = { { "label", "label_description", gap = 1 }, { "kind" } },
          },
        },
        documentation = {
          auto_show = true,
          auto_show_delay_ms = 200,
        },
      },
      signature = {
        enabled = true,
      },
      sources = {
        default = { "lsp", "path", "snippets", "buffer" },
      },
      fuzzy = {
        implementation = "prefer_rust_with_warning",
        prebuilt_binaries = { download = false },
      },
    },
    config = function(_, opts)
      require("blink.cmp.fuzzy.download").ensure_downloaded = bundled_fuzzy
      require("blink.cmp").setup(opts)
    end,
  }),
  pin("nvim-neo-tree/neo-tree.nvim", {
    dependencies = {
      pin("nvim-lua/plenary.nvim"),
      pin("nvim-tree/nvim-web-devicons"),
      pin("MunifTanjim/nui.nvim"),
    },
    config = function()
      require("neo-tree").setup({
        close_if_last_window = true,
        sources = { "filesystem", "buffers", "git_status", "document_symbols" },
        filesystem = {
          follow_current_file = { enabled = true },
          filtered_items = {
            hide_dotfiles = false,
            hide_gitignored = false,
          },
        },
        document_symbols = {
          follow_cursor = true,
        },
        window = {
          position = "right",
          width = 35,
          mappings = {
            ["<space>"] = "none",
          },
        },
      })
      vim.keymap.set("n", "-", "<cmd>Neotree toggle reveal filesystem<cr>", { desc = "Toggle file tree" })
      vim.keymap.set("n", "+", "<cmd>Neotree toggle document_symbols<cr>", { desc = "Toggle symbols tree" })
    end,
  }),
}
