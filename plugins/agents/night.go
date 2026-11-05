package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"clubmate/pkg/pluginsdk"
	"clubmate/plugins/agents/acp"
)

const (
	nightStartHour = 4
	nightWindow    = 4 * time.Hour
	nightTick      = time.Minute

	nightSessionLimit    = 30 * time.Minute
	nightLimit           = 60 * time.Minute
	nightSessionLimitEnv = "CLUBMATE_AGENTS_NIGHT_SESSION_LIMIT"
	nightLimitEnv        = "CLUBMATE_AGENTS_NIGHT_LIMIT"

	selfLastRunKey       = "self.lastRun"
	selfDigestedKey      = "self.digestedUntil"
	selfNightSessionsKey = "self.nightSessions"

	nightRoot        = "night"
	nightDigestFile  = "digest.md"
	rejectedFile     = "rejected.md"
	rejectedNone     = "Nothing undone yet.\n"
	rejectedLimit    = 10
	nightDigestDir   = "digest"
	nightEndedEarly  = "the night ended early: "
	sleepTitle       = "Sleep: "
	gitExcludeFile   = "info/exclude"
	gitSparseFile    = "info/sparse-checkout"
	selfSparse       = "/*\n!/" + selfPatchesDir + "/\n!/" + reportDir + "/\n"
	nightRecentLimit = 10
	nightRecentID    = 12
	nightNoChanges   = "none"
)

var (
	errSelfOff      = errors.New("self-improvement is off")
	errNightBusy    = errors.New("a night is already running")
	errNightFailed  = errors.New("a night session failed")
	errNightLimit   = errors.New("a night session reached its time limit")
	errNightExpired = errors.New("the night reached its time limit")
	errNightQuit    = errors.New("the plugin is stopping")
	errNightRestart = errors.New("Clubmate restarted during the night")
	errMemoryStuck  = errors.New("applied, not undoable")
	errNightApplied = errors.New("this night's changes are already applied; nothing would apply a new one")
)

type nightDay struct {
	pluginsdk.Workspace
	digest string
}

type memorySaver func(ctx context.Context, patch []byte, dirs map[string]string, summary string) (selfChange, error)

type nightLedger struct {
	Running  bool     `json:"running"`
	Sessions []string `json:"sessions"`
	Base     string   `json:"base,omitempty"`
	Date     string   `json:"date,omitempty"`
}

func nightDue(now, last time.Time) bool {
	start := time.Date(now.Year(), now.Month(), now.Day(), nightStartHour, 0, 0, 0, now.Location())
	return !now.Before(start) && now.Before(start.Add(nightWindow)) && last.Before(start)
}

func (a *app) nightLoop() {
	ticker := time.NewTicker(nightTick)
	defer ticker.Stop()
	for {
		select {
		case <-a.quit:
			return
		case <-ticker.C:
			a.nightTick()
		}
	}
}

func (a *app) runNight(ctx context.Context) error {
	if err := a.claimNight(ctx); err != nil {
		return err
	}
	defer a.nightMu.Unlock()
	return a.night(ctx)
}

func (a *app) nightRunCommand(ctx context.Context, _ pluginsdk.CommandRun) (any, error) {
	if err := a.claimNight(ctx); err != nil {
		return nil, err
	}
	go func() {
		defer a.nightMu.Unlock()
		if err := a.night(context.Background()); err != nil {
			fmt.Fprintln(os.Stderr, "agents: night:", err)
		}
	}()
	return nil, nil
}

func (a *app) nightTick() {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if !a.selfEnabled(ctx) || !nightDue(time.Now(), a.lastNight(ctx)) {
		return
	}
	if err := a.runNight(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night:", err)
	}
}

func (a *app) nightOff(ctx context.Context) error {
	if ctx.Err() != nil {
		return errNightExpired
	}
	if !a.selfEnabled(ctx) {
		return errSelfOff
	}
	return nil
}

func (a *app) claimNight(ctx context.Context) error {
	if !a.nightMu.TryLock() {
		return errNightBusy
	}
	if !a.selfEnabled(ctx) {
		a.nightMu.Unlock()
		return errSelfOff
	}
	return nil
}

