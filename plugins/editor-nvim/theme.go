package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/neovim/go-client/nvim"

	"clubmate/pkg/pluginsdk"
)

const (
	themeTopic    = "theme.changed"
	themeFileEnv  = "CLUBMATE_THEME_FILE"
	themeFileName = "theme.json"
	themeSeqKey   = "theme.seq"
	paletteSize   = 16
	themeLua      = "local ok, m = pcall(require, 'clubmate.theme') if ok then m.apply(...) end"
)

var (
	errThemeColor   = errors.New("theme: colors must be #rrggbb")
	errThemePalette = fmt.Errorf("theme: palette must hold %d colors", paletteSize)
	errNoStateDir   = errors.New("theme: no plugin state dir")
	hexColor        = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

type theme struct {
	Name       *string  `json:"name" msgpack:"name,omitempty"`
	Dark       bool     `json:"dark" msgpack:"dark"`
	Background string   `json:"background" msgpack:"background"`
	Foreground string   `json:"foreground" msgpack:"foreground"`
	Palette    []string `json:"palette" msgpack:"palette"`
}

func themeFile() string {
	dir := pluginsdk.StateDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, themeFileName)
}

func (a *app) themeChanged(payload json.RawMessage) {
	t, err := decodeTheme(payload)
	if err != nil {
		logError(themeTopic, err)
		return
	}
	a.themeMu.Lock()
	defer a.themeMu.Unlock()
	if err := writeTheme(payload); err != nil {
		logError(themeTopic, err)
	}
	for _, c := range a.connectedClients() {
		pushTheme(c, t)
	}
}

func (a *app) applyTheme(c *client) {
	a.themeMu.Lock()
	defer a.themeMu.Unlock()
	path := themeFile()
	if path == "" {
		return
	}
	payload, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	if err != nil {
		logError(themeTopic, err)
		return
	}
	t, err := decodeTheme(payload)
	if err != nil {
		logError(themeTopic, err)
		return
	}
	pushTheme(c, t)
}

func pushTheme(c *client, t theme) {
	if err := c.call(func(v *nvim.Nvim) error { return v.ExecLua(themeLua, nil, t) }); err != nil {
		logError(themeTopic, err)
	}
}

func decodeTheme(payload json.RawMessage) (theme, error) {
	var t theme
	if err := json.Unmarshal(payload, &t); err != nil {
		return theme{}, fmt.Errorf("theme: %w", err)
	}
	if len(t.Palette) != paletteSize {
		return theme{}, errThemePalette
	}
	for _, c := range append([]string{t.Background, t.Foreground}, t.Palette...) {
		if !hexColor.MatchString(c) {
			return theme{}, fmt.Errorf("%w: %q", errThemeColor, c)
		}
	}
	return t, nil
}

func writeTheme(payload json.RawMessage) error {
	path := themeFile()
	if path == "" {
		return errNoStateDir
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), themeFileName+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func (a *app) connectedClients() []*client {
	a.mu.Lock()
	editors := slices.Collect(maps.Values(a.editors))
	a.mu.Unlock()
	var clients []*client
	for _, e := range editors {
		if c := e.connected(); c != nil {
			clients = append(clients, c)
		}
	}
	return clients
}
