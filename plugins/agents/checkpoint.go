package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"clubmate/plugins/internal/gitrepo"
)

const checkpointRefs = "refs/clubmate/cp/"

var checkpointIdentity = []string{
	"GIT_AUTHOR_NAME=Clubmate",
	"GIT_AUTHOR_EMAIL=clubmate@localhost",
	"GIT_COMMITTER_NAME=Clubmate",
	"GIT_COMMITTER_EMAIL=clubmate@localhost",
}

func checkpoint(ctx context.Context, root, session string, turn int) (string, error) {
	if _, ok := gitrepo.RepoRoot(root); !ok {
		return "", nil
	}
	dir, err := os.MkdirTemp("", "clubmate-cp")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	index := filepath.Join(dir, "index")
	env := append([]string{"GIT_INDEX_FILE=" + index}, checkpointIdentity...)
	git := func(args ...string) (string, error) {
		out, err := gitrepo.RunEnv(ctx, root, env, args...)
		if err != nil {
			return "", fmt.Errorf("git %s: %w", args[0], err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	head, _ := git("rev-parse", "--verify", "-q", "HEAD")
	if head != "" && !seedIndex(ctx, root, index) {
		if _, err := git("read-tree", head); err != nil {
			return "", err
		}
	}
	if _, err := git("add", "-A"); err != nil {
		return "", err
	}
	tree, err := git("write-tree")
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", fmt.Sprintf("clubmate checkpoint %s turn %d", session, turn)}
	if head != "" {
		args = append(args, "-p", head)
	}
	sha, err := git(args...)
	if err != nil {
		return "", err
	}
	if _, err := git("update-ref", checkpointRefs+session+"/"+strconv.Itoa(turn), sha); err != nil {
		return "", err
	}
	return sha, nil
}

func seedIndex(ctx context.Context, root, dst string) bool {
	out, err := gitrepo.Run(ctx, root, "rev-parse", "--git-path", "index")
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

func deleteCheckpoints(ctx context.Context, repo, session string) error {
	if _, ok := gitrepo.RepoRoot(repo); !ok {
		return nil
	}
	out, err := gitrepo.Run(ctx, repo, "for-each-ref", "--format=%(refname)", checkpointRefs+session+"/")
	if err != nil {
		return err
	}
	for ref := range strings.FieldsSeq(string(out)) {
		if _, err := gitrepo.Mutate(ctx, repo, "update-ref", "-d", ref); err != nil {
			return err
		}
	}
	return nil
}
