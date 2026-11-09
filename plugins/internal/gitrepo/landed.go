package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

const (
	headsRefPrefix  = "refs/heads/"
	originRefPrefix = "refs/remotes/origin/"
)

var mergePins = []string{
	"-c", "merge.default=text",
	"-c", "merge.renormalize=false",
	"-c", "merge.renames=true",
	"-c", "merge.directoryRenames=conflict",
	"-c", "diff.algorithm=myers",
	"-c", "core.attributesFile=" + os.DevNull,
}

type runner func(ctx context.Context, repo string, env []string, args ...string) ([]byte, error)

func Landed(ctx context.Context, repo, tip string) (bool, error) {
	var failed error
	watched := func(ctx context.Context, repo string, env []string, args ...string) ([]byte, error) {
		out, err := runWith(ctx, repo, env, args...)
		if killed(err) {
			failed = err
		}
		return out, err
	}
	if landedWith(ctx, watched, repo, tip) {
		return true, nil
	}
	return false, failed
}

func LandedInert(ctx context.Context, repo, tip string) bool {
	return landedWith(ctx, inertWith, repo, tip)
}

func ForkPoint(ctx context.Context, repo, tip string) string {
	name, err := DefaultBranch(ctx, repo)
	if err != nil {
		return ""
	}
	for _, base := range []string{originRefPrefix + name, headsRefPrefix + name} {
		if out, err := Run(ctx, repo, "merge-base", base, tip); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}

func landedWith(ctx context.Context, git runner, repo, tip string) bool {
	head, err := defaultBase(ctx, git, repo)
	if err != nil {
		return false
	}
	name := strings.TrimPrefix(head, originPrefix)
	for _, base := range []string{originRefPrefix + name, headsRefPrefix + name} {
		if _, err := git(ctx, repo, nil, "rev-parse", "--verify", "--quiet", base); err == nil && landedIn(ctx, git, repo, tip, base) {
			return true
		}
	}
	return false
}

func landedIn(ctx context.Context, git runner, repo, tip, base string) bool {
	plain := func(args ...string) ([]byte, error) {
		return git(ctx, repo, nil, append([]string{"--no-replace-objects"}, args...)...)
	}
	if _, err := plain("merge-base", "--is-ancestor", tip, base); err == nil {
		return true
	}
	if localMergeAttributes(ctx, git, repo) || !netChange(plain, tip, base) {
		return false
	}
	empty, err := plain("hash-object", "-t", "tree", os.DevNull)
	if err != nil {
		return false
	}
	dir, env, err := scratchObjects(plain)
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	merged, err := git(ctx, repo, env, append(append([]string{"--no-replace-objects", "--attr-source=" + strings.TrimSpace(string(empty))}, mergePins...), "merge-tree", "--write-tree", base, tip)...)
	if err != nil {
		return false
	}
	baseTree, err := plain("rev-parse", base+"^{tree}")
	if err != nil {
		return false
	}
	result, _, _ := strings.Cut(string(merged), "\n")
	return result != "" && result == strings.TrimSpace(string(baseTree))
}

func scratchObjects(plain func(...string) ([]byte, error)) (string, []string, error) {
	objects, err := plain("rev-parse", "--path-format=absolute", "--git-path", "objects")
	if err != nil {
		return "", nil, err
	}
	dir, err := scratchDir()
	if err != nil {
		return "", nil, err
	}
	return dir, []string{"GIT_OBJECT_DIRECTORY=" + dir, "GIT_ALTERNATE_OBJECT_DIRECTORIES=" + quoteAlternate(strings.TrimSpace(string(objects)))}, nil
}

func quoteAlternate(path string) string {
	if !strings.ContainsAny(path, `:"\`) {
		return path
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(path) + `"`
}

func netChange(plain func(...string) ([]byte, error), tip, base string) bool {
	fork, err := plain("merge-base", base, tip)
	if err != nil {
		return false
	}
	forkTree, err := plain("rev-parse", strings.TrimSpace(string(fork))+"^{tree}")
	if err != nil {
		return false
	}
	tipTree, err := plain("rev-parse", tip+"^{tree}")
	return err == nil && string(forkTree) != string(tipTree)
}

func localMergeAttributes(ctx context.Context, git runner, repo string) bool {
	path, err := git(ctx, repo, nil, "rev-parse", "--path-format=absolute", "--git-path", "info/attributes")
	if err != nil {
		return true
	}
	data, err := os.ReadFile(strings.TrimSpace(string(path)))
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	return err != nil || bytes.Contains(data, []byte("merge"))
}

func killed(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	ee, ok := errors.AsType[*exec.ExitError](err)
	return ok && ee.ExitCode() < 0
}
