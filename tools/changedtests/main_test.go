package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChangedPicksOnlyNewOrEditedTestsAcrossRenames(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.invalid")
	write := func(path, body string) {
		full := filepath.Join(repo, path)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	commit := func() string {
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "c"}} {
			out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
			require.NoError(t, err, string(out))
		}
		out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
		require.NoError(t, err)
		return string(out[:len(out)-1])
	}
	out, err := exec.Command("git", "init", "-q", repo).CombinedOutput()
	require.NoError(t, err, string(out))

	write("go.mod", "module x\n")
	write("a/a_test.go", "package a\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {}\n\nfunc TestTwo(t *testing.T) {}\n")
	write("b/b_test.go", "package b\n\nimport \"testing\"\n\nfunc helper() int { return 1 }\n\nfunc TestThree(t *testing.T) { helper() }\n\nfunc TestFour(t *testing.T) {}\n")
	write("sub/go.mod", "module sub\n")
	write("sub/s_test.go", "package sub\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) {}\n")
	base := commit()

	write("a/a_test.go", "package a\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestOne(t *testing.T) { fmt.Println() }\n\nfunc TestTwo(t *testing.T) {}\n")
	require.NoError(t, os.Remove(filepath.Join(repo, "b/b_test.go")))
	write("b/bee_test.go", "package b\n\nimport \"testing\"\n\nfunc helper() int { return 2 }\n\nfunc TestThree(t *testing.T) { helper() }\n\nfunc TestFour(t *testing.T) {}\n\nfunc TestFive(t *testing.T) {}\n")
	write("sub/s_test.go", "package sub\n\nimport \"testing\"\n\nfunc TestSub(t *testing.T) { t.Log() }\n")
	commit()

	got, err := changed(repo, base)
	require.NoError(t, err)
	require.Equal(t, []pkgTests{
		{dir: "a", tests: []string{"TestOne"}},
		{dir: "b", tests: []string{"TestFive"}},
	}, got)
}
