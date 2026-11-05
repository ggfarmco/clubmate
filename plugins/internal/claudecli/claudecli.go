package claudecli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const (
	Haiku     = "claude-haiku-5-5"
	waitDelay = 3 * time.Second
)

var ErrMissing = errors.New("claude CLI not found")

func Find() (string, error) {
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	var paths []string
	if home, err := os.UserHomeDir(); err == nil {
		paths = []string{filepath.Join(home, ".local", "bin", "claude"), filepath.Join(home, ".claude", "local", "claude")}
	}
	for _, path := range append(paths, "/opt/homebrew/bin/claude") {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return path, nil
		}
	}
	return "", ErrMissing
}

func Command(ctx context.Context, claude, model, system string, extra ...string) *exec.Cmd {
	args := append([]string{"-p", "--model", model, "--tools", "", "--setting-sources", "", "--strict-mcp-config", "--no-session-persistence", "--system-prompt", system}, extra...)
	cmd := exec.CommandContext(ctx, claude, args...)
	cmd.Dir = os.TempDir()
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "CLUBMATE_") || strings.HasPrefix(kv, "CLAUDECODE=")
	})
	return cmd
}

func Ask(ctx context.Context, model, system, input string) (string, error) {
	claude, err := Find()
	if err != nil {
		return "", err
	}
	cmd := Command(ctx, claude, model, system, "--output-format", "text")
	cmd.Stdin = strings.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("claude: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}
