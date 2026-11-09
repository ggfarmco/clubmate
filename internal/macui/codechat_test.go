package macui_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t"}, args...)...).CombinedOutput()
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

func TestCodeChatQuestionCardSendKey(t *testing.T) {
	in := start(t, "macui-codechat", askScript)
	file := filepath.Join(in.repo, "f.txt")
	require.NoError(t, os.WriteFile(file, []byte("one\ntwo\n"), 0o600))
	gitRun(t, in.repo, "add", "f.txt")
	gitRun(t, in.repo, "commit", "-qm", "f")
	require.NoError(t, os.WriteFile(file, []byte("one\nTWO\n"), 0o600))

	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Ask two things")
	sid := spawned.Session
	card := sid + "/elicitation/elicitation-1"
	in.cm("command", "run", "ui.focus", "section=editor-nvim", "slot=editor-nvim.sidebar")
	in.cm("ui", "event", "--workspace="+in.workspace, "--slot=editor-nvim.sidebar", "--node=code/session", "--action=codeSelect", `--value="`+sid+`"`)
	in.cm("command", "run", "editor-nvim.diffview", "cwd="+in.repo, "rev="+gitRun(t, in.repo, "rev-parse", "HEAD"))
	nrev := exec.Command(filepath.Join(appPath(t), "Contents", "Resources", "tools", "nrev"), "add", "--file", "f.txt", "--line-start", "2", "--type", "issue", "--text", "fix it")
	nrev.Dir = in.repo
	out, err := nrev.CombinedOutput()
	require.NoError(t, err, string(out))
	in.eventually("the card and Send review show", func() bool {
		w := in.snapshot().main()
		return w.has(card+"/submit") && w.has("status/send")
	})
	waiting := func() bool {
		for _, s := range in.sessions() {
			if s.ID == sid {
				return s.Status == "waiting"
			}
		}
		return false
	}
	sendSheet := func() bool { return in.snapshot().key().node("review/sendSheet/note") != nil }

	t.Run("cmd+return from Neovim opens Send review and the card waits", func(t *testing.T) {
		in := in.at(t)
		in.cm("command", "run", "ui.focus", "section=editor-nvim", "slot=editor-nvim.main")
		in.eventually("Neovim has focus", func() bool { return in.snapshot().key().Responder == "TerminalSurfaceView" })
		in.key("cmd+return")
		in.eventually("the Send review sheet is the key window", sendSheet)
		assert.True(t, waiting())
		in.key("esc")
		in.eventually("esc closes it", func() bool { return in.snapshot().kind("sheet") == nil })
	})
	t.Run("cmd+return with nothing picked and focus elsewhere in the chat does nothing", func(t *testing.T) {
		in := in.at(t)
		in.key("--focus", "code/session", "cmd+return")
		assert.Equal(t, "code/session", in.snapshot().key().Focus)
		assert.Nil(t, in.snapshot().kind("sheet"))
		assert.True(t, waiting())
	})
	t.Run("cmd+return from the empty composer answers the card, not Send review", func(t *testing.T) {
		in := in.at(t)
		in.cm("command", "run", "ui.focus", "section=editor-nvim", "slot=editor-nvim.sidebar")
		in.focusIn(sid)
		in.pointer("click", card+"/f/0#Global")
		in.eventually("the pick folds the first question", func() bool { return in.snapshot().main().has(card + "/f/0#question") })
		in.key("cmd+return")
		in.eventually("the card is answered", func() bool { return !in.snapshot().main().has(card + "/submit") })
		assert.False(t, waiting())
		assert.Nil(t, in.snapshot().kind("sheet"))
	})
}
