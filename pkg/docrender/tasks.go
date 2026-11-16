package docrender

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

const TaskPath = "/task/"

var plain = goldmark.New(goldmark.WithExtensions(extension.GFM))

func TaskHref(id int) string {
	return "clubmate-doc:" + TaskPath + strconv.Itoa(id)
}

func TaskID(target string) (int, bool) {
	rest, ok := strings.CutPrefix(target, TaskPath)
	if !ok {
		return 0, false
	}
	id, err := strconv.Atoi(rest)
	return id, err == nil && id > 0
}

func TaskIDs(src []byte) []int {
	var ids []int
	for _, n := range taskTexts(plain.Parser().Parse(text.NewReader(src))) {
		for _, t := range taskTokens(n.Segment.Value(src)) {
			if !slices.Contains(ids, t.id) {
				ids = append(ids, t.id)
			}
		}
	}
	return ids
}

type taskToken struct {
	start, end, id int
}

func taskTexts(root ast.Node) []*ast.Text {
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
	return texts
}

func taskTokens(value []byte) []taskToken {
	var out []taskToken
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
		out = append(out, taskToken{start: i, end: end, id: id})
		i = end - 1
	}
	return out
}

func (s *state) taskLinks(root ast.Node, src []byte) {
	for _, n := range taskTexts(root) {
		s.splitTasks(n, src)
	}
}

func (s *state) splitTasks(n *ast.Text, src []byte) {
	seg := n.Segment
	parent := n.Parent()
	last := 0
	for _, t := range taskTokens(seg.Value(src)) {
		title, ok := s.opts.Task(t.id)
		if !ok {
			continue
		}
		if t.start > last {
			parent.InsertBefore(parent, n, ast.NewTextSegment(text.NewSegment(seg.Start+last, seg.Start+t.start)))
		}
		link := ast.NewLink()
		link.Destination = []byte(TaskHref(t.id))
		link.Title = []byte(title)
		link.AppendChild(link, ast.NewTextSegment(text.NewSegment(seg.Start+t.start, seg.Start+t.end)))
		parent.InsertBefore(parent, n, link)
		last = t.end
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
