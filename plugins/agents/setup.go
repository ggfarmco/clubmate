package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"clubmate/pkg/contract"
	"clubmate/pkg/pluginsdk"
)

const (
	setupChecking    = "checking"
	setupOK          = "ok"
	setupNodeMissing = "node_missing"
	setupNodeOld     = "node_old"
	setupSignedOut   = "signed_out"
	setupGitMissing  = "git_missing"

	setupTopic   = "agents.setup.changed"
	setupEnv     = "CLUBMATE_AGENTS_SETUP"
	setupFixEnv  = "CLUBMATE_AGENTS_SETUP_FIX"
	setupFixKey  = "setup/fixes"
	retryFile    = "claude-acp.retry"
	retryFirst   = time.Minute
	retryMax     = 6 * time.Hour
	actSetupFix  = "setupFix"
	setupFixNode = "main/setup-fix"
	nodeURL      = "https://nodejs.org/en/download"
	nodeFormula  = "node@22"
	terminalsNew = "terminals.new"
	xcodeSelect  = "/usr/bin/xcode-select"
	systemGit    = "/usr/bin/git"

	nodeTitle      = "Node 22+ is required to run Claude"
	nodeNotFound   = "Node not found"
	nodeNeedText   = "Claude runs through claude-agent-acp, which needs Node 22 or later."
	nodeAction     = "Install Node…"
	nodeBrewHint   = "Runs brew install node@22 in a terminal tab. Checked again when it finishes."
	nodeURLHint    = "Opens nodejs.org. Checked again when you come back."
	signInRow      = "Sign in to Claude to start agents"
	signInEmpty    = "Sign in to Claude"
	signInText     = "Clubmate runs Claude Code on this Mac with your Claude login. This Mac isn't signed in yet."
	signInAction   = "Sign in…"
	signInHint     = "Opens Claude's sign-in in your browser. Checked again when you come back."
	gitTitle       = "git is needed for worktrees and checkpoints"
	gitDetail      = "git not found"
	gitText        = "Agents still run, but turns aren't checkpointed and the wiki stays off."
	gitAction      = "Install…"
	gitHint        = "Installs the Xcode Command Line Tools (xcode-select --install)."
	checkingText   = "Getting Claude ready…"
	firstRunTitle  = "Start your first agent"
	firstRunText   = "An agent can work in its own worktree, and every turn saves a git checkpoint you can go back to."
	installNodeTab = "Install Node"
	signInTab      = "Sign in to Claude"
	installGitTab  = "Install git"
)

var (
	brewPaths      = []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"}
	nodeKegs       = []string{"/opt/homebrew/opt/node@22/bin/node", "/usr/local/opt/node@22/bin/node"}
	errSetupUser   = errors.New("only the user runs a setup fix")
	errNoHome      = errors.New("no Home workspace to run the fix in")
	errNoFixShown  = errors.New("no setup fix to run")
	errInstallWait = errors.New("the agent install failed; retrying later")
)

type setupFound struct {
	Node string `json:"node,omitempty"`
	Git  string `json:"git,omitempty"`
}

type setupFix struct {
	Label string `json:"label"`
	Run   string `json:"run,omitempty"`
	URL   string `json:"url,omitempty"`
	argv  []string
	tab   string
}

type setupState struct {
	State  string     `json:"state"`
	Found  setupFound `json:"found"`
	Fix    *setupFix  `json:"fix,omitempty"`
	Title  string     `json:"title,omitempty"`
	Detail string     `json:"detail,omitempty"`
	path   string
}

type setupProbe struct {
	node    func(ctx context.Context) (path, version string)
	brew    func() string
	install func(ctx context.Context, node string) (string, error)
	login   func(ctx context.Context, node, script string) (in, known bool)
	git     func(ctx context.Context) (string, bool)
}

type setupCheck struct {
	mu      sync.Mutex
	storeMu sync.Mutex
	state   setupState
	running bool
	again   bool
	fixes   map[string]bool
	probe   *setupProbe
}

