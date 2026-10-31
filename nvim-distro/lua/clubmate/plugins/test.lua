local pin = require("clubmate.pin")

return {
  pin("fredrikaverpil/neotest-golang"),
  pin("rouge8/neotest-rust"),
  pin("nvim-neotest/neotest", {
    lazy = false,
    dependencies = {
      pin("nvim-neotest/nvim-nio"),
      pin("nvim-lua/plenary.nvim"),
      pin("nvim-treesitter/nvim-treesitter"),
      pin("fredrikaverpil/neotest-golang"),
      pin("rouge8/neotest-rust"),
    },
    keys = {
      { "<leader>tr", function() require("neotest").run.run() end, desc = "Run nearest test" },
      { "<leader>tf", function() require("neotest").run.run(vim.fn.expand("%")) end, desc = "Run file tests" },
      { "<leader>ta", function() require("neotest").run.run(vim.fn.getcwd()) end, desc = "Run all tests" },
      { "<leader>td", function() require("neotest").run.run({ strategy = "dap" }) end, desc = "Debug nearest test" },
      { "<leader>ts", function() require("neotest").summary.toggle() end, desc = "Toggle summary" },
      { "<leader>to", function() require("neotest").output.open({ enter = true }) end, desc = "Show output" },
      { "<leader>tp", function() require("neotest").output_panel.toggle() end, desc = "Toggle output panel" },
      { "<leader>tc", function() require("neotest").output_panel.clear() end, desc = "Clear output panel" },
      { "<leader>tw", function() require("neotest").run.attach() end, desc = "Attach to running test" },
      { "<leader>tl", function() require("neotest").run.run_last() end, desc = "Run last test" },
      { "<leader>tL", function() require("neotest").run.run_last({ strategy = "dap" }) end, desc = "Debug last test" },
      { "[t", function() require("neotest").jump.prev({ status = "failed" }) end, desc = "Prev failed test" },
      { "]t", function() require("neotest").jump.next({ status = "failed" }) end, desc = "Next failed test" },
    },
    config = function()
      require("neotest").setup({
        adapters = {
          require("neotest-golang")({
            go_test_args = { "-v", "-count=1" },
            warn_test_name_dupes = false,
            dap_go_enabled = true,
            testify_enabled = true,
          }),
          require("neotest-rust")({
            args = { "--no-capture" },
          }),
        },
        status = {
          virtual_text = true,
        },
        output = {
          open_on_run = false,
        },
        output_panel = {
          enabled = true,
          open = "botright split | resize 15",
        },
      })
    end,
  }),
}
