package sdui_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

func TestNodeJSONShape(t *testing.T) {
	n := sdui.VStack(sdui.Button("new", "New", ""), sdui.MutedText("hi"))
	raw, err := json.Marshal(n)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"stack","props":{"direction":"vertical"},"children":[
		{"type":"button","id":"new","props":{"label":"New"}},
		{"type":"text","props":{"text":"hi","style":"muted"}}]}`, string(raw))
}
