package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/pluginsdk"
)

const (
	limitResumeCmd   = "agents.limit-resume"
	limitCancelCmd   = "agents.limit-cancel"
	cronAddCmd       = "cron.add"
	cronListCmd      = "cron.list"
	cronDeleteCmd    = "cron.delete"
	limitJobName     = "Continue sessions after the usage limit"
	limitMargin      = 90 * time.Second
	limitDueSlack    = 5 * time.Second
	limitMarginEnv   = "CLUBMATE_AGENTS_LIMIT_MARGIN"
	limitMaxAttempts = 3
	limitLayout      = "15:04"
	limitDayLayout   = "Mon"
	limitText        = "[Clubmate] Your usage limit has reset. Continue the task from where you stopped."
	limitNotice      = "Stopped by the usage limit; continuing at %s"
	limitGiveUp      = "Stopped by the usage limit again; stopped waiting"
	limitGiveUpHint  = "send a message to continue"
	limitNoJob       = "Stopped by the usage limit; could not schedule the continue"
	limitWord        = "resumes %s"
	limitLine        = "Stopped by the usage limit · resumes %s"
)

var (
	limitTextPrefixes = []string{"You've hit your", "You've reached your"}
	limitResetRe      = regexp.MustCompile(`resets (?:([A-Z][a-z]{2}) (\d{1,2}) at )?(\d{1,2})(?::(\d{2}))?\s?(am|pm)(?: \(([^)]+)\))?`)
)

type limitWait struct {
	At       time.Time `json:"at"`
	Job      string    `json:"job,omitempty"`
	Attempts int       `json:"attempts,omitempty"`
	Fired    bool      `json:"fired,omitempty"`
}

type limitJobs struct {
	mu   sync.Mutex
	byAt map[string]string
}

type limitResumeArgs struct {
	At string `json:"at"`
}

type limitJobArgs struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Args    string `json:"args"`
	At      string `json:"at"`
}

type cronJobRef struct {
	ID      string          `json:"id"`
	Command string          `json:"command"`
	Args    json.RawMessage `json:"args"`
}

func limitReset(f turnFailure, usage usageState, now time.Time) (time.Time, bool) {
	if f.kind != failureQuota || !slices.ContainsFunc(limitTextPrefixes, func(p string) bool { return strings.HasPrefix(f.title, p) }) {
		return time.Time{}, false
	}
	if at, ok := parseLimitReset(f.title, now); ok {
		return at, true
	}
	var latest time.Time
	for _, w := range usage.Windows {
		at := time.Unix(w.ResetsAt, 0)
		if (w.Status == "rejected" || w.Used >= 100) && at.After(now) && at.After(latest) {
			latest = at
		}
	}
	return latest, !latest.IsZero()
}

func parseLimitReset(text string, now time.Time) (time.Time, bool) {
	m := limitResetRe.FindStringSubmatch(text)
	if m == nil {
		return time.Time{}, false
	}
	loc := time.Local
	if m[6] != "" {
		l, err := time.LoadLocation(m[6])
		if err != nil {
			return time.Time{}, false
		}
		loc = l
	}
	hour, _ := strconv.Atoi(m[3])
	minute, _ := strconv.Atoi(m[4])
	if hour < 1 || hour > 12 || minute > 59 {
		return time.Time{}, false
	}
	hour %= 12
	if m[5] == "pm" {
		hour += 12
	}
	local := now.In(loc)
	if m[1] == "" {
		at := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
		if !at.After(now) {
			at = at.AddDate(0, 0, 1)
		}
		return at, true
	}
	month, err := time.Parse("Jan", m[1])
	if err != nil {
		return time.Time{}, false
	}
	day, _ := strconv.Atoi(m[2])
	at := time.Date(local.Year(), month.Month(), day, hour, minute, 0, 0, loc)
	if at.Before(now.AddDate(0, 0, -1)) {
		at = at.AddDate(1, 0, 0)
	}
	return at, true
}