func (p setupProbe) run(ctx context.Context) setupState {
	var st setupState
	node, version := p.node(ctx)
	st.Found.Node = version
	switch {
	case node == "":
		return p.nodeState(setupNodeMissing, st)
	case nodeMajor(version) < minNodeMajor:
		st.path = node
		return p.nodeState(setupNodeOld, st)
	}
	if script, err := p.install(ctx, node); err == nil {
		if in, known := p.login(ctx, node, script); known && !in {
			st.State, st.Title = setupSignedOut, signInRow
			st.Fix = &setupFix{Label: signInAction, Run: "claude auth login", argv: []string{node, script, "--cli", "auth", "login"}, tab: signInTab}
			return st
		}
	} else if !errors.Is(err, errInstallWait) {
		fmt.Fprintln(os.Stderr, "agents: preinstall:", err)
	}
	git, ok := p.git(ctx)
	st.Found.Git = git
	if !ok {
		st.State, st.Title, st.Detail = setupGitMissing, gitTitle, gitDetail
		st.Fix = &setupFix{Label: gitAction, Run: "xcode-select --install", argv: []string{xcodeSelect, "--install"}, tab: installGitTab}
		return st
	}
	st.State = setupOK
	return st
}

func (p setupProbe) nodeState(state string, st setupState) setupState {
	st.State, st.Title, st.Detail = state, nodeTitle, nodeNotFound
	if state == setupNodeOld {
		st.Detail = "found " + shortVersion(st.Found.Node)
	}
	if brew := p.brew(); brew != "" {
		st.Fix = &setupFix{Label: nodeAction, Run: "brew install " + nodeFormula, argv: []string{brew, "install", nodeFormula}, tab: installNodeTab}
	} else {
		st.Fix = &setupFix{Label: nodeAction, URL: nodeURL}
	}
	return st
}

func forcedSetup(p setupProbe, state string) setupState {
	st := setupState{State: state, Found: setupFound{Node: "22.11.0", Git: "2.47.1"}}
	switch state {
	case setupNodeMissing:
		st.Found = setupFound{}
		st = p.nodeState(state, st)
	case setupNodeOld:
		st.Found.Node, st.path = "18.19.0", "/usr/local/bin/node"
		st = p.nodeState(state, st)
	case setupSignedOut:
		st.Title = signInRow
		st.Fix = &setupFix{Label: signInAction, Run: "claude auth login", tab: signInTab}
	case setupGitMissing:
		st.Found.Git = ""
		st.Title, st.Detail = gitTitle, gitDetail
		st.Fix = &setupFix{Label: gitAction, Run: "xcode-select --install", tab: installGitTab}
	}
	if st.Fix != nil && st.Fix.URL == "" {
		st.Fix.argv = forcedFixArgv()
	}
	return st
}

func forcedFixArgv() []string {
	if c := os.Getenv(setupFixEnv); c != "" {
		return []string{"/bin/sh", "-c", c}
	}
	return []string{"/usr/bin/true"}
}

func forcedState() string {
	forced := os.Getenv(setupEnv)
	if path, ok := strings.CutPrefix(forced, "@"); ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	return forced
}

type installRetry struct {
	At    time.Time     `json:"at"`
	Delay time.Duration `json:"delay"`
}

type installBackoff struct {
	path string
	now  func() time.Time
}

func (b installBackoff) load() installRetry {
	var r installRetry
	if data, err := os.ReadFile(b.path); err == nil && json.Unmarshal(data, &r) != nil {
		r = installRetry{}
	}
	return r
}

func (b installBackoff) guard(install func(ctx context.Context, node string, banner bool) (string, error)) func(context.Context, string) (string, error) {
	return func(ctx context.Context, node string) (string, error) {
		r := b.load()
		if b.now().Before(r.At) {
			return "", errInstallWait
		}
		script, err := install(ctx, node, r.Delay == 0)
		if err != nil {
			delay := min(max(r.Delay*4, retryFirst), retryMax)
			if data, merr := json.Marshal(installRetry{At: b.now().Add(delay), Delay: delay}); merr == nil {
				if werr := os.WriteFile(b.path, data, 0o600); werr != nil {
					fmt.Fprintln(os.Stderr, "agents: store install retry:", werr)
				}
			}
			return "", err
		}
		if r.Delay != 0 {
			if rerr := os.Remove(b.path); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				fmt.Fprintln(os.Stderr, "agents: clear install retry:", rerr)
			}
		}
		return script, nil
	}
}

