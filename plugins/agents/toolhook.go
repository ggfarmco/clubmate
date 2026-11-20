package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"clubmate/internal/proc"
	"clubmate/plugins/agents/acp"
	"clubmate/plugins/internal/gitrepo"
	"clubmate/plugins/internal/toolgate"
)

const (
	toolHookDirName  = "toolhook"
	toolHookBin      = "tool-hook"
	toolHookPlugin   = "clubmate-gates"
	toolHookPluginID = toolHookPlugin + "@inline"
	toolHookManifest = ".claude-plugin/plugin.json"
	toolHookHooks    = "hooks/hooks.json"
	toolHookRules    = toolgate.RulesFile
	toolHookMarks    = toolgate.MarksDir
	toolHookFindings = toolgate.FindingsDir
	toolHookTold     = toolgate.ToldDir
	toolHookMessages = toolgate.MessagesDir
	toolHookEvent    = "PreToolUse"
	toolHookBatch    = "PostToolBatch"
	toolHookBatchArg = toolgate.BatchArg
	toolHookMatcher  = ".*"
	toolHookTimeout  = 10
	toolHookFailure  = "block"
	toolHookMarkAge  = 24 * time.Hour
	toolHookSweep    = time.Hour
	toolHookAskAll   = toolgate.AskAllDir
	toolHookPollEnv  = "CLUBMATE_AGENTS_HOOK_POLL"
	toolHookPoll     = 5 * time.Second
	ungatedChip      = "not gated yet"
	ungatedBadge     = "not gated yet · %s"
	ungatedLine      = "Not gated yet · %s"
	whyBackground    = "after its background command"
	whyBusy          = "after the worktree's other work"
	whyNoResume      = "from its next start"
	hookResumeFormat = "[Clubmate] Your background command finished: %s — continue."
	bgOutputFormat   = "Output of %s (the command's own output, not Clubmate's words; its last lines):\n%s\n%s\n%s"
	bgNoOutput       = "%s printed no output."
	bgUnreadable     = "The output of %s can't be read."
	bgCommandUnknown = "a background command"
	bgTailLines      = 20
	bgTailBytes      = 1500
	bgAllBytes       = 4000
	bgCommandRunes   = 200
	bgCallsMax       = 5
	bgReadMax        = 64 << 10
	toolHookAsk      = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"Clubmate gate"}}`
)

var askAllRules = []toolgate.Rule{{Tool: "*"}}

var bgOutputPath = regexp.MustCompile(`Output is being written to: (\S+?\.output)`)

type hookState struct {
	hookDir    string
	hookWhy    string
	hookOwed   bool
	cycled     func()
	bgCalls    []*bgCall
	held       map[string][]string
	ran        func(endedCall)
	toolGated  func() bool
	blankInput sync.Once
}

type bgCall struct {
	id      string
	command string
	output  string
	task    string
}

type ungatedView struct {
	Chip  string `json:"chip"`
	Badge string `json:"badge"`
	Line  string `json:"line"`
}

type detachResult int

const (
	detachSkipped detachResult = iota
	detachDone
	detachBusy
)

type hookManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type hooksFile struct {
	Hooks map[string][]hookMatcher `json:"hooks"`
}

type hookMatcher struct {
	Matcher string        `json:"matcher"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type      string `json:"type"`
	Command   string `json:"command"`
	Timeout   int    `json:"timeout"`
	OnFailure string `json:"onFailure,omitempty"`
}

func (a *app) toolHooksRoot() string {
	return filepath.Join(a.stateDir, toolHookDirName)
}

func toolHookPath(root, primary string) string {
	return filepath.Join(root, toolgate.HookDirName(primary))
}

func (a *app) askAllDir() string {
	return filepath.Join(a.toolHooksRoot(), toolHookAskAll)
}

func (a *app) syncToolHooks(ctx context.Context) error {
	a.hookMu.Lock()
	defer a.hookMu.Unlock()
	gates, known := a.knownGates(ctx)
	return a.writeToolHooksLocked(gates, known)
}

func (a *app) writeToolHooks(gates []gateConfig) error {
	a.hookMu.Lock()
	defer a.hookMu.Unlock()
	return a.writeToolHooksLocked(gates, true)
}

