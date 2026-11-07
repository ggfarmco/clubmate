package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/cli"
	"clubmate/internal/testutil"
	"clubmate/pkg/contract"
)

type connectRequest struct {
	ID    string        `json:"id"`
	From  string        `json:"from"`
	Repos []connectRepo `json:"repos"`
}

type connectRepo struct {
	Path    string `json:"path"`
	URL     string `json:"url"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
	LandsBy string `json:"landsBy"`
	Source  string `json:"source"`
	State   string `json:"state"`
	Facts   *struct {
		Branch string `json:"branch"`
		Clean  bool   `json:"clean"`
	} `json:"facts"`
}

type connectResult struct {
	Request  string `json:"request"`
	Status   string `json:"status"`
	Next     string `json:"next"`
	RaisedOn string `json:"raisedOn"`
	Repos    []struct {
		Path  string `json:"path"`
		URL   string `json:"url"`
		State string `json:"state"`
	} `json:"repos"`
}

type connectAnswer map[string]any

func startGit(t *testing.T, script map[string]any) *fixture {
	t.Helper()
	return start(t, script, testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"))
}

func (f *fixture) runFrom(t *testing.T, workspace, caller, command string, args, out any) error {
	t.Helper()
	return f.h.Client(t).Call(t.Context(), "command.run", map[string]any{"id": command, "workspaceId": workspace, "args": args, "caller": caller}, out)
}

func (f *fixture) connect(t *testing.T, workspace, caller string, repos ...map[string]any) connectResult {
	t.Helper()
	var out connectResult
	require.NoError(t, f.runFrom(t, workspace, caller, "agents.connect-repo", map[string]any{"repos": repos}, &out))
	return out
}

func (f *fixture) answerConnect(t *testing.T, workspace, request string, answers ...connectAnswer) error {
	t.Helper()
	return f.run(t, workspace, "agents.connect-answer", map[string]any{"request": request, "answers": answers}, nil)
}

func (f *fixture) folders(t *testing.T, workspace string) []contract.Folder {
	t.Helper()
	p, err := f.h.Svc.GetWorkspace(t.Context(), workspace)
	require.NoError(t, err)
	return p.Folders
}

func (f *fixture) folderPaths(t *testing.T, workspace string) []string {
	t.Helper()
	var out []string
	for _, folder := range f.folders(t, workspace) {
		out = append(out, folder.Path)
	}
	return out
}

func (f *fixture) restartAgents(t *testing.T, workspace string) {
	t.Helper()
	before := f.agentsPID(t)
	require.NoError(t, syscall.Kill(before, syscall.SIGKILL))
	testutil.Eventually(t, func() bool {
		now := f.agentsPID(t)
		return now != 0 && now != before && f.run(t, workspace, "agents.sessions", map[string]any{}, nil) == nil
	}, restartWait)
}

func (f *fixture) agentsPID(t *testing.T) int {
	t.Helper()
	for _, st := range f.h.Sup.Statuses() {
		if st.ID == "agents" {
			return st.PID
		}
	}
	return 0
}

func (f *fixture) waitNeedsInput(t *testing.T, session string) {
	t.Helper()
	testutil.Eventually(t, func() bool {
		events, err := f.h.Bus.Read(t.Context(), "notify.posted", 0, 100)
		require.NoError(t, err)
		return slices.ContainsFunc(events, func(e contract.Event) bool {
			return strings.Contains(string(e.Payload), session) && strings.Contains(string(e.Payload), "needs-input")
		})
	}, wait)
}

func (f *fixture) waitPromptWith(t *testing.T, want ...string) string {
	t.Helper()
	got := f.waitCall(t, "session/prompt", func(c fakeCall) bool {
		return !slices.ContainsFunc(want, func(w string) bool { return !strings.Contains(string(c.Params), w) })
	})
	return promptText(t, got)
}

func TestConnectRequestSurvivesRestart(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{"resume": true})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)

	res := f.connect(t, workspace, session, map[string]any{"path": other, "reason": "the API lives there", "landsBy": "pr", "source": "its CLAUDE.md"})
	require.Equal(t, "pending", res.Status)
	require.NotEmpty(t, res.Request)
	assert.NotEmpty(t, res.Next)

	f.restartAgents(t, workspace)

	rec := f.session(t, workspace, session)
	assert.True(t, rec.NeedsYou)
	assert.Contains(t, rec.Pending, res.Request)
	require.NotNil(t, rec.PendingSummary)
	assert.Equal(t, "connect", rec.PendingSummary.Kind)
	require.Len(t, rec.ConnectRequests, 1)
	stored := rec.ConnectRequests[0].Repos[0]
	assert.Equal(t, other, stored.Path)
	assert.Equal(t, filepath.Base(other), stored.Name)
	assert.Equal(t, "the API lives there", stored.Reason)
	assert.Equal(t, "pr", stored.LandsBy)
	assert.Equal(t, "its CLAUDE.md", stored.Source)
	require.NotNil(t, stored.Facts)
	assert.Equal(t, "main", stored.Facts.Branch)
	assert.True(t, stored.Facts.Clean)

	require.NoError(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": other, "action": "connect"}))

	folders := f.folders(t, workspace)
	require.Len(t, folders, 2)
	assert.Equal(t, other, folders[1].Path)
	assert.Equal(t, "done", folders[1].AddedBy)
	assert.False(t, folders[1].AddedAfk)
	rec = f.session(t, workspace, session)
	assert.Empty(t, rec.ConnectRequests)
	assert.False(t, rec.NeedsYou)
	assert.Contains(t, f.waitPromptWith(t, "[Clubmate]", other), "connected")
}

func TestConnectAnswersEveryRequestForThatPath(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, shared, own := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	one, two := f.doneSession(t, workspace), f.doneSession(t, workspace)
	three := f.doneSession(t, workspace)
	asked := f.connect(t, workspace, one, map[string]any{"path": shared})
	other := f.connect(t, workspace, two, map[string]any{"path": shared})
	mixed := f.connect(t, workspace, three, map[string]any{"path": shared}, map[string]any{"path": own})

	require.NoError(t, f.answerConnect(t, workspace, asked.Request, connectAnswer{"path": shared, "action": "connect"}))

	assert.Equal(t, []string{first, shared}, f.folderPaths(t, workspace))
	assert.Empty(t, f.session(t, workspace, two).ConnectRequests)
	assert.False(t, f.session(t, workspace, two).NeedsYou)
	f.waitPromptWith(t, f.session(t, workspace, two).AgentSessionID, shared, "connected")
	left := f.session(t, workspace, three)
	require.Len(t, left.ConnectRequests, 1)
	assert.Equal(t, mixed.Request, left.ConnectRequests[0].ID)
	assert.True(t, left.NeedsYou)
	states := map[string]string{}
	for _, repo := range left.ConnectRequests[0].Repos {
		states[repo.Path] = repo.State
	}
	assert.Equal(t, map[string]string{shared: "connected", own: "pending"}, states)
	assert.ErrorContains(t, f.answerConnect(t, workspace, other.Request, connectAnswer{"path": shared, "action": "connect"}), "already answered")
}

func TestConnectActionIgnoresOtherPath(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, asked := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": asked})

	require.NoError(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": asked, "action": "connect", "otherPath": t.TempDir()}))

	assert.Equal(t, []string{first, asked}, f.folderPaths(t, workspace))
}

func TestUntickedRepoIsSkippedNotPending(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, ticked, unticked := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": ticked}, map[string]any{"path": unticked})

	require.NoError(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": ticked, "action": "connect"}))

	assert.Equal(t, []string{first, ticked}, f.folderPaths(t, workspace))
	rec := f.session(t, workspace, session)
	assert.Empty(t, rec.ConnectRequests)
	assert.False(t, rec.NeedsYou)
	sent := f.waitPromptWith(t, "[Clubmate]", unticked)
	assert.Contains(t, sent, "skipped")
}

func TestOtherFolderConnectsInsteadOfTheAskedPath(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, asked, picked := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": asked})

	require.NoError(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": asked, "action": "other", "otherPath": picked}))

	assert.Equal(t, []string{first, picked}, f.folderPaths(t, workspace))
	assert.Contains(t, f.waitPromptWith(t, "[Clubmate]", picked), asked)
}

func TestAFKConnectsPathOnDiskAtOnce(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.afk", map[string]any{"session": session, "on": true}, nil))

	res := f.connect(t, workspace, session, map[string]any{"path": other})

	assert.Equal(t, "connected", res.Status)
	assert.Empty(t, res.Request)
	folders := f.folders(t, workspace)
	require.Len(t, folders, 2)
	assert.Equal(t, other, folders[1].Path)
	assert.True(t, folders[1].AddedAfk)
	assert.Equal(t, "done", folders[1].AddedBy)
	assert.False(t, f.session(t, workspace, session).NeedsYou)
	notices := ofKind(f.transcript(t, workspace, session).Entries, "notice")
	require.NotEmpty(t, notices)
	assert.Contains(t, string(notices[len(notices)-1].Notice), filepath.Base(other))
}

func TestAFKLeavesAURLWaiting(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	require.NoError(t, f.run(t, workspace, "agents.afk", map[string]any{"session": session, "on": true}, nil))

	res := f.connect(t, workspace, session, map[string]any{"path": other}, map[string]any{"url": "git@example.com:acme/ads.git"})

	assert.Equal(t, "pending", res.Status)
	assert.Equal(t, []string{first, other}, f.folderPaths(t, workspace))
	rec := f.session(t, workspace, session)
	assert.True(t, rec.NeedsYou)
	require.Len(t, rec.ConnectRequests, 1)
	require.Len(t, rec.ConnectRequests[0].Repos, 1)
	assert.Equal(t, "git@example.com:acme/ads.git", rec.ConnectRequests[0].Repos[0].URL)
	assert.Equal(t, "ads", rec.ConnectRequests[0].Repos[0].Name)
}

func TestConnectAnswerRefusesAgentCallers(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": other})
	answer := map[string]any{"request": res.Request, "answers": []connectAnswer{{"path": other, "action": "connect"}}}

	require.Error(t, f.runAs(t, workspace, session, "agents.connect-answer", answer, nil))
	require.Error(t, f.runFrom(t, workspace, session, "agents.connect-answer", answer, nil))

	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
	assert.True(t, f.session(t, workspace, session).NeedsYou)
}

func cm(t *testing.T, args ...string) error {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := cli.NewRoot(&stdout, &stderr)
	root.SetArgs(args)
	return root.Execute()
}

func cmRun(t *testing.T, command string, args any) error {
	t.Helper()
	raw, err := json.Marshal(args)
	require.NoError(t, err)
	return cm(t, "command", "run", command, "--args", string(raw))
}

func (f *fixture) cmEnv(t *testing.T, workspace string) {
	t.Helper()
	t.Setenv("CLUBMATE_STATE_DIR", f.h.Dirs.State)
	t.Setenv("CLUBMATE_RUNTIME_DIR", f.h.Dirs.Runtime)
	t.Setenv("CLUBMATE_WORKSPACE_ID", workspace)
}

func TestCmInAnAgentSessionIsNotTheUser(t *testing.T) {
	f := startGit(t, map[string]any{})
	first, second, third := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	closing := f.doneSession(t, workspace)
	answer := func(repo string) map[string]any {
		res := f.connect(t, workspace, session, map[string]any{"path": repo})
		return map[string]any{"request": res.Request, "answers": []connectAnswer{{"path": repo, "action": "connect"}}}
	}
	toSecond, toThird := answer(second), answer(third)
	f.cmEnv(t, workspace)
	t.Setenv("CLUBMATE_AGENT_SESSION", session)

	require.ErrorContains(t, cmRun(t, "agents.connect-answer", toSecond), "only the user")
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))

	require.NoError(t, cmRun(t, "agents.close", map[string]any{"session": closing}))
	assert.Equal(t, "closed", f.session(t, workspace, closing).Status)

	t.Setenv("CLUBMATE_AGENT_SESSION", "agt_of_another_core")
	require.NoError(t, cmRun(t, "agents.connect-answer", toSecond))
	assert.Equal(t, []string{first, second}, f.folderPaths(t, workspace))

	t.Setenv("CLUBMATE_AGENT_SESSION", "")
	require.NoError(t, cmRun(t, "agents.connect-answer", toThird))
	assert.Equal(t, []string{first, second, third}, f.folderPaths(t, workspace))
}

func TestCmUIEventInAnAgentSessionIsNotTheUser(t *testing.T) {
	f := start(t, map[string]any{"prompt": []map[string]any{{"permission": permissionScript()}}})
	workspace := f.workspace(t, newRepo(t))
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "go")
	f.waitStatus(t, workspace, session, "waiting")
	request := ofKind(f.transcript(t, workspace, session).Entries, "permission")[0].ID
	press := func() error {
		value := fmt.Sprintf(`{"request":%q,"option":"allow"}`, request)
		return cm(t, "ui", "event", "--workspace", workspace, "--slot", "agents.main", "--node", session+"/permission/"+request, "--action", "permission", "--value", value)
	}
	f.cmEnv(t, workspace)
	t.Setenv("CLUBMATE_AGENT_SESSION", session)

	require.ErrorContains(t, press(), "only the user")
	assert.Equal(t, "waiting", f.session(t, workspace, session).Status)
	require.NoError(t, cm(t, "ui", "event", "--workspace", workspace, "--slot", "agents.main", "--node", "filter", "--action", "filter", "--value", `"all"`))

	t.Setenv("CLUBMATE_AGENT_SESSION", "agt_of_another_core")
	require.NoError(t, press())
	f.waitStatus(t, workspace, session, "done")
}

func TestParentsWordIsNotTheUsersYes(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	parent := f.doneSession(t, workspace)
	kid := f.newChild(t, workspace, parent, map[string]any{"prompt": "hi", "afk": true})
	f.waitStatus(t, workspace, kid.Session, "done")

	res := f.connect(t, workspace, kid.Session, map[string]any{"path": other})

	require.Equal(t, "pending", res.Status)
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
	err := f.runAs(t, workspace, parent, "agents.permission", map[string]any{"session": parent, "request": res.Request, "option": "connect"}, nil)
	require.Error(t, err)
	require.NoError(t, f.runAs(t, workspace, parent, "agents.prompt", map[string]any{"session": kid.Session, "text": "connect it, yes"}, nil))
	testutil.Eventually(t, func() bool { return f.session(t, workspace, parent).NeedsYou }, wait)
	assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
}

func TestChildRequestLandsOnTopLevelAncestor(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	top := f.doneSession(t, workspace)
	mid := f.newChild(t, workspace, top, map[string]any{"prompt": "hi"})
	f.waitStatus(t, workspace, mid.Session, "done")
	leaf := f.newChild(t, workspace, mid.Session, map[string]any{"prompt": "hi"})
	f.waitStatus(t, workspace, leaf.Session, "done")

	res := f.connect(t, workspace, leaf.Session, map[string]any{"path": other})

	assert.Equal(t, top, res.RaisedOn)
	assert.Contains(t, res.Next, top)
	var rec record
	testutil.Eventually(t, func() bool {
		rec = f.session(t, workspace, top)
		return rec.NeedsYou
	}, wait)
	require.Len(t, rec.ConnectRequests, 1)
	assert.Equal(t, leaf.Session, rec.ConnectRequests[0].From)
	assert.Equal(t, "needsYou", rec.Group)
	assert.False(t, f.session(t, workspace, leaf.Session).NeedsYou)
	assert.Empty(t, f.session(t, workspace, mid.Session).ConnectRequests)
	f.waitNeedsInput(t, top)
}

func TestConnectNeedsYouOnlyOnceTheTurnEnded(t *testing.T) {
	t.Parallel()
	hold := filepath.Join(t.TempDir(), "hold")
	f := startGit(t, map[string]any{"prompt": []map[string]any{{"wait": hold}}})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.newSession(t, workspace)
	f.prompt(t, workspace, session, "work")
	f.waitCall(t, "session/prompt", func(fakeCall) bool { return true })

	f.connect(t, workspace, session, map[string]any{"path": other})

	assert.False(t, f.session(t, workspace, session).NeedsYou)
	write(t, hold, "")
	f.waitStatus(t, workspace, session, "done")
	assert.True(t, f.session(t, workspace, session).NeedsYou)
	f.waitNeedsInput(t, session)
}

func TestConnectClipsWhatAnAgentWrites(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	workspace := f.workspace(t, newRepo(t))
	session := f.doneSession(t, workspace)

	f.connect(t, workspace, session, map[string]any{"url": "git@example.com:a/b.git", "reason": strings.Repeat("r", 5000), "source": strings.Repeat("s", 5000)})

	repo := f.session(t, workspace, session).ConnectRequests[0].Repos[0]
	assert.Less(t, len(repo.Reason), 1000)
	assert.Less(t, len(repo.Source), 1000)
}

func TestConnectRefusesABadRepo(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	session := f.doneSession(t, workspace)
	tests := []struct {
		name string
		repo map[string]any
	}{
		{"not a repo", map[string]any{"path": t.TempDir()}},
		{"relative path", map[string]any{"path": "ads"}},
		{"neither path nor url", map[string]any{"reason": "r"}},
		{"both path and url", map[string]any{"path": repo, "url": "git@example.com:a/b.git"}},
		{"url with no name", map[string]any{"url": "/"}},
		{"unknown landing rule", map[string]any{"url": "git@example.com:a/b.git", "landsBy": "force-push"}},
		{"url that runs a command", map[string]any{"url": "ext::sh -c touch% /tmp/x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := f.runFrom(t, workspace, session, "agents.connect-repo", map[string]any{"repos": []map[string]any{tt.repo}}, nil)

			require.Error(t, err)
			assert.Empty(t, f.session(t, workspace, session).ConnectRequests)
		})
	}
	require.Error(t, f.run(t, workspace, "agents.connect-repo", map[string]any{"repos": []map[string]any{{"url": "git@example.com:a/b.git"}}}, nil))
}

func TestConnectAnswerRefusesAnAnswerThatDoesNotFit(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": other})
	tests := []struct {
		name   string
		answer connectAnswer
	}{
		{"repo outside the request", connectAnswer{"path": first, "action": "connect"}},
		{"unknown action", connectAnswer{"path": other, "action": "maybe"}},
		{"other folder that is no repo", connectAnswer{"path": other, "action": "other", "otherPath": t.TempDir()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, f.answerConnect(t, workspace, res.Request, tt.answer))

			assert.Equal(t, []string{first}, f.folderPaths(t, workspace))
			assert.True(t, f.session(t, workspace, session).NeedsYou)
		})
	}
}

func TestConnectAnswerClonesAURL(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	bare := filepath.Join(t.TempDir(), "ads.git")
	source := newRepo(t)
	git(t, source, "clone", "--bare", "-q", source, bare)
	first, picked := newRepo(t), filepath.Join(t.TempDir(), "ads")
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"url": bare})

	require.NoError(t, f.answerConnect(t, workspace, res.Request, connectAnswer{"path": bare, "action": "other", "otherPath": picked}))

	testutil.Eventually(t, func() bool { return len(f.folders(t, workspace)) == 2 }, wait)
	assert.Equal(t, picked, f.folders(t, workspace)[1].Path)
	assert.Equal(t, "one\n", readFile(t, filepath.Join(picked, "a.go")))
	assert.Contains(t, f.waitPromptWith(t, "[Clubmate]", picked), "connected")
	assert.Empty(t, f.session(t, workspace, session).ConnectRequests)
}

func TestConnectAnswerClonesSeveralURLsInTurn(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	source := newRepo(t)
	one, two := filepath.Join(t.TempDir(), "one.git"), filepath.Join(t.TempDir(), "two.git")
	git(t, source, "clone", "--bare", "-q", source, one)
	git(t, source, "clone", "--bare", "-q", source, two)
	first := newRepo(t)
	intoOne, intoTwo := filepath.Join(t.TempDir(), "one"), filepath.Join(t.TempDir(), "two")
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"url": one}, map[string]any{"url": two})

	require.NoError(t, f.answerConnect(t, workspace, res.Request,
		connectAnswer{"path": one, "action": "other", "otherPath": intoOne},
		connectAnswer{"path": two, "action": "other", "otherPath": intoTwo},
	))

	testutil.Eventually(t, func() bool { return len(f.folders(t, workspace)) == 3 }, wait)
	assert.Equal(t, []string{first, intoOne, intoTwo}, f.folderPaths(t, workspace))
	assert.NotContains(t, f.waitPromptWith(t, "[Clubmate]", intoTwo), "failed")
}

func TestPendingConnectKeepsTheSessionOpen(t *testing.T) {
	t.Parallel()
	f := startEnv(t, []string{"CLUBMATE_AGENTS_CLOSE_AFTER=300ms"}, map[string]any{},
		testutil.BuildPluginDir(t, "clubmate/plugins/git", "plugins/git/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/cron", "plugins/cron/plugin.json"),
		testutil.BuildPluginDir(t, "clubmate/plugins/triggers", "plugins/triggers/plugin.json"))
	first, other := newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.newSession(t, workspace)
	require.NoError(t, f.runFrom(t, workspace, session, "agents.connect-repo", map[string]any{"repos": []map[string]any{{"path": other}}}, nil))
	f.prompt(t, workspace, session, "work")
	f.waitStatus(t, workspace, session, "done")
	loner := f.doneSession(t, workspace)

	f.waitStatus(t, workspace, loner, "closed")

	assert.Equal(t, "done", f.session(t, workspace, session).Status)
}

func TestConnectCardAnswerClosesTheRequest(t *testing.T) {
	t.Parallel()
	f := startGit(t, map[string]any{})
	first, ticked, unticked := newRepo(t), newRepo(t), newRepo(t)
	workspace := f.workspace(t, first)
	session := f.doneSession(t, workspace)
	res := f.connect(t, workspace, session, map[string]any{"path": ticked}, map[string]any{"path": unticked})
	value := map[string]any{"request": res.Request, "answers": []connectAnswer{{"path": ticked, "action": "connect"}, {"path": unticked, "action": "skip"}}}

	require.NoError(t, f.cardEvent(t, workspace, session+"/connect/"+res.Request, "connectAnswer", value))

	assert.Equal(t, []string{first, ticked}, f.folderPaths(t, workspace))
	assert.Empty(t, f.session(t, workspace, session).ConnectRequests)
}
