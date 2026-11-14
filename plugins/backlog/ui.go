package main

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"clubmate/pkg/contract"
	"clubmate/pkg/docrender"
	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/agentcall"
)

const (
	kindDeleted    = "deleted"
	sessionError   = "error"
	sessionRunning = "running"
)

var (
	errUnknownAction = errors.New("unknown action")
	sessionStates    = map[string]string{"done": "turn done", sessionError: "failed"}
)

type workspaceUI struct {
	render     sync.Mutex
	selected   int
	collapsed  map[string]bool
	formRev    int
	fieldRev   int
	sent       map[string][32]byte
	fieldError fieldError
	editing    int
	prompt     int
	tagFilter  string
	query      string
	tagsOpen   bool
}

type fieldError struct {
	task  int
	field string
	text  string
}

func (b *backlog) workspace(workspaceID string) *workspaceUI {
	b.mu.Lock()
	defer b.mu.Unlock()
	w, ok := b.ui[workspaceID]
	if !ok {
		w = &workspaceUI{collapsed: map[string]bool{statusDone: true}, sent: map[string][32]byte{}}
		b.ui[workspaceID] = w
	}
	return w
}

func (b *backlog) isPresent(workspaceID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.present[workspaceID]
}

func (b *backlog) presentIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Collect(maps.Keys(b.present))
}

func (b *backlog) setPresent(workspaceID string, on bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if on {
		b.present[workspaceID] = true
		return
	}
	delete(b.present, workspaceID)
	if w, ok := b.ui[workspaceID]; ok {
		w.sent = map[string][32]byte{}
	}
}

func (b *backlog) dropUI(workspaceID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.present, workspaceID)
	delete(b.ui, workspaceID)
}

func (b *backlog) loadPresence(ctx context.Context) error {
	present, err := b.h.Presence(ctx)
	if err != nil {
		return err
	}
	for _, id := range present {
		b.setPresent(id, true)
	}
	for _, id := range present {
		b.render(ctx, id)
	}
	return nil
}

func (b *backlog) presenceEvent(ctx context.Context, ev pluginsdk.Event) {
	if ev.Topic != "ui.opened" && ev.Topic != "ui.idle" {
		return
	}
	var p contract.UIPresence
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		logError(ev.Topic, err)
		return
	}
	b.setPresent(p.Workspace, ev.Topic == "ui.opened")
	if ev.Topic == "ui.opened" {
		b.render(ctx, p.Workspace)
	}
}

func (b *backlog) render(ctx context.Context, workspaceID string) {
	if !b.isPresent(workspaceID) {
		return
	}
	logError("render "+workspaceID, b.renderNow(ctx, workspaceID))
}

