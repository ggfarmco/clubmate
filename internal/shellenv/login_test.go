package shellenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/internal/shellenv"
)

func TestLoginCommand(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	require.NoError(t, os.WriteFile(plain, []byte("#!/bin/sh\n"), 0o600))
	for shell, want := range map[string]string{"/bin/sh": "/bin/sh", "": "/bin/zsh", plain: "/bin/zsh"} {
		t.Setenv("SHELL", shell)
		assert.Equal(t, []string{want, "-l"}, shellenv.LoginCommand(), shell)
	}
}
