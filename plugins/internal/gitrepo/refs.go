package gitrepo

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var Identity = []string{
	"GIT_AUTHOR_NAME=Clubmate",
	"GIT_AUTHOR_EMAIL=clubmate@localhost",
	"GIT_COMMITTER_NAME=Clubmate",
	"GIT_COMMITTER_EMAIL=clubmate@localhost",
}

type Ref struct {
	Name    string
	Commit  string
	Parent  string
	Tree    string
	Time    int64
	Trailer string
}

func Snapshot(ctx context.Context, root string) (string, string, error) {
	dir, err := os.MkdirTemp("", "clubmate-cp")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	index := filepath.Join(dir, "index")
	env := []string{"GIT_INDEX_FILE=" + index}
	head, _ := output(ctx, root, env, "rev-parse", "--verify", "-q", "HEAD")
	if head != "" && !seedIndex(ctx, root, index) {
		if _, err := output(ctx, root, env, "read-tree", head); err != nil {
			return "", "", err
		}
	}
	if _, err := output(ctx, root, env, "add", "-A"); err != nil {
		return "", "", err
	}
	tree, err := output(ctx, root, env, "write-tree")
	if err != nil {
		return "", "", err
	}
	return tree, head, nil
}

func CommitTree(ctx context.Context, repo, tree, parent string, messages ...string) (string, error) {
	args := []string{"commit-tree", tree}
	for _, m := range messages {
		args = append(args, "-m", m)
	}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	return output(ctx, repo, Identity, args...)
}

func UpdateRef(ctx context.Context, repo, ref, commit string) error {
	_, err := output(ctx, repo, nil, "update-ref", ref, commit)
	return err
}

func DeleteRef(ctx context.Context, repo, ref string) error {
	_, err := output(ctx, repo, nil, "update-ref", "-d", ref)
	return err
}

func Refs(ctx context.Context, repo, prefix, trailer string) ([]Ref, error) {
	out, err := output(ctx, repo, nil, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(parent)%00%(tree)%00%(committerdate:unix)%00%(trailers:key="+trailer+",valueonly,separator=)", prefix)
	if err != nil {
		return nil, err
	}
	var refs []Ref
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimSpace(line), "\x00")
		if len(f) != 6 {
			continue
		}
		when, _ := strconv.ParseInt(f[4], 10, 64)
		refs = append(refs, Ref{Name: f[0], Commit: f[1], Parent: f[2], Tree: f[3], Time: when, Trailer: f[5]})
	}
	return refs, nil
}

func TreeOf(ctx context.Context, repo, rev string) (string, error) {
	return output(ctx, repo, nil, "rev-parse", "--verify", "-q", rev+"^{tree}")
}

func output(ctx context.Context, repo string, env []string, args ...string) (string, error) {
	out, err := RunEnv(ctx, repo, env, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

func seedIndex(ctx context.Context, root, dst string) bool {
	out, err := Run(ctx, root, "rev-parse", "--git-path", "index")
	if err != nil {
		return false
	}
	src := strings.TrimSpace(string(out))
	if !filepath.IsAbs(src) {
		src = filepath.Join(root, src)
	}
	in, err := os.Open(src)
	if err != nil {
		return false
	}
	defer in.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return false
	}
	_, err = io.Copy(f, in)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst)
		return false
	}
	return true
}
