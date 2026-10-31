package sessionhost

import (
	"bytes"
	"slices"
)

var (
	statusQuery = []byte("\x1b[5n")
	statusOK    = []byte("\x1b[0n")
)

type statusProbe struct {
	tail []byte
}

func (s *statusProbe) feed(p []byte) bool {
	edge := append(s.tail, p[:min(len(p), len(statusQuery)-1)]...)
	seen := bytes.Contains(edge, statusQuery) || bytes.Contains(p, statusQuery)
	if len(p) >= len(statusQuery)-1 {
		s.tail = slices.Clone(p[len(p)-(len(statusQuery)-1):])
	} else {
		s.tail = slices.Clone(edge[max(0, len(edge)-(len(statusQuery)-1)):])
	}
	return seen
}
