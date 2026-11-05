package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/pkg/pluginsdk"
)

const (
	entryOpenItems = "openItems"
	openItemsTool  = "mcp__" + mcpName + "__agents_open_items"

	itemDecision = "decision"
	itemOffer    = "offer"

	itemOpen     = "open"
	itemAnswered = "answered"
	itemNo       = "no"
	itemBacklog  = "backlog"
	itemHanded   = "handed"

	maxOpenDecisions = 3
	maxOpenOffers    = 4
	maxHeaderRunes   = 12
	minOptions       = 2
	maxOptions       = 4

	handedFormat = "[Clubmate] Child %q (%s) left open items:\n%s\nThey never reach the user. Answer the child with agents_prompt, or carry them into your own agents_open_items."
	convertText  = "[Clubmate] Your last message may have left a decision or an offer in prose. Post what is still open with agents_open_items, each with the sentence it quotes from that message."
)

var (
	errOpenOwn      = errors.New("an agent posts open items for its own session only")
	errNoAgentOwner = errors.New("open items need an agent session of Clubmate")
	errNoItems      = errors.New("items is empty")
	errNotOpen      = errors.New("not an open item of this session")
	errNotDecision  = errors.New("not a decision of this session")
	errNotOffer     = errors.New("not an offer of this session")
	errNoAnswer     = errors.New("pick an option or pass other")
	errOneOption    = errors.New("a decision without multiSelect takes one option")
	errOpenEmpty    = errors.New("nothing to send")
)

type openItem struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Header      string       `json:"header,omitempty"`
	Question    string       `json:"question,omitempty"`
	Label       string       `json:"label,omitempty"`
	Description string       `json:"description,omitempty"`
	Quote       string       `json:"quote"`
	Options     []openOption `json:"options,omitempty"`
	MultiSelect bool         `json:"multiSelect,omitempty"`
	State       string       `json:"state"`
	Answer      string       `json:"answer,omitempty"`
	Task        int          `json:"task,omitempty"`
	Number      int          `json:"number,omitempty"`
	At          time.Time    `json:"at,omitzero"`
	Changes     []string     `json:"changes,omitempty"`

	told bool
}

type openOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

type openItemsArgs struct {
	Session string      `json:"session"`
	Items   []itemInput `json:"items"`
}

type itemInput struct {
	Kind         string   `json:"kind"`
	Header       string   `json:"header"`
	Question     string   `json:"question"`
	Label        string   `json:"label"`
	Description  string   `json:"description"`
	Quote        string   `json:"quote"`
	MultiSelect  bool     `json:"multiSelect"`
	Options      []string `json:"options"`
	Descriptions []string `json:"descriptions"`
}

type openItemsResult struct {
	Items    []string `json:"items,omitempty"`
	Overflow []int    `json:"overflow,omitempty"`
	Handed   bool     `json:"handed,omitempty"`
}

type openAnswerArgs struct {
	Session string       `json:"session"`
	Answers []openAnswer `json:"answers"`
	Offers  []string     `json:"offers"`
	Note    string       `json:"note"`
}

type openAnswer struct {
	Item    string   `json:"item"`
	Option  string   `json:"option"`
	Options []string `json:"options"`
	Other   string   `json:"other"`
}

type openAnswerResult struct {
	Session string `json:"session,omitempty"`
}

type openPickArgs struct {
	Session string   `json:"session"`
	Items   []string `json:"items"`
}

type openBacklogResult struct {
	Tasks []int `json:"tasks,omitempty"`
}

type openQueueArgs struct {
	Workspace string `json:"workspace"`
}

type openQueueEntry struct {
	Session   string     `json:"session"`
	Title     string     `json:"title"`
	Worktree  string     `json:"worktree"`
	UpdatedAt time.Time  `json:"updatedAt"`
	Items     []openItem `json:"items"`
	since     time.Time
}

