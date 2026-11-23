package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
)

const (
	chatKey         = "chat/"
	codeChatPrefix  = "code:"
	wikiChatContext = "wiki"

	chatPickerNode   = "chat/picker"
	chatSessionsNode = "chat/sessions"
	chatNewNode      = "chat/new"
	chatBackNode     = "chat/back"
	chatHideNode     = "chat/hide"
	panelNode        = "panel"
	ctxChipPrefix    = "ctx/"
	suggestedRow     = "chat/section/suggested"
	openInRow        = "chat/section/open"

	actChatPick = "chatPick"
	actChatNew  = "chatNew"
	actChatBack = "chatBack"
	actChatOpen = "open"

	currentReviewTopic = "editor-nvim.review.current"
	wikiPageTopic      = "wiki.page"
	reviewOpenCmd      = "editor-nvim.review"

	reasonOpener    = "opened this review"
	reasonRecipient = "gets this review's comments"
	reasonLast      = "last used here"
	reasonNewest    = "newest"

	suggestedLabel  = "Suggested"
	openInLabel     = "Open in %s"
	newSessionLabel = "New session"
	newReviewLabel  = "New session for this review"
	backLabel       = "All sessions"
	backKey         = "cmd+["
	newKey          = "n"
	noOpenTitle     = "No open sessions in %s"
	closedStay      = "Closed sessions stay in Agents"
	pickerTitle     = "Chat"
	pickerSub       = "· pick a session"
	hideLabel       = "Hide chat"
	hideIcon        = "sidebar.right"
	hideKey         = "cmd+b"
	pickerGap       = 8
	filterHint      = "Filter sessions"
	filterMinRows   = 10
	pickerHints     = "↩ open · ↑↓ j k move · N new · / filter"
	emptyHints      = "↩ new session"
	needsYouBadge   = "needs you"
	reviewLabel     = "Review %s"
	pageLabel       = "%s page"
	reviewNote      = "The user has the review %s → %s open in Neovim (worktree %s)."
	pageNote        = "The user is reading the wiki page %s (%s) in Clubmate's Wiki."
)

var (
	reviewNoteRe = noteRe(reviewNote)
	pageNoteRe   = noteRe(pageNote)
)

func noteRe(format string) *regexp.Regexp {
	return regexp.MustCompile("^" + strings.ReplaceAll(regexp.QuoteMeta(format), "%s", "(.+)") + "$")
}

func contextAttachment(text string) (sdui.Attachment, bool) {
	if m := reviewNoteRe.FindStringSubmatch(text); m != nil {
		return sdui.Attachment{Kind: "review", Path: fmt.Sprintf(reviewLabel, shortRev(m[2]))}, true
	}
	if m := pageNoteRe.FindStringSubmatch(text); m != nil {
		return sdui.Attachment{Kind: "page", Path: fmt.Sprintf(pageLabel, m[1])}, true
	}
	return sdui.Attachment{}, false
}

type chatContext struct {
	Slot string
	Key  string
	Root string
}

type chatEntry struct {
	Session string `json:"session,omitempty"`
	Picking bool   `json:"picking,omitempty"`
}

type reviewRef struct {
	ID        string
	Worktree  string
	Base      string
	Head      string
	Opener    string
	Recipient string
}

type currentReview struct {
	WorkspaceID string `json:"workspaceId"`
	Worktree    string `json:"worktree"`
	ID          string `json:"id"`
}

type wikiPage struct {
	Workspace string `json:"workspace"`
	Page      string `json:"page"`
	Title     string `json:"title"`
}

type contextChip struct {
	chip sdui.Attachment
	att  attachment
}

type chatPicker struct {
	Workspace string
	Root      string
	Recs      []record
	Children  map[string]string
	Suggested string
	Reason    string
	NewLabel  string
	NewNote   string
	Now       time.Time
}

func (cc chatContext) wiki() bool {
	return cc.Slot == wikiChatSlot
}

func (cc chatContext) section() string {
	if cc.wiki() {
		return wikiSection
	}
	return editorSection
}

