package main

import (
	"cmp"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	landQueueKey     = "landQueue/"
	queueEntryPrefix = "lq"
	queueStateGit    = "queued"
	queueStateRefuse = "refused"

	queueWaiting = "waiting"
	queueReady   = "ready"
	queueHead    = "head"
	queueAsks    = "asks"
	queueBlocked = "blocked"

	queueModeBranch = "branch"
	queueModeIndex  = "index"
	queueModeDirty  = "dirty"

	queueLoopEnv   = "CLUBMATE_AGENTS_QUEUE_LOOP"
	queueLoopEvery = 30 * time.Second
	queueStuckEnv  = "CLUBMATE_AGENTS_QUEUE_STUCK"
	landQueueStuck = 15 * time.Minute
	queueFilesMax  = 10
	queueDoneMax   = 8
	queueDoneAge   = 24 * time.Hour
	queueUser      = "the user"

	queuedFormat       = "queued: place %d of %d into %s; ahead: %s. End your turn; Clubmate prompts you when it is your turn."
	queueAheadFormat   = "\"%s\" (%s)"
	queueTurnFormat    = "[Clubmate landing queue] Your turn to land into %s. Its head is now %s. Call git_land again; rebase only if it reports a conflict."
	queueIndexFormat   = "another session's change is staged in %s; wait for it to land, then stage yours"
	queueDirtyFormat   = "[Clubmate landing queue] Your uncommitted edits in %s (%s) overlap the queued landing of \"%s\" (%s) into %s, which waits for them. Stage them and call git_land with mode index to land them first, or drop them."
	queueRemovedFormat = "[Clubmate landing queue] The user removed the landing of %s into %s from the queue. Don't call git_land for it again unless the user asks."
	queueGoneFormat    = "[Clubmate landing queue] The landing queue into %s was dropped because its target is gone; your landing is not queued any more."
	queueLeftFormat    = "queued: your entry is no longer in the landing queue into %s, so nothing was committed. If the user removed it, don't call git_land again unless the user asks; else call git_land again to take a place."
	queueStaged        = "staged"
	queueEditsIn       = "edits in %s"
	queueWhyHead       = "waits for uncommitted edits in %s"
	queueWhyDirty      = "uncommitted edits in %s"
	queueWhyStuck      = "silent for %s; no entry behind it is ready to pass it"
)

var (
	queueStuck = envDelay(queueStuckEnv, landQueueStuck)

	errQueueUnknown = errors.New("no such landing queue or entry")
	errQueueIndex   = errors.New("another session's change is staged")
	errQueuePlace   = errors.New("to is a place from 1 to the queue's length")
	errQueueOrder   = errors.New("staged (index) entries stay before branch entries")
	errQueueDirty   = errors.New("a dirty entry stays until its files are committed or dropped")
)

type landQueue struct {
	ID        string       `json:"id"`
	Common    string       `json:"common"`
	Into      string       `json:"into"`
	Repo      string       `json:"repo"`
	Target    string       `json:"target"`
	Workspace string       `json:"workspace"`
	Tip       string       `json:"tip,omitempty"`
	Entries   []queueEntry `json:"entries"`
	Done      []queueEntry `json:"done,omitempty"`
	Updated   time.Time    `json:"updated"`
}

type queueEntry struct {
	ID         string    `json:"id"`
	Session    string    `json:"session,omitempty"`
	Source     string    `json:"source,omitempty"`
	Worktree   string    `json:"worktree,omitempty"`
	Mode       string    `json:"mode"`
	State      string    `json:"state"`
	Why        string    `json:"why,omitempty"`
	Old        string    `json:"old,omitempty"`
	OldTree    string    `json:"oldTree,omitempty"`
	Tree       string    `json:"tree,omitempty"`
	Files      []string  `json:"files,omitempty"`
	Since      time.Time `json:"since"`
	PromptedAt time.Time `json:"promptedAt,omitzero"`
	CalledAt   time.Time `json:"calledAt,omitzero"`
	HeadAt     time.Time `json:"headAt,omitzero"`
	DoneAt     time.Time `json:"doneAt,omitzero"`
	Tip        string    `json:"tip,omitempty"`
	SourceHead string    `json:"sourceHead,omitempty"`
	Checked    string    `json:"checked,omitempty"`
	Clean      bool      `json:"clean,omitempty"`
	Turn       bool      `json:"turn,omitempty"`
	Passed     bool      `json:"passed,omitempty"`
	Told       bool      `json:"told,omitempty"`
	Skips      int       `json:"skips,omitempty"`
	UserYes    bool      `json:"userYes,omitempty"`
	Ask        *queueAsk `json:"ask,omitempty"`
	Ahead      bool      `json:"ahead,omitempty"`
	AheadOn    string    `json:"aheadOn,omitempty"`
	AheadBase  string    `json:"aheadBase,omitempty"`
	AheadHead  string    `json:"aheadHead,omitempty"`
	AheadOld   string    `json:"aheadOld,omitempty"`
	AheadTree  string    `json:"aheadTree,omitempty"`
}