func (b *backlog) renderNow(ctx context.Context, workspaceID string) error {
	w := b.workspace(workspaceID)
	w.render.Lock()
	defer w.render.Unlock()
	tasks, err := b.store.snapshot(ctx, workspaceID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	if w.tagFilter != "" && tagCounts(tasks)[w.tagFilter] == 0 {
		w.tagFilter = ""
	}
	selected, collapsed, rev, fieldRev, fieldErr, editing, prompt, filter, query, tagsOpen := w.selected, maps.Clone(w.collapsed), w.formRev, w.fieldRev, w.fieldError, w.editing, w.prompt, w.tagFilter, w.query, w.tagsOpen
	b.mu.Unlock()
	var current *Task
	byID := make(map[int]Task, len(tasks))
	for i := range tasks {
		byID[tasks[i].ID] = tasks[i]
		if tasks[i].ID == selected {
			current = &tasks[i]
		}
	}
	selectedID := ""
	if current != nil {
		selectedID = strconv.Itoa(current.ID)
	}
	owners := b.ownerLabels(ctx, workspaceID, current)
	if err := b.push(ctx, w, workspaceID, sidebarSlot, sidebarView(tasks, selectedID, collapsed, rev, filter, query, tagsOpen)); err != nil {
		return err
	}
	var errs map[string]string
	if fieldErr.task == selected && fieldErr.text != "" {
		errs = map[string]string{fieldErr.field: fieldErr.text}
	}
	d := detail{owners: owners, linked: byID, hasTasks: len(tasks) > 0, errs: errs, editing: current != nil && editing == current.ID, prompt: current != nil && prompt == current.ID, rev: rev + fieldRev}
	return b.push(ctx, w, workspaceID, mainSlot, mainView(current, d))
}

func (b *backlog) push(ctx context.Context, w *workspaceUI, workspaceID, slot string, n sdui.Node) error {
	raw, err := json.Marshal(n)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	b.mu.Lock()
	same := w.sent[slot] == sum
	b.mu.Unlock()
	if same {
		return nil
	}
	if _, err := b.h.Render(ctx, workspaceID, slot, n); err != nil {
		return err
	}
	b.mu.Lock()
	w.sent[slot] = sum
	b.mu.Unlock()
	return nil
}

func (b *backlog) ownerLabels(ctx context.Context, workspaceID string, current *Task) map[string]owner {
	ids := map[string]bool{}
	if current != nil {
		for _, m := range ownerHistory(*current) {
			ids[m.Owner] = true
		}
		ids[current.ClaimedBy], ids[current.LastClaimedBy], ids[current.CreatedBy] = true, true, true
		delete(ids, "")
	}
	owners := make(map[string]owner, len(ids))
	for id := range ids {
		if !strings.HasPrefix(id, sessionPfx) {
			owners[id] = owner{label: ownerLabel(id, "", false)}
			continue
		}
		s, _ := b.live.session(ctx, workspaceID, id)
		o := owner{session: true, stale: b.live.stale(ctx, workspaceID, id)}
		title := s.Title
		if s.Run != nil {
			o.run, o.runTitle = s.Run.ID, s.Run.Title
			title = cmp.Or(s.Run.Title+" · "+s.Title, title)
		}
		o.label = ownerLabel(id, title, o.stale)
		if !o.stale {
			o.state = cmp.Or(sessionStates[s.Status], s.Status)
			switch failure := s.failure(); {
			case s.Status == sessionRunning && s.Turns > 0:
				o.state += fmt.Sprintf(" · turn %d", s.Turns)
			case failure != "" && failure != sessionError:
				o.state += " · " + failure
			}
			if s.NeedsYou {
				o.state = "needs you"
			}
		}
		owners[id] = o
	}
	return owners
}

func (b *backlog) uiEvent(ctx context.Context, _ *pluginsdk.Host, ev pluginsdk.UIEvent) (any, error) {
	err := b.act(ctx, ev)
	b.render(ctx, ev.WorkspaceID)
	return nil, err
}

func (b *backlog) act(ctx context.Context, ev pluginsdk.UIEvent) error {
	w := b.workspace(ev.WorkspaceID)
	user := pluginsdk.CommandRun{WorkspaceID: ev.WorkspaceID}
	verb, _ := parseAction(ev.Action)
	switch {
	case changesTasks(ev, verb) && (ev.Agent || b.fromSession(ctx, ev.WorkspaceID, ev.Caller)):
		return fmt.Errorf("%w: %q is the user's; agents use the backlog commands", errUnknownAction, ev.Action)
	case ev.NodeID == "new" && ev.Action == "add":
		var args commandArgs
		if err := json.Unmarshal(ev.Value, &args); err != nil {
			return fmt.Errorf("add value: %w", err)
		}
		res, err := b.addCmd(ctx, user, commandArgs{Title: args.Title})
		if err != nil {
			return err
		}
		b.mu.Lock()
		w.selected = res.(taskReply).ID
		w.formRev++
		b.mu.Unlock()
		return nil
	case ev.NodeID == "edit":
		verb, value := parseAction(ev.Action)
		if verb != "save" {
			return fmt.Errorf("%w: %s %q", errUnknownAction, ev.NodeID, ev.Action)
		}
		id, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("save task id %q: %w", value, err)
		}
		var args commandArgs
		var form struct {
			TagsText *string `json:"tagsText"`
		}
		if err := json.Unmarshal(ev.Value, &args); err != nil {
			return fmt.Errorf("save value: %w", err)
		}
		if err := json.Unmarshal(ev.Value, &form); err != nil {
			return fmt.Errorf("save value: %w", err)
		}
		args.ID = id
		args.Note, args.Owner, args.Tags, args.keepClaim = nil, "", nil, true
		if form.TagsText != nil {
			tags := splitTags(*form.TagsText)
			args.Tags = &tags
		}
		_, err = b.updateCmd(ctx, user, args)
		b.mu.Lock()
		defer b.mu.Unlock()
		w.fieldError = fieldError{}
		switch {
		case errors.Is(err, errTitleEmpty) || errors.Is(err, errTitleLine) || errors.Is(err, errTitleLong):
			w.fieldError = fieldError{task: id, field: "title", text: caption(err)}
			return nil
		case errors.Is(err, errTagChars) || errors.Is(err, errTagLong) || errors.Is(err, errTooManyTags):
			w.fieldError = fieldError{task: id, field: tagsField, text: caption(err)}
			return nil
		}
		return err
	case ev.NodeID == searchNode && ev.Action == actSearch:
		var query string
		if err := json.Unmarshal(ev.Value, &query); err != nil {
			return fmt.Errorf("search value: %w", err)
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		w.query = strings.TrimSpace(query)
		return nil
	case ev.NodeID == tagsNode && ev.Action == actFilter:
		var item string
		if err := json.Unmarshal(ev.Value, &item); err != nil {
			return fmt.Errorf("filter value: %w", err)
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		switch item {
		case tagMore, tagLess:
			w.tagsOpen = item == tagMore
		case tagAll:
			w.tagFilter = ""
		default:
			w.tagFilter, _ = strings.CutPrefix(item, tagPrefix)
		}
		return nil
	case ev.Slot == mainSlot && ev.NodeID == attachField && ev.Action == actDrop:
		var paths []string
		if err := json.Unmarshal(ev.Value, &paths); err != nil {
			return fmt.Errorf("drop value: %w", err)
		}
		refs, err := dropRefs(b.root(ctx, ev.WorkspaceID), paths)
		return b.addRef(ctx, ev.WorkspaceID, w, attachField, refs, err)
	case ev.Slot == mainSlot && (ev.NodeID == linkField || ev.NodeID == attachField):
		var text string
		if err := json.Unmarshal(ev.Value, &text); err != nil {
			return fmt.Errorf("%s value: %w", ev.NodeID, err)
		}
		return b.addRef(ctx, ev.WorkspaceID, w, ev.NodeID, []string{text}, nil)
	case ev.Slot == mainSlot && (ev.NodeID == linksNode || ev.NodeID == attachNode || ev.Action == actOpenDoc):
		var item string
		if err := json.Unmarshal(ev.Value, &item); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		return b.refAction(ctx, ev.WorkspaceID, w, ev.NodeID, ev.Action, item)
	case ev.Slot == mainSlot && ev.NodeID == ownersNode && ev.Action == actOpenMove:
		var ref string
		if err := json.Unmarshal(ev.Value, &ref); err != nil {
			return fmt.Errorf("%s value: %w", ev.Action, err)
		}
		if run, ok := strings.CutPrefix(ref, runRef); ok {
			return b.h.RunCommand(ctx, "graphs.reveal", ev.WorkspaceID, map[string]string{"id": run}, nil)
		}
		return b.openOwner(ctx, ev.WorkspaceID, ref)
	case ev.Slot == mainSlot && ev.Action == actStart:
		var opts startOptions
		if len(ev.Value) > 0 && string(ev.Value) != "null" {
			if err := json.Unmarshal(ev.Value, &opts); err != nil {
				return fmt.Errorf("start value: %w", err)
			}
		}
		if _, err := b.startAgent(ctx, ev.WorkspaceID, ev.Caller, b.selected(w), "", opts); err != nil {
			return err
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		w.prompt = 0
		return nil
	case ev.Slot == mainSlot && ev.Action == actEditPrompt:
		b.mu.Lock()
		defer b.mu.Unlock()
		w.prompt = w.selected
		return nil
	case ev.Slot == mainSlot && ev.Action == actDescribe:
		b.mu.Lock()
		defer b.mu.Unlock()
		if w.editing == w.selected {
			w.editing = 0
		} else {
			w.editing = w.selected
		}
		return nil
	case ev.Slot == mainSlot:
		return b.taskAction(ctx, ev.WorkspaceID, w, ev.Action, b.selected(w))
	case ev.NodeID != "tasks":
		return fmt.Errorf("%w: %s %q", errUnknownAction, ev.NodeID, ev.Action)
	}
	var item string
	if err := json.Unmarshal(ev.Value, &item); err != nil {
		return fmt.Errorf("%s value: %w", ev.Action, err)
	}
	if status, ok := strings.CutPrefix(item, groupPrefix); ok {
		b.mu.Lock()
		if w.query == "" && (ev.Action == "expand" || ev.Action == "collapse") {
			w.collapsed[status] = ev.Action == "collapse"
		}
		b.mu.Unlock()
		return nil
	}
	id, err := strconv.Atoi(item)
	if err != nil {
		return fmt.Errorf("task id %q: %w", item, err)
	}
	if ev.Action == "select" {
		b.mu.Lock()
		w.selected = id
		b.mu.Unlock()
		return nil
	}
	return b.taskAction(ctx, ev.WorkspaceID, w, ev.Action, id)
}

var taskVerbs = []string{actStart, actTakeBack, "release", "done", "status", "priority", "delete", actDrop, actUnlink, actDetach}

func (b *backlog) fromSession(ctx context.Context, workspaceID, caller string) bool {
	if caller == "" {
		return false
	}
	return b.live.maybeSession(ctx, workspaceID, caller)
}

func changesTasks(ev pluginsdk.UIEvent, verb string) bool {
	switch ev.NodeID {
	case "new", "edit", linkField, attachField:
		return true
	}
	return slices.Contains(taskVerbs, verb)
}

func caption(err error) string {
	text := err.Error()
	return strings.ToUpper(text[:1]) + text[1:]
}

func (b *backlog) selected(w *workspaceUI) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return w.selected
}

func (b *backlog) taskAction(ctx context.Context, workspaceID string, w *workspaceUI, action string, id int) error {
	user := pluginsdk.CommandRun{WorkspaceID: workspaceID}
	args := commandArgs{ID: id}
	var err error
	verb, value := parseAction(action)
	switch verb {
	case actStart:
		_, err = b.startAgent(ctx, workspaceID, "", id, "", startOptions{})
	case actTakeBack:
		_, err = b.takeBack(ctx, workspaceID, id, "")
	case "release":
		_, err = b.releaseCmd(ctx, user, args)
	case "done":
		_, err = b.doneCmd(ctx, user, args)
	case "status":
		args.Status = &value
		_, err = b.updateCmd(ctx, user, args)
	case "priority":
		args.Priority = &value
		_, err = b.updateCmd(ctx, user, args)
	case actReveal, actRevealLast:
		var t Task
		if t, err = b.store.get(ctx, workspaceID, id); err != nil {
			return err
		}
		target := t.ClaimedBy
		if verb == actRevealLast {
			target = t.LastClaimedBy
		} else if t.Status != statusInProgress {
			target = ""
		}
		err = b.openOwner(ctx, workspaceID, target)
	case "delete":
		if err = b.store.remove(ctx, workspaceID, id); err != nil {
			return err
		}
		b.mu.Lock()
		if w.selected == id {
			w.selected = 0
		}
		b.mu.Unlock()
		b.changed(ctx, workspaceID, id, kindDeleted)
	default:
		err = fmt.Errorf("%w: %q", errUnknownAction, action)
	}
	return err
}

func (b *backlog) addRef(ctx context.Context, workspaceID string, w *workspaceUI, field string, refs []string, err error) error {
	id := b.selected(w)
	t, gerr := b.store.get(ctx, workspaceID, id)
	if gerr != nil {
		return gerr
	}
	args := commandArgs{ID: id, keepClaim: true}
	if field == linkField {
		l, perr := parseLink(refs[0])
		err = perr
		links := append(slices.Clone(t.Links), l)
		args.Links = &links
	} else {
		attachments := slices.Clone(t.Attachments)
		for _, ref := range refs {
			attachments = append(attachments, parseAttachment(ref))
		}
		args.Attachments = &attachments
	}
	if err == nil {
		_, err = b.updateCmd(ctx, pluginsdk.CommandRun{WorkspaceID: workspaceID}, args)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	w.fieldError = fieldError{}
	if err != nil {
		w.fieldError = fieldError{task: id, field: field, text: caption(err)}
		return nil
	}
	w.fieldRev++
	return nil
}

func (b *backlog) refAction(ctx context.Context, workspaceID string, w *workspaceUI, node, action, item string) error {
	id := b.selected(w)
	if action == actOpenDoc {
		n, ok := strings.CutPrefix(item, docrender.TaskPath)
		if !ok {
			return fmt.Errorf("%w: %s %q", errUnknownAction, action, item)
		}
		item, action, node = n, "select", linksNode
	}
	n, err := strconv.Atoi(item)
	if err != nil {
		return fmt.Errorf("%s item %q: %w", node, item, err)
	}
	switch {
	case action == "select" && node == linksNode:
		if _, err := b.store.get(ctx, workspaceID, n); err != nil {
			return err
		}
		b.mu.Lock()
		w.selected = n
		b.mu.Unlock()
		return nil
	case action == "select":
		t, err := b.store.get(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if n < 0 || n >= len(t.Attachments) {
			return fmt.Errorf("task #%d has no attachment %d", id, n)
		}
		return b.openAttachment(ctx, workspaceID, t.Attachments[n])
	case action == actUnlink:
		t, err := b.store.get(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		links := slices.DeleteFunc(slices.Clone(t.Links), func(l Link) bool { return l.Task == n })
		_, err = b.updateCmd(ctx, pluginsdk.CommandRun{WorkspaceID: workspaceID}, commandArgs{ID: id, Links: &links, keepClaim: true})
		return err
	case action == actDetach:
		t, err := b.store.get(ctx, workspaceID, id)
		if err != nil {
			return err
		}
		if n < 0 || n >= len(t.Attachments) {
			return fmt.Errorf("task #%d has no attachment %d", id, n)
		}
		attachments := slices.Delete(slices.Clone(t.Attachments), n, n+1)
		_, err = b.updateCmd(ctx, pluginsdk.CommandRun{WorkspaceID: workspaceID}, commandArgs{ID: id, Attachments: &attachments, keepClaim: true})
		return err
	}
	return fmt.Errorf("%w: %s %q", errUnknownAction, node, action)
}

func (b *backlog) openOwner(ctx context.Context, workspaceID, owner string) error {
	if !strings.HasPrefix(owner, sessionPfx) {
		return fmt.Errorf("%q is not an agent session", owner)
	}
	if s, ok := b.live.session(ctx, workspaceID, owner); ok && s.Run != nil {
		return b.h.RunCommand(ctx, "graphs.reveal", workspaceID, map[string]string{"id": s.Run.ID}, nil)
	}
	return agentcall.Reveal(ctx, b.h, workspaceID, owner)
}
