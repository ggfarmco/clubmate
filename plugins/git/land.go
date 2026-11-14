package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
)

const (
	landModeBranch = "branch"
	landModeIndex  = "index"
	landGateCmd    = "agents.land-gate"
	landDoneCmd    = "agents.land-done"
	landApproved   = "approved"
	nothingToLand  = "%s adds nothing to %s; nothing landed"
)

var (
	errLandMessage   = errors.New("message is required: the commit message of the landed change")
	errLandMode      = errors.New("mode is branch or index")
	errLandOnTarget  = errors.New("this worktree is on the branch it would land into: stage the change and call git_land with mode index")
	errLandNotTarget = errors.New("mode index lands the staged change of a worktree that has the target branch checked out")
	errLandDetached  = errors.New("the worktree is on no branch")
	errLandStaged    = errors.New("the target worktree has staged changes; commit or unstage them first")
	errLandNothing   = errors.New("nothing is staged to land")
	errLandMoved     = errors.New("the target branch moved while landing; call git_land again")
	errLandTree      = errors.New("the squashed result differs from the reviewed one; call git_land again")
	errNoLandTarget  = errors.New("no worktree has the target branch checked out")
)

type landGateCall struct {
	Session  string `json:"session"`
	Target   string `json:"target"`
	Worktree string `json:"worktree"`
	Into     string `json:"into"`
	Source   string `json:"source"`
	Head     string `json:"head"`
	Old      string `json:"old"`
	Tree     string `json:"tree"`
	Commit   string `json:"commit,omitempty"`
}

type landGateReply struct {
	State string `json:"state"`
	Text  string `json:"text"`
}

type landResult struct {
	Commit  string `json:"commit,omitempty"`
	Into    string `json:"into"`
	Source  string `json:"source,omitempty"`
	Removed bool   `json:"removed,omitempty"`
	Note    string `json:"note,omitempty"`
}

type landPlan struct {
	folder *folder
	own    gitrepo.Worktree
	target gitrepo.Worktree
	into   string
	source string
	head   string
	old    string
	tree   string
	keep   bool
}

func landCommand(ctx context.Context, a *app, c call) (any, error) {
	a.landMu.Lock()
	defer a.landMu.Unlock()
	p, err := planLand(ctx, c)
	if err != nil {
		return nil, err
	}
	if p.tree == "" {
		return p.cleanup(ctx, c.h, c.workspaceID, landResult{Into: p.into, Source: p.source, Note: fmt.Sprintf(nothingToLand, p.source, p.into)}), nil
	}
	gate := landGateCall{Session: c.caller, Target: p.target.Path, Worktree: p.own.Path, Into: p.into, Source: p.source, Head: p.head, Old: p.old, Tree: p.tree}
	if c.agent {
		var reply landGateReply
		if err := c.h.RunCommand(ctx, landGateCmd, c.workspaceID, gate, &reply); err != nil {
			return nil, err
		}
		if reply.State != landApproved {
			return nil, errors.New(reply.Text)
		}
	}
	commit, err := p.commit(ctx, c.args.Message)
	if err != nil {
		return nil, err
	}
	if c.agent {
		gate.Commit = commit
		logError("record landing", c.h.RunCommand(ctx, landDoneCmd, c.workspaceID, gate, nil))
	}
	return p.cleanup(ctx, c.h, c.workspaceID, landResult{Commit: commit, Into: p.into, Source: p.source}), nil
}

func planLand(ctx context.Context, c call) (*landPlan, error) {
	args := c.args
	if strings.TrimSpace(args.Message) == "" {
		return nil, errLandMessage
	}
	mode := cmp.Or(args.Mode, landModeBranch)
	if mode != landModeBranch && mode != landModeIndex {
		return nil, errLandMode
	}
	if !filepath.IsAbs(args.Path) {
		return nil, errRelativePath
	}
	f, own, err := folderOf(ctx, c, args.Path)
	if err != nil {
		return nil, err
	}
	p := &landPlan{folder: f, own: own, into: args.Into}
	if p.into == "" {
		if p.into, err = gitrepo.DefaultBranch(ctx, f.Trees[0].Path); err != nil {
			return nil, err
		}
	}
	i := -1
	for j, t := range f.Trees {
		if t.Branch == p.into && !t.Bare {
			i = j
			break
		}
	}
	if i < 0 {
		return nil, fmt.Errorf("%w: %s", errNoLandTarget, p.into)
	}
	p.target = f.Trees[i]
	if p.old, err = revParse(ctx, p.target.Path, headsPrefix+p.into); err != nil {
		return nil, err
	}
	oldTree, err := revParse(ctx, p.target.Path, p.old+"^{tree}")
	if err != nil {
		return nil, err
	}
	if mode == landModeIndex {
		if own.Real != p.target.Real {
			return nil, errLandNotTarget
		}
		if p.tree, err = writeTree(ctx, own.Path); err != nil {
			return nil, err
		}
		if p.tree == oldTree {
			return nil, errLandNothing
		}
		return p, nil
	}
	switch {
	case own.Branch == "":
		return nil, errLandDetached
	case own.Branch == p.into:
		return nil, errLandOnTarget
	}
	p.source = own.Branch
	def, _ := gitrepo.DefaultBranch(ctx, f.Trees[0].Path)
	p.keep = p.source == def || own.Real == f.Trees[0].Real
	if p.head, err = revParse(ctx, own.Path, headsPrefix+p.source); err != nil {
		return nil, err
	}
	if p.tree, err = mergeTree(ctx, p.target.Path, p.old, p.head, p.into); err != nil {
		return nil, err
	}
	if p.tree == oldTree {
		p.tree = ""
	}
	return p, nil
}

