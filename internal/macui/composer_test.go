package macui_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pasteJXA = `ObjC.import('AppKit');
function run(argv) {
	const board = $.NSPasteboard.pasteboardWithName('com.clubmate.test');
	board.clearContents;
	board.setDataForType($.NSData.dataWithContentsOfFile(argv[0]), 'public.png');
}`

func (in *instance) chips(composer string) []string {
	in.t.Helper()
	n := in.sdui(composer)
	if n == nil {
		return nil
	}
	raw, err := json.Marshal(n.Props["attachments"])
	require.NoError(in.t, err)
	var atts []struct {
		Kind string `json:"kind"`
		Path string `json:"path"`
	}
	require.NoError(in.t, json.Unmarshal(raw, &atts))
	var out []string
	for _, a := range atts {
		out = append(out, a.Kind+" "+a.Path)
	}
	return out
}

func TestComposerTakesImagesDroppedOrPasted(t *testing.T) {
	in := start(t, "macui-composer-image", "")
	in.focusSection("agents", "agents.sidebar")
	session := in.idleSession("Hello")
	composer := session + "/composer/0"
	var img bytes.Buffer
	require.NoError(t, png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 4, 3))))
	shot := filepath.Join(in.repo, "Screenshot 2026-11-16 at 17.14.03.png")
	require.NoError(t, os.WriteFile(shot, img.Bytes(), 0o600))
	in.pointer("click", composer)
	in.focusIn(session)

	in.pointer("drop", composer, shot)
	in.eventually("the dropped path becomes an image chip", func() bool {
		return slices.Equal(in.chips(composer), []string{"image Screenshot 2026-11-16 at 17.14.03.png"})
	})
	assert.Equal(t, []string{"menu Undo (disabled, not sent)"}, in.key("cmd+z"), "undo must not bring the path back over a text it no longer matches")

	out, err := exec.Command("osascript", "-l", "JavaScript", "-e", pasteJXA, shot).CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Contains(t, in.key("cmd+v"), "monitor")
	in.eventually("a pasted image becomes a second chip", func() bool {
		return slices.Equal(in.chips(composer), []string{"image Screenshot 2026-11-16 at 17.14.03.png", "image Pasted image.png"})
	})
}
