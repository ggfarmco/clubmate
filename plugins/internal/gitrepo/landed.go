package gitrepo

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

const (
	headsRefPrefix  = "refs/heads/"
	originRefPrefix = "refs/remotes/origin/"
	squashDate      = "1000000000 +0000"
)

var squashDates = []string{"GIT_AUTHOR_DATE=" + squashDate, "GIT_COMMITTER_DATE=" + squashDate}

type runner func(ctx context.Context, repo string, args ...string) ([]byte, error)

func Landed(ctx context.Context, repo, tip string) (bool, error) {
	var failed error
	watched := func(ctx context.Context, repo string, args ...string) ([]byte, error) {
		out, err := runWith(ctx, repo, squashDates, args...)
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
	inert := func(ctx context.Context, repo string, args ...string) ([]byte, error) {
		return inertWith(ctx, repo, squashDates, args...)
	}
	return landedWith(ctx, inert, repo, tip)
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
		if _, err := git(ctx, repo, "rev-parse", "--verify", "--quiet", base); err == nil && landedIn(ctx, git, repo, tip, base) {
			return true
		}
	}
	return false
}

func landedIn(ctx context.Context, git runner, repo, tip, base string) bool {
	if _, err := git(ctx, repo, "merge-base", "--is-ancestor", tip, base); err == nil {
		return true
	}
	if cherryPicked(ctx, git, repo, base, tip) {
		return true
	}
	tree, err := git(ctx, repo, "rev-parse", tip+"^{tree}")
	if err != nil {
		return false
	}
	baseTree, err := git(ctx, repo, "rev-parse", base+"^{tree}")
	if err != nil {
		return false
	}
	if string(tree) == string(baseTree) {
		return true
	}
	fork, err := git(ctx, repo, "merge-base", base, tip)
	if err != nil {
		return false
	}
	squash, err := git(ctx, repo, "-c", "user.name=clubmate", "-c", "user.email=clubmate@localhost", "commit-tree", strings.TrimSpace(string(tree)), "-p", strings.TrimSpace(string(fork)), "-m", "squash")
	return err == nil && cherryPicked(ctx, git, repo, base, strings.TrimSpace(string(squash)))
}

func cherryPicked(ctx context.Context, git runner, repo, base, tip string) bool {
	out, err := git(ctx, repo, "cherry", base, tip)
	lines := strings.TrimSpace(string(out))
	return err == nil && lines != "" && !strings.Contains("\n"+lines, "\n+")
}

func killed(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	ee, ok := errors.AsType[*exec.ExitError](err)
	return ok && ee.ExitCode() < 0
}