func folderOf(ctx context.Context, c call, path string) (*folder, gitrepo.Worktree, error) {
	var found *folder
	var tree gitrepo.Worktree
	err := eachFolder(ctx, c.h, c.workspaceID, func(f *folder) error {
		if t, ok := f.Member(path); ok && found == nil {
			found, tree = f, t
		}
		return nil
	})
	if err == nil && found == nil {
		err = fmt.Errorf("%s is not a worktree of a git folder of this workspace", path)
	}
	return found, tree, err
}

func (p *landPlan) commit(ctx context.Context, message string) (string, error) {
	dir := p.target.Path
	if head, err := revParse(ctx, dir, currentHead); err != nil || head != p.old {
		return "", errLandMoved
	}
	if p.source != "" {
		if _, err := run(ctx, dir, "diff", "--cached", "--quiet"); err != nil {
			return "", errLandStaged
		}
		if _, err := mutate(ctx, dir, "merge", "--squash", p.head); err != nil {
			p.undo(ctx)
			return "", err
		}
	}
	if tree, err := writeTree(ctx, dir); err != nil || tree != p.tree {
		if p.source != "" {
			p.undo(ctx)
		}
		return "", errLandTree
	}
	if _, err := mutate(ctx, dir, "commit", "-q", "-m", message); err != nil {
		if p.source != "" {
			p.undo(ctx)
		}
		return "", err
	}
	return revParse(ctx, dir, currentHead)
}

func (p *landPlan) undo(ctx context.Context) {
	_, err := mutate(ctx, p.target.Path, "reset", "-q", "--merge")
	logError("undo squash", err)
}

func (p *landPlan) cleanup(ctx context.Context, h *pluginsdk.Host, workspaceID string, res landResult) landResult {
	if p.source == "" || p.keep {
		return res
	}
	top := p.folder.Trees[0].Path
	if terminals, err := worktreeSessions(ctx, h, workspaceID, p.own.Path); err != nil || len(terminals) > 0 {
		res.Note = strings.TrimSpace(res.Note + " Kept the worktree and branch: terminals still run in it.")
		return res
	}
	if _, err := mutate(ctx, top, "worktree", "remove", p.own.Path); err != nil {
		res.Note = strings.TrimSpace(res.Note + " Kept the worktree: " + err.Error())
		return res
	}
	res.Removed = true
	if _, err := mutate(ctx, top, "branch", "-D", p.source); err != nil {
		res.Note = strings.TrimSpace(res.Note + " Kept the branch: " + err.Error())
	}
	return res
}

func revParse(ctx context.Context, dir, rev string) (string, error) {
	out, err := run(ctx, dir, "rev-parse", "-q", "--verify", rev)
	return strings.TrimSpace(string(out)), err
}

func writeTree(ctx context.Context, dir string) (string, error) {
	out, err := run(ctx, dir, "write-tree")
	return strings.TrimSpace(string(out)), err
}

func mergeTree(ctx context.Context, dir, old, head, into string) (string, error) {
	out, err := gitrepo.Run(ctx, dir, "merge-tree", "--write-tree", "--no-messages", "--name-only", old, head)
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if ee, ok := errors.AsType[*exec.ExitError](err); ok && ee.ExitCode() == 1 && len(lines) > 1 {
		return "", fmt.Errorf("it conflicts with %s in %s; rebase onto %s first", into, strings.Join(lines[1:], ", "), into)
	}
	if err != nil {
		return "", gitError(out, err)
	}
	return lines[0], nil
}
