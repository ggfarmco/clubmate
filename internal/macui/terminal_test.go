package macui_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTerminalDropPastesAQuotedPath(t *testing.T) {
	in := start(t, "macui-drop", "")
	in.cm("command", "run", "terminals.new")
	in.focusSection("terminals", "terminals.main")
	var id string
	in.eventually("the terminal session runs", func() bool {
		var list []struct {
			ID    string `json:"id"`
			Owner string `json:"owner"`
		}
		in.json(&list, "session", "list")
		for _, s := range list {
			if s.Owner == "terminals" {
				id = s.ID
			}
		}
		return id != ""
	})
	read := func() string { return in.cm("session", "read", id) }
	in.eventually("the shell shows a prompt", func() bool { return strings.TrimSpace(read()) != "" })

	file := filepath.Join(in.repo, "it's a $file.txt")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	send := func(text string) {
		in.cm("command", "run", "session.send", "--args", `{"id":"`+id+`","text":"`+text+`"}`)
	}
	send("[ -e ")
	in.eventually("the shell echoes the typed start", func() bool { return strings.Contains(read(), "[ -e") })
	in.pointer("drop", "terminals.main/surface", file)
	in.eventually("the path is on the command line", func() bool { return strings.Contains(read(), "file.txt") })
	send(` ] && echo DROP-$((40+2))\r`)
	in.eventually("the shell sees the dropped path as one existing file", func() bool { return strings.Contains(read(), "DROP-42") })
}
