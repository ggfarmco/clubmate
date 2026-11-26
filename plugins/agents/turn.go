package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/landgate"
)

const (
	checktimeCmd = "editor-nvim.checktime"
	embedLimit   = 1 << 20
	imageOmitted = "[An image was attached, but this agent does not accept images.]"
	retryDelay   = 3 * time.Second
	sendHoldEnv  = "CLUBMATE_AGENTS_SEND_HOLD"
	takeHoldEnv  = "CLUBMATE_AGENTS_TAKE_HOLD"
	retryNotice  = "The turn failed; retrying once"
	retryFormat  = "[Clubmate] Your previous reply ended with an error (%s). Continue the task from where you stopped."
	fromFormat   = "[From Clubmate agent session \"%s\" (%s), not from the user]\n\n"
	fromUnknown  = "[From another agent session, not from the user]\n\n"
)

type runState struct {
	proc            *agentProc
	running         bool
	cycle           bool
	background      map[string]bool
	cancelRequested bool
	prior           string
	started         time.Time
	perms           map[string]*pendingPermission
	calls           []*heldCall
	refusals        []callRefusal
	owed            []owedFindings
	gateMsgs        map[string]gateMessage
	telling         map[string]bool
	userOnly        map[string]bool
	queue           []queuedPrompt
	sending         *queuedPrompt
	epoch           int
	superseded      map[string]bool
	asked           bool
	ending          *turnOutcome
	answered        bool
	late            bool
}

func (a *app) prompt(workspaceID string, args promptArgs) error {
	s, err := a.lookup(workspaceID, args.Session)
	if err != nil {
		return err
	}
	if err := a.saveUploads(s.snapshot().ID, args.Attachments); err != nil {
		return err
	}
	if err := a.refuseLeaving(s.snapshot().Worktree); err != nil {
		return err
	}
	if args.From == "" {
		err := a.submit(s, &args)
		if err == nil {
			s.clearDraft()
		}
		return err
	}
	if s.snapshot().ParentSession != args.From {
		return a.submit(s, &args)
	}
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if st := s.snapshot().Status; st != statusRunning && st != statusWaiting && a.limitChildren(args.From) >= maxChildren {
		return errChildLimit
	}
	return a.submit(s, &args)
}

func (a *app) submit(s *session, args *promptArgs) error {
	if args != nil && args.User {
		a.resetRounds(s, time.Time{})
	}
	if args != nil && !args.Queued && a.steered(s, *args) {
		return nil
	}
	unlock, err := a.lockAsideParent(s)
	if err != nil {
		return err
	}
	s.mu.Lock()
	unlock()
	s.loadLocked()
	switch {
	case s.rec.closed():
		s.mu.Unlock()
		return errClosed
	case s.running && args == nil:
		s.mu.Unlock()
		return nil
	case s.running:
		s.enqueueLocked(*args)
		s.mu.Unlock()
		s.changed(changeUpdate, true)
		a.wakeWaiters()
		return nil
	}
	if args != nil {
		s.enqueueLocked(*args)
	}
	a.runQueuedLocked(s)
	return nil
}

func (a *app) steered(s *session, args promptArgs) bool {
	s.mu.Lock()
	s.loadLocked()
	p, rec := s.proc, s.rec
	ok := s.running && !s.cancelRequested && !rec.closed() && p != nil && p.steering
	s.mu.Unlock()
	return ok && a.steer(s, p, rec, args)
}

func (a *app) lockAsideParent(s *session) (func(), error) {
	rec := s.snapshot()
	if !rec.isAside() {
		return func() {}, nil
	}
	a.asideMu.Lock()
	if parent := a.session(rec.ParentSession); parent == nil || parent.snapshot().closed() {
		a.asideMu.Unlock()
		return nil, errParentGone
	}
	return a.asideMu.Unlock, nil
}

func (a *app) runQueuedLocked(s *session) {
	next, ok := s.dequeueLocked()
	if !ok {
		s.mu.Unlock()
		return
	}
	turn, epoch := s.beginTurnLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	go a.runTurn(s, turn, epoch, next)
}

func (a *app) cancel(s *session, user bool) {
	a.cancelIf(s, user, func() bool { return true })
}

