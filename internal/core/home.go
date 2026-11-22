package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"clubmate/internal/workspace"
	"clubmate/pkg/contract"
)

const (
	homeDirEnv    = "CLUBMATE_HOME_WORKSPACE_DIR"
	homeName      = "Home"
	oldHomeName   = "~"
	homeReadme    = "README.md"
	homeReadmeMsg = "# Home\n\nYour personal workspace in Clubmate: talks, tasks and notes outside your projects.\n"
	homeCommitMsg = "Home"
)

var ErrHomeDelete = errors.New("the Home workspace can't be removed")

func HomeDir() (string, error) {
	if dir := os.Getenv(homeDirEnv); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".clubmate", "home"), nil
}

func (s *Service) EnsureHome(ctx context.Context) (workspace.Workspace, error) {
	dir, err := s.resolveHomeDir()
	if err != nil {
		return workspace.Workspace{}, err
	}
	s.homeMu.Lock()
	defer s.homeMu.Unlock()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return workspace.Workspace{}, fmt.Errorf("make home folder: %w", err)
	}
	if err := ensureHomeRepo(ctx, dir); err != nil {
		s.logger.Warn("prepare home repo", "dir", dir, "err", err)
	}
	dir = workspace.CanonicalPath(dir)
	s.workspaceMu.Lock()
	defer s.workspaceMu.Unlock()
	p, created, err := s.workspaces.Home(ctx, dir)
	if err != nil {
		return workspace.Workspace{}, err
	}
	if created {
		s.publishWorkspace(ctx, contract.TopicWorkspaceCreated, p)
		return p, nil
	}
	if p.Name != oldHomeName && len(p.Folders) > 0 && p.Folders[0].Path == dir {
		return p, nil
	}
	if err := s.workspaces.SetHome(ctx, p.ID, dir, oldHomeName, homeName); err != nil {
		return workspace.Workspace{}, err
	}
	if p, err = s.workspaces.Get(ctx, p.ID); err != nil {
		return workspace.Workspace{}, err
	}
	s.publishWorkspace(ctx, contract.TopicWorkspaceUpdated, p)
	return p, nil
}

func (s *Service) resolveHomeDir() (string, error) {
	if s.homeDir != "" {
		return s.homeDir, nil
	}
	return HomeDir()
}

func ensureHomeRepo(ctx context.Context, dir string) error {
	if _, err := os.Lstat(filepath.Join(dir, ".git")); errors.Is(err, fs.ErrNotExist) {
		if _, err := homeGit(ctx, dir, "init", "-q"); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("check home repo: %w", err)
	}
	if hasHead(ctx, dir) {
		return nil
	}
	return firstHomeCommit(ctx, dir)
}

func firstHomeCommit(ctx context.Context, dir string) error {
	f, err := os.OpenFile(filepath.Join(dir, homeReadme), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	switch {
	case err == nil:
		_, werr := f.WriteString(homeReadmeMsg)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return fmt.Errorf("write home readme: %w", werr)
		}
	case !errors.Is(err, fs.ErrExist):
		return fmt.Errorf("write home readme: %w", err)
	}
	blob, err := homeGit(ctx, dir, "hash-object", "-w", "--", homeReadme)
	if err != nil {
		return err
	}
	tree, err := homeGitIn(ctx, dir, "100644 blob "+blob+"\t"+homeReadme+"\n", "mktree")
	if err != nil {
		return err
	}
	commit, err := homeGit(ctx, dir, "commit-tree", "--no-gpg-sign", "-m", homeCommitMsg, tree)
	if err != nil {
		return err
	}
	if _, err := homeGit(ctx, dir, "update-ref", "-m", "Home: first commit", "HEAD", commit, ""); err != nil {
		if hasHead(ctx, dir) {
			return nil
		}
		return err
	}
	_, err = homeGit(ctx, dir, "update-index", "--add", "--cacheinfo", "100644,"+blob+","+homeReadme)
	return err
}

func hasHead(ctx context.Context, dir string) bool {
	_, err := homeGit(ctx, dir, "rev-parse", "--verify", "-q", "HEAD")
	return err == nil
}

func homeGit(ctx context.Context, dir string, args ...string) (string, error) {
	return homeGitIn(ctx, dir, "", args...)
}

func homeGitIn(ctx context.Context, dir, stdin string, args ...string) (string, error) {
	full := append([]string{"-C", dir, "-c", "user.name=Clubmate", "-c", "user.email=home@clubmate.local"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w: %s", args[0], dir, err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
