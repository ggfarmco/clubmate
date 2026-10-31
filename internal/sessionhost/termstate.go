package sessionhost

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const (
	maxAltCarry     = 32
	maxQueryBytes   = 512
	maxKittyStack   = 16
	resizeEventMode = 2048
)

var (
	altScreenReset = []byte("\x1b[?1049h\x1b[2J\x1b[H")
	replayedModes  = []int{1, 1000, 1002, 1003, 1004, 1005, 1006, 1015, 1016, 2004, 2027, 2031}
)

type termState struct {
	alt     bool
	modes   map[int]bool
	kitty   []int
	queries []byte
	carry   []byte
	report  bool
}

type csi struct {
	prefix byte
	params string
	inter  string
	final  byte
}

func (s *termState) feed(p []byte, detached bool) {
	data := p
	if len(s.carry) > 0 {
		data = append(s.carry, p...)
	}
	s.carry = nil
	for i := 0; i < len(data); {
		j := bytes.IndexByte(data[i:], 0x1b)
		if j < 0 {
			return
		}
		i += j
		n, complete := s.parse(data[i:], detached)
		if !complete {
			if len(data)-i <= maxAltCarry {
				s.carry = slices.Clone(data[i:])
			}
			return
		}
		i += n
	}
}

func (s *termState) prelude() []byte {
	out := slices.Clone(altScreenReset)
	for _, m := range replayedModes {
		if s.modes[m] {
			out = fmt.Appendf(out, "\x1b[?%dh", m)
		}
	}
	if k := s.kittyFlags(); k > 0 {
		out = fmt.Appendf(out, "\x1b[>%du", k)
	}
	return out
}

func (s *termState) takeQueries() []byte {
	q := s.queries
	s.queries = nil
	return q
}

func (s *termState) takeSizeReport() bool {
	r := s.report
	s.report = false
	return r
}

func (s *termState) resizeEvents() bool {
	return s.modes[resizeEventMode]
}

func (s *termState) parse(seq []byte, detached bool) (int, bool) {
	if len(seq) < 2 {
		return 0, false
	}
	if seq[1] == 'c' {
		*s = termState{}
		return 2, true
	}
	if seq[1] != '[' {
		return 1, true
	}
	j := 2
	var c csi
	if j < len(seq) && strings.IndexByte("<=>?", seq[j]) >= 0 {
		c.prefix = seq[j]
		j++
	}
	start := j
	for j < len(seq) && (seq[j] == ';' || seq[j] == ':' || seq[j] >= '0' && seq[j] <= '9') {
		j++
	}
	c.params = string(seq[start:j])
	start = j
	for j < len(seq) && seq[j] >= 0x20 && seq[j] <= 0x2f {
		j++
	}
	c.inter = string(seq[start:j])
	if j == len(seq) {
		return 0, false
	}
	if seq[j] < 0x40 || seq[j] > 0x7e {
		return j, true
	}
	c.final = seq[j]
	if s.isQuery(c) {
		if detached && s.alt && len(s.queries)+j+1 <= maxQueryBytes {
			s.queries = append(s.queries, seq[:j+1]...)
		}
	} else {
		s.apply(c)
	}
	return j + 1, true
}

func (s *termState) isQuery(c csi) bool {
	switch {
	case c.prefix == '?' && c.inter == "$" && c.final == 'p':
		return true
	case c.prefix == '?' && c.params == "" && c.inter == "" && c.final == 'u':
		return true
	default:
		return c.prefix == 0 && c.inter == "" && c.final == 'c' && (c.params == "" || c.params == "0")
	}
}

func (s *termState) apply(c csi) {
	if c.inter != "" {
		return
	}
	switch {
	case c.prefix == '?' && (c.final == 'h' || c.final == 'l'):
		s.setModes(c.params, c.final == 'h')
	case c.prefix == '>' && c.final == 'u':
		s.kitty = append(s.kitty, param(c.params, 0, 0))
		if len(s.kitty) > maxKittyStack {
			s.kitty = s.kitty[1:]
		}
	case c.prefix == '<' && c.final == 'u':
		s.kitty = s.kitty[:max(0, len(s.kitty)-param(c.params, 0, 1))]
	case c.prefix == '=' && c.final == 'u':
		s.setKitty(param(c.params, 0, 0), param(c.params, 1, 1))
	}
}

func (s *termState) setModes(params string, on bool) {
	for p := range strings.SplitSeq(params, ";") {
		m, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		switch m {
		case 47, 1047, 1049:
			s.alt = on
			if !on {
				s.queries = nil
			}
		case resizeEventMode:
			s.report = s.report || on && !s.modes[m]
			s.setMode(m, on)
		default:
			if slices.Contains(replayedModes, m) {
				s.setMode(m, on)
			}
		}
	}
}

func (s *termState) setMode(m int, on bool) {
	if s.modes == nil {
		s.modes = map[int]bool{}
	}
	s.modes[m] = on
}

func (s *termState) setKitty(flags, mode int) {
	if len(s.kitty) == 0 {
		s.kitty = []int{0}
	}
	top := &s.kitty[len(s.kitty)-1]
	switch mode {
	case 1:
		*top = flags
	case 2:
		*top |= flags
	case 3:
		*top &^= flags
	}
}

func (s *termState) kittyFlags() int {
	if len(s.kitty) == 0 {
		return 0
	}
	return s.kitty[len(s.kitty)-1]
}

func param(params string, i, def int) int {
	for n, p := range strings.Split(params, ";") {
		if n != i {
			continue
		}
		v, err := strconv.Atoi(p)
		if err != nil || v == 0 && def != 0 {
			return def
		}
		return v
	}
	return def
}
