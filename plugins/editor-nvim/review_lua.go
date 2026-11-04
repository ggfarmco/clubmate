package main

const reviewCallLua = `local name, args = ...
local m = package.loaded["clubmate"]
local okl, lazy = pcall(require, "lazy")
if okl then pcall(lazy.load, { plugins = { "diffview.nvim" } }) end
local ok, cm = pcall(require, "diffview.clubmate")
if not ok or type(cm) ~= "table" or (cm.version or 0) < 4 then
  return { ok = false, err = "diffview host API v4 is not available" }
end
if type(m) == "table" and m.chan and m.review_chan ~= m.chan then
  cm.subscribe(m.chan)
  m.review_chan = m.chan
end
local value, err = cm[name](unpack(args, 1, args.n or #args))
if err ~= nil then return { ok = false, err = tostring(err) } end
return { ok = true, value = value }`

const reviewListLua = `local cm = package.loaded["diffview.clubmate"]
if type(cm) ~= "table" or type(cm.reviews) ~= "function" then return { ok = true, value = {} } end
local m = package.loaded["clubmate"]
if type(m) == "table" and m.chan and m.review_chan ~= m.chan then
  cm.subscribe(m.chan)
  m.review_chan = m.chan
end
local value, err = cm.reviews()
if err ~= nil then return { ok = false, err = tostring(err) } end
return { ok = true, value = value }`

const reviewFocusLua = `local tab = ...
if not vim.api.nvim_tabpage_is_valid(tab) then return { ok = false, err = "review tab is gone" } end
vim.schedule(function()
  if vim.api.nvim_tabpage_is_valid(tab) and vim.api.nvim_get_current_tabpage() ~= tab then
    vim.api.nvim_set_current_tabpage(tab)
  end
end)
return { ok = true }`
