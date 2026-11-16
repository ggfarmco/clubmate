package pluginmigrate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"clubmate/internal/kv"
	"clubmate/internal/paths"
)

const (
	retiredPlugin = "graphs"
	targetPlugin  = "agents"
	keyPrefix     = "graph/"
	runPrefix     = "run/"
)

func Graphs(ctx context.Context, store *kv.Store, dirs paths.Dirs) error {
	from := dirs.PluginState(retiredPlugin)
	to := filepath.Join(dirs.PluginState(targetPlugin), "graphs")
	if err := moveDir(from, to); err != nil {
		return fmt.Errorf("move graphs state: %w", err)
	}
	oldOut, err := jsonPrefix(filepath.Join(from, "output") + string(filepath.Separator))
	if err != nil {
		return err
	}
	newOut, err := jsonPrefix(filepath.Join(to, "output") + string(filepath.Separator))
	if err != nil {
		return err
	}
	rewrite := func(key string, v json.RawMessage) json.RawMessage {
		if !strings.HasPrefix(key, runPrefix) {
			return v
		}
		return bytes.ReplaceAll(v, oldOut, newOut)
	}
	if _, err := store.MovePlugin(ctx, retiredPlugin, targetPlugin, keyPrefix, rewrite); err != nil {
		return fmt.Errorf("move graphs storage: %w", err)
	}
	return nil
}

func jsonPrefix(path string) ([]byte, error) {
	raw, err := json.Marshal(path)
	if err != nil {
		return nil, err
	}
	return raw[:len(raw)-1], nil
}

func moveDir(from, to string) error {
	if _, err := os.Lstat(from); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if err := os.Rename(from, to); err == nil {
		return nil
	} else if _, statErr := os.Lstat(to); statErr != nil {
		return err
	}
	if err := merge(from, to); err != nil {
		return err
	}
	return os.RemoveAll(from)
}

func merge(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		src, dst := filepath.Join(from, e.Name()), filepath.Join(to, e.Name())
		info, err := os.Lstat(dst)
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.Rename(src, dst); err != nil {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		if e.IsDir() && info.IsDir() {
			if err := merge(src, dst); err != nil {
				return err
			}
		}
	}
	return nil
}