func (a *app) openItemsCommand(ctx context.Context, cmd pluginsdk.CommandRun, args openItemsArgs) (any, error) {
	s, err := a.openTarget(cmd, args.Session)
	if err != nil {
		return nil, err
	}
	items, err := parseItems(args.Items)
	if err != nil {
		return nil, err
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	rec := s.snapshot()
	if rec.closed() {
		return nil, errClosed
	}
	if parent := a.openParent(rec); parent != "" {
		if a.prompt(rec.Workspace, promptArgs{Session: parent, Text: handedText(rec, items)}) == nil {
			return openItemsResult{Items: s.postOpen(items, nil, itemHanded), Handed: true}, nil
		}
	}
	kept, extra := s.fitOpen(items)
	overflow := make([]int, 0, len(extra))
	for _, item := range extra {
		id, err := a.fileItem(ctx, rec, item)
		if err != nil {
			return nil, fmt.Errorf("file open item %q in the backlog: %w", item.title(), err)
		}
		overflow = append(overflow, id)
	}
	ids := s.postOpen(kept, overflow, itemOpen)
	a.notes.persist(ctx, s)
	return openItemsResult{Items: ids, Overflow: overflow}, nil
}

func (a *app) openAnswerCommand(ctx context.Context, cmd pluginsdk.CommandRun, args openAnswerArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	return a.answerOpen(ctx, s, cmd.WorkspaceID, args, nil)
}

func (a *app) answerOpen(ctx context.Context, s *session, workspaceID string, args openAnswerArgs, attachments []attachment) (any, error) {
	if len(args.Answers) == 0 && len(args.Offers) == 0 && strings.TrimSpace(args.Note) == "" {
		return nil, errOpenEmpty
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	picks, err := s.answerPicks(args)
	if err != nil {
		return nil, err
	}
	if s.snapshot().Night != "" && len(picks.offers) > 0 {
		return nil, a.answerNight(ctx, s, picks)
	}
	if s.snapshot().closed() {
		return a.answerClosed(ctx, s, picks, args.Note)
	}
	if err := a.prompt(workspaceID, promptArgs{Session: args.Session, Text: picks.prompt(args.Note), Attachments: attachments, Queued: true}); err != nil {
		return nil, err
	}
	s.resolveOpen(openChange{Resolved: picks.resolved()})
	return nil, nil
}

func (a *app) openLaterCommand(_ context.Context, cmd pluginsdk.CommandRun, args sessionArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.resolveOpen(openChange{Later: true})
	return nil, nil
}

func (a *app) openBacklogCommand(ctx context.Context, cmd pluginsdk.CommandRun, args openPickArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	items, err := s.openPicked(args.Items)
	if err != nil {
		return nil, err
	}
	tasks, err := a.fileItems(ctx, s, items)
	return openBacklogResult{Tasks: tasks}, err
}

func (a *app) openNoCommand(_ context.Context, cmd pluginsdk.CommandRun, args openPickArgs) (any, error) {
	s, err := a.lookup(cmd.WorkspaceID, args.Session)
	if err != nil {
		return nil, err
	}
	s.openMu.Lock()
	defer s.openMu.Unlock()
	items, err := s.openPicked(args.Items)
	if err != nil {
		return nil, err
	}
	resolved := make([]openResolution, 0, len(items))
	for _, item := range items {
		resolved = append(resolved, openResolution{Item: item.ID, State: itemNo})
	}
	s.resolveOpen(openChange{Resolved: resolved})
	return nil, nil
}

func (a *app) openQueueCommand(_ context.Context, cmd pluginsdk.CommandRun, args openQueueArgs) (any, error) {
	return a.openQueue(cmp.Or(args.Workspace, cmd.WorkspaceID)), nil
}

func (a *app) openQueue(workspaceID string) []openQueueEntry {
	out := []openQueueEntry{}
	for _, rec := range a.list(workspaceID) {
		s := a.session(rec.ID)
		if s == nil || rec.OpenCount == 0 {
			continue
		}
		out = append(out, openQueueEntry{Session: rec.ID, Title: rec.Title, Worktree: rec.Worktree, UpdatedAt: rec.UpdatedAt, Items: s.numberedOpen(), since: rec.OpenSince})
	}
	slices.SortStableFunc(out, func(x, y openQueueEntry) int {
		return cmp.Or(x.since.Compare(y.since), cmp.Compare(x.Session, y.Session))
	})
	return out
}

func (a *app) openConvertCommand(_ context.Context, cmd pluginsdk.CommandRun, args sessionArgs) (any, error) {
	return nil, a.prompt(cmd.WorkspaceID, promptArgs{Session: args.Session, Text: convertText, Queued: true})
}

func (a *app) openTarget(cmd pluginsdk.CommandRun, session string) (*session, error) {
	switch {
	case !cmd.Agent:
		return a.lookup(cmd.WorkspaceID, session)
	case cmd.Caller == "":
		return nil, errNoAgentOwner
	case session != "" && session != cmd.Caller:
		return nil, errOpenOwn
	}
	return a.lookup(cmd.WorkspaceID, cmd.Caller)
}

func (a *app) openParent(rec record) string {
	if rec.ParentSession == "" || rec.Run != nil {
		return ""
	}
	if parent := a.session(rec.ParentSession); parent != nil && !parent.snapshot().closed() {
		return rec.ParentSession
	}
	return ""
}

func (a *app) answerNight(ctx context.Context, s *session, picks openPicks) error {
	err := a.undoOffers(ctx, picks.offerItems())
	a.refreshSettings(ctx)
	if err != nil {
		return err
	}
	s.resolveOpen(openChange{Resolved: picks.offerResolved()})
	return nil
}

func (a *app) answerClosed(ctx context.Context, s *session, picks openPicks, note string) (any, error) {
	var out openAnswerResult
	if len(picks.offers) > 0 {
		rec := s.snapshot()
		root := rec.Worktree
		if _, err := os.Stat(root); err != nil {
			root = ""
		}
		res, err := a.newSession(ctx, a.host, rec.Workspace, "", newArgs{Prompt: picks.doText(note), worktree: root})
		if err != nil {
			return nil, err
		}
		a.kick(rec.Workspace)
		s.resolveOpen(openChange{Resolved: picks.offerResolved()})
		out.Session = res.Session
	}
	_, err := a.fileItems(ctx, s, picks.decisionItems())
	return out, err
}

func parseItems(inputs []itemInput) ([]openItem, error) {
	if len(inputs) == 0 {
		return nil, errNoItems
	}
	items := make([]openItem, 0, len(inputs))
	for i, in := range inputs {
		item, err := in.item()
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (in itemInput) item() (openItem, error) {
	quote := strings.TrimSpace(in.Quote)
	if quote == "" {
		return openItem{}, errors.New("quote is required: the sentence of your report this item comes from")
	}
	switch in.Kind {
	case itemOffer:
		label := strings.TrimSpace(in.Label)
		if label == "" {
			return openItem{}, errors.New("an offer needs a label: what you would do")
		}
		return openItem{Kind: itemOffer, Label: label, Description: strings.TrimSpace(in.Description), Quote: quote}, nil
	case itemDecision:
		return in.decision(quote)
	}
	return openItem{}, fmt.Errorf("kind must be decision or offer, not %q", in.Kind)
}

func (in itemInput) decision(quote string) (openItem, error) {
	header, question := strings.TrimSpace(in.Header), strings.TrimSpace(in.Question)
	switch {
	case header == "":
		return openItem{}, errors.New("a decision needs a header of up to 12 characters")
	case utf8.RuneCountInString(header) > maxHeaderRunes:
		return openItem{}, fmt.Errorf("header %q is longer than 12 characters", header)
	case question == "":
		return openItem{}, errors.New("a decision needs a question")
	case len(in.Options) < minOptions || len(in.Options) > maxOptions:
		return openItem{}, fmt.Errorf("a decision takes 2 to 4 options, not %d; Other is always offered, don't list it", len(in.Options))
	case len(in.Descriptions) != len(in.Options):
		return openItem{}, fmt.Errorf("every option needs a description: %d options, %d descriptions", len(in.Options), len(in.Descriptions))
	}
	options := make([]openOption, 0, len(in.Options))
	for i, label := range in.Options {
		o := openOption{Label: strings.TrimSpace(label), Description: strings.TrimSpace(in.Descriptions[i])}
		if o.Label == "" || o.Description == "" {
			return openItem{}, fmt.Errorf("option %d needs a label and a one-line description", i+1)
		}
		options = append(options, o)
	}
	return openItem{Kind: itemDecision, Header: header, Question: question, Quote: quote, Options: options, MultiSelect: in.MultiSelect}, nil
}

func handedText(rec record, items []openItem) string {
	lines := make([]string, 0, len(items))
	for i, item := range items {
		lines = append(lines, strconv.Itoa(i+1)+". "+item.handedLine())
	}
	return fmt.Sprintf(handedFormat, firstLine(rec.Title), rec.ID, strings.Join(lines, "\n"))
}

func (it openItem) handedLine() string {
	if it.Kind == itemOffer {
		return strings.Join(nonEmpty("Offer: "+it.Label, it.Description), " — ") + " (from: " + it.Quote + ")"
	}
	options := make([]string, 0, len(it.Options))
	for _, o := range it.Options {
		options = append(options, o.Label+" ("+o.Description+")")
	}
	return it.Header + ": " + it.Question + " Options: " + strings.Join(options, "; ") + ". (from: " + it.Quote + ")"
}

func (it openItem) title() string {
	if it.Kind == itemOffer {
		return it.Label
	}
	return it.Header
}
