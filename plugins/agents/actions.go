package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
)

const (
	actNew         = "new"
	actNewBranch   = "newBranch"
	actShowBranch  = "showBranch"
	actHideBranch  = "hideBranch"
	actCodeNew     = "codeNew"
	actCodeSelect  = "codeSelect"
	actSelect      = "select"
	actExpand      = "expand"
	actCollapse    = "collapse"
	actClose       = "close"
	actDelete      = "delete"
	actHandoff     = "handoff"
	actNotifyAll   = "notifyAll"
	actNotifyNeeds = "notifyNeedsInput"
	actNotifyMuted = "notifyMuted"

	actSend            = "send"
	actStop            = "stop"
	actSelectParent    = "selectParent"
	actPermission      = "permission"
	actMode            = "mode"
	actConfig          = "config"
	actAttachFile      = "attachFile"
	actAttachSelection = "attachSelection"
	actDetach          = "detach"
	actOpenLocation    = "openLocation"
	actReview          = "review"
	actShowEarlier     = "showEarlier"
	actShowAll         = "showAll"
	actOpenOutput      = "openOutput"
	actRetry           = "retry"
	actContinue        = "continue"
	actOpenHandoff     = "openHandoff"
	actSignIn          = "signIn"
	actUnqueue         = "unqueue"
	actSendQueued      = "sendQueued"
	actMerge           = "merge"
	actStarter         = "starter"
	actStopSubagent    = "stopSubagent"

	handoffFromNode = "handoffFrom"
	handoffToNode   = "handoffTo"

	selectionCmd = "editor-nvim.selection"
	openCmd      = "editor.open"
	diffviewCmd  = "editor-nvim.diffview"

	outputDir = "output"
)

var (
	errUnknownAction = errors.New("unknown action")
	errNoBranch      = errors.New("type a branch name for the new worktree")
	errNoSelection   = errors.New("no selection in the editor")
	errNoCheckpoint  = errors.New("no checkpoint to review yet")
	errNoOutput      = errors.New("tool has no output")
)

type textValue struct {
	Text string `json:"text"`
}

type permissionValue struct {
	Request string `json:"request"`
	Option  string `json:"option"`
}

type modeValue struct {
	Mode string `json:"mode"`
}

type configValue struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type pathValue struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

type idValue struct {
	ID string `json:"id"`
}

type turnValue struct {
	Turn int `json:"turn"`
}

