package main

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
	"clubmate/pkg/pluginsdk"
)

type fakeSetup struct {
	node       string
	brew       string
	installErr error
	in, known  bool
	git        string
}

func (f fakeSetup) probe() setupProbe {
	return setupProbe{
		node: func(context.Context) (string, string) {
			if f.node == "" {
				return "", ""
			}
			return "/opt/homebrew/bin/node", f.node
		},
		brew:    func() string { return f.brew },
		install: func(context.Context, string) (string, error) { return "/acp/index.js", f.installErr },
		login:   func(context.Context, string, string) (bool, bool) { return f.in, f.known },
		git:     func(context.Context) (string, bool) { return f.git, f.git != "" },
	}
}

func TestSetupProbeShowsFirstFailingCheck(t *testing.T) {
	t.Parallel()
	ok := fakeSetup{node: "22.11.0", brew: "/opt/homebrew/bin/brew", in: true, known: true, git: "2.47.1"}
	cases := []struct {
		name   string
		change func(*fakeSetup)
		state  string
		detail string
		fix    setupFix
	}{
		{"node missing with brew", func(f *fakeSetup) { f.node = "" }, setupNodeMissing, nodeNotFound, setupFix{Label: nodeAction, Run: "brew install node@22"}},
		{"node missing without brew", func(f *fakeSetup) { f.node, f.brew = "", "" }, setupNodeMissing, nodeNotFound, setupFix{Label: nodeAction, URL: nodeURL}},
		{"node old", func(f *fakeSetup) { f.node = "18.19.0" }, setupNodeOld, "found 18.19", setupFix{Label: nodeAction, Run: "brew install node@22"}},
		{"signed out before git", func(f *fakeSetup) { f.in, f.git = false, "" }, setupSignedOut, "", setupFix{Label: signInAction, Run: "claude auth login"}},
		{"login unknown", func(f *fakeSetup) { f.in, f.known = false, false }, setupOK, "", setupFix{}},
		{"install failed", func(f *fakeSetup) { f.in, f.installErr = false, errors.New("npm") }, setupOK, "", setupFix{}},
		{"git missing", func(f *fakeSetup) { f.git = "" }, setupGitMissing, gitDetail, setupFix{Label: gitAction, Run: "xcode-select --install"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := ok
			c.change(&f)
			st := f.probe().run(t.Context())
			assert.Equal(t, c.state, st.State)
			assert.Equal(t, c.detail, st.Detail)
			if c.fix.Label == "" {
				assert.Nil(t, st.Fix)
				assert.Empty(t, st.Title)
				return
			}
			require.NotNil(t, st.Fix)
			assert.Equal(t, c.fix, setupFix{Label: st.Fix.Label, Run: st.Fix.Run, URL: st.Fix.URL})
			assert.NotEmpty(t, st.Title)
		})
	}
	st := ok.probe().run(t.Context())
	assert.Equal(t, setupFound{Node: "22.11.0", Git: "2.47.1"}, st.Found)
}

func TestParseLoginKnowsOnlyAnAnswer(t *testing.T) {
	t.Parallel()
	for out, want := range map[string][2]bool{
		`{"loggedIn": true}`:  {true, true},
		`{"loggedIn": false}`: {false, true},
		`{"authMethod": "x"}`: {false, false},
		`error: unknown`:      {false, false},
	} {
		in, known := parseLogin([]byte(out))
		assert.Equal(t, want, [2]bool{in, known}, out)
	}
}

