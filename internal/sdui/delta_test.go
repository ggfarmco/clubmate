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

	internalsdui "clubmate/internal/sdui"
)

type deltaCase struct {
	Name   string          `json:"name"`
	Append bool            `json:"append"`
	Base   json.RawMessage `json:"base"`
	Tree   json.RawMessage `json:"tree"`
	Delta  json.RawMessage `json:"delta"`
}

func TestDeltaRefersToUnchangedSubtrees(t *testing.T) {
	raw, err := os.ReadFile("testdata/delta.json")
	require.NoError(t, err)
	var cases []deltaCase
	require.NoError(t, json.Unmarshal(raw, &cases))
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			base := index(t, compact(t, tc.Base))
			delta, ok := index(t, compact(t, tc.Tree)).DeltaFrom(base, internalsdui.DeltaOptions{Appends: tc.Append})
			require.True(t, ok)
			assert.Equal(t, string(compact(t, tc.Delta)), string(delta))
		})
	}
}

func TestDeltaNotSmallerThanTreeIsDropped(t *testing.T) {
	base := index(t, []byte(`{"type":"stack","props":{"direction":"vertical"},"children":[{"type":"text","props":{"text":"a"}}]}`))
	_, ok := index(t, []byte(`{"type":"empty","props":{"text":""}}`)).DeltaFrom(base, internalsdui.DeltaOptions{})
	assert.False(t, ok)
}

func TestDeltaWithAppendShapedPropOverStringIsDropped(t *testing.T) {
	base := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"text","props":{"note":"plain"}}]}`))
	_, ok := index(t, []byte(`{"type":"stack","children":[{"type":"text","props":{"text":"a line that stays the same"}},{"type":"text","props":{"note":{"append":"x"}}}]}`)).DeltaFrom(base, internalsdui.DeltaOptions{Appends: true})
	assert.False(t, ok)
}

func BenchmarkDelta(b *testing.B) {
	base := transcriptTree(40, "Running")
	tree := transcriptTree(40, "Running the whole suite")
	prev, err := internalsdui.IndexTree(base)
	require.NoError(b, err)
	b.SetBytes(int64(len(tree)))
	b.ReportAllocs()
	for b.Loop() {
		next, err := internalsdui.IndexTree(tree)
		if err != nil {
			b.Fatal(err)
		}
		if _, ok := next.DeltaFrom(prev, internalsdui.DeltaOptions{Appends: true}); !ok {
			b.Fatal("no delta")
		}
	}
}

func index(t *testing.T, tree []byte) *internalsdui.TreeIndex {
	t.Helper()
	ix, err := internalsdui.IndexTree(tree)
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