func chatContextOf(slot string, workspace pluginsdk.Workspace) (chatContext, error) {
	root, _, err := workRoot(workspace)
	if slot == wikiChatSlot {
		return chatContext{Slot: slot, Key: wikiChatContext, Root: root}, nil
	}
	if err != nil {
		return chatContext{}, err
	}
	return chatContext{Slot: codeSlot, Key: codeChatPrefix + root, Root: root}, nil
}

func chatSlot(slot string) bool {
	return slot == codeSlot || slot == wikiChatSlot
}

func sameTree(a, b string) bool {
	return a == b || gitrepo.Canonical(a) == gitrepo.Canonical(b)
}

func openRecord(recs []record, sid string) (record, bool) {
	i := slices.IndexFunc(recs, func(r record) bool { return r.ID == sid && !r.closed() })
	if sid == "" || i < 0 {
		return record{}, false
	}
	return recs[i], true
}

func suggestChat(recs []record, cc chatContext, st chatEntry, rev *reviewRef) (string, string) {
	if rev != nil && !cc.wiki() {
		if _, ok := openRecord(recs, rev.Opener); ok {
			return rev.Opener, reasonOpener
		}
		if _, ok := openRecord(recs, rev.Recipient); ok {
			return rev.Recipient, reasonRecipient
		}
	}
	if _, ok := openRecord(recs, st.Session); ok {
		return st.Session, reasonLast
	}
	var newest *record
	for i, rec := range recs {
		if !rec.closed() && !serviceSession(rec) && (newest == nil || rec.UpdatedAt.After(newest.UpdatedAt)) {
			newest = &recs[i]
		}
	}
	if newest == nil {
		return "", ""
	}
	return newest.ID, reasonNewest
}

func pickerView(pk chatPicker) sdui.Node {
	open := slices.DeleteFunc(slices.Clone(pk.Recs), func(r record) bool { return r.closed() || serviceSession(r) && r.ID != pk.Suggested })
	newButton := func(variant string) sdui.Node {
		return sdui.WithKeyCap(sdui.WithHint(sdui.WithVariant(sdui.WithAction(sdui.Button(chatNewNode, pk.NewLabel, "plus"), actChatNew), variant), newKey))
	}
	if len(open) == 0 {
		add := newButton(sdui.VariantPrimary)
		text := strings.TrimSuffix(pk.NewNote, ".") + "."
		empty := sdui.EmptyState("A new session "+lowerFirst(text), sdui.EmptyProps{Title: fmt.Sprintf(noOpenTitle, pk.Workspace), Icon: "bubble.left.and.bubble.right", Note: closedStay, NoteSection: sectionID, Action: &add})
		return sdui.WithHints(sdui.WithAction(sdui.Keys(chatPickerNode, []string{newKey}, sdui.VStack(pickerHeader(), empty)), actChatNew), emptyHints)
	}
	slices.SortStableFunc(open, func(x, y record) int { return y.UpdatedAt.Compare(x.UpdatedAt) })
	var items []sdui.Item
	if i := slices.IndexFunc(open, func(r record) bool { return r.ID == pk.Suggested }); i >= 0 {
		items = append(items, sdui.Item{ID: suggestedRow, Label: suggestedLabel, Section: true}, pk.item(open[i], pk.Reason))
		open = slices.Delete(open, i, i+1)
	}
	if len(open) > 0 {
		items = append(items, sdui.Item{ID: openInRow, Label: fmt.Sprintf(openInLabel, pk.Workspace), Detail: strconv.Itoa(len(open)), Section: true})
		for _, rec := range open {
			items = append(items, pk.item(rec, pk.worktreeOf(rec)))
		}
	}
	list := sdui.WithAction(sdui.List(chatSessionsNode, items, ""), actChatPick).WithFocus(pk.Suggested).WithFilter(filterHint, filterMinRows)
	foot := sdui.WithInset(sdui.WithGap(sdui.VStack(sdui.WithFill(newButton(sdui.VariantSecondary)), sdui.MutedText(pk.NewNote)), 6), sdui.InsetFoot)
	return sdui.WithHints(sdui.WithAction(sdui.Keys(chatPickerNode, []string{newKey}, sdui.VStack(pickerHeader(), list, sdui.Divider(), foot)), actChatNew), pickerHints)
}