func (a *app) settleLimitWait(s *session, err error, stopped bool) {
	rec := s.snapshot()
	prev := rec.LimitWait
	f, typed := classifyFailure(err)
	reset, ok := time.Time{}, false
	if err != nil && typed && !stopped && rec.Night == "" {
		reset, ok = limitReset(f, a.usage.snapshot(), time.Now())
	}
	if !ok {
		if prev != nil {
			a.dropLimitWait(s, prev)
		}
		return
	}
	attempts := 1
	if prev != nil && prev.Fired {
		attempts = prev.Attempts + 1
	}
	if attempts > limitMaxAttempts {
		a.dropLimitWait(s, prev)
		s.notice(limitGiveUp, limitGiveUpHint)
		return
	}
	wait := &limitWait{At: reset.Add(envDelay(limitMarginEnv, limitMargin)), Attempts: attempts}
	s.mu.Lock()
	s.rec.LimitWait = wait
	s.mu.Unlock()
	s.notice(fmt.Sprintf(limitNotice, limitClock(wait.At, time.Now())), "")
	go a.scheduleLimitJob(s, wait)
}

func (a *app) scheduleLimitJob(s *session, wait *limitWait) {
	workspace := s.snapshot().Workspace
	if now := time.Now(); !wait.At.After(now.Add(limitDueSlack)) {
		a.continueLimited(workspace, now)
		return
	}
	job, err := a.limitJob(workspace, wait.At)
	s.mu.Lock()
	current := s.rec.LimitWait == wait
	if current && err == nil {
		next := *wait
		next.Job = job
		s.rec.LimitWait = &next
	}
	if current && err != nil {
		s.rec.LimitWait = nil
	}
	s.mu.Unlock()
	if !current {
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: limit job:", err)
		s.notice(limitNoJob, err.Error())
		s.changed(changeStatus, true)
		return
	}
	s.changed(changeUpdate, true)
}

func (a *app) dropLimitWait(s *session, wait *limitWait) {
	s.mu.Lock()
	if s.rec.LimitWait == wait {
		s.rec.LimitWait = nil
	}
	s.mu.Unlock()
	if wait != nil && !wait.Fired && wait.Job != "" {
		go a.releaseLimitJob(s.snapshot().Workspace, wait.Job)
	}
}

func (a *app) limitJob(workspace string, at time.Time) (string, error) {
	key := workspace + "/" + strconv.FormatInt(at.Unix(), 10)
	j := &a.limitJobs
	j.mu.Lock()
	defer j.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	jobs, err := a.limitJobsIn(ctx, workspace)
	if err != nil {
		return "", err
	}
	if id, ok := j.byAt[key]; ok && slices.ContainsFunc(jobs, func(c cronJobRef) bool { return c.ID == id }) {
		return id, nil
	}
	want := at.UTC().Format(time.RFC3339)
	if i := slices.IndexFunc(jobs, func(c cronJobRef) bool { return limitJobAt(c) == want }); i >= 0 {
		j.remember(key, jobs[i].ID)
		return jobs[i].ID, nil
	}
	args, _ := json.Marshal(limitResumeArgs{At: want})
	var job cronJobRef
	if err := a.host.RunCommand(ctx, cronAddCmd, workspace, limitJobArgs{Name: limitJobName, Command: limitResumeCmd, Args: string(args), At: want}, &job); err != nil {
		return "", err
	}
	j.remember(key, job.ID)
	return job.ID, nil
}

func (j *limitJobs) remember(key, id string) {
	if j.byAt == nil {
		j.byAt = map[string]string{}
	}
	j.byAt[key] = id
}

func (a *app) limitJobsIn(ctx context.Context, workspace string) ([]cronJobRef, error) {
	var out struct {
		Jobs []cronJobRef `json:"jobs"`
	}
	if err := a.host.RunCommand(ctx, cronListCmd, workspace, struct{}{}, &out); err != nil {
		return nil, err
	}
	return slices.DeleteFunc(out.Jobs, func(c cronJobRef) bool { return c.Command != limitResumeCmd }), nil
}

