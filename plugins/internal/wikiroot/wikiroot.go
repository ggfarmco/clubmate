package wikiroot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clubmate/plugins/internal/gitrepo"
)

const (
	EnvDir       = "CLUBMATE_WIKI_DIR"
	IdentityFile = ".wiki.json"
	Author       = "Clubmate"
	Email        = "wiki@clubmate.local"
)

type Project struct {
	Name     string
	Dir      string
	Origin   string
	Checkout string
}

type identity struct {
	Origin   string `json:"origin"`
	Checkout string `json:"checkout"`
}

func Root() string {
	if dir := os.Getenv(EnvDir); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".clubmate", "wiki")
}

func Find(ctx context.Context, folder string) (Project, bool, error) {
	origin, checkout := resolve(ctx, folder)
	return match(Root(), origin, checkout)
}

func Ensure(ctx context.Context, folder string) (Project, error) {
	origin, checkout := resolve(ctx, folder)
	root := Root()
	unlock, err := Lock(filepath.Join(root, ".lock"))
	if err != nil {
		return Project{}, err
	}
	defer unlock()
	if p, ok, err := match(root, origin, checkout); err != nil || ok {
		return p, err
	}
	dir, err := freeDir(root, filepath.Base(checkout))
	if err != nil {
		return Project{}, err
	}
	if err := create(ctx, dir, identity{Origin: origin, Checkout: checkout}); err != nil {
		os.RemoveAll(dir)
		return Project{}, err
	}
	return Project{Name: filepath.Base(dir), Dir: dir, Origin: origin, Checkout: checkout}, nil
}

func Git(ctx context.Context, dir string, args ...string) ([]byte, error) {
	pre := []string{"-c", "user.name=" + Author, "-c", "user.email=" + Email, "-c", "commit.gpgsign=false", "-c", "core.hooksPath=/dev/null", "-c", "core.quotePath=false"}
	return gitrepo.RunEnv(ctx, dir, []string{"GIT_LITERAL_PATHSPECS=1"}, append(pre, args...)...)
}

func resolve(ctx context.Context, folder string) (string, string) {
	canon := gitrepo.Canonical(filepath.Clean(folder))
	top, ok := gitrepo.RepoRoot(canon)
	if !ok {
		return "", canon
	}
	checkout := top
	if trees, err := gitrepo.Worktrees(ctx, top); err == nil {
		for _, t := range trees {
			if !t.Bare {
				checkout = t.Real
				break
			}
		}
	}
	origin := ""
	if out, err := gitrepo.Run(ctx, top, "remote", "get-url", "origin"); err == nil {
		origin = strings.TrimSpace(string(out))
	}
	return origin, gitrepo.Canonical(checkout)
}

func match(root, origin, checkout string) (Project, bool, error) {
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	var byPath *Project
	key := normalize(origin)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		id, ok := readIdentity(dir)
		if !ok {
			continue
		}
		p := Project{Name: e.Name(), Dir: dir, Origin: origin, Checkout: checkout}
		if key != "" && normalize(id.Origin) == key {
			return p, true, nil
		}
		if byPath == nil && id.Checkout == checkout {
			byPath = &p
		}
	}
	if byPath != nil {
		return *byPath, true, nil
	}
	return Project{}, false, nil
}

func readIdentity(dir string) (identity, bool) {
	data, err := os.ReadFile(filepath.Join(dir, IdentityFile))
	if err != nil {
		return identity{}, false
	}
	var id identity
	if json.Unmarshal(data, &id) != nil {
		return identity{}, false
	}
	return id, true
}

func normalize(origin string) string {
	s := strings.ToLower(strings.TrimSpace(origin))
	if s == "" {
		return ""
	}
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
		if at := strings.Index(s, "@"); at >= 0 && at < strings.Index(s+"/", "/") {
			s = s[at+1:]
		}
	} else if at := strings.Index(s, "@"); at >= 0 {
		s = strings.Replace(s[at+1:], ":", "/", 1)
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
	return s
}

func freeDir(root, name string) (string, error) {
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "wiki"
	}
	for i := 1; ; i++ {
		candidate := name
		if i > 1 {
			candidate = name + "-" + strconv.Itoa(i)
		}
		dir := filepath.Join(root, candidate)
		if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
			return dir, nil
		} else if err != nil {
			return "", err
		}
	}
}

func create(ctx context.Context, dir string, id identity) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if _, err := Git(ctx, dir, "init", "-q", "-b", "main"); err != nil {
		return err
	}
	data, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, IdentityFile), append(data, '\n'), 0o600); err != nil {
		return err
	}
	if _, err := Git(ctx, dir, "add", "--", IdentityFile); err != nil {
		return err
	}
	_, err = Git(ctx, dir, "commit", "-q", "-m", "wiki created")
	return err
}
