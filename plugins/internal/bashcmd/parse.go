package bashcmd

import (
	"cmp"
	"slices"
	"strings"
)

const (
	delimiterEnd = " \t\n;&|<>()"
	quoteChars   = `'"\`
	hereString   = "<<<"
	maxLen       = 256 << 10
	maxDepth     = 64
	arithDepth   = 32
)

var keywords = []string{"then", "do", "else", "{", "!", "time"}

const (
	sinkWords = iota
	sinkHere
	sinkDrop
)

type Command struct {
	Words    []string
	Offsets  []int
	At       int
	Heredocs []Heredoc
}

type Heredoc struct {
	Op   string
	Body string
	At   int
}

type heredoc struct {
	op     string
	delim  string
	strip  bool
	quoted bool
	owner  int
	done   bool
	doc    Heredoc
}

type parser struct {
	src     string
	out     []Command
	pending []*heredoc
	depth   int
	bad     bool
	unread  bool
}

type commandState struct {
	p      *parser
	words  []string
	offs   []int
	word   strings.Builder
	inWord bool
	wordAt int
	sink   int
	docs   []Heredoc
	mine   []*heredoc
	cases  int
	inCase bool
	caseAt bool
}

func Parse(line string) ([]Command, bool) {
	if len(line) > maxLen {
		return nil, false
	}
	p := &parser{src: line}
	p.commands(0, 0)
	slices.SortStableFunc(p.out, func(x, y Command) int { return cmp.Compare(x.At, y.At) })
	return p.out, !p.bad && !p.unread
}

func (c *commandState) begin(i int) {
	if !c.inWord {
		c.inWord, c.wordAt = true, i
	}
}

func (c *commandState) endWord() {
	if !c.inWord {
		return
	}
	switch c.sink {
	case sinkHere:
		c.docs = append(c.docs, Heredoc{Op: hereString, Body: c.word.String(), At: c.wordAt})
	case sinkWords:
		w := c.word.String()
		c.words = append(c.words, w)
		c.offs = append(c.offs, c.wordAt)
		n := len(c.words)
		switch {
		case w == "in" && (n >= 3 && caseWord(c.words[:n-1]) || n == 1 && c.caseAt):
			c.cases++
			c.inCase = true
		case w == "esac" && c.cases > 0 && (n == 1 || c.inCase && c.words[n-2] == "in"):
			c.cases--
			c.inCase = false
		}
	}
	c.word.Reset()
	c.inWord, c.sink = false, sinkWords
}

func (c *commandState) redirect() {
	if c.inWord && strings.Trim(c.word.String(), "0123456789") == "" {
		c.word.Reset()
		c.inWord = false
	}
	c.endWord()
	c.sink = sinkDrop
}

func caseWord(words []string) bool {
	n := len(words)
	return n >= 2 && words[n-2] == "case" && !slices.ContainsFunc(words[:n-2], func(k string) bool { return !slices.Contains(keywords, k) })
}

func (c *commandState) close() {
	c.end()
	if c.cases > 0 {
		c.p.bad = true
	}
}

func (c *commandState) end() {
	c.endWord()
	if len(c.words) > 0 {
		c.caseAt = caseWord(c.words)
		cmd := Command{Words: c.words, Offsets: c.offs, At: c.offs[0], Heredocs: c.docs}
		for _, h := range c.mine {
			if h.done {
				cmd.Heredocs = append(cmd.Heredocs, h.doc)
			}
		}
		c.p.out = append(c.p.out, cmd)
		for _, h := range c.mine {
			h.owner = len(c.p.out) - 1
		}
	}
	c.words, c.offs, c.docs, c.mine, c.sink = nil, nil, nil, nil, sinkWords
}

func (c *commandState) endPattern() bool {
	c.endWord()
	if !c.inCase {
		return false
	}
	c.inCase = false
	c.end()
	return true
}

func (p *parser) commands(i int, closer byte) int {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxDepth {
		p.bad = true
		return len(p.src)
	}
	c := &commandState{p: p}
	depth := 0
	src := p.src
	for i < len(src) && !p.bad {
		ch := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case ch == '\\':
			if next != 0 && next != '\n' {
				c.begin(i)
				c.word.WriteByte(next)
			}
			i += 2
		case ch == '\'':
			c.begin(i)
			j := strings.IndexByte(src[i+1:], '\'')
			if j < 0 {
				p.bad = true
				i = len(src)
				continue
			}
			c.word.WriteString(src[i+1 : i+1+j])
			i += j + 2
		case ch == '$' && next == '\'':
			c.begin(i)
			i = p.ansi(i+2, &c.word)
		case ch == '"':
			c.begin(i)
			i = p.double(i+1, &c.word)
		case ch == '$' && next == '(':
			c.begin(i)
			j := p.dollarParen(i)
			c.word.WriteString(src[i:j])
			i = j
		case ch == '$' && next == '[':
			c.begin(i)
			j := p.bracket(i + 2)
			c.word.WriteString(src[i:j])
			i = j
		case ch == '(' && next == '(' && !c.inWord && p.arith(i) > 0:
			c.begin(i)
			j := p.arithmetic(i)
			c.word.WriteString(src[i:j])
			i = j
		case (ch == '<' || ch == '>') && next == '(':
			c.begin(i)
			j := p.commands(i+2, ')')
			c.word.WriteString(src[i:j])
			i = j
		case ch == '$' && next == '{':
			c.begin(i)
			j := p.param(i + 2)
			c.word.WriteString(src[i:j])
			i = j
		case ch == '`' && closer == '`':
			c.close()
			return i + 1
		case ch == '`':
			c.begin(i)
			j := p.commands(i+1, '`')
			c.word.WriteString(src[i:j])
			i = j
		case ch == ')' && c.endPattern():
			i++
		case ch == ')' && depth == 0 && closer == ')':
			c.close()
			return i + 1
		case ch == ')':
			depth = max(depth-1, 0)
			c.end()
			i++
		case ch == '(' && c.inCase && !c.inWord:
			c.end()
			i++
		case ch == '(':
			depth++
			c.end()
			i++
		case ch == '&' && next == '>':
			c.redirect()
			i += 2
			if i < len(src) && src[i] == '>' {
				i++
			}
		case ch == ';' && (next == ';' || next == '&') && c.cases > 0:
			c.end()
			c.inCase = true
			i += 2
			if i < len(src) && src[i] == '&' {
				i++
			}
		case ch == ';' || ch == '&' || ch == '|':
			c.end()
			i++
		case ch == '\n':
			c.end()
			i = p.bodies(i+1, closer)
		case strings.HasPrefix(src[i:], hereString):
			c.endWord()
			c.sink = sinkHere
			i += len(hereString)
		case ch == '<' && next == '<':
			c.endWord()
			var h *heredoc
			h, i = p.heredocStart(i)
			if h != nil {
				c.mine = append(c.mine, h)
			}
		case ch == '<' || ch == '>':
			c.redirect()
			i++
			if i < len(src) && strings.IndexByte("<>&|", src[i]) >= 0 {
				i++
			}
		case ch == '#' && !c.inWord:
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				j = len(src) - i
			}
			i += j
		case ch == ' ' || ch == '\t' || ch == '\r':
			c.endWord()
			i++
		default:
			c.begin(i)
			c.word.WriteByte(ch)
			i++
		}
	}
	if closer != 0 {
		p.bad = true
	}
	c.close()
	return len(src)
}