func limitJobAt(c cronJobRef) string {
	var args limitResumeArgs
	_ = json.Unmarshal(c.Args, &args)
	return args.At
}

func (a *app) releaseLimitJob(workspace, job string) {
	for _, s := range a.workspaceSessions(workspace) {
		if w := s.snapshot().LimitWait; w != nil && w.Job == job && !w.Fired {
			return
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if err := a.host.RunCommand(ctx, cronDeleteCmd, workspace, map[string]string{"id": job}, nil); err != nil {
		fmt.Fprintln(os.Stderr, "agents: delete limit job:", err)
	}
}

func (a *app) limitResumeCommand(_ context.Context, cmd pluginsdk.CommandRun, _ limitResumeArgs) (any, error) {
	return map[string]int{"continued": a.continueLimited(cmd.WorkspaceID, time.Now())}, nil
}

func (a *app) continueLimited(workspace string, now time.Time) int {
	n := 0
	for _, s := range a.workspaceSessions(workspace) {
		s.mu.Lock()
		w := s.rec.LimitWait
		due := w != nil && !w.Fired && !w.At.After(now.Add(limitDueSlack))
		if !due {
			s.mu.Unlock()
			continue
		}
		waiting := s.rec.Status == statusError && !s.rec.closed() && !s.running && !s.deleted
		if waiting {
			fired := *w
			fired.Fired = true
			s.rec.LimitWait = &fired
		} else {
			s.rec.LimitWait = nil
		}
		s.mu.Unlock()
		s.changed(changeStatus, true)
		if !waiting {
			continue
		}
		if !a.submitIdle(s, promptArgs{Text: limitText}) {
			a.dropLimitWait(s, s.snapshot().LimitWait)
			s.changed(changeStatus, true)
			continue
		}
		n++
	}
	return n
}

func (a *app) limitCancelCommand(_ context.Context, _ pluginsdk.CommandRun, s *session) (any, error) {
	a.cancelLimitWait(s)
	return nil, nil
}

func (a *app) cancelLimitWait(s *session) {
	w := s.snapshot().LimitWait
	if w == nil {
		return
	}
	a.dropLimitWait(s, w)
	s.changed(changeStatus, true)
}

func (a *app) reconcileLimitWaits() {
	now := time.Now()
	due := map[string]bool{}
	ahead := map[string][]*session{}
	for _, s := range a.allSessions() {
		rec := s.snapshot()
		w := rec.LimitWait
		switch {
		case w == nil || w.Fired || rec.closed():
		case !w.At.After(now.Add(limitDueSlack)):
			due[rec.Workspace] = true
		default:
			ahead[rec.Workspace] = append(ahead[rec.Workspace], s)
		}
	}
	for workspace := range due {
		a.continueLimited(workspace, now)
	}
	for workspace, list := range ahead {
		for _, s := range list {
			w := s.snapshot().LimitWait
			if w == nil || w.Job != "" {
				continue
			}
			job, err := a.limitJob(workspace, w.At)
			if err != nil {
				fmt.Fprintln(os.Stderr, "agents: limit job:", err)
				continue
			}
			s.mu.Lock()
			if s.rec.LimitWait == w {
				next := *w
				next.Job = job
				s.rec.LimitWait = &next
			}
			s.mu.Unlock()
			s.changed(changeUpdate, true)
		}
	}
}

func limitBadge(w *limitWait) string {
	return fmt.Sprintf(limitWord, limitClock(w.At, time.Now()))
}

func limitClock(at, now time.Time) string {
	if at.Sub(now) < 24*time.Hour {
		return at.Local().Format(limitLayout)
	}
	return at.Local().Format(limitDayLayout)
}

func (w *limitWait) pausing() bool {
	return w != nil && !w.Fired
}
