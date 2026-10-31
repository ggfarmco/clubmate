package sessionhost

import "bytes"

const (
	scanGround byte = iota
	scanEscape
	scanString
	scanStringEscape
)

type bellScanner struct {
	state byte
}

func (b *bellScanner) feed(p []byte) bool {
	rang := false
	for i := 0; i < len(p); i++ {
		if b.state == scanGround {
			j := bytes.IndexAny(p[i:], "\x07\x1b")
			if j < 0 {
				return rang
			}
			i += j
		}
		rang = b.step(p[i]) || rang
	}
	return rang
}

func (b *bellScanner) step(c byte) bool {
	switch b.state {
	case scanGround:
		switch c {
		case 0x07:
			return true
		case 0x1b:
			b.state = scanEscape
		}
	case scanEscape:
		switch c {
		case ']', 'P', '_', '^', 'X':
			b.state = scanString
		case 0x1b:
		default:
			b.state = scanGround
		}
	case scanString:
		switch c {
		case 0x07, 0x18, 0x1a:
			b.state = scanGround
		case 0x1b:
			b.state = scanStringEscape
		}
	case scanStringEscape:
		if c == '\\' {
			b.state = scanGround
			return false
		}
		b.state = scanEscape
		return b.step(c)
	}
	return false
}
