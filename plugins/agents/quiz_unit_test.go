package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoGrade(t *testing.T) {
	t.Parallel()
	excerpt := quizSpec{From: 20, Lines: make([]string, 20)}
	line, rng := excerpt, excerpt
	line.Type, line.Key = quizLine, []int{30, 31}
	rng.Type, rng.Key = quizRange, []int{31, 33}
	value := quizSpec{Type: quizValue, Accept: []string{"0.4"}}
	cases := map[string]struct {
		spec quizSpec
		ans  quizAnswer
		want string
		flag string
	}{
		"key line":          {line, quizAnswer{From: 31, To: 31, Confidence: confGuess}, verdictCorrect, ""},
		"range over a key":  {line, quizAnswer{From: 29, To: 30, Confidence: confThink}, verdictClose, ""},
		"other line, sure":  {line, quizAnswer{From: 25, To: 25, Confidence: confSure}, verdictWrong, flagSureWrong},
		"exact range":       {rng, quizAnswer{From: 31, To: 33, Confidence: confSure}, verdictCorrect, ""},
		"overlapping range": {rng, quizAnswer{From: 33, To: 35, Confidence: confSure}, verdictClose, ""},
		"apart range":       {rng, quizAnswer{From: 20, To: 22, Confidence: confThink}, verdictWrong, ""},
		"value as number":   {value, quizAnswer{Text: " .40 ", Confidence: confThink}, verdictCorrect, ""},
		"words to a line":   {line, quizAnswer{Text: "the refill", Confidence: confSure}, verdictPending, ""},
	}
	for name, c := range cases {
		g := autoGrade("q", c.spec, c.ans)
		assert.Equal(t, c.want, g.Verdict, name)
		assert.Equal(t, c.flag, g.Flag, name)
	}
}

func TestQuizKeyMustFitTheType(t *testing.T) {
	t.Parallel()
	base := quizSpec{From: 10, Lines: make([]string, 5)}
	options := []elicitOption{{Value: "a"}, {Value: "b"}}
	cases := map[string]quizArgs{
		"line outside the excerpt": {Type: quizLine, Lines: []int{20}},
		"range of one line":        {Type: quizRange, Lines: []int{11}},
		"range backwards":          {Type: quizRange, Lines: []int{13, 11}},
		"value without accept":     {Type: quizValue},
		"choice with one option":   {Type: quizChoice, Options: options[:1], Accept: []string{"a"}},
		"choice accepts no option": {Type: quizChoice, Options: options, Accept: []string{"c"}},
	}
	for name, args := range cases {
		spec := base
		spec.Type = args.Type
		assert.Error(t, spec.setKey(args), name)
	}
	spec := base
	spec.Type = quizRange
	assert.NoError(t, spec.setKey(quizArgs{Type: quizRange, Lines: []int{11, 13}}))
}

func TestQuizExcerptReadsFromTheWorktreeFolder(t *testing.T) {
	t.Parallel()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	sub := filepath.Join(repo, "sub")
	require.NoError(t, os.MkdirAll(sub, 0o700))
	for _, name := range []string{"a.go", "b.go"} {
		require.NoError(t, os.WriteFile(filepath.Join(sub, name), []byte("one\ntwo\nthree\n"), 0o600))
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		require.NoError(t, exec.Command("git", append([]string{"-C", repo}, args...)...).Run())
	}
	require.NoError(t, os.Remove(filepath.Join(sub, "b.go")))
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(sub, link))

	for name, file := range map[string]string{"relative": "a.go", "absolute through a symlink": filepath.Join(link, "a.go"), "absolute, gone from the work tree": filepath.Join(sub, "b.go")} {
		spec, err := quizSpecOf(t.Context(), link, quizArgs{Type: quizLine, Question: "q", File: file, From: 2, To: 3, Lines: []int{2}})
		require.NoError(t, err, name)
		assert.Equal(t, []string{"two", "three"}, spec.Lines, name)
		assert.Equal(t, filepath.Base(file), spec.File, name)
	}
}
