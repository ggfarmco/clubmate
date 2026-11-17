package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/testutil"
)

const (
	flagEnv   = "AWAKE_TEST_LID_FLAG"
	holderEnv = "AWAKE_TEST_LID_HOLDER"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == guardArg {
		runGuard(fileShell(os.Getenv(flagEnv)))
		return
	}
	if os.Getenv(holderEnv) != "" {
		if err := fileShell(os.Getenv(flagEnv)).Hold(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println("held")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	os.Exit(m.Run())
}

func TestLidStaysAwakeWhileHeldAndSleepsAtLowBattery(t *testing.T) {
	e := newEnv(t)
	e.pow = power{Laptop: true, OnBattery: true}
	e.a.tick(t.Context())
	_, err := e.run("awake.settings-set", map[string]any{"lid": true})
	require.NoError(t, err)
	assert.True(t, e.a.state().Lid)
	assert.Empty(t, e.lid.writes, "nothing holds the Mac yet")

	e.lid.err = errors.New("denied")
	_, err = e.run("awake.on", nil)
	require.NoError(t, err)
	assert.Equal(t, "Kept awake by you until you turn it off. "+lidFailure+"denied.", e.a.state().Detail)
	e.lid.err = nil
	e.a.tick(t.Context())
	assert.Equal(t, []bool{true}, e.lid.writes)
	assert.Equal(t, "Kept awake by you until you turn it off. "+lidAwake, e.a.state().Detail)
	e.a.tick(t.Context())
	assert.Equal(t, []bool{true, true}, e.lid.writes, "every tick sends it again")

	e.pow.Low = true
	e.a.tick(t.Context())
	assert.Equal(t, []bool{true, true, false}, e.lid.writes)
	require.NotNil(t, e.current())
	assert.Equal(t, "Kept awake by you until you turn it off. "+lidLow, e.a.state().Detail)

	e.pow.Low = false
	e.a.tick(t.Context())
	_, err = e.run("awake.settings-set", map[string]any{"lid": false})
	require.NoError(t, err)
	assert.Equal(t, []bool{true, true, false, true, false}, e.lid.writes)

	_, err = e.run("awake.settings-set", map[string]any{"lid": true})
	require.NoError(t, err)
	e.a.shutdown()
	e.a.tick(t.Context())
	assert.Equal(t, []bool{true, true, false, true, false, true, false}, e.lid.writes, "a tick after shutdown holds nothing")
}

func TestRestartClearsTheLidItLeftHeld(t *testing.T) {
	e := newEnv(t)
	e.pow = power{Laptop: true}
	e.a.tick(t.Context())
	_, err := e.run("awake.settings-set", map[string]any{"lid": true})
	require.NoError(t, err)
	_, err = e.run("awake.on", nil)
	require.NoError(t, err)
	require.Equal(t, []bool{true}, e.lid.writes)

	assert.Equal(t, []bool{false}, newEnvWith(t, e.h).lid.writes, "the restarted plugin reads no laptop yet")
	assert.Empty(t, newEnvWith(t, e.h).lid.writes, "a clean start leaves the lid alone")
}

func TestLidBelongsToOneHolderAtATime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flag")
	holder := func() *clamshell {
		c := fileShell(path)
		c.guardIn = nopCloser{}
		return c
	}
	first, second := holder(), holder()
	require.NoError(t, first.Hold())
	require.NoError(t, second.Free(), "an old guard leaves a newer holder's lid alone")
	require.ErrorIs(t, second.Hold(), errLidBusy)
	require.NoError(t, first.Free())
	require.NoError(t, second.Hold())
	assert.Equal(t, "1\n0\n1\n", readFlag(t, path))
}

func TestGuardWaitsForADyingHolderToLetGo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flag")
	dying, err := tryLock(path + ".lock")
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- fileShell(path).free(testutil.Timeout(10 * time.Second)) }()
	require.NoError(t, dying.Close())
	require.NoError(t, <-done)
	assert.Equal(t, "0\n", readFlag(t, path))
}

func TestGuardFreesTheLidWhenItsPluginIsKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flag")
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), flagEnv+"="+path, holderEnv+"=1")
	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	line, err := bufio.NewReader(stdout).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "held\n", line)
	require.Equal(t, "1\n", readFlag(t, path))

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	testutil.Eventually(t, func() bool { return readFlag(t, path) == "1\n0\n" }, 10*time.Second)
}

func fileShell(path string) *clamshell {
	write := func(disable bool) error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		v := "0\n"
		if disable {
			v = "1\n"
		}
		_, err = f.WriteString(v)
		return errors.Join(err, f.Close())
	}
	return &clamshell{write: write, lockPath: path + ".lock"}
}

type nopCloser struct{}

func (nopCloser) Write(p []byte) (int, error) { return len(p), nil }

func (nopCloser) Close() error { return nil }

func readFlag(t *testing.T, path string) string {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ""
	}
	require.NoError(t, err)
	return string(raw)
}
