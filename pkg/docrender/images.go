package docrender

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

type inlineImage struct {
	ast.BaseInline
	src, alt, title []byte
}

func (n *inlineImage) Kind() ast.NodeKind {
	return kindImage
}

func (n *inlineImage) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type notice struct {
	ast.BaseInline
	text string
}

func (n *notice) Kind() ast.NodeKind {
	return kindNotice
}

func (n *notice) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

var (
	kindImage  = ast.NewNodeKind("DocImage")
	kindNotice = ast.NewNodeKind("DocNotice")
	svgBOM     = []byte("\xef\xbb\xbf")
)

func (s *state) image(n *ast.Image, src []byte) {
	uri, tooLarge := s.load(string(n.Destination))
	switch {
	case uri != "":
		img := &inlineImage{src: []byte(uri), alt: []byte(plainText(n, src)), title: n.Title}
		n.Parent().ReplaceChild(n.Parent(), n, img)
	case tooLarge:
		unwrap(n, &notice{text: "image too large"})
	default:
		unwrap(n)
	}
}

func (s *state) load(dest string) (string, bool) {
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", false
	}
	abs, ok := s.local(u.Path)
	if !ok {
		return "", false
	}
	mime := imageMIME(abs)
	if mime == "" {
		return "", false
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	if fi.Size() > MaxImage || s.images+fi.Size() > MaxImages {
		return "", true
	}
	data, err := readMax(abs, MaxImage)
	if err != nil {
		return "", false
	}
	if len(data) > MaxImage || s.images+int64(len(data)) > MaxImages {
		return "", true
	}
	if !matches(data, mime) {
		return "", false
	}
	s.images += int64(len(data))
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), false
}

func renderImage(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	img, ok := n.(*inlineImage)
	if !ok || !entering {
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<img src="`)
	_, _ = w.Write(util.EscapeHTML(img.src))
	_, _ = w.WriteString(`" alt="`)
	_, _ = w.Write(util.EscapeHTML(img.alt))
	_ = w.WriteByte('"')
	if img.title != nil {
		_, _ = w.WriteString(` title="`)
		_, _ = w.Write(util.EscapeHTML(img.title))
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	return ast.WalkSkipChildren, nil
}

func renderNotice(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if nt, ok := n.(*notice); ok && entering {
		_, _ = w.WriteString(` <span class="doc-notice">`)
		_, _ = w.Write(util.EscapeHTML([]byte(nt.text)))
		_, _ = w.WriteString(`</span>`)
	}
	return ast.WalkSkipChildren, nil
}

func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".svg":
		return "image/svg+xml"
	}
	return ""
}

func readMax(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

func matches(data []byte, mime string) bool {
	if mime != "image/svg+xml" {
		return http.DetectContentType(data) == mime
	}
	head := bytes.TrimSpace(bytes.TrimPrefix(data, svgBOM))
	return bytes.HasPrefix(head, []byte("<svg")) || bytes.HasPrefix(head, []byte("<?xml"))
}