func pickerHeader() sdui.Node {
	return sdui.WithNotes(chatBar(openStrong(pickerTitle), sdui.WithLines(sdui.MutedText(pickerSub), 1), sdui.Spacer(), hideButton()))
}

func chatBar(children ...sdui.Node) sdui.Node {
	return sdui.WithInset(sdui.WithGap(sdui.HStack(children...), pickerGap), sdui.InsetBar)
}

func hideButton() sdui.Node {
	return sdui.WithClientAction(sdui.WithHint(sdui.WithIconOnly(sdui.WithVariant(sdui.Button(chatHideNode, hideLabel, hideIcon), sdui.VariantGhost)), hideKey), sdui.ClientHideChat)
}

func serviceSession(rec record) bool {
	return rec.restricted() || rec.Run != nil || rec.GateFor != nil
}

func (pk chatPicker) item(rec record, lead string) sdui.Item {
	display := displayStatus(rec, pk.Children[rec.ID])
	it := sdui.Item{
		ID:      rec.ID,
		Label:   cmp.Or(rec.Title, newSessionLabel),
		Icon:    cmp.Or(statusIcons[display], statusIcons[statusIdle]),
		Tone:    statusTones[display],
		Caption: strings.Join(nonEmpty(lead, shortAge(pk.Now.Sub(rec.UpdatedAt), rec.UpdatedAt)), " · "),
	}
	if needsYou(rec) {
		it.Icon, it.Tone, it.Badge = statusIcons[statusWaiting], statusTones[statusWaiting], needsYouBadge
	}
	return it
}

func (pk chatPicker) worktreeOf(rec record) string {
	if rec.Worktree == "" || sameTree(rec.Worktree, pk.Root) {
		return ""
	}
	return filepath.Base(rec.Worktree)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func reviewChipLabel(rev reviewRef, branch string) string {
	head := rev.Head
	if head == "" || commitID(head) {
		head = cmp.Or(branch, shortRev(head))
	}
	return fmt.Sprintf(reviewLabel, head)
}

func commitID(rev string) bool {
	return len(rev) == 40 && strings.Trim(rev, "0123456789abcdef") == ""
}

func shortRev(rev string) string {
	if commitID(rev) {
		return rev[:7]
	}
	return rev
}

func newSessionNote(branch, file string, rev *reviewRef, page *wikiPage) string {
	starts := "Starts on " + cmp.Or(branch, "this worktree")
	switch {
	case rev != nil:
		return starts + " with this review"
	case page != nil:
		return starts + " and sees the page " + cmp.Or(page.Title, page.Page)
	case file != "":
		return starts + " and sees " + filepath.Base(file)
	}
	return starts
}

func (a *app) chatState(ctx context.Context, workspaceID, key string) (chatEntry, error) {
	all, err := a.chatStates(ctx, workspaceID)
	return all[key], err
}

func (a *app) chatStates(ctx context.Context, workspaceID string) (map[string]chatEntry, error) {
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	read, chat := p.chatRead, maps.Clone(p.chat)
	a.uiMu.Unlock()
	if read {
		return chat, nil
	}
	found, err := a.host.StorageGet(ctx, chatKey+workspaceID, &chat)
	if err != nil {
		return nil, err
	}
	if !found {
		if chat, err = a.legacyChat(ctx, workspaceID); err != nil {
			return nil, err
		}
	}
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if !p.chatRead {
		p.chat, p.chatRead = chat, true
	}
	return maps.Clone(p.chat), nil
}

func (a *app) legacyChat(ctx context.Context, workspaceID string) (map[string]chatEntry, error) {
	var old map[string]string
	found, err := a.host.StorageGet(ctx, codeKey+workspaceID, &old)
	if err != nil || !found {
		return nil, err
	}
	chat := make(map[string]chatEntry, len(old))
	for root, sid := range old {
		key := codeChatPrefix + root
		if root == wikiChatContext {
			key = wikiChatContext
		}
		chat[key] = chatEntry{Session: sid}
	}
	return chat, a.host.StorageSet(ctx, chatKey+workspaceID, chat)
}

func (a *app) setChatState(ctx context.Context, workspaceID, key string, change func(*chatEntry)) error {
	chat, err := a.chatStates(ctx, workspaceID)
	if err != nil {
		return err
	}
	if chat == nil {
		chat = map[string]chatEntry{}
	}
	e := chat[key]
	change(&e)
	if chat[key] == e {
		return nil
	}
	chat[key] = e
	p := a.workspaceState(workspaceID)
	a.uiMu.Lock()
	p.chat = maps.Clone(chat)
	a.uiMu.Unlock()
	return a.host.StorageSet(ctx, chatKey+workspaceID, chat)
}

func (a *app) chatContextFor(ctx context.Context, workspaceID, slot string) (chatContext, error) {
	workspace, err := a.host.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return chatContext{}, err
	}
	return chatContextOf(slot, workspace)
}