func (a *app) cancelIf(s *session, user bool, live func() bool) bool {
	s.mu.Lock()
	if !live() {
		s.mu.Unlock()
		return false
	}
	p, running, cycle, agentID, held := s.proc, s.running, s.cycle, s.rec.AgentSessionID, s.rec.Held
	idle := !running && s.busyLocked()
	if running {
		s.cancelRequested = true
		s.recordLocked(actorUser, kindCancel, acp.SessionRequest{SessionID: agentID})
	}
	if idle || !running && s.ending != nil {
		s.ending = &turnOutcome{cancelled: true}
	}
	if idle {
		s.dropBackgroundLocked()
	}
	s.followedUpLocked()
	if held != nil && !running {
		s.setStatusLocked(statusIdle, "")
		s.ending = nil
	}
	s.mu.Unlock()
	if held != nil && !running {
		s.changed(changeStatus, true)
		a.logHeld(s, *held, gateStopped, landgate.ByUser)
	}
	if p != nil && (running || cycle) {
		if err := p.conn.Notify(acp.MethodSessionCancel, acp.SessionRequest{SessionID: agentID}); err != nil {
			fmt.Fprintln(os.Stderr, "agents: cancel:", err)
		}
	}
	s.mu.Lock()
	s.cancelPermissionsLocked()
	s.mu.Unlock()
	a.endCalls(s, user)
	s.changed(changeUpdate, false)
	if idle {
		a.settleBackground(s)
	}
	return true
}

func (a *app) steerText(s *session, text string) {
	s.mu.Lock()
	p, agentID := s.proc, s.rec.AgentSessionID
	ok := s.running && !s.cancelRequested && !s.rec.closed() && p != nil && p.steering
	s.mu.Unlock()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var res acp.SteerResponse
	if err := p.conn.Call(ctx, acp.MethodSessionSteering, acp.NewSteerRequest(agentID, []acp.ContentBlock{{Type: "text", Text: text}}), &res); err != nil {
		fmt.Fprintln(os.Stderr, "agents: steer:", err)
		return
	}
	if res.Outcome == acp.SteerInjected {
		s.mu.Lock()
		s.supersedeLocked()
		s.mu.Unlock()
	}
}

func (a *app) steer(s *session, p *agentProc, rec record, args promptArgs) bool {
	blocks := promptBlocks(a.withImages(rec.ID, args), rec.Worktree, p.caps.PromptCapabilities)
	req := acp.NewSteerRequest(rec.AgentSessionID, slices.Clone(blocks))
	fromTitle := a.senderTitle(args.From)
	if args.From != "" {
		req.Prompt[0].Text = fromPrefix(args.From, fromTitle) + req.Prompt[0].Text
	}
	req.Prompt = sendable(req.Prompt)
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	var res acp.SteerResponse
	if err := p.conn.Call(ctx, acp.MethodSessionSteering, req, &res); err != nil {
		fmt.Fprintln(os.Stderr, "agents: steer:", err)
		return false
	}
	if res.Outcome != acp.SteerInjected {
		return false
	}
	s.mu.Lock()
	s.supersedeLocked()
	s.recordLocked(promptActor(args.From), kindPrompt, promptPayload{PromptRequest: acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: blocks}, From: args.From, FromTitle: fromTitle, Steered: true, Voice: args.Voice, Review: args.Review})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return true
}

func (a *app) runTurn(s *session, turn, epoch int, args promptArgs) {
	for turn > 0 {
		turn, epoch, args = a.playTurn(s, turn, epoch, args)
	}
}

