package docext

import (
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	KindMarkdown = "markdown"
	KindMermaid  = "mermaid"
	KindArtifact = "artifact"

	titleRead = 64 << 10
)

var (
	titleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	parser   = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()
)

func Is(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".mmd":
		return true
	}
	return false
}

func IsArtifact(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return true
	}
	return false
}

func Kind(path string) string {
	switch {
	case strings.EqualFold(filepath.Ext(path), ".mmd"):
		return KindMermaid
	case Is(path):
		return KindMarkdown
	case IsArtifact(path):
		return KindArtifact
	}
	return ""
}

func Title(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return FileTitle(path)
	}
	defer f.Close()
	src, err := io.ReadAll(io.LimitReader(f, titleRead))
	if err != nil {
		return FileTitle(path)
	}
	switch Kind(path) {
	case KindArtifact:
		return ArtifactTitle(src, path)
	case KindMarkdown:
		if title := HeadingTitle(parser.Parse(text.NewReader(src)), src); title != "" {
			return title
		}
	}
	return FileTitle(path)
}

func ArtifactTitle(src []byte, path string) string {
	if m := titleTag.FindSubmatch(src); m != nil {
		if t := strings.Join(strings.Fields(html.UnescapeString(string(m[1]))), " "); t != "" {
			return t
		}
	}
	return FileTitle(path)
}

func HeadingTitle(root ast.Node, src []byte) string {
	var title string
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		h, ok := n.(*ast.Heading)
		if !entering || !ok || h.Level != 1 {
			return ast.WalkContinue, nil
		}
		title = PlainText(h, src)
		return ast.WalkStop, nil
	})
	return strings.TrimSpace(title)
}

func FileTitle(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func PlainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.Write(c.Value(src))
			if c.SoftLineBreak() || c.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(c.Value)
		}
		return ast.WalkContinue, nil
	})
	return string(util.ResolveNumericReferences(util.ResolveEntityNames(util.UnescapePunctuations([]byte(b.String())))))
}