func (a *app) bindChat(ctx context.Context, workspaceID, slot, sid string) error {
	cc, err := a.chatContextFor(ctx, workspaceID, slot)
	if err != nil {
		return err
	}
	return a.setChatState(ctx, workspaceID, cc.Key, func(e *chatEntry) { e.Session, e.Picking = sid, false })
}

func (a *app) chatAct(ctx context.Context, ev pluginsdk.UIEvent) error {
	cc, err := a.chatContextFor(ctx, ev.WorkspaceID, ev.Slot)
	if err != nil {
		return err
	}
	switch ev.Action {
	case actChatOpen, actChatBack:
		return a.setChatState(ctx, ev.WorkspaceID, cc.Key, func(e *chatEntry) { e.Picking = true })
	case actChatPick:
		id, err := itemID(ev)
		if err != nil {
			return err
		}
		if _, err := a.lookup(ev.WorkspaceID, id); err != nil {
			return err
		}
		return a.setChatState(ctx, ev.WorkspaceID, cc.Key, func(e *chatEntry) { e.Session, e.Picking = id, false })
	}
	rev := a.panelReview(ev.WorkspaceID, cc)
	res, err := a.newSession(ctx, a.host, ev.WorkspaceID, "", newArgs{})
	if err != nil {
		return err
	}
	if err := a.setChatState(ctx, ev.WorkspaceID, cc.Key, func(e *chatEntry) { e.Session, e.Picking = res.Session, false }); err != nil {
		return err
	}
	if rev == nil {
		return nil
	}
	args := map[string]string{"base": rev.Base, "head": rev.Head, "cwd": rev.Worktree}
	if err := a.host.RunCommandFor(ctx, res.Session, reviewOpenCmd, ev.WorkspaceID, args, nil); err != nil {
		fmt.Fprintln(os.Stderr, "agents: open the review for the new session:", err)
	}
	return nil
}

func (a *app) chatFocus(ctx context.Context, ev pluginsdk.UIEvent) error {
	cc, err := a.chatContextFor(ctx, ev.WorkspaceID, ev.Slot)
	if err != nil {
		return err
	}
	st, err := a.chatState(ctx, ev.WorkspaceID, cc.Key)
	if err != nil {
		return err
	}
	if st.Picking || a.session(st.Session) == nil {
		return a.host.Focus(ctx, ev.WorkspaceID, cc.section(), cc.Slot)
	}
	return a.host.FocusNode(ctx, ev.WorkspaceID, cc.section(), cc.Slot, st.Session+"/composer")
}

func isChatAction(ev pluginsdk.UIEvent) bool {
	if !chatSlot(ev.Slot) {
		return false
	}
	switch ev.Action {
	case actChatPick, actChatNew, actChatBack:
		return true
	case actChatOpen:
		return ev.NodeID == panelNode
	}
	return false
}

