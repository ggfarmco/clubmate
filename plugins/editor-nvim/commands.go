package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/editorroot"
	"clubmate/plugins/internal/gitrepo"
)

const (
	lightMaxSize = 5 * 1024 * 1024
	binarySniff  = 8000
	rootBudget   = 55 * time.Second
)

var (
	errNoCurrentFile  = errors.New("no file in the current buffer")
	errBadPath        = errors.New("open: path must not contain line breaks")
	errNoPath         = errors.New("open: path is required")
	errUnknownCommand = errors.New("unknown command")
	errDiffviewCwd    = errors.New("diffview: cwd must be absolute")
	errDiffviewRev    = errors.New("diffview: rev must be a commit sha")
	errDiffviewBase   = errors.New("diffview: pass a commit sha as rev or a branch as base")
	errStopRoot       = errors.New("stop-root: root must be absolute")
	errChecktimeRoot  = errors.New("checktime: root must be absolute")
	errUnsavedRoot    = errors.New("unsaved: root must be absolute")
	errNoSelection    = errors.New("no selection")
	shaPattern        = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

type openArgs struct {
	Path  string `json:"path"`
	Line  int    `json:"line"`
	Col   int    `json:"col"`
	Focus bool   `json:"focus"`
}

type diffviewArgs struct {
	Cwd     string `json:"cwd"`
	Rev     string `json:"rev"`
	Base    string `json:"base"`
	Session string `json:"session"`
	Turn    int    `json:"turn"`
}

type rootArgs struct {
	Root  string `json:"root"`
	Force bool   `json:"force"`
}

type restartArgs struct {
	Force bool `json:"force"`
}

type reviewShowArgs struct {
	ID string `json:"id"`
}

type terminalArgs struct {
	Cwd string `json:"cwd"`
}

type selectionResult struct {
	Path      string `json:"path" msgpack:"path"`
	StartLine int    `json:"startLine" msgpack:"startLine"`
	EndLine   int    `json:"endLine" msgpack:"endLine"`
	Text      string `json:"text" msgpack:"text"`
}

func (a *app) command(ctx context.Context, h *pluginsdk.Host, cmd pluginsdk.CommandRun) (any, error) {
	name := strings.TrimPrefix(cmd.ID, h.PluginID()+".")
	if a.isDeleted(cmd.WorkspaceID) {
		return nil, nil
	}
	switch name {
	case "open":
		var args openArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("open args: %w", err)
		}
		return nil, a.open(ctx, h, cmd.WorkspaceID, args)
	case "code-root":
		var args editorroot.CodeRootArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("code-root args: %w", err)
		}
		return a.codeRoot(ctx, h, cmd.WorkspaceID, args)
	case "quickfix":
		var args quickfixArgs
		dec := json.NewDecoder(bytes.NewReader(cmd.Args))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&args); err != nil {
			return nil, fmt.Errorf("quickfix args: %w", err)
		}
		return nil, a.quickfix(ctx, h, cmd.WorkspaceID, args)
	case "diffview":
		var args diffviewArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("diffview args: %w", err)
		}
		return struct{}{}, a.diffview(ctx, h, cmd.WorkspaceID, args)
	case "review":
		return a.reviewCommand(ctx, h, cmd)
	case "review-show":
		var args reviewShowArgs
		if err := json.Unmarshal(cmd.Args, &args); err != nil {
			return nil, fmt.Errorf("review-show args: %w", err)
		}
		return struct{}{}, a.showReview(ctx, h, cmd.WorkspaceID, args.ID)
	case "checktime":
		if _, err := decodeRoot(name, cmd.Args, errChecktimeRoot); err != nil {
			return nil, err
		}
		return nil, a.checktime(cmd.WorkspaceID)
	case "stop-root":
		args, err := decodeRoot(name, cmd.Args, errStopRoot)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, rootBudget)
		defer cancel()
		return nil, a.stopRoot(ctx, h, cmd.WorkspaceID, args.Root, args.Force)
	case "unsaved":
		args, err := decodeRoot(name, cmd.Args, errUnsavedRoot)
		if err != nil {
			return nil, err
		}
		return a.unsaved(cmd.WorkspaceID, args.Root)
	case "guard-switch":
		args, err := decodeRoot(name, cmd.Args, errUnsavedRoot)
		if err != nil {
			return nil, err
		}
		return nil, a.guardSwitch(cmd.WorkspaceID, args.Root)
	case "release-root":
		args, err := decodeRoot(name, cmd.Args, errStopRoot)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, rootBudget)
		defer cancel()
		return nil, a.releaseRoot(ctx, h, cmd.WorkspaceID, args.Root)
	case "import-config":
		return a.importConfig(ctx, h, cmd.WorkspaceID)
	}
	e, err := a.active(ctx, h, cmd.WorkspaceID)
	if e == nil || err != nil {
		return nil, err
	}
	switch name {
	case "reveal":
		file := e.current().File
		if file == "" {
			return nil, errNoCurrentFile
		}
		return nil, exec.CommandContext(ctx, "open", "-R", file).Run()
	case "selection":
		c := e.connected()
		if c == nil {
			return nil, errNoSelection
		}
		return c.selection()
	case "terminal":
		return nil, a.terminal(ctx, h, e)
	case "restart":
		var args restartArgs
		if len(cmd.Args) > 0 {
			if err := json.Unmarshal(cmd.Args, &args); err != nil {
				return nil, fmt.Errorf("restart args: %w", err)
			}
		}
		return nil, a.restart(ctx, h, e, true, args.Force)
	}
	return nil, fmt.Errorf("%w: %q", errUnknownCommand, cmd.ID)
}