func (a *app) night(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, envDelay(nightLimitEnv, nightLimit))
	defer cancel()
	now := time.Now()
	if err := a.host.StorageSet(ctx, selfLastRunKey, now); err != nil {
		return err
	}
	workspaces, err := a.nightWorkspaces(ctx)
	if err != nil {
		return err
	}
	days := a.nightDays(workspaces, a.digestedUntil(ctx, now))
	if len(days) == 0 {
		return a.host.StorageSet(ctx, selfDigestedKey, now)
	}
	date := now.Format(time.DateOnly)
	if err := a.clearNight(ctx, date); err != nil {
		return err
	}
	var res nightResult
	var host *session
	err = a.sleepAll(ctx, days, date, &res)
	if err == nil {
		host, err = a.improve(ctx, workspaces[0].ID, days, &res)
	}
	if err != nil {
		res.Skipped = append(res.Skipped, nightEndedEarly+oneLine(err.Error()))
	}
	if errors.Is(err, errNightQuit) {
		return err
	}
	closing := context.WithoutCancel(parent)
	a.reportNight(closing, date, workspaces[0].ID, host, res, err)
	a.nightOver(closing)
	if err != nil {
		return err
	}
	return a.host.StorageSet(closing, selfDigestedKey, now)
}

func (a *app) sleepAll(ctx context.Context, days []nightDay, date string, res *nightResult) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, d := range days {
		dirs := slices.DeleteFunc(memoryDirs(d.Workspace, home), func(dir string) bool { return seen[dir] })
		if len(dirs) == 0 {
			continue
		}
		for _, dir := range dirs {
			seen[dir] = true
		}
		if err := a.sleep(ctx, d, dirs, date, res); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) sleep(ctx context.Context, d nightDay, dirs []string, date string, res *nightResult) error {
	if err := a.nightOff(ctx); err != nil {
		return err
	}
	dir := a.nightPath(nightSleep + "-" + d.ID)
	defer a.removeSandbox(dir)
	rejected, err := a.self.rejectedMemory(ctx, d.ID)
	if err != nil {
		return err
	}
	m, err := memoryForNight(ctx, dir, dirs, d.digest, rejected)
	if err != nil {
		return err
	}
	roots := d.Roots()
	s, err := a.nightTurn(ctx, nightSpec{Kind: nightSleep, Workspace: d.ID, Dir: dir, Reads: roots, Title: sleepTitle + d.Name, Prompt: fmt.Sprintf(sleepPrompt, strings.Join(roots, ", ")), System: nightSystem})
	if s != nil {
		res.Refused += s.snapshot().Refused
		defer a.closeNight(s)
	}
	if err != nil {
		return skipSlow(err, d.Name, res)
	}
	landing := context.WithoutCancel(ctx)
	if !a.selfEnabled(landing) {
		return errSelfOff
	}
	applySleep(landing, func(ctx context.Context, patch []byte, dirs map[string]string, summary string) (selfChange, error) {
		return a.self.saveMemoryPatch(ctx, date, d.ID, patch, dirs, summary)
	}, m, d.Name, res)
	return nil
}

func (a *app) improve(ctx context.Context, workspace string, days []nightDay, res *nightResult) (*session, error) {
	if err := a.nightOff(ctx); err != nil {
		return nil, err
	}
	dir := a.nightPath(nightImprove)
	recent, err := a.self.recent(ctx)
	if err == nil {
		err = a.improveSandbox(ctx, dir, days)
	}
	if err != nil {
		a.removeSandbox(dir)
		return nil, err
	}
	s, err := a.nightTurn(ctx, nightSpec{Kind: nightImprove, Workspace: workspace, Dir: dir, Title: nightTitle, Prompt: fmt.Sprintf(improvePrompt, recent), System: nightSystem})
	if s != nil {
		res.Refused += s.snapshot().Refused
	}
	landing := context.WithoutCancel(ctx)
	if err == nil && !a.selfEnabled(landing) {
		err = errSelfOff
	}
	if err != nil {
		a.dropImprove(s, dir)
		return s, skipSlow(err, nightTitle, res)
	}
	return s, a.applyImprove(landing, dir, res)
}

