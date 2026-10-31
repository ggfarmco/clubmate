local pin = require("clubmate.pin")

return {
  pin("folke/snacks.nvim", {
    lazy = false,
    priority = 900,
    opts = {
      input = { enabled = true },
      notifier = { enabled = true, style = "compact", top_down = true },
      picker = {
        enabled = true,
        ui_select = true,
        layout = { preset = "vertical", layout = { width = 0.8, height = 0.85, min_width = 80, backdrop = 60 } },
        sources = {
          files = { hidden = true, exclude = { "vendor", "node_modules" } },
          grep = { hidden = true, exclude = { "vendor", "node_modules" } },
        },
      },
      styles = {
        input = { border = "rounded", relative = "editor", row = 0.35 },
        notification = { border = "rounded" },
        notification_history = { border = "rounded" },
      },
    },
  }),
  pin("folke/which-key.nvim", {
    event = "VeryLazy",
    opts = {
      preset = "helix",
      delay = 300,
      win = { border = "rounded" },
      spec = {
        { "<leader>", group = "Leader" },
        { "<leader>c", group = "Code" },
        { "<leader>f", group = "Find" },
        { "<leader>g", group = "Git" },
        { "<leader>l", group = "LazyGit", icon = { icon = "󰊢", color = "orange" } },
        { "<leader>d", group = "Debug/Diff" },
        { "<leader>t", group = "Test", icon = { icon = "󰙨", color = "green" } },
        { "<leader>s", group = "Spell", icon = { icon = "󰓆", color = "cyan" } },
        { "<leader>b", group = "Buffer" },
        { "<leader>n", group = "New", icon = { icon = "", color = "azure" } },
        { "<leader>r", group = "Review", icon = { icon = "", color = "yellow" } },
      },
    },
  }),
  pin("nvim-lualine/lualine.nvim", {
    event = "VeryLazy",
    dependencies = { pin("nvim-tree/nvim-web-devicons") },
    opts = {
      options = {
        globalstatus = true,
        ignore_focus = { "snacks_picker_input", "snacks_picker_list", "snacks_input", "cmd", "msg", "pager", "dialog" },
      },
      sections = {
        lualine_a = { "mode" },
        lualine_b = { "branch", "diff", "diagnostics" },
        lualine_c = { { "filename", path = 1 } },
        lualine_x = { "filetype" },
        lualine_y = { "progress" },
        lualine_z = { "location" },
      },
    },
  }),
  pin("Aasim-A/scrollEOF.nvim", { event = "CursorMoved", opts = {} }),
}
