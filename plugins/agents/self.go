package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"clubmate/plugins/internal/gitrepo"
)

const (
	selfDir        = "self"
	selfPrompt     = "prompt.md"
	selfLessons    = "lessons.md"
	selfManifest   = ".claude-plugin/plugin.json"
	selfSkillsDir  = "skills"
	selfSkillFile  = "SKILL.md"
	selfBranch     = "main"
	selfPluginType = "local"
	selfTimeout    = 30 * time.Second

	selfKey          = "self.enabled"
	selfEnabledID    = "self-enabled"
	selfResetID      = "self-reset"
	selfHint         = "Once a day Claude reads all your sessions, then changes its own prompt, skills and project memory without asking. You get a report in the morning and can undo any change. Uses your Claude limit."
	selfResetConfirm = "Empty the agent's prompt and skills? Memory stays."
)

var selfSeed = map[string]string{
	selfPrompt:   "",
	selfLessons:  "",
	selfManifest: `{"name":"self"}`,
}

type selfStore struct {
	dir string
	mu  sync.Mutex
}

type selfParts struct {
	Prompt, PluginDir string
}

func newSelfStore(stateDir string) *selfStore {
	return &selfStore{dir: filepath.Join(stateDir, selfDir)}
}

func (x *selfStore) ensure(ctx context.Context) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	for name, content := range selfSeed {
		if err := writeIfMissing(filepath.Join(x.dir, name), content); err != nil {
			return err
		}
	}
	if _, err := gitOutput(ctx, x.dir, nil, "init", "-q", "-b", selfBranch); err != nil {
		return err
	}
	if _, err := gitOutput(ctx, x.dir, nil, "rev-parse", "-q", "--verify", "HEAD"); err == nil {
		return x.clearInterrupted(ctx)
	}
	return x.commit(ctx, "Start the self")
}

func (x *selfStore) prompt() string {
	data, err := os.ReadFile(filepath.Join(x.dir, selfPrompt))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (x *selfStore) skills() []string {
	root := filepath.Join(x.dir, selfSkillsDir)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(root, e.Name(), selfSkillFile)); err == nil {
			names = append(names, e.Name())
		}
	}
	return names
}

func (x *selfStore) reset(ctx context.Context) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if err := os.WriteFile(filepath.Join(x.dir, selfPrompt), nil, 0o600); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(x.dir, selfSkillsDir)); err != nil {
		return err
	}
	return x.commit(ctx, "Reset the prompt and skills")
}

func (x *selfStore) commit(ctx context.Context, message string) error {
	if _, err := gitOutput(ctx, x.dir, nil, "add", "-A"); err != nil {
		return err
	}
	changed, err := gitOutput(ctx, x.dir, nil, "status", "--porcelain")
	if err != nil || changed == "" {
		return err
	}
	_, err = gitOutput(ctx, x.dir, gitrepo.Identity, "commit", "-q", "--no-verify", "--no-gpg-sign", "-m", message)
	return err
}

func (a *app) selfFor(ctx context.Context, rec record) selfParts {
	if rec.restricted() || !a.selfEnabled(ctx) {
		return selfParts{}
	}
	return a.self.parts()
}

func (x *selfStore) parts() selfParts {
	x.mu.Lock()
	defer x.mu.Unlock()
	parts := selfParts{Prompt: x.prompt()}
	if len(x.skills()) > 0 {
		parts.PluginDir = x.dir
	}
	return parts
}

func (a *app) selfEnabled(ctx context.Context) bool {
	var on bool
	if found, err := a.host.StorageGet(ctx, selfKey, &on); err != nil || !found {
		return false
	}
	return on
}

func (a *app) setSelfEnabled(ctx context.Context, on bool) error {
	if err := a.host.StorageSet(ctx, selfKey, on); err != nil {
		return err
	}
	a.refreshSettings(ctx)
	return nil
}

func (a *app) resetSelf(ctx context.Context) error {
	err := a.self.reset(ctx)
	a.refreshSettings(ctx)
	return err
}

func (a *app) ensureSelf(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	if err := a.self.ensure(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agents: self repo:", err)
	}
}

func writeIfMissing(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	return errors.Join(err, f.Close())
}
