package docrender

import (
	"bytes"
	"html"
	"path/filepath"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"clubmate/pkg/docext"
)

const (
	MaxImage  = 2 << 20
	MaxImages = 8 << 20
)

type Options struct {
	Path    string
	Allowed func(abs string) bool
	IsDoc   func(abs string) bool
}

type Doc struct {
	Title string
	HTML  string
}

func Render(src []byte, opts Options) Doc {
	if opts.IsDoc == nil {
		opts.IsDoc = viewable
	}
	if strings.EqualFold(filepath.Ext(opts.Path), ".mmd") {
		return Doc{Title: docext.FileTitle(opts.Path), HTML: mermaidHTML(src)}
	}
	ctx := parser.NewContext()
	ctx.Set(stateKey, &state{opts: opts})
	root := markdown.Parser().Parse(text.NewReader(src), parser.WithContext(ctx))
	title := docext.HeadingTitle(root, src)
	if title == "" {
		title = docext.FileTitle(opts.Path)
	}
	var buf bytes.Buffer
	if err := markdown.Renderer().Render(&buf, src, root); err != nil {
		return Doc{Title: title, HTML: "<pre>" + html.EscapeString(string(src)) + "</pre>"}
	}
	return Doc{Title: title, HTML: buf.String()}
}

func Artifact(src []byte, path string) Doc {
	return Doc{Title: docext.ArtifactTitle(src, path), HTML: string(src)}
}

func viewable(path string) bool {
	return docext.Is(path) || docext.IsArtifact(path)
}

type state struct {
	opts   Options
	images int64
}

type transformer struct{}

func (transformer) Transform(root *ast.Document, reader text.Reader, pc parser.Context) {
	s, ok := pc.Get(stateKey).(*state)
	if !ok {
		s = &state{opts: Options{IsDoc: viewable}}
	}
	src := reader.Source()
	var nodes []ast.Node
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			if string(n.Language(src)) == "mermaid" {
				nodes = append(nodes, n)
			}
		case *ast.Link, *ast.AutoLink, *ast.Image:
			nodes = append(nodes, n)
		}
		return ast.WalkContinue, nil
	})
	for _, n := range nodes {
		switch n := n.(type) {
		case *ast.FencedCodeBlock:
			n.Parent().ReplaceChild(n.Parent(), n, &mermaidBlock{source: lines(n, src)})
		case *ast.Link:
			s.link(n)
		case *ast.AutoLink:
			s.autoLink(n, src)
		case *ast.Image:
			s.image(n, src)
		}
	}
}

type mermaidBlock struct {
	ast.BaseBlock
	source []byte
}

func (n *mermaidBlock) Kind() ast.NodeKind {
	return kindMermaid
}

func (n *mermaidBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type nodeRenderer struct{}

func (nodeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMermaid, renderMermaid)
	reg.Register(kindImage, renderImage)
	reg.Register(kindNotice, renderNotice)
}

var (
	stateKey    = parser.NewContextKey()
	kindMermaid = ast.NewNodeKind("DocMermaid")
	markdown    = goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			highlighting.NewHighlighting(highlighting.WithFormatOptions(chromahtml.WithClasses(true))),
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(transformer{}, 100)),
		),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(util.Prioritized(nodeRenderer{}, 100)),
		),
	)
)

func renderMermaid(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if b, ok := n.(*mermaidBlock); ok && entering {
		_, _ = w.WriteString(mermaidHTML(b.source) + "\n")
	}
	return ast.WalkSkipChildren, nil
}

func mermaidHTML(src []byte) string {
	return `<pre class="mermaid">` + html.EscapeString(strings.TrimRight(string(src), "\r\n")) + "</pre>"
}

func lines(n ast.Node, src []byte) []byte {
	var buf bytes.Buffer
	for i := range n.Lines().Len() {
		seg := n.Lines().At(i)
		buf.Write(seg.Value(src))
	}
	return buf.Bytes()
}

func unwrap(n ast.Node, extra ...ast.Node) {
	parent := n.Parent()
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		parent.InsertBefore(parent, n, c)
		c = next
	}
	for _, e := range extra {
		parent.InsertBefore(parent, n, e)
	}
	parent.RemoveChild(parent, n)
}