func TestSetupEmptyFollowsState(t *testing.T) {
	t.Parallel()
	ok := setupState{State: setupOK, Found: setupFound{Node: "22.11.0", Git: "2.47.1"}}
	first := pickSessionView(false, ok, true)
	assert.Equal(t, firstRunTitle, first.Props["title"])
	assert.Equal(t, firstRunText, first.Props["text"])
	assert.Equal(t, pickIcon, first.Props["icon"])
	assert.Equal(t, "Claude Code · Node 22.11 · git 2.47", first.Props["note"])
	assert.Equal(t, actNew, first.Children[0].Props["action"])
	assert.Equal(t, checkingText, pickSessionView(false, setupState{State: setupChecking}, true).Props["note"])
	assert.Nil(t, pickSessionView(false, ok, false).Props["note"])

	old := forcedSetup(fakeSetup{brew: "/opt/homebrew/bin/brew"}.probe(), setupNodeOld)
	for _, home := range []bool{false, true} {
		n := pickSessionView(home, old, false)
		assert.Equal(t, nodeTitle, n.Props["title"])
		assert.Equal(t, "Found Node 18.19 at /usr/local/bin/node. "+nodeNeedText, n.Props["text"])
		assert.Equal(t, nodeBrewHint, n.Props["note"])
		assert.Equal(t, setupIcon, n.Props["icon"])
		require.Len(t, n.Children, 1)
		assert.Equal(t, actSetupFix, n.Children[0].Props["action"])
		assert.Equal(t, nodeAction, n.Children[0].Props["label"])
	}
	assert.Equal(t, nodeURLHint, pickSessionView(false, forcedSetup(fakeSetup{}.probe(), setupNodeMissing), true).Props["note"])
	signIn := pickSessionView(false, forcedSetup(fakeSetup{}.probe(), setupSignedOut), true)
	assert.Equal(t, signInEmpty, signIn.Props["title"])
	assert.Equal(t, signInIcon, signIn.Props["icon"])
	git := pickSessionView(false, forcedSetup(fakeSetup{}.probe(), setupGitMissing), true)
	assert.Equal(t, gitText, git.Props["text"])
	require.Len(t, git.Children, 1)
	buttons := git.Children[0].Children
	require.Len(t, buttons, 2)
	assert.Equal(t, actNew, buttons[0].Props["action"])
	assert.Equal(t, "cmd+n", buttons[0].Props["shortcut"])
	assert.Equal(t, actSetupFix, buttons[1].Props["action"])
}

func TestForcedSetupNeverInstalls(t *testing.T) {
	p := fakeSetup{brew: "/opt/homebrew/bin/brew"}.probe()
	for _, state := range []string{setupNodeMissing, setupNodeOld, setupSignedOut, setupGitMissing} {
		st := forcedSetup(p, state)
		require.NotNil(t, st.Fix, state)
		assert.Equal(t, []string{"/usr/bin/true"}, st.Fix.argv, state)
	}
}

func TestInstallBackoffWaitsLongerAfterEachFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), retryFile)
	fail := true
	var banners []bool
	install := func(_ context.Context, _ string, banner bool) (string, error) {
		banners = append(banners, banner)
		if fail {
			return "", errors.New("npm")
		}
		return "/acp/index.js", nil
	}
	restart := func() func(context.Context, string) (string, error) {
		return installBackoff{path: path, now: func() time.Time { return now }}.guard(install)
	}

	_, err := restart()(t.Context(), "node")
	require.Error(t, err)
	_, err = restart()(t.Context(), "node")
	require.ErrorIs(t, err, errInstallWait)
	now = now.Add(retryFirst)
	_, err = restart()(t.Context(), "node")
	require.Error(t, err)
	now = now.Add(retryFirst)
	_, err = restart()(t.Context(), "node")
	require.ErrorIs(t, err, errInstallWait)
	now = now.Add(3 * retryFirst)
	fail = false
	script, err := restart()(t.Context(), "node")
	require.NoError(t, err)
	assert.Equal(t, "/acp/index.js", script)
	assert.NoFileExists(t, path)
	assert.Equal(t, []bool{true, false, false}, banners)
}

func TestSetupFixExitRunsTheCheckAgain(t *testing.T) {
	var runs atomic.Int32
	p := fakeSetup{node: "22.11.0", in: true, known: true, git: "2.47.1"}.probe()
	git := p.git
	p.git = func(ctx context.Context) (string, bool) {
		runs.Add(1)
		return git(ctx)
	}
	a := &app{uiState: newUIState()}
	a.setup.probe = &p
	a.setup.fixes = map[string]bool{"sess_fix": true}
	exited := func(id string) pluginsdk.Event {
		raw, err := json.Marshal(ptyEvent{ID: id, Owner: "terminals"})
		require.NoError(t, err)
		return pluginsdk.Event{Topic: "session.exited", Payload: raw}
	}

	a.setupFixExited(exited("sess_other"))
	testutil.Never(t, func() bool { return runs.Load() > 0 }, 200*time.Millisecond)
	a.setupFixExited(exited("sess_fix"))
	testutil.Eventually(t, func() bool { return runs.Load() == 1 && a.setupState().State == setupOK }, 5*time.Second)
	a.setupFixExited(exited("sess_fix"))
	testutil.Never(t, func() bool { return runs.Load() > 1 }, 200*time.Millisecond)
}
