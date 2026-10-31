package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sourcegraph/jsonrpc2"

	"clubmate/plugins/internal/gitrepo"
)

const (
	currentHead       = "HEAD"
	literalPathspecs  = "--literal-pathspecs"
	headsPrefix       = "refs/heads/"
	remotesPrefix     = "refs/remotes/"
	invalidBranchText = "'%s' is not a valid branch name"
)

func mutate(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := gitrepo.Mutate(ctx, repo, append([]string{literalPathspecs}, args...)...)
	if errors.Is(err, exec.ErrWaitDelay) {
		return out, nil
	}
	return out, gitError(out, err)
}

func mutateNotice(ctx context.Context, repo string, args ...string) (string, error) {
	out, stderr, err := gitrepo.MutateNotice(ctx, repo, append([]string{literalPathspecs}, args...)...)
	if errors.Is(err, exec.ErrWaitDelay) {
		return stderr, nil
	}
	return stderr, gitError(out, err)
}

func run(ctx context.Context, repo string, args ...string) ([]byte, error) {
	out, err := gitrepo.Run(ctx, repo, append([]string{literalPathspecs}, args...)...)
	return out, gitError(out, err)
}

func hasRef(ctx context.Context, dir, ref string) bool {
	_, err := gitrepo.Run(ctx, dir, "rev-parse", "-q", "--verify", ref+"^{commit}")
	return err == nil
}

func validBranch(ctx context.Context, dir, branch string) error {
	if !gitrepo.ValidBranchName(branch) {
		return fmt.Errorf(invalidBranchText, branch)
	}
	out, err := run(ctx, dir, "check-ref-format", "--branch", branch)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(out)) != branch {
		return fmt.Errorf(invalidBranchText, branch)
	}
	return nil
}

func gitError(out []byte, err error) error {
	ee, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		return err
	}
	for _, msg := range [][]byte{ee.Stderr, out} {
		if text := strings.TrimSpace(string(msg)); text != "" {
			return errors.New(text)
		}
	}
	return err
}

func rpcMessage(err error) string {
	if rpcErr, ok := errors.AsType[*jsonrpc2.Error](err); ok {
		return rpcErr.Message
	}
	return err.Error()
}
