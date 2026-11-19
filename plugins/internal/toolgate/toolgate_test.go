package toolgate

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clubmate/internal/testutil"
)

func bash(command string) json.RawMessage {
	raw, _ := json.Marshal(map[string]string{"command": command, "description": "d"})
	return raw
}

func TestCommandPresetIsCommandAware(t *testing.T) {
	t.Parallel()
	push := Rule{Tool: "Bash", How: HowCommand, Text: "git push"}
	pr := Rule{Tool: "Bash", How: HowCommand, Text: "gh pr create"}
	match := []string{
		"git push",
		"git commit -m x && git push origin a",
		"cd x && sudo -u bob git -C y push",
		"timeout 30 /usr/bin/git -c a=b push",
		"env A=1 git --git-dir=.git push",
		"if x; then git push; fi",
		"for r in a b; do git push $r; done",
		"{ git push; }",
		"! git push",
		"sudo timeout -s KILL 30 env -i -u X A=1 nohup git push",
		`git -C "$dir" push`,
		`sudo -u "$u" git -c x="a b" push`,
		"if git push; then x; fi",
		"while ! git push; do sleep 1; done",
		`echo "$(git push)"`,
		"git \\\n  push>log",
		`sh -c "git push"`,
		`sudo bash -lc 'cd y && /usr/bin/git -C z push origin'`,
		`eval "git push"`,
		"ls | xargs -I{} git -C {} push",
		claudeCommit + " && git push",
		"echo $(( $(git push) ))",
		`true&&git -C x push "unclosed`,
		"bash /dev/stdin <<< 'git push'",
		"echo ${x:-$(git push)}",
		"(( n = 1 << 2 ))\ngit push",
		">/dev/null git push",
		"2>&1 git push",
		"2>/dev/null git push origin main",
		"git >log push",
		"/usr/bin/env git push",
		"/usr/bin/sudo -u x git push",
		"/usr/bin/time -l git push",
		"bash -euo pipefail -c 'git push origin main'",
		"bash -o pipefail -O extglob -c 'git push'",
		"bash -c 'G=git; $G push'",
		`bash -c 'git "$@"' _ push`,
		`sh -c 'echo "git push"'`,
		"function f { git push; }; f",
		"coproc git push",
		"git --attr-source HEAD push",
		"xargs -J % git push %",
		"env -S 'git push'",
		"cat <<EOF\n$(git push)\nEOF",
		"cat <<EOF\n`git push`\nEOF",
		"sudo -h host git push",
		"env - git push",
		"coproc P { git push; }",
		"function f() { git push; }; f",
	}
	miss := []string{
		"git pushx", "echo git push", "git status --push", "git stash push", `sh -c "git status"`, "bash deploy.sh", "xargs -n1 echo push",
		claudeCommit, `git commit -m "fix; git push later"`, `sh -c "git commit -am 'git push later'"`, "echo 'unterminated",
		"cat cmds.txt | bash", "bash deploy.sh git push", "cat <<'EOF'\n$(git push)\nEOF", "cat <<EOF\n\\$(git push)\nEOF",
		`ls | xargs git commit -m "git push later"`,
		"sh -c 'x' _ git commit -m push", "bash -c 'bash -s' <<EOF\ngit push\nEOF", "sudo - git push",
		"echo $(echo case x in a) git push", "echo $(case x in a) true;; esac) git push", "bash -c 'cat' <<EOF\ngit push\nEOF", "bash script.sh <<EOF\ngit push\nEOF", "bash -c cat <<< 'git push'",
	}

	for _, c := range match {
		assert.True(t, push.Matches("Bash", bash(c)), c)
	}
	for _, c := range miss {
		assert.False(t, push.Matches("Bash", bash(c)), c)
	}
	assert.True(t, pr.Matches("Bash", bash("gh pr create --fill")))
	assert.True(t, pr.Matches("Bash", bash("gh -R o/r pr --repo=o/r create")))
	assert.True(t, pr.Matches("Bash", bash("gh pr -R o/r create")))
	assert.False(t, pr.Matches("Bash", bash("gh pr view")))
	assert.False(t, push.Matches("Read", bash("git push")))
}

const claudeCommit = `git commit -m "$(cat <<'EOF'
git push docs: say when the gate holds

Co-Authored-By: x
EOF
)"`

