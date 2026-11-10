package main

import (
	"context"
	"os"
	"strings"
	"sync"

	"clubmate/plugins/internal/gitrepo"
)

const (
	gitChangedTopic = "git.changed"
	detachedPrefix  = "(detached "
)

type gitChanged struct {
	Workspace string `json:"workspace"`
}

type branchCache struct {
	mu    sync.Mutex
	names map[string]map[string]string
}

func (c *branchCache) name(ctx context.Context, workspace, root string) string {
	c.mu.Lock()
	name, ok := c.names[workspace][root]
	c.mu.Unlock()
	if ok {
		return name
	}
	if _, err := os.Stat(root); err != nil {
		return ""
	}
	name, _ = gitrepo.Branch(ctx, root)
	c.mu.Lock()
	if c.names == nil {
		c.names = map[string]map[string]string{}
	}
	if c.names[workspace] == nil {
		c.names[workspace] = map[string]string{}
	}
	c.names[workspace][root] = name
	c.mu.Unlock()
	return name
}

func detachedBranch(name string) bool {
	return strings.HasPrefix(name, detachedPrefix)
}

func branchLabel(name string) string {
	if sha, ok := strings.CutPrefix(name, detachedPrefix); ok {
		return "detached · " + strings.TrimSuffix(sha, ")")
	}
	return name
}

func (c *branchCache) reset(workspace string) {
	c.mu.Lock()
	delete(c.names, workspace)
	c.mu.Unlock()
}
