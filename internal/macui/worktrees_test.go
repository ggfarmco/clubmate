package macui_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...)
	cmd.Env = append(cleanEnv(), env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func (in *instance) newRepo(name string) string {
	in.t.Helper()
	repo := filepath.Join(filepath.Dir(in.repo), name)
	require.NoError(in.t, os.MkdirAll(repo, 0o755))
	gitIn(in.t, repo, nil, "init", "-q", "-b", "main")
	gitIn(in.t, repo, nil, "commit", "-q", "--allow-empty", "-m", "init")
	return repo
}

func (in *instance) worktree(repo, branch, date string) string {
	in.t.Helper()
	path := filepath.Join(filepath.Dir(in.repo), "wt", filepath.Base(repo)+"-"+branch)
	gitIn(in.t, repo, nil, "worktree", "add", "-q", "-b", branch, path)
	gitIn(in.t, path, []string{"GIT_AUTHOR_DATE=" + date, "GIT_COMMITTER_DATE=" + date}, "commit", "-q", "--allow-empty", "-m", branch)
	return path
}

func (in *instance) addFolder(path string) {
	in.t.Helper()
	in.cm("workspace", "add-folder", in.workspace, path)
}

func (in *instance) folders() []string {
	in.t.Helper()
	var list []struct {
		ID      string `json:"id"`
		Folders []struct {
			Path string `json:"path"`
		} `json:"folders"`
	}
	in.json(&list, "workspace", "list")
	for _, w := range list {
		if w.ID == in.workspace {
			out := make([]string, 0, len(w.Folders))
			for _, f := range w.Folders {
				out = append(out, f.Path)
			}
			return out
		}
	}
	return nil
}

func (in *instance) sheet() *window {
	in.t.Helper()
	return in.snapshot().kind("sheet")
}

func TestWorktreePicker(t *testing.T) {
	app := start(t, "macui-wt-picker", "")
	older := app.worktree(app.repo, "older", "2026-11-01T10:00:00")
	newer := app.worktree(app.repo, "newer", "2026-11-08T10:00:00")
	app.worktree(app.repo, "mid", "2026-11-04T10:00:00")
	other := app.newRepo("other")
	side := app.worktree(other, "side", "2026-11-03T10:00:00")
	app.addFolder(other)
	row := func(path string) string { return "worktrees#worktree:" + path }
	open := func(in *instance) {
		in.t.Helper()
		in.eventually("the pill shows", func() bool { return in.snapshot().main().has("worktree.pill") })
		in.pointer("click", "worktree.pill")
		in.eventually("a click on the pill opens the picker", func() bool { return in.snapshot().main().has(row(newer)) })
	}
	closed := func(in *instance) func() bool {
		return func() bool { return !in.snapshot().main().has("worktrees") }
	}

	t.Run("hover tints a row", func(t *testing.T) {
		in := app.at(t)
		open(in)
		hovered := func(id string) func() bool {
			return func() bool {
				e := in.snapshot().main().node(id)
				return e != nil && strings.Contains(e.Role, "hovered")
			}
		}
		in.pointer("hover", row(older))
		in.eventually("the hovered row tints", hovered(row(older)))
		in.pointer("hover", row(newer))
		in.eventually("the tint follows the pointer", hovered(row(newer)))
		assert.False(t, hovered(row(older))())
	})
	t.Run("a click on a row switches and closes", func(t *testing.T) {
		in := app.at(t)
		in.pointer("click", row(older))
		in.eventually("the picker closes", closed(in))
		in.eventually("the pill names the picked worktree", func() bool {
			return strings.HasSuffix(in.snapshot().main().node("worktree.pill").Label, "▸ older")
		})
	})
	t.Run("a click outside closes", func(t *testing.T) {
		in := app.at(t)
		open(in)
		in.pointer("click", "editor-nvim.main/surface", "--at", "1000,600")
		in.eventually("the picker closes", closed(in))
	})
	t.Run("a click on the pill closes an open picker and it stays closed", func(t *testing.T) {
		in := app.at(t)
		open(in)
		in.pointer("click", "worktree.pill")
		in.eventually("the picker closes", closed(in))
		time.Sleep(500 * time.Millisecond)
		assert.True(t, closed(in)(), "the same click must not reopen it")
	})
	t.Run("the folder ⋯ lists its actions", func(t *testing.T) {
		in := app.at(t)
		open(in)
		in.pointer("click", "worktrees.more:"+other)
		var items []string
		in.eventually("the folder actions show", func() bool {
			items = nil
			for _, e := range in.snapshot().main().elements {
				if item, ok := strings.CutPrefix(e.ID, "worktrees.folder#"); ok {
					items = append(items, item)
				}
			}
			return len(items) > 0
		})
		assert.ElementsMatch(t, []string{"Reveal in Finder", "Clean up worktrees…", "Remove from repo…"}, items)
		in.key("esc")
		in.eventually("esc closes the actions, not the picker", func() bool {
			w := in.snapshot().main()
			return !w.hasPrefix("worktrees.folder#") && w.has("worktrees")
		})
		in.key("esc")
		in.eventually("a second esc closes the picker", closed(in))
	})
	prompt := func(in *instance, action string) *element {
		in.t.Helper()
		open(in)
		in.pointer("click", "worktrees.actions#"+action)
		var p *element
		in.eventually(action+" opens its dialog", func() bool {
			p = in.sheet().node("prompt")
			return p != nil
		})
		assert.True(in.t, closed(in)())
		return p
	}
	dismiss := func(in *instance) {
		in.t.Helper()
		in.key("esc")
		in.eventually("esc closes the dialog", func() bool { return in.sheet() == nil })
	}
	t.Run("New worktree… asks for the folder first", func(t *testing.T) {
		in := app.at(t)
		p := prompt(in, "New worktree…")
		assert.Equal(t, "New Worktree in", p.Label)
		assert.Equal(t, []string{"repo", "other"}, titles(p.Menu))
		dismiss(in)
	})
	t.Run("Remove worktree… lists the current worktree first, then the newest commit", func(t *testing.T) {
		in := app.at(t)
		p := prompt(in, "Remove worktree…")
		assert.Equal(t, "Remove Worktree", p.Label)
		assert.Equal(t, []string{"repo ▸ older", "repo ▸ newer", "repo ▸ mid", "other ▸ side"}, titles(p.Menu))
		dismiss(in)
		assert.DirExists(t, older)
	})
	t.Run("Add a folder… closes the picker", func(t *testing.T) {
		in := app.at(t)
		open(in)
		in.pointer("click", "worktrees.actions#Add a folder to repo…")
		in.eventually("the picker closes", closed(in))
		assert.Nil(t, in.sheet(), "its NSOpenPanel is skipped in test mode")
		assert.Len(t, in.folders(), 2)
	})
	t.Run("Clean up worktrees… narrows to the picked folder", func(t *testing.T) {
		in := app.at(t)
		open(in)
		in.pointer("click", "worktrees.actions#Clean up worktrees…")
		in.eventually("the sheet shows", func() bool { return in.sheet() != nil })
		byHand := func() string {
			e := in.sheet().find(func(e element) bool { return strings.HasPrefix(e.Label, "**Made by hand**") })
			if e == nil {
				return ""
			}
			return e.Label
		}
		assert.Equal(t, "**Made by hand** · 3", byHand())
		menu := in.pointer("click", "cleanupSheet/folder.menu").Menu
		require.Contains(t, titles(menu), "other")
		in.pointer("menu", "cleanupSheet/folder.menu", "other")
		in.eventually("only other's worktree is left", func() bool { return byHand() == "**Made by hand** · 1" })
		assert.True(t, in.sheet().has("cleanupSheet/tick/"+side))
		in.pointer("menu", "cleanupSheet/folder.menu", "repo")
		in.eventually("repo's two worktrees are listed", func() bool { return byHand() == "**Made by hand** · 2" })
		assert.False(t, in.sheet().has("cleanupSheet/tick/"+side))
		dismiss(in)
	})
	t.Run("Remove from repo… sheet: esc keeps the folder, cmd+return removes it", func(t *testing.T) {
		in := app.at(t)
		ask := func() {
			open(in)
			in.pointer("click", "worktrees.more:"+other)
			in.eventually("the folder actions show", func() bool { return in.snapshot().main().has("worktrees.folder#Remove from repo…") })
			in.pointer("click", "worktrees.folder#Remove from repo…")
			in.eventually("the Remove folder sheet is the key window", func() bool {
				w := in.sheet()
				return w != nil && w.Key && w.has("folderSheet/remove")
			})
		}
		ask()
		in.key("esc")
		in.eventually("esc closes the sheet", func() bool { return in.sheet() == nil })
		assert.Len(t, in.folders(), 2)
		ask()
		in.key("cmd+return")
		in.eventually("cmd+return removes the folder", func() bool { return len(in.folders()) == 1 })
		in.eventually("the sheet closes", func() bool { return in.sheet() == nil })
		assert.DirExists(t, other)
	})
}

func (in *instance) tryAs(caller string, args ...string) (string, error) {
	cmd := exec.Command(in.cli, args...)
	cmd.Env = append(cleanEnv(), "CLUBMATE_STATE_DIR="+in.dir+"/state", "CLUBMATE_RUNTIME_DIR="+in.dir+"/run", "CLUBMATE_WORKSPACE_ID="+in.workspace, "CLUBMATE_AGENT_SESSION="+caller)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (in *instance) runAs(caller string, args ...string) string {
	in.t.Helper()
	out, err := in.tryAs(caller, args...)
	require.NoError(in.t, err, "cm %s as %s: %s", strings.Join(args, " "), caller, out)
	return out
}

func (in *instance) connect(caller string, repos ...string) {
	in.t.Helper()
	list := make([]map[string]string, 0, len(repos))
	for _, r := range repos {
		list = append(list, map[string]string{"path": r, "reason": "needs it", "landsBy": "merge", "source": "test"})
	}
	data, err := json.Marshal(map[string]any{"repos": list})
	require.NoError(in.t, err)
	in.runAs(caller, "command", "run", "--agent", "agents.connect-repo", "--args", string(data))
}

func (in *instance) idleSession(prompt string) string {
	in.t.Helper()
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", prompt)
	in.eventually("the session's turn ends", func() bool {
		for _, s := range in.sessions() {
			if s.ID == spawned.Session {
				return s.Status != "running" && s.Status != "starting"
			}
		}
		return false
	})
	return spawned.Session
}

func TestAFKFolderRemoveSheet(t *testing.T) {
	in := start(t, "macui-wt-afk", "")
	ext := in.newRepo("ext")
	in.focusSection("agents", "agents.sidebar")
	session := in.idleSession("Hello")
	in.cm("command", "run", "agents.afk", "session="+session, "on=true")
	in.connect(session, ext)
	in.eventually("an AFK connect adds the folder by itself", func() bool { return len(in.folders()) == 2 })
	button := session + "/folderRemove/" + ext
	in.eventually("the AFK row offers Remove from repo…", func() bool {
		e := in.snapshot().main().node(button)
		return e != nil && e.Label == "Remove from repo…"
	})
	ask := func() {
		in.pointer("click", button)
		in.eventually("Remove from repo… opens the Remove folder sheet", func() bool {
			w := in.sheet()
			return w != nil && w.Key && w.has("folderSheet/remove")
		})
	}
	closed := func() bool { return in.sheet() == nil }

	ask()
	in.key("esc")
	in.eventually("esc closes the sheet", closed)
	assert.Len(t, in.folders(), 2)
	ask()
	in.pointer("click", "folderSheet/cancel")
	in.eventually("Cancel closes the sheet", closed)
	assert.Len(t, in.folders(), 2)
	assert.True(t, in.snapshot().main().has(button), "the folder stays, so the row still offers to remove it")
}

func TestConnectCardAndComposer(t *testing.T) {
	app := start(t, "macui-wt-connect", "")
	app.focusSection("agents", "agents.sidebar")
	cardRows := func(in *instance, names ...string) []element {
		var out []element
		for _, e := range in.snapshot().main().elements {
			if strings.Contains(e.ID, "/connect/") && strings.Contains(e.ID, "#") && slices.Contains(names, e.Label) {
				out = append(out, e)
			}
		}
		return out
	}
	roles := func(rows []element, role string) int {
		n := 0
		for _, r := range rows {
			if slices.Contains(strings.Fields(r.Role), role) {
				n++
			}
		}
		return n
	}
	sent := func(in *instance, session, text string) {
		in.t.Helper()
		in.eventually("the prompt reaches the session as "+text, func() bool {
			return strings.Contains(in.cm("--json", "command", "run", "agents.transcript", "session="+session), text)
		})
	}

	t.Run("a click on the composer takes the keys from the card", func(t *testing.T) {
		in := app.at(t)
		session := in.idleSession("Hello")
		in.connect(session, in.newRepo("conn-a"), in.newRepo("conn-b"))
		in.eventually("the card shows and takes the keys", func() bool {
			rows := cardRows(in, "conn-a", "conn-b")
			return len(rows) == 2 && roles(rows, "focused") == 1
		})
		in.pointer("click", session+"/composer/0")
		in.eventually("the card lets go", func() bool { return roles(cardRows(in, "conn-a", "conn-b"), "focused") == 0 })
		in.focusIn(session)
		in.key("a", "l", "p", "h", "a", "space", "b", "e", "t", "a")
		assert.Equal(t, 2, roles(cardRows(in, "conn-a", "conn-b"), "selected"), "space must not untick a repo")
		in.key("return")
		sent(in, session, "alpha beta")
	})
	t.Run("a card arriving while a draft is typed leaves the caret in the composer", func(t *testing.T) {
		in := app.at(t)
		session := in.idleSession("Hi again")
		in.key("--focus", session+"/composer/0", "d", "r", "a", "f", "t")
		in.focusIn(session)
		in.connect(session, in.newRepo("conn-c"), in.newRepo("conn-d"))
		in.eventually("the card shows", func() bool { return len(cardRows(in, "conn-c", "conn-d")) == 2 })
		time.Sleep(500 * time.Millisecond)
		assert.Equal(t, session, focusedSession(in.snapshot().key()))
		assert.Zero(t, roles(cardRows(in, "conn-c", "conn-d"), "focused"))
		in.key("space", "m", "o", "r", "e", "return")
		sent(in, session, "draft more")
		assert.Equal(t, 2, roles(cardRows(in, "conn-c", "conn-d"), "selected"))
	})
}

func TestArchiveTaskFromTheRowMenu(t *testing.T) {
	in := start(t, "macui-wt-archive", "")
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "command", "run", "agents.new", "prompt=Plan the change", "finish=merge")
	owner := spawned.Session
	in.eventually("the owner's turn ends", func() bool {
		for _, s := range in.sessions() {
			if s.ID == owner {
				return s.Status == "done"
			}
		}
		return false
	})
	task, err := json.Marshal(map[string]any{"slug": "arch", "repos": []map[string]any{{"path": in.repo, "order": 1, "landsBy": "merge", "source": "test"}}})
	require.NoError(t, err)
	in.runAs(owner, "command", "run", "--agent", "agents.task", "--args", string(task))
	var kid struct {
		Session  string `json:"session"`
		Worktree string `json:"worktree"`
	}
	require.NoError(t, json.Unmarshal([]byte(in.runAs(owner, "--json", "command", "run", "--agent", "agents.new", "repo="+in.repo, "slug=arch", "prompt=work")), &kid))
	in.eventually("the child's worktree exists", func() bool { _, err := os.Stat(kid.Worktree); return err == nil })
	in.eventually("the child's turn ends", func() bool {
		for _, s := range in.sessions() {
			if s.ID == kid.Session {
				return s.Status == "done"
			}
		}
		return false
	})
	require.NoError(t, os.WriteFile(filepath.Join(kid.Worktree, "arch.txt"), []byte("change\n"), 0o600))
	gitIn(t, kid.Worktree, nil, "add", ".")
	gitIn(t, kid.Worktree, nil, "commit", "-qm", "change")
	in.runAs(kid.Session, "command", "run", "--agent", "agents.task-report", "state=ready")
	gitIn(t, in.repo, nil, "merge", "-q", "--no-ff", "-m", "land", "feat/arch")
	in.eventually("the child reports merged once it is its turn to land", func() bool {
		_, err := in.tryAs(kid.Session, "command", "run", "--agent", "agents.task-report", "state=merged")
		return err == nil
	})

	row := "sessions#" + owner
	var menu []menuItem
	in.eventually("the owner's row menu offers Archive Task…", func() bool {
		menu = in.pointer("menu", row, "--context").Menu
		return slices.Contains(titles(menu), "Archive Task…")
	})
	in.pointer("menu", row, "Archive Task…", "--context")
	in.eventually("the Archive sheet is the key window", func() bool {
		w := in.sheet()
		return w != nil && w.Key && w.has("archiveSheet/archive")
	})
	in.key("cmd+return")
	in.eventually("cmd+return archives: the owner closes", func() bool {
		for _, s := range in.sessions() {
			if s.ID == owner {
				return s.Status == "closed"
			}
		}
		return false
	})
	in.eventually("the sheet closes", func() bool { return in.sheet() == nil })
	assert.NoDirExists(t, kid.Worktree)
}
