package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/pkg/id"
	"clubmate/pkg/sdui"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/landgate"
)

const (
	queueAskedText    = "queued: approved; waits for the user's yes"
	queueYesFormat    = "[Clubmate landing queue] The user said yes to landing %s into %s. Call git_land again now; Clubmate commits it."
	queueNotYetFormat = "[Clubmate landing queue] The user said not yet to landing %s into %s, so it left the queue. Don't call git_land for it again unless the user asks."
	queueAskTitle     = "Land into %s?"
	queueAskApproved  = "review approved"
	queueAskTests     = "tests pass"
	queueAskNoGate    = "no land gate"
	queueAskLand      = "Land"
	queueAskNotYet    = "Not yet"
	queueAskNode      = "landqueue"
	queueAskSep       = " · "
)

type queueAsk struct {
	Request string    `json:"request"`
	Queue   string    `json:"queue,omitempty"`
	Into    string    `json:"into,omitempty"`
	Source  string    `json:"source,omitempty"`
	Detail  string    `json:"detail"`
	Since   time.Time `json:"since"`
}

func asksBeforeLand(rec record) bool {
	return rec.ParentSession == "" && rec.GateFor == nil && rec.Run == nil && rec.Finish != finishMerge
}

func (q *landQueue) own(args landGateArgs) int {
	return slices.IndexFunc(q.Entries, func(e queueEntry) bool {
		return e.Session == args.Session && e.Source == args.Source && e.Mode != queueModeDirty
	})
}

func (a *app) askBeforeLand(ctx context.Context, workspaceID string, args landGateArgs, res landGateResult) (landGateResult, error) {
	s := a.session(args.Session)
	if res.State != gateApproved || s == nil || !asksBeforeLand(s.snapshot()) {
		return res, nil
	}
	_, common, err := gitrepo.Dirs(ctx, args.Target)
	if err != nil {
		return landGateResult{}, err
	}
	g := a.landingGate(ctx, workspaceID, args.Session, args.Target, args.Into, args.Source)
	detail := askDetail(ctx, g, args)
	var v landgate.Verdict
	decided := false
	if g != nil {
		v, decided = a.verdictOf(ctx, g.ID, args.Old, args.Tree)
	}
	yes := decided && v.Outcome == gateApproved && v.By == landgate.ByUser
	q, err := a.mutateQueue(ctx, queueID(common, args.Into), nil, args.Session, func(q *landQueue) error {
		i := q.own(args)
		if i < 0 {
			return errQueueUnknown
		}
		e := &q.Entries[i]
		if yes = yes || e.UserYes; yes || e.State == queueAsks {
			return nil
		}
		e.State, e.Ask = queueAsks, &queueAsk{Request: id.New(queueEntryPrefix), Source: e.Source, Detail: detail, Since: time.Now().UTC()}
		return nil
	})
	switch {
	case errors.Is(err, errQueueUnknown):
		return landGateResult{State: queueStateGit, Text: fmt.Sprintf(queueLeftFormat, args.Into)}, nil
	case yes:
		return res, nil
	case err != nil:
		return landGateResult{}, err
	}
	if i := q.own(args); i >= 0 && q.Entries[i].State == queueAsks {
		return landGateResult{State: queueStateGit, Text: queueAskedText}, nil
	}
	if turn := a.turnOf(&q, args); !turn.Head {
		return landGateResult{State: queueStateGit, Text: turn.Text}, nil
	}
	return res, nil
}

func askDetail(ctx context.Context, g *gateConfig, args landGateArgs) string {
	parts := []string{queueStaged}
	if args.Source != "" {
		out, err := gitrepo.Run(ctx, args.Target, "rev-list", "--count", args.Old+".."+cmp.Or(args.Head, headsPrefix+args.Source))
		n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
		parts = parts[:0]
		if err == nil && n > 0 {
			parts = append(parts, counted(n, "commit"))
		}
	}
	switch {
	case g == nil:
		parts = append(parts, queueAskNoGate)
	case g.Test != "":
		parts = append(parts, queueAskApproved, queueAskTests)
	default:
		parts = append(parts, queueAskApproved)
	}
	return strings.Join(parts, queueAskSep)
}

func (a *app) syncAsks(before, q landQueue) []*session {
	want := map[string]queueAsk{}
	for _, e := range q.Entries {
		if e.State == queueAsks && e.Ask != nil && e.Session != "" {
			ask := *e.Ask
			ask.Queue, ask.Into = q.ID, q.Into
			want[e.Session] = ask
		}
	}
	seen := map[string]bool{}
	var synced []*session
	for _, e := range slices.Concat(before.Entries, q.Entries) {
		if e.Session == "" || seen[e.Session] {
			continue
		}
		seen[e.Session] = true
		if s := a.session(e.Session); s != nil && s.putQueueAsk(q.ID, want) {
			synced = append(synced, s)
		}
	}
	return synced
}