type selection struct {
	Path      string `json:"path"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Text      string `json:"text"`
}

type diffviewArgs struct {
	Cwd string `json:"cwd"`
	Rev string `json:"rev"`
}

func (a *app) uiEvent(ctx context.Context, h *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	select {
	case <-a.loaded:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	err := a.act(ctx, ev)
	if rerr := a.render(ctx, ev.WorkspaceID); err == nil {
		err = rerr
	}
	switch {
	case err != nil:
	case ev.Action == actNew || ev.Action == actNewBranch:
		err = h.Focus(ctx, ev.WorkspaceID, sectionID, mainSlot)
	case ev.Action == actCodeNew:
		err = h.Focus(ctx, ev.WorkspaceID, editorSection, codeSlot)
	case ev.Action == actShowBranch:
		err = h.FocusField(ctx, ev.WorkspaceID, sectionID)
	}
	return nil, err
}

func (a *app) act(ctx context.Context, ev pluginsdk.UIEvent) error {
	if sid, ok := isContextEvent(ev); ok {
		return a.contextAct(ev, sid)
	}
	switch ev.Action {
	case actNew:
		res, err := a.newSession(ctx, a.host, ev.WorkspaceID, "", newArgs{})
		if err != nil {
			return err
		}
		return a.selectSession(ctx, ev.WorkspaceID, res.Session)
	case actCodeNew:
		res, err := a.newSession(ctx, a.host, ev.WorkspaceID, "", newArgs{})
		if err != nil {
			return err
		}
		return a.bindCode(ctx, ev.WorkspaceID, res.Session)
	case actCodeSelect:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		if _, err := a.lookup(ev.WorkspaceID, id); err != nil {
			return err
		}
		return a.bindCode(ctx, ev.WorkspaceID, id)
	case actShowBranch, actHideBranch:
		p := a.workspaceState(ev.WorkspaceID)
		a.uiMu.Lock()
		p.branch = ev.Action == actShowBranch
		a.uiMu.Unlock()
		return nil
	case actNewBranch:
		var args newArgs
		if err := decode(ev.Value, &args); err != nil {
			return err
		}
		branch := strings.TrimSpace(args.Branch)
		if branch == "" {
			return errNoBranch
		}
		res, err := a.newSession(ctx, a.host, ev.WorkspaceID, "", newArgs{Branch: branch})
		if err != nil {
			return err
		}
		p := a.workspaceState(ev.WorkspaceID)
		a.uiMu.Lock()
		p.branch = false
		a.uiMu.Unlock()
		return a.selectSession(ctx, ev.WorkspaceID, res.Session)
	case actSelect:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		if strings.HasPrefix(id, groupPrefix) {
			return nil
		}
		sid := strings.TrimPrefix(id, needsPrefix)
		s, err := a.lookup(ev.WorkspaceID, sid)
		if err != nil {
			return err
		}
		s.invalidateChanges()
		if err := a.selectSession(ctx, ev.WorkspaceID, sid); err != nil {
			return err
		}
		p := a.workspaceState(ev.WorkspaceID)
		a.uiMu.Lock()
		p.row = id
		a.uiMu.Unlock()
		return nil
	case actExpand, actCollapse:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		key := expandPrefix + id
		if wt, ok := strings.CutPrefix(id, groupPrefix); ok {
			key = wt
		}
		return a.setCollapsed(ctx, ev.WorkspaceID, map[string]bool{key: ev.Action == actCollapse})
	case actClose, actDelete, actHandoff:
		s, err := a.rowSession(ev)
		if err != nil {
			return err
		}
		switch ev.Action {
		case actDelete:
			return a.deleteTree(ctx, s)
		case actHandoff:
			return a.handoffAndSelect(ctx, ev, s)
		}
		a.closeTree(ctx, s)
		return nil
	case actNotifyAll, actNotifyNeeds, actNotifyMuted:
		s, err := a.rowSession(ev)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(notifyMenu, func(n notifyChoice) bool { return n.action == ev.Action })
		a.setNotify(s, notifyMenu[i].policy)
		return nil
	}
	sid, rest, _ := strings.Cut(ev.NodeID, "/")
	s, err := a.lookup(ev.WorkspaceID, sid)
	if err != nil {
		return err
	}
	return a.sessionAct(ctx, ev, s, rest)
}

func itemID(ev pluginsdk.UIEvent) (string, error) {
	var id string
	if err := json.Unmarshal(ev.Value, &id); err != nil {
		return "", fmt.Errorf("%s value: %w", ev.Action, err)
	}
	return id, nil
}

func (a *app) rowSession(ev pluginsdk.UIEvent) (*session, error) {
	id, err := itemID(ev)
	if err != nil {
		return nil, err
	}
	return a.lookup(ev.WorkspaceID, strings.TrimPrefix(id, needsPrefix))
}

func (a *app) sessionAct(ctx context.Context, ev pluginsdk.UIEvent, s *session, node string) error {
	sid := s.snapshot().ID
	st := a.sessionState(ev.WorkspaceID, sid)
	switch ev.Action {
	case actSend:
		var v textValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.send(ev.WorkspaceID, s, st, v.Text)
	case actStop:
		a.cancel(s)
		return nil
	case actSelectParent:
		parent := s.snapshot().ParentSession
		if a.session(parent) == nil {
			return nil
		}
		return a.show(ctx, ev, parent)
	case actPermission:
		var v permissionValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return s.answer(v.Request, v.Option)
	case actElicitation, actElicitPick, actElicitDecline, actElicitOpen:
		return a.elicitAct(ev, s, node)
	case actMode:
		var v modeValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.setMode(ctx, ev.WorkspaceID, modeArgs{Session: sid, Mode: v.Mode})
	case actConfig:
		var v configValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.setConfig(ctx, ev.WorkspaceID, configArgs{Session: sid, ID: v.ID, Value: v.Value})
	case actAttachFile:
		var v pathValue
		if err := decode(ev.Value, &v); err != nil || v.Path == "" {
			return errors.Join(err, errors.New("attachFile needs a path"))
		}
		a.addAttachment(st, attachment{Kind: "file", Path: v.Path})
		return nil
	case actAttachSelection:
		var sel selection
		if err := a.host.RunCommand(ctx, selectionCmd, ev.WorkspaceID, nil, &sel); err != nil {
			return err
		}
		if sel.Path == "" {
			return errNoSelection
		}
		a.addAttachment(st, attachment{Kind: "selection", Path: sel.Path, StartLine: sel.StartLine, EndLine: sel.EndLine, Text: sel.Text})
		return nil
	case actDetach:
		var v idValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		a.uiMu.Lock()
		st.attachments = slices.DeleteFunc(st.attachments, func(p pendingAttachment) bool { return p.ID == v.ID })
		a.uiMu.Unlock()
		return nil
	case actOpenLocation:
		var v pathValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		if err := a.host.RunCommand(ctx, openCmd, ev.WorkspaceID, v, nil); err != nil {
			return err
		}
		return a.host.Focus(ctx, ev.WorkspaceID, editorSection, editorSlot)
	case actReview:
		var v turnValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.review(ctx, ev.WorkspaceID, s, v.Turn)
	case actShowEarlier:
		a.uiMu.Lock()
		st.showEarlier()
		a.uiMu.Unlock()
		return nil
	case actShowAll:
		tool, ok := strings.CutPrefix(node, "tool/")
		if !ok {
			return fmt.Errorf("%w: showAll on %q", errUnknownAction, ev.NodeID)
		}
		a.uiMu.Lock()
		st.expanded[tool] = true
		a.uiMu.Unlock()
		return nil
	case actOpenOutput:
		tool, ok := strings.CutPrefix(node, "tool/")
		if !ok {
			return fmt.Errorf("%w: openOutput on %q", errUnknownAction, ev.NodeID)
		}
		path, err := a.writeOutput(s, tool)
		if err != nil {
			return err
		}
		if err := a.host.RunCommand(ctx, openCmd, ev.WorkspaceID, pathValue{Path: path}, nil); err != nil {
			return err
		}
		return a.host.Focus(ctx, ev.WorkspaceID, editorSection, editorSlot)
	case actStopSubagent:
		tool, ok := strings.CutPrefix(node, "tool/")
		if !ok {
			return fmt.Errorf("%w: stopSubagent on %q", errUnknownAction, ev.NodeID)
		}
		return s.stopSubagent(tool)
	case actRetry:
		a.resume(s)
		return nil
	case actContinue:
		return a.handoffAndSelect(ctx, ev, s)
	case actOpenHandoff:
		rec := s.snapshot()
		target := rec.HandoffTo
		if node == handoffFromNode {
			target = rec.HandoffFrom
		}
		if a.session(target) == nil {
			return nil
		}
		return a.show(ctx, ev, target)
	case actSignIn:
		return a.signIn(ctx, ev.WorkspaceID, s, st)
	case actUnqueue:
		id, ok := strings.CutPrefix(node, queuePrefix)
		if !ok {
			return fmt.Errorf("%w: unqueue on %q", errUnknownAction, ev.NodeID)
		}
		s.unqueue(id)
		return nil
	case actSendQueued:
		return a.submit(s, nil)
	case actMerge:
		return a.mergeToMain(ctx, ev.WorkspaceID, s)
	case actStarter:
		return a.start(ev.WorkspaceID, s, node)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) start(workspaceID string, s *session, node string) error {
	n, err := strconv.Atoi(strings.TrimPrefix(node, starterPrefix))
	rec := s.snapshot()
	file := a.editorFile(workspaceID, rec.Worktree)
	list := starters(file)
	if err != nil || n < 1 || n > len(list) {
		return fmt.Errorf("%w: starter %q", errUnknownAction, node)
	}
	args := promptArgs{Session: rec.ID, Text: list[n-1].prompt}
	if list[n-1].attach {
		args.Attachments = []attachment{{Kind: "file", Path: file}}
	}
	return a.prompt(workspaceID, args)
}

func (a *app) show(ctx context.Context, ev pluginsdk.UIEvent, sid string) error {
	if ev.Slot == codeSlot {
		return a.bindCode(ctx, ev.WorkspaceID, sid)
	}
	return a.selectSession(ctx, ev.WorkspaceID, sid)
}

func (a *app) handoffAndSelect(ctx context.Context, ev pluginsdk.UIEvent, s *session) error {
	next, err := a.handoff(ctx, s, handoffArgs{}, "")
	if err != nil {
		return err
	}
	return a.show(ctx, ev, next.snapshot().ID)
}

func (a *app) addAttachment(st *sessionUI, att attachment) {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	st.nextID++
	st.attachments = append(st.attachments, pendingAttachment{ID: "a" + strconv.Itoa(st.nextID), attachment: att})
}

func (a *app) send(workspaceID string, s *session, st *sessionUI, text string) error {
	a.uiMu.Lock()
	pending := st.attachments
	a.uiMu.Unlock()
	if strings.TrimSpace(text) == "" && len(pending) == 0 {
		return nil
	}
	atts := make([]attachment, 0, len(pending))
	for _, p := range pending {
		atts = append(atts, p.attachment)
	}
	if err := a.prompt(workspaceID, promptArgs{Session: s.snapshot().ID, Text: text, Attachments: atts}); err != nil {
		return err
	}
	a.uiMu.Lock()
	st.attachments = nil
	st.gen++
	a.uiMu.Unlock()
	return nil
}

func (a *app) writeOutput(s *session, tool string) (string, error) {
	rec := s.snapshot()
	raw, ok := s.tool(tool)
	if !ok {
		return "", fmt.Errorf("%w: tool %q", errNoOutput, tool)
	}
	f, kind, command := parseTool(raw)
	body := toolBody(kind, command, rec.Worktree, f.Content)
	prompt, _ := f.RawInput["prompt"].(string)
	if body == nil && prompt == "" {
		return "", fmt.Errorf("%w: tool %q", errNoOutput, tool)
	}
	body = cmp.Or(body, &sdui.ToolBody{})
	ext := map[string]string{"diff": "diff", "terminal": "log"}[body.Format]
	name := rec.ID + "-" + tool + "." + cmp.Or(ext, "txt")
	if filepath.Base(name) != name {
		return "", fmt.Errorf("%w: tool %q", errNoOutput, tool)
	}
	dir := filepath.Join(a.stateDir, outputDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	text := body.Text
	if prompt != "" {
		text = strings.TrimRight(prompt+"\n\n"+text, "\n")
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func (a *app) review(ctx context.Context, workspaceID string, s *session, turn int) error {
	v := s.view(0)
	if turn == 0 {
		turn = firstCheckpoint(v.Turns).Turn
	}
	i := slices.IndexFunc(v.Turns, func(t turnInfo) bool { return t.Turn == turn && t.CheckpointSha != "" })
	if i < 0 {
		return errNoCheckpoint
	}
	args := diffviewArgs{Cwd: v.Session.Worktree, Rev: v.Turns[i].CheckpointSha}
	return a.host.RunCommand(ctx, diffviewCmd, workspaceID, args, nil)
}
