package macui_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const removeScript = `{"prompt": [{"match": "Remove", "run": "\"%s\" command run --agent git.worktree-remove repo=\"$PWD\" path=\"$PWD/.worktrees/feat-multi-repo-landing\""}]}`

func (in *instance) alert() *window {
	for _, w := range in.snapshot().Windows {
		if w.Key && w.node("alert.detail") != nil {
			return &w
		}
	}
	return nil
}

func (in *instance) sendReport() <-chan string {
	done := make(chan string, 1)
	go func() {
		out, _ := in.run("--json", "command", "run", "report.send", "text=Broken")
		done <- string(out)
	}()
	in.eventually("the report sheet is the key window", func() bool { return in.snapshot().key().node("report.text") != nil })
	return done
}

func cancelled(t *testing.T, reply <-chan string) {
	t.Helper()
	select {
	case out := <-reply:
		assert.JSONEq(t, `{"sent": false, "reason": "cancelled"}`, out)
	case <-time.After(15 * time.Second):
		t.Fatal("report.send did not return")
	}
}

func TestApprovalAlertAndReportSheetKeys(t *testing.T) {
	cli := filepath.Join(appPath(t), "Contents", "Resources", "bin", "clubmate")
	in := start(t, "macui-approval", fmt.Sprintf(removeScript, cli), "-NSTreatUnknownArgumentsAsOpen", "NO", "--test-approvals")
	require.NoError(t, exec.Command("git", "-C", in.repo, "worktree", "add", "-q", "-b", "feat/multi-repo-landing", filepath.Join(in.repo, ".worktrees", "feat-multi-repo-landing")).Run())

	t.Run("long hyphenated paths are shown in full", func(t *testing.T) {
		in := in.at(t)
		in.cm("ui", "present", "approval-paths")
		var alert *window
		in.eventually("the alert is the key window", func() bool { alert = in.alert(); return alert != nil })
		facts := alert.node("alert.facts")
		assert.Equal(t, "text", facts.Role, "the facts label is as tall as its wrapped lines")
		assert.True(t, strings.HasSuffix(facts.Label, "/repos/ads-management"))
		assert.Equal(t, []string{"alert"}, in.key("esc"))
		in.eventually("esc denies", func() bool { return in.alert() == nil })
	})
	t.Run("the alert closes when the agent's call is gone", func(t *testing.T) {
		in := in.at(t)
		worktree := filepath.Join(in.repo, ".worktrees", "feat-multi-repo-landing")
		call := in.command("command", "run", "--agent", "git.worktree-remove", "repo="+in.repo, "path="+worktree)
		require.NoError(t, call.Start())
		in.eventually("the approval alert is the key window", func() bool { return in.alert() != nil })
		require.NoError(t, call.Process.Kill())
		_ = call.Wait()
		in.eventually("the alert closes", func() bool { return in.alert() == nil })
		assert.DirExists(t, worktree)
	})
	t.Run("the report sheet closes when the agent's call is gone", func(t *testing.T) {
		in := in.at(t)
		call := in.command("command", "run", "report.send", "text=Broken")
		require.NoError(t, call.Start())
		in.eventually("the report sheet is the key window", func() bool { return in.snapshot().key().node("report.text") != nil })
		require.NoError(t, call.Process.Kill())
		_ = call.Wait()
		in.eventually("the sheet closes with a banner", func() bool {
			snap := in.snapshot()
			banner := snap.main().node("banner")
			return snap.key().node("report.text") == nil && banner != nil && strings.HasPrefix(banner.Label, "The agent stopped waiting")
		})
	})
	t.Run("esc cancels the report with the text box focused", func(t *testing.T) {
		in := in.at(t)
		reply := in.sendReport()
		assert.Equal(t, "report.text", in.snapshot().key().Focus)
		in.key("esc")
		cancelled(t, reply)
	})
	t.Run("an agent's approval opens over the report sheet and both take esc", func(t *testing.T) {
		in := in.at(t)
		reply := in.sendReport()
		in.cm("agent", "spawn", "Remove the worktree")
		var alert *window
		in.eventually("the approval alert is the key window", func() bool { alert = in.alert(); return alert != nil })
		assert.Contains(t, alert.node("alert.facts").Label, "feat-multi-repo-landing")
		assert.Regexp(t, `\nFrom: Remove the worktree \(agt_[0-9a-f]+\)$`, alert.node("alert.detail").Label)
		assert.Equal(t, "text", alert.node("alert.facts").Role)
		in.key("esc")
		in.eventually("esc denies and the report sheet is key again", func() bool { return in.snapshot().key().node("report.text") != nil })
		in.key("esc")
		cancelled(t, reply)
		assert.DirExists(t, filepath.Join(in.repo, ".worktrees", "feat-multi-repo-landing"))
	})
}
