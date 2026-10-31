package main

import (
	"context"
	"fmt"
	"strconv"

	"clubmate/plugins/internal/gitrepo"
)

const (
	checkpointRefs  = "refs/clubmate/cp/"
	endSuffix       = ".end"
	worktreeTrailer = "Clubmate-Worktree"
)

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
	tree, head, err := gitrepo.Snapshot(ctx, root)
	if err != nil {
		return "", err
	}
	sha, err := gitrepo.CommitTree(ctx, root, tree, head, title, worktreeTrailer+": "+top)
	if err != nil {
		return "", err
	}
	if err := gitrepo.UpdateRef(ctx, root, ref, sha); err != nil {
		return "", err
	}
	return sha, nil
}

func changedSince(ctx context.Context, root, sha string) (bool, error) {
	tree, _, err := gitrepo.Snapshot(ctx, root)
	if err != nil {
		return false, err
	}
	base, err := gitrepo.TreeOf(ctx, root, sha)
	if err != nil {
		return false, err
	}
	return tree != base, nil
}

func deleteCheckpoints(ctx context.Context, repo, session string) error {
	if _, ok := gitrepo.RepoRoot(repo); !ok {
		return nil
	}
	refs, err := gitrepo.Refs(ctx, repo, checkpointRefs+session+"/", worktreeTrailer)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if err := gitrepo.DeleteRef(ctx, repo, ref.Name); err != nil {
			return err
		}
	}
	return nil
}
