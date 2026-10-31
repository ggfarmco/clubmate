local mode, nt = _G.arg[1], _G.arg[2]
local args = { unpack(_G.arg, 3) }

local function fail(msg)
  io.stderr:write("error: " .. msg .. "\n")
  os.exit(1)
end

if not (mode and nt and vim.uv.fs_stat(vim.fs.joinpath(nt, "lua/nvim-treesitter/parsers.lua"))) then
  fail("usage: parsers.lua list|check|build <nvim-treesitter dir> ...")
end
vim.opt.rtp:prepend(nt)

local function closure(langs)
  local parsers = require("nvim-treesitter.parsers")
  local seen, out = {}, {}
  local function add(lang)
    if seen[lang] then
      return
    end
    if not parsers[lang] then
      fail("nvim-treesitter has no parser " .. lang)
    end
    seen[lang] = true
    out[#out + 1] = lang
    for _, dep in ipairs(parsers[lang].requires or {}) do
      add(dep)
    end
  end
  for _, lang in ipairs(langs) do
    add(lang)
  end
  return out
end

local function read_lock(path)
  local out = {}
  for line in io.lines(path) do
    local kind, name, rev, _, url = line:match("^(%S+)%s+(%S+)%s+(%S+)%s+(%S+)%s+(%S+)$")
    if kind == "parser" then
      out[#out + 1] = { name = name, rev = rev, url = url }
    end
  end
  return out
end

local function list(langs)
  local parsers = require("nvim-treesitter.parsers")
  for _, lang in ipairs(closure(langs)) do
    local info = parsers[lang].install_info
    if info then
      io.stdout:write(("parser %s %s - %s\n"):format(lang, info.revision, info.url))
    end
  end
end

local function check(lock)
  local parsers = require("nvim-treesitter.parsers")
  local locked, names, bad = {}, {}, {}
  for _, p in ipairs(read_lock(lock)) do
    locked[p.name] = true
    names[#names + 1] = p.name
    local info = parsers[p.name] and parsers[p.name].install_info
    if not info or info.revision ~= p.rev or info.url ~= p.url then
      bad[#bad + 1] = p.name
    end
  end
  for _, lang in ipairs(closure(names)) do
    if parsers[lang].install_info and not locked[lang] then
      bad[#bad + 1] = lang .. " (required, not locked)"
    end
  end
  if #bad > 0 then
    fail("lock differs from nvim-treesitter: " .. table.concat(bad, ", "))
  end
end

local function build(site, src)
  local langs = {}
  for name, kind in vim.fs.dir(src) do
    if kind == "directory" then
      langs[#langs + 1] = name
    end
  end
  table.sort(langs)
  if #langs == 0 then
    fail("no parser sources in " .. src)
  end
  vim.api.nvim_create_autocmd("User", {
    pattern = "TSUpdate",
    callback = function()
      local parsers = require("nvim-treesitter.parsers")
      for _, lang in ipairs(langs) do
        local info = parsers[lang].install_info
        info.path = vim.fs.joinpath(src, lang)
        info.url = nil
        if info.generate and not vim.uv.fs_stat(vim.fs.joinpath(info.path, info.location or "", "src/grammar.json")) then
          info.generate_from_json = false
        end
      end
    end,
  })
  local ts = require("nvim-treesitter")
  ts.setup({ install_dir = site })
  local ok, done = ts.install(closure(langs), { force = true, summary = true }):pwait(600000)
  if not (ok and done) then
    fail("parser build failed: " .. tostring(done))
  end
end

if mode == "list" then
  list(args)
elseif mode == "check" then
  check(args[1])
elseif mode == "build" then
  build(args[1], args[2])
else
  fail("unknown mode " .. mode)
end
