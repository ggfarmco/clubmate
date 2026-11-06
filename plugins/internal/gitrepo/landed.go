package gitrepo

import (
	"context"
	"strings"
)

const (
	headsRefPrefix  = "refs/heads/"
	originRefPrefix = "refs/remotes/origin/"
)

func Landed(ctx context.Context, repo, tip string) bool {
	name, err := DefaultBranch(ctx, repo)
	if err != nil {
		return false
	}
	for _, base := range []string{originRefPrefix + name, headsRefPrefix + name} {
		if _, err := Run(ctx, repo, "rev-parse", "--verify", "--quiet", base); err == nil && landedIn(ctx, repo, tip, base) {
			return true
		}
	}
	return false
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

func landedIn(ctx context.Context, repo, tip, base string) bool {
	if _, err := Run(ctx, repo, "merge-base", "--is-ancestor", tip, base); err == nil {
		return true
	}
	if cherryPicked(ctx, repo, base, tip) {
		return true
	}
	tree, err := Run(ctx, repo, "rev-parse", tip+"^{tree}")
	if err != nil {
		return false
	}
	baseTree, err := Run(ctx, repo, "rev-parse", base+"^{tree}")
	if err != nil {
		return false
	}
	if string(tree) == string(baseTree) {
		return true
	}
	fork, err := Run(ctx, repo, "merge-base", base, tip)
	if err != nil {
		return false
	}
	squash, err := Run(ctx, repo, "-c", "user.name=clubmate", "-c", "user.email=clubmate@localhost", "commit-tree", strings.TrimSpace(string(tree)), "-p", strings.TrimSpace(string(fork)), "-m", "squash")
	return err == nil && cherryPicked(ctx, repo, base, strings.TrimSpace(string(squash)))
}

func cherryPicked(ctx context.Context, repo, base, tip string) bool {
	out, err := Run(ctx, repo, "cherry", base, tip)
	lines := strings.TrimSpace(string(out))
	return err == nil && lines != "" && !strings.Contains("\n"+lines, "\n+")
}
