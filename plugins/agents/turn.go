package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"clubmate/plugins/agents/acp"
)

const (
	checktimeCmd = "editor-nvim.checktime"
	embedLimit   = 1 << 20
	imageOmitted = "[An image was attached, but this agent does not accept images.]"
	retryDelay   = 3 * time.Second
	retryNotice  = "The turn failed; retrying once"
	retryFormat  = "[Clubmate] Your previous reply ended with an error (%s). Continue the task from where you stopped."
	fromFormat   = "[From Clubmate agent session \"%s\" (%s), not from the user]\n\n"
	fromUnknown  = "[From another agent session, not from the user]\n\n"
)

type runState struct {
	proc            *agentProc
	running         bool
	cancelRequested bool
	prior           string
	started         time.Time
	perms           map[string]*pendingPermission
	queue           []queuedPrompt
	queueSeq        int
}

func (a *app) prompt(projectID string, args promptArgs) error {
	s, err := a.lookup(projectID, args.Session)
	if err != nil {
		return err
	}
	if args.From == "" || s.snapshot().ParentSession != args.From {
		return a.submit(s, &args)
	}
	a.spawnMu.Lock()
	defer a.spawnMu.Unlock()
	if st := s.snapshot().Status; st != statusRunning && st != statusWaiting && a.runningChildren(args.From) >= maxChildren {
		return errChildLimit
	}
	return a.submit(s, &args)
}

func (a *app) submit(s *session, args *promptArgs) error {
	s.mu.Lock()
	s.loadLocked()
	if args != nil && s.running && !s.cancelRequested && !s.rec.Closed && s.proc != nil && s.proc.steering {
		p, rec := s.proc, s.rec
		s.mu.Unlock()
		if a.steer(s, p, rec, *args) {
			return nil
		}
		s.mu.Lock()
	}
	switch {
	case s.rec.Closed:
		s.mu.Unlock()
		return errClosed
	case s.running && args == nil:
		s.mu.Unlock()
		return nil
	case s.running:
		s.enqueueLocked(*args)
		s.mu.Unlock()
		s.changed(changeUpdate, false)
		a.wakeWaiters()
		return nil
	}
	if args != nil {
		s.enqueueLocked(*args)
	}
	next, ok := s.dequeueLocked()
	if !ok {
		s.mu.Unlock()
		return nil
	}
	turn := s.beginTurnLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	go a.runTurn(s, turn, next)
	return nil
}

func (a *app) cancel(s *session) {
	s.mu.Lock()
	p, running, agentID := s.proc, s.running, s.rec.AgentSessionID
	if running {
		s.cancelRequested = true
		s.recordLocked(actorUser, kindCancel, acp.SessionRequest{SessionID: agentID})
	}
	s.mu.Unlock()
	if p != nil && running {
		if err := p.conn.Notify(acp.MethodSessionCancel, acp.SessionRequest{SessionID: agentID}); err != nil {
			fmt.Fprintln(os.Stderr, "agents: cancel:", err)
		}
	}
	s.mu.Lock()
	s.cancelPermissionsLocked()
	s.mu.Unlock()
	s.changed(changeUpdate, false)
}

func (a *app) steer(s *session, p *agentProc, rec record, args promptArgs) bool {
	blocks := promptBlocks(args, rec.Worktree, p.caps.PromptCapabilities)
	req := acp.NewSteerRequest(rec.AgentSessionID, slices.Clone(blocks))
	if args.From != "" {
		req.Prompt[0].Text = a.fromPrefix(args.From) + req.Prompt[0].Text
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
	s.recordLocked(promptActor(args.From), kindPrompt, promptPayload{PromptRequest: acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: blocks}, From: args.From, Steered: true})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	return true
}

func (a *app) runTurn(s *session, turn int, args promptArgs) {
	for turn > 0 {
		turn, args = a.playTurn(s, turn, args)
	}
}