func TestWrappedOrUnreadLineSaysWhyItIsHeld(t *testing.T) {
	t.Parallel()
	rules := []Rule{{ID: "push", Tool: "Bash", How: HowCommand, Text: "git push"}}
	via := map[string]string{
		"git push":                             "",
		`sh -c "git -C x push"`:                "sh -c",
		`sudo bash -lc 'git push "x'`:          "bash -c",
		`eval "git push"`:                      "eval",
		"xargs -n1 git push":                   "xargs",
		"bash <<'EOF'\ngit push\nEOF":          "bash <<'EOF'",
		"bash <<-SCRIPT\n\tgit push\n\tSCRIPT": "bash <<-SCRIPT",
		`sh -c 'git status; echo git push'`:    "sh -c",
		`git push "unterminated`:               ViaUnread,
		`bash -c -e -- 'git push'`:             "bash -c",
		`bash <<< 'git push'`:                  "bash <<<",
		strings.Repeat("$(", 100) + "git push" + strings.Repeat(")", 100): ViaUnread,
		strings.Repeat("echo; ", 200_000) + "git push":                    ViaUnread,
		`echo "git push" | sh -s x`:                                       "sh",
		`git -C x  push "unterminated`:                                    ViaUnread,
		"echo $((1<<2))\ngit push":                                        "",
		"cat <<EOF\nx\ngit push":                                          ViaUnread,
		"cat <<EOF | bash\ngit push\nEOF":                                 "bash",
		"curl x | sh; git push":                                           "",
		"echo `cat <<EOF\nhi\nEOF`\ngit push":                             "",
		"echo $[1<<2]\ngit push":                                          "",
		"x=$(( $(nproc) << 1 ))\ngit push":                                "",
		`echo "$((1<<2))"; echo ${x:-$((1<<2))}` + "\ngit push":           "",
		"cat > f <<EOF\na ` b\nEOF\ngit push":                             ViaUnread,
		"cat <<EOF\n" + strings.Repeat("$(", 70) + "git push" + strings.Repeat(")", 70) + "\nEOF": ViaUnread,
		"bash $OPTS <<EOF\ngit push\nEOF":                           "bash <<EOF",
		"bash ${DEBUG:+-x} <<EOF\ngit push\nEOF":                    "bash <<EOF",
		"sh $FLAGS <<< 'git push'":                                  "sh <<<",
		`echo "$(case x in esac)"; git push; echo ")"`:              "",
		"echo $(case x\nin x) git push;; esac)":                     "",
		"echo $(time case x in x) git push;; esac)":                 "",
		"echo $(if x; then case y in a) git push;; esac; fi)":       "",
		"echo $(case x in a) true;; b) git push;; esac)":            "",
		"echo $(case x in a) true;& b) git push;; esac)":            "",
		"echo $[ $(git push) ]":                                     "",
		"env " + strings.Repeat("-S env ", 9) + `-S "g'i't push"`:   ViaUnread,
		"env " + strings.Repeat("-S env ", 9) + "-S 'git push'":     ViaUnread,
		"x=$(( `nproc` << 1 ))\ngit push":                           "",
		"echo $(case x in a) git push;; esac)":                      "",
		"case x in x) echo $(case x in (x) git push;; esac);; esac": "",
		`env -S 'bash -c "git push"'`:                               "bash -c",
		"fish --command 'git push'":                                 "fish -c",
	}

	got := map[string]string{}
	for line := range via {
		hits := Hits(rules, "Bash", bash(line))
		if assert.Len(t, hits, 1, line) {
			got[line] = hits[0].Via
		}
	}

	for line := range got {
		if len(line) > 200 {
			assert.Equal(t, ViaUnread, got[line], len(line))
			delete(got, line)
			delete(via, line)
		}
	}
	assert.Equal(t, via, got)
	assert.Empty(t, Hits([]Rule{{Tool: "Bash", How: HowStarts, Text: "git push"}}, "Bash", bash(`git push "unterminated`))[0].Via)
}

func TestARunnerPresetMatchesItsOwnCommand(t *testing.T) {
	t.Parallel()
	rm := Rule{Tool: "Bash", How: HowCommand, Text: "xargs rm"}
	script := Rule{Tool: "Bash", How: HowCommand, Text: "bash deploy.sh"}

	assert.True(t, rm.Matches("Bash", bash("ls | xargs rm")))
	assert.True(t, script.Matches("Bash", bash("bash deploy.sh prod")))
	assert.False(t, script.Matches("Bash", bash(`bash -c "echo deploy.sh"`)))
}