func realSetupProbe(stateDir string, installing func(bool)) *setupProbe {
	return &setupProbe{
		node: lookupNode,
		brew: findBrew,
		install: installBackoff{path: filepath.Join(stateDir, retryFile), now: time.Now}.guard(func(ctx context.Context, node string, banner bool) (string, error) {
			note := installing
			if !banner {
				note = func(bool) {}
			}
			ictx, cancel := context.WithTimeout(ctx, installTimeout)
			defer cancel()
			return installClaude(ictx, filepath.Join(stateDir, claudeDir), withPathDir(agentEnv(""), filepath.Dir(node)), note)
		}),
		login: loginStatus,
		git:   gitVersion,
	}
}

func (a *app) runSetupProbe(ctx context.Context) setupState {
	p := a.setup.probe
	if p == nil {
		p = realSetupProbe(a.stateDir, a.setInstalling)
	}
	if forced := forcedState(); forced != "" {
		return forcedSetup(*p, forced)
	}
	if os.Getenv(commandEnv) != "" {
		git, ok := p.git(ctx)
		st := setupState{State: setupOK, Found: setupFound{Git: git}}
		if !ok {
			return forcedSetup(*p, setupGitMissing)
		}
		return st
	}
	return p.run(ctx)
}

func lookupNode(ctx context.Context) (string, string) {
	var candidates []string
	if p, err := exec.LookPath("node"); err == nil {
		candidates = append(candidates, p)
	}
	candidates = append(candidates, nodeKegs...)
	first, firstVersion := "", ""
	for _, c := range candidates {
		if info, err := os.Stat(c); err != nil || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		v := nodeVersion(ctx, c)
		if v == "" {
			continue
		}
		if nodeMajor(v) >= minNodeMajor {
			return c, v
		}
		if first == "" {
			first, firstVersion = c, v
		}
	}
	return first, firstVersion
}

func nodeVersion(ctx context.Context, node string) string {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.TrimSpace(string(out)), "v")
}

func findBrew() string {
	if p, err := exec.LookPath("brew"); err == nil {
		return p
	}
	for _, p := range brewPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func loginStatus(ctx context.Context, node, script string) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script, "--cli", "auth", "status", "--json")
	cmd.Env = withPathDir(agentEnv(""), filepath.Dir(node))
	out, _ := cmd.Output()
	return parseLogin(out)
}

func parseLogin(out []byte) (bool, bool) {
	var status struct {
		LoggedIn *bool `json:"loggedIn"`
	}
	if json.Unmarshal(out, &status) != nil || status.LoggedIn == nil {
		return false, false
	}
	return *status.LoggedIn, true
}

func gitVersion(ctx context.Context) (string, bool) {
	git, err := exec.LookPath("git")
	if err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if git == systemGit && exec.CommandContext(ctx, xcodeSelect, "-p").Run() != nil {
		return "", false
	}
	out, err := exec.CommandContext(ctx, git, "--version").Output()
	if err != nil {
		return "", false
	}
	return parseGitVersion(string(out)), true
}

func parseGitVersion(out string) string {
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

func shortVersion(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

func withPathDir(env []string, dir string) []string {
	i := slices.IndexFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "PATH=") })
	if i < 0 {
		return append(env, "PATH="+dir)
	}
	if slices.Contains(strings.Split(strings.TrimPrefix(env[i], "PATH="), ":"), dir) {
		return env
	}
	env[i] = "PATH=" + dir + ":" + strings.TrimPrefix(env[i], "PATH=")
	return env
}

func (a *app) setupState() setupState {
	a.setup.mu.Lock()
	defer a.setup.mu.Unlock()
	if a.setup.state.State == "" {
		return setupState{State: setupChecking}
	}
	return a.setup.state
}

func (a *app) recheckSetup() {
	c := &a.setup
	c.mu.Lock()
	if c.running {
		c.again = true
		c.mu.Unlock()
		return
	}
	c.running = true
	c.mu.Unlock()
	go func() {
		for {
			st := a.runSetupProbe(context.Background())
			c.mu.Lock()
			changed := !reflect.DeepEqual(c.state, st)
			c.state = st
			again := c.again
			c.again = false
			c.running = again
			c.mu.Unlock()
			if changed {
				a.setupChanged(st)
			}
			if !again {
				return
			}
		}
	}()
}

