local M = { errors = {} }

local function config()
  return vim.fn.stdpath("config")
end

function M.has_plugins()
  return #vim.fn.glob(config() .. "/lua/user/plugins/*.lua", true, true) > 0
end

function M.load(mod)
  local base = config() .. "/lua/" .. mod:gsub("%.", "/")
  if not (vim.uv.fs_stat(base .. ".lua") or vim.uv.fs_stat(base .. "/init.lua")) then
    return
  end
  local ok, err = pcall(require, mod)
  if not ok then
    M.errors[mod] = err
    vim.schedule(function()
      vim.notify("user layer: " .. tostring(err), vim.log.levels.ERROR)
    end)
  end
end

return M