type queueTurn struct {
	Head bool
	Text string
}

type queuePlace struct {
	Queue string `json:"queue"`
	Into  string `json:"into"`
	Place int    `json:"place"`
	Of    int    `json:"of"`
	State string `json:"state"`
	Why   string `json:"why,omitempty"`
}

type queueRefused struct {
	text string
}

func (r queueRefused) Error() string { return r.text }

func (r queueRefused) Unwrap() error { return errQueueIndex }

type queueTell struct {
	session string
	text    string
}

type landQueues struct {
	mu     sync.Mutex
	byID   map[string]*landQueue
	locks  map[string]*queueLock
	active map[string]time.Time
	ahead  map[string]bool
	shown  bool
	live   atomic.Bool
}

func (l *landQueues) markLocked() {
	live := false
	for _, q := range l.byID {
		live = live || len(q.Entries) > 0
	}
	l.live.Store(live)
}

type queueLock struct {
	sync.Mutex
	users int
}

func (l *landQueues) lock(qid string) *queueLock {
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]*queueLock{}
	}
	m, ok := l.locks[qid]
	if !ok {
		m = &queueLock{}
		l.locks[qid] = m
	}
	m.users++
	l.mu.Unlock()
	m.Lock()
	return m
}

func (l *landQueues) unlock(qid string, m *queueLock) {
	m.Unlock()
	l.mu.Lock()
	if m.users--; m.users == 0 {
		delete(l.locks, qid)
	}
	l.mu.Unlock()
}

type landQueueResult struct {
	Queues []queueRow `json:"queues"`
}

type queueRow struct {
	ID         string          `json:"id"`
	Repo       string          `json:"repo"`
	Into       string          `json:"into"`
	NeedsYou   bool            `json:"needsYou"`
	Entries    []queueEntryRow `json:"entries"`
	Title      string          `json:"title"`
	Glyph      string          `json:"glyph"`
	Detail     string          `json:"detail"`
	DetailTone string          `json:"detailTone,omitempty"`
	Grouped    bool            `json:"grouped"`
	Owner      string          `json:"owner,omitempty"`
	Help       string          `json:"help,omitempty"`
}

type queueEntryRow struct {
	ID         string           `json:"id"`
	Session    string           `json:"session,omitempty"`
	Title      string           `json:"title"`
	Source     string           `json:"source,omitempty"`
	Mode       string           `json:"mode"`
	State      string           `json:"state"`
	Place      int              `json:"place"`
	Files      []string         `json:"files"`
	More       int              `json:"more,omitempty"`
	Overlaps   []string         `json:"overlaps"`
	Why        string           `json:"why,omitempty"`
	Skips      int              `json:"skips,omitempty"`
	NeedsYou   bool             `json:"needsYou"`
	Word       string           `json:"word"`
	Glyph      string           `json:"glyph"`
	Tone       string           `json:"tone,omitempty"`
	Line       string           `json:"line"`
	Marks      []queueMark      `json:"marks"`
	Actions    []queueAction    `json:"actions"`
	FileGroups []queueFileGroup `json:"fileGroups"`
}

type queueEntryArgs struct {
	Queue string `json:"queue"`
	Entry string `json:"entry"`
	To    int    `json:"to"`
}

func queueID(common, into string) string {
	sum := sha1.Sum([]byte(common + "\x00" + into))
	return hex.EncodeToString(sum[:])[:16]
}

func (a *app) queueList() []landQueue {
	a.queues.mu.Lock()
	var out []landQueue
	for _, q := range a.queues.byID {
		if len(q.Entries) > 0 {
			out = append(out, q.clone())
		}
	}
	a.queues.mu.Unlock()
	slices.SortFunc(out, func(x, y landQueue) int {
		return cmp.Or(strings.Compare(x.Repo, y.Repo), strings.Compare(x.Into, y.Into))
	})
	return out
}

func (q *landQueue) clone() landQueue {
	c := *q
	c.Entries = slices.Clone(q.Entries)
	for i := range c.Entries {
		c.Entries[i].Files = slices.Clone(c.Entries[i].Files)
	}
	c.Done = slices.Clone(q.Done)
	return c
}

func (q *landQueue) head() int {
	return slices.IndexFunc(q.Entries, func(e queueEntry) bool { return e.Mode != queueModeDirty })
}

func (q *landQueue) entry(id string) int {
	return slices.IndexFunc(q.Entries, func(e queueEntry) bool { return e.ID == id })
}

