package plugin

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"clubmate/internal/paths"
)

const searchPathEnv = "CLUBMATE_PLUGINS_PATH"

var ErrDuplicatePlugin = errors.New("duplicate plugin id")

func SearchPath(coreExe string, dirs paths.Dirs) []string {
	if env, ok := os.LookupEnv(searchPathEnv); ok {
		var out []string
		for _, dir := range filepath.SplitList(env) {
			if dir != "" {
				out = append(out, dir)
			}
		}
		return out
	}
	return []string{filepath.Join(filepath.Dir(coreExe), "plugins"), dirs.PluginsDir()}
}

func Discover(searchPath []string) ([]Manifest, []error) {
	var (
		manifests []Manifest
		errs      []error
	)
	seen := map[string]string{}
	for _, root := range searchPath {
		entries, err := os.ReadDir(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("read plugin dir: %w", err))
			continue
		}
		for _, e := range entries {
			dir := filepath.Join(root, e.Name())
			if !isPluginDir(dir) {
				continue
			}
			m, err := Load(dir)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if first, ok := seen[m.ID]; ok {
				errs = append(errs, fmt.Errorf("%s: %w %q (already loaded from %s)", m.Dir, ErrDuplicatePlugin, m.ID, first))
				continue
			}
			seen[m.ID] = m.Dir
			manifests = append(manifests, m)
		}
	}
	return manifests, errs
}

func isPluginDir(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, ManifestFile))
	return err == nil
}
