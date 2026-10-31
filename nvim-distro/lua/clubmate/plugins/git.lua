local pin = require("clubmate.pin")

local EXCLUDES = table.concat({
  ":(exclude)**/go.mod",
  ":(exclude)**/go.sum",
  ":(exclude)**/*-lock.json",
  ":(exclude)**/*-lock.yaml",
}, " ")

local function diff_against_merge_base(base)
  local merge_base = vim.trim(vim.fn.system({ "git", "merge-base", base, "HEAD" }))
  if vim.v.shell_error ~= 0 then
    vim.notify("git merge-base " .. base .. " HEAD failed: " .. merge_base, vim.log.levels.ERROR)
    return
  end
  vim.cmd("DiffviewOpen " .. merge_base .. " -- " .. EXCLUDES)
end

return {
  pin("lewis6991/gitsigns.nvim", {
    event = { "BufReadPost", "BufNewFile" },
    config = function()
      require("gitsigns").setup({
        current_line_blame = true,
        current_line_blame_opts = {
          delay = 500,
          ignore_whitespace = true,
        },
        on_attach = function(bufnr)
          local gs = package.loaded.gitsigns
          local map = function(mode, l, r, desc)
            vim.keymap.set(mode, l, r, { buffer = bufnr, desc = desc })
          end

          map("n", "]h", gs.next_hunk, "Next hunk")
          map("n", "[h", gs.prev_hunk, "Prev hunk")
          map("n", "<leader>gb", gs.blame_line, "Blame line")
          map("n", "<leader>gB", function() gs.blame_line({ full = true }) end, "Blame line (full)")
          map("n", "<leader>gp", gs.preview_hunk, "Preview hunk")
          map("n", "<leader>gr", gs.reset_hunk, "Reset hunk")
          map("n", "<leader>gs", gs.stage_hunk, "Stage hunk")
          map("v", "<leader>gs", function() gs.stage_hunk({ vim.fn.line("."), vim.fn.line("v") }) end, "Stage hunk")
        end,
      })
    end,
  }),
  pin("kdheepak/lazygit.nvim", {
    cmd = { "LazyGit", "LazyGitConfig", "LazyGitCurrentFile", "LazyGitFilter", "LazyGitFilterCurrentFile" },
    dependencies = { pin("nvim-lua/plenary.nvim") },
  }),
  pin("0xnorimori/diffview.nvim", {
    event = "VeryLazy",
    cmd = { "DiffviewOpen", "DiffviewFileHistory" },
    keys = {
      {
        "<leader>dv",
        function()
          vim.cmd("DiffviewOpen -- " .. EXCLUDES)
        end,
        desc = "Diffview open",
      },
      {
        "<leader>dm",
        function()
          diff_against_merge_base("main")
        end,
        desc = "Diff vs merge-base(main)",
      },
      {
        "<leader>dM",
        function()
          Snacks.picker.git_branches({
            all = true,
            title = "Diff base",
            confirm = function(picker, item)
              picker:close()
              if item and item.branch then
                diff_against_merge_base(item.branch)
              end
            end,
          })
        end,
        desc = "Diff vs merge-base(picked branch)",
      },
      { "<leader>dh", "<cmd>DiffviewFileHistory %<cr>", desc = "File history" },
      { "<leader>dq", "<cmd>DiffviewClose<cr>", desc = "Diffview close" },
    },
    config = function()
      local FileEntry = require("diffview.scene.file_entry").FileEntry
      local Diff1 = require("diffview.scene.layouts.diff_1").Diff1
      local original_with_layout = FileEntry.with_layout

      FileEntry.with_layout = function(layout_class, opt)
        if opt.status and vim.tbl_contains({ "A", "?" }, opt.status) then
          return original_with_layout(Diff1, opt)
        end
        return original_with_layout(layout_class, opt)
      end

      require("diffview").setup({
        enhanced_diff_hl = true,
        hooks = {
          diff_buf_read = function()
            vim.opt_local.wrap = false
            vim.opt_local.foldenable = false
          end,
          diff_buf_win_enter = function()
            vim.opt_local.foldenable = false
          end,
        },
        view = {
          default = {
            layout = "diff2_horizontal",
          },
        },
        file_panel = {
          listing_style = "tree",
          win_config = {
            position = "right",
            height = 15,
          },
        },
        keymaps = {
          view = {
            ["q"] = "<cmd>DiffviewClose<cr>",
            ["e"] = require("diffview.actions").goto_file_edit,
            ["a"] = false,
            ["dd"] = false,
            ["@"] = false,
          },
          file_panel = {
            ["q"] = "<cmd>DiffviewClose<cr>",
            ["e"] = require("diffview.actions").goto_file_edit,
            ["s"] = require("diffview.actions").toggle_stage_entry,
            ["r"] = require("diffview.actions").toggle_reviewed_entry,
            ["A"] = function() end,
            ["@"] = function() end,
          },
          file_history_panel = {
            ["q"] = "<cmd>DiffviewClose<cr>",
            ["e"] = require("diffview.actions").goto_file_edit,
          },
        },
      })

      require("nrev").setup({ bin = vim.fs.dirname(pin.root) .. "/tools/nrev" })
    end,
  }),
}
