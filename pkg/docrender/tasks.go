package docrender

import (
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

const TaskPath = "/task/"

func TaskHref(id int) string {
	return "clubmate-doc:" + TaskPath + strconv.Itoa(id)
}

func (s *state) taskLinks(root ast.Node, src []byte) {
	var texts []*ast.Text
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link, *ast.AutoLink, *ast.Image, *ast.CodeSpan, *ast.CodeBlock, *ast.FencedCodeBlock, *ast.HTMLBlock, *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		case *ast.Text:
			texts = append(texts, n)
		}
		return ast.WalkContinue, nil
	})
	for _, n := range texts {
		s.splitTasks(n, src)
	}
}

func (s *state) splitTasks(n *ast.Text, src []byte) {
	seg := n.Segment
	value := seg.Value(src)
	parent := n.Parent()
	last := 0
	for i := 0; i < len(value); i++ {
		if value[i] != '#' || i > 0 && wordRune(value[:i], true) {
			continue
		}
		end := i + 1
		for end < len(value) && value[end] >= '0' && value[end] <= '9' {
			end++
		}
		if end == i+1 || end-i > 10 || end < len(value) && wordRune(value[end:], false) {
			continue
		}
		id, err := strconv.Atoi(string(value[i+1 : end]))
		if err != nil {
			continue
		}
		title, ok := s.opts.Task(id)
		if !ok {
			continue
		}
		if i > last {
			parent.InsertBefore(parent, n, ast.NewTextSegment(text.NewSegment(seg.Start+last, seg.Start+i)))
		}
		link := ast.NewLink()
		link.Destination = []byte(TaskHref(id))
		link.Title = []byte(title)
		link.AppendChild(link, ast.NewTextSegment(text.NewSegment(seg.Start+i, seg.Start+end)))
		parent.InsertBefore(parent, n, link)
		last = end
		i = end - 1
	}
	if last > 0 {
		n.Segment = text.NewSegment(seg.Start+last, seg.Stop)
	}
}

func wordRune(b []byte, before bool) bool {
	var r rune
	if before {
		r, _ = utf8.DecodeLastRune(b)
	} else {
		r, _ = utf8.DecodeRune(b)
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '&' || r == '#' || r == '/'
}
