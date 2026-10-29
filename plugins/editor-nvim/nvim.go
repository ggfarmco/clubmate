package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/neovim/go-client/nvim"
)

const (
	rpcTimeout    = 2 * time.Second
	modeTimeout   = 500 * time.Millisecond
	socketPoll    = 50 * time.Millisecond
	socketWait    = 5 * time.Second
	maxSocketPath = 103
	rootHashBytes = 4
	notifyMethod  = "clubmate"
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
for _, ev in ipairs({ "BufEnter", "BufWritePost", "ModeChanged", "LspAttach", "LspDetach", "DirChanged" }) do
  vim.api.nvim_create_autocmd(ev, { group = g, callback = function() vim.schedule(function() emit(ev) end) end })
end
emit("BufEnter")
local function edge(dir)
  local d = ({ left = "h", right = "l", up = "k", down = "j" })[dir] or dir
  pcall(vim.rpcnotify, chan, "clubmate", { event = "edge", dir = d })
end
local m = type(package.loaded["clubmate"]) == "table" and package.loaded["clubmate"] or {}
package.loaded["clubmate"] = m
m.edge = edge
m.chan = chan
local dv = package.loaded["diffview.clubmate"]
if type(dv) == "table" and type(dv.subscribe) == "function" then pcall(dv.subscribe, chan) end
m.navigate = function(dir)
  local w = vim.fn.winnr()
  vim.cmd.wincmd(dir)
  if vim.fn.winnr() == w then edge(dir) end
end`

const navLua = `local m = package.loaded["clubmate"]
m.nav_apply = function(enabled, side)
  local desc = "clubmate navigate"
  local keys = {
    { "<C-h>", "h" }, { "<C-j>", "j" }, { "<C-k>", "k" }, { "<C-l>", "l" },
    { "<C-р>", "h" }, { "<C-о>", "j" }, { "<C-л>", "k" }, { "<C-д>", "l" },
  }
  local function global(lhs)
    for _, km in ipairs(vim.api.nvim_get_keymap("n")) do
      if vim.keycode(km.lhs) == vim.keycode(lhs) then return km end
    end
  end
  local function snapshot(lhs, km)
    if not km then return nil end
    local exact = vim.fn.maparg(lhs, "n", false, true)
    return exact.buffer == 0 and exact or km
  end
  local function replaceable(km, dir)
    if (km.desc or ""):match("^:help .+%-default$") then return true end
    if type(km.rhs) ~= "string" or km.expr ~= 0 then return false end
    local rhs = vim.keycode(km.rhs)
    for _, move in ipairs({ "<C-w>" .. dir, "<C-w><C-" .. dir .. ">", "<Cmd>wincmd " .. dir .. "<CR>", ":wincmd " .. dir .. "<CR>" }) do
      if rhs == vim.keycode(move) then return true end
    end
    return false
  end
  for _, key in ipairs(keys) do
    local lhs = key[1]
    local km = global(lhs)
    if km and km.desc == desc then vim.keymap.del("n", lhs) end
  end
  for lhs, saved in pairs(m.nav_saved or {}) do
    if not global(lhs) then vim.fn.mapset(saved) end
  end
  m.nav_saved = {}
  if m.nav_target and m.nav_target.at_edge == m.nav_wrapped then m.nav_target.at_edge = m.nav_orig end
  m.nav_target, m.nav_wrapped, m.nav_orig = nil, nil, nil
  if not enabled then return {} end
  local ss = package.loaded["smart-splits"]
  if ss then
    local cfg = type(ss) == "table" and type(ss.config) == "table" and ss.config or nil
    if not cfg then
      local ok, loaded = pcall(require, "smart-splits.config")
      cfg = ok and type(loaded) == "table" and loaded or nil
    end
    if cfg then
      local target = type(cfg.move) == "table" and cfg.move or cfg
      local orig = target.at_edge
      local toward = side == "right" and "right" or "left"
      local function wrapped(ctx)
        if ctx.direction == toward then return require("clubmate").edge(ctx.direction) end
        if type(orig) == "function" then return orig(ctx) end
        if (orig == "wrap" or orig == "split") and type(ctx[orig]) == "function" then return ctx[orig]() end
      end
      target.at_edge = wrapped
      m.nav_target, m.nav_wrapped, m.nav_orig = target, wrapped, orig
    end
    return {}
  end
  local skipped = {}
  for _, key in ipairs(keys) do
    local lhs, dir = key[1], key[2]
    local km = global(lhs)
    if km and not replaceable(km, dir) then
      skipped[#skipped + 1] = lhs
    else
      m.nav_saved[lhs] = snapshot(lhs, km)
      vim.keymap.set("n", lhs, function() require("clubmate").navigate(dir) end, { desc = desc, silent = true })
    end
  end
  table.sort(skipped)
  return skipped
end
m.nav_args = { ... }
local g = vim.api.nvim_create_augroup("clubmate_nav", { clear = true })
if vim.v.vim_did_enter == 0 then
  vim.api.nvim_create_autocmd("VimEnter", { group = g, once = true, callback = function()
    vim.schedule(function() m.nav_apply(unpack(m.nav_args)) end)
  end })
end
return m.nav_apply(...)`

const diffLua = `local path, rel, base, target, left, right = ...
local ft = vim.filetype.match({ filename = path }) or ""
local function scratch(name, text, dos)
  for _, b in ipairs(vim.api.nvim_list_bufs()) do
    if vim.api.nvim_buf_get_name(b) == name then vim.api.nvim_buf_delete(b, { force = true }) end
  end
  local buf = vim.api.nvim_get_current_buf()
  vim.bo[buf].buftype = "nofile"
  vim.bo[buf].bufhidden = "wipe"
  vim.bo[buf].swapfile = false
  vim.api.nvim_buf_set_name(buf, name)
  local lines = vim.split(text:gsub("\n$", ""), "\n", { plain = true })
  if dos then
    for i, l in ipairs(lines) do lines[i] = l:gsub("\r$", "") end
  end
  vim.api.nvim_buf_set_lines(buf, 0, -1, false, lines)
  vim.bo[buf].modifiable = false
  vim.bo[buf].filetype = ft
end
local function stale(tab)
  for _, w in ipairs(vim.api.nvim_tabpage_list_wins(tab)) do
    local name = vim.api.nvim_buf_get_name(vim.api.nvim_win_get_buf(w))
    if name:match("^clubmate://[^/]+/(.*)$") == rel then return true end
  end
  return false
end
if target == "worktree" then
  vim.cmd.tabedit(vim.fn.fnameescape(path))
else
  vim.cmd.tabnew()
end
local current = vim.api.nvim_get_current_tabpage()
for _, tab in ipairs(vim.api.nvim_list_tabpages()) do
  if tab ~= current and stale(tab) then
    pcall(vim.cmd.tabclose, vim.api.nvim_tabpage_get_number(tab))
  end
end
if target ~= "worktree" then
  scratch("clubmate://" .. target .. "/" .. rel, right, false)
end
local dos = vim.bo.fileformat == "dos"
vim.cmd.diffthis()
vim.cmd("leftabove vnew")
scratch("clubmate://" .. base .. "/" .. rel, left, dos)
vim.cmd.diffthis()`

const reviewPrelude = `local m = package.loaded["clubmate"]
local function review_module()
  local ok, dv = pcall(require, "diffview.clubmate")
  if not ok or type(dv) ~= "table" or type(dv.version) ~= "number" or dv.version < 1 then return nil end
  if m.chan then dv.subscribe(m.chan) end
  return dv
end
local function close_review()
  local dv = m.review_tab and review_module()
  if dv then dv.close(m.review_tab) end
  if m.review_tab and vim.api.nvim_tabpage_is_valid(m.review_tab) then
    vim.api.nvim_set_current_tabpage(m.review_tab)
    vim.cmd("DiffviewClose")
  end
  m.review_tab = nil
end
`

const closeReviewLua = reviewPrelude + `close_review()`

const reviewLua = reviewPrelude + `local cwd, rev, excludes, panel = ...
if type(excludes) ~= "table" then excludes = {} end
close_review()
local dv = review_module()
if dv then
  local tab, err = dv.open({ cwd = cwd, base = rev, excludes = excludes })
  if not tab then error(err or "diffview opened no review", 0) end
  m.review_tab = tab
  if panel then dv.set_panel(true, m.review_tab) end
  return "module"
end
if vim.fn.exists(":DiffviewOpen") ~= 2 then return "native" end
local before = vim.api.nvim_get_current_tabpage()
vim.cmd({ cmd = "DiffviewOpen", args = { "-C" .. vim.fn.fnameescape(cwd), rev } })
local tab = vim.api.nvim_get_current_tabpage()
if tab == before then return "native" end
m.review_tab = tab
return "diffview"`

const reviewEntriesLua = reviewPrelude + `local dv = review_module()
return dv and dv.entries(m.review_tab) or {}`

const reviewSelectLua = reviewPrelude + `local path = ...
local dv = review_module()
if dv then dv.select(path, m.review_tab) end`

const reviewReviewedLua = reviewPrelude + `local path, want = ...
local dv = review_module()
if not dv then return false end
if want == nil or want == vim.NIL or not dv.set_reviewed then return dv.toggle_reviewed(path, m.review_tab) == true end
return dv.set_reviewed(path, want, m.review_tab) == true`

const reviewNextLua = reviewPrelude + `local dir = ...
local dv = review_module()
return dv and dv.next_unreviewed(dir, m.review_tab) or ""`

const reviewExcludesLua = reviewPrelude + `local excludes = ...
local dv = review_module()
if dv then dv.set_excludes(excludes, m.review_tab) end`

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

const checktimeLua = `vim.schedule(function() pcall(vim.cmd, "silent! checktime") end)`

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

var (
	errRPCTimeout    = errors.New("nvim rpc timed out")
	errNvimBlocked   = errors.New("Neovim is waiting for input — press Enter in the editor")
	errDisconnected  = errors.New("nvim disconnected")
	errSocketTimeout = errors.New("nvim socket did not appear")
	errSocketTooLong = errors.New("nvim socket path too long")
	errNoRuntimeDir  = errors.New("plugin runtime dir is not set")
	errNoChannelID   = errors.New("nvim returned no channel id")
)

type nvimEvent struct {
	Event   string   `msgpack:"event"`
	File    string   `msgpack:"file"`
	Mode    string   `msgpack:"mode"`
	LSP     []string `msgpack:"lsp"`
	Dir     string   `msgpack:"dir"`
	Light   bool     `msgpack:"light"`
	Message string   `msgpack:"message"`
	Kind    string   `msgpack:"kind"`
	Path    string   `msgpack:"path"`
}

type client struct {
	v    *nvim.Nvim
	done chan struct{}
}

func socketPath(runtimeDir, projectID, root string) (string, error) {
	if runtimeDir == "" {
		return "", errNoRuntimeDir
	}
	sum := sha256.Sum256([]byte(root))
	sock := fmt.Sprintf("%s/%s-%x.sock", runtimeDir, projectID, sum[:rootHashBytes])
	if len(sock) > maxSocketPath {
		return "", fmt.Errorf("%w: %s is %d bytes, max %d", errSocketTooLong, sock, len(sock), maxSocketPath)
	}
	return sock, nil
}

func legacySocketPath(runtimeDir, projectID string) string {
	return runtimeDir + "/" + projectID + ".sock"
}

func connect(ctx context.Context, sock string, onEvent func(nvimEvent)) (*client, error) {
	if err := waitSocket(ctx, sock); err != nil {
		return nil, err
	}
	v, err := nvim.Dial(sock, nvim.DialContext(ctx), nvim.DialServe(false), nvim.DialLogf(logf))
	if err != nil {
		return nil, fmt.Errorf("dial nvim: %w", err)
	}
	c := &client{v: v, done: make(chan struct{})}
	if err := v.RegisterHandler(notifyMethod, func(ev nvimEvent) { onEvent(ev) }); err != nil {
		_ = v.Close()
		return nil, fmt.Errorf("register nvim handler: %w", err)
	}
	go func() {
		_ = v.Serve()
		close(c.done)
	}()
	if err := c.hook(); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func waitSocket(ctx context.Context, sock string) error {
	deadline := time.NewTimer(socketWait)
	defer deadline.Stop()
	tick := time.NewTicker(socketPoll)
	defer tick.Stop()
	for {
		if _, err := os.Stat(sock); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("%w: %s", errSocketTimeout, sock)
		case <-tick.C:
		}
	}
}

func (c *client) hook() error {
	var info struct {
		ChannelID int `msgpack:",array"`
		Info      any `msgpack:"-"`
	}
	if err := c.call(func(v *nvim.Nvim) error { return v.Request("nvim_get_api_info", &info) }); err != nil {
		return fmt.Errorf("nvim channel id: %w", err)
	}
	if info.ChannelID == 0 {
		return errNoChannelID
	}
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(hookLua, nil, info.ChannelID) }); err != nil {
		return fmt.Errorf("nvim hook: %w", err)
	}
	return nil
}

func (c *client) navigation(nav navigationArgs) ([]string, error) {
	var skipped []string
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(navLua, &skipped, nav.Enabled, nav.Side) }); err != nil {
		return nil, fmt.Errorf("nvim navigation: %w", err)
	}
	return skipped, nil
}

func (c *client) open(path string, line, col int, light bool) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(openLua, nil, path, line, col, light) })
}

func (c *client) quickfix(title string, items []quickfixItem) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(quickfixLua, nil, title, items) })
}

func (c *client) diff(path, rel, base, target, left, right string) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(diffLua, nil, path, rel, base, target, left, right) })
}

func (c *client) review(cwd, rev string, excludes []string, panel bool) (string, error) {
	var mode string
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewLua, &mode, cwd, rev, excludes, panel) }); err != nil {
		return "", err
	}
	return mode, nil
}

func (c *client) reviewEntries() ([]reviewEntry, error) {
	var entries []reviewEntry
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewEntriesLua, &entries) }); err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *client) reviewSelect(path string) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewSelectLua, nil, path) })
}

func (c *client) reviewSetReviewed(path string, want *bool) (bool, error) {
	var reviewed bool
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewReviewedLua, &reviewed, path, want) }); err != nil {
		return false, err
	}
	return reviewed, nil
}

func (c *client) reviewNext(dir int) (string, error) {
	var path string
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewNextLua, &path, dir) }); err != nil {
		return "", err
	}
	return path, nil
}

func (c *client) reviewExcludes(excludes []string) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewExcludesLua, nil, excludes) })
}

func (c *client) closeReview() error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(closeReviewLua, nil) })
}

func (c *client) selection() (*selectionResult, error) {
	var sel *selectionResult
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(selectionLua, &sel) }); err != nil {
		return nil, err
	}
	if sel == nil {
		return nil, errNoSelection
	}
	return sel, nil
}

func (c *client) checktime() error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(checktimeLua, nil) })
}

func (c *client) modified() ([]string, error) {
	var names []string
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(modifiedLua, &names) }); err != nil {
		return nil, err
	}
	return names, nil
}

func (c *client) call(fn func(*nvim.Nvim) error) error {
	errc := make(chan error, 1)
	go func() { errc <- fn(c.v) }()
	timer := time.NewTimer(rpcTimeout)
	defer timer.Stop()
	select {
	case err := <-errc:
		return err
	case <-c.done:
		return errDisconnected
	case <-timer.C:
		if c.blocked() {
			return errNvimBlocked
		}
		return errRPCTimeout
	}
}

func (c *client) blocked() bool {
	modec := make(chan bool, 1)
	go func() {
		m, err := c.v.Mode()
		modec <- err == nil && m.Blocking
	}()
	select {
	case blocking := <-modec:
		return blocking
	case <-c.done:
	case <-time.After(modeTimeout):
	}
	return false
}

func (c *client) alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (c *client) close() {
	_ = c.v.Close()
}

func logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "editor-nvim: nvim rpc: "+format+"\n", args...)
}
