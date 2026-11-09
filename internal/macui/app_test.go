package macui_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type instance struct {
	t         *testing.T
	name      string
	dir       string
	cli       string
	repo      string
	workspace string
}

type element struct {
	ID     string     `json:"id"`
	Role   string     `json:"role"`
	Label  string     `json:"label"`
	Parent string     `json:"parent"`
	Frame  rect       `json:"frame"`
	Menu   []menuItem `json:"menu"`
}

type rect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type menuItem struct {
	Title    string     `json:"title"`
	Enabled  bool       `json:"enabled"`
	Checked  bool       `json:"checked"`
	Children []menuItem `json:"children"`
}

type window struct {
	Kind      string `json:"kind"`
	Title     string `json:"title"`
	Layout    string `json:"layout"`
	Key       bool   `json:"key"`
	Focus     string `json:"focus"`
	Responder string `json:"responder"`
	elements  []element
}

type snapshot struct {
	Windows []window `json:"windows"`
}

var root = func() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}()

func appPath(t *testing.T) string {
	t.Helper()
	app := os.Getenv("CLUBMATE_TEST_APP")
	if app == "" {
		t.Skip("CLUBMATE_TEST_APP is not set; make mac-ui-test runs these against the built app")
	}
	return app
}

func start(t *testing.T, name, script string, args ...string) *instance {
	t.Helper()
	app := appPath(t)
	work := filepath.Join(root, "build", "mac-ui", name)
	require.NoError(t, os.RemoveAll(work))
	repo := filepath.Join(work, "repo")
	require.NoError(t, os.MkdirAll(repo, 0o755))
	for _, cmd := range [][]string{{"init", "-q", "-b", "main"}, {"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"}} {
		require.NoError(t, exec.Command("git", append([]string{"-C", repo}, cmd...)...).Run())
	}
	env := []string{"CLUBMATE_AGENTS_COMMAND=" + filepath.Join(root, "build", "mac-ui", "fakeagent")}
	if script != "" {
		path := filepath.Join(work, "script.json")
		require.NoError(t, os.WriteFile(path, []byte(script), 0o600))
		env = append(env, "FAKEAGENT_SCRIPT="+path)
	}
	in := &instance{t: t, name: name, dir: "/tmp/clubmate-test-" + name, cli: filepath.Join(app, "Contents", "Resources", "bin", "clubmate"), repo: repo}
	in.script(env, append([]string{"stop", name}, nil...)...)
	out, err := in.script(env, append([]string{"start", name, app, "--"}, args...)...)
	require.NoError(t, err, string(out))
	t.Cleanup(func() {
		if out, err := in.script(nil, "stop", name); err != nil {
			t.Logf("stop %s: %v %s", name, err, out)
		}
	})
	var opened struct {
		ID string `json:"id"`
	}
	in.json(&opened, "workspace", "open", repo)
	in.workspace = opened.ID
	require.NoError(t, os.WriteFile(filepath.Join(in.dir, "workspace"), []byte(opened.ID+"\n"), 0o600))
	in.eventually("the scratch workspace is shown", func() bool {
		w := in.snapshot().main()
		return w != nil && strings.HasPrefix(w.Title, "repo ")
	})
	return in
}

func (in *instance) at(t *testing.T) *instance {
	c := *in
	c.t = t
	return &c
}

func (in *instance) terminals() int {
	in.t.Helper()
	var list []struct {
		Owner       string `json:"owner"`
		WorkspaceID string `json:"workspaceId"`
	}
	in.json(&list, "session", "list")
	n := 0
	for _, s := range list {
		if s.Owner == "terminals" && s.WorkspaceID == in.workspace {
			n++
		}
	}
	return n
}

func (in *instance) script(env []string, args ...string) ([]byte, error) {
	cmd := exec.Command(filepath.Join(root, "scripts", "test-app.sh"), args...)
	cmd.Env = append(cleanEnv(), env...)
	return cmd.CombinedOutput()
}

func cleanEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "CLUBMATE_") || strings.HasPrefix(kv, "FAKEAGENT_")
	})
}

func (in *instance) run(args ...string) ([]byte, error) {
	cmd := exec.Command(in.cli, args...)
	cmd.Env = append(cleanEnv(), "CLUBMATE_STATE_DIR="+in.dir+"/state", "CLUBMATE_RUNTIME_DIR="+in.dir+"/run", "CLUBMATE_WORKSPACE_ID="+in.workspace)
	return cmd.CombinedOutput()
}