func (a *app) restoreQueues(ctx context.Context) error {
	keys, err := a.host.StorageList(ctx, landQueueKey)
	if err != nil {
		return err
	}
	byID := map[string]*landQueue{}
	active := map[string]time.Time{}
	now := time.Now().UTC()
	for _, key := range keys {
		var q landQueue
		if found, err := a.host.StorageGet(ctx, key, &q); err != nil || !found {
			logError("read landing queue "+key, err)
			continue
		}
		q.Entries = slices.DeleteFunc(q.Entries, func(e queueEntry) bool { return e.Mode != queueModeDirty && a.goneLander(e.Session) })
		q.pruneDone(now)
		a.storeQueue(ctx, &q)
		if !q.empty() {
			byID[q.ID] = &q
		}
		if h := q.head(); h >= 0 {
			active[q.Entries[h].ID] = now
		}
	}
	shown := a.aheadEnabled(ctx)
	a.queues.mu.Lock()
	a.queues.byID, a.queues.shown, a.queues.active = byID, shown, active
	a.queues.markLocked()
	a.queues.mu.Unlock()
	return nil
}

func (a *app) goneLander(session string) bool {
	s := a.session(session)
	return s == nil || s.snapshot().closed()
}

func (q *landQueue) pruneDone(now time.Time) {
	q.Done = slices.DeleteFunc(q.Done, func(e queueEntry) bool { return now.Sub(e.DoneAt) > queueDoneAge })
}

func (q *landQueue) empty() bool {
	return len(q.Entries) == 0 && len(q.Done) == 0
}

func (a *app) mutateQueue(ctx context.Context, qid string, fresh *landQueue, caller string, change func(*landQueue) error) (landQueue, error) {
	lock := a.queues.lock(qid)
	q, tells, synced, changed, err := a.mutateLocked(ctx, qid, fresh, caller, change)
	a.queues.unlock(qid, lock)
	if err != nil {
		return q, err
	}
	for _, s := range synced {
		a.saveNow(ctx, s)
	}
	if changed {
		a.kick(q.Workspace)
	}
	a.tellQueue(tells)
	return q, nil
}

func (a *app) mutateLocked(ctx context.Context, qid string, fresh *landQueue, caller string, change func(*landQueue) error) (landQueue, []queueTell, []*session, bool, error) {
	a.queues.mu.Lock()
	cur, known := a.queues.byID[qid]
	var q landQueue
	switch {
	case known:
		q = cur.clone()
	case fresh != nil:
		q = *fresh
	default:
		a.queues.mu.Unlock()
		return landQueue{}, nil, nil, false, errQueueUnknown
	}
	a.queues.mu.Unlock()
	before := q.clone()
	if change != nil {
		if err := change(&q); err != nil {
			return before, nil, nil, false, err
		}
	}
	now := time.Now().UTC()
	tells := a.settle(ctx, &q, now, caller)
	tells = append(tells, a.passStuck(ctx, &q, now)...)
	q.pruneDone(now)
	changed := !known || before.Tip != q.Tip || !reflect.DeepEqual(before.Entries, q.Entries) || len(before.Done) != len(q.Done)
	a.queues.mu.Lock()
	if a.queues.byID == nil {
		a.queues.byID = map[string]*landQueue{}
	}
	for _, e := range before.Entries {
		if q.entry(e.ID) < 0 {
			delete(a.queues.active, e.ID)
		}
	}
	if q.empty() {
		delete(a.queues.byID, qid)
	} else {
		stored := q.clone()
		a.queues.byID[qid] = &stored
	}
	a.queues.markLocked()
	a.queues.mu.Unlock()
	if !changed {
		return q, tells, nil, false, nil
	}
	a.storeQueue(ctx, &q)
	return q, tells, a.syncAsks(before, q), true, nil
}

func (a *app) joinQueue(ctx context.Context, workspaceID string, args landGateArgs) (queueTurn, error) {
	_, common, err := gitrepo.Dirs(ctx, args.Target)
	if err != nil {
		return queueTurn{}, err
	}
	files := landFiles(ctx, args.Target, args.Old, args.Tree)
	oldTree, _ := gitrepo.TreeOf(ctx, args.Target, args.Old)
	repo := primaryOf(ctx, args.Target)
	now := time.Now().UTC()
	qid := queueID(common, args.Into)
	fresh := &landQueue{ID: qid, Common: common, Into: args.Into}
	q, err := a.mutateQueue(ctx, qid, fresh, args.Session, func(q *landQueue) error {
		q.Target, q.Workspace, q.Repo = args.Target, workspaceID, cmp.Or(repo, q.Repo)
		i, err := q.place(args, now)
		if err != nil {
			return err
		}
		e := &q.Entries[i]
		same := e.Old == args.Old && e.Tree == args.Tree
		asks := same && e.State == queueAsks
		e.UserYes = same && e.UserYes
		e.Worktree, e.Old, e.OldTree, e.Tree, e.Files, e.CalledAt, e.Tip = args.Worktree, args.Old, oldTree, args.Tree, files, now, args.Old
		e.SourceHead, e.Checked, e.Clean = args.Head, args.Old, true
		e.State, e.Why, e.Passed = queueReady, "", false
		if asks {
			e.State = queueAsks
		}
		return nil
	})
	if err != nil {
		return queueTurn{}, err
	}
	if repo != "" {
		go a.rehook(context.WithoutCancel(ctx), repo)
	}
	if args.Source != "" {
		a.leaveOtherQueues(ctx, qid, common, args.Session, args.Source)
	}
	return a.turnOf(&q, args), nil
}

