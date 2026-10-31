local M = {}

local function ask()
  vim.ui.select({ "Import with an agent", "Not now" }, { prompt = "Bring your ~/.config/nvim keys and plugins into Clubmate?" }, function(_, idx)
    local m = package.loaded["clubmate"]
    if idx == 1 and type(m) == "table" and m.chan then
      vim.rpcnotify(m.chan, "clubmate", { event = "import" })
    end
  end)
end

function M.offer()
  if vim.g.clubmate_ready then
    vim.schedule(ask)
    return
  end
  vim.api.nvim_create_autocmd("User", {
    pattern = "VeryLazy",
    once = true,
    callback = function()
      vim.schedule(ask)
    end,
  })
end

return M
