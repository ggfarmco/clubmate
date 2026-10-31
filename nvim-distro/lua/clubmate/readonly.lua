local M = {}

function M.setup(root)
  local function bundled(dir)
    return dir ~= nil and vim.startswith(dir, root .. "/")
  end

  local docs = require("lazy.manage.task.plugin").docs
  local skip = docs.skip
  docs.skip = function(plugin, ...)
    return bundled(plugin.dir) or skip(plugin, ...)
  end

  local help = require("lazy.help")
  local update = help.update
  help.update = function(...)
    local lazy = require("lazy.core.config").plugins["lazy.nvim"]
    if lazy and bundled(lazy.dir) then
      return
    end
    return update(...)
  end
end

return M