func TestStartsIsPlain(t *testing.T) {
	t.Parallel()
	deploy := Rule{Tool: "Bash", How: HowStarts, Text: "make deploy"}

	for _, c := range []string{
		"FOO=1 make deploy", "time make deploy", "(cd x; make deploy)", "make deploy prod", "echo ok; make deploy 'unclosed",
		"echo `cat <<EOF\nhi\nEOF`\nmake deploy", "echo $[1<<2]\nmake deploy", "x=$(( $(nproc) << 1 ))\nmake deploy",
	} {
		assert.True(t, deploy.Matches("Bash", bash(c)), c)
	}
	assert.True(t, Rule{Tool: "Bash", How: HowStarts, Text: "echo $HOME"}.Matches("Bash", bash("echo $HOME | wc")))
	for _, c := range []string{
		"sudo -u bob make deploy", "/usr/bin/make deploy", "make deployer", `echo "x; make deploy now"`,
		"git commit -m \"$(cat <<'EOF'\nmake deploy works now\nEOF\n)\"", "cat > README.md <<EOF\nmake deploy\nEOF",
	} {
		assert.False(t, deploy.Matches("Bash", bash(c)), c)
	}
}

func TestStartsHoldsALineItCannotReadWhenItMentionsTheCommand(t *testing.T) {
	t.Parallel()
	rules := []Rule{
		{Tool: "Bash", How: HowStarts, Text: "make deploy"},
		{Tool: "Bash", How: HowStarts, Text: "echo $HOME"},
		{Tool: "Bash", How: HowStarts, Text: "./deploy.sh prod"},
	}
	via := func(line string) []string {
		var out []string
		for _, h := range Hits(rules, "Bash", bash(line)) {
			out = append(out, h.Text+" "+h.Via)
		}
		return out
	}

	assert.Equal(t, []string{"make deploy " + ViaUnread}, via(`echo "unclosed; make deploy`))
	assert.Equal(t, []string{"make deploy " + ViaUnread}, via(strings.Repeat("echo; ", 50_000)+"make deploy"))
	assert.Equal(t, []string{"echo $HOME " + ViaUnread}, via(`x "; echo $HOME`))
	assert.Empty(t, via(`echo "unclosed; make test`))
	assert.Equal(t, []string{"make deploy "}, via("make deploy 'unclosed"))
	assert.Equal(t, []string{"./deploy.sh prod " + ViaUnread}, via(strings.Repeat("echo; ", 50_000)+"./deploy.sh prod"))
}

func TestAGateIsAcceptedOnlyWhenItMatchesItsOwnText(t *testing.T) {
	t.Parallel()
	accepted := []string{"make deploy", `echo "a b"`, "./deploy.sh prod"}
	commands := []string{"git push", "bash deploy.sh", "xargs rm"}
	refused := map[[2]string]string{
		{HowStarts, "sudo make install"}:    "make install",
		{HowStarts, "sudo -u bob make"}:     "make",
		{HowStarts, "time -p make deploy"}:  "make deploy",
		{HowStarts, "FOO=1 make deploy"}:    "make deploy",
		{HowStarts, "time"}:                 "",
		{HowCommand, "./deploy.sh prod"}:    "deploy.sh prod",
		{HowCommand, "env A=1 git push"}:    "git push",
		{HowCommand, "sudo -u x reboot"}:    "",
		{HowCommand, "git push --force"}:    "",
		{HowCommand, "rm -rf"}:              "",
		{HowCommand, "gh pr merge --admin"}: "",
		{HowCommand, `git "push"`}:          "",
		{HowCommand, "make deploy;"}:        "",
		{HowCommand, "sh -c"}:               "",
	}

	for _, text := range accepted {
		assert.True(t, Accepts(HowStarts, text), text)
	}
	for _, text := range commands {
		assert.True(t, Accepts(HowCommand, text), text)
	}
	for c, want := range refused {
		assert.False(t, Accepts(c[0], c[1]), c)
		assert.Equal(t, want, Suggest(c[0], c[1]), c)
	}
}

