package main

const (
	offerLua       = "local ok, m = pcall(require, 'clubmate.import') if ok then m.offer() end"
	importErrorLua = "vim.notify(..., vim.log.levels.ERROR)"
	themeLua       = "local ok, m = pcall(require, 'clubmate.theme') if ok then m.apply(...) end"
)

const hookLua = `local chan = ...
local g = vim.api.nvim_create_augroup("clubmate", { clear = true })
local function emit(ev)
  local buf = vim.api.nvim_get_current_buf()
  local file = vim.bo[buf].buftype == "" and vim.api.nvim_buf_get_name(buf) or ""
  local lsp = {}
  for _, c in ipairs(vim.lsp.get_clients({ bufnr = buf })) do lsp[#lsp + 1] = c.name end
  pcall(vim.rpcnotify, chan, "clubmate", { event = ev, file = file, mode = vim.api.nvim_get_mode().mode, lsp = lsp, light = vim.b[buf].clubmate_light == true })
end
for _, ev in ipairs({ "BufEnter", "ModeChanged", "LspAttach", "LspDetach", "DirChanged" }) do
  vim.api.nvim_create_autocmd(ev, { group = g, callback = function() vim.schedule(function() emit(ev) end) end })
end
emit("BufEnter")
local m = type(package.loaded["clubmate"]) == "table" and package.loaded["clubmate"] or {}
package.loaded["clubmate"] = m
m.chan = chan`

const diffviewLua = `local cwd, rev = ...
local m = package.loaded["clubmate"]
vim.schedule(function()
  local ok, err = pcall(vim.cmd, { cmd = "DiffviewOpen", args = { "-C" .. vim.fn.fnameescape(cwd), vim.fn.fnameescape(rev), "--untracked-scoped" } })
  if not ok and type(m) == "table" and m.chan then
    pcall(vim.rpcnotify, m.chan, "clubmate", { event = "open_error", message = tostring(err) })
  end
end)`

const selectionLua = `local buf = vim.api.nvim_get_current_buf()
if vim.bo[buf].buftype ~= "" then return nil end
local mode = vim.api.nvim_get_mode().mode
local a, b
if mode == "v" or mode == "V" or mode == "\22" then
  a, b = vim.fn.getpos("v"), vim.fn.getpos(".")
else
  a, b = vim.fn.getpos("'<"), vim.fn.getpos("'>")
end
if a[2] == 0 or b[2] == 0 then return nil end
if a[2] > b[2] or (a[2] == b[2] and a[3] > b[3]) then a, b = b, a end
local kind = mode == "v" and "v" or mode == "V" and "V" or mode == "\22" and "\22" or vim.fn.visualmode()
if kind == "" then kind = "v" end
local text = table.concat(vim.fn.getregion(a, b, { type = kind }), "\n")
return { path = vim.api.nvim_buf_get_name(buf), startLine = a[2], endLine = b[2], text = text }`

const checktimeLua = `vim.schedule(function()
  local ar = vim.o.autoread
  vim.o.autoread = true
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_is_loaded(b) and vim.bo[b].buftype == "" and not vim.bo[b].modified then
      local name = vim.api.nvim_buf_get_name(b)
      local empty = vim.api.nvim_buf_line_count(b) == 1 and vim.api.nvim_buf_get_lines(b, 0, 1, false)[1] == ""
      if not empty then
        pcall(vim.cmd, "silent! checktime " .. b)
      elseif name ~= "" and vim.uv.fs_stat(name) then
        pcall(vim.api.nvim_buf_call, b, function() vim.cmd("silent! edit") end)
      end
    end
  end
  vim.o.autoread = ar
end)`

const modifiedLua = `local names = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.bo[b].modified and vim.bo[b].buftype == "" and vim.bo[b].buflisted then
    names[#names + 1] = vim.api.nvim_buf_get_name(b)
  end
end
return names`

const openLua = `local path, line, col, light = ...
local m = package.loaded["clubmate"]
vim.schedule(function()
  local ok, err = pcall(function()
    if light then
      vim.cmd({ cmd = "edit", args = { vim.fn.fnameescape(path) }, mods = { noautocmd = true, noswapfile = true } })
      vim.cmd("setlocal syntax=off")
      vim.b.clubmate_light = true
      pcall(vim.api.nvim_exec_autocmds, "BufEnter", { group = "clubmate", buffer = 0 })
    else
      vim.cmd.edit(vim.fn.fnameescape(path))
      vim.b.clubmate_light = nil
    end
    if line > 0 then
      local row = math.min(line, vim.api.nvim_buf_line_count(0))
      local byte = 0
      if col > 0 then
        local len = #vim.api.nvim_buf_get_lines(0, row - 1, row, false)[1]
        byte = math.max(0, math.min(col, len) - 1)
      end
      vim.api.nvim_win_set_cursor(0, { row, byte })
      vim.cmd("normal! zz")
    end
  end)
  if not ok and type(m) == "table" and m.chan then
    pcall(vim.rpcnotify, m.chan, "clubmate", { event = "open_error", message = tostring(err) })
  end
end)`

const quickfixLua = `local title, items = ...
local m = package.loaded["clubmate"]
m.qf_gen = (m.qf_gen or 0) + 1
local gen = m.qf_gen
local budget, target = 40e6, 10e6
local id, pos, size, opened
local function incomplete()
  if id then pcall(vim.fn.setqflist, {}, "a", { id = id, title = title .. " (incomplete)" }) end
end
local function fail(err)
  incomplete()
  if m.chan then
    pcall(vim.rpcnotify, m.chan, "clubmate", { event = "open_error", message = "quickfix: " .. tostring(err) })
  end
end
local function step()
  if m.qf_gen ~= gen then return incomplete() end
  local ok, err = pcall(function()
    local start = vim.uv.hrtime()
    while pos <= #items and vim.uv.hrtime() - start < budget do
      local chunk = vim.list_slice(items, pos, pos + size - 1)
      local t = vim.uv.hrtime()
      if vim.fn.setqflist({}, "a", { id = id, items = chunk }) == -1 then
        pos = #items + 1
        return
      end
      pos = pos + #chunk
      size = math.max(1, math.min(1000, math.floor(#chunk * target / math.max(vim.uv.hrtime() - t, 1))))
      if not opened then
        opened = true
        vim.cmd.copen()
        vim.cmd.wincmd("p")
      end
    end
  end)
  if not ok then return fail(err) end
  if pos <= #items then vim.schedule(step) end
end
vim.schedule(function()
  if m.qf_gen ~= gen then return end
  local ok, err = pcall(function()
    vim.fn.setqflist({}, " ", { title = title })
    id = vim.fn.getqflist({ id = 0 }).id
  end)
  if not ok then return fail(err) end
  pos, size, opened = 1, 50, false
  step()
end)`