func (q *landQueue) place(args landGateArgs, now time.Time) (int, error) {
	if args.Source == "" {
		own := gitrepo.Canonical(args.Worktree)
		if slices.ContainsFunc(q.Entries, func(e queueEntry) bool {
			return e.Mode == queueModeIndex && e.Session != args.Session && gitrepo.Canonical(e.Worktree) == own
		}) {
			return 0, queueRefused{text: fmt.Sprintf(queueIndexFormat, args.Worktree)}
		}
	}
	if i := slices.IndexFunc(q.Entries, func(e queueEntry) bool { return e.Session == args.Session && e.Source == args.Source }); i >= 0 {
		if args.Source == "" {
			q.Entries[i].Mode = queueModeIndex
		}
		return i, nil
	}
	e := queueEntry{ID: id.New(queueEntryPrefix), Session: args.Session, Source: args.Source, Mode: queueModeBranch, Since: now}
	if args.Source != "" {
		q.Entries = append(q.Entries, e)
		return len(q.Entries) - 1, nil
	}
	e.Mode = queueModeIndex
	at := slices.IndexFunc(q.Entries, func(e queueEntry) bool { return e.Mode == queueModeBranch })
	if at < 0 {
		at = len(q.Entries)
	}
	q.Entries = slices.Insert(q.Entries, at, e)
	return at, nil
}

func (a *app) leaveOtherQueues(ctx context.Context, joined, common, session, source string) {
	for _, q := range a.queueList() {
		if q.ID == joined || q.Common != common || !slices.ContainsFunc(q.Entries, func(e queueEntry) bool { return e.Session == session && e.Source == source }) {
			continue
		}
		_, err := a.mutateQueue(ctx, q.ID, nil, "", func(q *landQueue) error {
			q.Entries = slices.DeleteFunc(q.Entries, func(e queueEntry) bool { return e.Session == session && e.Source == source })
			return nil
		})
		logError("leave landing queue", err)
	}
}

func (a *app) turnOf(q *landQueue, args landGateArgs) queueTurn {
	i := slices.IndexFunc(q.Entries, func(e queueEntry) bool {
		return e.Session == args.Session && e.Source == args.Source && e.Mode != queueModeDirty
	})
	if i < 0 {
		return queueTurn{}
	}
	if s := q.Entries[i].State; s == queueHead || s == queueAsks {
		return queueTurn{Head: true}
	}
	var ahead []string
	for _, e := range q.Entries[:i] {
		ahead = append(ahead, fmt.Sprintf(queueAheadFormat, a.entryTitle(e), entryWhat(e)))
	}
	return queueTurn{Text: fmt.Sprintf(queuedFormat, i+1, len(q.Entries), q.Into, strings.Join(ahead, ", "))}
}

func (a *app) entryTitle(e queueEntry) string {
	if e.Session == "" {
		return queueUser
	}
	return cmp.Or(firstLine(a.titleOf(e.Session)), e.Session)
}

func entryWhat(e queueEntry) string {
	switch e.Mode {
	case queueModeIndex:
		return queueStaged
	case queueModeDirty:
		return fmt.Sprintf(queueEditsIn, strings.Join(e.Files, ", "))
	}
	return e.Source
}

func (a *app) settle(ctx context.Context, q *landQueue, now time.Time, caller string) []queueTell {
	tip, err := revParseIn(ctx, q.Target, headsPrefix+q.Into)
	if err != nil {
		if _, serr := os.Stat(q.Target); !errors.Is(serr, os.ErrNotExist) && !refMissing(ctx, err) {
			return nil
		}
		var tells []queueTell
		told := map[string]bool{}
		for _, e := range q.Entries {
			if e.Session != "" && e.Mode != queueModeDirty && !told[e.Session] {
				told[e.Session] = true
				tells = append(tells, queueTell{session: e.Session, text: fmt.Sprintf(queueGoneFormat, q.Into)})
			}
		}
		q.Entries, q.Done = nil, nil
		return tells
	}
	if tip != q.Tip {
		q.landedTo(ctx, tip, now)
		q.Tip = tip
	}
	q.refresh(ctx)
	tells := a.dirty(ctx, q)
	h := q.head()
	for i := range q.Entries {
		e := &q.Entries[i]
		if i == h || e.Mode == queueModeDirty {
			continue
		}
		e.Turn, e.Why = false, ""
		e.State = queueWaiting
		if !e.Passed && (e.Mode == queueModeIndex || e.Clean) {
			e.State = queueReady
		}
	}
	if h < 0 {
		return tells
	}
	e := &q.Entries[h]
	if e.State == queueBlocked || e.State == queueAsks && e.Turn && e.Tip == tip {
		return tells
	}
	e.State, e.Why = queueHead, ""
	if e.Turn && e.Tip == tip {
		return tells
	}
	e.Turn, e.Tip = true, tip
	if e.HeadAt.IsZero() {
		e.HeadAt = now
	}
	if e.Session != caller && e.Session != "" {
		e.PromptedAt = now
		tells = append(tells, queueTell{session: e.Session, text: fmt.Sprintf(queueTurnFormat, q.Into, tip)})
	}
	return tells
}