func (a *app) writeToolHooksLocked(gates []gateConfig, known bool) error {
	root := a.toolHooksRoot()
	bin := toolHookBinary()
	wrote := map[string]bool{a.askAllDir(): true}
	errs := []error{writeToolHook(a.askAllDir(), bin, askAllRules)}
	if !known {
		return errors.Join(errs...)
	}
	for _, primary := range toolGatedRepos(gates) {
		dir := toolHookPath(root, primary)
		wrote[dir] = true
		errs = append(errs, writeToolHook(dir, bin, toolRulesOf(gates, primary)))
	}
	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	for _, e := range entries {
		if dir := filepath.Join(root, e.Name()); e.IsDir() && !wrote[dir] {
			errs = append(errs, writeJSON(filepath.Join(dir, toolHookRules), []toolgate.Rule{}))
		}
	}
	return errors.Join(errs...)
}

func writeToolHook(dir, bin string, rules []toolgate.Rule) error {
	for _, sub := range []string{toolHookMarks, toolHookFindings, toolHookTold} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	hook := hookCommand{Type: "command", Command: toolHookCommand(bin, dir), Timeout: toolHookTimeout, OnFailure: toolHookFailure}
	batch := hookCommand{Type: "command", Command: toolBatchCommand(bin, dir), Timeout: toolHookTimeout}
	return errors.Join(
		writeJSON(filepath.Join(dir, toolHookManifest), hookManifest{Name: toolHookPlugin, Version: "1"}),
		writeJSON(filepath.Join(dir, toolHookHooks), hooksFile{Hooks: map[string][]hookMatcher{
			toolHookEvent: {{Matcher: toolHookMatcher, Hooks: []hookCommand{hook}}},
			toolHookBatch: {{Matcher: toolHookMatcher, Hooks: []hookCommand{batch}}},
		}}),
		writeJSON(filepath.Join(dir, toolHookRules), rules),
	)
}

func writeJSON(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeAtomic(path, raw)
}

func toolHookCommand(bin, dir string) string {
	return shellQuote(bin) + " " + shellQuote(dir) + " || printf '%s' " + shellQuote(toolHookAsk)
}

func toolBatchCommand(bin, dir string) string {
	return shellQuote(bin) + " " + toolHookBatchArg + " " + shellQuote(dir) + " || true"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func toolHookBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return toolHookBin
	}
	return filepath.Join(filepath.Dir(exe), toolHookBin)
}

func toolGatedRepos(gates []gateConfig) []string {
	var repos []string
	for _, g := range gates {
		if g.Point == gatePointTool && !slices.Contains(repos, g.Repo) {
			repos = append(repos, g.Repo)
		}
	}
	slices.Sort(repos)
	return repos
}

func (a *app) gatesOrLast(ctx context.Context) []gateConfig {
	gates, err := a.gates(ctx)
	if err == nil {
		return gates
	}
	logError("read gates", err)
	if last := a.lastGatesRead(); last != nil {
		return *last
	}
	return nil
}

func (a *app) knownGates(ctx context.Context) ([]gateConfig, bool) {
	if last := a.lastGates.Load(); last != nil {
		return *last, true
	}
	gates, err := a.gates(ctx)
	if err == nil {
		return gates, true
	}
	logError("read gates", err)
	if last := a.lastGatesRead(); last != nil {
		return *last, true
	}
	return nil, false
}

type hookPrimaryOf struct {
	primary string
	git     os.FileInfo
}

func (a *app) hookPrimary(ctx context.Context, worktree string) (string, error) {
	git := gitMarker(worktree)
	if v, ok := a.hookPrimaries.Load(worktree); ok {
		if e := v.(hookPrimaryOf); git != nil && os.SameFile(e.git, git) {
			return e.primary, nil
		}
		a.hookPrimaries.CompareAndDelete(worktree, v)
	}
	primary, err := primaryRead(ctx, worktree)
	if err == nil && primary != "" && git != nil {
		a.hookPrimaries.Store(worktree, hookPrimaryOf{primary: primary, git: git})
	}
	return primary, err
}

