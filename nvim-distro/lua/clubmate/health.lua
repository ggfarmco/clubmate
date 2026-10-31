local M = {}

local root = vim.fs.dirname(vim.fs.dirname(vim.fs.dirname(vim.fs.normalize(debug.getinfo(1, "S").source:sub(2)))))
local tools = vim.fs.dirname(root) .. "/tools"
local health = vim.health

local function sorted(t)
  local out = vim.tbl_keys(t)
  table.sort(out)
  return out
end

local function distribution(manifest)
  health.start("Distribution")
  health.info("root: " .. root)
  health.info("manifest lock: " .. manifest.lock)
  local v = vim.version()
  local version = string.format("%d.%d.%d", v.major, v.minor, v.patch)
  if version == "0.12.6" then
    health.ok("nvim " .. version)
  else
    health.warn("nvim " .. version .. ", the distribution is tested only against 0.12.6")
  end
  if vim.uv.fs_access(root, "W") then
    health.info("root is writable (development build)")
  else
    health.ok("root is read-only")
  end
end

local function plugins(manifest)
  health.start("Plugins")
  local loaded = require("lazy.core.config").plugins
  for _, name in ipairs(sorted(manifest.plugins)) do
    local p = loaded[name]
    if p and p._.installed and p.dir == root .. "/plugins/" .. name then
      health.ok(name)
    else
      health.error(name .. " is not installed from the bundle")
    end
  end
end

local function parsers(manifest)
  health.start("Parsers")
  if #manifest.parsers == 0 then
    health.info("no bundled parsers")
  end
  for _, lang in ipairs(manifest.parsers) do
    local ok, added = pcall(vim.treesitter.language.add, lang)
    if ok and added then
      health.ok(lang)
    else
      health.error(lang .. " parser does not load")
    end
  end
end

local function completion()
  health.start("Completion")
  if pcall(require, "blink.cmp.fuzzy.rust") then
    health.ok("blink.cmp uses the bundled Rust fuzzy matcher")
  else
    health.warn("blink.cmp falls back to the Lua fuzzy matcher: the bundled library did not load")
  end
end

local function theme()
  health.start("Theme")
  local t = vim.g.clubmate_theme
  if type(t) == "table" then
    local name = t.name or "unnamed"
    local ok, mod = pcall(require, "clubmate.theme")
    local known = ok and type(mod.known) == "table" and mod.known[name]
    health.info("app theme: " .. name .. (known and (" (known: " .. known .. ")") or " (generated)"))
  else
    health.info("no app theme received")
  end
  health.info("colors_name: " .. tostring(vim.g.colors_name))
end

local function user_layer()
  health.start("User layer")
  local config = vim.fn.stdpath("config")
  health.info("path: " .. config .. "/lua/user")
  local files = vim.fn.glob(config .. "/lua/user/**/*.lua", true, true)
  if #files == 0 then
    health.info("no files")
  end
  for _, f in ipairs(files) do
    health.info(f:sub(#config + 2))
  end
  local errors = require("clubmate.user").errors
  for _, mod in ipairs(sorted(errors)) do
    health.error(mod .. ": " .. tostring(errors[mod]))
  end
end

local function lsp()
  health.start("LSP")
  local lsp = require("clubmate.lsp")
  local missing = {}
  for _, m in ipairs(lsp.missing()) do
    missing[m.name] = true
  end
  for _, name in ipairs(sorted(lsp.servers)) do
    if missing[name] then
      health.warn(lsp.notice(name))
    else
      health.ok(name)
    end
  end
end

local function tool(name, report)
  local bundled = tools .. "/" .. name
  local path = vim.fn.executable(bundled) == 1 and bundled or vim.fn.exepath(name)
  if path ~= "" then
    health.ok(name .. ": " .. path)
  else
    report(name .. " not found")
  end
end

local function tooling()
  health.start("Tools")
  tool("rg", health.error)
  tool("nrev", health.warn)
  tool("lazygit", health.warn)
end

function M.check()
  local ok, manifest = pcall(require, "clubmate.manifest")
  if not ok then
    health.start("Distribution")
    health.error("manifest missing: " .. tostring(manifest))
    return
  end
  distribution(manifest)
  plugins(manifest)
  parsers(manifest)
  completion()
  theme()
  user_layer()
  lsp()
  tooling()
end

return M