func (q *landQueue) landedTo(ctx context.Context, tip string, now time.Time) {
	out, err := gitrepo.Run(ctx, q.Target, "rev-parse", tip+"^{tree}", tip+"^")
	lines := strings.Fields(string(out))
	if err != nil || len(lines) != 2 {
		return
	}
	tree, parent := lines[0], lines[1]
	for i := 0; i < len(q.Entries); {
		e := q.Entries[i]
		if e.Mode == queueModeDirty || e.Tree != tree || e.Old != parent {
			i++
			continue
		}
		e.DoneAt = now
		q.Done = append(q.Done, e)
		if len(q.Done) > queueDoneMax {
			q.Done = q.Done[len(q.Done)-queueDoneMax:]
		}
		q.Entries = slices.Delete(q.Entries, i, i+1)
	}
}

func (q *landQueue) refresh(ctx context.Context) {
	tipTree, err := gitrepo.TreeOf(ctx, q.Target, q.Tip)
	if err != nil {
		return
	}
	for i := 0; i < len(q.Entries); {
		e := &q.Entries[i]
		if e.Mode != queueModeBranch {
			i++
			continue
		}
		head, err := revParseIn(ctx, q.Target, headsPrefix+e.Source)
		if err != nil {
			if refMissing(ctx, err) {
				q.Entries = slices.Delete(q.Entries, i, i+1)
				continue
			}
			i++
			continue
		}
		if head == e.SourceHead && e.Checked == q.Tip {
			i++
			continue
		}
		e.SourceHead, e.Checked = head, q.Tip
		out, err := gitrepo.Run(ctx, q.Target, "merge-tree", "--write-tree", "--name-only", "--no-messages", q.Tip, head)
		tree, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
		if err == nil && tree == tipTree {
			q.Entries = slices.Delete(q.Entries, i, i+1)
			continue
		}
		e.Clean = err == nil
		i++
	}
}

func refMissing(ctx context.Context, err error) bool {
	ee, ok := errors.AsType[*exec.ExitError](err)
	return ok && ee.ExitCode() == 1 && ctx.Err() == nil
}

func (a *app) dirty(ctx context.Context, q *landQueue) []queueTell {
	changed, ok := dirtyFiles(ctx, q.Target)
	if !ok {
		return nil
	}
	var old []queueEntry
	rest := slices.DeleteFunc(slices.Clone(q.Entries), func(e queueEntry) bool {
		if e.Mode == queueModeDirty {
			old = append(old, e)
			return true
		}
		return false
	})
	if len(rest) == 0 {
		q.Entries = rest
		return nil
	}
	head := &rest[0]
	staged := map[string]bool{}
	for _, e := range rest {
		if e.Mode == queueModeIndex {
			for _, f := range e.Files {
				staged[f] = true
			}
		}
	}
	var overlap []string
	for _, f := range changed {
		if !staged[f] && slices.Contains(head.Files, f) {
			overlap = append(overlap, f)
		}
	}
	ownerOf := a.dirtyOwners(q.Target, overlap)
	byOwner := map[string][]string{}
	var owners []string
	for _, f := range overlap {
		owner := ownerOf[f]
		if _, ok := byOwner[owner]; !ok {
			owners = append(owners, owner)
		}
		byOwner[owner] = append(byOwner[owner], f)
	}
	var dirty []queueEntry
	var tells []queueTell
	var all []string
	for _, owner := range owners {
		files := byOwner[owner]
		all = append(all, files...)
		e := queueEntry{ID: id.New(queueEntryPrefix), Session: owner, Mode: queueModeDirty, Since: time.Now().UTC()}
		if i := slices.IndexFunc(old, func(o queueEntry) bool { return o.Session == owner }); i >= 0 {
			e = old[i]
		}
		e.Files, e.State, e.Why = files, queueBlocked, fmt.Sprintf(queueWhyDirty, strings.Join(files, ", "))
		if owner != "" && !e.Told {
			e.Told = true
			tells = append(tells, queueTell{session: owner, text: fmt.Sprintf(queueDirtyFormat, q.Target, strings.Join(files, ", "), a.entryTitle(*head), entryWhat(*head), q.Into)})
		}
		dirty = append(dirty, e)
	}
	if len(dirty) > 0 {
		head.State, head.Why, head.Turn = queueBlocked, fmt.Sprintf(queueWhyHead, strings.Join(all, ", ")), false
	} else if head.State == queueBlocked {
		head.State = queueHead
	}
	q.Entries = append(dirty, rest...)
	return tells
}

