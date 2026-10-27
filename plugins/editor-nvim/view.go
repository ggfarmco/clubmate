package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"clubmate/pkg/sdui"
)

type status struct {
	Mode   string
	File   string
	LSP    []string
	Branch string
}

var modeNames = map[byte]string{
	'n':  "NORMAL",
	'i':  "INSERT",
	'v':  "VISUAL",
	'V':  "VISUAL",
	0x16: "VISUAL",
	'c':  "COMMAND",
	't':  "TERMINAL",
	'R':  "REPLACE",
}

func startingView() sdui.Node {
	return sdui.Empty("Starting Neovim…")
}

func missingView() sdui.Node {
	return sdui.Empty("nvim not found in PATH")
}

func runningView(sessionID string) sdui.Node {
	return sdui.Surface(sessionID)
}

func exitedView(code *int) sdui.Node {
	text := "Neovim exited"
	if code != nil {
		text = fmt.Sprintf("Neovim exited (code %d)", *code)
	}
	return sdui.VStack(sdui.Text(text), sdui.Button("restart", "Restart", "arrow.clockwise"))
}

func failedView(err error) sdui.Node {
	return sdui.VStack(sdui.Text("Neovim failed to start: "+err.Error()), sdui.Button("restart", "Restart", "arrow.clockwise"))
}

func statusView(st status, repos []string) sdui.Node {
	var items []sdui.Node
	if st.File != "" {
		items = append(items, sdui.Node{Type: "text", Props: map[string]any{"text": relative(st.File, repos), "style": "mono"}})
	}
	if st.Mode != "" {
		items = append(items, sdui.Badge(modeName(st.Mode), ""))
	}
	if len(st.LSP) > 0 {
		items = append(items, sdui.MutedText(strings.Join(st.LSP, ", ")))
	}
	if st.Branch != "" {
		items = append(items, sdui.MutedText(st.Branch))
	}
	return sdui.HStack(items...)
}

func modeName(mode string) string {
	if name, ok := modeNames[mode[0]]; ok {
		return name
	}
	return mode
}

func relative(file string, repos []string) string {
	best := ""
	for _, repo := range repos {
		if strings.HasPrefix(file, repo+string(filepath.Separator)) && len(repo) > len(best) {
			best = repo
		}
	}
	if best == "" {
		return file
	}
	return file[len(best)+1:]
}