func gitMarker(worktree string) os.FileInfo {
	top, ok := gitrepo.RepoRoot(worktree)
	if !ok {
		return nil
	}
	info, err := os.Lstat(filepath.Join(top, ".git"))
	if err != nil {
		return nil
	}
	return info
}

func hookable(rec record) bool {
	return rec.GateFor == nil && rec.Night == ""
}

func (a *app) toolHookDir(ctx context.Context, rec record) string {
	if !hookable(rec) {
		return ""
	}
	gates, known := a.knownGates(ctx)
	if known && len(toolGatedRepos(gates)) == 0 {
		return ""
	}
	primary, err := a.hookPrimary(ctx, rec.Worktree)
	dir := a.askAllDir()
	if known && err == nil {
		dir = toolHookDirOf(a.toolHooksRoot(), gates, rec, primary)
	}
	if dir != "" && !hookDirReady(dir) {
		logError("write the tool hooks", a.syncToolHooks(ctx))
	}
	return dir
}

func hookDirReady(dir string) bool {
	for _, f := range []string{toolHookManifest, toolHookHooks, toolHookRules, toolHookMarks} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

func toolHookDirOf(root string, gates []gateConfig, rec record, primary string) string {
	if !hookable(rec) || primary == "" || !slices.Contains(toolGatedRepos(gates), primary) {
		return ""
	}
	return toolHookPath(root, primary)
}

func withToolHook(meta *acp.SessionMeta, dir string) {
	opts := &meta.ClaudeCode.Options
	opts.Plugins = append(opts.Plugins, acp.ClaudeCodePlugin{Type: selfPluginType, Path: dir})
	if opts.Settings == nil {
		opts.Settings = &acp.ClaudeCodeSettings{}
	}
	off := false
	opts.Settings.DisableAllHooks = &off
	if opts.Settings.EnabledPlugins == nil {
		opts.Settings.EnabledPlugins = map[string]bool{}
	}
	opts.Settings.EnabledPlugins[toolHookPluginID] = true
}

func newlyToolGated(old *[]gateConfig, gates []gateConfig) []string {
	var before []string
	if old != nil {
		before = toolGatedRepos(*old)
	}
	return slices.DeleteFunc(toolGatedRepos(gates), func(repo string) bool { return slices.Contains(before, repo) })
}

func (s *session) hookLocked(dir string) {
	s.hookDir, s.rec.Hooked = dir, dir != ""
}

func (s *session) hookView() (record, bool, string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rec, s.proc != nil && s.proc.alive(), s.hookDir, s.hookWhy
}

func (a *app) needsHook(ctx context.Context, s *session) bool {
	rec, live, have, _ := s.hookView()
	if !live || rec.closed() || !hookable(rec) {
		return false
	}
	if last := a.lastGates.Load(); last != nil && len(toolGatedRepos(*last)) == 0 && have != a.askAllDir() {
		return false
	}
	want := a.toolHookDir(ctx, rec)
	switch {
	case want == have:
		return false
	case want == "":
		return have == a.askAllDir()
	case want == a.askAllDir():
		return have == ""
	}
	return true
}

func (a *app) rehook(ctx context.Context, primary string) {
	for _, s := range a.allSessions() {
		if p, err := a.hookPrimary(ctx, s.snapshot().Worktree); err == nil && p == primary {
			a.rehookSession(s)
		}
	}
}

func (a *app) unhooked(primary string) int {
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	n := 0
	for _, s := range a.allSessions() {
		if p, err := a.hookPrimary(ctx, s.snapshot().Worktree); err == nil && p == primary && a.needsHook(ctx, s) {
			n++
		}
	}
	return n
}

func (a *app) rehookSession(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	if !a.needsHook(ctx, s) {
		a.setHookWhy(s, "")
		if _, live, _, _ := s.hookView(); live {
			a.takeOwed(s)
		}
		return
	}
	_, _, _, before := s.hookView()
	switch res, why := a.detachForHook(s, false, sync.OnceValues(proc.List)); res {
	case detachSkipped:
		a.refreshHookWhy(s)
	case detachBusy:
		a.setHookWhy(s, why)
	case detachDone:
		a.setHookWhy(s, "")
		if a.takeOwed(s) || before == whyBackground {
			if text := a.hookResumePrompt(s); text != "" {
				a.submitIdle(s, promptArgs{Text: text})
			}
		}
	}
}

func (s *session) owesHook() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hookOwed
}