func (in *instance) cm(args ...string) string {
	in.t.Helper()
	out, err := in.run(args...)
	require.NoError(in.t, err, "cm %s: %s", strings.Join(args, " "), out)
	return string(out)
}

func (in *instance) json(v any, args ...string) {
	in.t.Helper()
	require.NoError(in.t, json.Unmarshal([]byte(in.cm(append([]string{"--json"}, args...)...)), v))
}

func (in *instance) key(keys ...string) []string {
	in.t.Helper()
	var res struct {
		Routes []string `json:"routes"`
	}
	in.json(&res, append([]string{"ui", "key"}, keys...)...)
	return res.Routes
}

func (in *instance) pointer(args ...string) pointerResult {
	in.t.Helper()
	var res pointerResult
	in.json(&res, append([]string{"ui"}, args...)...)
	return res
}

type pointerResult struct {
	View string     `json:"view"`
	Menu []menuItem `json:"menu"`
}

func (in *instance) focusSection(section, slot string) {
	in.t.Helper()
	in.cm("command", "run", "ui.focus", "section="+section, "slot="+slot)
	in.eventually("section "+section+" is shown", func() bool { return in.snapshot().main().has(slot) || in.snapshot().main().hasPrefix(section) })
}

func (in *instance) snapshot() snapshot {
	in.t.Helper()
	out := filepath.Join(in.dir, "snapshots")
	var snap snapshot
	in.json(&snap, "ui", "snapshot", "--no-png", "--out", out)
	for i := range snap.Windows {
		data, err := os.ReadFile(snap.Windows[i].Layout)
		require.NoError(in.t, err)
		require.NoError(in.t, json.Unmarshal(data, &snap.Windows[i].elements))
	}
	return snap
}

func (s snapshot) main() *window {
	for i := range s.Windows {
		if s.Windows[i].Kind == "window" {
			return &s.Windows[i]
		}
	}
	return nil
}

func (s snapshot) key() *window {
	for i := range s.Windows {
		if s.Windows[i].Key {
			return &s.Windows[i]
		}
	}
	return nil
}

func (s snapshot) kind(kind string) *window {
	for i := range s.Windows {
		if s.Windows[i].Kind == kind {
			return &s.Windows[i]
		}
	}
	return nil
}

func (w *window) find(match func(element) bool) *element {
	if w == nil {
		return nil
	}
	for i := range w.elements {
		if match(w.elements[i]) {
			return &w.elements[i]
		}
	}
	return nil
}

func (w *window) node(id string) *element {
	return w.find(func(e element) bool { return e.ID == id })
}

func (w *window) has(id string) bool { return w.node(id) != nil }

func (w *window) hasPrefix(prefix string) bool {
	return w.find(func(e element) bool { return strings.HasPrefix(e.ID, prefix) }) != nil
}

func (w *window) label(role, label string) *element {
	return w.find(func(e element) bool { return e.Role == role && e.Label == label })
}

func (in *instance) eventually(what string, ok func() bool) {
	in.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			in.t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

type session struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Finish string `json:"finish"`
}

func (in *instance) sessions() []session {
	in.t.Helper()
	var list []session
	in.json(&list, "command", "run", "agents.sessions")
	return list
}

func (in *instance) added(before []session) session {
	in.t.Helper()
	var added session
	in.eventually("a new session appears", func() bool {
		for _, s := range in.sessions() {
			if !slices.ContainsFunc(before, func(b session) bool { return b.ID == s.ID }) {
				added = s
				return true
			}
		}
		return false
	})
	return added
}

func (in *instance) focusIn(id string) {
	in.t.Helper()
	var last *window
	in.eventually("the caret is in "+id+"'s prompt", func() bool {
		last = in.snapshot().key()
		return focusedSession(last) == id
	})
}

var composerID = regexp.MustCompile(`^(agt_[0-9a-f]+)/composer/0$`)

func focusedSession(w *window) string {
	if w == nil {
		return ""
	}
	if m := composerID.FindStringSubmatch(w.Focus); m != nil {
		return m[1]
	}
	return ""
}

func describe(w *window) string {
	if w == nil {
		return "no window"
	}
	return fmt.Sprintf("%s %q focus=%s responder=%s", w.Kind, w.Title, w.Focus, w.Responder)
}