func (a *app) open(ctx context.Context, h *pluginsdk.Host, workspaceID string, args openArgs) error {
	if args.Path == "" {
		return errNoPath
	}
	if strings.ContainsAny(args.Path, "\n\r") {
		return errBadPath
	}
	e, c, err := a.attach(ctx, h, workspaceID, args.Path)
	if c == nil || err != nil {
		return err
	}
	if err := c.open(args.Path, args.Line, args.Col, lightFile(args.Path)); err != nil {
		return err
	}
	if args.Focus {
		focusMain(ctx, h, e.id)
	}
	return nil
}

func (a *app) diffview(ctx context.Context, h *pluginsdk.Host, workspaceID string, args diffviewArgs) error {
	if !filepath.IsAbs(args.Cwd) {
		return errDiffviewCwd
	}
	if (args.Rev == "") == (args.Base == "") || (args.Base != "" && !revPattern.MatchString(args.Base)) {
		return errDiffviewBase
	}
	if args.Rev != "" && !shaPattern.MatchString(args.Rev) {
		return errDiffviewRev
	}
	cwd := filepath.Clean(args.Cwd)
	if top, ok := gitrepo.RepoRoot(cwd); ok {
		cwd = gitrepo.Canonical(top)
	}
	req := openReviewReq{Key: reviewKey{Worktree: cwd, Base: args.Rev}, BaseRev: args.Rev}
	if args.Base != "" {
		rev, left, err := reviewBase(ctx, cwd, args.Base, "")
		if err != nil {
			return err
		}
		req = openReviewReq{Key: reviewKey{Worktree: cwd, Base: args.Base}, BaseRev: rev, Left: left}
	} else if args.Turn > 0 {
		req.Left = fmt.Sprintf("Before turn %d", args.Turn)
	}
	if args.Session != "" {
		s := a.agentSession(ctx, h, workspaceID, args.Session)
		req.Opener, req.OpenerTitle = args.Session, s.Title
	}
	_, err := a.openReview(ctx, h, workspaceID, req)
	return err
}

func (a *app) checktime(workspaceID string) error {
	var errs []error
	for _, e := range a.workspaceEditors(workspaceID) {
		if c := e.connected(); c != nil {
			errs = append(errs, c.checktime())
		}
	}
	return errors.Join(errs...)
}

func (a *app) terminal(ctx context.Context, h *pluginsdk.Host, e *editor) error {
	cwd := e.root
	if file := e.current().File; file != "" {
		cwd = filepath.Dir(file)
	}
	return h.RunCommand(ctx, "terminals.new", e.id, terminalArgs{Cwd: cwd}, nil)
}

func (a *app) attach(ctx context.Context, h *pluginsdk.Host, workspaceID, path string) (*editor, *client, error) {
	e, err := a.target(ctx, h, workspaceID, path)
	if e == nil || err != nil {
		return nil, nil, err
	}
	c, err := a.ensure(ctx, h, e)
	if err != nil {
		return nil, nil, err
	}
	return e, c, nil
}

func decodeRoot(name string, raw json.RawMessage, errRelative error) (rootArgs, error) {
	var args rootArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return rootArgs{}, fmt.Errorf("%s args: %w", name, err)
	}
	if !filepath.IsAbs(args.Root) {
		return rootArgs{}, errRelative
	}
	args.Root = filepath.Clean(args.Root)
	return args, nil
}

func focusMain(ctx context.Context, h *pluginsdk.Host, workspaceID string) {
	if err := h.Focus(ctx, workspaceID, h.PluginID(), mainSlot); err != nil {
		logError("focus", err)
	}
}

func lightFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if info.Size() > lightMaxSize {
		return true
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, binarySniff)
	n, _ := io.ReadFull(f, buf)
	return bytes.IndexByte(buf[:n], 0) >= 0
}