func (p *parser) double(i int, word *strings.Builder) int {
	src := p.src
	for i < len(src) && !p.bad {
		ch := src[i]
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case ch == '"':
			return i + 1
		case ch == '\\' && next != 0:
			if !strings.ContainsRune("$`\"\\\n", rune(next)) {
				word.WriteByte(ch)
			}
			if next != '\n' {
				word.WriteByte(next)
			}
			i += 2
		case ch == '$' && next == '(':
			j := p.dollarParen(i)
			word.WriteString(src[i:j])
			i = j
		case ch == '`':
			j := p.commands(i+1, '`')
			word.WriteString(src[i:j])
			i = j
		default:
			word.WriteByte(ch)
			i++
		}
	}
	p.bad = true
	return len(src)
}

func (p *parser) arith(i int) int {
	src := p.src
	if !strings.HasPrefix(src[i:], "((") {
		return -1
	}
	depth := 0
	for i += 2; i < len(src) && depth <= arithDepth; i++ {
		switch {
		case src[i] == '`':
			j := strings.IndexByte(src[i+1:], '`')
			if j < 0 {
				return -1
			}
			i += j + 1
		case src[i] == '(':
			depth++
		case src[i] == ')' && depth > 0:
			depth--
		case src[i] == ')' && i+1 < len(src) && src[i+1] == ')':
			return i + 2
		case src[i] == ')':
			return -1
		}
	}
	return -1
}

func (p *parser) dollarParen(i int) int {
	if p.arith(i+1) > 0 {
		return p.arithmetic(i + 1)
	}
	return p.commands(i+2, ')')
}