func dirtyFiles(ctx context.Context, worktree string) ([]string, bool) {
	out, err := gitrepo.Run(ctx, worktree, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return nil, false
	}
	var files []string
	for rec := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if len(rec) > 3 {
			files = append(files, rec[3:])
		}
	}
	return files, true
}

func (a *app) dirtyOwners(worktree string, files []string) map[string]string {
	owners := map[string]string{}
	if len(files) == 0 {
		return owners
	}
	dir := gitrepo.Canonical(worktree)
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(dir, filepath.FromSlash(f))
	}
	newest := map[string]time.Time{}
	for _, s := range a.allSessions() {
		rec := s.snapshot()
		if rec.closed() || rec.GateFor != nil || rec.Worktree == "" || gitrepo.Canonical(rec.Worktree) != dir {
			continue
		}
		edited := s.editedSet()
		for i, f := range files {
			if edited[paths[i]] && (owners[f] == "" || rec.UpdatedAt.After(newest[f])) {
				owners[f], newest[f] = rec.ID, rec.UpdatedAt
			}
		}
	}
	return owners
}

func (s *session) editedSet() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	seen, set := map[string]bool{}, map[string]bool{}
	add := func(entries []*entry) {
		for _, en := range entries {
			if en.Tool == nil {
				continue
			}
			for _, p := range editedPaths(en.Tool) {
				if !seen[p] {
					seen[p] = true
					set[gitrepo.Canonical(p)] = true
				}
			}
		}
	}
	add(s.entries)
	for _, sub := range s.subagents {
		add(sub.entries)
	}
	return set
}

func landFiles(ctx context.Context, dir, old, tree string) []string {
	out, err := gitrepo.Run(ctx, dir, "diff", "--name-only", "-z", "--no-renames", "--no-ext-diff", old, tree)
	if err != nil {
		return nil
	}
	var files []string
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name != "" {
			files = append(files, name)
		}
	}
	return files
}

func revParseIn(ctx context.Context, dir, rev string) (string, error) {
	out, err := gitrepo.Run(ctx, dir, "rev-parse", "-q", "--verify", rev)
	return strings.TrimSpace(string(out)), err
}

func (a *app) storeQueue(ctx context.Context, q *landQueue) {
	q.Updated = time.Now().UTC()
	if len(q.Entries) == 0 && len(q.Done) == 0 {
		logError("forget landing queue", a.host.StorageDelete(ctx, landQueueKey+q.ID))
	} else {
		logError("store landing queue", a.host.StorageSet(ctx, landQueueKey+q.ID, q))
	}
	logError("write landing queue mark", landgate.WriteQueueMark(landgate.QueueDir(a.hooksDir(), q.Common), q.Into, len(q.Entries) > 0))
}

func (a *app) tellQueue(tells []queueTell) {
	for _, t := range tells {
		s := a.session(t.session)
		if s == nil {
			continue
		}
		if err := a.submit(s, &promptArgs{Text: t.text, Land: true}); err != nil && !errors.Is(err, errClosed) {
			fmt.Fprintln(os.Stderr, "agents: landing queue prompt:", err)
		}
	}
}

func (a *app) advanceQueue(ctx context.Context, common, into string) {
	if _, err := a.mutateQueue(ctx, queueID(common, into), nil, "", nil); err != nil && !errors.Is(err, errQueueUnknown) {
		logError("advance landing queue", err)
	}
}

func (a *app) passStuck(ctx context.Context, q *landQueue, now time.Time) []queueTell {
	h := q.head()
	if h < 0 || q.Entries[h].State != queueHead && q.Entries[h].State != queueAsks || !q.Entries[h].Turn {
		return nil
	}
	asks := q.Entries[h].State == queueAsks
	if !asks && a.headActive(q, q.Entries[h]) {
		a.queues.mu.Lock()
		if a.queues.active == nil {
			a.queues.active = map[string]time.Time{}
		}
		a.queues.active[q.Entries[h].ID] = now
		a.queues.mu.Unlock()
		return nil
	}
	if !asks && !a.headStuck(q, q.Entries[h], now) {
		return nil
	}
	j := slices.IndexFunc(q.Entries[h+1:], func(e queueEntry) bool { return e.State == queueReady })
	if j < 0 {
		return nil
	}
	j += h + 1
	passed := q.Entries[h]
	passed.Skips++
	passed.Passed, passed.Turn, passed.State = true, false, queueWaiting
	next := q.Entries[j]
	q.Entries = slices.Delete(q.Entries, j, j+1)
	q.Entries[h] = next
	q.Entries = slices.Insert(q.Entries, h+1, passed)
	return a.settle(ctx, q, now, "")
}

