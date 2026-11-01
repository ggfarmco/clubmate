package sdui_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/sdui"
)

type deltaCase struct {
	Name   string          `json:"name"`
	Append bool            `json:"append"`
	Keep   bool            `json:"keep"`
	Base   json.RawMessage `json:"base"`
	Tree   json.RawMessage `json:"tree"`
	Delta  json.RawMessage `json:"delta"`
}

func TestDeltaRefersToUnchangedSubtreesAndResolvesBack(t *testing.T) {
	raw, err := os.ReadFile("testdata/delta.json")
	require.NoError(t, err)
	var cases []deltaCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			base := index(t, compact(t, tc.Base))
			delta, ok := index(t, compact(t, tc.Tree)).DeltaFrom(base, sdui.DeltaOptions{Appends: tc.Append, Keeps: tc.Keep})
			require.True(t, ok)
			assert.Equal(t, string(compact(t, tc.Delta)), string(delta))
			resolved, err := base.Resolve(delta)
			require.NoError(t, err)
			assert.Equal(t, string(compact(t, tc.Tree)), string(resolved))
		})
	}
}

func TestDeltaNotSmallerThanTreeIsDropped(t *testing.T) {
	base := index(t, []byte(`{"type":"stack","props":{"direction":"vertical"},"children":[{"type":"text","props":{"text":"a"}}]}`))
	_, ok := index(t, []byte(`{"type":"empty","props":{"text":""}}`)).DeltaFrom(base, sdui.DeltaOptions{})
	assert.False(t, ok)
}

func TestDeltaWithAppendShapedPropOverStringIsDropped(t *testing.T) {
	base := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"text","props":{"note":"plain"}}]}`))
	_, ok := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"text","props":{"note":{"append":"x"}}}]}`)).DeltaFrom(base, sdui.DeltaOptions{Appends: true})
	assert.False(t, ok)
}

func TestDeltaWithKeepShapedElementOverArrayIsDropped(t *testing.T) {
	base := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"table","props":{"rows":[{"id":"a long enough row"}]}}]}`))
	_, ok := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"table","props":{"rows":[{"keep":0}]}}]}`)).DeltaFrom(base, sdui.DeltaOptions{Keeps: true})
	assert.False(t, ok)
}

func TestMalformedDeltaDoesNotResolve(t *testing.T) {
	base := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a"}},{"type":"table","props":{"text":"a text long enough to be worth copying many times over","rows":["a text long enough to be worth copying many times over"]}}]}`))
	keeps := `{"type":"stack","children":[{"ref":[0]},{"type":"table","props":{"rows":[` + strings.Repeat(`{"keep":0},`, 40) + `{"keep":0}]}}]}`
	appends := `{"type":"stack","children":[{"ref":[0]},{"type":"table","props":{` + strings.Repeat(`"text":{"append":""},`, 40) + `"text":{"append":""}}}]}`
	for _, delta := range []string{keeps, appends, `{"ref":[2]}`, `{"ref":[0,0]}`, `{"ref":[-1]}`, `{"ref":[0],"type":"x"}`, `[]`, `{"type":"stack"}{}`, `{"type":"stack","children":[{"ref":"0"}]}`, `{"type":"stack","children":[{"ref":[]},{"ref":[]},{"ref":[]}]}`} {
		_, err := base.Resolve([]byte(delta))
		assert.ErrorIs(t, err, sdui.ErrUnresolvedDelta, delta)
	}
}

func BenchmarkDelta(b *testing.B) {
	base := transcriptTree(40, "Running")
	tree := transcriptTree(40, "Running the whole suite")
	prev, err := sdui.IndexTree(base)
	require.NoError(b, err)
	b.SetBytes(int64(len(tree)))
	b.ReportAllocs()
	for b.Loop() {
		next, err := sdui.IndexTree(tree)
		if err != nil {
			b.Fatal(err)
		}
		if _, ok := next.DeltaFrom(prev, sdui.DeltaOptions{Appends: true}); !ok {
			b.Fatal("no delta")
		}
	}
}

func index(t *testing.T, tree []byte) *sdui.TreeIndex {
	t.Helper()
	ix, err := sdui.IndexTree(tree)
	require.NoError(t, err)
	return ix
}

func compact(t *testing.T, raw json.RawMessage) []byte {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, raw))
	return buf.Bytes()
}

func transcriptTree(cards int, last string) []byte {
	children := make([]string, 0, cards+1)
	body := strings.Repeat("output line of a long command\n", 400)
	for i := range cards {
		card := map[string]any{"type": "toolCard", "id": "s/tool/" + strconv.Itoa(i), "props": map[string]any{"kind": "execute", "title": "go test", "body": map[string]any{"format": "terminal", "text": body}}}
		b, _ := json.Marshal(card)
		children = append(children, string(b))
	}
	msg, _ := json.Marshal(map[string]any{"type": "message", "id": "s/msg/last", "props": map[string]any{"role": "agent", "text": last}})
	children = append(children, string(msg))
	return []byte(`{"type":"split","props":{"direction":"vertical"},"children":[{"type":"transcript","id":"s/transcript","children":[` + strings.Join(children, ",") + `]},{"type":"composer","id":"s/composer/1","props":{"placeholder":"Message"}}]}`)
}