func (a *app) playTurn(s *session, turn, epoch int, args promptArgs) (int, int, promptArgs) {
	p, err := a.attach(s)
	if err == nil && a.hookTurn(s) {
		p, err = a.attach(s)
	}
	if err != nil {
		s.mu.Lock()
		stale := s.attachFailedLocked(epoch)
		s.mu.Unlock()
		if !stale {
			a.fail(s, err.Error())
		}
		return 0, 0, promptArgs{}
	}
	if a.abandon(s, epoch) {
		return 0, 0, promptArgs{}
	}
	rec := s.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	sha, err := turnCheckpoint(ctx, rec, turn)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: checkpoint:", err)
	}
	s.mu.Lock()
	deleted := s.deleted
	if !deleted && s.epoch == epoch {
		s.recordLocked(actorClubmate, kindCheckpoint, checkpointPayload{Turn: turn, Sha: sha})
		s.rec.GateSince = cmp.Or(s.rec.GateSince, sha)
	}
	s.mu.Unlock()
	if deleted {
		ctx, cancel := context.WithTimeout(context.Background(), refsTimeout)
		defer cancel()
		if err := dropCheckpoints(ctx, rec); err != nil {
			fmt.Fprintln(os.Stderr, "agents: delete checkpoints:", err)
		}
		return 0, 0, promptArgs{}
	}
	if a.abandon(s, epoch) {
		return 0, 0, promptArgs{}
	}
	blocks := promptBlocks(a.withImages(rec.ID, args), rec.Worktree, p.caps.PromptCapabilities)
	req := acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: slices.Clone(blocks)}
	fromTitle := a.senderTitle(args.From)
	ctx, cancel = context.WithTimeout(context.Background(), callTimeout)
	policies, told := a.policies(ctx, rec, turn)
	req.Prompt = append(slices.Clip(req.Prompt), policies...)
	cancel()
	if s.isCommand(args.Text) {
		req.Prompt = append(slices.Clip(req.Prompt[1:]), req.Prompt[0])
		if args.From != "" {
			req.Prompt = slices.Insert(req.Prompt, 0, acp.ContentBlock{Type: "text", Text: fromPrefix(args.From, fromTitle)})
		}
	} else if args.From != "" {
		req.Prompt[0].Text = fromPrefix(args.From, fromTitle) + req.Prompt[0].Text
	}
	ctx, cancel = context.WithTimeout(context.Background(), callTimeout)
	a.saveNow(ctx, s)
	cancel()
	parentTitle := a.frameParentTitle(rec)
	time.Sleep(a.sendHold)
	s.mu.Lock()
	if a.stopping() {
		s.mu.Unlock()
		return 0, 0, promptArgs{}
	}
	if s.epoch != epoch || s.deleted || s.rec.closed() || s.cancelRequested {
		s.mu.Unlock()
		a.abandon(s, epoch)
		return 0, 0, promptArgs{}
	}
	if none := s.takeNoneLocked(); none != "" {
		req.Prompt = slices.Insert(slices.Clip(req.Prompt), 0, acp.ContentBlock{Type: "text", Text: none})
	}
	if frame := s.takeFrameLocked(parentTitle); frame != "" {
		req.Prompt = slices.Insert(slices.Clip(req.Prompt), 0, acp.ContentBlock{Type: "text", Text: frame})
	}
	s.recordSentLocked(promptPayload{PromptRequest: acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: blocks}, From: args.From, FromTitle: fromTitle, Voice: args.Voice, Review: args.Review, Gate: cmp.Or(args.Message, doneMessage(args, args.Gate || s.sending != nil && s.sending.Gate))})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	req.Prompt = sendable(req.Prompt)
	var res acp.PromptResponse
	answered := acp.WithAnswered(context.Background(), func() {
		s.mu.Lock()
		if s.epoch == epoch {
			s.answered = true
		}
		s.mu.Unlock()
	})
	err = p.conn.Call(answered, acp.MethodSessionPrompt, req, &res)
	if s.supersededOnly(err) {
		err, res = nil, acp.PromptResponse{StopReason: acp.StopEndTurn}
	}
	if retry, ok := a.retryTurn(s, rec, epoch, err); ok {
		res = acp.PromptResponse{}
		err = p.conn.Call(answered, acp.MethodSessionPrompt, retry, &res)
	}
	if err == nil {
		ctx, cancel = context.WithTimeout(context.Background(), callTimeout)
		a.markTold(ctx, s, told)
		cancel()
	}
	s.mu.Lock()
	s.answered = true
	s.mu.Unlock()
	if errors.Is(err, acp.ErrClosed) {
		go p.stop()
		return 0, 0, promptArgs{}
	}
	if !s.current(epoch) {
		return 0, 0, promptArgs{}
	}
	status := ""
	if err != nil {
		status = failureStatus(err)
		if authErr := a.authRequired(rec.ID, p, err); authErr != nil {
			status = authErr.Error()
		}
	}
	a.endSnapshot(s, rec, turn, sha)
	commits := a.attributeTurn(rec, turn, sha, args.Text)
	a.logUngatedMain(rec, commits)
	a.pruneBackground(s)
	s.mu.Lock()
	cancelled := s.cancelRequested || res.StopReason == acp.StopCancelled
	s.mu.Unlock()
	a.tellFindings(s, cancelled)
	s.mu.Lock()
	ends := err == nil && !s.rec.closed() && (cancelled || len(s.queue) == 0) && s.rec.Status != statusWaiting && len(s.background) == 0 && !s.late
	s.mu.Unlock()
	var plan *donePlan
	if ends {
		p := a.planDone(s, cancelled)
		plan = &p
	}
	a.lockDropped(s)
	if s.epoch != epoch {
		s.mu.Unlock()
		return 0, 0, promptArgs{}
	}
	s.log.flush()
	if len(commits) > 0 {
		s.recordLocked(actorClubmate, kindCommits, commitsPayload{Turn: turn, Commits: commits})
	}
	s.running = false
	s.followedUpLocked()
	edited := s.edited
	var next promptArgs
	nextTurn, nextEpoch, finished, held, settle, stale := 0, 0, false, false, false, false
	cancelled = s.cancelRequested || res.StopReason == acp.StopCancelled
	if cancelled && plan != nil {
		plan = &donePlan{stop: true}
	}
	late := !s.rec.closed() && !cancelled && s.late
	handover := late && (err != nil || len(s.queue) == 0)
	if !late {
		s.refuseCallsLocked()
	}
	prior := s.rec.Held
	switch {
	case s.rec.closed():
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
	case err != nil:
		s.recordLocked(actorClubmate, kindTurnEnd, turnEndPayload{Error: err.Error()})
		s.setStatusLocked(statusError, status)
		if handover {
			s.cycle, s.started = true, time.Now()
		}
	case !cancelled && len(s.queue) > 0:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
		next, _ = s.dequeueLocked()
		nextTurn, nextEpoch = s.beginTurnLocked()
	default:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
		if handover {
			s.cycle, s.started = true, time.Now()
		}
		switch {
		case s.rec.Status == statusWaiting:
			s.ending = &turnOutcome{cancelled: cancelled}
		case s.busyLocked():
			s.setStatusLocked(statusRunning, "")
			s.ending = &turnOutcome{cancelled: cancelled}
		case plan == nil:
			s.ending = &turnOutcome{cancelled: cancelled}
			settle = true
		default:
			held, stale = s.finishLocked(*plan)
			finished = !held
		}
	}
	closed, ended := s.rec.closed(), s.turnLocked()
	s.mu.Unlock()
	a.logRefusals(s)
	if finished {
		a.closeHeldRound(s.doneHold(), prior)
	}
	if !closed && !held && !settle {
		s.turned(ended)
	}
	if err != nil {
		if !a.retryNetwork(s, err) && !closed && !cancelled {
			a.notifyEnd(s, ended)
		}
	} else {
		a.resetNetRetry(rec.ID)
	}
	a.settleLimitWait(s, err, closed || cancelled)
	if finished && !cancelled {
		a.notifyEnd(s, ended)
	}
	s.changed(changeStatus, true)
	if err == nil && !cancelled && !closed {
		a.firstTurnDone(s, turn)
	}
	if edited && rec.Night == "" {
		a.checktime(rec)
	}
	if !finished && !settle && (err != nil || cancelled) {
		a.gateTurnEnded(s, false)
	}
	if finished {
		a.markReview(s)
		a.gateTurnEnded(s, !cancelled)
		a.cleanupChild(s)
		a.closeVanishedChildren(s)
		a.handoffAfter(s, cancelled)
		if plan.release != "" && prior != nil {
			a.logReleased(s, *prior, plan.release)
		}
	}
	if held && !stale {
		a.planned(s, *plan, prior, true, ended, cancelled)
	}
	if settle || stale {
		a.finishTurn(s)
	}
	if nextTurn == 0 && !settle && !stale {
		go a.rehookSession(s)
	}
	return nextTurn, nextEpoch, next
}