func (a *app) headActive(q *landQueue, e queueEntry) bool {
	s := a.session(e.Session)
	if s == nil {
		return false
	}
	rec := s.snapshot()
	if rec.Status == statusRunning {
		return true
	}
	st := s.landReview(q.Into, e.Source)
	if st.Run == "" || st.Last != "" || st.Request != "" {
		return false
	}
	_, reviewing := a.reviewing(rec.ID, st)
	return reviewing || a.checking(st) || a.gateRunOpen(st.Run)
}

func (a *app) headStuck(q *landQueue, e queueEntry, now time.Time) bool {
	if e.State != queueHead || !e.Turn || a.headActive(q, e) {
		return false
	}
	a.queues.mu.Lock()
	last := a.queues.active[e.ID]
	a.queues.mu.Unlock()
	for _, t := range []time.Time{e.HeadAt, e.PromptedAt, e.CalledAt} {
		if t.After(last) {
			last = t
		}
	}
	return now.Sub(last) >= queueStuck
}

func (a *app) landQueueLoop(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-t.C:
			a.checkQueues("")
		}
	}
}

func (a *app) checkQueues(workspaceID string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupLimit)
	defer cancel()
	a.queues.mu.Lock()
	var ids []string
	for id, q := range a.queues.byID {
		if workspaceID == "" || q.Workspace == workspaceID {
			ids = append(ids, id)
		}
	}
	a.queues.mu.Unlock()
	for _, id := range ids {
		if _, err := a.mutateQueue(ctx, id, nil, "", nil); err != nil && !errors.Is(err, errQueueUnknown) {
			logError("check landing queue", err)
		}
		go a.reviewAhead(id)
	}
}

func (a *app) advanceLanded(ctx context.Context, args landGateArgs) {
	if _, common, err := gitrepo.Dirs(ctx, args.Target); err == nil {
		a.advanceQueue(ctx, common, args.Into)
	}
}

func (a *app) leaveQueues(ctx context.Context, session string) {
	for _, q := range a.queueList() {
		if !slices.ContainsFunc(q.Entries, func(e queueEntry) bool { return e.Session == session && e.Mode != queueModeDirty }) {
			continue
		}
		_, err := a.mutateQueue(ctx, q.ID, nil, "", func(q *landQueue) error {
			q.Entries = slices.DeleteFunc(q.Entries, func(e queueEntry) bool { return e.Session == session && e.Mode != queueModeDirty })
			return nil
		})
		logError("leave landing queue", err)
	}
}

func (a *app) queueWait(args landGateArgs) float64 {
	a.queues.mu.Lock()
	defer a.queues.mu.Unlock()
	for _, q := range a.queues.byID {
		if q.Into != args.Into {
			continue
		}
		for _, e := range slices.Concat(q.Entries, q.Done) {
			if e.Session == args.Session && e.Source == args.Source && e.Tree == args.Tree && !e.HeadAt.IsZero() {
				return e.HeadAt.Sub(e.Since).Seconds()
			}
		}
	}
	return 0
}

func (a *app) queuePlaceOf(session string) *queuePlace {
	a.queues.mu.Lock()
	defer a.queues.mu.Unlock()
	var best *queuePlace
	for _, q := range a.queues.byID {
		for i, e := range q.Entries {
			if e.Session != session || e.Mode == queueModeDirty {
				continue
			}
			p := queuePlace{Queue: q.ID, Into: q.Into, Place: i + 1, Of: len(q.Entries), State: e.State, Why: e.Why}
			if best == nil || p.Place < best.Place || p.Place == best.Place && p.Queue < best.Queue {
				best = &p
			}
		}
	}
	return best
}

func (a *app) inLandQueue(session string) bool {
	a.queues.mu.Lock()
	defer a.queues.mu.Unlock()
	for _, q := range a.queues.byID {
		if slices.ContainsFunc(q.Entries, func(e queueEntry) bool { return e.Session == session }) {
			return true
		}
	}
	return false
}

func (a *app) landQueueCommand(_ context.Context, cmd pluginsdk.CommandRun, _ struct{}) (any, error) {
	return landQueueResult{Queues: a.queueTrees(cmd.WorkspaceID, a.list(cmd.WorkspaceID))}, nil
}

func (a *app) queueInWorkspace(q landQueue, workspaceID string) bool {
	if q.Workspace == workspaceID {
		return true
	}
	return slices.ContainsFunc(q.Entries, func(e queueEntry) bool {
		s := a.session(e.Session)
		return s != nil && s.snapshot().Workspace == workspaceID
	})
}

