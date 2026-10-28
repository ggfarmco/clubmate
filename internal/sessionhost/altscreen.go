package sessionhost

import (
	"bytes"
	"slices"
)

const maxAltCarry = 32

type altScreen struct {
	on    bool
	carry []byte
}

func (a *altScreen) feed(p []byte) {
	data := p
	if len(a.carry) > 0 {
		data = append(a.carry, p...)
	}
	a.carry = nil
	for i := 0; i < len(data); {
		j := bytes.IndexByte(data[i:], 0x1b)
		if j < 0 {
			return
		}
		i += j
		n, complete := a.parse(data[i:])
		if !complete {
			if len(data)-i <= maxAltCarry {
				a.carry = slices.Clone(data[i:])
			}
			return
		}
		i += n
	}
}

func (a *altScreen) parse(seq []byte) (int, bool) {
	if len(seq) < 2 {
		return 0, false
	}
	if seq[1] == 'c' {
		a.on = false
		return 2, true
	}
	if seq[1] != '[' {
		return 1, true
	}
	if len(seq) < 3 {
		return 0, false
	}
	if seq[2] != '?' {
		return 2, true
	}
	j := 3
	for j < len(seq) && (seq[j] == ';' || seq[j] >= '0' && seq[j] <= '9') {
		j++
	}
	if j == len(seq) {
		return 0, false
	}
	if seq[j] == 0x1b {
		return j, true
	}
	if final := seq[j]; final == 'h' || final == 'l' {
		for param := range bytes.SplitSeq(seq[3:j], []byte{';'}) {
			switch string(param) {
			case "47", "1047", "1049":
				a.on = final == 'h'
			}
		}
	}
	return j + 1, true
}