func (a *app) lockDropped(s *session) {
	s.mu.Lock()
	if !s.late {
		return
	}
	s.mu.Unlock()
	a.dropEnded(s)
	s.mu.Lock()
}

func (a *app) retryTurn(s *session, rec record, epoch int, err error) (acp.PromptRequest, bool) {
	rpcErr, ok := errors.AsType[*acp.Error](err)
	if !ok || rpcErr.Code != acp.CodeInternal || rec.Night != "" {
		return acp.PromptRequest{}, false
	}
	if f, typed := classifyFailure(err); typed && f.retry != retryInTurn {
		return acp.PromptRequest{}, false
	}
	reason := firstLine(rpcErr.Message)
	s.notice(retryNotice, reason)
	time.Sleep(retryDelay)
	req := acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: []acp.ContentBlock{{Type: "text", Text: fmt.Sprintf(retryFormat, reason)}}}
	s.mu.Lock()
	ok = s.epoch == epoch && !s.cancelRequested && !s.rec.closed() && !s.deleted
	if ok {
		s.answered, s.late = false, false
		s.recordLocked(actorClubmate, kindPrompt, promptPayload{PromptRequest: req, Steered: true})
	}
	s.mu.Unlock()
	if ok {
		s.changed(changeUpdate, true)
	}
	return req, ok
}

