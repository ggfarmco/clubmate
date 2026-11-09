package macui_test

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const imageScript = `{"prompt": [
  {"match": "Code", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "` + "`shot.png`" + ` is the screenshot"}}},
  {"match": "Link", "update": {"sessionUpdate": "agent_message_chunk", "content": {"type": "text", "text": "[shot.png](shot.png) is the screenshot"}}}]}`

func TestTranscriptImageLinkOpensViewer(t *testing.T) {
	in := start(t, "macui-image", imageScript)
	f, err := os.Create(filepath.Join(in.repo, "shot.png"))
	require.NoError(t, err)
	require.NoError(t, png.Encode(f, image.NewGray(image.Rect(0, 0, 40, 30))))
	require.NoError(t, f.Close())
	in.focusSection("agents", "agents.sidebar")
	var spawned struct {
		Session string `json:"session"`
	}
	in.json(&spawned, "agent", "spawn", "Code")
	sid := spawned.Session

	for i, tc := range []struct{ prompt, link string }{{"Code", "`shot.png`"}, {"Link", "[shot.png]"}} {
		t.Run(tc.prompt, func(t *testing.T) {
			in := in.at(t)
			if i > 0 {
				in.cm("agent", "tell", sid, tc.prompt)
			}
			var msg *element
			in.eventually("the agent's message shows", func() bool {
				msg = in.snapshot().main().find(func(e element) bool {
					return strings.HasPrefix(e.ID, sid+"/msg/agent/") && strings.HasPrefix(e.Label, tc.link)
				})
				return msg != nil
			})
			in.pointer("click", msg.ID, "--at", "12,8")
			in.eventually("the image viewer sheet opens", func() bool { return in.snapshot().kind("sheet") != nil })
			in.key("esc")
			in.eventually("esc closes it", func() bool { return in.snapshot().kind("sheet") == nil })
		})
	}
}