func (a *app) applyImprove(ctx context.Context, dir string, res *nightResult) error {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	got, err := a.self.apply(ctx, dir)
	res.Applied = append(res.Applied, got)
	a.addDiffs(ctx, got.Changes, res)
	return err
}

func (a *app) addDiffs(ctx context.Context, changes []selfChange, res *nightResult) {
	for _, c := range changes {
		diff, err := a.self.changeDiff(ctx, c.ID)
		if err != nil {
			fmt.Fprintln(os.Stderr, "agents: night diff:", err)
			continue
		}
		if res.Diffs == nil {
			res.Diffs = map[string]string{}
		}
		res.Diffs[c.ID] = diff
	}
}

func (a *app) improveSandbox(ctx context.Context, dir string, days []nightDay) error {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	if err := a.self.clone(ctx, dir); err != nil {
		return err
	}
	taken := map[string]bool{}
	for _, d := range days {
		name := strings.ReplaceAll(d.Name, "/", "-")
		if name == "" || taken[name] {
			name += "-" + d.ID
		}
		taken[name] = true
		if err := writeAtomic(filepath.Join(dir, nightDigestDir, name+memoryExt), []byte(d.digest)); err != nil {
			return err
		}
	}
	return excludeFromSandbox(dir, nightDigestDir)
}

func (x *selfStore) clone(ctx context.Context, dir string) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	args := []string{"clone", "-q", "--template=", "--separate-git-dir=" + sandboxGitDir(dir), x.dir, dir}
	if _, err := x.git(ctx, args...); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, gitDirName)); err != nil {
		return err
	}
	return hidePatches(ctx, dir)
}

func (x *selfStore) recent(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	list, err := x.changes(ctx, selfChangesLimit)
	if err != nil {
		return "", err
	}
	list = slices.DeleteFunc(list, func(c selfChange) bool { return c.Undone || !slices.Contains(revertKinds, c.Kind) })
	if len(list) == 0 {
		return nightNoChanges, nil
	}
	lines := make([]string, 0, nightRecentLimit)
	for _, c := range list[:min(nightRecentLimit, len(list))] {
		lines = append(lines, c.ID[:min(nightRecentID, len(c.ID))]+" "+c.Summary)
	}
	return strings.Join(lines, "; "), nil
}

func (a *app) nightTurn(ctx context.Context, spec nightSpec) (*session, error) {
	s, err := a.spawnNight(spec)
	if s == nil {
		return nil, err
	}
	a.noteNight(ctx, s)
	if err != nil {
		a.sealNight(s)
		return s, err
	}
	return s, a.awaitNight(ctx, s)
}

func (a *app) awaitNight(ctx context.Context, s *session) error {
	defer a.sealNight(s)
	timer := time.NewTimer(envDelay(nightSessionLimitEnv, nightSessionLimit))
	defer timer.Stop()
	for {
		woken := a.statusWake()
		r := s.waitResult()
		switch {
		case r.Status == statusError, r.Status == statusClosed:
			return fmt.Errorf("%w: %s", errNightFailed, r.Status+" "+r.Error)
		case r.Status == statusDone && r.StopReason == acp.StopCancelled:
			return fmt.Errorf("%w: %s", errNightFailed, acp.StopCancelled)
		case r.Status == statusDone:
			return nil
		}
		select {
		case <-woken:
		case <-timer.C:
			a.cancel(s)
			return errNightLimit
		case <-ctx.Done():
			a.cancel(s)
			return errNightExpired
		case <-a.quit:
			return errNightQuit
		}
	}
}

func (a *app) sealNight(s *session) {
	a.declareMu.Lock()
	defer a.declareMu.Unlock()
	s.mu.Lock()
	s.rec.NightSealed = true
	s.mu.Unlock()
	s.changed(changeUpdate, true)
}

func (a *app) closeNight(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	if !a.closeIdle(ctx, s) {
		a.closeSession(ctx, s, false)
	}
}

func (a *app) dropImprove(s *session, dir string) {
	if s != nil {
		a.closeNight(s)
	}
	a.removeSandbox(dir)
}