func (s *session) attachFailedLocked(epoch int) bool {
	if s.epoch != epoch {
		return true
	}
	s.requeueSendingLocked()
	if s.cancelRequested {
		s.unqueueMessageLocked("")
	}
	return false
}

func (a *app) abandon(s *session, epoch int) bool {
	s.mu.Lock()
	switch {
	case s.epoch != epoch:
		s.mu.Unlock()
		return true
	case s.deleted || s.rec.closed():
		s.running = false
		s.mu.Unlock()
		return true
	case s.cancelRequested:
		s.running = false
		s.requeueSendingLocked()
		s.unqueueMessageLocked("")
		s.recordLocked(actorUser, kindTurnEnd, turnEndPayload{StopReason: acp.StopCancelled})
		prior := s.rec.Held
		stopped := false
		if s.rec.Status == statusWaiting {
			s.ending = &turnOutcome{cancelled: true}
		} else {
			stopped, _ = s.finishLocked(donePlan{stop: true})
		}
		ended := s.turnLocked()
		s.mu.Unlock()
		if !stopped {
			s.turned(ended)
		}
		s.changed(changeStatus, true)
		if stopped && prior != nil {
			a.logHeld(s, *prior, gateStopped, landgate.ByUser)
		}
		return true
	}
	s.mu.Unlock()
	return false
}

func (a *app) checktime(rec record) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := a.host.RunCommand(ctx, checktimeCmd, rec.Workspace, map[string]string{"root": rec.Worktree}, nil); err != nil {
		fmt.Fprintln(os.Stderr, "agents: checktime:", err)
	}
}

func (s *session) cutTurnLocked() {
	s.epoch++
	s.requeueSendingLocked()
}

func (s *session) beginTurnLocked() (int, int) {
	s.epoch++
	s.running = true
	s.cycle = false
	s.cancelRequested = false
	s.started = time.Now()
	s.turn++
	s.stampRecLocked()
	s.superseded = nil
	s.asked = false
	s.ending = nil
	s.answered, s.late = false, false
	if s.sending == nil || !s.sending.Gate {
		if h := s.rec.Held; h != nil && h.holding() {
			s.rec.HeldGate = h.Gate
		}
		s.rec.Held = nil
	}
	if w := s.rec.LimitWait; w != nil && !w.Fired {
		s.rec.LimitWait = nil
	}
	s.settleLocked()
	if s.began != nil {
		s.began(s.rec.ID)
	}
	return s.turn, s.epoch
}

func (a *app) stopping() bool {
	if a.halting.Load() {
		return true
	}
	select {
	case <-a.quit:
		return true
	default:
		return false
	}
}