func (a *app) setupChanged(st setupState) {
	if a.host != nil {
		if _, err := a.host.Publish(context.Background(), setupTopic, st); err != nil {
			fmt.Fprintln(os.Stderr, "agents: publish setup:", err)
		}
	}
	a.uiMu.Lock()
	workspaces := slices.Collect(maps.Keys(a.ui))
	a.uiMu.Unlock()
	for _, id := range workspaces {
		a.kick(id)
	}
}

type setupArgs struct {
	Check bool `json:"check"`
}

func (a *app) setupCommand(_ context.Context, _ pluginsdk.CommandRun, args setupArgs) (any, error) {
	if args.Check {
		a.recheckSetup()
	}
	return a.setupState(), nil
}

func (a *app) setupFixCommand(ctx context.Context, cmd pluginsdk.CommandRun, _ struct{}) (any, error) {
	if !a.userWord(cmd) {
		return nil, errSetupUser
	}
	return nil, a.runSetupFix(ctx, cmd.WorkspaceID)
}

func (a *app) runSetupFix(ctx context.Context, workspaceID string) error {
	fix := a.setupState().Fix
	if fix == nil {
		return errNoFixShown
	}
	if fix.URL != "" {
		return exec.CommandContext(ctx, "/usr/bin/open", fix.URL).Run()
	}
	if workspaceID == "" {
		home, err := a.homeWorkspace(ctx)
		if err != nil {
			return err
		}
		if err := a.host.OpenWorkspace(ctx, home); err != nil {
			return err
		}
		workspaceID = home
	}
	var out struct {
		Session string `json:"session"`
	}
	if err := a.host.RunCommand(ctx, terminalsNew, workspaceID, map[string]any{"command": fix.argv, "title": fix.tab}, &out); err != nil {
		return err
	}
	a.setup.mu.Lock()
	if a.setup.fixes == nil {
		a.setup.fixes = map[string]bool{}
	}
	a.setup.fixes[out.Session] = true
	a.setup.mu.Unlock()
	a.storeSetupFixes(ctx)
	a.recheckSetup()
	return nil
}

func (a *app) storeSetupFixes(ctx context.Context) {
	if a.host == nil {
		return
	}
	a.setup.storeMu.Lock()
	defer a.setup.storeMu.Unlock()
	a.setup.mu.Lock()
	ids := slices.Sorted(maps.Keys(a.setup.fixes))
	a.setup.mu.Unlock()
	if err := a.host.StorageSet(ctx, setupFixKey, ids); err != nil {
		fmt.Fprintln(os.Stderr, "agents: store setup fixes:", err)
	}
}

func (a *app) loadSetupFixes(ctx context.Context) error {
	var ids []string
	if _, err := a.host.StorageGet(ctx, setupFixKey, &ids); err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	sessions, err := a.host.ListSessions(ctx, "", false)
	if err != nil {
		return err
	}
	running := map[string]bool{}
	for _, s := range sessions {
		if s.State == contract.SessionStateRunning {
			running[s.ID] = true
		}
	}
	a.setup.mu.Lock()
	a.setup.fixes = map[string]bool{}
	for _, id := range ids {
		if running[id] {
			a.setup.fixes[id] = true
		}
	}
	a.setup.mu.Unlock()
	a.storeSetupFixes(ctx)
	return nil
}

func (a *app) homeWorkspace(ctx context.Context) (string, error) {
	list, err := a.host.ListWorkspaces(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range list {
		if w.Home {
			return w.ID, nil
		}
	}
	return "", errNoHome
}

func (a *app) setupFixExited(ev pluginsdk.Event) {
	var p ptyEvent
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	a.setup.mu.Lock()
	ours := a.setup.fixes[p.ID]
	delete(a.setup.fixes, p.ID)
	a.setup.mu.Unlock()
	if ours {
		a.storeSetupFixes(context.Background())
		a.recheckSetup()
	}
}
