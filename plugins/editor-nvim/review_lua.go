package main

const reviewCallLua = `local name, args = ...
local m = package.loaded["clubmate"]
local okl, lazy = pcall(require, "lazy")
if okl then pcall(lazy.load, { plugins = { "diffview.nvim" } }) end
local ok, cm = pcall(require, "diffview.clubmate")
if not ok or type(cm) ~= "table" or (cm.version or 0) < 2 then
  return { ok = false, err = "diffview host API v2 is not available" }
end
if type(m) == "table" and m.chan and m.review_chan ~= m.chan then
  cm.subscribe(m.chan)
  m.review_chan = m.chan
end
local value, err = cm[name](unpack(args, 1, args.n or #args))
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

const reviewJumpLua = `local tab, path, line, side = ...
local cm = require("diffview.clubmate")
local lib = require("diffview.lib")
cm.select(path, tab)
local tries = 0
local function place()
  tries = tries + 1
  local view = lib.tabpage_to_view(tab)
  local cur = view and view.cur_entry
  if not (cur and cur.path == path and cur.opened) then
    if tries < 40 then vim.defer_fn(place, 50) end
    return
  end
  local layout = view.cur_layout
  local win = side == "old" and layout.a or layout.b
  if not (win and win:is_valid()) then win = layout.b end
  if not (win and win:is_valid()) then return end
  local buf = vim.api.nvim_win_get_buf(win.id)
  local row = math.max(1, math.min(line, vim.api.nvim_buf_line_count(buf)))
  vim.api.nvim_set_current_win(win.id)
  pcall(vim.api.nvim_win_set_cursor, win.id, { row, 0 })
  vim.cmd("normal! zz")
end
vim.schedule(place)
return {}`

const reviewFilterLua = `local tab, name, hidden = ...
local view = require("diffview.lib").tabpage_to_view(tab)
local panel = view and view.panel
if not (panel and panel.hidden) then return { ok = false, err = "review tab is gone" } end
if name ~= "" and (panel.hidden[name] == true) ~= hidden then panel:toggle_filter(name) end
local counts = { test = 0, generated = 0 }
for _, file in view.files:iter() do
  if counts[file.review_kind] then counts[file.review_kind] = counts[file.review_kind] + 1 end
end
return { ok = true, value = { tests = counts.test, generated = counts.generated, hide_tests = panel.hidden.tests == true, hide_generated = panel.hidden.generated == true } }`
