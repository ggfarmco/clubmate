package bashcmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func words(line string) [][]string {
	commands, _ := Parse(line)
	out := make([][]string, 0, len(commands))
	for _, c := range commands {
		out = append(out, c.Words)
	}
	return out
}

func TestParseReadsCommandsAsTheShellDoes(t *testing.T) {
	t.Parallel()
	cases := map[string][][]string{
		`a "b c" 'd'\ e; f && g | h`:    {{"a", "b c", "d e"}, {"f"}, {"g"}, {"h"}},
		`echo "$(git push)" # x; y`:     {{"echo", "$(git push)"}, {"git", "push"}},
		">/dev/null git push":           {{"git", "push"}},
		"2>&1 git push origin":          {{"git", "push", "origin"}},
		"git >log push 2>/dev/null &>x": {{"git", "push"}},
		"cat < in > $(git push)":        {{"cat"}, {"git", "push"}},
		"diff <(git push) x":            {{"diff", "<(git push)", "x"}, {"git", "push"}},
		"echo x2>f":                     {{"echo", "x2"}},
	}

	for line, want := range cases {
		assert.Equal(t, want, words(line), line)
	}
}

func TestHeredocBodiesAreDataUnlessUnquotedSubstitutions(t *testing.T) {
	t.Parallel()
	commands, ok := Parse("cat <<'EOF' >f\n$(git push)\nEOF\nbash <<-X\n\t`gh pr create`\n\tX\nsh <<< 'ls'")

	assert.True(t, ok)
	assert.Equal(t, [][]string{{"cat"}, {"bash"}, {"gh", "pr", "create"}, {"sh"}}, words("cat <<'EOF' >f\n$(git push)\nEOF\nbash <<-X\n\t`gh pr create`\n\tX\nsh <<< 'ls'"))
	assert.Equal(t, []Heredoc{{Op: "<<'EOF'", Body: "$(git push)", At: 15}}, commands[0].Heredocs)
	assert.Equal(t, "<<-X", commands[1].Heredocs[0].Op)
	assert.Equal(t, []Heredoc{{Op: "<<<", Body: "ls", At: 67}}, commands[3].Heredocs)
}

func TestOffsetsPointAtEachWord(t *testing.T) {
	t.Parallel()
	commands, _ := Parse(`x; 2>&1 sudo  "git" push`)

	assert.Equal(t, []int{0}, commands[0].Offsets)
	assert.Equal(t, 8, commands[1].At)
	assert.Equal(t, []int{8, 14, 20}, commands[1].Offsets)
}

func TestParseGivesUpOnUnfinishedDeepOrHugeLines(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		`git push "x`,
		"echo $(git push",
		"cat <<EOF\nx",
		strings.Repeat("$(", 200) + "git push" + strings.Repeat(")", 200),
		strings.Repeat("echo x; ", 200_000) + "git push",
	} {
		_, ok := Parse(line)
		assert.False(t, ok, line[:min(len(line), 20)])
	}
}