func openFocuses(ev pluginsdk.UIEvent) bool {
	var v struct {
		Focus bool `json:"focus"`
	}
	return json.Unmarshal(ev.Value, &v) == nil && v.Focus
}

func (a *app) setCurrentReview(c currentReview) bool {
	tree := gitrepo.Canonical(c.Worktree)
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	byTree, ok := a.currentReviews[c.WorkspaceID]
	if a.goneWorkspaces[c.WorkspaceID] || ok && byTree[tree] == c.ID {
		return false
	}
	if !ok {
		byTree = map[string]string{}
		a.currentReviews[c.WorkspaceID] = byTree
	}
	if c.ID == "" {
		delete(byTree, tree)
		return true
	}
	byTree[tree] = c.ID
	return true
}

func (a *app) setWikiPage(p wikiPage) bool {
	a.uiMu.Lock()
	defer a.uiMu.Unlock()
	if a.goneWorkspaces[p.Workspace] || a.wikiPages[p.Workspace] == p {
		return false
	}
	a.wikiPages[p.Workspace] = p
	return true
}

func (a *app) panelReview(workspaceID string, cc chatContext) *reviewRef {
	if cc.wiki() {
		return nil
	}
	a.uiMu.Lock()
	id := a.currentReviews[workspaceID][gitrepo.Canonical(cc.Root)]
	a.uiMu.Unlock()
	if id == "" {
		return nil
	}
	rev, ok := a.reviews.ref(id)
	if !ok {
		return nil
	}
	return &rev
}

func (a *app) panelPage(workspaceID string, cc chatContext) *wikiPage {
	if !cc.wiki() {
		return nil
	}
	a.uiMu.Lock()
	page, ok := a.wikiPages[workspaceID]
	a.uiMu.Unlock()
	if !ok || page.Page == "" {
		return nil
	}
	return &page
}

func (a *app) contextChips(workspaceID string, cc chatContext, rec record, branch string) []contextChip {
	if page := a.panelPage(workspaceID, cc); page != nil {
		label := fmt.Sprintf(pageLabel, cmp.Or(page.Title, page.Page))
		return []contextChip{{
			chip: sdui.Attachment{ID: ctxChipPrefix + "page", Kind: "page", Path: label, Context: true, Removable: true},
			att:  attachment{Kind: "page", Path: label, Text: fmt.Sprintf(pageNote, cmp.Or(page.Title, page.Page), page.Page)},
		}}
	}
	if cc.wiki() || !sameTree(rec.Worktree, cc.Root) {
		return nil
	}
	var chips []contextChip
	if file := a.editorFile(workspaceID, cc.Root); file != "" {
		chips = append(chips, contextChip{
			chip: sdui.Attachment{ID: ctxChipPrefix + "file", Kind: "file", Path: relPath(file, rec.Worktree), Context: true, Removable: true},
			att:  attachment{Kind: "file", Path: file},
		})
	}
	if rev := a.panelReview(workspaceID, cc); rev != nil {
		label := reviewChipLabel(*rev, branch)
		chips = append(chips, contextChip{
			chip: sdui.Attachment{ID: ctxChipPrefix + "review", Kind: "review", Path: label, Context: true, Removable: true},
			att:  attachment{Kind: "review", Path: label, Text: fmt.Sprintf(reviewNote, rev.Base, cmp.Or(rev.Head, branch), rev.Worktree)},
		})
	}
	return chips
}

func (c contextChip) content() string {
	return c.att.Path + "\n" + c.att.Text
}

func (a *app) shownChips(workspaceID string, cc chatContext, rec record, branch string) []contextChip {
	st := a.sessionState(workspaceID, rec.ID)
	a.uiMu.Lock()
	off := maps.Clone(st.contextOff)
	a.uiMu.Unlock()
	return slices.DeleteFunc(a.contextChips(workspaceID, cc, rec, branch), func(c contextChip) bool {
		content, ok := off[c.chip.ID]
		return ok && content == c.content()
	})
}

