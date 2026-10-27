package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/neovim/go-client/nvim"
)

const (
	rpcTimeout    = 2 * time.Second
	socketPoll    = 50 * time.Millisecond
	socketWait    = 5 * time.Second
	maxSocketPath = 103
	notifyMethod  = "clubmate"
)

const hookLua = `local chan = ...
local g = vim.api.nvim_create_augroup("clubmate", { clear = true })
local function emit(ev)
  local buf = vim.api.nvim_get_current_buf()
  local file = vim.bo[buf].buftype == "" and vim.api.nvim_buf_get_name(buf) or ""
  local lsp = {}
  for _, c in ipairs(vim.lsp.get_clients({ bufnr = buf })) do lsp[#lsp + 1] = c.name end
  pcall(vim.rpcnotify, chan, "clubmate", { event = ev, file = file, mode = vim.api.nvim_get_mode().mode, lsp = lsp })
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
m.navigate = function(dir)
  local w = vim.fn.winnr()
  vim.cmd.wincmd(dir)
  if vim.fn.winnr() == w then edge(dir) end
end`

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
  scratch("clubmate://index/" .. rel, right, false)
end
local dos = vim.bo.fileformat == "dos"
vim.cmd.diffthis()
vim.cmd("leftabove vnew")
scratch("clubmate://" .. base .. "/" .. rel, left, dos)
vim.cmd.diffthis()`

const closeReviewLua = `local m = package.loaded["clubmate"]
if m.review_tab and vim.api.nvim_tabpage_is_valid(m.review_tab) then
  vim.api.nvim_set_current_tabpage(m.review_tab)
  vim.cmd("DiffviewClose")
end
m.review_tab = nil`

const reviewLua = `local repo, base = ...
if vim.fn.exists(":DiffviewOpen") ~= 2 then return "builtin" end
` + closeReviewLua + `
vim.cmd({ cmd = "DiffviewOpen", args = { "-C" .. vim.fn.fnameescape(repo), base .. "...HEAD" } })
m.review_tab = vim.api.nvim_get_current_tabpage()
return "diffview"`

const modifiedLua = `local names = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.bo[b].modified and vim.bo[b].buftype == "" and vim.bo[b].buflisted then
    names[#names + 1] = vim.api.nvim_buf_get_name(b)
  end
end
return names`

const openLua = `local path, line = ...
vim.cmd.edit(vim.fn.fnameescape(path))
if line > 0 then
  vim.api.nvim_win_set_cursor(0, { math.min(line, vim.api.nvim_buf_line_count(0)), 0 })
  vim.cmd("normal! zz")
end`

var (
	errRPCTimeout    = errors.New("nvim rpc timed out")
	errDisconnected  = errors.New("nvim disconnected")
	errSocketTimeout = errors.New("nvim socket did not appear")
	errSocketTooLong = errors.New("nvim socket path too long")
	errNoRuntimeDir  = errors.New("plugin runtime dir is not set")
	errNoChannelID   = errors.New("nvim returned no channel id")
)

type nvimEvent struct {
	Event string   `msgpack:"event"`
	File  string   `msgpack:"file"`
	Mode  string   `msgpack:"mode"`
	LSP   []string `msgpack:"lsp"`
	Dir   string   `msgpack:"dir"`
}

type client struct {
	v    *nvim.Nvim
	done chan struct{}
}

func socketPath(runtimeDir, projectID string) (string, error) {
	if runtimeDir == "" {
		return "", errNoRuntimeDir
	}
	sock := runtimeDir + "/" + projectID + ".sock"
	if len(sock) > maxSocketPath {
		return "", fmt.Errorf("%w: %s is %d bytes, max %d", errSocketTooLong, sock, len(sock), maxSocketPath)
	}
	return sock, nil
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

func (c *client) open(path string, line int) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(openLua, nil, path, line) })
}

func (c *client) diff(path, rel, base, target, left, right string) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(diffLua, nil, path, rel, base, target, left, right) })
}

func (c *client) review(repo, base string) (string, error) {
	var mode string
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(reviewLua, &mode, repo, base) }); err != nil {
		return "", err
	}
	return mode, nil
}

func (c *client) closeReview() error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(closeReviewLua, nil) })
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
		return errRPCTimeout
	}
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
