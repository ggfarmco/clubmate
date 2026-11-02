package docext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clubmate/pkg/docext"
)

func TestExt(t *testing.T) {
	assert.True(t, docext.Is("/r/A.MD"))
	assert.True(t, docext.Is("x.markdown"))
	assert.True(t, docext.Is("x.mmd"))
	assert.False(t, docext.Is("x.html"))
	assert.True(t, docext.IsArtifact("/r/report.HTML"))
	assert.True(t, docext.IsArtifact("x.htm"))
	assert.False(t, docext.IsArtifact("x.md"))
}

func TestKind(t *testing.T) {
	assert.Equal(t, docext.KindMarkdown, docext.Kind("/r/a.Markdown"))
	assert.Equal(t, docext.KindMermaid, docext.Kind("/r/flow.MMD"))
	assert.Equal(t, docext.KindArtifact, docext.Kind("/r/report.htm"))
	assert.Empty(t, docext.Kind("/r/main.go"))
}

func TestTitleReadsTheFile(t *testing.T) {
	tests := []struct {
		name, file, src, want string
	}{
		{"heading", "plan.md", "intro\n\n## Sub\n\n# The **plan**\n", "The plan"},
		{"no heading", "notes.md", "just text\n", "notes"},
		{"heading past 64 KiB", "late.md", strings.Repeat("x\n", 40<<10) + "# Late\n", "late"},
		{"html title", "report.html", "<html><head><title>\n Q3 &amp; Q4 </title></head></html>", "Q3 & Q4"},
		{"mermaid", "flow.mmd", "graph TD\n# A\n", "flow"},
	}
	dir := t.TempDir()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.file)
			require.NoError(t, os.WriteFile(path, []byte(tt.src), 0o600))
			assert.Equal(t, tt.want, docext.Title(path))
		})
	}
	assert.Equal(t, "gone", docext.Title(filepath.Join(dir, "gone.md")))
}
