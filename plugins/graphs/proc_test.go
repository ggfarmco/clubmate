package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runShell(t *testing.T, shell string, timeout time.Duration) (stepResult, string) {
	t.Helper()
	dir := t.TempDir()
	out := filepath.Join(dir, "out", "step.log")
	res := shellRunner{grace: 200 * time.Millisecond}.Run(t.Context(), stepSpec{Dir: dir, Shell: shell, Env: []string{"PATH=" + os.Getenv("PATH")}, Timeout: timeout, OutPath: out}, func(int) {})
	return res, dir
}

func TestShellRunnerExitCodes(t *testing.T) {
	tests := []struct {
		shell  string
		code   int
		tail   string
		reason string
	}{
		{"exit 0", 0, "", ""},
		{"echo hi; exit 3", 3, "hi", ""},
		{"echo not found; nosuchcmd-xyz", 127, "", ""},
		{"kill -9 $$", exitUnknown, "", "killed by signal 9"},
	}
	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			res, _ := runShell(t, tt.shell, 5*time.Second)
			assert.Equal(t, tt.code, res.ExitCode)
			assert.Equal(t, tt.reason, res.Reason)
			if tt.tail != "" {
				assert.Equal(t, tt.tail, res.Tail)
			}
		})
	}
}

func TestShellRunnerTimeoutKillsGroup(t *testing.T) {
	res, dir := runShell(t, "sleep 30 & echo $! > child; wait", 5*time.Second)
	assert.Equal(t, "timed out", res.Reason)
	data, err := os.ReadFile(filepath.Join(dir, "child"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }, 5*time.Second, 20*time.Millisecond)
}

func TestShellRunnerCapsAndCleansOutput(t *testing.T) {
	res, dir := runShell(t, `head -c 2097152 /dev/zero | tr '\0' 'a'; printf '\377\376end'`, 10*time.Second)
	info, err := os.Stat(filepath.Join(dir, "out", "step.log"))
	require.NoError(t, err)
	assert.LessOrEqual(t, info.Size(), int64(outputCap+64))
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	data, err := os.ReadFile(filepath.Join(dir, "out", "step.log"))
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(string(data), "… cut at 1 MB\n"))
	assert.True(t, utf8.ValidString(res.Tail))
	assert.LessOrEqual(t, utf8.RuneCountInString(res.Tail), outputTail)
	assert.True(t, strings.HasSuffix(res.Tail, "end"))
}

func TestShellRunnerExactCapIsNotCut(t *testing.T) {
	_, dir := runShell(t, `head -c 1048576 /dev/zero | tr '\0' 'a'`, 10*time.Second)
	data, err := os.ReadFile(filepath.Join(dir, "out", "step.log"))
	require.NoError(t, err)
	assert.Len(t, data, outputCap)
}

func TestShellRunnerTailDropsControlChars(t *testing.T) {
	res, _ := runShell(t, `printf '\033[31mred\033[0m \033]0;title\007\a\001ok\tx\r\nend'`, 5*time.Second)
	assert.Equal(t, "red ok\tx\nend", res.Tail)
}

func TestTailNeverStartsInsideEscape(t *testing.T) {
	out, err := openOutput(filepath.Join(t.TempDir(), "step.log"))
	require.NoError(t, err)
	defer out.close()
	for range 2000 {
		_, _ = out.Write([]byte("\x1b[31mrr\x1b[0m\n"))
	}
	assert.Regexp(t, `^(rr\n)*rr$`, out.tailText())
}

func TestShellRunnerStopsOnContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	res := shellRunner{grace: 200 * time.Millisecond}.Run(ctx, stepSpec{Dir: t.TempDir(), Shell: "sleep 30", Timeout: time.Minute, OutPath: filepath.Join(t.TempDir(), "x.log")}, func(int) {})
	assert.Equal(t, "stopped", res.Reason)
	assert.Less(t, time.Since(start), time.Second)
}

func TestShellRunnerBackgroundChildDoesNotHang(t *testing.T) {
	start := time.Now()
	res, dir := runShell(t, "sleep 30 & echo $! > child; echo started", 10*time.Second)
	assert.Less(t, time.Since(start), 3*time.Second)
	assert.Equal(t, 0, res.ExitCode)
	assert.Empty(t, res.Reason)
	assert.Equal(t, "started", res.Tail)
	data, err := os.ReadFile(filepath.Join(dir, "child"))
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) }, 5*time.Second, 20*time.Millisecond)
}

func TestShellRunnerCountsEveryLine(t *testing.T) {
	res, _ := runShell(t, `seq 1 5000; printf end`, 5*time.Second)
	assert.Equal(t, 5001, res.Lines)
}

func TestKillGroupSparesReusedPID(t *testing.T) {
	cmd := exec.Command("sleep", "300")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})
	killGroup(leftover{pid: cmd.Process.Pid, start: time.Unix(1, 0)}, 100*time.Millisecond)
	select {
	case <-exited:
		t.Fatal("killed a process whose start time differs")
	case <-time.After(200 * time.Millisecond):
	}
}
