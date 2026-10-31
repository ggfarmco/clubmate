package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"clubmate/pkg/pluginsdk"
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
	actRetry           = "retry"
	actContinue        = "continue"
	actOpenHandoff     = "openHandoff"
	actSignIn          = "signIn"
	actUnqueue         = "unqueue"
	actSendQueued      = "sendQueued"
	actMerge           = "merge"

	handoffFromNode = "handoffFrom"
	handoffToNode   = "handoffTo"

	selectionCmd = "editor-nvim.selection"
	openCmd      = "editor.open"
	diffviewCmd  = "editor-nvim.diffview"
)

var (
	errUnknownAction = errors.New("unknown action")
	errNoBranch      = errors.New("type a branch name for the new worktree")
	errNoSelection   = errors.New("no selection in the editor")
	errNoCheckpoint  = errors.New("no checkpoint to review yet")
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
	if rerr := a.render(ctx, ev.ProjectID); err == nil {
		err = rerr
	}
	switch {
	case err != nil:
	case ev.Action == actNew || ev.Action == actNewBranch:
		err = h.Focus(ctx, ev.ProjectID, sectionID, mainSlot)
	case ev.Action == actCodeNew:
		err = h.Focus(ctx, ev.ProjectID, editorSection, codeSlot)
	case ev.Action == actShowBranch:
		err = h.FocusField(ctx, ev.ProjectID, sectionID)
	}
	return nil, err
}

func (a *app) act(ctx context.Context, ev pluginsdk.UIEvent) error {
	switch ev.Action {
	case actNew:
		res, err := a.newSession(ctx, a.host, ev.ProjectID, "", newArgs{})
		if err != nil {
			return err
		}
		return a.selectSession(ctx, ev.ProjectID, res.Session)
	case actCodeNew:
		res, err := a.newSession(ctx, a.host, ev.ProjectID, "", newArgs{})
		if err != nil {
			return err
		}
		return a.bindCode(ctx, ev.ProjectID, res.Session)
	case actCodeSelect:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		if _, err := a.lookup(ev.ProjectID, id); err != nil {
			return err
		}
		return a.bindCode(ctx, ev.ProjectID, id)
	case actShowBranch, actHideBranch:
		p := a.projectState(ev.ProjectID)
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
		res, err := a.newSession(ctx, a.host, ev.ProjectID, "", newArgs{Branch: branch})
		if err != nil {
			return err
		}
		p := a.projectState(ev.ProjectID)
		a.uiMu.Lock()
		p.branch = false
		a.uiMu.Unlock()
		return a.selectSession(ctx, ev.ProjectID, res.Session)
	case actSelect:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		if strings.HasPrefix(id, groupPrefix) {
			return nil
		}
		sid := strings.TrimPrefix(id, needsPrefix)
		s, err := a.lookup(ev.ProjectID, sid)
		if err != nil {
			return err
		}
		s.invalidateChanges()
		if err := a.selectSession(ctx, ev.ProjectID, sid); err != nil {
			return err
		}
		p := a.projectState(ev.ProjectID)
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
		return a.setCollapsed(ctx, ev.ProjectID, map[string]bool{key: ev.Action == actCollapse})
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
	s, err := a.lookup(ev.ProjectID, sid)
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
	return a.lookup(ev.ProjectID, strings.TrimPrefix(id, needsPrefix))
}

func (a *app) sessionAct(ctx context.Context, ev pluginsdk.UIEvent, s *session, node string) error {
	sid := s.snapshot().ID
	st := a.sessionState(ev.ProjectID, sid)
	switch ev.Action {
	case actSend:
		var v textValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.send(ev.ProjectID, s, st, v.Text)
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
	case actMode:
		var v modeValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.setMode(ctx, ev.ProjectID, modeArgs{Session: sid, Mode: v.Mode})
	case actConfig:
		var v configValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.setConfig(ctx, ev.ProjectID, configArgs{Session: sid, ID: v.ID, Value: v.Value})
	case actAttachFile:
		var v pathValue
		if err := decode(ev.Value, &v); err != nil || v.Path == "" {
			return errors.Join(err, errors.New("attachFile needs a path"))
		}
		a.addAttachment(st, attachment{Kind: "file", Path: v.Path})
		return nil
	case actAttachSelection:
		var sel selection
		if err := a.host.RunCommand(ctx, selectionCmd, ev.ProjectID, nil, &sel); err != nil {
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
		if err := a.host.RunCommand(ctx, openCmd, ev.ProjectID, v, nil); err != nil {
			return err
		}
		return a.host.Focus(ctx, ev.ProjectID, editorSection, editorSlot)
	case actReview:
		var v turnValue
		if err := decode(ev.Value, &v); err != nil {
			return err
		}
		return a.review(ctx, ev.ProjectID, s, v.Turn)
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
		return a.signIn(ctx, ev.ProjectID, s, st)
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
		return a.mergeToMain(ctx, ev.ProjectID, s)
	}
	return fmt.Errorf("%w: %q", errUnknownAction, ev.Action)
}

func (a *app) show(ctx context.Context, ev pluginsdk.UIEvent, sid string) error {
	if ev.Slot == codeSlot {
		return a.bindCode(ctx, ev.ProjectID, sid)
	}
	return a.selectSession(ctx, ev.ProjectID, sid)
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

func (a *app) send(projectID string, s *session, st *sessionUI, text string) error {
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
	if err := a.prompt(projectID, promptArgs{Session: s.snapshot().ID, Text: text, Attachments: atts}); err != nil {
		return err
	}
	a.uiMu.Lock()
	st.attachments = nil
	st.gen++
	a.uiMu.Unlock()
	return nil
}

func (a *app) review(ctx context.Context, projectID string, s *session, turn int) error {
	v := s.view(0)
	if turn == 0 {
		turn = firstCheckpoint(v.Turns).Turn
	}
	i := slices.IndexFunc(v.Turns, func(t turnInfo) bool { return t.Turn == turn && t.CheckpointSha != "" })
	if i < 0 {
		return errNoCheckpoint
	}
	args := diffviewArgs{Cwd: v.Session.Worktree, Rev: v.Turns[i].CheckpointSha}
	return a.host.RunCommand(ctx, diffviewCmd, projectID, args, nil)
}