func TestReadingAnyLineEndsInBoundedTime(t *testing.T) {
	t.Parallel()
	alphabet := []string{
		"git", "push", "gh", "pr", "create", "make", "deploy", "env", "-S", "sudo", "-u", "-h", "timeout", "coproc", "function", "time",
		"--", "-I", "-euo", "-o", "exec", "fish", "--command", "/dev/stdin", "-s", "-m", "-R", "-C", "case", "in", "esac", ";;",
		" ", " ", " ", "\n", "\\", "'", "\"", "$", "(", ")", "((", "))", "$[", "]", "`", "<<", "<<<", "<<-", "EOF", "{", "}", "${",
		";", "&", "|", "<", ">", "#", "-c", "bash", "sh", "eval", "xargs", "$'", "\t", "=", "-",
	}
	rules := []Rule{
		{Tool: Bash, How: HowCommand, Text: "git push"},
		{Tool: Bash, How: HowStarts, Text: "make deploy"},
		{Tool: Bash, How: HowCommand, Text: "gh pr create"},
		{Tool: Bash, How: HowStarts, Text: "$$"},
	}
	r := rand.New(rand.NewPCG(1, 2))
	lines := []string{
		"echo 'x",
		strings.Repeat("$((", 20_000) + " git push",
		strings.Repeat(`"$(`, 20_000) + " git push",
		strings.Repeat("`", 20_000) + " git push",
		strings.Repeat("<<E\n", 20_000) + "make deploy",
		strings.Repeat("'; ", 80_000) + "make deploy",
		strings.Repeat("case x in ", 20_000) + "git push",
		strings.Repeat("env -S env ", 20_000) + "git push",
	}
	for range 5000 {
		var b strings.Builder
		for range r.IntN(40) + 1 {
			b.WriteString(alphabet[r.IntN(len(alphabet))])
		}
		lines = append(lines, b.String())
	}
	done := make(chan struct{})

	go func() {
		defer close(done)
		for _, line := range lines {
			Hits(rules, Bash, bash(line))
		}
	}()

	select {
	case <-done:
	case <-time.After(testutil.Timeout(20 * time.Second)):
		t.Fatal("reading the lines did not end")
	}
}

func TestContainsRegexAndAnyCallReadTheInput(t *testing.T) {
	t.Parallel()
	write := json.RawMessage(`{"file_path":"/r/a<b>.md","content":"x"}`)

	assert.True(t, Rule{Tool: "Write", How: HowContains, Text: "a<b>"}.Matches("Write", write))
	assert.True(t, Rule{Tool: "W*", How: HowRegex, Text: `"file_path":"/r/`}.Matches("Write", write))
	assert.True(t, Rule{Tool: "mcp__*"}.Matches("mcp__clubmate__git_land", json.RawMessage(`{}`)))
	assert.False(t, Rule{Tool: "Bash", How: HowContains, Text: "description"}.Matches("Bash", bash("ls")))
}

func TestInputIsLosslessForTheKey(t *testing.T) {
	t.Parallel()
	key := func(tool string, input json.RawMessage) string {
		return Key("gt_1", "agt_1", tool, Input(tool, input), "head", "tree")
	}

	assert.NotEqual(t, key("Bash", bash(`echo "a  b"`)), key("Bash", bash(`echo "a b"`)))
	assert.Equal(t, key("Bash", bash(" ls \n")), key("Bash", json.RawMessage(`{"command":"ls"}`)))
	assert.NotEqual(t, key("Bash", bash(`touch a\ `)), key("Bash", bash(`touch a\`)))
	assert.Equal(t, "git push", Input("Bash", json.RawMessage(`{"command":"git push","Command":"ls"}`)))
	assert.Equal(t, key("Write", json.RawMessage(`{"b":1,"a":"x"}`)), key("Write", json.RawMessage(`{"a": "x", "b": 1}`)))
	assert.NotEqual(t, key("Write", json.RawMessage(`{"n":9007199254740993}`)), key("Write", json.RawMessage(`{"n":9007199254740992}`)))
	assert.NotEqual(t, Key("a", "bc", "", "", "", ""), Key("ab", "c", "", "", "", ""))
}

func TestAllReturnsEveryMatchingGateInCommandOrder(t *testing.T) {
	t.Parallel()
	rules := []Rule{
		{ID: "pr", Tool: "Bash", How: HowCommand, Text: "gh pr create"},
		{ID: "any", Tool: "Bash", How: HowContains, Text: "origin"},
		{ID: "push", Tool: "Bash", How: HowCommand, Text: "git push"},
		{ID: "deploy", Tool: "Bash", How: HowStarts, Text: "make deploy"},
	}

	ids := func(line string) []string {
		var out []string
		for _, r := range All(rules, "Bash", bash(line)) {
			out = append(out, r.ID)
		}
		return out
	}

	assert.Equal(t, []string{"push", "any", "pr"}, ids("git push origin a && gh pr create --fill"))
	for _, line := range []string{
		"sh -c 'git push && gh pr create'",
		"bash <<EOF\ngit push\ngh pr create\nEOF",
		`git push "x && gh pr create`,
		"echo hi; sh -c 'echo; git push'; gh pr create",
		`eval "git push && gh pr create"`,
		"echo 'git push; gh pr create' | sh",
		"echo x | xargs sh -c 'git push && gh pr create'",
	} {
		assert.Equal(t, []string{"push", "pr"}, ids(line), line)
	}
}
