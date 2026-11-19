package main_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolGateAddValidates(t *testing.T) {
	t.Parallel()
	f := start(t, map[string]any{})
	repo := newRepo(t)
	workspace := f.workspace(t, repo)
	add := func(args map[string]any) (gateListed, error) {
		args["repo"], args["point"] = repo, "tool"
		var g gateListed
		err := f.run(t, workspace, "agents.gate-add", args, &g)
		return g, err
	}
	push, err := add(map[string]any{"how": "command", "text": " git  push "})
	require.NoError(t, err)
	_, err = add(map[string]any{"tool": "Write"})
	require.NoError(t, err)
	env, err := add(map[string]any{"tool": "Write", "text": ".env"})
	require.NoError(t, err)
	require.NoError(t, f.run(t, workspace, "agents.gate-add", map[string]any{"repo": repo}, nil))

	refused := map[string]struct {
		args map[string]any
		want string
	}{
		"glob":          {map[string]any{"tool": "Bash[", "text": "x"}, "is a tool name or a glob"},
		"regex":         {map[string]any{"how": "regex", "text": "("}, "not a valid regex"},
		"how":           {map[string]any{"how": "glob", "text": "x"}, "args.how"},
		"starts":        {map[string]any{"tool": "Read", "how": "starts", "text": "x"}, "match Bash commands"},
		"command":       {map[string]any{"tool": "Read", "how": "command", "text": "git push"}, "match Bash commands"},
		"one word":      {map[string]any{"how": "command", "text": "git"}, "binary and its subcommand"},
		"no text":       {map[string]any{}, "names a command or a tool"},
		"wildcard":      {map[string]any{"tool": "mcp__*"}, "names a command or a tool"},
		"into":          {map[string]any{"into": "main", "text": "x"}, "not into, sessions or wrote"},
		"sessions":      {map[string]any{"sessions": []string{"top"}, "text": "x"}, "not into, sessions or wrote"},
		"wrote":         {map[string]any{"wrote": []string{"**"}, "text": "x"}, "not into, sessions or wrote"},
		"duplicate":     {map[string]any{"how": "command", "text": "git push"}, push.ID},
		"duplicate any": {map[string]any{"tool": "Write", "how": "regex"}, "already has a gate"},
		"starts two":    {map[string]any{"how": "starts", "text": "make deploy | tee"}, "one command"},
		"starts sudo":   {map[string]any{"how": "starts", "text": "sudo make install"}, `write "make install"`},
		"starts var":    {map[string]any{"how": "starts", "text": "FOO=1 make deploy"}, `can't start with one; write "make deploy"`},
		"command path":  {map[string]any{"how": "command", "text": "./deploy.sh prod"}, `write "deploy.sh prod"`},
		"command sudo":  {map[string]any{"how": "command", "text": "sudo git push"}, `write "git push"`},
		"options":       {map[string]any{"how": "command", "text": "git push --force"}, "never match its own text"},
		"starts opts":   {map[string]any{"how": "starts", "text": "sudo -u bob make"}, `write "make"`},
	}
	for name, c := range refused {
		_, err := add(c.args)
		assert.ErrorContains(t, err, c.want, name)
	}

	assert.ErrorContains(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "text": "git"}, nil), "binary and its subcommand")
	assert.ErrorContains(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "text": "rm -rf"}, nil), "never match its own text")
	reboot := f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "text": "sudo reboot"}, nil)
	require.ErrorContains(t, reboot, "wrapper")
	assert.NotContains(t, reboot.Error(), "write")
	assert.ErrorContains(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "into": "main"}, nil), "not into, sessions or wrote")
	assert.ErrorContains(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": env.ID, "tool": " "}, nil), "is a tool name or a glob")
	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "how": "starts", "text": "make deploy"}, nil))
	words := func() map[string]string {
		out := map[string]string{}
		for _, g := range f.gates(t, workspace) {
			out[g.ID] = g.Name + " | " + g.Words
			if g.Point == "land" {
				assert.ErrorContains(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": g.ID, "text": "x"}, nil), "are for a Tool call gate")
			}
		}
		return out
	}
	assert.Equal(t, "Tool call · make deploy | Bash · a command that starts with make deploy", words()[push.ID])
	assert.Equal(t, "Tool call · .env | Write · the input contains .env", words()[env.ID])
	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "tool": "Read"}, nil))
	assert.Equal(t, "Tool call · make deploy | Read · the input contains make deploy", words()[push.ID])
	require.NoError(t, f.run(t, workspace, "agents.gate-update", map[string]any{"id": push.ID, "tool": "Bash"}, nil))
	assert.Equal(t, "Tool call · make deploy | Bash · the command contains make deploy", words()[push.ID])
}