func (a *app) takeOwed(s *session) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	owed := s.hookOwed
	s.hookOwed = false
	return owed
}

func (a *app) cycleStarted(s *session) {
	s.mu.Lock()
	if s.hookWhy == whyBackground && (s.cycle || len(s.background) == 0) {
		s.hookOwed = true
	}
	owed := s.hookOwed
	s.mu.Unlock()
	if owed {
		a.hookCycle(s)
	}
}

func (a *app) hookCycle(s *session) {
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	if !a.needsHook(ctx, s) {
		return
	}
	s.attachMu.Lock()
	defer s.attachMu.Unlock()
	ready := s.hookCycleReadyLocked
	s.mu.Lock()
	p, ok := s.proc, ready()
	if p != nil && s.prompted && p.caps.SessionCapabilities.Resume == nil {
		ok = false
	}
	s.mu.Unlock()
	if p == nil || !ok {
		return
	}
	if procs, err := proc.List(); err != nil || backgroundWork(p.pgid, procs) {
		return
	}
	s.mu.Lock()
	if s.proc != p || !ready() {
		s.mu.Unlock()
		return
	}
	agentID := s.rec.AgentSessionID
	s.proc = nil
	s.rec.Suspended = true
	s.hookOwed, s.hookWhy = false, ""
	s.dropBackgroundLocked()
	s.mu.Unlock()
	s.changed(changeStatus, true)
	a.closeAgentSession(p, agentID)
	go func() {
		a.finishTurn(s)
		if text := a.hookResumePrompt(s); text != "" {
			a.submitIdle(s, promptArgs{Text: text})
		}
	}()
}

func (a *app) hookTurn(s *session) bool {
	ctx, cancel := context.WithTimeout(context.Background(), checkpointLimit)
	defer cancel()
	if !a.needsHook(ctx, s) {
		a.setHookWhy(s, "")
		return false
	}
	switch res, why := a.detachForHook(s, true, sync.OnceValues(proc.List)); res {
	case detachBusy:
		a.setHookWhy(s, why)
	case detachDone:
		a.setHookWhy(s, "")
		a.takeOwed(s)
		return true
	}
	return false
}

func (a *app) setHookWhy(s *session, why string) {
	s.mu.Lock()
	changed := s.hookWhy != why
	s.hookWhy = why
	s.mu.Unlock()
	if changed {
		s.changed(changeUpdate, false)
	}
}

func (s *session) canResume() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc == nil || !s.prompted || s.proc.caps.SessionCapabilities.Resume != nil
}

func (s *session) hookCycleReadyLocked() bool {
	return s.hookOwed && !s.running && len(s.perms) == 0 && len(s.calls) == 0 && (s.cycle || s.quietLocked() && len(s.background) == 0)
}

func (s *session) quietLocked() bool {
	return !s.rec.closed() && !s.deleted && !s.running && !s.cycle && len(s.perms) == 0 && len(s.calls) == 0 && len(s.queue) == 0 && s.rec.Status != statusWaiting && s.rec.Held == nil
}

func (a *app) detachForHook(s *session, starting bool, table func() ([]proc.Process, error)) (detachResult, string) {
	if !a.resumes(s.snapshot().ID) || !s.canResume() {
		return detachBusy, whyNoResume
	}
	if starting {
		s.attachMu.Lock()
	} else if !s.attachMu.TryLock() {
		return detachSkipped, ""
	}
	defer s.attachMu.Unlock()
	ready := func() bool {
		if starting {
			return s.running && !s.cycle && len(s.perms) == 0 && len(s.calls) == 0
		}
		return s.quietLocked()
	}
	s.mu.Lock()
	p, ok, tracked := s.proc, ready(), len(s.background) > 0
	s.mu.Unlock()
	switch {
	case p == nil || !ok:
		return detachSkipped, ""
	case tracked:
		return detachBusy, whyBackground
	}
	if procs, err := table(); err != nil || backgroundWork(p.pgid, procs) {
		return detachBusy, whyBusy
	}
	s.mu.Lock()
	if s.proc != p || !ready() || len(s.background) > 0 {
		s.mu.Unlock()
		return detachSkipped, ""
	}
	agentID := s.rec.AgentSessionID
	s.proc = nil
	s.rec.Suspended = true
	s.mu.Unlock()
	s.changed(changeUpdate, true)
	a.closeAgentSession(p, agentID)
	return detachDone, ""
}