func (s *session) current(epoch int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.epoch == epoch
}

func (s *session) supersedeLocked() {
	for _, en := range slices.Backward(s.entries) {
		if en.Turn != s.turn {
			break
		}
		var status string
		if en.Kind != entryTool || json.Unmarshal(en.Tool["status"], &status) == nil && (status == "completed" || status == "failed") {
			continue
		}
		if s.superseded == nil {
			s.superseded = map[string]bool{}
		}
		s.superseded[en.ID] = true
	}
}

func (s *session) supersededOnly(err error) bool {
	rpcErr, ok := errors.AsType[*acp.Error](err)
	if !ok || rpcErr.Code != acp.CodeInternal {
		return false
	}
	m := stuckToolsRe.FindStringSubmatch(firstLine(rpcErr.Message))
	if m == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range strings.SplitSeq(m[1], ", ") {
		if !s.superseded[id] {
			return false
		}
	}
	return true
}

func (a *app) senderTitle(id string) string {
	if id == "" || id == unknownCaller {
		return ""
	}
	if s := a.session(id); s != nil {
		return s.snapshot().Title
	}
	return ""
}

func fromPrefix(id, title string) string {
	if id == unknownCaller {
		return fromUnknown
	}
	return fmt.Sprintf(fromFormat, title, id)
}

func promptActor(from string) string {
	if from == "" {
		return actorUser
	}
	return "agent:" + from
}

func promptBlocks(args promptArgs, root string, caps acp.PromptCapabilities) []acp.ContentBlock {
	blocks := []acp.ContentBlock{{Type: "text", Text: args.Text}}
	for _, att := range args.Attachments {
		path := att.Path
		if path != "" && !filepath.IsAbs(path) {
			path = filepath.Join(root, path)
		}
		uri := (&url.URL{Scheme: "file", Path: path}).String()
		switch att.Kind {
		case "image":
			source := att.Source != "" && isFile(att.Source)
			if caps.Image {
				blocks = append(blocks, acp.ContentBlock{Type: "image", Data: att.Data, MimeType: att.MimeType})
			}
			if source {
				blocks = append(blocks, acp.ContentBlock{Type: "resource_link", URI: (&url.URL{Scheme: "file", Path: att.Source}).String(), Name: filepath.Base(att.Source)})
			}
			if !caps.Image && !source {
				blocks = append(blocks, acp.ContentBlock{Type: "text", Text: imageOmitted})
			}
		case "file":
			if caps.EmbeddedContext {
				if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() <= embedLimit {
					if data, err := os.ReadFile(path); err == nil && utf8.Valid(data) {
						blocks = append(blocks, acp.ContentBlock{Type: "resource", Resource: &acp.TextResourceContents{URI: uri, Text: string(data)}})
						continue
					}
				}
			}
			blocks = append(blocks, acp.ContentBlock{Type: "resource_link", URI: uri, Name: filepath.Base(path)})
		case "selection":
			lines := strconv.Itoa(att.StartLine) + "-" + strconv.Itoa(att.EndLine)
			if caps.EmbeddedContext {
				blocks = append(blocks, acp.ContentBlock{Type: "resource", Resource: &acp.TextResourceContents{URI: uri + "#L" + lines, Text: att.Text}})
				continue
			}
			blocks = append(blocks, acp.ContentBlock{Type: "text", Text: path + ":" + lines + "\n```\n" + att.Text + "\n```"})
		case "review", "page":
			blocks = append(blocks, acp.ContentBlock{Type: "text", Text: att.Text})
		}
	}
	return blocks
}

func (s *session) isCommand(text string) bool {
	words := strings.Fields(text)
	if len(words) == 0 {
		return false
	}
	name, ok := strings.CutPrefix(words[0], "/")
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	return name != "" && slices.ContainsFunc(s.commands, func(c acp.AvailableCommand) bool { return c.Name == name })
}

func sendable(blocks []acp.ContentBlock) []acp.ContentBlock {
	return slices.DeleteFunc(slices.Clone(blocks), func(b acp.ContentBlock) bool { return b.Type == "text" && b.Text == "" })
}
