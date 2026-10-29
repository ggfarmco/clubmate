package session

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxLineRunes = 4096
	maxColumn    = 512
)

type screenLine struct {
	runes  []rune
	cursor int
}

func Plain(b []byte, limit int) string {
	out := make([]byte, 0, min(len(b), limit))
	commit := func(text string) {
		out = append(out, text...)
		if len(out) > 2*limit {
			out = append(out[:0], out[len(out)-limit:]...)
		}
	}
	var line screenLine
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b && i+1 < len(b) && b[i+1] == '[':
			end, final, params := csi(b, i)
			line.control(final, params)
			i = end
			continue
		case c == 0x1b:
			i = skipEscape(b, i)
			continue
		case c == '\r':
			line.cursor = 0
		case c == '\b':
			line.move(-1)
		case c == '\n':
			commit(line.text() + "\n")
			line = screenLine{}
		case c == '\t':
			line.put('\t')
		case c < 0x20 || c == 0x7f:
		default:
			r, size := utf8.DecodeRune(b[i:])
			if r != utf8.RuneError || size > 1 {
				line.put(r)
			}
			i += size
			continue
		}
		i++
	}
	commit(line.text())
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return strings.ToValidUTF8(string(out), "")
}

func (l *screenLine) put(r rune) {
	if l.cursor >= maxLineRunes {
		return
	}
	for len(l.runes) < l.cursor {
		l.runes = append(l.runes, ' ')
	}
	if l.cursor < len(l.runes) {
		l.runes[l.cursor] = r
	} else {
		l.runes = append(l.runes, r)
	}
	l.cursor++
}

func (l *screenLine) move(n int) {
	target := max(l.cursor+n, 0)
	if n > 0 {
		target = min(target, max(l.cursor, maxColumn))
	}
	l.cursor = target
}

func (l *screenLine) control(final byte, params string) {
	n, err := strconv.Atoi(params)
	switch {
	case errors.Is(err, strconv.ErrRange):
		n = maxColumn
	case err != nil || n < 1:
		n = 1
	}
	n = min(n, maxColumn)
	switch final {
	case 'D':
		l.move(-n)
	case 'C':
		l.move(n)
	case 'G':
		l.cursor = n - 1
	case 'K':
		switch params {
		case "", "0":
			l.runes = l.runes[:min(l.cursor, len(l.runes))]
		case "1":
			for i := 0; i <= l.cursor && i < len(l.runes); i++ {
				l.runes[i] = ' '
			}
		case "2":
			l.runes = l.runes[:0]
		}
	}
}

func (l *screenLine) text() string {
	return strings.TrimRight(string(l.runes), " ")
}

func csi(b []byte, i int) (int, byte, string) {
	j := i + 2
	for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
		j++
	}
	if j >= len(b) {
		return len(b), 0, ""
	}
	return j + 1, b[j], string(b[i+2 : j])
}

func skipEscape(b []byte, i int) int {
	if i+1 >= len(b) {
		return len(b)
	}
	if b[i+1] < 0x20 {
		return i + 1
	}
	switch b[i+1] {
	case ']', 'P', 'X', '^', '_':
		for j := i + 2; j < len(b); j++ {
			if b[j] == 0x07 {
				return j + 1
			}
			if b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\' {
				return j + 2
			}
		}
		return len(b)
	}
	j := i + 1
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f {
		j++
	}
	return min(j+1, len(b))
}