func (a *app) hookLoop(poll time.Duration) {
	if poll <= 0 {
		logError("tool hook poll", fmt.Errorf("%s must be positive, using %s", toolHookPollEnv, toolHookPoll))
		poll = toolHookPoll
	}
	t := time.NewTicker(poll)
	defer t.Stop()
	swept := time.Now()
	for {
		select {
		case <-a.quit:
			return
		case now := <-t.C:
			for _, s := range a.allSessions() {
				rec, _, _, why := s.hookView()
				if rec.closed() {
					if why != "" || s.owesHook() {
						a.clearHook(s)
					}
					continue
				}
				if why != "" || s.owesHook() {
					if a.pruneBackground(s) {
						a.finishTurn(s)
					}
					a.cycleStarted(s)
					a.rehookSession(s)
				}
			}
			if now.Sub(swept) >= toolHookSweep {
				swept = now
				a.sweepToolHooks(now)
			}
		}
	}
}

func (a *app) sweepToolHooks(now time.Time) {
	a.sweepToolMarks(now)
	gates, known := a.knownGates(context.Background())
	if !known {
		return
	}
	keep := map[string]bool{a.askAllDir(): true}
	for _, primary := range toolGatedRepos(gates) {
		keep[toolHookPath(a.toolHooksRoot(), primary)] = true
	}
	for _, s := range a.allSessions() {
		if _, live, dir, _ := s.hookView(); live && dir != "" {
			keep[dir] = true
		}
	}
	a.hookMu.Lock()
	defer a.hookMu.Unlock()
	entries, err := os.ReadDir(a.toolHooksRoot())
	logError("list tool hook dirs", err)
	for _, e := range entries {
		if dir := filepath.Join(a.toolHooksRoot(), e.Name()); e.IsDir() && !keep[dir] {
			logError("remove a tool hook dir", os.RemoveAll(dir))
		}
	}
}

func (a *app) sweepToolMarks(now time.Time) {
	var marks []string
	for _, sub := range []string{toolHookMarks, toolHookFindings, toolHookTold, toolHookMessages} {
		found, err := filepath.Glob(filepath.Join(a.toolHooksRoot(), "*", sub, "*"))
		logError("list tool hook marks", err)
		marks = append(marks, found...)
	}
	for _, m := range marks {
		if info, err := os.Stat(m); err == nil && now.Sub(info.ModTime()) > toolHookMarkAge {
			logError("remove a tool hook mark", os.Remove(m))
		}
	}
}

func (a *app) ungated(rec record) *ungatedView {
	s := a.session(rec.ID)
	if s == nil || rec.closed() {
		return nil
	}
	_, _, _, why := s.hookView()
	if why == "" {
		return nil
	}
	return &ungatedView{Chip: ungatedChip, Badge: fmt.Sprintf(ungatedBadge, why), Line: fmt.Sprintf(ungatedLine, why)}
}

func ungatedLanding(u *ungatedView) *landingView {
	if u == nil {
		return nil
	}
	return &landingView{Words: u.Chip, Tone: tonePlain, Badge: u.Badge, BadgeTone: tonePlain, Line: u.Line, Ungated: true}
}

func (a *app) refreshHookWhy(s *session) {
	s.mu.Lock()
	why, tracked, busy, p := s.hookWhy, len(s.background) > 0, s.running || s.cycle, s.proc
	s.mu.Unlock()
	if why == "" || why == whyNoResume {
		return
	}
	next := ""
	switch {
	case tracked:
		next = whyBackground
	case p != nil && procBusy(p.pgid):
		next = whyBusy
	case busy:
		next = why
	}
	a.setHookWhy(s, next)
}

