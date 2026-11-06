package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	graphsFolder   = hooksDir + "/graphs"
	graphExt       = ".json"
	skillsFolder   = ".claude/skills"
	skillFile      = "SKILL.md"
	trustedCommand = "graphs.trusted"
	trustedTimeout = 2 * time.Second
	upstreamRef    = "@{upstream}"
)

var (
	graphNameRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	instructionFiles = []string{"CLAUDE.md", ".claude/CLAUDE.md", "AGENTS.md", "CONTRIBUTING.md"}
)

type repoFacts struct {
	Name          string    `json:"name"`
	Path          string    `json:"path"`
	Remote        string    `json:"remote"`
	Branch        string    `json:"branch"`
	Clean         bool      `json:"clean"`
	Dirty         int       `json:"dirty"`
	Behind        int       `json:"behind"`
	Ahead         int       `json:"ahead"`
	DefaultBranch string    `json:"defaultBranch"`
	Fetched       bool      `json:"fetched"`
	Brings        brought   `json:"brings"`
	Trusted       repoTrust `json:"trusted"`
}

type brought struct {
	Graphs       []string `json:"graphs"`
	Hooks        []string `json:"hooks"`
	Instructions []string `json:"instructions"`
	Skills       []string `json:"skills"`
}

type repoTrust struct {
	Hooks  bool     `json:"hooks"`
	Graphs []string `json:"graphs"`
}

type pathArgs struct {
	Path string `json:"path"`
}

type trustedGraphs struct {
	Graphs []string `json:"graphs"`
}

func readRepoFacts(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) (repoFacts, error) {
	if !filepath.IsAbs(path) {
		return repoFacts{}, fmt.Errorf("invalid_params: %w: %s", errRelativePath, path)
	}
	path = filepath.Clean(path)
	f, err := gitrepo.OpenFolder(ctx, path)
	if err != nil {
		return repoFacts{}, fmt.Errorf("invalid_params: not a git repo: %s", path)
	}
	top := f.Checkout.Path
	status, err := gitrepo.Inert(ctx, top, "status", "--porcelain", "--ignore-submodules=all")
	if err != nil {
		return repoFacts{}, err
	}
	dirty := strings.Count(string(status), "\n")
	out := repoFacts{Name: filepath.Base(filepath.Join(f.Trees[0].Path, f.Rel)), Path: path, Branch: f.Checkout.Branch, Clean: dirty == 0, Dirty: dirty}
	out.DefaultBranch, _ = gitrepo.DefaultBranch(ctx, top)
	if name := remoteName(ctx, top); name != "" {
		if raw, err := run(ctx, top, "remote", "get-url", name); err == nil {
			out.Remote = remoteForDisplay(strings.TrimSpace(string(raw)))
		}
		out.Behind, out.Ahead = divergence(ctx, top, remotesPrefix+name+"/"+out.DefaultBranch)
	}
	out.Brings = bringsOf(top, f.Real)
	out.Trusted = repoTrust{
		Hooks:  hooks{h: h, workspaceID: workspaceID, primary: f.Trees[0].Path}.allTrusted(ctx, top, out.Brings.Hooks),
		Graphs: trustedGraphNames(ctx, h, workspaceID, f.Real, out.Brings.Graphs),
	}
	return out, nil
}

func prepareClone(_ context.Context, _ *pluginsdk.Host, _ string, args worktreeArgs) (opFunc, error) {
	if !filepath.IsAbs(args.Into) {
		return nil, errRelativePath
	}
	into := filepath.Clean(args.Into)
	entries, err := os.ReadDir(into)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, err
	case len(entries) > 0:
		return nil, fmt.Errorf("%s is not empty", into)
	}
	return func(ctx context.Context, _ *opReport) (string, error) {
		if err := gitrepo.Clone(ctx, args.URL, into); err != nil {
			return "", gitError(nil, err)
		}
		return into, nil
	}, nil
}

func remoteName(ctx context.Context, top string) string {
	remotes, err := gitrepo.Remotes(ctx, top)
	if err != nil || len(remotes) == 0 {
		return ""
	}
	if slices.Contains(remotes, originRemote) {
		return originRemote
	}
	return remotes[0]
}

func remoteForDisplay(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" && u.Scheme != "https" {
		return raw
	}
	u.User = nil
	return u.String()
}

func divergence(ctx context.Context, top, fallback string) (int, int) {
	for _, ref := range []string{upstreamRef, fallback} {
		out, err := gitrepo.Inert(ctx, top, "rev-list", "--left-right", "--count", currentHead+"..."+ref, "--")
		if err != nil {
			continue
		}
		var ahead, behind int
		if _, err := fmt.Sscan(string(out), &ahead, &behind); err == nil {
			return behind, ahead
		}
	}
	return 0, 0
}

func bringsOf(top, folderDir string) brought {
	out := brought{Graphs: graphNames(folderDir), Hooks: []string{}, Instructions: []string{}, Skills: skillNames(folderDir)}
	for _, script := range []string{setupScript, teardownScript} {
		if info, err := os.Lstat(filepath.Join(top, script)); err == nil && info.Mode().IsRegular() {
			out.Hooks = append(out.Hooks, filepath.Base(script))
		}
	}
	for _, name := range instructionFiles {
		if info, err := os.Stat(filepath.Join(folderDir, name)); err == nil && info.Mode().IsRegular() {
			out.Instructions = append(out.Instructions, name)
		}
	}
	return out
}

func graphNames(dir string) []string {
	names := []string{}
	entries, err := os.ReadDir(filepath.Join(dir, graphsFolder))
	if err != nil {
		return names
	}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), graphExt); ok && graphNameRe.MatchString(name) {
			names = append(names, name)
		}
	}
	return names
}

func skillNames(dir string) []string {
	names := []string{}
	root := filepath.Join(dir, skillsFolder)
	entries, err := os.ReadDir(root)
	if err != nil {
		return names
	}
	for _, e := range entries {
		if info, err := os.Stat(filepath.Join(root, e.Name(), skillFile)); err == nil && info.Mode().IsRegular() {
			names = append(names, e.Name())
		}
	}
	return names
}

func trustedGraphNames(ctx context.Context, h *pluginsdk.Host, workspaceID, dir string, graphs []string) []string {
	if len(graphs) == 0 {
		return []string{}
	}
	ctx, cancel := context.WithTimeout(ctx, trustedTimeout)
	defer cancel()
	var out trustedGraphs
	if err := h.RunCommand(ctx, trustedCommand, workspaceID, pathArgs{Path: dir}, &out); err != nil {
		logError(trustedCommand, err)
		return []string{}
	}
	return out.Graphs
}
