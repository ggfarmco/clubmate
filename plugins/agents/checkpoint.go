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

const (
	checkpointRefs  = "refs/clubmate/cp/"
	endSuffix       = ".end"
	worktreeTrailer = "Clubmate-Worktree"
)

var checkpointIdentity = []string{
	"GIT_AUTHOR_NAME=Clubmate",
	"GIT_AUTHOR_EMAIL=clubmate@localhost",
	"GIT_COMMITTER_NAME=Clubmate",
	"GIT_COMMITTER_EMAIL=clubmate@localhost",
}

func checkpoint(ctx context.Context, root, session string, turn int) (string, error) {
	return pin(ctx, root, checkpointRef(session, turn), fmt.Sprintf("clubmate checkpoint %s turn %d", session, turn))
}

func endCheckpoint(ctx context.Context, root, session string, turn int) (string, error) {
	return pin(ctx, root, checkpointRef(session, turn)+endSuffix, fmt.Sprintf("clubmate turn end %s turn %d", session, turn))
}

func checkpointRef(session string, turn int) string {
	return checkpointRefs + session + "/" + strconv.Itoa(turn)
}

func pin(ctx context.Context, root, ref, title string) (string, error) {
	top, ok := gitrepo.RepoRoot(root)
	if !ok {
		return "", nil
	}
	tree, head, err := snapshot(ctx, root)
	if err != nil {
		return "", err
	}
	args := []string{"commit-tree", tree, "-m", title, "-m", worktreeTrailer + ": " + top}
	if head != "" {
		args = append(args, "-p", head)
	}
	sha, err := gitOutput(ctx, root, checkpointIdentity, args...)
	if err != nil {
		return "", err
	}
	if _, err := gitOutput(ctx, root, nil, "update-ref", ref, sha); err != nil {
		return "", err
	}
	return sha, nil
}

func changedSince(ctx context.Context, root, sha string) (bool, error) {
	tree, _, err := snapshot(ctx, root)
	if err != nil {
		return false, err
	}
	base, err := gitOutput(ctx, root, nil, "rev-parse", "--verify", "-q", sha+"^{tree}")
	if err != nil {
		return false, err
	}
	return tree != base, nil
}

func snapshot(ctx context.Context, root string) (string, string, error) {
	dir, err := os.MkdirTemp("", "clubmate-cp")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(dir)
	index := filepath.Join(dir, "index")
	env := []string{"GIT_INDEX_FILE=" + index}
	head, _ := gitOutput(ctx, root, env, "rev-parse", "--verify", "-q", "HEAD")
	if head != "" && !seedIndex(ctx, root, index) {
		if _, err := gitOutput(ctx, root, env, "read-tree", head); err != nil {
			return "", "", err
		}
	}
	if _, err := gitOutput(ctx, root, env, "add", "-A"); err != nil {
		return "", "", err
	}
	tree, err := gitOutput(ctx, root, env, "write-tree")
	if err != nil {
		return "", "", err
	}
	return tree, head, nil
}

func gitOutput(ctx context.Context, root string, env []string, args ...string) (string, error) {
	out, err := gitrepo.RunEnv(ctx, root, env, args...)
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
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