func (a *app) nightWorkspaces(ctx context.Context) ([]pluginsdk.Workspace, error) {
	list, err := a.host.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(list, func(p pluginsdk.Workspace) bool { return p.Home }), nil
}

func (a *app) nightDays(workspaces []pluginsdk.Workspace, since time.Time) []nightDay {
	var days []nightDay
	for _, p := range workspaces {
		if digest := a.digest(p.ID, since); digest != "" {
			days = append(days, nightDay{Workspace: p, digest: digest})
		}
	}
	return days
}

func (a *app) digestedUntil(ctx context.Context, now time.Time) time.Time {
	var until time.Time
	if _, err := a.host.StorageGet(ctx, selfDigestedKey, &until); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night digest start:", err)
	}
	if until.IsZero() {
		return now.Add(-24 * time.Hour)
	}
	return until
}

func (a *app) lastNight(ctx context.Context) time.Time {
	var last time.Time
	if _, err := a.host.StorageGet(ctx, selfLastRunKey, &last); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night last run:", err)
	}
	return last
}

func (a *app) clearNight(ctx context.Context, date string) error {
	for _, id := range a.ledger(ctx).Sessions {
		s := a.session(id)
		if s == nil {
			continue
		}
		if err := a.deleteTree(ctx, s); err != nil {
			fmt.Fprintln(os.Stderr, "agents: delete night session:", err)
		}
	}
	if err := a.removeNightDir(); err != nil {
		return err
	}
	base, err := a.self.git(ctx, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	return a.host.StorageSet(ctx, selfNightSessionsKey, nightLedger{Running: true, Base: base, Date: date})
}

func (a *app) noteNight(ctx context.Context, s *session) {
	ledger := a.ledger(ctx)
	ledger.Sessions = append(ledger.Sessions, s.snapshot().ID)
	if err := a.host.StorageSet(ctx, selfNightSessionsKey, ledger); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night sessions:", err)
	}
}

func (a *app) nightOver(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	ledger := a.ledger(ctx)
	ledger.Running = false
	if err := a.host.StorageSet(ctx, selfNightSessionsKey, ledger); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night sessions:", err)
	}
}

func (a *app) reapNight(ctx context.Context) {
	ledger := a.ledger(ctx)
	if !ledger.Running {
		return
	}
	var ids []string
	refused := 0
	for _, s := range a.nightSessions() {
		a.closeSession(ctx, s, false)
		ids = append(ids, s.snapshot().ID)
		refused += s.snapshot().Refused
	}
	if err := a.removeNightDir(); err != nil {
		fmt.Fprintln(os.Stderr, "agents: remove night sandboxes:", err)
	}
	if err := a.host.StorageSet(ctx, selfNightSessionsKey, nightLedger{Sessions: ids}); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night sessions:", err)
	}
	a.reportRestart(ctx, ledger, refused)
}

func (a *app) nightSessions() []*session {
	a.mu.Lock()
	all := make([]*session, 0, len(a.sessions))
	for _, s := range a.sessions {
		all = append(all, s)
	}
	a.mu.Unlock()
	return slices.DeleteFunc(all, func(s *session) bool { return s.snapshot().Night == "" })
}

func (a *app) ledger(ctx context.Context) nightLedger {
	var ledger nightLedger
	if _, err := a.host.StorageGet(ctx, selfNightSessionsKey, &ledger); err != nil {
		fmt.Fprintln(os.Stderr, "agents: night sessions:", err)
	}
	return ledger
}

func (a *app) nightPath(parts ...string) string {
	return filepath.Join(append([]string{a.stateDir, nightRoot}, parts...)...)
}

func memoryForNight(ctx context.Context, dir string, real []string, digest, rejected string) (memorySandbox, error) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	m, err := newMemorySandbox(ctx, dir, real)
	if err != nil {
		return m, err
	}
	if err := excludeFromSandbox(dir, nightDigestFile, rejectedFile); err != nil {
		return m, err
	}
	return m, errors.Join(writeAtomic(filepath.Join(dir, nightDigestFile), []byte(digest)), writeAtomic(filepath.Join(dir, rejectedFile), []byte(rejected)))
}

