package docrender

import (
	"net/url"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark/ast"
)

func (s *state) link(n *ast.Link) {
	href, ok := s.href(string(n.Destination))
	if !ok {
		unwrap(n)
		return
	}
	n.Destination = []byte(href)
}

func (s *state) autoLink(n *ast.AutoLink, src []byte) {
	if web(string(n.URL(src))) {
		return
	}
	n.Parent().ReplaceChild(n.Parent(), n, ast.NewString(n.Label(src)))
}

func (s *state) href(dest string) (string, bool) {
	if strings.HasPrefix(dest, "#") || web(dest) {
		return dest, true
	}
	u, err := url.Parse(dest)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", false
	}
	abs, ok := s.local(u.Path)
	if !ok {
		return "", false
	}
	if !s.opts.IsDoc(abs) {
		return "clubmate-file:" + abs, true
	}
	if u.Fragment != "" {
		return "clubmate-doc:" + abs + "#" + u.Fragment, true
	}
	return "clubmate-doc:" + abs, true
}

func (s *state) local(path string) (string, bool) {
	if s.opts.Path == "" || s.opts.Allowed == nil {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(s.opts.Path), path)
	}
	abs := resolve(path)
	return abs, s.opts.Allowed(abs)
}

func web(dest string) bool {
	u, err := url.Parse(dest)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	return (scheme == "http" || scheme == "https") && u.Host != ""
}

func resolve(path string) string {
	path = filepath.Clean(path)
	var rest []string
	for {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return filepath.Join(append([]string{resolved}, rest...)...)
		}
		dir := filepath.Dir(path)
		if dir == path {
			return filepath.Join(append([]string{path}, rest...)...)
		}
		rest = append([]string{filepath.Base(path)}, rest...)
		path = dir
	}
}