func (p *parser) arithmetic(i int) int {
	j := p.arith(i)
	p.expand(i+2, j-2)
	return j
}

func (p *parser) bracket(start int) int {
	depth := 0
	for i := start; i < len(p.src); i++ {
		switch {
		case p.src[i] == '[':
			depth++
		case p.src[i] == ']' && depth == 0:
			p.expand(start, i)
			return i + 1
		case p.src[i] == ']':
			depth--
		}
	}
	p.bad = true
	return len(p.src)
}

func (p *parser) param(i int) int {
	src := p.src
	var discard strings.Builder
	depth := 0
	for i < len(src) && !p.bad {
		ch := src[i]
		switch {
		case ch == '}' && depth == 0:
			return i + 1
		case ch == '}':
			depth--
			i++
		case ch == '{':
			depth++
			i++
		case ch == '\\':
			i += 2
		case ch == '\'':
			j := strings.IndexByte(src[i+1:], '\'')
			if j < 0 {
				p.bad = true
				return len(src)
			}
			i += j + 2
		case ch == '"':
			i = p.double(i+1, &discard)
		case ch == '$' && i+1 < len(src) && src[i+1] == '(':
			i = p.dollarParen(i)
		case ch == '`':
			i = p.commands(i+1, '`')
		default:
			i++
		}
	}
	p.bad = true
	return len(src)
}

func (p *parser) ansi(i int, word *strings.Builder) int {
	src := p.src
	for i < len(src) {
		switch src[i] {
		case '\'':
			return i + 1
		case '\\':
			word.WriteString(src[i:min(i+2, len(src))])
			i += 2
		default:
			word.WriteByte(src[i])
			i++
		}
	}
	p.bad = true
	return len(src)
}

func (p *parser) heredocStart(op int) (*heredoc, int) {
	src := p.src
	i := op + 2
	strip := i < len(src) && src[i] == '-'
	if strip {
		i++
	}
	for i < len(src) && (src[i] == ' ' || src[i] == '\t') {
		i++
	}
	var delim strings.Builder
	quoted := false
	for i < len(src) && !strings.ContainsRune(delimiterEnd, rune(src[i])) {
		if strings.IndexByte(quoteChars, src[i]) >= 0 {
			quoted = true
		} else {
			delim.WriteByte(src[i])
		}
		i++
	}
	if delim.Len() == 0 {
		p.bad = true
		return nil, len(src)
	}
	h := &heredoc{op: src[op:i], delim: delim.String(), strip: strip, quoted: quoted, owner: -1}
	p.pending = append(p.pending, h)
	return h, i
}

func (p *parser) bodies(i int, closer byte) int {
	src := p.src
	pending := p.pending
	p.pending = nil
	for _, h := range pending {
		var body []string
		start, end := i, len(src)
		closed := false
		for i < len(src) && !closed {
			n := strings.IndexByte(src[i:], '\n')
			if n < 0 {
				n = len(src) - i
			}
			line := src[i : i+n]
			if h.strip {
				line = strings.TrimLeft(line, "\t")
			}
			if line == h.delim {
				end = i
				i, closed = min(i+n+1, len(src)), true
				continue
			}
			if rest, ok := strings.CutPrefix(line, h.delim); ok && closer != 0 && rest != "" && rest[0] == closer {
				end = i
				i, closed = i+n-len(rest), true
				continue
			}
			body = append(body, line)
			i = min(i+n+1, len(src))
		}
		if !closed {
			p.bad = true
		}
		if !h.quoted {
			p.expandBody(start, end)
		}
		h.doc, h.done = Heredoc{Op: h.op, Body: strings.Join(body, "\n"), At: start}, true
		if h.owner >= 0 {
			p.out[h.owner].Heredocs = append(p.out[h.owner].Heredocs, h.doc)
		}
	}
	return i
}

func (p *parser) expandBody(i, end int) {
	sub := &parser{src: p.src[:end], depth: p.depth}
	sub.expand(i, end)
	p.out = append(p.out, sub.out...)
	p.unread = p.unread || sub.bad || sub.unread
}

func (p *parser) expand(i, end int) {
	src := p.src
	for i < end && !p.bad {
		next := byte(0)
		if i+1 < len(src) {
			next = src[i+1]
		}
		switch {
		case src[i] == '\\':
			i += 2
		case src[i] == '$' && next == '(':
			i = p.dollarParen(i)
		case src[i] == '$' && next == '{':
			i = p.param(i + 2)
		case src[i] == '`':
			i = p.commands(i+1, '`')
		default:
			i++
		}
	}
	if i > end {
		p.bad = true
	}
}
