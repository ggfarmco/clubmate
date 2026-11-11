local pin = require("clubmate.pin")

local function diff_against_merge_base(base)
  local merge_base = vim.trim(vim.fn.system({ "git", "merge-base", base, "HEAD" }))
  if vim.v.shell_error ~= 0 then
    vim.notify("git merge-base " .. base .. " HEAD failed: " .. merge_base, vim.log.levels.ERROR)
    return
  end
  vim.cmd("DiffviewOpen " .. merge_base)
end

local commit_labels = {}

local function git_out(top, args)
  local res = vim.system(vim.list_extend({ "git", "-C", top }, args), { text = true }):wait()
  if res.code ~= 0 then
    return nil
  end
  return vim.trim(res.stdout)
end

local function commit_label(top, sha)
  local key = top .. "\0" .. sha
  if commit_labels[key] then
    return commit_labels[key]
  end
  local label = sha:sub(1, 10)
  local log = git_out(top, { "log", "-1", "--format=%h%x00%s%x00%cr", sha })
  if log then
    local short, subject, when = unpack(vim.split(log, "\0", { plain = true }))
    local turn = subject:match("^clubmate checkpoint %S+ turn (%d+)$")
    local ended = subject:match("^clubmate turn end %S+ turn (%d+)$")
    if turn then
      subject = "before agent turn " .. turn
    elseif ended then
      subject = "after agent turn " .. ended
    end
    local head = git_out(top, { "symbolic-ref", "-q", "HEAD" })
    local name = head and git_out(top, { "name-rev", "--name-only", "--no-undefined", "--refs=" .. head, sha })
      or git_out(top, { "name-rev", "--name-only", "--no-undefined", "--refs=refs/heads/*", sha })
    label = table.concat({ name and (name .. " " .. short) or short, when, subject }, " · ")
  end
  commit_labels[key] = label
  return label
end

local function file_winbar(file)
  local RevType = require("diffview.vcs.rev").RevType
  local label
  if file.rev.type == RevType.LOCAL then
    label = "Working tree"
  elseif file.rev.type == RevType.STAGE then
    label = "Index"
  elseif file.rev.type == RevType.COMMIT then
    label = commit_label(file.adapter.ctx.toplevel, file.rev.commit)
  else
    return file.winbar
  end
  if file.nulled then
    label = label .. " · no file"
  end
  local esc = function(s) return (s:gsub("%%", "%%%%")) end
  return "%#DiffviewWinBarRev# " .. esc(label) .. " %#DiffviewWinBarPath#%<" .. esc(file.path)
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
          vim.cmd("DiffviewOpen")
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
            cmd_args = { "--sort=-committerdate" },
            on_show = function(picker)
              for i, item in ipairs(picker:items()) do
                if item.branch and not item.current then
                  picker.list:view(i)
                  return
                end
              end
            end,
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
      local Window = require("diffview.scene.window").Window
      local original_show_winbar_info = Window.show_winbar_info

      Window.show_winbar_info = function(self)
        local show = original_show_winbar_info(self)
        if show and self.file.rev and self.file.kind ~= "conflicting" then
          self.file.winbar = file_winbar(self.file)
        end
        return show
      end

      require("diffview").setup({
        enhanced_diff_hl = true,
        use_icons = false,
        signs = {
          fold_closed = " ",
          fold_open = " ",
        },
        hooks = {
          view_opened = function()
            commit_labels = {}
          end,
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
            winbar_info = true,
          },
        },
        file_panel = {
          checkbox = { on = "󰄲", off = "󰄱" },
          filters = {
            generated = {
              extend = {
                "vendor/",
                "node_modules/",
                "bower_components/",
                "jspm_packages/",
                "Pods/",
                "Carthage/",
                ".venv/",
                "venv/",
                "site-packages/",
              },
            },
          },
          listing_style = "tree",
          win_config = {
            position = "right",
            height = 15,
            win_opts = { signcolumn = "no" },
          },
        },
        file_history_panel = {
          win_config = {
            win_opts = { signcolumn = "no" },
          },
        },
        keymaps = {
          view = {
            ["q"] = "<cmd>DiffviewClose<cr>",
            ["e"] = require("diffview.actions").goto_file_edit,
          },
          file_panel = {
            ["q"] = "<cmd>DiffviewClose<cr>",
            ["e"] = require("diffview.actions").goto_file_edit,
            ["s"] = require("diffview.actions").toggle_stage_entry,
            ["r"] = require("diffview.actions").toggle_reviewed_entry,
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
