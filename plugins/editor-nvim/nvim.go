package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"syscall"
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

var (
	errRPCTimeout    = errors.New("nvim rpc timed out")
	errNvimBlocked   = errors.New("Neovim is waiting for input — press Enter in the editor")
	errDisconnected  = errors.New("nvim disconnected")
	errSocketTimeout = errors.New("nvim socket not accepting connections")
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
	v, err := dial(ctx, sock)
	if err != nil {
		return nil, err
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

func dial(ctx context.Context, sock string) (*nvim.Nvim, error) {
	deadline := time.NewTimer(socketWait)
	defer deadline.Stop()
	tick := time.NewTicker(socketPoll)
	defer tick.Stop()
	for {
		v, err := nvim.Dial(sock, nvim.DialContext(ctx), nvim.DialServe(false), nvim.DialLogf(logf))
		if err == nil {
			return v, nil
		}
		if !errors.Is(err, syscall.ENOENT) && !errors.Is(err, syscall.ECONNREFUSED) {
			return nil, fmt.Errorf("dial nvim: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("%w: %s: %w", errSocketTimeout, sock, err)
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
	if err := c.exec(hookLua, nil, info.ChannelID); err != nil {
		return fmt.Errorf("nvim hook: %w", err)
	}
	return nil
}

func (c *client) open(path string, line, col int, light bool) error {
	return c.exec(openLua, nil, path, line, col, light)
}

func (c *client) quickfix(title string, items []quickfixItem) error {
	return c.exec(quickfixLua, nil, title, items)
}

func (c *client) diffview(cwd, rev string) error {
	return c.exec(diffviewLua, nil, cwd, rev)
}

func (c *client) selection() (*selectionResult, error) {
	var sel *selectionResult
	if err := c.exec(selectionLua, &sel); err != nil {
		return nil, err
	}
	if sel == nil {
		return nil, errNoSelection
	}
	return sel, nil
}

func (c *client) checktime() error {
	return c.exec(checktimeLua, nil)
}

func (c *client) modified() ([]string, error) {
	var names []string
	if err := c.exec(modifiedLua, &names); err != nil {
		return nil, err
	}
	return names, nil
}

func (c *client) exec(code string, result any, args ...any) error {
	return c.call(func(v *nvim.Nvim) error { return v.ExecLua(code, result, args...) })
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