func skipSlow(err error, name string, res *nightResult) error {
	if !errors.Is(err, errNightLimit) {
		return err
	}
	res.Skipped = append(res.Skipped, name+": "+errNightLimit.Error())
	return nil
}

func applySleep(ctx context.Context, save memorySaver, m memorySandbox, name string, res *nightResult) {
	ctx, cancel := context.WithTimeout(ctx, selfTimeout)
	defer cancel()
	c, err := landMemory(ctx, save, m)
	switch {
	case errors.Is(err, errMemoryStuck):
		res.Stuck = append(res.Stuck, name+": "+oneLine(err.Error()))
	case errors.Is(err, errMemoryMoved):
		res.Skipped = append(res.Skipped, name+": "+errMemoryMoved.Error())
	case err != nil:
		res.Skipped = append(res.Skipped, name+": "+oneLine(err.Error()))
	case c != nil:
		res.Memory = append(res.Memory, *c)
	}
}

func landMemory(ctx context.Context, save memorySaver, m memorySandbox) (*selfChange, error) {
	subject, err := sandboxGit(ctx, m.Dir, "log", "-1", "--format=%s")
	if err != nil {
		return nil, err
	}
	patch, err := m.apply(ctx)
	if err != nil || len(patch) == 0 {
		return nil, err
	}
	c, err := save(ctx, patch, m.Dirs, oneLine(string(subject)))
	if err == nil {
		return &c, nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), selfTimeout)
	defer cancel()
	if undo := reverseMemory(ctx, patch, m.Dirs); undo != nil {
		return nil, fmt.Errorf("%w: %w", errMemoryStuck, errors.Join(err, undo))
	}
	return nil, err
}

func hidePatches(ctx context.Context, dir string) error {
	if err := writeAtomic(filepath.Join(sandboxGitDir(dir), gitSparseFile), []byte(selfSparse)); err != nil {
		return err
	}
	for _, kv := range [][2]string{{"core.sparseCheckout", "true"}, {"core.sparseCheckoutCone", "false"}} {
		if _, err := sandboxGit(ctx, dir, "config", kv[0], kv[1]); err != nil {
			return fmt.Errorf("configure the self's copy: %w", err)
		}
	}
	if _, err := sandboxGit(ctx, dir, "read-tree", "-mu", "HEAD"); err != nil {
		return fmt.Errorf("hide memory patches: %w", err)
	}
	return nil
}

func excludeFromSandbox(dir string, names ...string) error {
	var lines strings.Builder
	for _, name := range names {
		lines.WriteString("/" + name + "\n")
	}
	return writeAtomic(filepath.Join(sandboxGitDir(dir), gitExcludeFile), []byte(lines.String()))
}

func sandboxGitDir(dir string) string {
	return filepath.Clean(dir) + gitDirName
}

func (a *app) removeSandbox(dir string) {
	err := errors.Join(os.RemoveAll(dir), os.RemoveAll(sandboxGitDir(dir)))
	for _, logs := range a.nightLogs(dir) {
		err = errors.Join(err, os.RemoveAll(logs))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: remove sandbox:", err)
	}
}

func (a *app) removeNightDir() error {
	entries, err := os.ReadDir(a.nightPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() && filepath.Ext(entry.Name()) != gitDirName {
			a.removeSandbox(a.nightPath(entry.Name()))
		}
	}
	return os.RemoveAll(a.nightPath())
}

func (a *app) nightLogs(sandbox string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agents: night logs:", err)
		return nil
	}
	root := a.nightPath()
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		real = root
	}
	return nightLogDirs(home, root, real, sandbox)
}

func nightLogDirs(home, root, realRoot, sandbox string) []string {
	rel, err := filepath.Rel(root, sandbox)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil
	}
	projects := filepath.Join(home, ".claude", "projects")
	var dirs []string
	for _, path := range []string{filepath.Join(root, rel), filepath.Join(realRoot, rel)} {
		dir := filepath.Join(projects, claudeProjectDir(path))
		if filepath.Dir(dir) == projects {
			dirs = append(dirs, dir)
		}
	}
	slices.Sort(dirs)
	return slices.Compact(dirs)
}
