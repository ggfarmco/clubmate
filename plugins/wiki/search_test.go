package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchRanksTitleOverHeadingOverBody(t *testing.T) {
	docs := []doc{
		{Page: "a.md", Title: "Alpha", Body: "# Alpha\n\nthe widget lives here\n"},
		{Page: "b.md", Title: "Beta", Body: "# Beta\n\n## Widget rules\n\ntext\n"},
		{Page: "c.md", Title: "Widget guide", Body: "# Widget guide\n"},
		{Page: "d.md", Title: "Delta", Body: "# Delta\n\nnothing\n"},
	}

	hits := search(docs, "widget", 10)

	pages := make([]string, 0, len(hits))
	for _, h := range hits {
		pages = append(pages, h.Page)
	}
	assert.Equal(t, []string{"c.md", "b.md", "a.md"}, pages)
}

func TestSearchCaseInsensitiveSnippet(t *testing.T) {
	long := "  The SDUI node " + strings.Repeat("x", 300) + "  "
	docs := []doc{{Page: "a.md", Title: "Nodes", Body: "# Nodes\n\nfirst line\n" + long + "\n"}}

	hits := search(docs, "Sdui node", 10)

	require.Len(t, hits, 1)
	assert.Equal(t, []rune(strings.TrimSpace(long))[:160], []rune(hits[0].Snippet))
	assert.Empty(t, search(docs, "sdui missing", 10))
}

func TestSearchHtmlText(t *testing.T) {
	docs := []doc{{Page: "demo.html", Title: "Demo", Body: `<html><head><style>.zebra{}</style><script>var giraffe = 1</script></head><body><p class="lion">Hello <b>tiger</b> world</p></body></html>`}}

	assert.Len(t, search(docs, "tiger", 10), 1)
	assert.Empty(t, search(docs, "giraffe", 10))
	assert.Empty(t, search(docs, "zebra", 10))
	assert.Empty(t, search(docs, "lion", 10))
	assert.Equal(t, "Hello tiger world", search(docs, "tiger", 10)[0].Snippet)
}
