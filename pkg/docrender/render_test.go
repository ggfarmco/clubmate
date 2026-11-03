package docrender_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/docrender"
)

func TestMermaidFence(t *testing.T) {
	doc := docrender.Render([]byte("```mermaid\nA-->B<C\n```\n"), docrender.Options{})
	assert.Contains(t, doc.HTML, `<pre class="mermaid">A--&gt;B&lt;C</pre>`)
}

func TestRawHTMLDropped(t *testing.T) {
	doc := docrender.Render([]byte("<script>x</script>\n\n<b>y</b>"), docrender.Options{})
	assert.NotContains(t, doc.HTML, "<script")
	assert.NotContains(t, doc.HTML, "<b>")
}

func TestGFM(t *testing.T) {
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n\n~~s~~\n"
	doc := docrender.Render([]byte(src), docrender.Options{})
	for _, want := range []string{"<table>", `type="checkbox"`, "checked", "<del>"} {
		assert.Contains(t, doc.HTML, want)
	}
}

func TestLinks(t *testing.T) {
	opts := docrender.Options{
		Path:    "/r/docs/a.md",
		Allowed: func(abs string) bool { return strings.HasPrefix(abs, "/r/") },
	}
	tests := []struct {
		name string
		src  string
		want string
	}{
		{"doc", "[b](b.md#x)", `<a href="clubmate-doc:/r/docs/b.md#x">b</a>`},
		{"file", "[c](../main.go)", `<a href="clubmate-file:/r/main.go">c</a>`},
		{"web", "[w](https://e.com)", `<a href="https://e.com">w</a>`},
		{"anchor", "[h](#sec)", `<a href="#sec">h</a>`},
		{"outside", "[o](../../etc/x.md)", "<p>o</p>"},
		{"script", "[j](javascript:alert(1))", "<p>j</p>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := docrender.Render([]byte(tt.src), opts)
			assert.Contains(t, doc.HTML, tt.want)
		})
	}
}

func TestNoBaseNoLocal(t *testing.T) {
	doc := docrender.Render([]byte("[b](b.md) ![i](i.png)"), docrender.Options{
		Allowed: func(string) bool { return true },
	})
	assert.Equal(t, "<p>b i</p>\n", doc.HTML)
}

func TestImageInlined(t *testing.T) {
	dir := tempDir(t)
	writePNG(t, filepath.Join(dir, "i.png"))
	doc := docrender.Render([]byte("![pic](i.png)"), inDir(dir))
	assert.Contains(t, doc.HTML, `src="data:image/png;base64,`)
	assert.Contains(t, doc.HTML, `alt="pic"`)
}

func TestImageWrongMagic(t *testing.T) {
	dir := tempDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fake.png"), []byte("just text"), 0o600))
	doc := docrender.Render([]byte("![fake alt](fake.png)"), inDir(dir))
	assert.NotContains(t, doc.HTML, "<img")
	assert.Contains(t, doc.HTML, "fake alt")
	assert.NotContains(t, doc.HTML, "image too large")
}

func TestImageTooLarge(t *testing.T) {
	dir := tempDir(t)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))))
	data := append(buf.Bytes(), make([]byte, docrender.MaxImage+1-buf.Len())...)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.png"), data, 0o600))
	doc := docrender.Render([]byte("![big alt](big.png)"), inDir(dir))
	assert.NotContains(t, doc.HTML, "<img")
	assert.Contains(t, doc.HTML, "big alt")
	assert.Contains(t, doc.HTML, "image too large")
}

func TestImageOutsideScopeShowsAlt(t *testing.T) {
	root := tempDir(t)
	in, out := filepath.Join(root, "in"), filepath.Join(root, "out")
	require.NoError(t, os.Mkdir(in, 0o700))
	require.NoError(t, os.Mkdir(out, 0o700))
	writePNG(t, filepath.Join(out, "x.png"))
	require.NoError(t, os.Symlink(filepath.Join(out, "x.png"), filepath.Join(in, "x.png")))
	doc := docrender.Render([]byte("![secret](x.png)"), inDir(in))
	assert.NotContains(t, doc.HTML, "<img")
	assert.Contains(t, doc.HTML, "secret")
}

func TestTitle(t *testing.T) {
	tests := []struct {
		name string
		src  string
		path string
		want string
	}{
		{"heading", "# Hello *you*\n# Second\n", "/r/plan.md", "Hello you"},
		{"entities", "# Tom &amp; \\*Jerry\\*\n", "", "Tom & *Jerry*"},
		{"file name", "text\n", "/r/plan.md", "plan"},
		{"none", "text\n", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := docrender.Render([]byte(tt.src), docrender.Options{Path: tt.path})
			assert.Equal(t, tt.want, doc.Title)
		})
	}
}

func TestMMDFile(t *testing.T) {
	doc := docrender.Render([]byte("graph TD; A-->B\n"), docrender.Options{Path: "/r/flow.mmd"})
	assert.Equal(t, `<pre class="mermaid">graph TD; A--&gt;B</pre>`, doc.HTML)
	assert.Equal(t, "flow", doc.Title)
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}

func inDir(dir string) docrender.Options {
	return docrender.Options{
		Path:    filepath.Join(dir, "doc.md"),
		Allowed: func(abs string) bool { return strings.HasPrefix(abs, dir+string(filepath.Separator)) },
	}
}

func writePNG(t *testing.T, path string) {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1))))
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

func TestTaskLinks(t *testing.T) {
	tasks := map[int]string{3: "Parser", 4: "Lexer"}
	opts := docrender.Options{Task: func(id int) (string, bool) { title, ok := tasks[id]; return title, ok }}
	doc := docrender.Render([]byte("Builds on #3, not #9 or a#3; `#4` stays code\n"), opts)
	assert.Contains(t, doc.HTML, `Builds on <a href="clubmate-doc:/task/3" title="Parser">#3</a>, not #9 or a#3; <code>#4</code>`)
	assert.NotContains(t, docrender.Render([]byte("#3"), docrender.Options{}).HTML, "<a")
}