func (a *app) playTurn(s *session, turn int, args promptArgs) (int, promptArgs) {
	p, err := a.attach(s)
	if err != nil {
		s.mu.Lock()
		s.requeueLocked(args)
		s.mu.Unlock()
		a.fail(s, err.Error())
		return 0, promptArgs{}
	}
	if a.abandon(s, args) {
		return 0, promptArgs{}
	}
	rec := s.snapshot()
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	sha, err := checkpoint(ctx, rec.Worktree, rec.ID, turn)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: checkpoint:", err)
	}
	s.mu.Lock()
	deleted := s.deleted
	if !deleted {
		s.recordLocked(actorClubmate, kindCheckpoint, checkpointPayload{Turn: turn, Sha: sha})
	}
	s.mu.Unlock()
	if deleted {
		ctx, cancel := context.WithTimeout(context.Background(), refsTimeout)
		defer cancel()
		if err := deleteCheckpoints(ctx, rec.Worktree, rec.ID); err != nil {
			fmt.Fprintln(os.Stderr, "agents: delete checkpoints:", err)
		}
		return 0, promptArgs{}
	}
	if a.abandon(s, args) {
		return 0, promptArgs{}
	}
	req := acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: promptBlocks(args, rec.Worktree, p.caps.PromptCapabilities)}
	s.mu.Lock()
	s.recordLocked(promptActor(args.From), kindPrompt, promptPayload{PromptRequest: req, From: args.From})
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	ctx, cancel = context.WithTimeout(context.Background(), callTimeout)
	req.Prompt = append(slices.Clip(req.Prompt), a.policies(ctx, rec)...)
	cancel()
	if args.From != "" {
		req.Prompt[0].Text = a.fromPrefix(args.From) + req.Prompt[0].Text
	}
	req.Prompt = sendable(req.Prompt)
	var res acp.PromptResponse
	err = p.conn.Call(context.Background(), acp.MethodSessionPrompt, req, &res)
	if retry, ok := a.retryTurn(s, rec, err); ok {
		res = acp.PromptResponse{}
		err = p.conn.Call(context.Background(), acp.MethodSessionPrompt, retry, &res)
	}
	if errors.Is(err, acp.ErrClosed) {
		go p.stop()
		return 0, promptArgs{}
	}
	a.endSnapshot(s, rec, turn, sha)
	commits := a.attributeTurn(rec, turn, sha, args.Text)
	s.mu.Lock()
	s.log.flush()
	if len(commits) > 0 {
		s.recordLocked(actorClubmate, kindCommits, commitsPayload{Turn: turn, Commits: commits})
	}
	s.running = false
	edited := s.edited
	var next promptArgs
	nextTurn, finished := 0, false
	switch {
	case s.rec.Closed:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
	case err != nil:
		s.recordLocked(actorClubmate, kindTurnEnd, turnEndPayload{Error: err.Error()})
		s.setStatusLocked(statusError, err.Error())
	case !s.cancelRequested && res.StopReason != acp.StopCancelled && len(s.queue) > 0:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
		next, _ = s.dequeueLocked()
		nextTurn = s.beginTurnLocked()
	default:
		s.recordLocked(s.actor(), kindTurnEnd, turnEndPayload{StopReason: res.StopReason})
		if s.rec.Status == statusWaiting {
			s.prior = statusDone
		} else {
			s.setStatusLocked(statusDone, "")
			finished = true
		}
	}
	closed, ended := s.rec.Closed, s.turnLocked()
	s.mu.Unlock()
	if !closed {
		s.turned(ended)
	}
	s.changed(changeStatus, true)
	if edited {
		a.checktime(rec)
	}
	if finished {
		a.cleanupChild(s)
		a.closeVanishedChildren(s)
	}
	return nextTurn, next
}

func (a *app) retryTurn(s *session, rec record, err error) (acp.PromptRequest, bool) {
	rpcErr, ok := errors.AsType[*acp.Error](err)
	if !ok || rpcErr.Code != acp.CodeInternal {
		return acp.PromptRequest{}, false
	}
	reason := firstLine(rpcErr.Message)
	s.notice(retryNotice, reason)
	time.Sleep(retryDelay)
	req := acp.PromptRequest{SessionID: rec.AgentSessionID, Prompt: []acp.ContentBlock{{Type: "text", Text: fmt.Sprintf(retryFormat, reason)}}}
	s.mu.Lock()
	ok = !s.cancelRequested && !s.rec.Closed && !s.deleted
	if ok {
		s.recordLocked(actorClubmate, kindPrompt, promptPayload{PromptRequest: req, Steered: true})
	}
	s.mu.Unlock()
	if ok {
		s.changed(changeUpdate, true)
	}
	return req, ok
}

func (a *app) abandon(s *session, args promptArgs) bool {
	s.mu.Lock()
	switch {
	case s.deleted || s.rec.Closed:
		s.running = false
		s.mu.Unlock()
		return true
	case s.cancelRequested:
		s.running = false
		s.requeueLocked(args)
		s.recordLocked(actorUser, kindTurnEnd, turnEndPayload{StopReason: acp.StopCancelled})
		if s.rec.Status == statusWaiting {
			s.prior = statusDone
		} else {
			s.setStatusLocked(statusDone, "")
		}
		ended := s.turnLocked()
		s.mu.Unlock()
		s.turned(ended)
		s.changed(changeStatus, true)
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

func (s *session) beginTurnLocked() int {
	s.running = true
	s.cancelRequested = false
	s.started = time.Now()
	s.turn++
	s.settleLocked()
	return s.turn
}

func (a *app) fromPrefix(id string) string {
	if id == unknownCaller {
		return fromUnknown
	}
	var title string
	if s := a.session(id); s != nil {
		title = s.snapshot().Title
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
			if caps.Image {
				blocks = append(blocks, acp.ContentBlock{Type: "image", Data: att.Data, MimeType: att.MimeType})
				continue
			}
			blocks = append(blocks, acp.ContentBlock{Type: "text", Text: imageOmitted})
		case "file":
			if caps.EmbeddedContext {
				if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Size() <= embedLimit {
					if data, err := os.ReadFile(path); err == nil {
						blocks = append(blocks, acp.ContentBlock{Type: "resource", Resource: &acp.TextResourceContents{URI: uri, Text: strings.ToValidUTF8(string(data), "")}})
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
		}
	}
	return blocks
}

func sendable(blocks []acp.ContentBlock) []acp.ContentBlock {
	return slices.DeleteFunc(slices.Clone(blocks), func(b acp.ContentBlock) bool { return b.Type == "text" && b.Text == "" })
}
