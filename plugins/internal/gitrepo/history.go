package gitrepo

import (
	"context"
	"os"
	"strconv"
	"strings"
)

type Commit struct {
	SHA     string
	Parents []string
	Tree    string
	Date    string
}

func RevList(ctx context.Context, repo, revs string, limit int) ([]string, error) {
	out, err := output(ctx, repo, nil, "rev-list", "--reverse", "--no-merges", "--max-count="+strconv.Itoa(limit), "--end-of-options", revs)
	return strings.Fields(out), err
}

func ResolveCommit(ctx context.Context, repo, rev string) (string, error) {
	return output(ctx, repo, nil, "rev-parse", "--verify", "-q", "--end-of-options", rev+"^{commit}")
}

func CommitOf(ctx context.Context, repo, sha string) (Commit, error) {
	out, err := output(ctx, repo, nil, "show", "-s", "--format=%P%x00%T%x00%cI", sha)
	if err != nil {
		return Commit{}, err
	}
	f := strings.Split(out, "\x00")
	if len(f) != 3 {
		return Commit{SHA: sha}, nil
	}
	return Commit{SHA: sha, Parents: strings.Fields(f[0]), Tree: f[1], Date: f[2]}, nil
}

func EmptyTree(ctx context.Context, repo string) (string, error) {
	return output(ctx, repo, nil, "hash-object", "-t", "tree", os.DevNull)
}

func ChangedFiles(ctx context.Context, repo, base, commit string) ([]string, error) {
	out, err := output(ctx, repo, nil, "diff", "--name-only", "-z", "--no-renames", "--diff-filter=d", base, commit)
	if err != nil {
		return nil, err
	}
	var names []string
	for name := range strings.SplitSeq(out, "\x00") {
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func DiffZero(ctx context.Context, repo, base, commit, path string) (string, error) {
	return output(ctx, repo, nil, "diff", "-U0", "--no-color", "--no-ext-diff", base, commit, "--", path)
}

func BlameLines(ctx context.Context, repo, rev, path string, ranges [][2]int) ([]byte, error) {
	args := []string{"blame", "--porcelain"}
	for _, r := range ranges {
		args = append(args, "-L", strconv.Itoa(r[0])+",+"+strconv.Itoa(r[1]))
	}
	return Run(ctx, repo, append(args, rev, "--", path)...)
}
