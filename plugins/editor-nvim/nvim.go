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
	Light   bool     `msgpack:"light"`
	Message string   `msgpack:"message"`
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

func (c *client) open(path string, line, col int, light bool) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(openLua, nil, path, line, col, light) })
}

func (c *client) quickfix(title string, items []quickfixItem) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(quickfixLua, nil, title, items) })
}

func (c *client) diffview(cwd, rev string) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(diffviewLua, nil, cwd, rev) })
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