func (a *app) queueRowOf(q landQueue, now time.Time) queueRow {
	row := queueRow{ID: q.ID, Repo: q.Repo, Into: q.Into, Entries: []queueEntryRow{}}
	h := q.head()
	steps := map[string]string{}
	for i, e := range q.Entries {
		step := a.headStep(q, e)
		steps[e.ID] = step
		r := queueEntryRow{ID: e.ID, Session: e.Session, Title: a.entryTitle(e), Source: e.Source, Mode: e.Mode, State: e.State, Place: i + 1, Why: cmp.Or(e.Why, entryWhy(e, step)), Skips: e.Skips, Overlaps: []string{}}
		if e.Session == "" {
			r.Title = queueUserEdits
		}
		r.Marks = a.entryMarks(q, e)
		r.Files = slices.Clone(e.Files)
		if len(r.Files) > queueFilesMax {
			r.Files, r.More = r.Files[:queueFilesMax], len(r.Files)-queueFilesMax
		}
		if r.Files == nil {
			r.Files = []string{}
		}
		for _, o := range q.Entries {
			if o.ID != e.ID && slices.ContainsFunc(o.Files, func(f string) bool { return slices.Contains(e.Files, f) }) {
				r.Overlaps = append(r.Overlaps, o.ID)
			}
		}
		switch {
		case e.State == queueAsks:
			r.NeedsYou = true
		case e.Mode == queueModeDirty:
			r.NeedsYou = e.Session == ""
		case i == h && len(q.Entries) > h+1 && a.headStuck(&q, e, now) && !slices.ContainsFunc(q.Entries[h+1:], func(o queueEntry) bool { return o.State == queueReady }):
			r.NeedsYou, r.Why = true, fmt.Sprintf(queueWhyStuck, queueStuck)
		}
		row.NeedsYou = row.NeedsYou || r.NeedsYou
		row.Entries = append(row.Entries, r)
	}
	dressQueue(&row, steps)
	return row
}

func (q *landQueue) userEntry(id string) (int, error) {
	i := q.entry(id)
	switch {
	case i < 0:
		return 0, errQueueUnknown
	case q.Entries[i].Mode == queueModeDirty:
		return 0, errQueueDirty
	}
	return i, nil
}

func (q *landQueue) ordered() error {
	branch := false
	for _, e := range q.Entries {
		if e.Mode == queueModeBranch {
			branch = true
		} else if branch {
			return errQueueOrder
		}
	}
	return nil
}

func (a *app) landQueueMoveCommand(ctx context.Context, cmd pluginsdk.CommandRun, args queueEntryArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errUserOnly
	}
	return nil, a.moveEntry(ctx, args)
}

func (a *app) moveEntry(ctx context.Context, args queueEntryArgs) error {
	_, err := a.mutateQueue(ctx, args.Queue, nil, "", func(q *landQueue) error {
		i, err := q.userEntry(args.Entry)
		if err != nil {
			return err
		}
		if args.To < 1 || args.To > len(q.Entries) {
			return errQueuePlace
		}
		e := q.Entries[i]
		q.Entries = slices.Insert(slices.Delete(q.Entries, i, i+1), args.To-1, e)
		return q.ordered()
	})
	return err
}

func (a *app) landQueueSkipCommand(ctx context.Context, cmd pluginsdk.CommandRun, args queueEntryArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errUserOnly
	}
	return nil, a.skipEntry(ctx, args)
}

func (a *app) skipEntry(ctx context.Context, args queueEntryArgs) error {
	_, err := a.mutateQueue(ctx, args.Queue, nil, "", func(q *landQueue) error {
		i, err := q.userEntry(args.Entry)
		if err != nil || i == len(q.Entries)-1 {
			return err
		}
		e := q.Entries[i]
		e.Skips++
		e.Turn = false
		q.Entries = slices.Insert(slices.Delete(q.Entries, i, i+1), i+1, e)
		return q.ordered()
	})
	return err
}

func (a *app) landQueueRemoveCommand(ctx context.Context, cmd pluginsdk.CommandRun, args queueEntryArgs) (any, error) {
	if !a.userWord(cmd) {
		return nil, errUserOnly
	}
	return nil, a.removeEntry(ctx, args)
}

func (a *app) removeEntry(ctx context.Context, args queueEntryArgs) error {
	var gone queueEntry
	q, err := a.mutateQueue(ctx, args.Queue, nil, "", func(q *landQueue) error {
		i, err := q.userEntry(args.Entry)
		if err != nil {
			return err
		}
		gone = q.Entries[i]
		q.Entries = slices.Delete(q.Entries, i, i+1)
		return nil
	})
	if err == nil && gone.Session != "" {
		a.tellQueue([]queueTell{{session: gone.Session, text: fmt.Sprintf(queueRemovedFormat, cmp.Or(gone.Source, queueStaged+" change"), q.Into)}})
	}
	return err
}