func procBusy(pgid int) bool {
	procs, err := proc.List()
	return err != nil || backgroundWork(pgid, procs)
}

func (s *session) noteBackgroundCallLocked(u backgroundUpdate, task string) {
	if u.ToolCallID == "" {
		return
	}
	i := slices.IndexFunc(s.bgCalls, func(c *bgCall) bool { return c.id == u.ToolCallID })
	if i < 0 {
		if !u.RawInput.InBackground && task == "" {
			return
		}
		s.bgCalls = append(s.bgCalls, &bgCall{id: u.ToolCallID})
		i = len(s.bgCalls) - 1
	}
	c := s.bgCalls[i]
	if u.RawInput.Command != "" {
		c.command = u.RawInput.Command
	}
	if task != "" {
		c.task = task
	}
	var out string
	if json.Unmarshal(u.RawOutput, &out) == nil {
		if m := bgOutputPath.FindStringSubmatch(out); m != nil {
			c.output = m[1]
		}
	}
}

func (s *session) followedUpLocked() {
	s.hookOwed = false
	s.bgCalls = slices.DeleteFunc(s.bgCalls, func(c *bgCall) bool { return c.task != "" && !s.background[c.task] })
}

func (a *app) clearHook(s *session) {
	s.mu.Lock()
	changed := s.hookWhy != ""
	s.hookOwed, s.hookWhy, s.bgCalls = false, "", nil
	s.mu.Unlock()
	if changed {
		s.changed(changeUpdate, false)
	}
}

func (a *app) hookResumePrompt(s *session) string {
	s.mu.Lock()
	var calls []*bgCall
	s.bgCalls = slices.DeleteFunc(s.bgCalls, func(c *bgCall) bool {
		ended := c.task != "" && !s.background[c.task]
		if ended {
			calls = append(calls, c)
		}
		return ended
	})
	s.mu.Unlock()
	return resumePrompt(calls)
}

func resumePrompt(calls []*bgCall) string {
	if len(calls) == 0 {
		return ""
	}
	calls = calls[max(0, len(calls)-bgCallsMax):]
	budget := min(bgTailBytes, bgAllBytes/len(calls))
	names := make([]string, 0, len(calls))
	blocks := make([]string, 0, len(calls))
	for _, c := range calls {
		name := "`" + commandText(c.command) + "`"
		if c.command == "" {
			name = bgCommandUnknown
		}
		names = append(names, name)
		blocks = append(blocks, outputBlock(name, c.output, budget))
	}
	return fmt.Sprintf(hookResumeFormat, strings.Join(names, "; ")) + "\n\n" + strings.Join(blocks, "\n\n")
}

func commandText(command string) string {
	command = strings.Join(strings.Fields(stripControl(command)), " ")
	if r := []rune(command); len(r) > bgCommandRunes {
		command = string(r[:bgCommandRunes]) + "…"
	}
	return strings.ReplaceAll(command, "`", "'")
}

func outputBlock(name, path string, budget int) string {
	raw, err := readTail(path, bgReadMax)
	if path == "" || err != nil {
		return fmt.Sprintf(bgUnreadable, name)
	}
	lines := strings.Split(strings.TrimSpace(stripControl(string(raw))), "\n")
	tail := strings.Join(lines[max(0, len(lines)-bgTailLines):], "\n")
	if len(tail) > budget {
		tail = tail[len(tail)-budget:]
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
	}
	if strings.TrimSpace(tail) == "" {
		return fmt.Sprintf(bgNoOutput, name)
	}
	fence := strings.Repeat("`", longestRun(tail)+1)
	return fmt.Sprintf(bgOutputFormat, name, fence, tail, fence)
}

func readTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	from := max(0, info.Size()-limit)
	raw := make([]byte, info.Size()-from)
	n, err := f.ReadAt(raw, from)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	raw = raw[:n]
	if i := bytes.IndexByte(raw, '\n'); from > 0 && i >= 0 {
		raw = raw[i+1:]
	}
	return raw, nil
}

func stripControl(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) && r != utf8.RuneError {
			return r
		}
		return -1
	}, strings.ToValidUTF8(text, ""))
}
