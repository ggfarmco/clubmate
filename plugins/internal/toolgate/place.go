package toolgate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Place struct {
	Repo string `json:"repo"`
	Tree string `json:"tree"`
}

func HookDirName(repo string) string {
	sum := sha256.Sum256([]byte(repo))
	return hex.EncodeToString(sum[:])[:16]
}

func PlaceOf(dir string) (Place, bool) {
	if !filepath.IsAbs(dir) {
		return Place{}, false
	}
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return Place{}, false
	}
	for {
		dot := filepath.Join(dir, ".git")
		if info, err := os.Stat(dot); err == nil {
			common, ok := commonDir(dir, dot, info)
			if !ok {
				return Place{}, false
			}
			repo := common
			if filepath.Base(common) == ".git" {
				repo = filepath.Dir(common)
			}
			return Place{Repo: repo, Tree: dir}, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Place{}, false
		}
		dir = parent
	}
}

func commonDir(top, dot string, info fs.FileInfo) (string, bool) {
	gitdir := dot
	if !info.IsDir() {
		raw, err := os.ReadFile(dot)
		if err != nil {
			return "", false
		}
		rest, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir:")
		if !ok {
			return "", false
		}
		gitdir = within(top, strings.TrimSpace(rest))
	}
	common := gitdir
	raw, err := os.ReadFile(filepath.Join(gitdir, "commondir"))
	switch {
	case err == nil:
		common = within(gitdir, strings.TrimSpace(string(raw)))
	case !errors.Is(err, fs.ErrNotExist):
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(common)
	if err != nil {
		return "", false
	}
	return resolved, true
}

func within(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