func (a *app) panelChips(ctx context.Context, ev pluginsdk.UIEvent, rec record, shown bool) []contextChip {
	if !chatSlot(ev.Slot) {
		return nil
	}
	cc, err := a.chatContextFor(ctx, ev.WorkspaceID, ev.Slot)
	if err != nil {
		return nil
	}
	if shown {
		return a.shownChips(ev.WorkspaceID, cc, rec, a.branchOf(ctx, rec))
	}
	return a.contextChips(ev.WorkspaceID, cc, rec, a.branchOf(ctx, rec))
}

func (a *app) panelAttachments(ctx context.Context, ev pluginsdk.UIEvent, rec record) []attachment {
	var atts []attachment
	for _, c := range a.panelChips(ctx, ev, rec, true) {
		atts = append(atts, c.att)
	}
	return atts
}

func (a *app) branchOf(ctx context.Context, rec record) string {
	if rec.Worktree == "" {
		return ""
	}
	return branchLabel(a.branches.name(ctx, rec.Workspace, rec.Worktree))
}

func (a *app) renderChat(ctx context.Context, p *workspaceUI, workspace pluginsdk.Workspace, all []record, children map[string]string, shown *screen, slot string) error {
	cc, err := chatContextOf(slot, workspace)
	if err != nil {
		return a.push(ctx, p, workspace.ID, slot, sdui.Empty(err.Error()))
	}
	st, err := a.chatState(ctx, workspace.ID, cc.Key)
	if err != nil {
		return err
	}
	recs := all
	if rec, ok := openRecord(recs, st.Session); ok && !st.Picking {
		if s := a.session(rec.ID); s != nil {
			return a.push(ctx, p, workspace.ID, slot, a.panelChat(ctx, workspace, cc, recs, s, children, shown))
		}
	}
	rev := a.panelReview(workspace.ID, cc)
	page := a.panelPage(workspace.ID, cc)
	sid, reason := suggestChat(recs, cc, st, rev)
	branch := branchLabel(a.branches.name(ctx, workspace.ID, cc.Root))
	pk := chatPicker{
		Workspace: workspace.Name, Root: cc.Root, Recs: recs, Children: children,
		Suggested: sid, Reason: reason, NewLabel: newSessionLabel, NewNote: newSessionNote(branch, a.panelFile(workspace.ID, cc), rev, page), Now: time.Now(),
	}
	if rev != nil {
		pk.NewLabel = newReviewLabel
	}
	return a.push(ctx, p, workspace.ID, slot, pickerView(pk))
}

func (a *app) panelChat(ctx context.Context, workspace pluginsdk.Workspace, cc chatContext, recs []record, s *session, children map[string]string, shown *screen) sdui.Node {
	rec := s.snapshot()
	sc := shown
	if sc == nil || sc.View.Session.ID != rec.ID {
		v := a.screen(ctx, workspace.ID, s, children[rec.ID])
		v.Workspace, v.Folders = workspace.Name, workspace
		sc = &v
	}
	branch := a.branchOf(ctx, rec)
	panel := codePanel{
		Back:     backButton(),
		Worktree: chatPicker{Root: cc.Root}.worktreeOf(rec),
		Chips:    a.shownChips(workspace.ID, cc, rec, branch),
	}
	if sameTree(rec.Worktree, cc.Root) {
		panel.File = a.panelFile(workspace.ID, cc)
	}
	return codeView(recs, *sc, panel)
}

func backButton() sdui.Node {
	return sdui.WithKeyCap(sdui.WithIconOnly(sdui.WithShortcut(sdui.WithVariant(sdui.WithAction(sdui.Button(chatBackNode, backLabel, "chevron.left"), actChatBack), sdui.VariantGhost), backKey)))
}

func (a *app) panelFile(workspaceID string, cc chatContext) string {
	if cc.wiki() {
		return ""
	}
	return a.editorFile(workspaceID, cc.Root)
}
