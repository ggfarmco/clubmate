package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapturePhoneScript(t *testing.T) {
	script, err := filepath.Abs("../../scripts/design/capture-phone.sh")
	require.NoError(t, err)
	capture := func(t *testing.T, boards ...string) (string, error) {
		args := append([]string{"--tree", t.TempDir(), "--out", t.TempDir(), "--sim", "cm-design-test", "--dry-run"}, boards...)
		out, err := exec.Command(script, args...).CombinedOutput()
		return string(out), err
	}

	t.Run("dry run", func(t *testing.T) {
		out, err := capture(t, "C-Permission-Light", "C-iPad")
		require.NoError(t, err, out)
		assert.Equal(t, "would C-Permission-Light light 9s -macs 3 -open agents/agt_0000000000000001\n"+
			"skipped C-iPad needs a UI-test rotation (judge the committed design/check/companion/C-iPad.png)\n", out)
	})

	t.Run("unknown board", func(t *testing.T) {
		out, err := capture(t, "C-Macs", "C-Nope")
		var exitErr *exec.ExitError
		require.True(t, errors.As(err, &exitErr), out)
		assert.Equal(t, 2, exitErr.ExitCode())
		assert.Equal(t, "capture-phone.sh: unknown board C-Nope\n", out)
	})
}