func (s *session) putQueueAsk(queue string, want map[string]queueAsk) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	ask, ok := want[s.rec.ID]
	i := slices.IndexFunc(s.rec.QueueAsks, func(qa queueAsk) bool { return qa.Queue == queue })
	switch {
	case i < 0 && !ok:
		return false
	case i >= 0 && ok && s.rec.QueueAsks[i] == ask:
		return false
	}
	asks := slices.DeleteFunc(slices.Clone(s.rec.QueueAsks), func(qa queueAsk) bool { return qa.Queue == queue })
	if ok {
		asks = append(asks, ask)
	}
	s.rec.QueueAsks = asks
	return true
}

func (a *app) findQueueAsk(workspaceID, request string) (landQueue, queueEntry, bool) {
	for _, q := range a.queueList() {
		for _, e := range q.Entries {
			if e.State != queueAsks || e.Ask == nil || e.Ask.Request != request {
				continue
			}
			if _, err := a.lookup(workspaceID, e.Session); err != nil {
				return landQueue{}, queueEntry{}, false
			}
			return q, e, true
		}
	}
	return landQueue{}, queueEntry{}, false
}

func (a *app) answerQueueAsk(ctx context.Context, workspaceID string, args landAnswerArgs) error {
	q, asked, ok := a.findQueueAsk(workspaceID, args.Request)
	if !ok {
		return errLandStale
	}
	format := queueYesFormat
	switch args.Action {
	case landActionMerge:
	case landActionLeave:
		format = queueNotYetFormat
	default:
		return fmt.Errorf("%w: %s", errLandAction, args.Action)
	}
	_, err := a.mutateQueue(ctx, q.ID, nil, "", func(q *landQueue) error {
		i := q.entry(asked.ID)
		if i < 0 || q.Entries[i].State != queueAsks || q.Entries[i].Ask == nil || q.Entries[i].Ask.Request != args.Request {
			return errLandStale
		}
		if args.Action == landActionLeave {
			q.Entries = slices.Delete(q.Entries, i, i+1)
			return nil
		}
		e := &q.Entries[i]
		e.UserYes, e.State, e.Ask = true, queueHead, nil
		return nil
	})
	if err != nil {
		return err
	}
	a.tellQueue([]queueTell{{session: asked.Session, text: fmt.Sprintf(format, cmp.Or(asked.Source, queueStaged+" change"), q.Into)}})
	return nil
}

func (r record) queueAskWaits() bool {
	return !r.closed() && len(r.QueueAsks) > 0
}

func (r record) queueAskRequests() []string {
	if r.closed() {
		return nil
	}
	var out []string
	for _, qa := range r.QueueAsks {
		out = append(out, qa.Request)
	}
	return out
}

func (r record) queueAskPending() *pendingSummary {
	if !r.queueAskWaits() {
		return nil
	}
	qa := r.QueueAsks[0]
	return &pendingSummary{Kind: pendingLand, Text: fmt.Sprintf(queueAskTitle, qa.Into), At: qa.Since, OnMac: true}
}

func (r record) queueAskMessage(request string) string {
	i := slices.IndexFunc(r.QueueAsks, func(qa queueAsk) bool { return qa.Request == request })
	if i < 0 {
		return ""
	}
	return fmt.Sprintf(cardMessageFormat, fmt.Sprintf(queueAskTitle, r.QueueAsks[i].Into), r.QueueAsks[i].Detail)
}

func queueAskCard(rec record, qa queueAsk) sdui.Node {
	return sdui.WithDock(sdui.LandRequestCard(rec.ID+"/"+queueAskNode+"/"+qa.Request, qa.Request, sdui.LandRequestProps{
		Title:  fmt.Sprintf(queueAskTitle, qa.Into),
		Name:   cmp.Or(qa.Source, qa.Into),
		Detail: qa.Detail,
		Options: []sdui.PermissionOption{
			{ID: landActionMerge, Label: queueAskLand, Kind: "allow_once", Shortcut: "opt+1", Short: queueAskLand},
			{ID: landActionLeave, Label: queueAskNotYet, Kind: "reject_once", Shortcut: "opt+2", Short: queueAskNotYet},
		},
	}))
}
