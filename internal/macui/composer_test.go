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

func TestSentMessageShowsDimmedUntilTheAgentHasIt(t *testing.T) {
	in := startWith(t, "macui-composer-sending", "", []string{"CLUBMATE_AGENTS_TAKE_HOLD=4s", "CLUBMATE_AGENTS_SEND_HOLD=4s"})
	in.focusSection("agents", "agents.sidebar")
	session := in.idleSession("Hello")
	echo := session + "/echo/"
	sent := session + "/msg/user/prompt-2"
	in.pointer("click", session+"/composer/0")
	in.focusIn(session)
	in.key("h", "i")

	in.key("return")

	drawn := func(want func(w *window) bool) func() bool {
		return func() bool {
			w := in.snapshot().main()
			require.False(t, w.hasPrefix(echo) && w.has(sent), "the echo and the plugin's message are never both drawn")
			return want(w)
		}
	}
	in.eventually("the echo is drawn before the plugin takes the message", drawn(func(w *window) bool {
		return w.hasPrefix(echo) && w.has(session+"/composer/0")
	}))
	assert.Equal(t, []string{"menu Undo (disabled, not sent)"}, in.key("cmd+z"), "the cleared composer has nothing to undo")
	in.eventually("the plugin draws it as sending above the next composer", drawn(func(w *window) bool {
		n := in.sdui(sent)
		return n != nil && n.Props["sending"] == true && w.has(sent) && w.has(session+"/composer/1")
	}))
	in.eventually("the message lands", drawn(func(w *window) bool {
		n := in.sdui(sent)
		return n != nil && n.Props["sending"] == nil && n.Props["text"] == "hi" && w.has(sent)
	}))
}
